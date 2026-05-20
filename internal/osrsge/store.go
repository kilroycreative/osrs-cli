package osrsge

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

func openDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 5000;`); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func ensureSchema(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS items (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  members INTEGER NOT NULL,
  examine TEXT,
  low_alch INTEGER,
  high_alch INTEGER,
  value INTEGER,
  buy_limit INTEGER,
  icon TEXT,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_items_name ON items(name);

CREATE TABLE IF NOT EXISTS latest_prices (
  item_id INTEGER PRIMARY KEY,
  high INTEGER,
  high_time INTEGER,
  low INTEGER,
  low_time INTEGER,
  updated_at INTEGER NOT NULL,
  FOREIGN KEY(item_id) REFERENCES items(id)
);

CREATE TABLE IF NOT EXISTS latest_snapshots (
  item_id INTEGER NOT NULL,
  snapshot_at INTEGER NOT NULL,
  high INTEGER,
  high_time INTEGER,
  low INTEGER,
  low_time INTEGER,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY(item_id, snapshot_at),
  FOREIGN KEY(item_id) REFERENCES items(id)
);
CREATE INDEX IF NOT EXISTS idx_latest_snapshots_item ON latest_snapshots(item_id, snapshot_at);

CREATE TABLE IF NOT EXISTS interval_prices (
  item_id INTEGER NOT NULL,
  interval TEXT NOT NULL,
  timestamp INTEGER NOT NULL,
  avg_high_price INTEGER,
  high_volume INTEGER NOT NULL,
  avg_low_price INTEGER,
  low_volume INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY(item_id, interval, timestamp),
  FOREIGN KEY(item_id) REFERENCES items(id)
);
CREATE INDEX IF NOT EXISTS idx_interval_latest ON interval_prices(interval, timestamp);
CREATE INDEX IF NOT EXISTS idx_interval_item ON interval_prices(item_id, interval, timestamp);

CREATE TABLE IF NOT EXISTS sync_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS watchlist (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  item_id INTEGER NOT NULL,
  below INTEGER,
  above INTEGER,
  min_net_margin INTEGER,
  min_roi REAL,
  min_volume INTEGER,
  cooldown_seconds INTEGER NOT NULL DEFAULT 3600,
  enabled INTEGER NOT NULL DEFAULT 1,
  note TEXT,
  last_triggered_at INTEGER,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  FOREIGN KEY(item_id) REFERENCES items(id)
);
CREATE INDEX IF NOT EXISTS idx_watchlist_item ON watchlist(item_id);

CREATE TABLE IF NOT EXISTS alert_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  rule_id INTEGER NOT NULL,
  item_id INTEGER NOT NULL,
  reason TEXT NOT NULL,
  low INTEGER,
  high INTEGER,
  net_margin INTEGER,
  roi REAL,
  volume INTEGER,
  triggered_at INTEGER NOT NULL,
  FOREIGN KEY(rule_id) REFERENCES watchlist(id),
  FOREIGN KEY(item_id) REFERENCES items(id)
);
CREATE INDEX IF NOT EXISTS idx_alert_events_rule ON alert_events(rule_id, triggered_at);
`)
	return err
}

func (a *app) cmdSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	skipMapping := fs.Bool("skip-mapping", false, "skip mapping refresh")
	skip5m := fs.Bool("skip-5m", false, "skip 5m snapshot")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	start := time.Now()
	counts, err := a.syncCurrent(ctx, !*skipMapping, true, !*skip5m)
	if err != nil {
		return err
	}
	fmt.Printf("synced mapping=%d latest=%d 1h=%d 5m=%d db=%s elapsed=%s\n",
		counts.Mapping, counts.Latest, counts.OneHour, counts.FiveMinute, a.dbPath, time.Since(start).Round(time.Millisecond))
	return nil
}

type syncCounts struct {
	Mapping    int
	Latest     int
	OneHour    int
	FiveMinute int
}

func (a *app) syncCurrent(ctx context.Context, refreshMapping, refresh1h, refresh5m bool) (syncCounts, error) {
	var counts syncCounts
	if refreshMapping {
		items, err := a.client.mapping(ctx)
		if err != nil {
			return counts, err
		}
		if err := saveMapping(a.db, items); err != nil {
			return counts, err
		}
		counts.Mapping = len(items)
	}
	latest, err := a.client.latest(ctx)
	if err != nil {
		return counts, err
	}
	if err := saveLatest(a.db, latest); err != nil {
		return counts, err
	}
	counts.Latest = len(latest.Data)
	if refresh1h {
		hourly, err := a.client.interval(ctx, "1h")
		if err != nil {
			return counts, err
		}
		if err := saveInterval(a.db, "1h", hourly); err != nil {
			return counts, err
		}
		counts.OneHour = len(hourly.Data)
	}
	if refresh5m {
		five, err := a.client.interval(ctx, "5m")
		if err != nil {
			return counts, err
		}
		if err := saveInterval(a.db, "5m", five); err != nil {
			return counts, err
		}
		counts.FiveMinute = len(five.Data)
	}
	return counts, nil
}

func saveMapping(db *sql.DB, items []mappingItem) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`
INSERT INTO items (id, name, members, examine, low_alch, high_alch, value, buy_limit, icon, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  name=excluded.name,
  members=excluded.members,
  examine=excluded.examine,
  low_alch=excluded.low_alch,
  high_alch=excluded.high_alch,
  value=excluded.value,
  buy_limit=excluded.buy_limit,
  icon=excluded.icon,
  updated_at=excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for _, item := range items {
		if _, err := stmt.Exec(item.ID, item.Name, boolInt(item.Members), item.Examine, ptrAny(item.LowAlch), ptrAny(item.HighAlch), ptrAny(item.Value), ptrAny(item.Limit), item.Icon, now); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO sync_state(key, value, updated_at) VALUES('mapping', ?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, strconv.FormatInt(now, 10), now); err != nil {
		return err
	}
	return tx.Commit()
}

func saveLatest(db *sql.DB, resp latestResponse) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`
INSERT INTO latest_prices (item_id, high, high_time, low, low_time, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(item_id) DO UPDATE SET
  high=excluded.high,
  high_time=excluded.high_time,
  low=excluded.low,
  low_time=excluded.low_time,
  updated_at=excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	snapshotStmt, err := tx.Prepare(`
INSERT INTO latest_snapshots (item_id, snapshot_at, high, high_time, low, low_time, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(item_id, snapshot_at) DO UPDATE SET
  high=excluded.high,
  high_time=excluded.high_time,
  low=excluded.low,
  low_time=excluded.low_time,
  updated_at=excluded.updated_at`)
	if err != nil {
		return err
	}
	defer snapshotStmt.Close()
	now := time.Now().Unix()
	for idStr, point := range resp.Data {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue
		}
		if _, err := stmt.Exec(id, ptrAny(point.High), ptrAny(point.HighTime), ptrAny(point.Low), ptrAny(point.LowTime), now); err != nil {
			return err
		}
		if _, err := snapshotStmt.Exec(id, now, ptrAny(point.High), ptrAny(point.HighTime), ptrAny(point.Low), ptrAny(point.LowTime), now); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO sync_state(key, value, updated_at) VALUES('latest', ?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, strconv.FormatInt(now, 10), now); err != nil {
		return err
	}
	return tx.Commit()
}

func saveInterval(db *sql.DB, interval string, resp intervalResponse) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`
INSERT INTO interval_prices (item_id, interval, timestamp, avg_high_price, high_volume, avg_low_price, low_volume, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(item_id, interval, timestamp) DO UPDATE SET
  avg_high_price=excluded.avg_high_price,
  high_volume=excluded.high_volume,
  avg_low_price=excluded.avg_low_price,
  low_volume=excluded.low_volume,
  updated_at=excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for idStr, point := range resp.Data {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue
		}
		if _, err := stmt.Exec(id, interval, resp.Timestamp, ptrAny(point.AvgHighPrice), point.HighVolume, ptrAny(point.AvgLowPrice), point.LowVolume, now); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO sync_state(key, value, updated_at) VALUES(?, ?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, "interval_"+interval, strconv.FormatInt(resp.Timestamp, 10), now); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *app) ensureCurrent(noSync bool, interval string) error {
	if noSync {
		return nil
	}
	var itemCount int
	if err := a.db.QueryRow(`SELECT count(*) FROM items`).Scan(&itemCount); err != nil {
		return err
	}
	refreshMapping := itemCount == 0
	_, err := a.syncCurrent(context.Background(), refreshMapping, interval == "1h", interval == "5m")
	return err
}

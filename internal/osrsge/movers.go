package osrsge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"text/tabwriter"
)

func (a *app) cmdMovers(args []string) error {
	fs := flag.NewFlagSet("movers", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	interval := fs.String("interval", "1h", "comparison interval: 1h or 5m")
	back := fs.Int("back", 1, "number of intervals back to compare")
	limit := fs.Int("limit", 25, "maximum rows")
	minVolume := fs.Int64("min-volume", 100, "minimum current volume")
	sortBy := fs.String("sort", "price-change", "price-change, volume-ratio, volume-change, net-margin")
	members := fs.String("members", "any", "any, members, or free")
	noSync := fs.Bool("no-sync", false, "do not refresh current interval cache")
	jsonOut := fs.Bool("json", false, "emit JSON")
	_, err := parseCommandFlags(fs, args, map[string]bool{"interval": true, "back": true, "limit": true, "min-volume": true, "sort": true, "members": true})
	if err != nil {
		return err
	}
	if *interval != "1h" && *interval != "5m" {
		return errors.New("--interval must be 1h or 5m")
	}
	if *back < 1 {
		return errors.New("--back must be >= 1")
	}
	if *members != "any" && *members != "members" && *members != "free" {
		return errors.New("--members must be any, members, or free")
	}
	if err := a.ensureItems(false); err != nil {
		return err
	}
	if err := a.ensureCurrent(*noSync, *interval); err != nil {
		return err
	}
	step := int64(3600)
	if *interval == "5m" {
		step = 300
	}
	var currentTS int64
	if err := a.db.QueryRow(`SELECT max(timestamp) FROM interval_prices WHERE interval = ?`, *interval).Scan(&currentTS); err != nil {
		return err
	}
	previousTS := currentTS - int64(*back)*step
	if previousTS > 0 {
		if err := a.ensureIntervalBucket(context.Background(), *interval, previousTS); err != nil {
			return err
		}
	}
	movs, err := a.loadMovers(*interval, currentTS, previousTS, *minVolume, *members)
	if err != nil {
		return err
	}
	sortMovers(movs, *sortBy)
	if *limit > 0 && len(movs) > *limit {
		movs = movs[:*limit]
	}
	for i := range movs {
		movs[i].Rank = i + 1
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(movs)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tITEM\tMID THEN\tMID NOW\tMOVE\tMOVE %\tVOL THEN\tVOL NOW\tVOL X\tNET\tROI")
	for _, m := range movs {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%.2f%%\t%s\t%s\t%.2fx\t%s\t%.2f%%\n",
			m.Rank, m.Name, gp(int64(math.Round(m.PreviousMid))), gp(int64(math.Round(m.CurrentMid))),
			gp(int64(math.Round(m.PriceChange))), m.PriceChangePct*100, gp(m.PreviousVolume),
			gp(m.CurrentVolume), m.VolumeRatio, gp(m.CurrentNetMargin), m.CurrentROI*100)
	}
	if len(movs) == 0 {
		fmt.Fprintln(tw, "No movers passed the filters. Try lowering --min-volume or run osrs-ge sync again later.")
	}
	return tw.Flush()
}

func (a *app) ensureIntervalBucket(ctx context.Context, interval string, timestamp int64) error {
	var count int
	if err := a.db.QueryRow(`SELECT count(*) FROM interval_prices WHERE interval = ? AND timestamp = ?`, interval, timestamp).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	resp, err := a.client.intervalAt(ctx, interval, timestamp)
	if err != nil {
		return err
	}
	return saveInterval(a.db, interval, resp)
}

func (a *app) loadMovers(interval string, currentTS, previousTS, minVolume int64, members string) ([]mover, error) {
	rows, err := a.db.Query(`
SELECT i.id, i.name, i.members,
       p.avg_high_price, p.avg_low_price, p.high_volume, p.low_volume,
       c.avg_high_price, c.avg_low_price, c.high_volume, c.low_volume
FROM interval_prices c
JOIN interval_prices p ON p.item_id = c.item_id AND p.interval = c.interval AND p.timestamp = ?
JOIN items i ON i.id = c.item_id
WHERE c.interval = ? AND c.timestamp = ? AND (c.high_volume + c.low_volume) >= ?`, previousTS, interval, currentTS, minVolume)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mover
	for rows.Next() {
		var id int64
		var name string
		var memberInt int
		var ph, pl, ch, cl sql.NullInt64
		var phv, plv, chv, clv int64
		if err := rows.Scan(&id, &name, &memberInt, &ph, &pl, &phv, &plv, &ch, &cl, &chv, &clv); err != nil {
			return nil, err
		}
		isMembers := memberInt != 0
		if members == "members" && !isMembers {
			continue
		}
		if members == "free" && isMembers {
			continue
		}
		prevMid, okPrev := avgPrice(ph, pl)
		curMid, okCur := avgPrice(ch, cl)
		if !okPrev || !okCur || prevMid <= 0 {
			continue
		}
		curVol := chv + clv
		prevVol := phv + plv
		net := int64(0)
		roi := 0.0
		if ch.Valid && cl.Valid && cl.Int64 > 0 {
			net = ch.Int64 - cl.Int64 - geTax(ch.Int64, 0.02, 5_000_000)
			roi = float64(net) / float64(cl.Int64)
		}
		ratio := 0.0
		if prevVol > 0 {
			ratio = float64(curVol) / float64(prevVol)
		}
		out = append(out, mover{
			ID:                id,
			Name:              name,
			Members:           isMembers,
			PreviousMid:       prevMid,
			CurrentMid:        curMid,
			PriceChange:       curMid - prevMid,
			PriceChangePct:    (curMid - prevMid) / prevMid,
			PreviousVolume:    prevVol,
			CurrentVolume:     curVol,
			VolumeChange:      curVol - prevVol,
			VolumeRatio:       ratio,
			CurrentNetMargin:  net,
			CurrentROI:        roi,
			PreviousTimestamp: previousTS,
			CurrentTimestamp:  currentTS,
		})
	}
	return out, rows.Err()
}

func sortMovers(movs []mover, sortBy string) {
	sort.Slice(movs, func(i, j int) bool {
		a, b := movs[i], movs[j]
		switch sortBy {
		case "price-change":
			return math.Abs(a.PriceChangePct) > math.Abs(b.PriceChangePct)
		case "volume-ratio":
			return a.VolumeRatio > b.VolumeRatio
		case "volume-change":
			return a.VolumeChange > b.VolumeChange
		case "net-margin":
			return a.CurrentNetMargin > b.CurrentNetMargin
		default:
			return math.Abs(a.PriceChangePct) > math.Abs(b.PriceChangePct)
		}
	})
}

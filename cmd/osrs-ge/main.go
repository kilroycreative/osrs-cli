package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	_ "modernc.org/sqlite"
)

const (
	version    = "0.1.0"
	apiBaseURL = "https://prices.runescape.wiki/api/v1/osrs"
)

type app struct {
	dbPath    string
	userAgent string
	client    *wikiClient
	db        *sql.DB
}

type wikiClient struct {
	baseURL   string
	userAgent string
	http      *http.Client
}

type mappingItem struct {
	Examine  string `json:"examine"`
	ID       int64  `json:"id"`
	Members  bool   `json:"members"`
	LowAlch  *int64 `json:"lowalch"`
	Limit    *int64 `json:"limit"`
	Value    *int64 `json:"value"`
	HighAlch *int64 `json:"highalch"`
	Icon     string `json:"icon"`
	Name     string `json:"name"`
}

type latestResponse struct {
	Data map[string]latestPoint `json:"data"`
}

type latestPoint struct {
	High     *int64 `json:"high"`
	HighTime *int64 `json:"highTime"`
	Low      *int64 `json:"low"`
	LowTime  *int64 `json:"lowTime"`
}

type intervalResponse struct {
	Timestamp int64                    `json:"timestamp"`
	Data      map[string]intervalPoint `json:"data"`
}

type intervalPoint struct {
	AvgHighPrice *int64 `json:"avgHighPrice"`
	HighVolume   int64  `json:"highPriceVolume"`
	AvgLowPrice  *int64 `json:"avgLowPrice"`
	LowVolume    int64  `json:"lowPriceVolume"`
}

type timeseriesResponse struct {
	Data []timeseriesPoint `json:"data"`
}

type timeseriesPoint struct {
	Timestamp    int64  `json:"timestamp"`
	AvgHighPrice *int64 `json:"avgHighPrice"`
	AvgLowPrice  *int64 `json:"avgLowPrice"`
	HighVolume   int64  `json:"highPriceVolume"`
	LowVolume    int64  `json:"lowPriceVolume"`
}

type itemRecord struct {
	ID       int64
	Name     string
	Members  bool
	BuyLimit sql.NullInt64
	Value    sql.NullInt64
	HighAlch sql.NullInt64
	LowAlch  sql.NullInt64
	Examine  string
}

type opportunity struct {
	Rank           int     `json:"rank"`
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Members        bool    `json:"members"`
	Low            int64   `json:"low"`
	High           int64   `json:"high"`
	Tax            int64   `json:"tax"`
	GrossMargin    int64   `json:"gross_margin"`
	NetMargin      int64   `json:"net_margin"`
	ROI            float64 `json:"roi"`
	SpreadPct      float64 `json:"spread_pct"`
	Volume         int64   `json:"volume"`
	BaselineVolume float64 `json:"baseline_volume"`
	VolumeRatio    float64 `json:"volume_ratio"`
	BuyLimit       int64   `json:"buy_limit,omitempty"`
	LimitProfit    int64   `json:"limit_profit,omitempty"`
	HighAgeSeconds int64   `json:"high_age_seconds"`
	LowAgeSeconds  int64   `json:"low_age_seconds"`
	Score          float64 `json:"score"`
}

type watchRule struct {
	ID              int64           `json:"id"`
	ItemID          int64           `json:"item_id"`
	Name            string          `json:"name"`
	Below           sql.NullInt64   `json:"below,omitempty"`
	Above           sql.NullInt64   `json:"above,omitempty"`
	MinNetMargin    sql.NullInt64   `json:"min_net_margin,omitempty"`
	MinROI          sql.NullFloat64 `json:"min_roi,omitempty"`
	MinVolume       sql.NullInt64   `json:"min_volume,omitempty"`
	CooldownSeconds int64           `json:"cooldown_seconds"`
	Enabled         bool            `json:"enabled"`
	Note            string          `json:"note,omitempty"`
	LastTriggeredAt sql.NullInt64   `json:"last_triggered_at,omitempty"`
	CreatedAt       int64           `json:"created_at"`
	UpdatedAt       int64           `json:"updated_at"`
}

type alertHit struct {
	RuleID          int64   `json:"rule_id"`
	ItemID          int64   `json:"item_id"`
	Name            string  `json:"name"`
	Reason          string  `json:"reason"`
	Low             int64   `json:"low"`
	High            int64   `json:"high"`
	NetMargin       int64   `json:"net_margin"`
	ROI             float64 `json:"roi"`
	Volume          int64   `json:"volume"`
	LastTriggeredAt int64   `json:"last_triggered_at,omitempty"`
}

type mover struct {
	Rank              int     `json:"rank"`
	ID                int64   `json:"id"`
	Name              string  `json:"name"`
	Members           bool    `json:"members"`
	PreviousMid       float64 `json:"previous_mid"`
	CurrentMid        float64 `json:"current_mid"`
	PriceChange       float64 `json:"price_change"`
	PriceChangePct    float64 `json:"price_change_pct"`
	PreviousVolume    int64   `json:"previous_volume"`
	CurrentVolume     int64   `json:"current_volume"`
	VolumeChange      int64   `json:"volume_change"`
	VolumeRatio       float64 `json:"volume_ratio"`
	CurrentNetMargin  int64   `json:"current_net_margin"`
	CurrentROI        float64 `json:"current_roi"`
	PreviousTimestamp int64   `json:"previous_timestamp"`
	CurrentTimestamp  int64   `json:"current_timestamp"`
}

type allocationRow struct {
	Rank          int     `json:"rank"`
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Units         int64   `json:"units"`
	Cost          int64   `json:"cost"`
	ExpectedEdge  int64   `json:"expected_edge"`
	NetMargin     int64   `json:"net_margin"`
	ROI           float64 `json:"roi"`
	Volume        int64   `json:"volume"`
	BuyLimit      int64   `json:"buy_limit,omitempty"`
	RemainingCash int64   `json:"remaining_cash"`
}

type backtestSummary struct {
	ItemID        int64   `json:"item_id"`
	Name          string  `json:"name"`
	Step          string  `json:"step"`
	Signals       int     `json:"signals"`
	Windows       int     `json:"windows"`
	TotalEdge     int64   `json:"total_edge"`
	AverageEdge   float64 `json:"average_edge"`
	WinRate       float64 `json:"win_rate"`
	MaxWindowEdge int64   `json:"max_window_edge"`
	MinWindowEdge int64   `json:"min_window_edge"`
	Assumption    string  `json:"assumption"`
}

type patternReport struct {
	Strategy      string       `json:"strategy"`
	Like          string       `json:"like,omitempty"`
	Cash          int64        `json:"cash"`
	Step          string       `json:"step"`
	Days          int          `json:"days"`
	GeneratedAt   string       `json:"generated_at"`
	CandidateRows int          `json:"candidate_rows"`
	Scanned       int          `json:"scanned"`
	Errors        int          `json:"errors"`
	Hits          []patternHit `json:"hits"`
	Notes         []string     `json:"notes,omitempty"`
}

type patternHit struct {
	Rank            int      `json:"rank"`
	ID              int64    `json:"id"`
	Name            string   `json:"name"`
	Members         bool     `json:"members"`
	BuyLimit        int64    `json:"buy_limit,omitempty"`
	Low             int64    `json:"low"`
	High            int64    `json:"high"`
	Tax             int64    `json:"tax"`
	NetMargin       int64    `json:"net_margin"`
	ROI             float64  `json:"roi"`
	Ratio           float64  `json:"ratio"`
	LowVolume       int64    `json:"low_volume"`
	HighVolume      int64    `json:"high_volume"`
	LowTime         int64    `json:"low_time"`
	HighTime        int64    `json:"high_time"`
	LowTimeISO      string   `json:"low_time_iso"`
	HighTimeISO     string   `json:"high_time_iso"`
	ReboundSeconds  int64    `json:"rebound_seconds"`
	PortfolioUnits  int64    `json:"portfolio_units,omitempty"`
	PortfolioCost   int64    `json:"portfolio_cost,omitempty"`
	PortfolioProfit int64    `json:"portfolio_profit,omitempty"`
	CurrentLow      *int64   `json:"current_low,omitempty"`
	CurrentHigh     *int64   `json:"current_high,omitempty"`
	Setup           string   `json:"setup"`
	Score           float64  `json:"score"`
	Rationale       []string `json:"rationale"`
}

type patternCandidate struct {
	Item        itemRecord
	CurrentLow  *int64
	CurrentHigh *int64
}

type patternScanOptions struct {
	Strategy      string
	Cash          int64
	Step          string
	Cutoff        int64
	MaxRebound    time.Duration
	MinLow        int64
	MaxLow        int64
	MinHigh       int64
	MaxHigh       int64
	MinLowVolume  int64
	MinHighVolume int64
	MinRatio      float64
	MaxRatio      float64
	MinNetMargin  int64
	TaxRate       float64
	TaxCap        int64
}

type rangeBottomReport struct {
	Strategy      string           `json:"strategy"`
	QueryIntent   string           `json:"query_intent"`
	Cash          int64            `json:"cash"`
	Step          string           `json:"step"`
	Days          int              `json:"days"`
	GeneratedAt   string           `json:"generated_at"`
	CandidateRows int              `json:"candidate_rows"`
	Scanned       int              `json:"scanned"`
	Errors        int              `json:"errors"`
	Hits          []rangeBottomHit `json:"hits"`
	Notes         []string         `json:"notes,omitempty"`
}

type rangeBottomHit struct {
	Rank                   int      `json:"rank"`
	ID                     int64    `json:"id"`
	Name                   string   `json:"name"`
	Members                bool     `json:"members"`
	BuyLimit               int64    `json:"buy_limit,omitempty"`
	CurrentPrice           float64  `json:"current_price"`
	CurrentLow             *int64   `json:"current_low,omitempty"`
	CurrentHigh            *int64   `json:"current_high,omitempty"`
	VWAP                   float64  `json:"vwap"`
	RangeLow               float64  `json:"range_low"`
	RangeHigh              float64  `json:"range_high"`
	BottomBand             float64  `json:"bottom_band"`
	Percentile             float64  `json:"percentile"`
	DiscountToVWAP         float64  `json:"discount_to_vwap"`
	DiscountToRangeHigh    float64  `json:"discount_to_range_high"`
	MedianVolume           int64    `json:"median_volume"`
	P25Volume              int64    `json:"p25_volume"`
	ActiveBuckets          int      `json:"active_buckets"`
	ObservedBuckets        int      `json:"observed_buckets"`
	ActiveRatio            float64  `json:"active_ratio"`
	ReboundCycles          int      `json:"rebound_cycles"`
	BottomVisits           int      `json:"bottom_visits"`
	ReboundReliability     float64  `json:"rebound_reliability"`
	LastBottomTime         int64    `json:"last_bottom_time,omitempty"`
	LastBottomTimeISO      string   `json:"last_bottom_time_iso,omitempty"`
	LastReboundTime        int64    `json:"last_rebound_time,omitempty"`
	LastReboundTimeISO     string   `json:"last_rebound_time_iso,omitempty"`
	EstimatedNetToVWAP     int64    `json:"estimated_net_to_vwap"`
	EstimatedNetToRangeTop int64    `json:"estimated_net_to_range_top"`
	PortfolioUnits         int64    `json:"portfolio_units,omitempty"`
	PortfolioCost          int64    `json:"portfolio_cost,omitempty"`
	PortfolioProfitToVWAP  int64    `json:"portfolio_profit_to_vwap,omitempty"`
	Setup                  string   `json:"setup"`
	Score                  float64  `json:"score"`
	Rationale              []string `json:"rationale"`
}

type rangeBottomOptions struct {
	Cash             int64
	Step             string
	Cutoff           int64
	MaxPercentile    float64
	BottomPercentile float64
	MinActiveBuckets int
	MinMedianVolume  int64
	MinP25Volume     int64
	MinCycles        int
	ReboundWindow    time.Duration
	TaxRate          float64
	TaxCap           int64
}

type rangeBucket struct {
	timestamp int64
	mid       float64
	volume    int64
}

type agentManifest struct {
	Name         string              `json:"name"`
	Version      string              `json:"version"`
	GeneratedAt  string              `json:"generated_at"`
	DataSources  []string            `json:"data_sources"`
	Principles   []string            `json:"principles"`
	Tools        []agentToolManifest `json:"tools"`
	OutputNotice []string            `json:"output_notice"`
}

type agentToolManifest struct {
	Name        string   `json:"name"`
	Intent      string   `json:"intent"`
	Command     string   `json:"command"`
	Returns     []string `json:"returns"`
	GoodFor     []string `json:"good_for"`
	Caveats     []string `json:"caveats,omitempty"`
	Example     string   `json:"example"`
	JSONSupport bool     `json:"json_support"`
}

type agentRunReport struct {
	Strategy    string             `json:"strategy"`
	Query       string             `json:"query"`
	GeneratedAt string             `json:"generated_at"`
	Spec        agentResearchSpec  `json:"spec"`
	Probes      []agentProbeResult `json:"probes"`
	SummaryRows []agentSummaryRow  `json:"summary_rows"`
	Warnings    []string           `json:"warnings,omitempty"`
	NextSteps   []string           `json:"next_steps,omitempty"`
}

type agentResearchSpec struct {
	Intent        string   `json:"intent"`
	Cash          string   `json:"cash"`
	Horizons      []string `json:"horizons"`
	PrimaryTools  []string `json:"primary_tools"`
	EvidenceTests []string `json:"evidence_tests"`
	Notes         []string `json:"notes,omitempty"`
}

type agentProbeSpec struct {
	Name    string   `json:"name"`
	Intent  string   `json:"intent"`
	Command []string `json:"command"`
}

type agentProbeResult struct {
	Name        string            `json:"name"`
	Intent      string            `json:"intent"`
	Command     []string          `json:"command"`
	OK          bool              `json:"ok"`
	Error       string            `json:"error,omitempty"`
	Artifact    json.RawMessage   `json:"artifact,omitempty"`
	SummaryRows []agentSummaryRow `json:"summary_rows,omitempty"`
}

type agentSummaryRow struct {
	Rank          int     `json:"rank"`
	Probe         string  `json:"probe"`
	Item          string  `json:"item"`
	PrimaryMetric string  `json:"primary_metric"`
	Evidence      string  `json:"evidence"`
	Setup         string  `json:"setup"`
	Score         float64 `json:"score"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage(os.Stdout)
		return nil
	}

	global := flag.NewFlagSet("osrs-ge", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	dbPath := global.String("db", defaultDBPath(), "SQLite cache path")
	ua := global.String("user-agent", defaultUserAgent(), "descriptive OSRS Wiki API User-Agent")

	commandIndex := 0
	for commandIndex < len(args) {
		arg := args[commandIndex]
		switch {
		case arg == "--db" || arg == "-db" || arg == "--user-agent" || arg == "-user-agent":
			commandIndex += 2
		case strings.HasPrefix(arg, "--db=") || strings.HasPrefix(arg, "-db=") || strings.HasPrefix(arg, "--user-agent=") || strings.HasPrefix(arg, "-user-agent="):
			commandIndex++
		case strings.HasPrefix(arg, "-"):
			return fmt.Errorf("unknown global flag %q", arg)
		default:
			goto parsedGlobalPrefix
		}
	}
parsedGlobalPrefix:
	if commandIndex > 0 {
		if err := global.Parse(args[:commandIndex]); err != nil {
			return err
		}
	}
	if commandIndex >= len(args) {
		printUsage(os.Stdout)
		return nil
	}

	a := &app{
		dbPath:    *dbPath,
		userAgent: *ua,
		client: &wikiClient{
			baseURL:   apiBaseURL,
			userAgent: *ua,
			http: &http.Client{
				Timeout: 30 * time.Second,
			},
		},
	}
	cmd := args[commandIndex]
	cmdArgs := args[commandIndex+1:]

	switch cmd {
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return nil
	case "version":
		fmt.Println(version)
		return nil
	case "sync":
		return a.withDB(func() error { return a.cmdSync(cmdArgs) })
	case "search":
		return a.withDB(func() error { return a.cmdSearch(cmdArgs) })
	case "price", "item":
		return a.withDB(func() error { return a.cmdPrice(cmdArgs) })
	case "opportunities", "margins":
		return a.withDB(func() error { return a.cmdOpportunities(cmdArgs) })
	case "movers":
		return a.withDB(func() error { return a.cmdMovers(cmdArgs) })
	case "allocate":
		return a.withDB(func() error { return a.cmdAllocate(cmdArgs) })
	case "patterns", "shocks":
		return a.withDB(func() error { return a.cmdPatterns(cmdArgs) })
	case "range-bottom", "bottoms", "vwap-bottom":
		return a.withDB(func() error { return a.cmdRangeBottom(cmdArgs) })
	case "agent", "workbench", "research":
		return a.cmdAgent(cmdArgs)
	case "serve", "dashboard":
		return a.cmdServe(cmdArgs)
	case "watch", "alerts":
		return a.withDB(func() error { return a.cmdWatch(cmdArgs) })
	case "backtest":
		return a.withDB(func() error { return a.cmdBacktest(cmdArgs) })
	case "timeseries", "history":
		return a.withDB(func() error { return a.cmdTimeseries(cmdArgs) })
	case "sql":
		return a.withDB(func() error { return a.cmdSQL(cmdArgs) })
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func (a *app) withDB(fn func() error) error {
	db, err := openDB(a.dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	a.db = db
	return fn()
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `osrs-ge - local OSRS Grand Exchange research CLI

Usage:
  osrs-ge [--db PATH] [--user-agent UA] <command> [flags]

Commands:
  sync             Refresh mapping, latest prices, and 1h/5m snapshots
  search QUERY     Fuzzy item search
  price ITEM       Show current price, margin, tax, and volume for one item
  opportunities    Rank high-margin items with volume/freshness filters
  margins          Alias for opportunities
  movers           Compare current and previous 1h/5m buckets
  allocate         Budget-aware advisory allocation across opportunities
  patterns         Find dump/rebound patterns similar to cheap high-limit flips
  shocks           Alias for patterns
  range-bottom     Find items near the bottom of their own range/VWAP
  agent            Agent workbench manifest and flexible multi-probe runs
  serve            Run a local read-only dashboard
  watch            Add/list/remove/check local watch rules
  backtest         Single-item theoretical spread backtest
  timeseries ITEM  Fetch recent item time-series data
  sql QUERY        Read-only SQL against the local cache
  version          Print version

Examples:
  osrs-ge sync
  osrs-ge search "abyssal"
  osrs-ge price "abyssal whip"
  osrs-ge opportunities --limit 25 --min-volume 500 --volume-baseline global-average
  osrs-ge movers --interval 1h --back 1 --limit 20
  osrs-ge allocate --cash 20m --limit 8 --min-volume 1000
  osrs-ge patterns --like "bronze knife" --cash 40m --limit 15
  osrs-ge range-bottom --cash 40m --days 90 --step 6h
  osrs-ge agent run "items at the bottom of VWAP with consistent volume" --json
  osrs-ge serve --addr 127.0.0.1:8765
  osrs-ge watch add "blood rune" --below 280 --min-volume 100000
  osrs-ge watch check
  osrs-ge backtest "blood rune" --cash 5m --step 1h
  osrs-ge margins --sort net-margin --members members --json
  osrs-ge timeseries "blood rune" --step 1h --limit 48`)
}

func parseCommandFlags(fs *flag.FlagSet, args []string, valueFlags map[string]bool) ([]string, error) {
	var flagArgs []string
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			flagArgs = append(flagArgs, arg)
			name := strings.TrimLeft(arg, "-")
			if before, _, ok := strings.Cut(name, "="); ok {
				name = before
			}
			if valueFlags[name] && !strings.Contains(arg, "=") {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("flag needs an argument: %s", arg)
				}
				i++
				flagArgs = append(flagArgs, args[i])
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	if err := fs.Parse(flagArgs); err != nil {
		return nil, err
	}
	return positionals, nil
}

func defaultDBPath() string {
	if p := os.Getenv("OSRS_GE_DB"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "osrs-ge.sqlite"
	}
	return filepath.Join(home, ".osrs-ge", "osrs-ge.sqlite")
}

func defaultUserAgent() string {
	if ua := os.Getenv("OSRS_GE_USER_AGENT"); ua != "" {
		return ua
	}
	return "osrs-ge-cli/0.1 (+local research CLI; set OSRS_GE_USER_AGENT for contact)"
}

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

func (c *wikiClient) getJSON(ctx context.Context, path string, dst any) error {
	u, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("GET %s: HTTP %d: %s", u, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

func (c *wikiClient) getJSONRaw(ctx context.Context, rawURL string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("GET %s: HTTP %d: %s", rawURL, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

func (c *wikiClient) mapping(ctx context.Context) ([]mappingItem, error) {
	var items []mappingItem
	err := c.getJSON(ctx, "mapping", &items)
	return items, err
}

func (c *wikiClient) latest(ctx context.Context) (latestResponse, error) {
	var resp latestResponse
	err := c.getJSON(ctx, "latest", &resp)
	return resp, err
}

func (c *wikiClient) interval(ctx context.Context, interval string) (intervalResponse, error) {
	var resp intervalResponse
	err := c.getJSON(ctx, interval, &resp)
	return resp, err
}

func (c *wikiClient) intervalAt(ctx context.Context, interval string, timestamp int64) (intervalResponse, error) {
	var resp intervalResponse
	raw := fmt.Sprintf("%s/%s?timestamp=%d", c.baseURL, url.PathEscape(interval), timestamp)
	err := c.getJSONRaw(ctx, raw, &resp)
	if resp.Timestamp == 0 {
		resp.Timestamp = timestamp
	}
	return resp, err
}

func (c *wikiClient) timeseries(ctx context.Context, id int64, step string) (timeseriesResponse, error) {
	var resp timeseriesResponse
	raw := fmt.Sprintf("%s/timeseries?id=%d&timestep=%s", c.baseURL, id, url.QueryEscape(step))
	err := c.getJSONRaw(ctx, raw, &resp)
	return resp, err
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

func (a *app) cmdSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", 20, "maximum rows")
	jsonOut := fs.Bool("json", false, "emit JSON")
	noSync := fs.Bool("no-sync", false, "do not refresh missing cache")
	positionals, err := parseCommandFlags(fs, args, map[string]bool{"limit": true})
	if err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(positionals, " "))
	if query == "" {
		return errors.New("search requires a query")
	}
	if err := a.ensureItems(*noSync); err != nil {
		return err
	}
	items, err := a.searchItems(query, *limit)
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(items)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tMEMBERS\tLIMIT\tVALUE\tEXAMINE")
	for _, item := range items {
		fmt.Fprintf(tw, "%d\t%s\t%t\t%s\t%s\t%s\n", item.ID, item.Name, item.Members, nullInt(item.BuyLimit), nullInt(item.Value), clip(item.Examine, 72))
	}
	return tw.Flush()
}

func (a *app) ensureItems(noSync bool) error {
	var itemCount int
	if err := a.db.QueryRow(`SELECT count(*) FROM items`).Scan(&itemCount); err != nil {
		return err
	}
	if itemCount == 0 && !noSync {
		items, err := a.client.mapping(context.Background())
		if err != nil {
			return err
		}
		return saveMapping(a.db, items)
	}
	return nil
}

func (a *app) searchItems(query string, limit int) ([]itemRecord, error) {
	pat := "%" + strings.ToLower(query) + "%"
	rows, err := a.db.Query(`
SELECT id, name, members, buy_limit, value, high_alch, low_alch, examine
FROM items
WHERE lower(name) LIKE ?
ORDER BY
  CASE WHEN lower(name) = lower(?) THEN 0 ELSE 1 END,
  instr(lower(name), lower(?)),
  length(name),
  name
LIMIT ?`, pat, query, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []itemRecord
	for rows.Next() {
		var item itemRecord
		var members int
		if err := rows.Scan(&item.ID, &item.Name, &members, &item.BuyLimit, &item.Value, &item.HighAlch, &item.LowAlch, &item.Examine); err != nil {
			return nil, err
		}
		item.Members = members != 0
		out = append(out, item)
	}
	return out, rows.Err()
}

func (a *app) resolveItem(input string) (itemRecord, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return itemRecord{}, errors.New("item is required")
	}
	if id, err := strconv.ParseInt(input, 10, 64); err == nil {
		return a.getItemByID(id)
	}
	items, err := a.searchItems(input, 5)
	if err != nil {
		return itemRecord{}, err
	}
	if len(items) == 0 {
		return itemRecord{}, fmt.Errorf("no item matched %q", input)
	}
	return items[0], nil
}

func (a *app) getItemByID(id int64) (itemRecord, error) {
	var item itemRecord
	var members int
	err := a.db.QueryRow(`SELECT id, name, members, buy_limit, value, high_alch, low_alch, examine FROM items WHERE id = ?`, id).
		Scan(&item.ID, &item.Name, &members, &item.BuyLimit, &item.Value, &item.HighAlch, &item.LowAlch, &item.Examine)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return itemRecord{}, fmt.Errorf("no item id %d in cache; run osrs-ge sync", id)
		}
		return itemRecord{}, err
	}
	item.Members = members != 0
	return item, nil
}

func (a *app) cmdPrice(args []string) error {
	fs := flag.NewFlagSet("price", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit JSON")
	noSync := fs.Bool("no-sync", false, "do not refresh latest/1h cache")
	taxRate := fs.Float64("tax-rate", 0.02, "GE tax rate")
	taxCap := fs.Int64("tax-cap", 5_000_000, "GE tax cap per item")
	positionals, err := parseCommandFlags(fs, args, map[string]bool{"tax-rate": true, "tax-cap": true})
	if err != nil {
		return err
	}
	input := strings.TrimSpace(strings.Join(positionals, " "))
	if input == "" {
		return errors.New("price requires an item name or id")
	}
	if err := a.ensureItems(false); err != nil {
		return err
	}
	if err := a.ensureCurrent(*noSync, "1h"); err != nil {
		return err
	}
	item, err := a.resolveItem(input)
	if err != nil {
		return err
	}
	opp, err := a.oneOpportunity(item.ID, *taxRate, *taxCap)
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(opp)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FIELD\tVALUE")
	fmt.Fprintf(tw, "Item\t%s (%d)\n", opp.Name, opp.ID)
	fmt.Fprintf(tw, "Members\t%t\n", opp.Members)
	fmt.Fprintf(tw, "Low / High\t%s / %s gp\n", gp(opp.Low), gp(opp.High))
	fmt.Fprintf(tw, "Tax\t%s gp\n", gp(opp.Tax))
	fmt.Fprintf(tw, "Net margin\t%s gp\n", gp(opp.NetMargin))
	fmt.Fprintf(tw, "ROI\t%.2f%%\n", opp.ROI*100)
	fmt.Fprintf(tw, "1h volume\t%s\n", gp(opp.Volume))
	fmt.Fprintf(tw, "Buy limit\t%s\n", emptyZero(opp.BuyLimit))
	fmt.Fprintf(tw, "Limit profit\t%s gp\n", gp(opp.LimitProfit))
	fmt.Fprintf(tw, "High/low age\t%s / %s\n", durationSeconds(opp.HighAgeSeconds), durationSeconds(opp.LowAgeSeconds))
	return tw.Flush()
}

func (a *app) oneOpportunity(id int64, taxRate float64, taxCap int64) (opportunity, error) {
	rows, err := a.loadCandidates("1h")
	if err != nil {
		return opportunity{}, err
	}
	for _, row := range rows {
		if row.ID == id {
			return computeOpportunity(row, 1, taxRate, taxCap, time.Now().Unix()), nil
		}
	}
	return opportunity{}, fmt.Errorf("no current price/volume row for item id %d", id)
}

type candidateRow struct {
	ID           int64
	Name         string
	Members      bool
	BuyLimit     sql.NullInt64
	High         sql.NullInt64
	HighTime     sql.NullInt64
	Low          sql.NullInt64
	LowTime      sql.NullInt64
	HighVolume   int64
	LowVolume    int64
	Timestamp    int64
	LocalAvg     sql.NullFloat64
	LocalSamples int64
}

type opportunityFilters struct {
	Interval       string
	Limit          int
	MinVolume      int64
	MinMargin      int64
	MinROI         float64
	AboveAverage   bool
	VolumeBaseline string
	Members        string
	SortBy         string
	MaxAge         time.Duration
	MaxSpreadPct   float64
	TaxRate        float64
	TaxCap         int64
	NoSync         bool
}

func (a *app) loadCandidates(interval string) ([]candidateRow, error) {
	rows, err := a.db.Query(`
WITH latest_interval AS (
  SELECT max(timestamp) AS ts FROM interval_prices WHERE interval = ?
)
SELECT i.id, i.name, i.members, i.buy_limit,
       l.high, l.high_time, l.low, l.low_time,
       p.high_volume, p.low_volume, p.timestamp,
       (
         SELECT avg(ip.high_volume + ip.low_volume)
         FROM interval_prices ip
         WHERE ip.item_id = i.id AND ip.interval = ? AND ip.timestamp < p.timestamp
       ) AS local_avg,
       (
         SELECT count(*)
         FROM interval_prices ip
         WHERE ip.item_id = i.id AND ip.interval = ? AND ip.timestamp < p.timestamp
       ) AS local_samples
FROM items i
JOIN latest_prices l ON l.item_id = i.id
JOIN interval_prices p ON p.item_id = i.id AND p.interval = ?
JOIN latest_interval li ON li.ts = p.timestamp`, interval, interval, interval, interval)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []candidateRow
	for rows.Next() {
		var row candidateRow
		var members int
		if err := rows.Scan(&row.ID, &row.Name, &members, &row.BuyLimit, &row.High, &row.HighTime, &row.Low, &row.LowTime, &row.HighVolume, &row.LowVolume, &row.Timestamp, &row.LocalAvg, &row.LocalSamples); err != nil {
			return nil, err
		}
		row.Members = members != 0
		out = append(out, row)
	}
	return out, rows.Err()
}

func (a *app) collectOpportunities(f opportunityFilters) ([]opportunity, error) {
	if f.Interval == "" {
		f.Interval = "1h"
	}
	if f.Members == "" {
		f.Members = "any"
	}
	if f.VolumeBaseline == "" {
		f.VolumeBaseline = "global-average"
	}
	if f.SortBy == "" {
		f.SortBy = "score"
	}
	if f.TaxRate == 0 {
		f.TaxRate = 0.02
	}
	if f.TaxCap == 0 {
		f.TaxCap = 5_000_000
	}
	if f.Interval != "1h" && f.Interval != "5m" {
		return nil, errors.New("--interval must be 1h or 5m")
	}
	if err := a.ensureItems(false); err != nil {
		return nil, err
	}
	if err := a.ensureCurrent(f.NoSync, f.Interval); err != nil {
		return nil, err
	}
	rows, err := a.loadCandidates(f.Interval)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	baseVolumes := make([]int64, 0, len(rows))
	for _, row := range rows {
		if row.High.Valid && row.Low.Valid && row.High.Int64 > row.Low.Int64 {
			v := row.HighVolume + row.LowVolume
			if v > 0 {
				baseVolumes = append(baseVolumes, v)
			}
		}
	}
	globalAvg := meanInt64(baseVolumes)
	globalMedian := medianInt64(baseVolumes)

	opps := make([]opportunity, 0, len(rows))
	for _, row := range rows {
		if !row.High.Valid || !row.Low.Valid {
			continue
		}
		opp := computeOpportunity(row, 1, f.TaxRate, f.TaxCap, now)
		if opp.NetMargin < f.MinMargin || opp.Volume < f.MinVolume || opp.ROI < f.MinROI {
			continue
		}
		if f.MaxSpreadPct > 0 && opp.SpreadPct > f.MaxSpreadPct {
			continue
		}
		if f.MaxAge > 0 && time.Duration(max(opp.HighAgeSeconds, opp.LowAgeSeconds))*time.Second > f.MaxAge {
			continue
		}
		switch f.Members {
		case "any":
		case "members":
			if !opp.Members {
				continue
			}
		case "free":
			if opp.Members {
				continue
			}
		default:
			return nil, errors.New("--members must be any, members, or free")
		}

		switch f.VolumeBaseline {
		case "none":
			opp.BaselineVolume = 0
			opp.VolumeRatio = 0
		case "global", "global-average":
			opp.BaselineVolume = globalAvg
			if globalAvg > 0 {
				opp.VolumeRatio = float64(opp.Volume) / globalAvg
			}
		case "global-median":
			opp.BaselineVolume = globalMedian
			if globalMedian > 0 {
				opp.VolumeRatio = float64(opp.Volume) / globalMedian
			}
		case "local":
			if !row.LocalAvg.Valid || row.LocalSamples < 2 || row.LocalAvg.Float64 <= 0 {
				continue
			}
			opp.BaselineVolume = row.LocalAvg.Float64
			opp.VolumeRatio = float64(opp.Volume) / row.LocalAvg.Float64
		default:
			return nil, errors.New("--volume-baseline must be none, global-average, global-median, or local")
		}
		if f.AboveAverage && f.VolumeBaseline != "none" && opp.VolumeRatio < 1 {
			continue
		}
		opp.Score = opportunityScore(opp, f.MinVolume, f.MaxAge)
		opps = append(opps, opp)
	}
	sortOpportunities(opps, f.SortBy)
	if f.Limit > 0 && len(opps) > f.Limit {
		opps = opps[:f.Limit]
	}
	for i := range opps {
		opps[i].Rank = i + 1
	}
	return opps, nil
}

func (a *app) cmdOpportunities(args []string) error {
	fs := flag.NewFlagSet("opportunities", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", 25, "maximum rows")
	interval := fs.String("interval", "1h", "volume interval: 1h or 5m")
	minVolume := fs.Int64("min-volume", 100, "minimum interval volume")
	minMargin := fs.Int64("min-margin", 1, "minimum net margin in gp")
	minROI := fs.Float64("min-roi", 0.005, "minimum ROI as decimal")
	aboveAverage := fs.Bool("above-average", true, "require volume above selected baseline")
	volumeBaseline := fs.String("volume-baseline", "global-average", "none, global-average, global-median, local")
	members := fs.String("members", "any", "any, members, or free")
	sortBy := fs.String("sort", "score", "score, net-margin, roi, volume, volume-ratio, limit-profit")
	maxAge := fs.Duration("max-age", 2*time.Hour, "maximum high/low price age")
	maxSpreadPct := fs.Float64("max-spread-pct", 0.50, "reject extreme gross spread percentage; 0 disables")
	taxRate := fs.Float64("tax-rate", 0.02, "GE tax rate")
	taxCap := fs.Int64("tax-cap", 5_000_000, "GE tax cap per item")
	jsonOut := fs.Bool("json", false, "emit JSON")
	csvOut := fs.Bool("csv", false, "emit CSV")
	noSync := fs.Bool("no-sync", false, "do not refresh latest/interval cache")
	_, err := parseCommandFlags(fs, args, map[string]bool{
		"limit": true, "interval": true, "min-volume": true, "min-margin": true,
		"min-roi": true, "volume-baseline": true, "members": true, "sort": true,
		"max-age": true, "max-spread-pct": true, "tax-rate": true, "tax-cap": true,
	})
	if err != nil {
		return err
	}
	opps, err := a.collectOpportunities(opportunityFilters{
		Interval:       *interval,
		Limit:          *limit,
		MinVolume:      *minVolume,
		MinMargin:      *minMargin,
		MinROI:         *minROI,
		AboveAverage:   *aboveAverage,
		VolumeBaseline: *volumeBaseline,
		Members:        *members,
		SortBy:         *sortBy,
		MaxAge:         *maxAge,
		MaxSpreadPct:   *maxSpreadPct,
		TaxRate:        *taxRate,
		TaxCap:         *taxCap,
		NoSync:         *noSync,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(opps)
	}
	if *csvOut {
		return writeOpportunitiesCSV(os.Stdout, opps)
	}
	return writeOpportunitiesTable(os.Stdout, opps)
}

func computeOpportunity(row candidateRow, baseline float64, taxRate float64, taxCap int64, now int64) opportunity {
	high := row.High.Int64
	low := row.Low.Int64
	volume := row.HighVolume + row.LowVolume
	tax := geTax(high, taxRate, taxCap)
	net := high - low - tax
	gross := high - low
	limit := int64(0)
	if row.BuyLimit.Valid {
		limit = row.BuyLimit.Int64
	}
	highAge := int64(0)
	if row.HighTime.Valid {
		highAge = max(0, now-row.HighTime.Int64)
	}
	lowAge := int64(0)
	if row.LowTime.Valid {
		lowAge = max(0, now-row.LowTime.Int64)
	}
	roi := 0.0
	spreadPct := 0.0
	if low > 0 {
		roi = float64(net) / float64(low)
		spreadPct = float64(gross) / float64(low)
	}
	opp := opportunity{
		ID:             row.ID,
		Name:           row.Name,
		Members:        row.Members,
		Low:            low,
		High:           high,
		Tax:            tax,
		GrossMargin:    gross,
		NetMargin:      net,
		ROI:            roi,
		SpreadPct:      spreadPct,
		Volume:         volume,
		BaselineVolume: baseline,
		BuyLimit:       limit,
		HighAgeSeconds: highAge,
		LowAgeSeconds:  lowAge,
	}
	if baseline > 0 {
		opp.VolumeRatio = float64(volume) / baseline
	}
	if limit > 0 {
		opp.LimitProfit = net * limit
	}
	return opp
}

func geTax(sellPrice int64, taxRate float64, taxCap int64) int64 {
	if sellPrice <= 0 || taxRate <= 0 {
		return 0
	}
	tax := int64(math.Floor(float64(sellPrice) * taxRate))
	if taxCap > 0 && tax > taxCap {
		return taxCap
	}
	return tax
}

func opportunityScore(opp opportunity, minVolume int64, maxAge time.Duration) float64 {
	if opp.NetMargin <= 0 || opp.Low <= 0 {
		return 0
	}
	volumeRatio := opp.VolumeRatio
	if volumeRatio <= 0 {
		volumeRatio = 1
	}
	volBoost := math.Min(volumeRatio, 5)
	liquidity := math.Min(1, float64(opp.Volume)/float64(max(1, minVolume)))
	freshAge := time.Duration(max(opp.HighAgeSeconds, opp.LowAgeSeconds)) * time.Second
	freshness := 1.0
	if maxAge > 0 {
		freshness = math.Max(0, 1-(float64(freshAge)/float64(maxAge)))
	}
	return float64(opp.NetMargin) * math.Log10(float64(opp.Volume)+10) * math.Max(opp.ROI, 0.0001) * volBoost * liquidity * freshness
}

func sortOpportunities(opps []opportunity, sortBy string) {
	sort.Slice(opps, func(i, j int) bool {
		a, b := opps[i], opps[j]
		switch sortBy {
		case "score":
			return a.Score > b.Score
		case "net-margin":
			return a.NetMargin > b.NetMargin
		case "roi":
			return a.ROI > b.ROI
		case "volume":
			return a.Volume > b.Volume
		case "volume-ratio":
			return a.VolumeRatio > b.VolumeRatio
		case "limit-profit":
			return a.LimitProfit > b.LimitProfit
		default:
			return a.Score > b.Score
		}
	})
}

func writeOpportunitiesTable(w io.Writer, opps []opportunity) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tITEM\tLOW\tHIGH\tTAX\tNET\tROI\tVOL\tBASE\tV/B\tLIMIT\tLIMIT PROFIT\tAGE\tSCORE")
	for _, opp := range opps {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%.2f%%\t%s\t%s\t%.2fx\t%s\t%s\t%s\t%.1f\n",
			opp.Rank, opp.Name, gp(opp.Low), gp(opp.High), gp(opp.Tax), gp(opp.NetMargin),
			opp.ROI*100, gp(opp.Volume), baseline(opp.BaselineVolume), opp.VolumeRatio,
			emptyZero(opp.BuyLimit), gp(opp.LimitProfit), durationSeconds(max(opp.HighAgeSeconds, opp.LowAgeSeconds)), opp.Score)
	}
	return tw.Flush()
}

func writeOpportunitiesCSV(w io.Writer, opps []opportunity) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()
	if err := cw.Write([]string{"rank", "id", "name", "members", "low", "high", "tax", "net_margin", "roi", "volume", "baseline_volume", "volume_ratio", "buy_limit", "limit_profit", "high_age_seconds", "low_age_seconds", "score"}); err != nil {
		return err
	}
	for _, opp := range opps {
		if err := cw.Write([]string{
			strconv.Itoa(opp.Rank),
			strconv.FormatInt(opp.ID, 10),
			opp.Name,
			strconv.FormatBool(opp.Members),
			strconv.FormatInt(opp.Low, 10),
			strconv.FormatInt(opp.High, 10),
			strconv.FormatInt(opp.Tax, 10),
			strconv.FormatInt(opp.NetMargin, 10),
			fmt.Sprintf("%.8f", opp.ROI),
			strconv.FormatInt(opp.Volume, 10),
			fmt.Sprintf("%.2f", opp.BaselineVolume),
			fmt.Sprintf("%.4f", opp.VolumeRatio),
			strconv.FormatInt(opp.BuyLimit, 10),
			strconv.FormatInt(opp.LimitProfit, 10),
			strconv.FormatInt(opp.HighAgeSeconds, 10),
			strconv.FormatInt(opp.LowAgeSeconds, 10),
			fmt.Sprintf("%.4f", opp.Score),
		}); err != nil {
			return err
		}
	}
	return cw.Error()
}

func (a *app) cmdAllocate(args []string) error {
	fs := flag.NewFlagSet("allocate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cashText := fs.String("cash", "", "cash budget, e.g. 20m or 500k")
	limit := fs.Int("limit", 10, "maximum suggested rows")
	minVolume := fs.Int64("min-volume", 500, "minimum interval volume")
	minMargin := fs.Int64("min-margin", 1, "minimum net margin in gp")
	minROI := fs.Float64("min-roi", 0.005, "minimum ROI as decimal")
	perItemCapText := fs.String("per-item-cap", "", "maximum cash per item")
	interval := fs.String("interval", "1h", "volume interval: 1h or 5m")
	noSync := fs.Bool("no-sync", false, "do not refresh latest/interval cache")
	jsonOut := fs.Bool("json", false, "emit JSON")
	_, err := parseCommandFlags(fs, args, map[string]bool{"cash": true, "limit": true, "min-volume": true, "min-margin": true, "min-roi": true, "per-item-cap": true, "interval": true})
	if err != nil {
		return err
	}
	cash, err := parseGP(*cashText)
	if err != nil || cash <= 0 {
		return errors.New("--cash is required, e.g. --cash 20m")
	}
	perItemCap := int64(0)
	if strings.TrimSpace(*perItemCapText) != "" {
		perItemCap, err = parseGP(*perItemCapText)
		if err != nil {
			return fmt.Errorf("--per-item-cap: %w", err)
		}
	}
	opps, err := a.collectOpportunities(opportunityFilters{
		Interval:       *interval,
		MinVolume:      *minVolume,
		MinMargin:      *minMargin,
		MinROI:         *minROI,
		AboveAverage:   true,
		VolumeBaseline: "global-average",
		Members:        "any",
		SortBy:         "score",
		MaxAge:         2 * time.Hour,
		MaxSpreadPct:   0.50,
		NoSync:         *noSync,
	})
	if err != nil {
		return err
	}
	remaining := cash
	var rows []allocationRow
	for _, opp := range opps {
		if remaining <= 0 || opp.Low <= 0 || opp.NetMargin <= 0 {
			break
		}
		spendable := remaining
		if perItemCap > 0 && spendable > perItemCap {
			spendable = perItemCap
		}
		units := spendable / opp.Low
		if opp.BuyLimit > 0 && units > opp.BuyLimit {
			units = opp.BuyLimit
		}
		if units <= 0 {
			continue
		}
		cost := units * opp.Low
		edge := units * opp.NetMargin
		remaining -= cost
		rows = append(rows, allocationRow{
			Rank:          len(rows) + 1,
			ID:            opp.ID,
			Name:          opp.Name,
			Units:         units,
			Cost:          cost,
			ExpectedEdge:  edge,
			NetMargin:     opp.NetMargin,
			ROI:           opp.ROI,
			Volume:        opp.Volume,
			BuyLimit:      opp.BuyLimit,
			RemainingCash: remaining,
		})
		if *limit > 0 && len(rows) >= *limit {
			break
		}
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(rows)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tITEM\tUNITS\tCOST\tEDGE\tNET/EA\tROI\tVOL\tLIMIT\tCASH LEFT")
	for _, row := range rows {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%.2f%%\t%s\t%s\t%s\n",
			row.Rank, row.Name, gp(row.Units), gp(row.Cost), gp(row.ExpectedEdge),
			gp(row.NetMargin), row.ROI*100, gp(row.Volume), emptyZero(row.BuyLimit), gp(row.RemainingCash))
	}
	if len(rows) == 0 {
		fmt.Fprintln(tw, "No allocation candidates passed the filters.")
	}
	return tw.Flush()
}

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

func (a *app) cmdWatch(args []string) error {
	if len(args) == 0 {
		return errors.New("watch requires a subcommand: add, list, remove, check")
	}
	switch args[0] {
	case "add":
		return a.cmdWatchAdd(args[1:])
	case "list", "ls":
		return a.cmdWatchList(args[1:])
	case "remove", "rm", "delete":
		return a.cmdWatchRemove(args[1:])
	case "check":
		return a.cmdWatchCheck(args[1:])
	default:
		return fmt.Errorf("unknown watch subcommand %q", args[0])
	}
}

func (a *app) cmdWatchAdd(args []string) error {
	fs := flag.NewFlagSet("watch add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	belowText := fs.String("below", "", "trigger when low price is at or below this gp value")
	aboveText := fs.String("above", "", "trigger when high price is at or above this gp value")
	marginText := fs.String("min-margin", "", "trigger when net margin is at least this gp value")
	minROI := fs.Float64("min-roi", 0, "trigger when ROI is at least this decimal value")
	minVolume := fs.Int64("min-volume", 0, "trigger when 1h volume is at least this value")
	cooldown := fs.Duration("cooldown", time.Hour, "minimum time between repeated triggers")
	note := fs.String("note", "", "optional note")
	positionals, err := parseCommandFlags(fs, args, map[string]bool{"below": true, "above": true, "min-margin": true, "min-roi": true, "min-volume": true, "cooldown": true, "note": true})
	if err != nil {
		return err
	}
	input := strings.TrimSpace(strings.Join(positionals, " "))
	if input == "" {
		return errors.New("watch add requires an item")
	}
	if err := a.ensureItems(false); err != nil {
		return err
	}
	item, err := a.resolveItem(input)
	if err != nil {
		return err
	}
	below, err := parseOptionalGP(*belowText)
	if err != nil {
		return fmt.Errorf("--below: %w", err)
	}
	above, err := parseOptionalGP(*aboveText)
	if err != nil {
		return fmt.Errorf("--above: %w", err)
	}
	margin, err := parseOptionalGP(*marginText)
	if err != nil {
		return fmt.Errorf("--min-margin: %w", err)
	}
	if !below.Valid && !above.Valid && !margin.Valid && *minROI <= 0 && *minVolume <= 0 {
		return errors.New("watch add requires at least one condition")
	}
	now := time.Now().Unix()
	var roi any
	if *minROI > 0 {
		roi = *minROI
	}
	var volume any
	if *minVolume > 0 {
		volume = *minVolume
	}
	res, err := a.db.Exec(`
INSERT INTO watchlist (item_id, below, above, min_net_margin, min_roi, min_volume, cooldown_seconds, enabled, note, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		item.ID, nullIntArg(below), nullIntArg(above), nullIntArg(margin), roi, volume, int64(cooldown.Seconds()), *note, now, now)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	fmt.Printf("added watch rule %d for %s (%d)\n", id, item.Name, item.ID)
	return nil
}

func (a *app) cmdWatchList(args []string) error {
	fs := flag.NewFlagSet("watch list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit JSON")
	_, err := parseCommandFlags(fs, args, nil)
	if err != nil {
		return err
	}
	rules, err := a.loadWatchRules(false)
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(rules)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tITEM\tBELOW\tABOVE\tMIN NET\tMIN ROI\tMIN VOL\tCOOLDOWN\tENABLED\tNOTE")
	for _, r := range rules {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%t\t%s\n",
			r.ID, r.Name, nullGP(r.Below), nullGP(r.Above), nullGP(r.MinNetMargin),
			nullPct(r.MinROI), nullGP(r.MinVolume), durationSeconds(r.CooldownSeconds), r.Enabled, r.Note)
	}
	return tw.Flush()
}

func (a *app) cmdWatchRemove(args []string) error {
	if len(args) == 0 {
		return errors.New("watch remove requires a rule id or item")
	}
	input := strings.TrimSpace(strings.Join(args, " "))
	if id, err := strconv.ParseInt(input, 10, 64); err == nil {
		res, err := a.db.Exec(`DELETE FROM watchlist WHERE id = ?`, id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		fmt.Printf("removed %d rule(s)\n", n)
		return nil
	}
	if err := a.ensureItems(false); err != nil {
		return err
	}
	item, err := a.resolveItem(input)
	if err != nil {
		return err
	}
	res, err := a.db.Exec(`DELETE FROM watchlist WHERE item_id = ?`, item.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	fmt.Printf("removed %d rule(s) for %s\n", n, item.Name)
	return nil
}

func (a *app) cmdWatchCheck(args []string) error {
	fs := flag.NewFlagSet("watch check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	noSync := fs.Bool("no-sync", false, "do not refresh latest/1h cache")
	jsonOut := fs.Bool("json", false, "emit JSON")
	_, err := parseCommandFlags(fs, args, nil)
	if err != nil {
		return err
	}
	if err := a.ensureItems(false); err != nil {
		return err
	}
	if err := a.ensureCurrent(*noSync, "1h"); err != nil {
		return err
	}
	rules, err := a.loadWatchRules(true)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	var hits []alertHit
	for _, rule := range rules {
		if rule.LastTriggeredAt.Valid && now-rule.LastTriggeredAt.Int64 < rule.CooldownSeconds {
			continue
		}
		opp, err := a.oneOpportunity(rule.ItemID, 0.02, 5_000_000)
		if err != nil {
			continue
		}
		reasons := watchReasons(rule, opp)
		if len(reasons) == 0 {
			continue
		}
		reason := strings.Join(reasons, "; ")
		if _, err := a.db.Exec(`INSERT INTO alert_events (rule_id, item_id, reason, low, high, net_margin, roi, volume, triggered_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rule.ID, rule.ItemID, reason, opp.Low, opp.High, opp.NetMargin, opp.ROI, opp.Volume, now); err != nil {
			return err
		}
		if _, err := a.db.Exec(`UPDATE watchlist SET last_triggered_at = ?, updated_at = ? WHERE id = ?`, now, now, rule.ID); err != nil {
			return err
		}
		hits = append(hits, alertHit{
			RuleID:          rule.ID,
			ItemID:          rule.ItemID,
			Name:            rule.Name,
			Reason:          reason,
			Low:             opp.Low,
			High:            opp.High,
			NetMargin:       opp.NetMargin,
			ROI:             opp.ROI,
			Volume:          opp.Volume,
			LastTriggeredAt: now,
		})
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(hits)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RULE\tITEM\tREASON\tLOW\tHIGH\tNET\tROI\tVOL")
	for _, hit := range hits {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%.2f%%\t%s\n", hit.RuleID, hit.Name, hit.Reason, gp(hit.Low), gp(hit.High), gp(hit.NetMargin), hit.ROI*100, gp(hit.Volume))
	}
	if len(hits) == 0 {
		fmt.Fprintln(tw, "No watch rules triggered.")
	}
	return tw.Flush()
}

func (a *app) loadWatchRules(enabledOnly bool) ([]watchRule, error) {
	query := `
SELECT w.id, w.item_id, i.name, w.below, w.above, w.min_net_margin, w.min_roi, w.min_volume,
       w.cooldown_seconds, w.enabled, coalesce(w.note, ''), w.last_triggered_at, w.created_at, w.updated_at
FROM watchlist w
JOIN items i ON i.id = w.item_id`
	if enabledOnly {
		query += ` WHERE w.enabled = 1`
	}
	query += ` ORDER BY w.id`
	rows, err := a.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []watchRule
	for rows.Next() {
		var r watchRule
		var enabled int
		if err := rows.Scan(&r.ID, &r.ItemID, &r.Name, &r.Below, &r.Above, &r.MinNetMargin, &r.MinROI, &r.MinVolume, &r.CooldownSeconds, &enabled, &r.Note, &r.LastTriggeredAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func watchReasons(rule watchRule, opp opportunity) []string {
	var reasons []string
	if rule.Below.Valid && opp.Low <= rule.Below.Int64 {
		reasons = append(reasons, fmt.Sprintf("low <= %s", gp(rule.Below.Int64)))
	}
	if rule.Above.Valid && opp.High >= rule.Above.Int64 {
		reasons = append(reasons, fmt.Sprintf("high >= %s", gp(rule.Above.Int64)))
	}
	if rule.MinNetMargin.Valid && opp.NetMargin >= rule.MinNetMargin.Int64 {
		reasons = append(reasons, fmt.Sprintf("net >= %s", gp(rule.MinNetMargin.Int64)))
	}
	if rule.MinROI.Valid && opp.ROI >= rule.MinROI.Float64 {
		reasons = append(reasons, fmt.Sprintf("roi >= %.2f%%", rule.MinROI.Float64*100))
	}
	if rule.MinVolume.Valid && opp.Volume >= rule.MinVolume.Int64 {
		reasons = append(reasons, fmt.Sprintf("volume >= %s", gp(rule.MinVolume.Int64)))
	}
	return reasons
}

func (a *app) cmdBacktest(args []string) error {
	fs := flag.NewFlagSet("backtest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	step := fs.String("step", "1h", "5m, 1h, 6h, or 24h")
	cashText := fs.String("cash", "10m", "cash budget per window")
	minMargin := fs.Int64("min-margin", 1, "minimum net margin in gp")
	minROI := fs.Float64("min-roi", 0.005, "minimum ROI as decimal")
	minVolume := fs.Int64("min-volume", 1, "minimum total volume")
	taxRate := fs.Float64("tax-rate", 0.02, "GE tax rate")
	taxCap := fs.Int64("tax-cap", 5_000_000, "GE tax cap per item")
	jsonOut := fs.Bool("json", false, "emit JSON")
	positionals, err := parseCommandFlags(fs, args, map[string]bool{"step": true, "cash": true, "min-margin": true, "min-roi": true, "min-volume": true, "tax-rate": true, "tax-cap": true})
	if err != nil {
		return err
	}
	input := strings.TrimSpace(strings.Join(positionals, " "))
	if input == "" {
		return errors.New("backtest requires an item")
	}
	cash, err := parseGP(*cashText)
	if err != nil {
		return fmt.Errorf("--cash: %w", err)
	}
	if err := a.ensureItems(false); err != nil {
		return err
	}
	item, err := a.resolveItem(input)
	if err != nil {
		return err
	}
	resp, err := a.client.timeseries(context.Background(), item.ID, *step)
	if err != nil {
		return err
	}
	summary := backtestSpread(item, resp.Data, *step, cash, *minMargin, *minROI, *minVolume, *taxRate, *taxCap)
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(summary)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FIELD\tVALUE")
	fmt.Fprintf(tw, "Item\t%s (%d)\n", summary.Name, summary.ItemID)
	fmt.Fprintf(tw, "Step\t%s\n", summary.Step)
	fmt.Fprintf(tw, "Windows\t%d\n", summary.Windows)
	fmt.Fprintf(tw, "Signals\t%d\n", summary.Signals)
	fmt.Fprintf(tw, "Theoretical edge\t%s gp\n", gp(summary.TotalEdge))
	fmt.Fprintf(tw, "Average edge/signal\t%s gp\n", gp(int64(math.Round(summary.AverageEdge))))
	fmt.Fprintf(tw, "Win rate\t%.2f%%\n", summary.WinRate*100)
	fmt.Fprintf(tw, "Best/worst window\t%s / %s gp\n", gp(summary.MaxWindowEdge), gp(summary.MinWindowEdge))
	fmt.Fprintf(tw, "Assumption\t%s\n", summary.Assumption)
	return tw.Flush()
}

func backtestSpread(item itemRecord, points []timeseriesPoint, step string, cash, minMargin int64, minROI float64, minVolume int64, taxRate float64, taxCap int64) backtestSummary {
	summary := backtestSummary{
		ItemID:     item.ID,
		Name:       item.Name,
		Step:       step,
		Windows:    len(points),
		Assumption: "same-window theoretical spread capture using avgLow buy and avgHigh sell; not live execution",
	}
	first := true
	for _, p := range points {
		if p.AvgHighPrice == nil || p.AvgLowPrice == nil || *p.AvgLowPrice <= 0 {
			continue
		}
		volume := p.HighVolume + p.LowVolume
		if volume < minVolume {
			continue
		}
		net := *p.AvgHighPrice - *p.AvgLowPrice - geTax(*p.AvgHighPrice, taxRate, taxCap)
		roi := float64(net) / float64(*p.AvgLowPrice)
		if net < minMargin || roi < minROI {
			continue
		}
		units := cash / *p.AvgLowPrice
		if item.BuyLimit.Valid && units > item.BuyLimit.Int64 {
			units = item.BuyLimit.Int64
		}
		if units <= 0 {
			continue
		}
		edge := units * net
		summary.Signals++
		summary.TotalEdge += edge
		if edge > 0 {
			summary.WinRate++
		}
		if first || edge > summary.MaxWindowEdge {
			summary.MaxWindowEdge = edge
		}
		if first || edge < summary.MinWindowEdge {
			summary.MinWindowEdge = edge
		}
		first = false
	}
	if summary.Signals > 0 {
		summary.AverageEdge = float64(summary.TotalEdge) / float64(summary.Signals)
		summary.WinRate = summary.WinRate / float64(summary.Signals)
	}
	return summary
}

func (a *app) cmdPatterns(args []string) error {
	fs := flag.NewFlagSet("patterns", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	strategy := fs.String("strategy", "bronze-knife", "strategy template: bronze-knife")
	like := fs.String("like", "Bronze knife", "reference item or alpha label")
	cashText := fs.String("cash", "40m", "portfolio cash lens, e.g. 40m")
	step := fs.String("step", "5m", "timeseries step: 5m, 1h, 6h, or 24h")
	days := fs.Int("days", 14, "lookback window in days")
	limit := fs.Int("limit", 20, "maximum pattern hits")
	candidateLimit := fs.Int("candidate-limit", 500, "maximum candidate items to scan")
	minBuyLimit := fs.Int64("min-buy-limit", 5000, "minimum GE buy limit")
	maxValueText := fs.String("max-value", "1000", "maximum item value metadata threshold")
	minLowText := fs.String("min-low", "10", "minimum dip low price")
	maxLowText := fs.String("max-low", "80", "maximum dip low price")
	minHighText := fs.String("min-high", "60", "minimum rebound high price")
	maxHighText := fs.String("max-high", "250", "maximum rebound high price")
	minLowVolume := fs.Int64("min-low-volume", 100, "minimum dip-side volume")
	minHighVolume := fs.Int64("min-high-volume", 300, "minimum rebound-side volume")
	minRatio := fs.Float64("min-ratio", 1.75, "minimum high/low rebound ratio")
	maxRatio := fs.Float64("max-ratio", 8, "maximum high/low rebound ratio")
	minNetText := fs.String("min-net", "20", "minimum tax-adjusted gp margin")
	maxRebound := fs.Duration("max-rebound", 72*time.Hour, "maximum time from dip to rebound")
	requestDelay := fs.Duration("request-delay", 25*time.Millisecond, "delay between item timeseries requests")
	taxRate := fs.Float64("tax-rate", 0.02, "GE tax rate")
	taxCap := fs.Int64("tax-cap", 5_000_000, "GE tax cap per item")
	jsonOut := fs.Bool("json", false, "emit JSON report")
	noSync := fs.Bool("no-sync", false, "do not refresh mapping/latest cache")
	_, err := parseCommandFlags(fs, args, map[string]bool{
		"strategy": true, "like": true, "cash": true, "step": true, "days": true,
		"limit": true, "candidate-limit": true, "min-buy-limit": true, "max-value": true,
		"min-low": true, "max-low": true, "min-high": true, "max-high": true,
		"min-low-volume": true, "min-high-volume": true, "min-ratio": true,
		"max-ratio": true, "min-net": true, "max-rebound": true, "request-delay": true,
		"tax-rate": true, "tax-cap": true,
	})
	if err != nil {
		return err
	}
	if *strategy != "bronze-knife" && *strategy != "shock-reversion" {
		return errors.New("--strategy must be bronze-knife or shock-reversion")
	}
	if *step != "5m" && *step != "1h" && *step != "6h" && *step != "24h" {
		return errors.New("--step must be 5m, 1h, 6h, or 24h")
	}
	if *days <= 0 {
		return errors.New("--days must be > 0")
	}
	cash, err := parseGP(*cashText)
	if err != nil {
		return fmt.Errorf("--cash: %w", err)
	}
	maxValue, err := parseGP(*maxValueText)
	if err != nil {
		return fmt.Errorf("--max-value: %w", err)
	}
	minLow, err := parseGP(*minLowText)
	if err != nil {
		return fmt.Errorf("--min-low: %w", err)
	}
	maxLow, err := parseGP(*maxLowText)
	if err != nil {
		return fmt.Errorf("--max-low: %w", err)
	}
	minHigh, err := parseGP(*minHighText)
	if err != nil {
		return fmt.Errorf("--min-high: %w", err)
	}
	maxHigh, err := parseGP(*maxHighText)
	if err != nil {
		return fmt.Errorf("--max-high: %w", err)
	}
	minNet, err := parseGP(*minNetText)
	if err != nil {
		return fmt.Errorf("--min-net: %w", err)
	}
	if minLow <= 0 || maxLow < minLow || maxHigh < minHigh || minHigh <= 0 {
		return errors.New("price bands are invalid")
	}
	if *minRatio <= 1 || *maxRatio < *minRatio {
		return errors.New("ratio bounds are invalid")
	}
	if *maxRebound <= 0 {
		return errors.New("--max-rebound must be positive")
	}
	if err := a.ensureItems(*noSync); err != nil {
		return err
	}
	if err := a.ensureCurrent(*noSync, "1h"); err != nil {
		return err
	}
	candidates, err := a.loadPatternCandidates(*minBuyLimit, maxValue, *candidateLimit)
	if err != nil {
		return err
	}
	opts := patternScanOptions{
		Strategy:      *strategy,
		Cash:          cash,
		Step:          *step,
		Cutoff:        time.Now().AddDate(0, 0, -*days).Unix(),
		MaxRebound:    *maxRebound,
		MinLow:        minLow,
		MaxLow:        maxLow,
		MinHigh:       minHigh,
		MaxHigh:       maxHigh,
		MinLowVolume:  *minLowVolume,
		MinHighVolume: *minHighVolume,
		MinRatio:      *minRatio,
		MaxRatio:      *maxRatio,
		MinNetMargin:  minNet,
		TaxRate:       *taxRate,
		TaxCap:        *taxCap,
	}
	report := patternReport{
		Strategy:      *strategy,
		Like:          strings.TrimSpace(*like),
		Cash:          cash,
		Step:          *step,
		Days:          *days,
		GeneratedAt:   time.Now().Format(time.RFC3339),
		CandidateRows: len(candidates),
		Hits:          []patternHit{},
		Notes: []string{
			"Research-only market dislocation scan; does not automate accounts, clients, offers, or gameplay.",
			"Public API data is aggregated, so hits are evidence of market patterns rather than guaranteed fillability.",
		},
	}
	ctx := context.Background()
	for i, candidate := range candidates {
		resp, err := a.client.timeseries(ctx, candidate.Item.ID, *step)
		if err != nil {
			report.Errors++
			continue
		}
		report.Scanned++
		if hit, ok := analyzePatternCandidate(candidate, resp.Data, opts); ok {
			report.Hits = append(report.Hits, hit)
		}
		if *requestDelay > 0 && i < len(candidates)-1 {
			time.Sleep(*requestDelay)
		}
	}
	sort.Slice(report.Hits, func(i, j int) bool {
		return report.Hits[i].Score > report.Hits[j].Score
	})
	if *limit > 0 && len(report.Hits) > *limit {
		report.Hits = report.Hits[:*limit]
	}
	for i := range report.Hits {
		report.Hits[i].Rank = i + 1
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	return writePatternReport(os.Stdout, report)
}

func (a *app) loadPatternCandidates(minBuyLimit, maxValue int64, limit int) ([]patternCandidate, error) {
	if limit <= 0 {
		limit = -1
	}
	rows, err := a.db.Query(`
SELECT i.id, i.name, i.members, i.buy_limit, i.value, i.high_alch, i.low_alch, i.examine,
       l.low, l.high
FROM items i
LEFT JOIN latest_prices l ON l.item_id = i.id
WHERE coalesce(i.buy_limit, 0) >= ?
  AND (? <= 0 OR i.value IS NULL OR i.value <= ?)
ORDER BY coalesce(i.buy_limit, 0) DESC, lower(i.name)
LIMIT ?`, minBuyLimit, maxValue, maxValue, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []patternCandidate
	for rows.Next() {
		var candidate patternCandidate
		var members int
		var currentLow, currentHigh sql.NullInt64
		if err := rows.Scan(&candidate.Item.ID, &candidate.Item.Name, &members, &candidate.Item.BuyLimit, &candidate.Item.Value, &candidate.Item.HighAlch, &candidate.Item.LowAlch, &candidate.Item.Examine, &currentLow, &currentHigh); err != nil {
			return nil, err
		}
		candidate.Item.Members = members != 0
		if currentLow.Valid {
			candidate.CurrentLow = int64Ptr(currentLow.Int64)
		}
		if currentHigh.Valid {
			candidate.CurrentHigh = int64Ptr(currentHigh.Int64)
		}
		out = append(out, candidate)
	}
	return out, rows.Err()
}

func analyzePatternCandidate(candidate patternCandidate, points []timeseriesPoint, opts patternScanOptions) (patternHit, bool) {
	if len(points) == 0 {
		return patternHit{}, false
	}
	points = append([]timeseriesPoint(nil), points...)
	sort.Slice(points, func(i, j int) bool { return points[i].Timestamp < points[j].Timestamp })
	maxReboundSeconds := int64(opts.MaxRebound.Seconds())
	var best patternHit
	found := false
	for lowIdx, lowPoint := range points {
		if lowPoint.Timestamp < opts.Cutoff || lowPoint.AvgLowPrice == nil {
			continue
		}
		low := *lowPoint.AvgLowPrice
		if low < opts.MinLow || low > opts.MaxLow || lowPoint.LowVolume < opts.MinLowVolume {
			continue
		}
		for highIdx := lowIdx; highIdx < len(points); highIdx++ {
			highPoint := points[highIdx]
			if highPoint.Timestamp < lowPoint.Timestamp {
				continue
			}
			if highPoint.Timestamp-lowPoint.Timestamp > maxReboundSeconds {
				break
			}
			if highPoint.AvgHighPrice == nil {
				continue
			}
			high := *highPoint.AvgHighPrice
			if high < opts.MinHigh || high > opts.MaxHigh || highPoint.HighVolume < opts.MinHighVolume {
				continue
			}
			ratio := float64(high) / float64(low)
			if ratio < opts.MinRatio || ratio > opts.MaxRatio {
				continue
			}
			tax := geTax(high, opts.TaxRate, opts.TaxCap)
			net := high - low - tax
			if net < opts.MinNetMargin {
				continue
			}
			hit := buildPatternHit(candidate, lowPoint, highPoint, low, high, tax, net, ratio, opts)
			if !found || hit.Score > best.Score {
				best = hit
				found = true
			}
		}
	}
	return best, found
}

func buildPatternHit(candidate patternCandidate, lowPoint, highPoint timeseriesPoint, low, high, tax, net int64, ratio float64, opts patternScanOptions) patternHit {
	limit := int64(0)
	if candidate.Item.BuyLimit.Valid {
		limit = candidate.Item.BuyLimit.Int64
	}
	units := int64(0)
	if opts.Cash > 0 && low > 0 {
		units = opts.Cash / low
		if limit > 0 && units > limit {
			units = limit
		}
	}
	cost := units * low
	profit := units * net
	reboundSeconds := max[int64](0, highPoint.Timestamp-lowPoint.Timestamp)
	roi := float64(net) / float64(low)
	minEventVolume := min[int64](lowPoint.LowVolume, highPoint.HighVolume)
	limitForScore := limit
	if limitForScore <= 0 {
		limitForScore = 1000
	}
	speed := 1.0 / (1.0 + (float64(reboundSeconds) / math.Max(float64(opts.MaxRebound.Seconds()), 1)))
	score := float64(net) * math.Max(roi, 0.01) * math.Log1p(float64(minEventVolume)) * math.Log1p(float64(limitForScore)) * speed
	if profit > 0 {
		score += math.Sqrt(float64(profit))
	}
	lowTime := time.Unix(lowPoint.Timestamp, 0)
	highTime := time.Unix(highPoint.Timestamp, 0)
	setup := classifyPatternSetup(candidate, high, opts)
	return patternHit{
		ID:              candidate.Item.ID,
		Name:            candidate.Item.Name,
		Members:         candidate.Item.Members,
		BuyLimit:        limit,
		Low:             low,
		High:            high,
		Tax:             tax,
		NetMargin:       net,
		ROI:             roi,
		Ratio:           ratio,
		LowVolume:       lowPoint.LowVolume,
		HighVolume:      highPoint.HighVolume,
		LowTime:         lowPoint.Timestamp,
		HighTime:        highPoint.Timestamp,
		LowTimeISO:      lowTime.Format(time.RFC3339),
		HighTimeISO:     highTime.Format(time.RFC3339),
		ReboundSeconds:  reboundSeconds,
		PortfolioUnits:  units,
		PortfolioCost:   cost,
		PortfolioProfit: profit,
		CurrentLow:      candidate.CurrentLow,
		CurrentHigh:     candidate.CurrentHigh,
		Setup:           setup,
		Score:           score,
		Rationale: []string{
			fmt.Sprintf("dip %s gp to rebound %s gp after tax-adjusted %s gp/ea", gp(low), gp(high), gp(net)),
			fmt.Sprintf("dip volume %s and rebound volume %s", gp(lowPoint.LowVolume), gp(highPoint.HighVolume)),
			fmt.Sprintf("%s portfolio lens can test %s units for %s gp theoretical edge per limit cycle", gp(opts.Cash), gp(units), gp(profit)),
		},
	}
}

func classifyPatternSetup(candidate patternCandidate, targetHigh int64, opts patternScanOptions) string {
	if candidate.CurrentLow != nil && *candidate.CurrentLow <= opts.MaxLow {
		return "Fresh dump candidate"
	}
	if candidate.CurrentHigh != nil && *candidate.CurrentHigh >= int64(math.Round(float64(targetHigh)*0.9)) {
		return "Rebound/target area"
	}
	return "Historical analog"
}

func writePatternReport(w io.Writer, report patternReport) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Strategy\t%s\n", report.Strategy)
	if report.Like != "" {
		fmt.Fprintf(tw, "Reference\t%s\n", report.Like)
	}
	fmt.Fprintf(tw, "Cash lens\t%s gp\n", gp(report.Cash))
	fmt.Fprintf(tw, "Scanned\t%d/%d candidates (%d errors)\n\n", report.Scanned, report.CandidateRows, report.Errors)
	fmt.Fprintln(tw, "#\tITEM\tDIP -> REBOUND\tNET\tROI\tLOW VOL\tHIGH VOL\tLIMIT\t40M EDGE\tSETUP\tWHEN\tSCORE")
	for _, hit := range report.Hits {
		when := fmt.Sprintf("%s -> %s", time.Unix(hit.LowTime, 0).Format("01-02 15:04"), time.Unix(hit.HighTime, 0).Format("01-02 15:04"))
		fmt.Fprintf(tw, "%d\t%s\t%s -> %s\t%s\t%.2f%%\t%s\t%s\t%s\t%s\t%s\t%s\t%.1f\n",
			hit.Rank, hit.Name, gp(hit.Low), gp(hit.High), gp(hit.NetMargin), hit.ROI*100,
			gp(hit.LowVolume), gp(hit.HighVolume), emptyZero(hit.BuyLimit), gp(hit.PortfolioProfit),
			hit.Setup, when, hit.Score)
	}
	if len(report.Hits) == 0 {
		fmt.Fprintln(tw, "No pattern hits passed the filters.")
	}
	return tw.Flush()
}

func (a *app) cmdRangeBottom(args []string) error {
	fs := flag.NewFlagSet("range-bottom", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cashText := fs.String("cash", "40m", "portfolio cash lens, e.g. 40m")
	step := fs.String("step", "6h", "timeseries step: 1h, 6h, or 24h")
	days := fs.Int("days", 90, "lookback window in days")
	limit := fs.Int("limit", 25, "maximum rows")
	candidateLimit := fs.Int("candidate-limit", 500, "maximum candidate items to scan")
	minBuyLimit := fs.Int64("min-buy-limit", 5000, "minimum GE buy limit")
	maxValueText := fs.String("max-value", "0", "maximum item value metadata threshold; 0 disables")
	maxPercentile := fs.Float64("max-percentile", 0.25, "maximum current price percentile within item history")
	bottomPercentile := fs.Float64("bottom-percentile", 0.25, "percentile used as bottom-range band")
	minActiveBuckets := fs.Int("min-active-buckets", 20, "minimum buckets with non-zero volume")
	minMedianVolume := fs.Int64("min-median-volume", 250, "minimum median bucket volume")
	minP25Volume := fs.Int64("min-p25-volume", 1, "minimum p25 bucket volume")
	minCycles := fs.Int("min-cycles", 1, "minimum historical bottom-to-rebound cycles")
	reboundWindow := fs.Duration("rebound-window", 30*24*time.Hour, "maximum time from bottom visit to rebound")
	requestDelay := fs.Duration("request-delay", 25*time.Millisecond, "delay between item timeseries requests")
	taxRate := fs.Float64("tax-rate", 0.02, "GE tax rate")
	taxCap := fs.Int64("tax-cap", 5_000_000, "GE tax cap per item")
	jsonOut := fs.Bool("json", false, "emit JSON report")
	noSync := fs.Bool("no-sync", false, "do not refresh mapping/latest cache")
	_, err := parseCommandFlags(fs, args, map[string]bool{
		"cash": true, "step": true, "days": true, "limit": true, "candidate-limit": true,
		"min-buy-limit": true, "max-value": true, "max-percentile": true, "bottom-percentile": true,
		"min-active-buckets": true, "min-median-volume": true, "min-p25-volume": true,
		"min-cycles": true, "rebound-window": true, "request-delay": true,
		"tax-rate": true, "tax-cap": true,
	})
	if err != nil {
		return err
	}
	if *step != "1h" && *step != "6h" && *step != "24h" {
		return errors.New("--step must be 1h, 6h, or 24h")
	}
	if *days <= 0 {
		return errors.New("--days must be > 0")
	}
	if *maxPercentile <= 0 || *maxPercentile > 1 || *bottomPercentile <= 0 || *bottomPercentile > 1 {
		return errors.New("percentile values must be between 0 and 1")
	}
	cash, err := parseGP(*cashText)
	if err != nil {
		return fmt.Errorf("--cash: %w", err)
	}
	maxValue, err := parseGP(*maxValueText)
	if err != nil {
		return fmt.Errorf("--max-value: %w", err)
	}
	if err := a.ensureItems(*noSync); err != nil {
		return err
	}
	if err := a.ensureCurrent(*noSync, "1h"); err != nil {
		return err
	}
	candidates, err := a.loadPatternCandidates(*minBuyLimit, maxValue, *candidateLimit)
	if err != nil {
		return err
	}
	opts := rangeBottomOptions{
		Cash:             cash,
		Step:             *step,
		Cutoff:           time.Now().AddDate(0, 0, -*days).Unix(),
		MaxPercentile:    *maxPercentile,
		BottomPercentile: *bottomPercentile,
		MinActiveBuckets: *minActiveBuckets,
		MinMedianVolume:  *minMedianVolume,
		MinP25Volume:     *minP25Volume,
		MinCycles:        *minCycles,
		ReboundWindow:    *reboundWindow,
		TaxRate:          *taxRate,
		TaxCap:           *taxCap,
	}
	report := rangeBottomReport{
		Strategy:      "range-bottom",
		QueryIntent:   "bottom-of-vwap-or-normal-range",
		Cash:          cash,
		Step:          *step,
		Days:          *days,
		GeneratedAt:   time.Now().Format(time.RFC3339),
		CandidateRows: len(candidates),
		Hits:          []rangeBottomHit{},
		Notes: []string{
			"Research-only range/VWAP scan; does not automate accounts, clients, offers, or gameplay.",
			"VWAP is estimated from public aggregated avgHigh/avgLow prices weighted by their reported volumes.",
		},
	}
	ctx := context.Background()
	for i, candidate := range candidates {
		resp, err := a.client.timeseries(ctx, candidate.Item.ID, *step)
		if err != nil {
			report.Errors++
			continue
		}
		report.Scanned++
		if hit, ok := analyzeRangeBottomCandidate(candidate, resp.Data, opts); ok {
			report.Hits = append(report.Hits, hit)
		}
		if *requestDelay > 0 && i < len(candidates)-1 {
			time.Sleep(*requestDelay)
		}
	}
	sort.Slice(report.Hits, func(i, j int) bool {
		return report.Hits[i].Score > report.Hits[j].Score
	})
	if *limit > 0 && len(report.Hits) > *limit {
		report.Hits = report.Hits[:*limit]
	}
	for i := range report.Hits {
		report.Hits[i].Rank = i + 1
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	return writeRangeBottomReport(os.Stdout, report)
}

func analyzeRangeBottomCandidate(candidate patternCandidate, points []timeseriesPoint, opts rangeBottomOptions) (rangeBottomHit, bool) {
	var buckets []rangeBucket
	var mids []float64
	var volumes []int64
	var weightedPrice float64
	var weightedVolume float64
	for _, p := range points {
		if p.Timestamp < opts.Cutoff {
			continue
		}
		mid, ok := timeseriesMid(p)
		if !ok || mid <= 0 {
			continue
		}
		volume := p.HighVolume + p.LowVolume
		buckets = append(buckets, rangeBucket{timestamp: p.Timestamp, mid: mid, volume: volume})
		mids = append(mids, mid)
		if volume > 0 {
			volumes = append(volumes, volume)
		}
		if p.AvgHighPrice != nil && p.HighVolume > 0 {
			weightedPrice += float64(*p.AvgHighPrice) * float64(p.HighVolume)
			weightedVolume += float64(p.HighVolume)
		}
		if p.AvgLowPrice != nil && p.LowVolume > 0 {
			weightedPrice += float64(*p.AvgLowPrice) * float64(p.LowVolume)
			weightedVolume += float64(p.LowVolume)
		}
	}
	if len(buckets) < 3 || len(volumes) == 0 || weightedVolume <= 0 {
		return rangeBottomHit{}, false
	}
	current, ok := currentCandidateMid(candidate, buckets[len(buckets)-1].mid)
	if !ok || current <= 0 {
		return rangeBottomHit{}, false
	}
	vwap := weightedPrice / weightedVolume
	if vwap <= 0 {
		return rangeBottomHit{}, false
	}
	rangeLow := minFloat64(mids)
	rangeHigh := maxFloat64(mids)
	if rangeHigh <= rangeLow {
		return rangeBottomHit{}, false
	}
	percentile := percentileRankFloat64(mids, current)
	bottomBand := quantileFloat64(mids, opts.BottomPercentile)
	if percentile > opts.MaxPercentile && current > bottomBand {
		return rangeBottomHit{}, false
	}
	medianVol := int64(math.Round(quantileInt64(volumes, 0.5)))
	p25Vol := int64(math.Round(quantileInt64(volumes, 0.25)))
	activeBuckets := len(volumes)
	if activeBuckets < opts.MinActiveBuckets || medianVol < opts.MinMedianVolume || p25Vol < opts.MinP25Volume {
		return rangeBottomHit{}, false
	}
	visits, cycles, lastBottom, lastRebound := bottomReboundCycles(buckets, bottomBand, vwap, opts.ReboundWindow)
	if visits == 0 || cycles < opts.MinCycles {
		return rangeBottomHit{}, false
	}
	targetVWAP := int64(math.Round(vwap))
	targetTop := int64(math.Round(rangeHigh))
	currentGP := int64(math.Round(current))
	netToVWAP := targetVWAP - currentGP - geTax(targetVWAP, opts.TaxRate, opts.TaxCap)
	netToTop := targetTop - currentGP - geTax(targetTop, opts.TaxRate, opts.TaxCap)
	if netToVWAP <= 0 {
		return rangeBottomHit{}, false
	}
	limit := int64(0)
	if candidate.Item.BuyLimit.Valid {
		limit = candidate.Item.BuyLimit.Int64
	}
	units := int64(0)
	if opts.Cash > 0 && currentGP > 0 {
		units = opts.Cash / currentGP
		if limit > 0 && units > limit {
			units = limit
		}
	}
	activeRatio := float64(activeBuckets) / float64(len(buckets))
	reliability := float64(cycles) / float64(visits)
	discountToVWAP := (vwap - current) / vwap
	discountToTop := (rangeHigh - current) / rangeHigh
	profit := units * netToVWAP
	limitForScore := limit
	if limitForScore <= 0 {
		limitForScore = 1000
	}
	score := math.Max(discountToVWAP, 0.01) *
		math.Log1p(float64(medianVol)) *
		math.Log1p(float64(limitForScore)) *
		(1 + reliability) *
		(1 + math.Min(float64(cycles), 8)/3) *
		activeRatio *
		math.Sqrt(float64(max[int64](profit, 1)))
	setup := "Range-bottom candidate"
	if percentile <= 0.10 {
		setup = "Deep range-bottom"
	}
	if cycles >= 3 {
		setup = "Recurring range-bottom"
	}
	if percentile <= 0.10 && cycles >= 3 {
		setup = "Deep recurring range-bottom"
	}
	hit := rangeBottomHit{
		ID:                     candidate.Item.ID,
		Name:                   candidate.Item.Name,
		Members:                candidate.Item.Members,
		BuyLimit:               limit,
		CurrentPrice:           current,
		CurrentLow:             candidate.CurrentLow,
		CurrentHigh:            candidate.CurrentHigh,
		VWAP:                   vwap,
		RangeLow:               rangeLow,
		RangeHigh:              rangeHigh,
		BottomBand:             bottomBand,
		Percentile:             percentile,
		DiscountToVWAP:         discountToVWAP,
		DiscountToRangeHigh:    discountToTop,
		MedianVolume:           medianVol,
		P25Volume:              p25Vol,
		ActiveBuckets:          activeBuckets,
		ObservedBuckets:        len(buckets),
		ActiveRatio:            activeRatio,
		ReboundCycles:          cycles,
		BottomVisits:           visits,
		ReboundReliability:     reliability,
		LastBottomTime:         lastBottom,
		LastReboundTime:        lastRebound,
		EstimatedNetToVWAP:     netToVWAP,
		EstimatedNetToRangeTop: netToTop,
		PortfolioUnits:         units,
		PortfolioCost:          units * currentGP,
		PortfolioProfitToVWAP:  profit,
		Setup:                  setup,
		Score:                  score,
		Rationale: []string{
			fmt.Sprintf("current %.1f gp is at %.1f%% of its sampled range percentile", current, percentile*100),
			fmt.Sprintf("estimated VWAP %.1f gp, %.1f%% discount to VWAP", vwap, discountToVWAP*100),
			fmt.Sprintf("%d bottom visits and %d rebounds over the selected window", visits, cycles),
			fmt.Sprintf("median bucket volume %s across %d active buckets", gp(medianVol), activeBuckets),
		},
	}
	if lastBottom > 0 {
		hit.LastBottomTimeISO = time.Unix(lastBottom, 0).Format(time.RFC3339)
	}
	if lastRebound > 0 {
		hit.LastReboundTimeISO = time.Unix(lastRebound, 0).Format(time.RFC3339)
	}
	return hit, true
}

func timeseriesMid(p timeseriesPoint) (float64, bool) {
	switch {
	case p.AvgHighPrice != nil && p.AvgLowPrice != nil:
		return float64(*p.AvgHighPrice+*p.AvgLowPrice) / 2, true
	case p.AvgHighPrice != nil:
		return float64(*p.AvgHighPrice), true
	case p.AvgLowPrice != nil:
		return float64(*p.AvgLowPrice), true
	default:
		return 0, false
	}
}

func currentCandidateMid(candidate patternCandidate, fallback float64) (float64, bool) {
	switch {
	case candidate.CurrentLow != nil && candidate.CurrentHigh != nil:
		return float64(*candidate.CurrentLow+*candidate.CurrentHigh) / 2, true
	case candidate.CurrentLow != nil:
		return float64(*candidate.CurrentLow), true
	case candidate.CurrentHigh != nil:
		return float64(*candidate.CurrentHigh), true
	case fallback > 0:
		return fallback, true
	default:
		return 0, false
	}
}

func bottomReboundCycles(buckets []rangeBucket, bottomBand, reboundTarget float64, window time.Duration) (int, int, int64, int64) {
	maxWindow := int64(window.Seconds())
	var visits, cycles int
	var lastBottom, lastRebound int64
	for i := 0; i < len(buckets); i++ {
		b := buckets[i]
		if b.volume <= 0 || b.mid > bottomBand {
			continue
		}
		visits++
		lastBottom = b.timestamp
		reboundedAt := -1
		for j := i + 1; j < len(buckets); j++ {
			next := buckets[j]
			if next.timestamp-b.timestamp > maxWindow {
				break
			}
			if next.volume > 0 && next.mid >= reboundTarget {
				reboundedAt = j
				lastRebound = next.timestamp
				break
			}
		}
		for i+1 < len(buckets) {
			next := buckets[i+1]
			if next.mid > bottomBand {
				break
			}
			i++
		}
		if reboundedAt >= 0 {
			cycles++
			i = reboundedAt
		}
	}
	return visits, cycles, lastBottom, lastRebound
}

func writeRangeBottomReport(w io.Writer, report rangeBottomReport) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Strategy\t%s\n", report.Strategy)
	fmt.Fprintf(tw, "Intent\t%s\n", report.QueryIntent)
	fmt.Fprintf(tw, "Cash lens\t%s gp\n", gp(report.Cash))
	fmt.Fprintf(tw, "Step / days\t%s / %d\n", report.Step, report.Days)
	fmt.Fprintf(tw, "Scanned\t%d/%d candidates (%d errors)\n\n", report.Scanned, report.CandidateRows, report.Errors)
	fmt.Fprintln(tw, "#\tITEM\tCURRENT\tVWAP\tPCTL\tDISC VWAP\tMED VOL\tACTIVE\tCYCLES\tNET VWAP\t40M EDGE\tSETUP\tSCORE")
	for _, hit := range report.Hits {
		fmt.Fprintf(tw, "%d\t%s\t%.1f\t%.1f\t%.1f%%\t%.1f%%\t%s\t%d/%d\t%d/%d\t%s\t%s\t%s\t%.1f\n",
			hit.Rank, hit.Name, hit.CurrentPrice, hit.VWAP, hit.Percentile*100, hit.DiscountToVWAP*100,
			gp(hit.MedianVolume), hit.ActiveBuckets, hit.ObservedBuckets, hit.ReboundCycles, hit.BottomVisits,
			gp(hit.EstimatedNetToVWAP), gp(hit.PortfolioProfitToVWAP), hit.Setup, hit.Score)
	}
	if len(report.Hits) == 0 {
		fmt.Fprintln(tw, "No range-bottom hits passed the filters.")
	}
	return tw.Flush()
}

func (a *app) cmdAgent(args []string) error {
	if len(args) == 0 {
		return a.cmdAgentManifest(nil)
	}
	switch args[0] {
	case "manifest", "tools", "schema":
		return a.cmdAgentManifest(args[1:])
	case "run", "ask", "query":
		return a.cmdAgentRun(args[1:])
	default:
		return fmt.Errorf("unknown agent subcommand %q; use manifest or run", args[0])
	}
}

func (a *app) cmdAgentManifest(args []string) error {
	fs := flag.NewFlagSet("agent manifest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", true, "emit JSON")
	_, err := parseCommandFlags(fs, args, nil)
	if err != nil {
		return err
	}
	manifest := buildAgentManifest()
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(manifest)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Workbench\t%s %s\n", manifest.Name, manifest.Version)
	fmt.Fprintln(tw, "TOOL\tINTENT\tCOMMAND\tGOOD FOR")
	for _, tool := range manifest.Tools {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", tool.Name, tool.Intent, tool.Command, strings.Join(tool.GoodFor, "; "))
	}
	return tw.Flush()
}

func buildAgentManifest() agentManifest {
	return agentManifest{
		Name:        "osrs-ge-agent-workbench",
		Version:     version,
		GeneratedAt: time.Now().Format(time.RFC3339),
		DataSources: []string{
			"OSRS Wiki real-time prices mapping/latest/interval/timeseries API",
			"local SQLite cache at ~/.osrs-ge/osrs-ge.sqlite",
			"GE tax approximation via configured tax rate and cap",
		},
		Principles: []string{
			"Use natural language only as the user query; derive all parameters as generated artifacts.",
			"Run multiple probes when intent is ambiguous.",
			"Return evidence and caveats, not trade execution instructions.",
			"Prefer reproducible commands and JSON artifacts over fixed product assumptions.",
		},
		Tools: []agentToolManifest{
			{
				Name:        "range-bottom",
				Intent:      "find items near bottom of own VWAP/range",
				Command:     "osrs-ge range-bottom --cash 40m --days 90 --step 6h --json",
				Returns:     []string{"current price", "VWAP", "range percentile", "median volume", "active buckets", "rebound cycles"},
				GoodFor:     []string{"VWAP/range-bottom queries", "undervalued/liquid candidates", "monthly/quarterly historical context"},
				Caveats:     []string{"VWAP is estimated from aggregate high/low volumes", "6h/24h are better for longer horizons than 5m"},
				Example:     `items at the bottom of their VWAP range with consistent volume`,
				JSONSupport: true,
			},
			{
				Name:        "patterns",
				Intent:      "find dump/rebound shock patterns",
				Command:     "osrs-ge patterns --cash 40m --days 14 --step 5m --json",
				Returns:     []string{"dip low", "later rebound high", "event volumes", "net margin", "portfolio edge"},
				GoodFor:     []string{"Bronze-knife-style supply shocks", "recent dislocation events", "cheap high-limit flips"},
				Caveats:     []string{"default 5m history is short", "one-off events need recurring validation"},
				Example:     `cheap items that dump and rebound like bronze knives`,
				JSONSupport: true,
			},
			{
				Name:        "opportunities",
				Intent:      "current margin and liquidity snapshot",
				Command:     "osrs-ge opportunities --json",
				Returns:     []string{"current low/high", "tax-adjusted margin", "ROI", "volume", "buy limit"},
				GoodFor:     []string{"highest margins now", "current spread checks", "capital allocation starting point"},
				Caveats:     []string{"current spread is not recurrence", "thin volume can create false positives"},
				Example:     `highest current margin with above-average volume`,
				JSONSupport: true,
			},
			{
				Name:        "movers",
				Intent:      "short-term price or volume regime change",
				Command:     "osrs-ge movers --interval 1h --json",
				Returns:     []string{"price change", "volume change", "volume ratio", "current net margin"},
				GoodFor:     []string{"volume spikes", "recent dumps/pumps", "fresh regime changes"},
				Caveats:     []string{"short horizon only", "requires follow-up history check"},
				Example:     `items with unusual volume spikes today`,
				JSONSupport: true,
			},
			{
				Name:        "sql",
				Intent:      "read-only cache inspection",
				Command:     `osrs-ge sql "SELECT ..." --json`,
				Returns:     []string{"arbitrary read-only rows from local cache"},
				GoodFor:     []string{"schema inspection", "custom filters", "debugging freshness and metadata"},
				Caveats:     []string{"cache only contains synced bulk snapshots plus metadata", "timeseries is fetched live per item"},
				Example:     `show high buy-limit members items`,
				JSONSupport: true,
			},
		},
		OutputNotice: []string{
			"All outputs are research artifacts.",
			"No command places offers, controls a client, or uses account credentials.",
			"Public aggregate data cannot prove personal fills.",
		},
	}
}

func (a *app) cmdAgentRun(args []string) error {
	fs := flag.NewFlagSet("agent run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cash := fs.String("cash", "40m", "cash lens")
	limit := fs.Int("limit", 6, "rows per probe")
	candidateLimit := fs.Int("candidate-limit", 120, "candidate rows per expensive probe")
	requestDelay := fs.Duration("request-delay", 5*time.Millisecond, "delay between item timeseries requests")
	jsonOut := fs.Bool("json", false, "emit JSON")
	positionals, err := parseCommandFlags(fs, args, map[string]bool{"cash": true, "limit": true, "candidate-limit": true, "request-delay": true})
	if err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(positionals, " "))
	if query == "" {
		return errors.New("agent run requires a natural-language query")
	}
	spec := deriveAgentResearchSpec(query, *cash)
	probes := buildAgentProbes(spec, *cash, *limit, *candidateLimit, *requestDelay)
	report := agentRunReport{
		Strategy:    "agent-workbench",
		Query:       query,
		GeneratedAt: time.Now().Format(time.RFC3339),
		Spec:        spec,
		Warnings: []string{
			"This is a deterministic workbench run, not a live LLM reasoning loop yet.",
			"Use probe artifacts to iterate before treating any candidate as alpha.",
			"Scores are only comparable within the same probe family.",
		},
		NextSteps: []string{
			"Inspect the strongest rows from each probe.",
			"Run focused item history checks on candidates that survive liquidity and recurrence filters.",
			"Add a declarative backtest once entry/exit rules are explicit.",
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, probe := range probes {
		result := a.runAgentProbe(ctx, probe)
		report.Probes = append(report.Probes, result)
		report.SummaryRows = append(report.SummaryRows, result.SummaryRows...)
	}
	for i := range report.SummaryRows {
		report.SummaryRows[i].Rank = i + 1
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	return writeAgentRunTable(os.Stdout, report)
}

func deriveAgentResearchSpec(query, cash string) agentResearchSpec {
	q := strings.ToLower(query)
	spec := agentResearchSpec{
		Intent:       "broad_research",
		Cash:         cash,
		Horizons:     []string{"current", "14d", "90d"},
		PrimaryTools: []string{"range-bottom", "patterns", "opportunities"},
		EvidenceTests: []string{
			"liquidity and median volume",
			"tax-adjusted edge",
			"buy-limit portfolio fit",
			"stale or one-off signal rejection",
		},
	}
	if detectStrategyIntent(query) == "range_bottom" {
		spec.Intent = "range_bottom"
		spec.Horizons = []string{"30d", "90d", "365d"}
		spec.PrimaryTools = []string{"range-bottom", "movers", "opportunities"}
		spec.EvidenceTests = []string{
			"current price percentile versus own history",
			"discount to estimated VWAP",
			"median and p25 volume consistency",
			"historical bottom-to-rebound cycles",
			"single-event concentration rejection",
		}
	}
	if strings.Contains(q, "dump") || strings.Contains(q, "rebound") || strings.Contains(q, "bronze") || strings.Contains(q, "supply shock") {
		spec.Intent = "shock_reversion"
		spec.PrimaryTools = []string{"patterns", "range-bottom", "movers"}
		spec.EvidenceTests = []string{
			"repeated dump-to-rebound cycles",
			"event volume on both legs",
			"median volume outside event windows",
			"tax-adjusted spread",
			"GE limit and bankroll fit",
		}
	}
	if strings.Contains(q, "margin") || strings.Contains(q, "spread") || strings.Contains(q, "profit") {
		spec.PrimaryTools = appendUnique(spec.PrimaryTools, "opportunities")
	}
	if strings.Contains(q, "volume spike") || strings.Contains(q, "mover") || strings.Contains(q, "today") {
		spec.PrimaryTools = appendUnique([]string{"movers"}, spec.PrimaryTools...)
	}
	if cashMatch := regexpLikeCash(q); cashMatch != "" {
		spec.Cash = cashMatch
	}
	return spec
}

func buildAgentProbes(spec agentResearchSpec, cash string, limit, candidateLimit int, requestDelay time.Duration) []agentProbeSpec {
	if spec.Cash != "" {
		cash = spec.Cash
	}
	seen := map[string]bool{}
	var probes []agentProbeSpec
	add := func(name, intent string, cmd []string) {
		if seen[name] {
			return
		}
		seen[name] = true
		probes = append(probes, agentProbeSpec{Name: name, Intent: intent, Command: cmd})
	}
	for _, tool := range spec.PrimaryTools {
		switch tool {
		case "range-bottom":
			add("range-bottom-90d", "range_bottom", []string{
				"range-bottom", "--json", "--cash", cash, "--days", "90", "--step", "6h",
				"--limit", strconv.Itoa(limit), "--candidate-limit", strconv.Itoa(candidateLimit),
				"--request-delay", requestDelay.String(),
			})
		case "patterns":
			add("patterns-recent", "shock_reversion", []string{
				"patterns", "--json", "--cash", cash, "--days", "14", "--step", "5m",
				"--limit", strconv.Itoa(limit), "--candidate-limit", strconv.Itoa(candidateLimit),
				"--request-delay", requestDelay.String(),
			})
		case "opportunities":
			add("opportunities-now", "current_margin", []string{
				"opportunities", "--json", "--limit", strconv.Itoa(limit), "--min-volume", "500",
			})
		case "movers":
			add("movers-1h", "short_term_movers", []string{
				"movers", "--json", "--interval", "1h", "--limit", strconv.Itoa(limit), "--min-volume", "500",
			})
		}
	}
	return probes
}

func (a *app) runAgentProbe(ctx context.Context, probe agentProbeSpec) agentProbeResult {
	args := append([]string{"--db", a.dbPath, "--user-agent", a.userAgent}, probe.Command...)
	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	result := agentProbeResult{
		Name:    probe.Name,
		Intent:  probe.Intent,
		Command: append([]string{os.Args[0]}, args...),
	}
	if err != nil {
		result.OK = false
		result.Error = strings.TrimSpace(string(out) + "\n" + err.Error())
		return result
	}
	result.OK = true
	result.Artifact = json.RawMessage(out)
	result.SummaryRows = summarizeAgentProbe(probe.Name, probe.Intent, out)
	return result
}

func summarizeAgentProbe(name, intent string, raw []byte) []agentSummaryRow {
	switch intent {
	case "range_bottom":
		var report rangeBottomReport
		if err := json.Unmarshal(raw, &report); err != nil {
			return nil
		}
		var rows []agentSummaryRow
		for _, hit := range report.Hits {
			rows = append(rows, agentSummaryRow{
				Probe:         name,
				Item:          hit.Name,
				PrimaryMetric: fmt.Sprintf("current %.1f vs VWAP %.1f", hit.CurrentPrice, hit.VWAP),
				Evidence:      fmt.Sprintf("pctl %.1f%%, discount %.1f%%, median vol %s, cycles %d/%d", hit.Percentile*100, hit.DiscountToVWAP*100, gp(hit.MedianVolume), hit.ReboundCycles, hit.BottomVisits),
				Setup:         hit.Setup,
				Score:         hit.Score,
			})
		}
		return rows
	case "shock_reversion":
		var report patternReport
		if err := json.Unmarshal(raw, &report); err != nil {
			return nil
		}
		var rows []agentSummaryRow
		for _, hit := range report.Hits {
			rows = append(rows, agentSummaryRow{
				Probe:         name,
				Item:          hit.Name,
				PrimaryMetric: fmt.Sprintf("%s -> %s gp", gp(hit.Low), gp(hit.High)),
				Evidence:      fmt.Sprintf("net %s gp, low vol %s, high vol %s, 40m edge %s", gp(hit.NetMargin), gp(hit.LowVolume), gp(hit.HighVolume), gp(hit.PortfolioProfit)),
				Setup:         hit.Setup,
				Score:         hit.Score,
			})
		}
		return rows
	case "current_margin":
		var opps []opportunity
		if err := json.Unmarshal(raw, &opps); err != nil {
			return nil
		}
		var rows []agentSummaryRow
		for _, opp := range opps {
			rows = append(rows, agentSummaryRow{
				Probe:         name,
				Item:          opp.Name,
				PrimaryMetric: fmt.Sprintf("%s net / %.2f%% ROI", gp(opp.NetMargin), opp.ROI*100),
				Evidence:      fmt.Sprintf("low %s, high %s, vol %s, limit %s", gp(opp.Low), gp(opp.High), gp(opp.Volume), emptyZero(opp.BuyLimit)),
				Setup:         "Current margin candidate",
				Score:         opp.Score,
			})
		}
		return rows
	case "short_term_movers":
		var movs []mover
		if err := json.Unmarshal(raw, &movs); err != nil {
			return nil
		}
		var rows []agentSummaryRow
		for _, m := range movs {
			rows = append(rows, agentSummaryRow{
				Probe:         name,
				Item:          m.Name,
				PrimaryMetric: fmt.Sprintf("%.2f%% price move", m.PriceChangePct*100),
				Evidence:      fmt.Sprintf("volume %s -> %s, vol ratio %.2fx, net %s", gp(m.PreviousVolume), gp(m.CurrentVolume), m.VolumeRatio, gp(m.CurrentNetMargin)),
				Setup:         "Short-term mover",
				Score:         math.Abs(m.PriceChangePct) * math.Log1p(float64(max[int64](m.CurrentVolume, 1))),
			})
		}
		return rows
	default:
		return nil
	}
}

func writeAgentRunTable(w io.Writer, report agentRunReport) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Query\t%s\n", report.Query)
	fmt.Fprintf(tw, "Intent\t%s\n", report.Spec.Intent)
	fmt.Fprintf(tw, "Tools\t%s\n\n", strings.Join(report.Spec.PrimaryTools, ", "))
	fmt.Fprintln(tw, "#\tPROBE\tITEM\tMETRIC\tEVIDENCE\tSETUP\tSCORE")
	for _, row := range report.SummaryRows {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%.1f\n", row.Rank, row.Probe, row.Item, row.PrimaryMetric, row.Evidence, row.Setup, row.Score)
	}
	if len(report.SummaryRows) == 0 {
		fmt.Fprintln(tw, "No summary rows returned. Inspect JSON probe errors/artifacts.")
	}
	return tw.Flush()
}

func appendUnique[T comparable](base []T, values ...T) []T {
	seen := make(map[T]bool, len(base)+len(values))
	for _, v := range base {
		seen[v] = true
	}
	for _, v := range values {
		if !seen[v] {
			base = append(base, v)
			seen[v] = true
		}
	}
	return base
}

func regexpLikeCash(q string) string {
	fields := strings.Fields(strings.ReplaceAll(q, ",", ""))
	for _, f := range fields {
		if strings.HasSuffix(f, "m") {
			num := strings.TrimSuffix(f, "m")
			if _, err := strconv.ParseFloat(num, 64); err == nil {
				return num + "m"
			}
		}
	}
	return ""
}

func (a *app) cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "127.0.0.1:8765", "listen address")
	_, err := parseCommandFlags(fs, args, map[string]bool{"addr": true})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, dashboardHTML)
	})
	mux.HandleFunc("/api/patterns", a.handlePatternsAPI)
	mux.HandleFunc("/api/range-bottom", a.handleRangeBottomAPI)
	mux.HandleFunc("/api/strategy", a.handleStrategyAPI)
	mux.HandleFunc("/api/agent/manifest", a.handleAgentManifestAPI)
	mux.HandleFunc("/api/agent/run", a.handleAgentRunAPI)
	fmt.Printf("osrs-ge dashboard listening on http://%s\n", *addr)
	return http.ListenAndServe(*addr, mux)
}

func (a *app) handlePatternsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	args := []string{
		"--db", a.dbPath,
		"--user-agent", a.userAgent,
		"patterns",
		"--json",
		"--like", queryDefault(q, "like", "bronze knife"),
		"--cash", queryDefault(q, "cash", "40m"),
		"--limit", queryDefault(q, "limit", "20"),
		"--candidate-limit", queryDefault(q, "candidate_limit", "250"),
		"--days", queryDefault(q, "days", "14"),
		"--request-delay", queryDefault(q, "request_delay", "10ms"),
		"--max-rebound", queryDefault(q, "max_rebound", "72h"),
		"--min-low", queryDefault(q, "min_low", "10"),
		"--max-low", queryDefault(q, "max_low", "80"),
		"--min-high", queryDefault(q, "min_high", "60"),
		"--max-high", queryDefault(q, "max_high", "250"),
		"--min-low-volume", queryDefault(q, "min_low_volume", "100"),
		"--min-high-volume", queryDefault(q, "min_high_volume", "300"),
	}
	a.runJSONCommand(w, r, args)
}

func (a *app) handleRangeBottomAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	args := []string{
		"--db", a.dbPath,
		"--user-agent", a.userAgent,
		"range-bottom",
		"--json",
		"--cash", queryDefault(q, "cash", "40m"),
		"--step", queryDefault(q, "step", "6h"),
		"--days", queryDefault(q, "days", "90"),
		"--limit", queryDefault(q, "limit", "25"),
		"--candidate-limit", queryDefault(q, "candidate_limit", "500"),
		"--request-delay", queryDefault(q, "request_delay", "10ms"),
		"--min-buy-limit", queryDefault(q, "min_buy_limit", "5000"),
		"--max-value", queryDefault(q, "max_value", "0"),
		"--max-percentile", queryDefault(q, "max_percentile", "0.25"),
		"--bottom-percentile", queryDefault(q, "bottom_percentile", "0.25"),
		"--min-active-buckets", queryDefault(q, "min_active_buckets", "20"),
		"--min-median-volume", queryDefault(q, "min_median_volume", "250"),
		"--min-p25-volume", queryDefault(q, "min_p25_volume", "1"),
		"--min-cycles", queryDefault(q, "min_cycles", "1"),
		"--rebound-window", queryDefault(q, "rebound_window", "720h"),
	}
	a.runJSONCommand(w, r, args)
}

func (a *app) handleStrategyAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	intent := strings.TrimSpace(q.Get("intent"))
	if intent == "" {
		intent = detectStrategyIntent(q.Get("query"))
	}
	switch intent {
	case "range_bottom", "range-bottom", "vwap_bottom", "vwap-bottom":
		a.handleRangeBottomAPI(w, r)
	default:
		a.handlePatternsAPI(w, r)
	}
}

func (a *app) handleAgentManifestAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(buildAgentManifest())
}

func (a *app) handleAgentRunAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("query"))
	if query == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}
	args := []string{
		"--db", a.dbPath,
		"--user-agent", a.userAgent,
		"agent", "run", query,
		"--json",
		"--cash", queryDefault(q, "cash", "40m"),
		"--limit", queryDefault(q, "limit", "6"),
		"--candidate-limit", queryDefault(q, "candidate_limit", "120"),
		"--request-delay", queryDefault(q, "request_delay", "5ms"),
	}
	a.runJSONCommand(w, r, args)
}

func detectStrategyIntent(query string) string {
	q := strings.ToLower(query)
	switch {
	case strings.Contains(q, "vwap"),
		strings.Contains(q, "trading range"),
		strings.Contains(q, "normal range"),
		strings.Contains(q, "bottom"),
		strings.Contains(q, "historical values"),
		strings.Contains(q, "undervalued"):
		return "range_bottom"
	default:
		return "patterns"
	}
}

func (a *app) runJSONCommand(w http.ResponseWriter, r *http.Request, args []string) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		status := http.StatusInternalServerError
		if ctx.Err() == context.DeadlineExceeded {
			status = http.StatusGatewayTimeout
		}
		http.Error(w, strings.TrimSpace(string(out)+"\n"+err.Error()), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

func queryDefault(values url.Values, key, fallback string) string {
	if v := strings.TrimSpace(values.Get(key)); v != "" {
		return v
	}
	return fallback
}

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>OSRS GE Strategy Desk</title>
<style>
:root {
  color-scheme: light;
  --ink: #18202a;
  --muted: #667085;
  --line: #d7dde6;
  --panel: #f7f9fc;
  --accent: #176b87;
  --accent-2: #7a4b12;
  --good: #176c47;
  --warn: #9a5b00;
  --bad: #a83c32;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  color: var(--ink);
  background: #eef2f6;
}
header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 18px 24px;
  background: #ffffff;
  border-bottom: 1px solid var(--line);
}
h1 {
  margin: 0;
  font-size: 20px;
  font-weight: 720;
}
.sub {
  color: var(--muted);
  font-size: 13px;
}
main {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 380px;
  gap: 16px;
  padding: 16px;
}
.panel, .strategy-panel {
  background: #ffffff;
  border: 1px solid var(--line);
  border-radius: 8px;
}
.strategy-panel {
  padding: 12px;
  margin-bottom: 12px;
}
.strategy-panel h2 {
  margin: 0 0 8px;
  font-size: 16px;
}
.strategy-text {
  min-height: 128px;
}
.action-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 10px;
}
.ghost {
  color: var(--accent);
  background: #e9f4f8;
}
label {
  display: grid;
  gap: 5px;
  color: var(--muted);
  font-size: 12px;
}
input, textarea, select {
  width: 100%;
  border: 1px solid #c9d2df;
  border-radius: 6px;
  padding: 8px 9px;
  color: var(--ink);
  background: #ffffff;
  font: inherit;
}
button {
  border: 0;
  border-radius: 6px;
  padding: 9px 12px;
  color: #ffffff;
  background: var(--accent);
  font-weight: 680;
  cursor: pointer;
}
button.secondary {
  background: var(--accent-2);
}
.tablewrap {
  overflow: auto;
  background: #ffffff;
  border: 1px solid var(--line);
  border-radius: 8px;
}
table {
  width: 100%;
  border-collapse: collapse;
  min-width: 980px;
  font-size: 13px;
}
th, td {
  text-align: left;
  padding: 9px 10px;
  border-bottom: 1px solid #e6eaf0;
  white-space: nowrap;
}
th {
  position: sticky;
  top: 0;
  background: #f3f6fa;
  color: #475467;
  font-size: 12px;
}
.score {
  font-variant-numeric: tabular-nums;
  font-weight: 700;
}
.setup {
  display: inline-block;
  border-radius: 999px;
  padding: 3px 8px;
  background: #e9f4f8;
  color: #135a72;
}
.setup.fresh { background: #e9f7ef; color: var(--good); }
.setup.rebound { background: #fff3df; color: var(--warn); }
.side {
  display: grid;
  gap: 12px;
  align-content: start;
}
.panel {
  padding: 12px;
}
.panel h2 {
  margin: 0 0 10px;
  font-size: 15px;
}
.metric-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.metric {
  background: var(--panel);
  border: 1px solid #e2e7ef;
  border-radius: 6px;
  padding: 8px;
}
.metric b {
  display: block;
  font-size: 18px;
}
textarea {
  min-height: 178px;
  resize: vertical;
  line-height: 1.4;
}
.generated {
  min-height: 240px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
}
.status {
  min-height: 20px;
  color: var(--muted);
  font-size: 13px;
}
@media (max-width: 980px) {
  main { grid-template-columns: 1fr; }
}
</style>
</head>
<body>
<header>
  <div>
    <h1>OSRS GE Strategy Desk</h1>
    <div class="sub">Natural-language market research for manual Grand Exchange traders</div>
  </div>
  <button id="scan">Run Scan</button>
</header>
<main>
  <section>
    <div class="strategy-panel">
      <h2>Describe The Edge</h2>
      <textarea class="strategy-text" id="strategy_text" spellcheck="false">Looking back at historical values, which items are at the bottom of their VWAP or normal trading range, still have consistent volume, and have a history of rebounding? I have 40m and want to filter out one-off low-volume spikes.</textarea>
      <div class="action-row">
        <button id="compile">Build LLM Search Plan</button>
        <button class="ghost" id="scanFromText">Run Agent Workbench</button>
      </div>
    </div>
    <div class="status" id="status"></div>
    <div class="tablewrap">
      <table>
        <thead>
          <tr id="headrow">
            <th>#</th><th>Item</th><th>Dip</th><th>Rebound</th><th>Net</th><th>ROI</th>
            <th>Dip Vol</th><th>Rebound Vol</th><th>Limit</th><th>40M Edge</th><th>Setup</th><th>Score</th>
          </tr>
        </thead>
        <tbody id="rows"></tbody>
      </table>
    </div>
  </section>
  <aside class="side">
    <div class="panel">
      <h2>Run</h2>
      <div class="metric-grid">
        <div class="metric"><span>Scanned</span><b id="scanned">-</b></div>
        <div class="metric"><span>Hits</span><b id="hits">-</b></div>
        <div class="metric"><span>Errors</span><b id="errors">-</b></div>
        <div class="metric"><span>Cash</span><b id="cashmetric">-</b></div>
      </div>
    </div>
    <div class="panel">
      <h2>LLM Search Plan</h2>
      <textarea id="brief" spellcheck="false"></textarea>
    </div>
    <div class="panel">
      <h2>Generated Strategy Fields</h2>
      <textarea class="generated" id="generated_fields" spellcheck="false" readonly></textarea>
    </div>
  </aside>
</main>
<script>
const $ = (id) => document.getElementById(id);
const fmt = (n) => n == null ? "-" : Number(n).toLocaleString();
const pct = (n) => n == null ? "-" : (Number(n) * 100).toFixed(2) + "%";
const shortTime = (iso) => iso ? new Date(iso).toLocaleString([], {month:"2-digit", day:"2-digit", hour:"2-digit", minute:"2-digit"}) : "-";
let lastReport = null;

function deriveStrategySpec() {
  const text = $("strategy_text").value.toLowerCase();
  const spec = {
    intent: "shock_reversion",
    objective: "find cheap, high-limit items with dump/rebound behavior",
    cash: "40m",
    candidate_limit: "250",
    limit: "20",
    days: "14",
    step: "5m",
    max_low: "80",
    min_high: "60",
    requested_tools: ["scan_patterns"],
    evidence_tests: [
      "tax-adjusted spread",
      "buy-limit adjusted portfolio fit",
      "dip and rebound volume",
      "one-off spike rejection"
    ],
    llm_generated: true
  };
  if (text.includes("vwap") || text.includes("trading range") || text.includes("bottom") || text.includes("historical values")) {
    spec.intent = "range_bottom";
    spec.objective = "find items trading near the bottom of their historical VWAP or normal range";
    spec.days = "90";
    spec.step = "6h";
    spec.candidate_limit = "500";
    spec.limit = "25";
    spec.requested_tools = ["scan_range_bottom", "scan_recurring", "scan_patterns"];
    spec.evidence_tests = [
      "current price percentile vs 30d and 90d range",
      "distance from VWAP or volume-weighted normal band",
      "active trading days and median volume",
      "rebound history after bottom-range visits",
      "single-event concentration rejection",
      "tax-adjusted spread and GE-limit portfolio fit"
    ];
  }
  if (text.includes("quarter") || text.includes("90") || text.includes("recurring") || text.includes("consistent")) {
    spec.days = "90";
    spec.candidate_limit = "500";
    spec.limit = "25";
    if (!spec.requested_tools.includes("scan_recurring")) {
      spec.requested_tools.unshift("scan_recurring");
    }
  }
  if (text.includes("monthly") || text.includes("30d") || text.includes("30 day")) {
    spec.days = "30";
  }
  if (text.includes("year") || text.includes("365")) {
    spec.days = "365";
  }
  const cashMatch = text.match(/(\d+(?:\.\d+)?)\s*m\b/);
  if (cashMatch) {
    spec.cash = cashMatch[1] + "m";
  }
  const lowBand = text.match(/(\d+)\s*[-to]+\s*(\d+)\s*gp/);
  if (lowBand) {
    spec.max_low = lowBand[2];
  }
  $("generated_fields").value = JSON.stringify(spec, null, 2);
  return spec;
}

async function scan() {
  const spec = deriveStrategySpec();
  $("status").textContent = "Running agent workbench probes...";
  $("scan").disabled = true;
  const params = new URLSearchParams({
    query: $("strategy_text").value,
    cash: spec.cash,
    candidate_limit: spec.candidate_limit,
    limit: "6"
  });
  try {
    const res = await fetch("/api/agent/run?" + params.toString());
    const text = await res.text();
    if (!res.ok) throw new Error(text);
    lastReport = JSON.parse(text);
    render(lastReport);
    $("status").textContent = "Updated " + new Date(lastReport.generated_at).toLocaleTimeString();
  } catch (err) {
    $("status").textContent = "Scan failed: " + err.message;
  } finally {
    $("scan").disabled = false;
  }
}

function compilePlan() {
  deriveStrategySpec();
  $("brief").value = buildStrategyPlan(lastReport);
}

function render(report) {
  if (report.strategy === "agent-workbench") {
    $("scanned").textContent = fmt(report.probes.length) + " probes";
    $("hits").textContent = fmt(report.summary_rows.length);
    $("errors").textContent = fmt(report.probes.filter((probe) => !probe.ok).length);
    $("cashmetric").textContent = report.spec.cash || "-";
    $("generated_fields").value = JSON.stringify(report.spec, null, 2);
    $("headrow").innerHTML = "<th>#</th><th>Probe</th><th>Item</th><th>Metric</th><th>Evidence</th><th>Setup</th><th>Score</th>";
    $("rows").innerHTML = report.summary_rows.map((row) =>
      "<tr>" +
        "<td>" + row.rank + "</td><td>" + row.probe + "</td><td>" + row.item + "</td>" +
        "<td>" + row.primary_metric + "</td><td>" + row.evidence + "</td>" +
        "<td><span class=\"setup\">" + row.setup + "</span></td><td class=\"score\">" + Number(row.score).toFixed(1) + "</td>" +
      "</tr>"
    ).join("");
    $("brief").value = buildStrategyPlan(report);
    return;
  }
  $("scanned").textContent = fmt(report.scanned) + "/" + fmt(report.candidate_rows);
  $("hits").textContent = fmt(report.hits.length);
  $("errors").textContent = fmt(report.errors);
  $("cashmetric").textContent = fmt(report.cash);
  if (report.strategy === "range-bottom") {
    $("headrow").innerHTML = "<th>#</th><th>Item</th><th>Current</th><th>VWAP</th><th>Percentile</th><th>Discount</th>" +
      "<th>Median Vol</th><th>Active</th><th>Cycles</th><th>40M Edge</th><th>Setup</th><th>Score</th>";
    $("rows").innerHTML = report.hits.map((hit) => {
      const cls = hit.setup.startsWith("Deep") ? "fresh" : hit.setup.startsWith("Recurring") ? "rebound" : "";
      return "<tr>" +
        "<td>" + hit.rank + "</td><td>" + hit.name + "</td><td>" + Number(hit.current_price).toFixed(1) + "</td>" +
        "<td>" + Number(hit.vwap).toFixed(1) + "</td><td>" + pct(hit.percentile) + "</td>" +
        "<td>" + pct(hit.discount_to_vwap) + "</td><td>" + fmt(hit.median_volume) + "</td>" +
        "<td>" + fmt(hit.active_buckets) + "/" + fmt(hit.observed_buckets) + "</td>" +
        "<td>" + fmt(hit.rebound_cycles) + "/" + fmt(hit.bottom_visits) + "</td>" +
        "<td>" + fmt(hit.portfolio_profit_to_vwap) + "</td>" +
        "<td><span class=\"setup " + cls + "\">" + hit.setup + "</span></td><td class=\"score\">" + Number(hit.score).toFixed(1) + "</td>" +
      "</tr>";
    }).join("");
  } else {
    $("headrow").innerHTML = "<th>#</th><th>Item</th><th>Dip</th><th>Rebound</th><th>Net</th><th>ROI</th>" +
      "<th>Dip Vol</th><th>Rebound Vol</th><th>Limit</th><th>40M Edge</th><th>Setup</th><th>Score</th>";
    $("rows").innerHTML = report.hits.map((hit) => {
    const cls = hit.setup.startsWith("Fresh") ? "fresh" : hit.setup.startsWith("Rebound") ? "rebound" : "";
    return "<tr>" +
      "<td>" + hit.rank + "</td><td>" + hit.name + "</td><td>" + fmt(hit.low) + " @ " + shortTime(hit.low_time_iso) + "</td>" +
      "<td>" + fmt(hit.high) + " @ " + shortTime(hit.high_time_iso) + "</td><td>" + fmt(hit.net_margin) + "</td>" +
      "<td>" + pct(hit.roi) + "</td><td>" + fmt(hit.low_volume) + "</td><td>" + fmt(hit.high_volume) + "</td>" +
      "<td>" + fmt(hit.buy_limit) + "</td><td>" + fmt(hit.portfolio_profit) + "</td>" +
      "<td><span class=\"setup " + cls + "\">" + hit.setup + "</span></td><td class=\"score\">" + Number(hit.score).toFixed(1) + "</td>" +
    "</tr>";
  }).join("");
  }
  $("brief").value = buildStrategyPlan(report);
}

function buildStrategyPlan(report) {
  const strategyText = $("strategy_text").value.trim();
  const spec = report && report.strategy === "agent-workbench" ? report.spec : deriveStrategySpec();
  let top = "";
  if (report && report.strategy === "agent-workbench") {
    top = report.summary_rows.slice(0, 12).map((row) =>
      row.rank + ". [" + row.probe + "] " + row.item + ": " + row.primary_metric +
      " | " + row.evidence + " | " + row.setup
    ).join("\n");
  } else if (report && report.hits && report.strategy === "range-bottom") {
    top = report.hits.slice(0, 8).map((hit) =>
      hit.rank + ". " + hit.name + ": current " + Number(hit.current_price).toFixed(1) +
      ", VWAP " + Number(hit.vwap).toFixed(1) + ", pctl " + pct(hit.percentile) +
      ", discount " + pct(hit.discount_to_vwap) + ", cycles " + hit.rebound_cycles + "/" + hit.bottom_visits +
      ", median vol " + fmt(hit.median_volume) + ", setup=" + hit.setup
    ).join("\n");
  } else if (report && report.hits) {
    top = report.hits.slice(0, 8).map((hit) =>
      hit.rank + ". " + hit.name + ": " + fmt(hit.low) + " -> " + fmt(hit.high) +
      ", net " + fmt(hit.net_margin) + " gp/ea, " + pct(hit.roi) + " ROI, " +
      fmt(hit.portfolio_profit) + " gp 40M-limit-cycle edge, setup=" + hit.setup
    ).join("\n");
  }
  const scanLine = report && report.strategy === "agent-workbench" ?
    "Current workbench run: " + report.probes.length + " probes, " + report.summary_rows.length + " summary rows.\n\n" :
    report ?
    "Current scan: " + report.strategy + ", " + report.days + "d lookback, " + report.scanned + "/" + report.candidate_rows + " candidates scanned.\n\n" :
    "Current scan: not run yet. Generate several candidate scans before answering.\n\n";
  return "You are the LLM strategy compiler for an OSRS Grand Exchange research product.\n\n" +
    "User request:\n" + strategyText + "\n\n" +
    "Generated strategy fields:\n" + JSON.stringify(spec, null, 2) + "\n\n" +
    "Interpret the request into backend scans. The text above is the only user-authored query; all advanced fields are LLM-generated and may be revised after evidence comes back.\n\n" +
    "Execution plan:\n" +
    "1. Use the agent manifest and generated tool list to fan out scans over 30d, 90d, and 365d as relevant.\n" +
    "2. For VWAP/range-bottom intent, rank items by current price percentile, distance below normal range, volume consistency, and rebound history.\n" +
    "3. For dump/rebound intent, rank items by repeated cycles, median event volume, tax-adjusted edge, and GE-limit fit.\n" +
    "4. Reject candidates where one spike, thin volume, stale data, or permanent repricing explains the signal.\n\n" +
    scanLine +
    "Visible agent evidence:\n" + (top || "No routed hits available yet.") + "\n\n" +
    "Return: recommended strategy template, exact scanner parameter sweeps to run, top candidates with evidence, rejected traps, and watch rules. " +
    "Keep the product read-only: no account credentials, client automation, or offer placement.";
}

$("scan").addEventListener("click", scan);
$("scanFromText").addEventListener("click", scan);
$("compile").addEventListener("click", compilePlan);
scan();
</script>
</body>
</html>`

func (a *app) cmdTimeseries(args []string) error {
	fs := flag.NewFlagSet("timeseries", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	step := fs.String("step", "1h", "5m, 1h, 6h, or 24h")
	limit := fs.Int("limit", 30, "maximum rows to print from the end")
	jsonOut := fs.Bool("json", false, "emit JSON")
	positionals, err := parseCommandFlags(fs, args, map[string]bool{"step": true, "limit": true})
	if err != nil {
		return err
	}
	input := strings.TrimSpace(strings.Join(positionals, " "))
	if input == "" {
		return errors.New("timeseries requires an item name or id")
	}
	if err := a.ensureItems(false); err != nil {
		return err
	}
	item, err := a.resolveItem(input)
	if err != nil {
		return err
	}
	resp, err := a.client.timeseries(context.Background(), item.ID, *step)
	if err != nil {
		return err
	}
	points := resp.Data
	if *limit > 0 && len(points) > *limit {
		points = points[len(points)-*limit:]
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(points)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Item\t%s (%d)\n", item.Name, item.ID)
	fmt.Fprintln(tw, "TIME\tAVG LOW\tAVG HIGH\tLOW VOL\tHIGH VOL\tTOTAL VOL")
	for _, p := range points {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", time.Unix(p.Timestamp, 0).Format("2006-01-02 15:04"), ptrGP(p.AvgLowPrice), ptrGP(p.AvgHighPrice), gp(p.LowVolume), gp(p.HighVolume), gp(p.LowVolume+p.HighVolume))
	}
	return tw.Flush()
}

func (a *app) cmdSQL(args []string) error {
	fs := flag.NewFlagSet("sql", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit JSON")
	positionals, err := parseCommandFlags(fs, args, nil)
	if err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(positionals, " "))
	if query == "" {
		return errors.New("sql requires a query")
	}
	lower := strings.ToLower(query)
	if !(strings.HasPrefix(lower, "select") || strings.HasPrefix(lower, "with")) {
		return errors.New("sql is read-only; query must start with SELECT or WITH")
	}
	rows, err := a.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	if *jsonOut {
		var out []map[string]any
		for rows.Next() {
			if err := rows.Scan(ptrs...); err != nil {
				return err
			}
			row := make(map[string]any, len(cols))
			for i, col := range cols {
				row[col] = normalizeSQLValue(values[i])
			}
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(cols, "\t"))
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		cells := make([]string, len(cols))
		for i := range cols {
			cells[i] = fmt.Sprint(normalizeSQLValue(values[i]))
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return tw.Flush()
}

func normalizeSQLValue(v any) any {
	switch t := v.(type) {
	case []byte:
		return string(t)
	default:
		return t
	}
}

func parseGP(input string) (int64, error) {
	s := strings.TrimSpace(strings.ToLower(strings.ReplaceAll(input, ",", "")))
	if s == "" {
		return 0, errors.New("empty gp value")
	}
	mult := float64(1)
	switch {
	case strings.HasSuffix(s, "gp"):
		s = strings.TrimSpace(strings.TrimSuffix(s, "gp"))
	case strings.HasSuffix(s, "k"):
		mult = 1_000
		s = strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		mult = 1_000_000
		s = strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "b"):
		mult = 1_000_000_000
		s = strings.TrimSuffix(s, "b")
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, err
	}
	return int64(math.Round(v * mult)), nil
}

func parseOptionalGP(input string) (sql.NullInt64, error) {
	if strings.TrimSpace(input) == "" {
		return sql.NullInt64{}, nil
	}
	v, err := parseGP(input)
	if err != nil {
		return sql.NullInt64{}, err
	}
	return sql.NullInt64{Int64: v, Valid: true}, nil
}

func nullIntArg(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

func nullGP(v sql.NullInt64) string {
	if !v.Valid {
		return "-"
	}
	return gp(v.Int64)
}

func nullPct(v sql.NullFloat64) string {
	if !v.Valid {
		return "-"
	}
	return fmt.Sprintf("%.2f%%", v.Float64*100)
}

func avgPrice(high sql.NullInt64, low sql.NullInt64) (float64, bool) {
	switch {
	case high.Valid && low.Valid:
		return float64(high.Int64+low.Int64) / 2, true
	case high.Valid:
		return float64(high.Int64), true
	case low.Valid:
		return float64(low.Int64), true
	default:
		return 0, false
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func ptrAny(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullInt(v sql.NullInt64) string {
	if !v.Valid {
		return "-"
	}
	return gp(v.Int64)
}

func ptrGP(v *int64) string {
	if v == nil {
		return "-"
	}
	return gp(*v)
}

func gp(v int64) string {
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	s := strconv.FormatInt(v, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return sign + s
}

func baseline(v float64) string {
	if v <= 0 {
		return "-"
	}
	return gp(int64(math.Round(v)))
}

func emptyZero(v int64) string {
	if v == 0 {
		return "-"
	}
	return gp(v)
}

func durationSeconds(sec int64) string {
	if sec <= 0 {
		return "0s"
	}
	d := time.Duration(sec) * time.Second
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	if d < time.Hour {
		return d.Round(time.Minute).String()
	}
	return d.Round(time.Hour).String()
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "..."
}

func meanInt64(values []int64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += float64(v)
	}
	return sum / float64(len(values))
}

func medianInt64(values []int64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]int64(nil), values...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return float64(cp[mid])
	}
	return float64(cp[mid-1]+cp[mid]) / 2
}

func quantileInt64(values []int64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]int64(nil), values...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	if q <= 0 {
		return float64(cp[0])
	}
	if q >= 1 {
		return float64(cp[len(cp)-1])
	}
	pos := q * float64(len(cp)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return float64(cp[lo])
	}
	frac := pos - float64(lo)
	return float64(cp[lo])*(1-frac) + float64(cp[hi])*frac
}

func quantileFloat64(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]float64(nil), values...)
	sort.Float64s(cp)
	if q <= 0 {
		return cp[0]
	}
	if q >= 1 {
		return cp[len(cp)-1]
	}
	pos := q * float64(len(cp)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return cp[lo]
	}
	frac := pos - float64(lo)
	return cp[lo]*(1-frac) + cp[hi]*frac
}

func percentileRankFloat64(values []float64, value float64) float64 {
	if len(values) == 0 {
		return 0
	}
	lessOrEqual := 0
	for _, v := range values {
		if v <= value {
			lessOrEqual++
		}
	}
	return float64(lessOrEqual) / float64(len(values))
}

func minFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	out := values[0]
	for _, v := range values[1:] {
		if v < out {
			out = v
		}
	}
	return out
}

func maxFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	out := values[0]
	for _, v := range values[1:] {
		if v > out {
			out = v
		}
	}
	return out
}

func max[T ~int64](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func min[T ~int64](a, b T) T {
	if a < b {
		return a
	}
	return b
}

func int64Ptr(v int64) *int64 {
	return &v
}

package osrsge

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func Main(args []string) error {
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
	case "schema", "describe":
		return a.withDB(func() error { return a.cmdSchema(cmdArgs) })
	case "doctor", "health":
		return a.withDB(func() error { return a.cmdDoctor(cmdArgs) })
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
  schema           Describe local SQLite tables, columns, row counts, sync state
  doctor           Check cache freshness, API reachability, and setup warnings
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
  osrs-ge doctor
  osrs-ge schema --json
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
	return defaultUserAgentValue
}

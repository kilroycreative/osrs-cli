package osrsge

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

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
		Name:        "pp-osrs-ge-agent-workbench",
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
			{
				Name:        "schema",
				Intent:      "discover local cache shape before custom queries",
				Command:     "osrs-ge schema --json",
				Returns:     []string{"tables", "columns", "row counts", "sync state"},
				GoodFor:     []string{"agent planning", "custom SQL generation", "debugging available data"},
				Caveats:     []string{"describes the local cache, not every OSRS Wiki API field"},
				Example:     `which cache tables can support this query?`,
				JSONSupport: true,
			},
			{
				Name:        "doctor",
				Intent:      "check cache freshness and API/setup readiness",
				Command:     "osrs-ge doctor --json",
				Returns:     []string{"cache counts", "interval freshness", "API check", "setup warnings"},
				GoodFor:     []string{"preflight checks", "stale-data diagnosis", "agent run readiness"},
				Caveats:     []string{"API check is live unless --no-api is supplied"},
				Example:     `is this cache fresh enough to trust?`,
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

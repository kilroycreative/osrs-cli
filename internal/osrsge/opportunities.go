package osrsge

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

func (a *app) oneOpportunity(id int64, taxRate float64, taxCap int64) (opportunity, error) {
	rows, err := a.loadCandidates("1h")
	if err != nil {
		return opportunity{}, err
	}
	for _, row := range rows {
		if row.ID == id {
			opp := computeOpportunity(row, 1, taxRate, taxCap, time.Now().Unix())
			opportunityScore(&opp, defaultScoreMinVolume, defaultScoreMaxAge)
			return opp, nil
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
	Capital        int64
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
		opportunityScore(&opp, f.MinVolume, f.MaxAge)
		opps = append(opps, opp)
	}
	sortOpportunities(opps, f.SortBy)
	if f.Limit > 0 && len(opps) > f.Limit {
		opps = opps[:f.Limit]
	}
	for i := range opps {
		opps[i].Rank = i + 1
		if f.Capital > 0 && opps[i].CapitalRequired > 0 {
			opps[i].BelowScale = opps[i].CapitalRequired > f.Capital
		}
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
	capitalText := fs.String("capital", "", "available capital, e.g. 20m; items needing more to fill a window are flagged below-scale")
	preTax := fs.Bool("pre-tax", false, "skip GE tax in margin/ROI calculations")
	explain := fs.Bool("explain", false, "print raw inputs and the full computation for each item")
	jsonOut := fs.Bool("json", false, "emit JSON")
	csvOut := fs.Bool("csv", false, "emit CSV")
	noSync := fs.Bool("no-sync", false, "do not refresh latest/interval cache")
	_, err := parseCommandFlags(fs, args, map[string]bool{
		"limit": true, "interval": true, "min-volume": true, "min-margin": true,
		"min-roi": true, "volume-baseline": true, "members": true, "sort": true,
		"max-age": true, "max-spread-pct": true, "tax-rate": true, "tax-cap": true,
		"capital": true,
	})
	if err != nil {
		return err
	}
	effectiveTaxRate := *taxRate
	if *preTax {
		effectiveTaxRate = 0
	}
	capital := int64(0)
	if strings.TrimSpace(*capitalText) != "" {
		capital, err = parseGP(*capitalText)
		if err != nil {
			return fmt.Errorf("--capital: %w", err)
		}
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
		TaxRate:        effectiveTaxRate,
		TaxCap:         *taxCap,
		Capital:        capital,
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
	if *explain {
		explainOpportunities(os.Stdout, opps, effectiveTaxRate, *taxCap, capital)
		return nil
	}
	return writeOpportunitiesTable(os.Stdout, opps, opportunityRender{Capital: capital, PreTax: *preTax})
}

// geWindowsPerDay is the number of 4-hour buy-limit windows in a day. It is
// used only to derive the theoretical-maximum gp_per_day_max figure.
const geWindowsPerDay = 6

func computeOpportunity(row candidateRow, baseline float64, taxRate float64, taxCap int64, now int64) opportunity {
	high := row.High.Int64
	low := row.Low.Int64
	volume := row.HighVolume + row.LowVolume
	tax := geTaxForItem(row.ID, high, taxRate, taxCap)
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
	opp.TaxDragPerUnit = tax
	if limit > 0 {
		opp.LimitProfit = net * limit
		opp.TaxDragPerLimit = tax * limit
		opp.CapitalRequired = limit * low
	}
	// gp_per_limit (LimitProfit) is one 4h buy-limit window of post-tax profit.
	opp.GPPer4h = opp.LimitProfit
	opp.GPPerDayMax = opp.LimitProfit * geWindowsPerDay
	effectiveTaxRate := taxRate
	if isTaxExempt(row.ID) {
		effectiveTaxRate = 0
	}
	opp.BreakEvenSell = breakEvenSell(low, effectiveTaxRate)
	if opp.BreakEvenSell > 0 && high < opp.BreakEvenSell {
		opp.Invalidated = true
		opp.InvalidationReason = fmt.Sprintf(
			"sell zone %s gp is below break-even %s gp; the sale cannot recover the %s gp buy cost after tax",
			gp(high), gp(opp.BreakEvenSell), gp(low))
	}
	return opp
}

const (
	defaultScoreMinVolume = 100
	defaultScoreMaxAge    = 2 * time.Hour
)

// opportunityScore computes the ranking score for an opportunity and stores
// it on opp together with its trend / liquidity / scale decomposition.
// score = trend * liquidity * scale.
func opportunityScore(opp *opportunity, minVolume int64, maxAge time.Duration) {
	opp.Score, opp.ScoreTrend, opp.ScoreLiquidity, opp.ScoreScale = 0, 0, 0, 0
	if opp.NetMargin <= 0 || opp.Low <= 0 {
		return
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
	// trend: profit edge weighted by ROI and how fresh the prices are.
	trend := float64(opp.NetMargin) * math.Max(opp.ROI, 0.0001) * freshness
	// liquidity: traded depth and how far volume beats its baseline.
	liq := math.Log10(float64(opp.Volume)+10) * volBoost * liquidity
	// scale: capital the buy limit lets you deploy per refresh window.
	// A missing buy limit yields zero here on purpose — never fabricated.
	scale := math.Log1p(float64(opp.BuyLimit))
	opp.ScoreTrend = trend
	opp.ScoreLiquidity = liq
	opp.ScoreScale = scale
	opp.Score = trend * liq * scale
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

// opportunityRender carries flag-driven context into the table renderer.
type opportunityRender struct {
	Capital int64
	PreTax  bool
}

func writeOpportunitiesTable(w io.Writer, opps []opportunity, render opportunityRender) error {
	if len(opps) == 0 {
		fmt.Fprintln(w, "No opportunities passed the filters.")
		return nil
	}
	if render.PreTax {
		fmt.Fprintln(w, "[--pre-tax] margin, ROI, and break-even below exclude GE tax.")
		fmt.Fprintln(w)
	}
	reachable := 0
	for _, opp := range opps {
		members := "free-to-play"
		if opp.Members {
			members = "members"
		}
		capStr, perLimitStr, perDayStr := "unknown (no buy limit)", "unknown (no buy limit)", "unknown (no buy limit)"
		if opp.BuyLimit > 0 {
			capStr = gp(opp.CapitalRequired) + " gp"
			perLimitStr = gp(opp.GPPer4h) + " gp"
			perDayStr = gp(opp.GPPerDayMax) + " gp"
		}
		fmt.Fprintf(w, "#%d  %s (id %d) - %s\n", opp.Rank, opp.Name, opp.ID, members)
		fmt.Fprintf(w, "    low %s -> high %s    net %s gp    roi %.2f%%    vol %s (%.2fx base)    age %s\n",
			gp(opp.Low), gp(opp.High), gp(opp.NetMargin), opp.ROI*100, gp(opp.Volume), opp.VolumeRatio,
			durationSeconds(max(opp.HighAgeSeconds, opp.LowAgeSeconds)))
		fmt.Fprintf(w, "    break-even sell %s gp    capital required %s    buy limit %s\n",
			gp(opp.BreakEvenSell), capStr, emptyZero(opp.BuyLimit))
		if opp.TaxDragPerUnit > 0 {
			fmt.Fprintf(w, "    tax drag -%s/unit · -%s/limit\n", gp(opp.TaxDragPerUnit), gp(opp.TaxDragPerLimit))
		} else {
			fmt.Fprintln(w, "    tax drag none (exempt or 100 gp or below)")
		}
		fmt.Fprintf(w, "    gp/limit %s    gp/day(max) %s (theoretical)\n", perLimitStr, perDayStr)
		fmt.Fprintf(w, "    score %.1f = trend %.2f × liq %.2f × scale %.2f\n",
			opp.Score, opp.ScoreTrend, opp.ScoreLiquidity, opp.ScoreScale)
		if opp.Invalidated {
			fmt.Fprintf(w, "    INVALIDATED: %s\n", opp.InvalidationReason)
		}
		if render.Capital > 0 {
			switch {
			case opp.BuyLimit <= 0:
				fmt.Fprintln(w, "    SCALE UNKNOWN: missing buy limit")
			case opp.BelowScale:
				fmt.Fprintf(w, "    BELOW SCALE: one window needs %s gp, capital is %s gp\n",
					gp(opp.CapitalRequired), gp(render.Capital))
			default:
				reachable++
			}
		}
		fmt.Fprintln(w)
	}
	if render.Capital > 0 {
		fmt.Fprintf(w, "Reachable: %d/%d within %s gp capital.\n", reachable, len(opps), gp(render.Capital))
	}
	fmt.Fprintln(w, "gp/day(max) is a theoretical ceiling: 6 buy-limit windows per day, each fully bought and sold at current prices with no slippage.")
	return nil
}

func writeOpportunitiesCSV(w io.Writer, opps []opportunity) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()
	if err := cw.Write([]string{
		"rank", "id", "name", "members", "low", "high", "tax", "net_margin", "roi",
		"volume", "baseline_volume", "volume_ratio", "buy_limit", "limit_profit",
		"high_age_seconds", "low_age_seconds", "score", "score_trend",
		"score_liquidity", "score_scale", "break_even_sell", "tax_drag_per_unit",
		"tax_drag_per_limit", "capital_required", "gp_per_4h", "gp_per_day_max",
		"invalidated", "invalidation_reason",
	}); err != nil {
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
			fmt.Sprintf("%.4f", opp.ScoreTrend),
			fmt.Sprintf("%.4f", opp.ScoreLiquidity),
			fmt.Sprintf("%.4f", opp.ScoreScale),
			strconv.FormatInt(opp.BreakEvenSell, 10),
			strconv.FormatInt(opp.TaxDragPerUnit, 10),
			strconv.FormatInt(opp.TaxDragPerLimit, 10),
			strconv.FormatInt(opp.CapitalRequired, 10),
			strconv.FormatInt(opp.GPPer4h, 10),
			strconv.FormatInt(opp.GPPerDayMax, 10),
			strconv.FormatBool(opp.Invalidated),
			opp.InvalidationReason,
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

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
	"strings"
	"text/tabwriter"
	"time"
)

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

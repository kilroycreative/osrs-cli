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
	"sort"
	"text/tabwriter"
	"time"
)

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
	netToVWAP := targetVWAP - currentGP - geTaxForItem(candidate.Item.ID, targetVWAP, opts.TaxRate, opts.TaxCap)
	netToTop := targetTop - currentGP - geTaxForItem(candidate.Item.ID, targetTop, opts.TaxRate, opts.TaxCap)
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

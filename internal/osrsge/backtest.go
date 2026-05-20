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
	"strings"
	"text/tabwriter"
)

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

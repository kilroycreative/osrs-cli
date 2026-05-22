package osrsge

import (
	"fmt"
	"io"
	"math"
)

// explainOpportunities prints the full computation chain for each opportunity.
func explainOpportunities(w io.Writer, opps []opportunity, taxRate float64, taxCap, capital int64) {
	for i, opp := range opps {
		if i > 0 {
			fmt.Fprintln(w)
		}
		explainOpportunity(w, opp, taxRate, taxCap, capital)
	}
}

// explainOpportunity prints raw inputs, intermediate steps, and final numbers
// for one opportunity so the user can audit every figure by hand.
func explainOpportunity(w io.Writer, opp opportunity, taxRate float64, taxCap, capital int64) {
	exempt := isTaxExempt(opp.ID)
	gross := opp.High - opp.Low

	fmt.Fprintf(w, "=== %s (id %d) ===\n", opp.Name, opp.ID)

	fmt.Fprintln(w, "Raw inputs:")
	fmt.Fprintf(w, "  low  (buy zone, live)  = %s gp\n", gp(opp.Low))
	fmt.Fprintf(w, "  high (sell zone, live) = %s gp\n", gp(opp.High))
	if opp.BuyLimit > 0 {
		fmt.Fprintf(w, "  buy limit              = %s\n", gp(opp.BuyLimit))
	} else {
		fmt.Fprintln(w, "  buy limit              = MISSING (per-limit figures unavailable)")
	}
	fmt.Fprintf(w, "  volume                 = %s\n", gp(opp.Volume))
	fmt.Fprintf(w, "  tax rate / cap         = %.2f%% / %s gp\n", taxRate*100, gp(taxCap))

	fmt.Fprintln(w, "Tax and margin:")
	fmt.Fprintf(w, "  gross margin = high - low = %s - %s = %s gp\n", gp(opp.High), gp(opp.Low), gp(gross))
	switch {
	case exempt:
		fmt.Fprintln(w, "  tax per unit = 0 gp (item is tax-exempt)")
	case opp.High <= 100:
		fmt.Fprintln(w, "  tax per unit = 0 gp (sell price 100 gp or below: untaxed)")
	case taxRate <= 0:
		fmt.Fprintln(w, "  tax per unit = 0 gp (--pre-tax: tax skipped)")
	default:
		uncapped := int64(math.Floor(float64(opp.High) * taxRate))
		if taxCap > 0 && uncapped > taxCap {
			fmt.Fprintf(w, "  tax per unit = min(floor(high * rate), cap) = min(%s, %s) = %s gp\n",
				gp(uncapped), gp(taxCap), gp(opp.Tax))
		} else {
			fmt.Fprintf(w, "  tax per unit = floor(high * rate) = floor(%s * %.4f) = %s gp\n",
				gp(opp.High), taxRate, gp(opp.Tax))
		}
	}
	fmt.Fprintf(w, "  net margin   = gross - tax = %s - %s = %s gp\n", gp(gross), gp(opp.Tax), gp(opp.NetMargin))
	if opp.Low > 0 {
		fmt.Fprintf(w, "  ROI          = net / low = %s / %s = %.4f%%\n", gp(opp.NetMargin), gp(opp.Low), opp.ROI*100)
	}

	fmt.Fprintln(w, "Break-even and invalidation:")
	rate := taxRate
	if exempt {
		rate = 0
	}
	if rate > 0 && rate < 1 {
		fmt.Fprintf(w, "  break-even sell = ceil(low / (1 - rate)) = ceil(%s / %.4f) = %s gp\n",
			gp(opp.Low), 1-rate, gp(opp.BreakEvenSell))
	} else {
		fmt.Fprintf(w, "  break-even sell = low = %s gp (no tax applies)\n", gp(opp.BreakEvenSell))
	}
	if opp.Invalidated {
		fmt.Fprintf(w, "  invalidated     = YES: %s\n", opp.InvalidationReason)
	} else {
		fmt.Fprintf(w, "  invalidated     = no (high %s >= break-even %s)\n", gp(opp.High), gp(opp.BreakEvenSell))
	}

	fmt.Fprintln(w, "Scale (per buy-limit window):")
	if opp.BuyLimit > 0 {
		fmt.Fprintf(w, "  capital required = buy limit * low = %s * %s = %s gp\n",
			gp(opp.BuyLimit), gp(opp.Low), gp(opp.CapitalRequired))
		fmt.Fprintf(w, "  gp / limit (4h)  = net * buy limit = %s * %s = %s gp\n",
			gp(opp.NetMargin), gp(opp.BuyLimit), gp(opp.GPPer4h))
		fmt.Fprintf(w, "  gp / day (max)   = gp/4h * %d windows = %s gp  [THEORETICAL CEILING: assumes every window fully bought and sold, no slippage]\n",
			geWindowsPerDay, gp(opp.GPPerDayMax))
		if capital > 0 {
			if opp.CapitalRequired > capital {
				fmt.Fprintf(w, "  vs --capital %s = BELOW SCALE (one window needs %s gp)\n",
					gp(capital), gp(opp.CapitalRequired))
			} else {
				fmt.Fprintf(w, "  vs --capital %s = reachable\n", gp(capital))
			}
		}
	} else {
		fmt.Fprintln(w, "  capital required = unknown (missing buy limit)")
		fmt.Fprintln(w, "  gp / limit (4h)  = unknown (missing buy limit)")
		fmt.Fprintln(w, "  gp / day (max)   = unknown (missing buy limit)")
	}

	fmt.Fprintln(w, "Score (trend x liquidity x scale):")
	fmt.Fprintf(w, "  trend     = net * max(ROI, 0.0001) * freshness   = %.4f\n", opp.ScoreTrend)
	fmt.Fprintf(w, "  liquidity = log10(vol+10) * volBoost * liquidity = %.4f\n", opp.ScoreLiquidity)
	fmt.Fprintf(w, "  scale     = log1p(buy limit)                     = %.4f\n", opp.ScoreScale)
	fmt.Fprintf(w, "  score     = trend * liquidity * scale            = %.4f\n", opp.Score)
}

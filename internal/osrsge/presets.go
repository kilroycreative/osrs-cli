package osrsge

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
)

// strategyPreset is a named, reusable GE research strategy. Scale tags:
// micro (<100k gp), mid (100k-10M gp), macro (>10M gp).
type strategyPreset struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Command     string `json:"command"`
	Capital     string `json:"capital"`
	Scale       string `json:"scale"`
}

// strategyPresets returns the built-in library of GE trading strategies.
// Item names and command shapes are illustrative starting points — prices,
// buy limits, and volumes are always read live from the cache.
func strategyPresets() []strategyPreset {
	return []strategyPreset{
		// --- Margin flips ---
		{
			Name:        "Emerald amulet flip",
			Category:    "Margin flips",
			Description: "Flip the buy/sell spread on a steady mid-volume jewellery line.",
			Prompt:      "Show the buy and sell prices for emerald amulets, subtract GE tax from the sell side, and report net margin per unit and per buy limit. Flag it if the sell zone has fallen below break-even.",
			Command:     `osrs-ge price "emerald amulet" --capital 1m`,
			Capital:     "~50k-1m",
			Scale:       "micro",
		},
		{
			Name:        "Prayer potion(4) flip",
			Category:    "Margin flips",
			Description: "Flip one of the highest-volume consumables in the game.",
			Prompt:      "Report prayer potion(4) net margin, ROI, tax drag and gp per buy limit. It trades constantly, so prioritise a tight spread and fast fills over a large per-unit margin.",
			Command:     `osrs-ge price "prayer potion(4)"`,
			Capital:     "~1m-10m",
			Scale:       "mid",
		},
		{
			Name:        "Super combat potion(4) flip",
			Category:    "Margin flips",
			Description: "Flip a high-value combat consumable with deep volume.",
			Prompt:      "Report super combat potion(4) net margin and the capital required to fill one buy limit, and tell me whether my capital covers a full 4h window.",
			Command:     `osrs-ge price "super combat potion(4)" --capital 20m`,
			Capital:     "~5m-30m",
			Scale:       "mid",
		},
		{
			Name:        "Anglerfish flip",
			Category:    "Margin flips",
			Description: "Flip the staple high-end food item.",
			Prompt:      "Give me anglerfish margin and volume from the members opportunities scan, and confirm volume is comfortably above its baseline before I commit capital.",
			Command:     `osrs-ge opportunities --members members --min-volume 1000 --sort net-margin`,
			Capital:     "~2m-15m",
			Scale:       "mid",
		},
		// --- Process arbitrage ---
		{
			Name:        "Grimy to clean herbs",
			Category:    "Process arbitrage",
			Description: "Buy grimy herbs, clean them, sell the clean herb for the spread.",
			Prompt:      "Compare the buy price of grimy ranarr weed with the sell price of clean ranarr weed. Net margin = clean sell - grimy buy - tax on the clean sell. Tell me if cleaning is currently profitable per buy limit.",
			Command:     `osrs-ge price "grimy ranarr weed" && osrs-ge price "ranarr weed"`,
			Capital:     "~500k-5m",
			Scale:       "mid",
		},
		{
			Name:        "Dragon scale dust",
			Category:    "Process arbitrage",
			Description: "Buy blue dragon scales, grind them to dust, sell the dust.",
			Prompt:      "Compare blue dragon scale buy price with dragon scale dust sell price after tax. Scales grind 1:1 to dust. Report per-unit and per-limit margin.",
			Command:     `osrs-ge price "blue dragon scale" && osrs-ge price "dragon scale dust"`,
			Capital:     "~200k-2m",
			Scale:       "mid",
		},
		{
			Name:        "Dose decanting",
			Category:    "Process arbitrage",
			Description: "Buy 3-dose potions, decant to 4-dose, sell the 4-dose.",
			Prompt:      "For a chosen potion, compare 4/3 of the (3)-dose buy price against the (4)-dose sell price after tax. Decanting moves dose value with no material cost. Tell me which potions decant profitably right now.",
			Command:     `osrs-ge price "prayer potion(3)" && osrs-ge price "prayer potion(4)"`,
			Capital:     "~1m-10m",
			Scale:       "mid",
		},
		{
			Name:        "Unfinished potions",
			Category:    "Process arbitrage",
			Description: "Combine clean herbs with vials of water into unfinished potions.",
			Prompt:      "Compare the cost of one clean herb plus one vial of water with the unfinished-potion sell price after tax. Report per-unit margin and how many you can make per herb buy limit.",
			Command:     `osrs-ge price "ranarr weed" && osrs-ge price "ranarr potion (unf)"`,
			Capital:     "~500k-5m",
			Scale:       "mid",
		},
		// --- Swing breakouts ---
		{
			Name:        "Current breakout scanner",
			Category:    "Swing breakouts",
			Description: "Scan for items that dumped then rebounded — buy the dip, sell the bounce.",
			Prompt:      "Run the pattern scanner for dump/rebound setups and rank them by score. I want recent, liquid setups where the rebound completed within a few hours.",
			Command:     `osrs-ge patterns --cash 40m --limit 15`,
			Capital:     "~10m-50m",
			Scale:       "macro",
		},
		{
			Name:        "90-day gap closers",
			Category:    "Swing breakouts",
			Description: "Find items sitting near the bottom of their own 90-day range.",
			Prompt:      "Scan for items trading in the bottom decile of their 90-day VWAP range with consistent volume and a history of rebounding. Rank by rebound reliability.",
			Command:     `osrs-ge range-bottom --cash 40m --days 90 --step 6h`,
			Capital:     "~10m-50m",
			Scale:       "macro",
		},
		// --- Buy-limit grinds ---
		{
			Name:        "Dragon bones ladder",
			Category:    "Buy-limit grinds",
			Description: "Refill a full dragon bones buy limit every 4h window.",
			Prompt:      "Show dragon bones margin, capital required per buy limit, gp per 4h window and the theoretical gp per day. I plan to refill the limit every window.",
			Command:     `osrs-ge price "dragon bones" --capital 20m`,
			Capital:     "~5m-30m",
			Scale:       "mid",
		},
		{
			Name:        "Magic & yew log grind",
			Category:    "Buy-limit grinds",
			Description: "Grind buy limits on high-demand fletching and firemaking logs.",
			Prompt:      "Compare magic logs and yew logs on net margin, gp per buy limit and capital required. Tell me which gives more gp per 4h window for my capital.",
			Command:     `osrs-ge opportunities --min-volume 1000 --sort limit-profit --capital 20m`,
			Capital:     "~2m-20m",
			Scale:       "mid",
		},
		// --- Event-driven ---
		{
			Name:        "DMM / Leagues demand spike",
			Category:    "Event-driven",
			Description: "Position ahead of seasonal-mode demand spikes.",
			Prompt:      "Around a Deadman or Leagues launch, supplies and gear spike in demand. Scan movers for items with volume far above baseline and a rising mid price.",
			Command:     `osrs-ge movers --interval 1h --limit 25`,
			Capital:     "~10m+",
			Scale:       "macro",
		},
		{
			Name:        "Bond arbitrage",
			Category:    "Event-driven",
			Description: "Track the Old school bond spread — note bonds are GE tax-exempt.",
			Prompt:      "Report the Old school bond buy/sell spread. Bonds are exempt from GE tax, so the entire spread is margin. Flag a wide spread worth flipping.",
			Command:     `osrs-ge price "old school bond"`,
			Capital:     "~10m+",
			Scale:       "macro",
		},
	}
}

func (a *app) cmdPresets(args []string) error {
	fs := flag.NewFlagSet("presets", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	category := fs.String("category", "", "filter by category (margin, process, swing, grind, event)")
	jsonOut := fs.Bool("json", false, "emit JSON")
	positionals, err := parseCommandFlags(fs, args, map[string]bool{"category": true})
	if err != nil {
		return err
	}
	presets := strategyPresets()
	if *category != "" {
		needle := strings.ToLower(strings.TrimSpace(*category))
		var matched []strategyPreset
		for _, p := range presets {
			if strings.Contains(strings.ToLower(p.Category), needle) {
				matched = append(matched, p)
			}
		}
		presets = matched
	}
	query := strings.TrimSpace(strings.Join(positionals, " "))
	if query != "" {
		needle := strings.ToLower(query)
		var matched []strategyPreset
		for _, p := range presets {
			if strings.Contains(strings.ToLower(p.Name), needle) {
				matched = append(matched, p)
			}
		}
		presets = matched
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(presets)
	}
	if len(presets) == 0 {
		fmt.Println("No presets matched.")
		return nil
	}
	if query != "" && len(presets) == 1 {
		writePresetDetail(os.Stdout, presets[0])
		return nil
	}
	writePresetList(os.Stdout, presets)
	return nil
}

func writePresetList(w io.Writer, presets []strategyPreset) {
	fmt.Fprintln(w, "OSRS GE strategy presets")
	fmt.Fprintln(w, "Scale tags: micro <100k gp · mid 100k-10M gp · macro >10M gp")
	currentCategory := ""
	for _, p := range presets {
		if p.Category != currentCategory {
			currentCategory = p.Category
			fmt.Fprintf(w, "\n%s\n", strings.ToUpper(p.Category))
		}
		fmt.Fprintf(w, "  %s  [%s]  capital %s\n", p.Name, p.Scale, p.Capital)
		fmt.Fprintf(w, "    %s\n", p.Description)
		fmt.Fprintf(w, "    $ %s\n", p.Command)
	}
	fmt.Fprintln(w, "\nRun 'osrs-ge presets \"<name>\"' for the full prompt of one strategy.")
}

func writePresetDetail(w io.Writer, p strategyPreset) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Name\t%s\n", p.Name)
	fmt.Fprintf(tw, "Category\t%s\n", p.Category)
	fmt.Fprintf(tw, "Scale\t%s\n", p.Scale)
	fmt.Fprintf(tw, "Capital\t%s\n", p.Capital)
	fmt.Fprintf(tw, "Description\t%s\n", p.Description)
	fmt.Fprintf(tw, "Command\t%s\n", p.Command)
	_ = tw.Flush()
	fmt.Fprintf(w, "\nPrompt:\n  %s\n", p.Prompt)
}

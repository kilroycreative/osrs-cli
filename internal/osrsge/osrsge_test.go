package osrsge

import (
	"database/sql"
	"flag"
	"math"
	"strings"
	"testing"
	"time"
)

func TestGETax(t *testing.T) {
	tests := []struct {
		name string
		sell int64
		want int64
	}{
		{name: "100 gp threshold is untaxed", sell: 100, want: 0},
		{name: "101 gp clears threshold and floors", sell: 101, want: 2},
		{name: "two percent floors", sell: 199, want: 3},
		{name: "just below cap", sell: 4_999_999, want: 99_999},
		{name: "exactly cap input", sell: 5_000_000, want: 100_000},
		{name: "cap clamps large sale", sell: 250_000_001, want: 5_000_000},
		{name: "billion clamps to cap", sell: 1_000_000_000, want: 5_000_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := geTax(tt.sell, 0.02, 5_000_000); got != tt.want {
				t.Fatalf("geTax(%d) = %d, want %d", tt.sell, got, tt.want)
			}
		})
	}
}

func TestGETaxForItemExempt(t *testing.T) {
	// Old school bond (id 13190) is on the exempt list: never taxed.
	if got := geTaxForItem(13190, 8_000_000, 0.02, 5_000_000); got != 0 {
		t.Fatalf("geTaxForItem(bond) = %d, want 0", got)
	}
	// A non-exempt item is taxed normally.
	if got := geTaxForItem(2, 8_000_000, 0.02, 5_000_000); got != 160_000 {
		t.Fatalf("geTaxForItem(non-exempt) = %d, want 160000", got)
	}
}

func TestParseCommandFlagsAllowsTrailingFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	noSync := fs.Bool("no-sync", false, "")
	limit := fs.Int("limit", 20, "")

	pos, err := parseCommandFlags(fs, []string{"abyssal", "whip", "--no-sync", "--limit", "5"}, map[string]bool{"limit": true})
	if err != nil {
		t.Fatal(err)
	}
	if !*noSync {
		t.Fatal("expected no-sync")
	}
	if *limit != 5 {
		t.Fatalf("limit = %d, want 5", *limit)
	}
	if got := len(pos); got != 2 || pos[0] != "abyssal" || pos[1] != "whip" {
		t.Fatalf("positionals = %#v", pos)
	}
}

func TestParseGP(t *testing.T) {
	tests := map[string]int64{
		"500":   500,
		"1,250": 1250,
		"2.5k":  2500,
		"20m":   20_000_000,
		"1.2b":  1_200_000_000,
	}
	for input, want := range tests {
		got, err := parseGP(input)
		if err != nil {
			t.Fatalf("parseGP(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("parseGP(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestBacktestSpread(t *testing.T) {
	low := int64(100)
	high := int64(130)
	item := itemRecord{ID: 1, Name: "Example", BuyLimit: validInt(10)}
	summary := backtestSpread(item, []timeseriesPoint{{
		AvgLowPrice:  &low,
		AvgHighPrice: &high,
		LowVolume:    100,
		HighVolume:   100,
	}}, "1h", 10_000, 1, 0.01, 1, 0.02, 5_000_000)
	if summary.Signals != 1 {
		t.Fatalf("signals = %d, want 1", summary.Signals)
	}
	if summary.TotalEdge != 280 {
		t.Fatalf("edge = %d, want 280", summary.TotalEdge)
	}
}

func TestSchemaReportIncludesCoreTables(t *testing.T) {
	a, closeDB := testApp(t)
	defer closeDB()

	report, err := a.buildSchemaReport("")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tables) == 0 {
		t.Fatal("expected schema tables")
	}
	if !schemaHasColumn(report, "items", "buy_limit") {
		t.Fatalf("schema did not include items.buy_limit: %#v", report.Tables)
	}
}

func TestDoctorReportNoAPIWarnsOnEmptyCache(t *testing.T) {
	a, closeDB := testApp(t)
	defer closeDB()

	report, err := a.buildDoctorReport(false, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if report.APICheck.Checked {
		t.Fatal("expected API check to be skipped")
	}
	if report.Counts["items"] != 0 {
		t.Fatalf("items count = %d, want 0", report.Counts["items"])
	}
	if !warningsContain(report.Warnings, "item mapping cache is empty") {
		t.Fatalf("expected empty-cache warning, got %#v", report.Warnings)
	}
}

func TestDoctorReportDoesNotWarnOnCustomUserAgent(t *testing.T) {
	a, closeDB := testApp(t)
	defer closeDB()
	a.userAgent = "osrs-ge-test/0.1 (+contact@example.com)"

	report, err := a.buildDoctorReport(false, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if report.DefaultUserAgent {
		t.Fatal("expected custom user agent to be detected")
	}
	if warningsContain(report.Warnings, "OSRS_GE_USER_AGENT") {
		t.Fatalf("did not expect user-agent warning, got %#v", report.Warnings)
	}
}

func validInt(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: true}
}

func testApp(t *testing.T) (*app, func()) {
	t.Helper()
	path := t.TempDir() + "/test.sqlite"
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{
		dbPath:    path,
		userAgent: defaultUserAgent(),
		db:        db,
		client: &wikiClient{
			baseURL: apiBaseURL,
		},
	}
	return a, func() { _ = db.Close() }
}

func schemaHasColumn(report schemaReport, tableName, columnName string) bool {
	for _, table := range report.Tables {
		if table.Name != tableName {
			continue
		}
		for _, column := range table.Columns {
			if column.Name == columnName {
				return true
			}
		}
	}
	return false
}

func warningsContain(warnings []string, want string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, want) {
			return true
		}
	}
	return false
}

// withinTolerance reports whether got is within 1% of the hand-computed want.
func withinTolerance(got, want float64) bool {
	if want == 0 {
		return math.Abs(got) < 1e-9
	}
	return math.Abs(got-want)/math.Abs(want) <= 0.01
}

// TestDataIntegrityReferenceValues locks the opportunity math against
// hand-computed reference values for three representative items. Every CLI
// figure flows from computeOpportunity, so asserting it here guards the
// numbers the CLI prints. References assume the default 2% tax / 5M cap.
func TestDataIntegrityReferenceValues(t *testing.T) {
	const (
		taxRate = 0.02
		taxCap  = int64(5_000_000)
	)
	now := int64(1_700_000_000)

	type fixture struct {
		name             string
		id               int64
		low, high        int64
		buyLimit         int64
		volume           int64
		wantTax          int64   // floor(high * 0.02)
		wantNetMargin    int64   // high - low - tax
		wantROI          float64 // net / low
		wantBreakEven    int64   // ceil(low / 0.98)
		wantTaxDragLimit int64   // tax * buy limit
		wantCapital      int64   // buy limit * low
		wantGPPerLimit   int64   // net * buy limit
		wantGPPerDayMax  int64   // gp/limit * 6
		wantInvalidated  bool    // high < break-even
	}

	fixtures := []fixture{
		{
			// Dragon bones: tax floor(2520*.02)=50, net 120-50=70,
			// roi 70/2400, break-even ceil(2400/.98)=2449.
			name: "Dragon bones", id: 536, low: 2400, high: 2520,
			buyLimit: 7500, volume: 1_200_000,
			wantTax: 50, wantNetMargin: 70, wantROI: 70.0 / 2400.0,
			wantBreakEven: 2449, wantTaxDragLimit: 375_000,
			wantCapital: 18_000_000, wantGPPerLimit: 525_000,
			wantGPPerDayMax: 3_150_000, wantInvalidated: false,
		},
		{
			// Nature rune: tax floor(105*.02)=2, net 10-2=8,
			// roi 8/95, break-even ceil(95/.98)=97.
			name: "Nature rune", id: 561, low: 95, high: 105,
			buyLimit: 25_000, volume: 4_000_000,
			wantTax: 2, wantNetMargin: 8, wantROI: 8.0 / 95.0,
			wantBreakEven: 97, wantTaxDragLimit: 50_000,
			wantCapital: 2_375_000, wantGPPerLimit: 200_000,
			wantGPPerDayMax: 1_200_000, wantInvalidated: false,
		},
		{
			// Magic logs: tax floor(1100*.02)=22, net 100-22=78,
			// roi 78/1000, break-even ceil(1000/.98)=1021.
			name: "Magic logs", id: 384, low: 1000, high: 1100,
			buyLimit: 15_000, volume: 900_000,
			wantTax: 22, wantNetMargin: 78, wantROI: 78.0 / 1000.0,
			wantBreakEven: 1021, wantTaxDragLimit: 330_000,
			wantCapital: 15_000_000, wantGPPerLimit: 1_170_000,
			wantGPPerDayMax: 7_020_000, wantInvalidated: false,
		},
		{
			// Magic logs at a sell zone below break-even: tax
			// floor(1010*.02)=20, net 10-20=-10, invalidated.
			name: "Magic logs (invalidated)", id: 384, low: 1000, high: 1010,
			buyLimit: 15_000, volume: 900_000,
			wantTax: 20, wantNetMargin: -10, wantROI: -10.0 / 1000.0,
			wantBreakEven: 1021, wantTaxDragLimit: 300_000,
			wantCapital: 15_000_000, wantGPPerLimit: -150_000,
			wantGPPerDayMax: -900_000, wantInvalidated: true,
		},
	}

	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			row := candidateRow{
				ID:         f.id,
				Name:       f.name,
				BuyLimit:   validInt(f.buyLimit),
				High:       validInt(f.high),
				HighTime:   validInt(now),
				Low:        validInt(f.low),
				LowTime:    validInt(now),
				HighVolume: f.volume,
				Timestamp:  now,
			}
			opp := computeOpportunity(row, 1, taxRate, taxCap, now)

			checks := []struct {
				field     string
				got, want float64
			}{
				{"tax", float64(opp.Tax), float64(f.wantTax)},
				{"net_margin", float64(opp.NetMargin), float64(f.wantNetMargin)},
				{"roi", opp.ROI, f.wantROI},
				{"break_even_sell", float64(opp.BreakEvenSell), float64(f.wantBreakEven)},
				{"tax_drag_per_unit", float64(opp.TaxDragPerUnit), float64(f.wantTax)},
				{"tax_drag_per_limit", float64(opp.TaxDragPerLimit), float64(f.wantTaxDragLimit)},
				{"capital_required", float64(opp.CapitalRequired), float64(f.wantCapital)},
				{"gp_per_limit", float64(opp.LimitProfit), float64(f.wantGPPerLimit)},
				{"gp_per_4h", float64(opp.GPPer4h), float64(f.wantGPPerLimit)},
				{"gp_per_day_max", float64(opp.GPPerDayMax), float64(f.wantGPPerDayMax)},
			}
			for _, c := range checks {
				if !withinTolerance(c.got, c.want) {
					t.Errorf("%s = %v, want %v (outside 1%% tolerance)", c.field, c.got, c.want)
				}
			}
			if opp.Invalidated != f.wantInvalidated {
				t.Errorf("invalidated = %v, want %v", opp.Invalidated, f.wantInvalidated)
			}
		})
	}
}

// TestScoreDecompositionMultipliesToScore verifies the score equals the
// product of its trend / liquidity / scale components.
func TestScoreDecompositionMultipliesToScore(t *testing.T) {
	opp := opportunity{
		Name: "Dragon bones", NetMargin: 70, Low: 2400, High: 2520,
		Volume: 1_200_000, BuyLimit: 7500, ROI: 70.0 / 2400.0, VolumeRatio: 1.8,
	}
	opportunityScore(&opp, 100, 2*time.Hour)
	if opp.ScoreTrend <= 0 || opp.ScoreLiquidity <= 0 || opp.ScoreScale <= 0 {
		t.Fatalf("decomposition components must be positive: %#v", opp)
	}
	product := opp.ScoreTrend * opp.ScoreLiquidity * opp.ScoreScale
	if !withinTolerance(opp.Score, product) {
		t.Fatalf("score = %v, trend*liq*scale = %v", opp.Score, product)
	}
}

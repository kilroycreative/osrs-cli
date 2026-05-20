package osrsge

import (
	"database/sql"
	"flag"
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
		{name: "below one coin rounds to zero", sell: 49, want: 0},
		{name: "two percent floors", sell: 199, want: 3},
		{name: "cap", sell: 1_000_000_000, want: 5_000_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := geTax(tt.sell, 0.02, 5_000_000); got != tt.want {
				t.Fatalf("geTax(%d) = %d, want %d", tt.sell, got, tt.want)
			}
		})
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

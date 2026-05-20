package osrsge

import (
	"database/sql"
	"flag"
	"testing"
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

func validInt(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: true}
}

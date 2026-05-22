package osrsge

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

const (
	version               = "0.1.0"
	apiBaseURL            = "https://prices.runescape.wiki/api/v1/osrs"
	defaultUserAgentValue = "pp-osrs-ge/0.1 (+https://printingpress.dev; set OSRS_GE_USER_AGENT for contact)"
)

type app struct {
	dbPath    string
	userAgent string
	client    *wikiClient
	db        *sql.DB
}

type wikiClient struct {
	baseURL   string
	userAgent string
	http      *http.Client
}

type mappingItem struct {
	Examine  string `json:"examine"`
	ID       int64  `json:"id"`
	Members  bool   `json:"members"`
	LowAlch  *int64 `json:"lowalch"`
	Limit    *int64 `json:"limit"`
	Value    *int64 `json:"value"`
	HighAlch *int64 `json:"highalch"`
	Icon     string `json:"icon"`
	Name     string `json:"name"`
}

type latestResponse struct {
	Data map[string]latestPoint `json:"data"`
}

type latestPoint struct {
	High     *int64 `json:"high"`
	HighTime *int64 `json:"highTime"`
	Low      *int64 `json:"low"`
	LowTime  *int64 `json:"lowTime"`
}

type intervalResponse struct {
	Timestamp int64                    `json:"timestamp"`
	Data      map[string]intervalPoint `json:"data"`
}

type intervalPoint struct {
	AvgHighPrice *int64 `json:"avgHighPrice"`
	HighVolume   int64  `json:"highPriceVolume"`
	AvgLowPrice  *int64 `json:"avgLowPrice"`
	LowVolume    int64  `json:"lowPriceVolume"`
}

type timeseriesResponse struct {
	Data []timeseriesPoint `json:"data"`
}

type timeseriesPoint struct {
	Timestamp    int64  `json:"timestamp"`
	AvgHighPrice *int64 `json:"avgHighPrice"`
	AvgLowPrice  *int64 `json:"avgLowPrice"`
	HighVolume   int64  `json:"highPriceVolume"`
	LowVolume    int64  `json:"lowPriceVolume"`
}

type itemRecord struct {
	ID       int64
	Name     string
	Members  bool
	BuyLimit sql.NullInt64
	Value    sql.NullInt64
	HighAlch sql.NullInt64
	LowAlch  sql.NullInt64
	Examine  string
}

type opportunity struct {
	Rank           int     `json:"rank"`
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Members        bool    `json:"members"`
	Low            int64   `json:"low"`
	High           int64   `json:"high"`
	Tax            int64   `json:"tax"`
	GrossMargin    int64   `json:"gross_margin"`
	NetMargin      int64   `json:"net_margin"`
	ROI            float64 `json:"roi"`
	SpreadPct      float64 `json:"spread_pct"`
	Volume         int64   `json:"volume"`
	BaselineVolume float64 `json:"baseline_volume"`
	VolumeRatio    float64 `json:"volume_ratio"`
	BuyLimit       int64   `json:"buy_limit,omitempty"`
	LimitProfit    int64   `json:"limit_profit,omitempty"`
	HighAgeSeconds int64   `json:"high_age_seconds"`
	LowAgeSeconds  int64   `json:"low_age_seconds"`
	Score          float64 `json:"score"`
	ScoreTrend     float64 `json:"score_trend"`
	ScoreLiquidity float64 `json:"score_liquidity"`
	ScoreScale     float64 `json:"score_scale"`
	// BreakEvenSell is the minimum sell price that recovers the buy cost
	// after GE tax: ceil(low / (1 - taxRate)). It is the sole invalidation
	// level — high/low are live prices, not prescribed entry/exit levels.
	BreakEvenSell   int64 `json:"break_even_sell"`
	TaxDragPerUnit  int64 `json:"tax_drag_per_unit"`
	TaxDragPerLimit int64 `json:"tax_drag_per_limit"`
	// CapitalRequired is the gp needed to fill one buy-limit window at the
	// low (buy-zone) price: buy_limit * low.
	CapitalRequired int64 `json:"capital_required"`
	// GPPer4h is the post-tax profit from one full buy-limit window. OSRS
	// buy limits reset every 4 hours, so one limit == one 4h window.
	GPPer4h int64 `json:"gp_per_4h"`
	// GPPerDayMax is a THEORETICAL CEILING only: gp_per_4h times the six
	// 4-hour windows in a day. It assumes every window is fully bought and
	// fully sold at these prices with no slippage — rarely achievable.
	GPPerDayMax int64 `json:"gp_per_day_max"`
	// Invalidated is true when the live sell zone cannot clear break-even.
	Invalidated        bool   `json:"invalidated"`
	InvalidationReason string `json:"invalidation_reason,omitempty"`
}

type watchRule struct {
	ID              int64           `json:"id"`
	ItemID          int64           `json:"item_id"`
	Name            string          `json:"name"`
	Below           sql.NullInt64   `json:"below,omitempty"`
	Above           sql.NullInt64   `json:"above,omitempty"`
	MinNetMargin    sql.NullInt64   `json:"min_net_margin,omitempty"`
	MinROI          sql.NullFloat64 `json:"min_roi,omitempty"`
	MinVolume       sql.NullInt64   `json:"min_volume,omitempty"`
	CooldownSeconds int64           `json:"cooldown_seconds"`
	Enabled         bool            `json:"enabled"`
	Note            string          `json:"note,omitempty"`
	LastTriggeredAt sql.NullInt64   `json:"last_triggered_at,omitempty"`
	CreatedAt       int64           `json:"created_at"`
	UpdatedAt       int64           `json:"updated_at"`
}

type alertHit struct {
	RuleID          int64   `json:"rule_id"`
	ItemID          int64   `json:"item_id"`
	Name            string  `json:"name"`
	Reason          string  `json:"reason"`
	Low             int64   `json:"low"`
	High            int64   `json:"high"`
	NetMargin       int64   `json:"net_margin"`
	ROI             float64 `json:"roi"`
	Volume          int64   `json:"volume"`
	LastTriggeredAt int64   `json:"last_triggered_at,omitempty"`
}

type mover struct {
	Rank              int     `json:"rank"`
	ID                int64   `json:"id"`
	Name              string  `json:"name"`
	Members           bool    `json:"members"`
	PreviousMid       float64 `json:"previous_mid"`
	CurrentMid        float64 `json:"current_mid"`
	PriceChange       float64 `json:"price_change"`
	PriceChangePct    float64 `json:"price_change_pct"`
	PreviousVolume    int64   `json:"previous_volume"`
	CurrentVolume     int64   `json:"current_volume"`
	VolumeChange      int64   `json:"volume_change"`
	VolumeRatio       float64 `json:"volume_ratio"`
	CurrentNetMargin  int64   `json:"current_net_margin"`
	CurrentROI        float64 `json:"current_roi"`
	PreviousTimestamp int64   `json:"previous_timestamp"`
	CurrentTimestamp  int64   `json:"current_timestamp"`
}

type allocationRow struct {
	Rank          int     `json:"rank"`
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Units         int64   `json:"units"`
	Cost          int64   `json:"cost"`
	ExpectedEdge  int64   `json:"expected_edge"`
	NetMargin     int64   `json:"net_margin"`
	ROI           float64 `json:"roi"`
	Volume        int64   `json:"volume"`
	BuyLimit      int64   `json:"buy_limit,omitempty"`
	RemainingCash int64   `json:"remaining_cash"`
}

type backtestSummary struct {
	ItemID        int64   `json:"item_id"`
	Name          string  `json:"name"`
	Step          string  `json:"step"`
	Signals       int     `json:"signals"`
	Windows       int     `json:"windows"`
	TotalEdge     int64   `json:"total_edge"`
	AverageEdge   float64 `json:"average_edge"`
	WinRate       float64 `json:"win_rate"`
	MaxWindowEdge int64   `json:"max_window_edge"`
	MinWindowEdge int64   `json:"min_window_edge"`
	Assumption    string  `json:"assumption"`
}

type patternReport struct {
	Strategy      string       `json:"strategy"`
	Like          string       `json:"like,omitempty"`
	Cash          int64        `json:"cash"`
	Step          string       `json:"step"`
	Days          int          `json:"days"`
	GeneratedAt   string       `json:"generated_at"`
	CandidateRows int          `json:"candidate_rows"`
	Scanned       int          `json:"scanned"`
	Errors        int          `json:"errors"`
	Hits          []patternHit `json:"hits"`
	Notes         []string     `json:"notes,omitempty"`
}

type patternHit struct {
	Rank            int      `json:"rank"`
	ID              int64    `json:"id"`
	Name            string   `json:"name"`
	Members         bool     `json:"members"`
	BuyLimit        int64    `json:"buy_limit,omitempty"`
	Low             int64    `json:"low"`
	High            int64    `json:"high"`
	Tax             int64    `json:"tax"`
	NetMargin       int64    `json:"net_margin"`
	ROI             float64  `json:"roi"`
	Ratio           float64  `json:"ratio"`
	LowVolume       int64    `json:"low_volume"`
	HighVolume      int64    `json:"high_volume"`
	LowTime         int64    `json:"low_time"`
	HighTime        int64    `json:"high_time"`
	LowTimeISO      string   `json:"low_time_iso"`
	HighTimeISO     string   `json:"high_time_iso"`
	ReboundSeconds  int64    `json:"rebound_seconds"`
	PortfolioUnits  int64    `json:"portfolio_units,omitempty"`
	PortfolioCost   int64    `json:"portfolio_cost,omitempty"`
	PortfolioProfit int64    `json:"portfolio_profit,omitempty"`
	CurrentLow      *int64   `json:"current_low,omitempty"`
	CurrentHigh     *int64   `json:"current_high,omitempty"`
	Setup           string   `json:"setup"`
	Score           float64  `json:"score"`
	Rationale       []string `json:"rationale"`
}

type patternCandidate struct {
	Item        itemRecord
	CurrentLow  *int64
	CurrentHigh *int64
}

type patternScanOptions struct {
	Strategy      string
	Cash          int64
	Step          string
	Cutoff        int64
	MaxRebound    time.Duration
	MinLow        int64
	MaxLow        int64
	MinHigh       int64
	MaxHigh       int64
	MinLowVolume  int64
	MinHighVolume int64
	MinRatio      float64
	MaxRatio      float64
	MinNetMargin  int64
	TaxRate       float64
	TaxCap        int64
}

type rangeBottomReport struct {
	Strategy      string           `json:"strategy"`
	QueryIntent   string           `json:"query_intent"`
	Cash          int64            `json:"cash"`
	Step          string           `json:"step"`
	Days          int              `json:"days"`
	GeneratedAt   string           `json:"generated_at"`
	CandidateRows int              `json:"candidate_rows"`
	Scanned       int              `json:"scanned"`
	Errors        int              `json:"errors"`
	Hits          []rangeBottomHit `json:"hits"`
	Notes         []string         `json:"notes,omitempty"`
}

type rangeBottomHit struct {
	Rank                   int      `json:"rank"`
	ID                     int64    `json:"id"`
	Name                   string   `json:"name"`
	Members                bool     `json:"members"`
	BuyLimit               int64    `json:"buy_limit,omitempty"`
	CurrentPrice           float64  `json:"current_price"`
	CurrentLow             *int64   `json:"current_low,omitempty"`
	CurrentHigh            *int64   `json:"current_high,omitempty"`
	VWAP                   float64  `json:"vwap"`
	RangeLow               float64  `json:"range_low"`
	RangeHigh              float64  `json:"range_high"`
	BottomBand             float64  `json:"bottom_band"`
	Percentile             float64  `json:"percentile"`
	DiscountToVWAP         float64  `json:"discount_to_vwap"`
	DiscountToRangeHigh    float64  `json:"discount_to_range_high"`
	MedianVolume           int64    `json:"median_volume"`
	P25Volume              int64    `json:"p25_volume"`
	ActiveBuckets          int      `json:"active_buckets"`
	ObservedBuckets        int      `json:"observed_buckets"`
	ActiveRatio            float64  `json:"active_ratio"`
	ReboundCycles          int      `json:"rebound_cycles"`
	BottomVisits           int      `json:"bottom_visits"`
	ReboundReliability     float64  `json:"rebound_reliability"`
	LastBottomTime         int64    `json:"last_bottom_time,omitempty"`
	LastBottomTimeISO      string   `json:"last_bottom_time_iso,omitempty"`
	LastReboundTime        int64    `json:"last_rebound_time,omitempty"`
	LastReboundTimeISO     string   `json:"last_rebound_time_iso,omitempty"`
	EstimatedNetToVWAP     int64    `json:"estimated_net_to_vwap"`
	EstimatedNetToRangeTop int64    `json:"estimated_net_to_range_top"`
	PortfolioUnits         int64    `json:"portfolio_units,omitempty"`
	PortfolioCost          int64    `json:"portfolio_cost,omitempty"`
	PortfolioProfitToVWAP  int64    `json:"portfolio_profit_to_vwap,omitempty"`
	Setup                  string   `json:"setup"`
	Score                  float64  `json:"score"`
	Rationale              []string `json:"rationale"`
}

type rangeBottomOptions struct {
	Cash             int64
	Step             string
	Cutoff           int64
	MaxPercentile    float64
	BottomPercentile float64
	MinActiveBuckets int
	MinMedianVolume  int64
	MinP25Volume     int64
	MinCycles        int
	ReboundWindow    time.Duration
	TaxRate          float64
	TaxCap           int64
}

type rangeBucket struct {
	timestamp int64
	mid       float64
	volume    int64
}

type agentManifest struct {
	Name         string              `json:"name"`
	Version      string              `json:"version"`
	GeneratedAt  string              `json:"generated_at"`
	DataSources  []string            `json:"data_sources"`
	Principles   []string            `json:"principles"`
	Tools        []agentToolManifest `json:"tools"`
	OutputNotice []string            `json:"output_notice"`
}

type agentToolManifest struct {
	Name        string   `json:"name"`
	Intent      string   `json:"intent"`
	Command     string   `json:"command"`
	Returns     []string `json:"returns"`
	GoodFor     []string `json:"good_for"`
	Caveats     []string `json:"caveats,omitempty"`
	Example     string   `json:"example"`
	JSONSupport bool     `json:"json_support"`
}

type agentRunReport struct {
	Strategy    string             `json:"strategy"`
	Query       string             `json:"query"`
	GeneratedAt string             `json:"generated_at"`
	Spec        agentResearchSpec  `json:"spec"`
	Probes      []agentProbeResult `json:"probes"`
	SummaryRows []agentSummaryRow  `json:"summary_rows"`
	Warnings    []string           `json:"warnings,omitempty"`
	NextSteps   []string           `json:"next_steps,omitempty"`
}

type agentResearchSpec struct {
	Intent        string   `json:"intent"`
	Cash          string   `json:"cash"`
	Horizons      []string `json:"horizons"`
	PrimaryTools  []string `json:"primary_tools"`
	EvidenceTests []string `json:"evidence_tests"`
	Notes         []string `json:"notes,omitempty"`
}

type agentProbeSpec struct {
	Name    string   `json:"name"`
	Intent  string   `json:"intent"`
	Command []string `json:"command"`
}

type agentProbeResult struct {
	Name        string            `json:"name"`
	Intent      string            `json:"intent"`
	Command     []string          `json:"command"`
	OK          bool              `json:"ok"`
	Error       string            `json:"error,omitempty"`
	Artifact    json.RawMessage   `json:"artifact,omitempty"`
	SummaryRows []agentSummaryRow `json:"summary_rows,omitempty"`
}

type agentSummaryRow struct {
	Rank          int     `json:"rank"`
	Probe         string  `json:"probe"`
	Item          string  `json:"item"`
	PrimaryMetric string  `json:"primary_metric"`
	Evidence      string  `json:"evidence"`
	Setup         string  `json:"setup"`
	Score         float64 `json:"score"`
}

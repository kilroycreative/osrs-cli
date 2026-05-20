package osrsge

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

func normalizeSQLValue(v any) any {
	switch t := v.(type) {
	case []byte:
		return string(t)
	default:
		return t
	}
}

func parseGP(input string) (int64, error) {
	s := strings.TrimSpace(strings.ToLower(strings.ReplaceAll(input, ",", "")))
	if s == "" {
		return 0, errors.New("empty gp value")
	}
	mult := float64(1)
	switch {
	case strings.HasSuffix(s, "gp"):
		s = strings.TrimSpace(strings.TrimSuffix(s, "gp"))
	case strings.HasSuffix(s, "k"):
		mult = 1_000
		s = strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		mult = 1_000_000
		s = strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "b"):
		mult = 1_000_000_000
		s = strings.TrimSuffix(s, "b")
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, err
	}
	return int64(math.Round(v * mult)), nil
}

func parseOptionalGP(input string) (sql.NullInt64, error) {
	if strings.TrimSpace(input) == "" {
		return sql.NullInt64{}, nil
	}
	v, err := parseGP(input)
	if err != nil {
		return sql.NullInt64{}, err
	}
	return sql.NullInt64{Int64: v, Valid: true}, nil
}

func nullIntArg(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

func nullGP(v sql.NullInt64) string {
	if !v.Valid {
		return "-"
	}
	return gp(v.Int64)
}

func nullPct(v sql.NullFloat64) string {
	if !v.Valid {
		return "-"
	}
	return fmt.Sprintf("%.2f%%", v.Float64*100)
}

func avgPrice(high sql.NullInt64, low sql.NullInt64) (float64, bool) {
	switch {
	case high.Valid && low.Valid:
		return float64(high.Int64+low.Int64) / 2, true
	case high.Valid:
		return float64(high.Int64), true
	case low.Valid:
		return float64(low.Int64), true
	default:
		return 0, false
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func ptrAny(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullInt(v sql.NullInt64) string {
	if !v.Valid {
		return "-"
	}
	return gp(v.Int64)
}

func ptrGP(v *int64) string {
	if v == nil {
		return "-"
	}
	return gp(*v)
}

func gp(v int64) string {
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	s := strconv.FormatInt(v, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return sign + s
}

func baseline(v float64) string {
	if v <= 0 {
		return "-"
	}
	return gp(int64(math.Round(v)))
}

func emptyZero(v int64) string {
	if v == 0 {
		return "-"
	}
	return gp(v)
}

func durationSeconds(sec int64) string {
	if sec <= 0 {
		return "0s"
	}
	d := time.Duration(sec) * time.Second
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	if d < time.Hour {
		return d.Round(time.Minute).String()
	}
	return d.Round(time.Hour).String()
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "..."
}

func meanInt64(values []int64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += float64(v)
	}
	return sum / float64(len(values))
}

func medianInt64(values []int64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]int64(nil), values...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return float64(cp[mid])
	}
	return float64(cp[mid-1]+cp[mid]) / 2
}

func quantileInt64(values []int64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]int64(nil), values...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	if q <= 0 {
		return float64(cp[0])
	}
	if q >= 1 {
		return float64(cp[len(cp)-1])
	}
	pos := q * float64(len(cp)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return float64(cp[lo])
	}
	frac := pos - float64(lo)
	return float64(cp[lo])*(1-frac) + float64(cp[hi])*frac
}

func quantileFloat64(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]float64(nil), values...)
	sort.Float64s(cp)
	if q <= 0 {
		return cp[0]
	}
	if q >= 1 {
		return cp[len(cp)-1]
	}
	pos := q * float64(len(cp)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return cp[lo]
	}
	frac := pos - float64(lo)
	return cp[lo]*(1-frac) + cp[hi]*frac
}

func percentileRankFloat64(values []float64, value float64) float64 {
	if len(values) == 0 {
		return 0
	}
	lessOrEqual := 0
	for _, v := range values {
		if v <= value {
			lessOrEqual++
		}
	}
	return float64(lessOrEqual) / float64(len(values))
}

func minFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	out := values[0]
	for _, v := range values[1:] {
		if v < out {
			out = v
		}
	}
	return out
}

func maxFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	out := values[0]
	for _, v := range values[1:] {
		if v > out {
			out = v
		}
	}
	return out
}

func max[T ~int64](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func min[T ~int64](a, b T) T {
	if a < b {
		return a
	}
	return b
}

func int64Ptr(v int64) *int64 {
	return &v
}

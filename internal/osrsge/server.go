package osrsge

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

func (a *app) cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "127.0.0.1:8765", "listen address")
	_, err := parseCommandFlags(fs, args, map[string]bool{"addr": true})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, dashboardHTML)
	})
	mux.HandleFunc("/api/patterns", a.handlePatternsAPI)
	mux.HandleFunc("/api/range-bottom", a.handleRangeBottomAPI)
	mux.HandleFunc("/api/strategy", a.handleStrategyAPI)
	mux.HandleFunc("/api/agent/manifest", a.handleAgentManifestAPI)
	mux.HandleFunc("/api/agent/run", a.handleAgentRunAPI)
	fmt.Printf("osrs-ge dashboard listening on http://%s\n", *addr)
	return http.ListenAndServe(*addr, mux)
}

func (a *app) handlePatternsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	args := []string{
		"--db", a.dbPath,
		"--user-agent", a.userAgent,
		"patterns",
		"--json",
		"--like", queryDefault(q, "like", "bronze knife"),
		"--cash", queryDefault(q, "cash", "40m"),
		"--limit", queryDefault(q, "limit", "20"),
		"--candidate-limit", queryDefault(q, "candidate_limit", "250"),
		"--days", queryDefault(q, "days", "14"),
		"--request-delay", queryDefault(q, "request_delay", "10ms"),
		"--max-rebound", queryDefault(q, "max_rebound", "72h"),
		"--min-low", queryDefault(q, "min_low", "10"),
		"--max-low", queryDefault(q, "max_low", "80"),
		"--min-high", queryDefault(q, "min_high", "60"),
		"--max-high", queryDefault(q, "max_high", "250"),
		"--min-low-volume", queryDefault(q, "min_low_volume", "100"),
		"--min-high-volume", queryDefault(q, "min_high_volume", "300"),
	}
	a.runJSONCommand(w, r, args)
}

func (a *app) handleRangeBottomAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	args := []string{
		"--db", a.dbPath,
		"--user-agent", a.userAgent,
		"range-bottom",
		"--json",
		"--cash", queryDefault(q, "cash", "40m"),
		"--step", queryDefault(q, "step", "6h"),
		"--days", queryDefault(q, "days", "90"),
		"--limit", queryDefault(q, "limit", "25"),
		"--candidate-limit", queryDefault(q, "candidate_limit", "500"),
		"--request-delay", queryDefault(q, "request_delay", "10ms"),
		"--min-buy-limit", queryDefault(q, "min_buy_limit", "5000"),
		"--max-value", queryDefault(q, "max_value", "0"),
		"--max-percentile", queryDefault(q, "max_percentile", "0.25"),
		"--bottom-percentile", queryDefault(q, "bottom_percentile", "0.25"),
		"--min-active-buckets", queryDefault(q, "min_active_buckets", "20"),
		"--min-median-volume", queryDefault(q, "min_median_volume", "250"),
		"--min-p25-volume", queryDefault(q, "min_p25_volume", "1"),
		"--min-cycles", queryDefault(q, "min_cycles", "1"),
		"--rebound-window", queryDefault(q, "rebound_window", "720h"),
	}
	a.runJSONCommand(w, r, args)
}

func (a *app) handleStrategyAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	intent := strings.TrimSpace(q.Get("intent"))
	if intent == "" {
		intent = detectStrategyIntent(q.Get("query"))
	}
	switch intent {
	case "range_bottom", "range-bottom", "vwap_bottom", "vwap-bottom":
		a.handleRangeBottomAPI(w, r)
	default:
		a.handlePatternsAPI(w, r)
	}
}

func (a *app) handleAgentManifestAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(buildAgentManifest())
}

func (a *app) handleAgentRunAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("query"))
	if query == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}
	args := []string{
		"--db", a.dbPath,
		"--user-agent", a.userAgent,
		"agent", "run", query,
		"--json",
		"--cash", queryDefault(q, "cash", "40m"),
		"--limit", queryDefault(q, "limit", "6"),
		"--candidate-limit", queryDefault(q, "candidate_limit", "120"),
		"--request-delay", queryDefault(q, "request_delay", "5ms"),
	}
	a.runJSONCommand(w, r, args)
}

func detectStrategyIntent(query string) string {
	q := strings.ToLower(query)
	switch {
	case strings.Contains(q, "vwap"),
		strings.Contains(q, "trading range"),
		strings.Contains(q, "normal range"),
		strings.Contains(q, "bottom"),
		strings.Contains(q, "historical values"),
		strings.Contains(q, "undervalued"):
		return "range_bottom"
	default:
		return "patterns"
	}
}

func (a *app) runJSONCommand(w http.ResponseWriter, r *http.Request, args []string) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		status := http.StatusInternalServerError
		if ctx.Err() == context.DeadlineExceeded {
			status = http.StatusGatewayTimeout
		}
		http.Error(w, strings.TrimSpace(string(out)+"\n"+err.Error()), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

func queryDefault(values url.Values, key, fallback string) string {
	if v := strings.TrimSpace(values.Get(key)); v != "" {
		return v
	}
	return fallback
}

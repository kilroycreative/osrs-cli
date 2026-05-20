package osrsge

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

type schemaReport struct {
	DBPath      string           `json:"db_path"`
	GeneratedAt string           `json:"generated_at"`
	Tables      []schemaTable    `json:"tables"`
	SyncState   []syncStateEntry `json:"sync_state"`
}

type schemaTable struct {
	Name     string         `json:"name"`
	RowCount int64          `json:"row_count"`
	Columns  []schemaColumn `json:"columns"`
}

type schemaColumn struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	NotNull    bool   `json:"not_null"`
	Default    string `json:"default,omitempty"`
	PrimaryKey bool   `json:"primary_key"`
}

type syncStateEntry struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt int64  `json:"updated_at"`
	Age       string `json:"age,omitempty"`
}

type doctorReport struct {
	Version          string              `json:"version"`
	DBPath           string              `json:"db_path"`
	GeneratedAt      string              `json:"generated_at"`
	APIBaseURL       string              `json:"api_base_url"`
	UserAgent        string              `json:"user_agent"`
	DefaultUserAgent bool                `json:"default_user_agent"`
	Counts           map[string]int64    `json:"counts"`
	Intervals        []intervalFreshness `json:"intervals"`
	SyncState        []syncStateEntry    `json:"sync_state"`
	APICheck         apiHealth           `json:"api_check"`
	Warnings         []string            `json:"warnings,omitempty"`
}

type intervalFreshness struct {
	Interval   string `json:"interval"`
	Timestamp  int64  `json:"timestamp"`
	AgeSeconds int64  `json:"age_seconds"`
	Rows       int64  `json:"rows"`
}

type apiHealth struct {
	Checked     bool   `json:"checked"`
	OK          bool   `json:"ok"`
	LatestItems int    `json:"latest_items,omitempty"`
	Error       string `json:"error,omitempty"`
}

func (a *app) cmdSchema(args []string) error {
	fs := flag.NewFlagSet("schema", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit JSON")
	tableFilter := fs.String("table", "", "only describe one table")
	_, err := parseCommandFlags(fs, args, map[string]bool{"table": true})
	if err != nil {
		return err
	}
	report, err := a.buildSchemaReport(strings.TrimSpace(*tableFilter))
	if err != nil {
		return err
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	return writeSchemaReport(os.Stdout, report)
}

func (a *app) buildSchemaReport(tableFilter string) (schemaReport, error) {
	tables, err := a.schemaTables(tableFilter)
	if err != nil {
		return schemaReport{}, err
	}
	syncState, err := a.loadSyncState()
	if err != nil {
		return schemaReport{}, err
	}
	return schemaReport{
		DBPath:      a.dbPath,
		GeneratedAt: time.Now().Format(time.RFC3339),
		Tables:      tables,
		SyncState:   syncState,
	}, nil
}

func (a *app) schemaTables(tableFilter string) ([]schemaTable, error) {
	query := `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`
	var args []any
	if tableFilter != "" {
		query += ` AND name = ?`
		args = append(args, tableFilter)
	}
	query += ` ORDER BY name`
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []schemaTable
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		table, err := a.describeTable(name)
		if err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if tableFilter != "" && len(tables) == 0 {
		return nil, fmt.Errorf("unknown table %q", tableFilter)
	}
	return tables, nil
}

func (a *app) describeTable(name string) (schemaTable, error) {
	countSQL := fmt.Sprintf("SELECT count(*) FROM %s", quoteSQLiteIdentifier(name))
	var rowCount int64
	if err := a.db.QueryRow(countSQL).Scan(&rowCount); err != nil {
		return schemaTable{}, err
	}

	rows, err := a.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", quoteSQLiteIdentifier(name)))
	if err != nil {
		return schemaTable{}, err
	}
	defer rows.Close()

	table := schemaTable{Name: name, RowCount: rowCount}
	for rows.Next() {
		var cid int
		var colName, colType string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &colName, &colType, &notNull, &defaultValue, &pk); err != nil {
			return schemaTable{}, err
		}
		col := schemaColumn{
			Name:       colName,
			Type:       colType,
			NotNull:    notNull != 0,
			PrimaryKey: pk != 0,
		}
		if defaultValue.Valid {
			col.Default = defaultValue.String
		}
		table.Columns = append(table.Columns, col)
	}
	return table, rows.Err()
}

func (a *app) cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit JSON")
	noAPI := fs.Bool("no-api", false, "skip live OSRS Wiki API check")
	timeout := fs.Duration("timeout", 8*time.Second, "API check timeout")
	_, err := parseCommandFlags(fs, args, map[string]bool{"timeout": true})
	if err != nil {
		return err
	}
	report, err := a.buildDoctorReport(!*noAPI, *timeout)
	if err != nil {
		return err
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	return writeDoctorReport(os.Stdout, report)
}

func (a *app) buildDoctorReport(checkAPI bool, timeout time.Duration) (doctorReport, error) {
	report := doctorReport{
		Version:          version,
		DBPath:           a.dbPath,
		GeneratedAt:      time.Now().Format(time.RFC3339),
		APIBaseURL:       a.client.baseURL,
		UserAgent:        a.userAgent,
		DefaultUserAgent: a.userAgent == defaultUserAgentValue,
		Counts:           make(map[string]int64),
	}

	for _, table := range []string{"items", "latest_prices", "latest_snapshots", "interval_prices", "watchlist", "alert_events"} {
		count, err := a.tableCount(table)
		if err != nil {
			return doctorReport{}, err
		}
		report.Counts[table] = count
	}

	syncState, err := a.loadSyncState()
	if err != nil {
		return doctorReport{}, err
	}
	report.SyncState = syncState

	for _, interval := range []string{"5m", "1h"} {
		freshness, err := a.intervalFreshness(interval)
		if err != nil {
			return doctorReport{}, err
		}
		report.Intervals = append(report.Intervals, freshness)
	}

	if checkAPI {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		report.APICheck.Checked = true
		latest, err := a.client.latest(ctx)
		if err != nil {
			report.APICheck.Error = err.Error()
			report.Warnings = append(report.Warnings, "OSRS Wiki latest endpoint check failed")
		} else {
			report.APICheck.OK = true
			report.APICheck.LatestItems = len(latest.Data)
		}
	}

	report.Warnings = append(report.Warnings, doctorWarnings(report)...)
	return report, nil
}

func (a *app) tableCount(table string) (int64, error) {
	var count int64
	err := a.db.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s", quoteSQLiteIdentifier(table))).Scan(&count)
	return count, err
}

func (a *app) loadSyncState() ([]syncStateEntry, error) {
	rows, err := a.db.Query(`SELECT key, value, updated_at FROM sync_state ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []syncStateEntry
	now := time.Now().Unix()
	for rows.Next() {
		var entry syncStateEntry
		if err := rows.Scan(&entry.Key, &entry.Value, &entry.UpdatedAt); err != nil {
			return nil, err
		}
		if entry.UpdatedAt > 0 {
			entry.Age = durationSeconds(max[int64](0, now-entry.UpdatedAt))
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (a *app) intervalFreshness(interval string) (intervalFreshness, error) {
	var timestamp sql.NullInt64
	var rows int64
	err := a.db.QueryRow(`SELECT max(timestamp), count(*) FROM interval_prices WHERE interval = ?`, interval).Scan(&timestamp, &rows)
	if err != nil {
		return intervalFreshness{}, err
	}
	freshness := intervalFreshness{Interval: interval, Rows: rows}
	if timestamp.Valid {
		freshness.Timestamp = timestamp.Int64
		freshness.AgeSeconds = max[int64](0, time.Now().Unix()-timestamp.Int64)
	}
	return freshness, nil
}

func doctorWarnings(report doctorReport) []string {
	var warnings []string
	if report.DefaultUserAgent {
		warnings = append(warnings, "set OSRS_GE_USER_AGENT to a descriptive value before heavy API use")
	}
	if report.Counts["items"] == 0 {
		warnings = append(warnings, "item mapping cache is empty; run osrs-ge sync")
	}
	if report.Counts["latest_prices"] == 0 {
		warnings = append(warnings, "latest price cache is empty; run osrs-ge sync")
	}
	for _, interval := range report.Intervals {
		if interval.Rows == 0 {
			warnings = append(warnings, fmt.Sprintf("%s interval cache is empty; run osrs-ge sync", interval.Interval))
			continue
		}
		switch interval.Interval {
		case "5m":
			if interval.AgeSeconds > int64((30 * time.Minute).Seconds()) {
				warnings = append(warnings, fmt.Sprintf("5m interval cache is stale: %s old", durationSeconds(interval.AgeSeconds)))
			}
		case "1h":
			if interval.AgeSeconds > int64((4 * time.Hour).Seconds()) {
				warnings = append(warnings, fmt.Sprintf("1h interval cache is stale: %s old", durationSeconds(interval.AgeSeconds)))
			}
		}
	}
	return warnings
}

func writeSchemaReport(w io.Writer, report schemaReport) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "DB\t%s\n", report.DBPath)
	fmt.Fprintf(tw, "Generated\t%s\n\n", report.GeneratedAt)
	fmt.Fprintln(tw, "TABLE\tROWS\tCOLUMNS")
	for _, table := range report.Tables {
		var columns []string
		for _, column := range table.Columns {
			label := column.Name
			if column.PrimaryKey {
				label += " pk"
			}
			if column.Type != "" {
				label += " " + column.Type
			}
			columns = append(columns, label)
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\n", table.Name, table.RowCount, strings.Join(columns, ", "))
	}
	if len(report.SyncState) > 0 {
		fmt.Fprintln(tw, "\nSYNC KEY\tVALUE\tAGE")
		for _, entry := range report.SyncState {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", entry.Key, entry.Value, entry.Age)
		}
	}
	return tw.Flush()
}

func writeDoctorReport(w io.Writer, report doctorReport) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Version\t%s\n", report.Version)
	fmt.Fprintf(tw, "DB\t%s\n", report.DBPath)
	fmt.Fprintf(tw, "API\t%s\n", report.APIBaseURL)
	if report.APICheck.Checked {
		status := "ok"
		if !report.APICheck.OK {
			status = "failed"
		}
		fmt.Fprintf(tw, "API check\t%s", status)
		if report.APICheck.LatestItems > 0 {
			fmt.Fprintf(tw, " (%d latest items)", report.APICheck.LatestItems)
		}
		if report.APICheck.Error != "" {
			fmt.Fprintf(tw, " %s", report.APICheck.Error)
		}
		fmt.Fprintln(tw)
	} else {
		fmt.Fprintln(tw, "API check\tskipped")
	}
	fmt.Fprintf(tw, "User-Agent\t%s\n\n", report.UserAgent)

	fmt.Fprintln(tw, "COUNT\tROWS")
	for _, name := range []string{"items", "latest_prices", "latest_snapshots", "interval_prices", "watchlist", "alert_events"} {
		fmt.Fprintf(tw, "%s\t%d\n", name, report.Counts[name])
	}

	fmt.Fprintln(tw, "\nINTERVAL\tROWS\tLATEST\tAGE")
	for _, interval := range report.Intervals {
		latest := "-"
		age := "-"
		if interval.Timestamp > 0 {
			latest = strconv.FormatInt(interval.Timestamp, 10)
			age = durationSeconds(interval.AgeSeconds)
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", interval.Interval, interval.Rows, latest, age)
	}

	if len(report.Warnings) > 0 {
		fmt.Fprintln(tw, "\nWARNINGS")
		for _, warning := range report.Warnings {
			fmt.Fprintf(tw, "-\t%s\n", warning)
		}
	}
	return tw.Flush()
}

func quoteSQLiteIdentifier(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

package osrsge

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

func (a *app) cmdTimeseries(args []string) error {
	fs := flag.NewFlagSet("timeseries", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	step := fs.String("step", "1h", "5m, 1h, 6h, or 24h")
	limit := fs.Int("limit", 30, "maximum rows to print from the end")
	jsonOut := fs.Bool("json", false, "emit JSON")
	positionals, err := parseCommandFlags(fs, args, map[string]bool{"step": true, "limit": true})
	if err != nil {
		return err
	}
	input := strings.TrimSpace(strings.Join(positionals, " "))
	if input == "" {
		return errors.New("timeseries requires an item name or id")
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
	points := resp.Data
	if *limit > 0 && len(points) > *limit {
		points = points[len(points)-*limit:]
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(points)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Item\t%s (%d)\n", item.Name, item.ID)
	fmt.Fprintln(tw, "TIME\tAVG LOW\tAVG HIGH\tLOW VOL\tHIGH VOL\tTOTAL VOL")
	for _, p := range points {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", time.Unix(p.Timestamp, 0).Format("2006-01-02 15:04"), ptrGP(p.AvgLowPrice), ptrGP(p.AvgHighPrice), gp(p.LowVolume), gp(p.HighVolume), gp(p.LowVolume+p.HighVolume))
	}
	return tw.Flush()
}

func (a *app) cmdSQL(args []string) error {
	fs := flag.NewFlagSet("sql", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit JSON")
	positionals, err := parseCommandFlags(fs, args, nil)
	if err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(positionals, " "))
	if query == "" {
		return errors.New("sql requires a query")
	}
	lower := strings.ToLower(query)
	if !(strings.HasPrefix(lower, "select") || strings.HasPrefix(lower, "with")) {
		return errors.New("sql is read-only; query must start with SELECT or WITH")
	}
	rows, err := a.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	if *jsonOut {
		var out []map[string]any
		for rows.Next() {
			if err := rows.Scan(ptrs...); err != nil {
				return err
			}
			row := make(map[string]any, len(cols))
			for i, col := range cols {
				row[col] = normalizeSQLValue(values[i])
			}
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(cols, "\t"))
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		cells := make([]string, len(cols))
		for i := range cols {
			cells[i] = fmt.Sprint(normalizeSQLValue(values[i]))
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return tw.Flush()
}

package osrsge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
)

func (a *app) cmdSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", 20, "maximum rows")
	jsonOut := fs.Bool("json", false, "emit JSON")
	noSync := fs.Bool("no-sync", false, "do not refresh missing cache")
	positionals, err := parseCommandFlags(fs, args, map[string]bool{"limit": true})
	if err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(positionals, " "))
	if query == "" {
		return errors.New("search requires a query")
	}
	if err := a.ensureItems(*noSync); err != nil {
		return err
	}
	items, err := a.searchItems(query, *limit)
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(items)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tMEMBERS\tLIMIT\tVALUE\tEXAMINE")
	for _, item := range items {
		fmt.Fprintf(tw, "%d\t%s\t%t\t%s\t%s\t%s\n", item.ID, item.Name, item.Members, nullInt(item.BuyLimit), nullInt(item.Value), clip(item.Examine, 72))
	}
	return tw.Flush()
}

func (a *app) ensureItems(noSync bool) error {
	var itemCount int
	if err := a.db.QueryRow(`SELECT count(*) FROM items`).Scan(&itemCount); err != nil {
		return err
	}
	if itemCount == 0 && !noSync {
		items, err := a.client.mapping(context.Background())
		if err != nil {
			return err
		}
		return saveMapping(a.db, items)
	}
	return nil
}

func (a *app) searchItems(query string, limit int) ([]itemRecord, error) {
	pat := "%" + strings.ToLower(query) + "%"
	rows, err := a.db.Query(`
SELECT id, name, members, buy_limit, value, high_alch, low_alch, examine
FROM items
WHERE lower(name) LIKE ?
ORDER BY
  CASE WHEN lower(name) = lower(?) THEN 0 ELSE 1 END,
  instr(lower(name), lower(?)),
  length(name),
  name
LIMIT ?`, pat, query, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []itemRecord
	for rows.Next() {
		var item itemRecord
		var members int
		if err := rows.Scan(&item.ID, &item.Name, &members, &item.BuyLimit, &item.Value, &item.HighAlch, &item.LowAlch, &item.Examine); err != nil {
			return nil, err
		}
		item.Members = members != 0
		out = append(out, item)
	}
	return out, rows.Err()
}

func (a *app) resolveItem(input string) (itemRecord, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return itemRecord{}, errors.New("item is required")
	}
	if id, err := strconv.ParseInt(input, 10, 64); err == nil {
		return a.getItemByID(id)
	}
	items, err := a.searchItems(input, 5)
	if err != nil {
		return itemRecord{}, err
	}
	if len(items) == 0 {
		return itemRecord{}, fmt.Errorf("no item matched %q", input)
	}
	return items[0], nil
}

func (a *app) getItemByID(id int64) (itemRecord, error) {
	var item itemRecord
	var members int
	err := a.db.QueryRow(`SELECT id, name, members, buy_limit, value, high_alch, low_alch, examine FROM items WHERE id = ?`, id).
		Scan(&item.ID, &item.Name, &members, &item.BuyLimit, &item.Value, &item.HighAlch, &item.LowAlch, &item.Examine)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return itemRecord{}, fmt.Errorf("no item id %d in cache; run osrs-ge sync", id)
		}
		return itemRecord{}, err
	}
	item.Members = members != 0
	return item, nil
}

func (a *app) cmdPrice(args []string) error {
	fs := flag.NewFlagSet("price", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit JSON")
	noSync := fs.Bool("no-sync", false, "do not refresh latest/1h cache")
	taxRate := fs.Float64("tax-rate", 0.02, "GE tax rate")
	taxCap := fs.Int64("tax-cap", 5_000_000, "GE tax cap per item")
	positionals, err := parseCommandFlags(fs, args, map[string]bool{"tax-rate": true, "tax-cap": true})
	if err != nil {
		return err
	}
	input := strings.TrimSpace(strings.Join(positionals, " "))
	if input == "" {
		return errors.New("price requires an item name or id")
	}
	if err := a.ensureItems(false); err != nil {
		return err
	}
	if err := a.ensureCurrent(*noSync, "1h"); err != nil {
		return err
	}
	item, err := a.resolveItem(input)
	if err != nil {
		return err
	}
	opp, err := a.oneOpportunity(item.ID, *taxRate, *taxCap)
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(opp)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FIELD\tVALUE")
	fmt.Fprintf(tw, "Item\t%s (%d)\n", opp.Name, opp.ID)
	fmt.Fprintf(tw, "Members\t%t\n", opp.Members)
	fmt.Fprintf(tw, "Low / High\t%s / %s gp\n", gp(opp.Low), gp(opp.High))
	fmt.Fprintf(tw, "Tax\t%s gp\n", gp(opp.Tax))
	fmt.Fprintf(tw, "Net margin\t%s gp\n", gp(opp.NetMargin))
	fmt.Fprintf(tw, "ROI\t%.2f%%\n", opp.ROI*100)
	fmt.Fprintf(tw, "1h volume\t%s\n", gp(opp.Volume))
	fmt.Fprintf(tw, "Buy limit\t%s\n", emptyZero(opp.BuyLimit))
	fmt.Fprintf(tw, "Limit profit\t%s gp\n", gp(opp.LimitProfit))
	fmt.Fprintf(tw, "High/low age\t%s / %s\n", durationSeconds(opp.HighAgeSeconds), durationSeconds(opp.LowAgeSeconds))
	return tw.Flush()
}

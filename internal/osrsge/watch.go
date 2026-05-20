package osrsge

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

func (a *app) cmdWatch(args []string) error {
	if len(args) == 0 {
		return errors.New("watch requires a subcommand: add, list, remove, check")
	}
	switch args[0] {
	case "add":
		return a.cmdWatchAdd(args[1:])
	case "list", "ls":
		return a.cmdWatchList(args[1:])
	case "remove", "rm", "delete":
		return a.cmdWatchRemove(args[1:])
	case "check":
		return a.cmdWatchCheck(args[1:])
	default:
		return fmt.Errorf("unknown watch subcommand %q", args[0])
	}
}

func (a *app) cmdWatchAdd(args []string) error {
	fs := flag.NewFlagSet("watch add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	belowText := fs.String("below", "", "trigger when low price is at or below this gp value")
	aboveText := fs.String("above", "", "trigger when high price is at or above this gp value")
	marginText := fs.String("min-margin", "", "trigger when net margin is at least this gp value")
	minROI := fs.Float64("min-roi", 0, "trigger when ROI is at least this decimal value")
	minVolume := fs.Int64("min-volume", 0, "trigger when 1h volume is at least this value")
	cooldown := fs.Duration("cooldown", time.Hour, "minimum time between repeated triggers")
	note := fs.String("note", "", "optional note")
	positionals, err := parseCommandFlags(fs, args, map[string]bool{"below": true, "above": true, "min-margin": true, "min-roi": true, "min-volume": true, "cooldown": true, "note": true})
	if err != nil {
		return err
	}
	input := strings.TrimSpace(strings.Join(positionals, " "))
	if input == "" {
		return errors.New("watch add requires an item")
	}
	if err := a.ensureItems(false); err != nil {
		return err
	}
	item, err := a.resolveItem(input)
	if err != nil {
		return err
	}
	below, err := parseOptionalGP(*belowText)
	if err != nil {
		return fmt.Errorf("--below: %w", err)
	}
	above, err := parseOptionalGP(*aboveText)
	if err != nil {
		return fmt.Errorf("--above: %w", err)
	}
	margin, err := parseOptionalGP(*marginText)
	if err != nil {
		return fmt.Errorf("--min-margin: %w", err)
	}
	if !below.Valid && !above.Valid && !margin.Valid && *minROI <= 0 && *minVolume <= 0 {
		return errors.New("watch add requires at least one condition")
	}
	now := time.Now().Unix()
	var roi any
	if *minROI > 0 {
		roi = *minROI
	}
	var volume any
	if *minVolume > 0 {
		volume = *minVolume
	}
	res, err := a.db.Exec(`
INSERT INTO watchlist (item_id, below, above, min_net_margin, min_roi, min_volume, cooldown_seconds, enabled, note, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		item.ID, nullIntArg(below), nullIntArg(above), nullIntArg(margin), roi, volume, int64(cooldown.Seconds()), *note, now, now)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	fmt.Printf("added watch rule %d for %s (%d)\n", id, item.Name, item.ID)
	return nil
}

func (a *app) cmdWatchList(args []string) error {
	fs := flag.NewFlagSet("watch list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit JSON")
	_, err := parseCommandFlags(fs, args, nil)
	if err != nil {
		return err
	}
	rules, err := a.loadWatchRules(false)
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(rules)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tITEM\tBELOW\tABOVE\tMIN NET\tMIN ROI\tMIN VOL\tCOOLDOWN\tENABLED\tNOTE")
	for _, r := range rules {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%t\t%s\n",
			r.ID, r.Name, nullGP(r.Below), nullGP(r.Above), nullGP(r.MinNetMargin),
			nullPct(r.MinROI), nullGP(r.MinVolume), durationSeconds(r.CooldownSeconds), r.Enabled, r.Note)
	}
	return tw.Flush()
}

func (a *app) cmdWatchRemove(args []string) error {
	if len(args) == 0 {
		return errors.New("watch remove requires a rule id or item")
	}
	input := strings.TrimSpace(strings.Join(args, " "))
	if id, err := strconv.ParseInt(input, 10, 64); err == nil {
		res, err := a.db.Exec(`DELETE FROM watchlist WHERE id = ?`, id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		fmt.Printf("removed %d rule(s)\n", n)
		return nil
	}
	if err := a.ensureItems(false); err != nil {
		return err
	}
	item, err := a.resolveItem(input)
	if err != nil {
		return err
	}
	res, err := a.db.Exec(`DELETE FROM watchlist WHERE item_id = ?`, item.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	fmt.Printf("removed %d rule(s) for %s\n", n, item.Name)
	return nil
}

func (a *app) cmdWatchCheck(args []string) error {
	fs := flag.NewFlagSet("watch check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	noSync := fs.Bool("no-sync", false, "do not refresh latest/1h cache")
	jsonOut := fs.Bool("json", false, "emit JSON")
	_, err := parseCommandFlags(fs, args, nil)
	if err != nil {
		return err
	}
	if err := a.ensureItems(false); err != nil {
		return err
	}
	if err := a.ensureCurrent(*noSync, "1h"); err != nil {
		return err
	}
	rules, err := a.loadWatchRules(true)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	var hits []alertHit
	for _, rule := range rules {
		if rule.LastTriggeredAt.Valid && now-rule.LastTriggeredAt.Int64 < rule.CooldownSeconds {
			continue
		}
		opp, err := a.oneOpportunity(rule.ItemID, 0.02, 5_000_000)
		if err != nil {
			continue
		}
		reasons := watchReasons(rule, opp)
		if len(reasons) == 0 {
			continue
		}
		reason := strings.Join(reasons, "; ")
		if _, err := a.db.Exec(`INSERT INTO alert_events (rule_id, item_id, reason, low, high, net_margin, roi, volume, triggered_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rule.ID, rule.ItemID, reason, opp.Low, opp.High, opp.NetMargin, opp.ROI, opp.Volume, now); err != nil {
			return err
		}
		if _, err := a.db.Exec(`UPDATE watchlist SET last_triggered_at = ?, updated_at = ? WHERE id = ?`, now, now, rule.ID); err != nil {
			return err
		}
		hits = append(hits, alertHit{
			RuleID:          rule.ID,
			ItemID:          rule.ItemID,
			Name:            rule.Name,
			Reason:          reason,
			Low:             opp.Low,
			High:            opp.High,
			NetMargin:       opp.NetMargin,
			ROI:             opp.ROI,
			Volume:          opp.Volume,
			LastTriggeredAt: now,
		})
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(hits)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RULE\tITEM\tREASON\tLOW\tHIGH\tNET\tROI\tVOL")
	for _, hit := range hits {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%.2f%%\t%s\n", hit.RuleID, hit.Name, hit.Reason, gp(hit.Low), gp(hit.High), gp(hit.NetMargin), hit.ROI*100, gp(hit.Volume))
	}
	if len(hits) == 0 {
		fmt.Fprintln(tw, "No watch rules triggered.")
	}
	return tw.Flush()
}

func (a *app) loadWatchRules(enabledOnly bool) ([]watchRule, error) {
	query := `
SELECT w.id, w.item_id, i.name, w.below, w.above, w.min_net_margin, w.min_roi, w.min_volume,
       w.cooldown_seconds, w.enabled, coalesce(w.note, ''), w.last_triggered_at, w.created_at, w.updated_at
FROM watchlist w
JOIN items i ON i.id = w.item_id`
	if enabledOnly {
		query += ` WHERE w.enabled = 1`
	}
	query += ` ORDER BY w.id`
	rows, err := a.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []watchRule
	for rows.Next() {
		var r watchRule
		var enabled int
		if err := rows.Scan(&r.ID, &r.ItemID, &r.Name, &r.Below, &r.Above, &r.MinNetMargin, &r.MinROI, &r.MinVolume, &r.CooldownSeconds, &enabled, &r.Note, &r.LastTriggeredAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func watchReasons(rule watchRule, opp opportunity) []string {
	var reasons []string
	if rule.Below.Valid && opp.Low <= rule.Below.Int64 {
		reasons = append(reasons, fmt.Sprintf("low <= %s", gp(rule.Below.Int64)))
	}
	if rule.Above.Valid && opp.High >= rule.Above.Int64 {
		reasons = append(reasons, fmt.Sprintf("high >= %s", gp(rule.Above.Int64)))
	}
	if rule.MinNetMargin.Valid && opp.NetMargin >= rule.MinNetMargin.Int64 {
		reasons = append(reasons, fmt.Sprintf("net >= %s", gp(rule.MinNetMargin.Int64)))
	}
	if rule.MinROI.Valid && opp.ROI >= rule.MinROI.Float64 {
		reasons = append(reasons, fmt.Sprintf("roi >= %.2f%%", rule.MinROI.Float64*100))
	}
	if rule.MinVolume.Valid && opp.Volume >= rule.MinVolume.Int64 {
		reasons = append(reasons, fmt.Sprintf("volume >= %s", gp(rule.MinVolume.Int64)))
	}
	return reasons
}

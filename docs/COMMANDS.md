# Commands

`pp-osrs-ge` installs a single command: `osrs-ge`.

Global flags:

```bash
osrs-ge [--db PATH] [--user-agent UA] <command> [flags]
```

## Data And Diagnostics

```bash
osrs-ge sync
osrs-ge doctor
osrs-ge doctor --json --no-api
osrs-ge schema
osrs-ge schema --table items --json
```

- `sync` refreshes item mapping, latest prices, and interval snapshots.
- `doctor` checks cache counts, interval freshness, API reachability, and setup
  warnings.
- `schema` describes the local SQLite cache for humans, scripts, and agents.

## Item Lookup

```bash
osrs-ge search "abyssal"
osrs-ge price "abyssal whip"
osrs-ge timeseries "blood rune" --step 1h --limit 48
```

- `search` finds item metadata and buy limits.
- `price` shows latest low/high, tax-adjusted spread, ROI, volume, and age.
- `timeseries` fetches recent item history from the OSRS Wiki API.

## Opportunity Scans

```bash
osrs-ge opportunities --limit 25 --min-volume 500
osrs-ge movers --interval 1h --back 1 --limit 20
osrs-ge allocate --cash 20m --limit 8 --min-volume 1000
```

- `opportunities` ranks current spread candidates with liquidity filters.
- `movers` compares current and prior 1h/5m buckets.
- `allocate` applies a bankroll lens to current opportunities.

## Research Probes

```bash
osrs-ge patterns --like "bronze knife" --cash 40m --limit 15
osrs-ge range-bottom --cash 40m --days 90 --step 6h
osrs-ge agent manifest --json
osrs-ge agent run "items at the bottom of VWAP with consistent volume" --json
```

- `patterns` searches for dump/rebound setups.
- `range-bottom` finds items near the bottom of their own range/VWAP.
- `agent` exposes deterministic multi-probe research bundles designed for LLM
  loops.

## Local Tools

```bash
osrs-ge serve --addr 127.0.0.1:8765
osrs-ge watch add "blood rune" --below 280 --min-volume 100000
osrs-ge watch check
osrs-ge sql "select name, buy_limit from items order by buy_limit desc limit 20"
```

- `serve` runs a local read-only dashboard.
- `watch` stores local watch rules and alert events.
- `sql` runs read-only SQL against the local cache.


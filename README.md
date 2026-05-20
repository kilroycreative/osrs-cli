# pp-osrs-ge

Local-first Old School RuneScape Grand Exchange research CLI from
[Printing Press](https://printingpress.dev).

`osrs-ge` is a read-only command-line tool for exploring OSRS Wiki price data:
current margins, item metadata, price history, volume changes, watch rules,
budget-aware allocation, and experimental strategy-research probes.

It does not automate the game client, place offers, use account credentials, or
control gameplay.

## Features

- Search item metadata and GE buy limits.
- Show current price, tax-adjusted margin, ROI, volume, and freshness.
- Rank current opportunities with volume and freshness filters.
- Compare short-term price/volume movers.
- Pull item time series from OSRS Wiki.
- Run simple theoretical spread-capture backtests.
- Add local watch rules and record triggered alerts.
- Explore range/VWAP-bottom and dump/rebound research probes.
- Use the experimental agent workbench to run multiple probes from one natural
  language research query.
- Inspect the local cache schema and run setup/freshness diagnostics.
- Serve a local read-only dashboard.

## Install

Requires Go 1.24 or newer.

```bash
git clone https://github.com/kierandotai/pp-osrs-ge.git
cd pp-osrs-ge
go build -o osrs-ge ./cmd/osrs-ge
./osrs-ge version
```

Optional local install:

```bash
go install ./cmd/osrs-ge
```

Remote install:

```bash
go install github.com/kierandotai/pp-osrs-ge/cmd/osrs-ge@latest
```

The default SQLite cache is:

```text
~/.osrs-ge/osrs-ge.sqlite
```

You can override it with:

```bash
OSRS_GE_DB=/path/to/cache.sqlite osrs-ge sync
```

## API Etiquette

Data comes from the OSRS Wiki real-time prices API:

- `mapping` for item metadata, GE buy limits, and members status
- `latest` for latest high/low prices
- `1h` and `5m` for average prices and volume snapshots
- `timeseries` for single-item history

Set a descriptive User-Agent before heavy use:

```bash
export OSRS_GE_USER_AGENT="your-app-name/0.1 (+contact@example.com)"
```

The tool caches bulk data locally where possible, but some commands fetch
per-item time series. Use candidate limits and request delays responsibly.

## Printing Press

`pp-osrs-ge` is part of the Printing Press CLI family:

```text
https://printingpress.dev
```

The project follows the Printing Press pattern of small local-first CLIs with
plain terminal output for humans and structured JSON output for agents.

## Quick Start

```bash
osrs-ge sync
osrs-ge doctor
osrs-ge search "abyssal"
osrs-ge price "abyssal whip"
osrs-ge opportunities --limit 25 --min-volume 500
osrs-ge movers --interval 1h --limit 20
osrs-ge timeseries "blood rune" --step 1h --limit 48
```

## Examples

Find current high-margin rows with volume:

```bash
osrs-ge opportunities --limit 25 --min-volume 500 --volume-baseline global-average
```

Compare short-term movers:

```bash
osrs-ge movers --interval 1h --back 1 --limit 20
```

Allocate a hypothetical cash budget across current candidates:

```bash
osrs-ge allocate --cash 20m --limit 8 --min-volume 1000
```

Inspect one item history:

```bash
osrs-ge timeseries "blood rune" --step 1h --limit 48
```

Run a theoretical spread-capture check:

```bash
osrs-ge backtest "blood rune" --cash 5m --step 1h
```

Create a local watch rule:

```bash
osrs-ge watch add "blood rune" --below 280 --min-volume 100000
osrs-ge watch check
```

Use read-only SQL against the local cache:

```bash
osrs-ge schema --json
osrs-ge sql "select name, buy_limit from items where members = 0 order by buy_limit desc limit 20"
```

Check local readiness before a research run:

```bash
osrs-ge doctor
osrs-ge doctor --json --no-api
```

## Research Probes

The research probes are opinionated examples of how to build analysis on top of
the raw price API. Treat them as research aids, not guaranteed trading signals.

Find recent dump/rebound patterns:

```bash
osrs-ge patterns --cash 40m --limit 15
```

Find items near the bottom of their own range/VWAP:

```bash
osrs-ge range-bottom --cash 40m --days 90 --step 6h
```

Run the experimental agent workbench:

```bash
osrs-ge agent manifest --json
osrs-ge agent run "items at the bottom of VWAP with consistent volume" --json
```

The agent workbench derives a research spec from one natural-language query,
runs multiple probe families, and returns an evidence bundle. It is deterministic
today; it is designed to be a clean tool surface for an LLM loop.

## Dashboard

Run the local dashboard:

```bash
osrs-ge serve --addr 127.0.0.1:8765
```

Then open:

```text
http://127.0.0.1:8765
```

The dashboard is local and read-only.

## Output Formats

Most analytical commands support `--json`. Some commands also support `--csv`.
Table output is intended for humans; JSON output is intended for scripts and
LLM/tooling integrations.

## Safety And Scope

This project is for market research only.

- No account credentials.
- No game-client automation.
- No offer placement.
- No mouse/keyboard generation.
- No gameplay automation.

Public API data is aggregated. It can show market-level patterns, but it cannot
prove your personal fills or guarantee future liquidity.

## Development

Repo layout:

```text
cmd/osrs-ge/          CLI entrypoint
internal/osrsge/      focused internal package files for app, store, API client,
                      analysis probes, agent workbench, diagnostics, and server
docs/                 design notes and agent-workbench docs
.github/workflows/   CI
```

```bash
go test ./...
go vet ./...
go build -o osrs-ge ./cmd/osrs-ge
```

The root `osrs-ge` binary is ignored by git. Build artifacts and local SQLite
caches should not be committed.

## License

MIT. See [LICENSE](LICENSE).

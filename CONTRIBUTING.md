# Contributing

Thanks for considering a contribution.

## Development

```bash
go test ./...
go vet ./...
go build -o osrs-ge ./cmd/osrs-ge
```

The built `osrs-ge` binary is ignored and should not be committed.

## Scope

This project is read-only market research tooling. Contributions should not add:

- game-client automation
- account credential handling
- offer placement
- mouse/keyboard automation
- evasion or botting features

## Data Etiquette

Commands that call the OSRS Wiki API should use descriptive User-Agent strings
and avoid unnecessary request volume. Prefer bulk endpoints and local cache
reads where possible.

## Tests

Add focused tests for parsing, scoring, and output-shape behavior when changing
shared CLI logic. Avoid tests that depend on live API responses.


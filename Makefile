.PHONY: build test vet smoke check clean

build:
	go build -o osrs-ge ./cmd/osrs-ge

test:
	go test ./...

vet:
	go vet ./...

smoke: build
	tmpdir=$$(mktemp -d); \
	OSRS_GE_DB="$$tmpdir/osrs-ge.sqlite" ./osrs-ge doctor --no-api >/dev/null; \
	OSRS_GE_DB="$$tmpdir/osrs-ge.sqlite" ./osrs-ge schema --table items --json >/dev/null; \
	rm -rf "$$tmpdir"

check: test vet smoke

clean:
	rm -f osrs-ge

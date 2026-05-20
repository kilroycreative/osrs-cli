.PHONY: build test vet check clean

build:
	go build -o osrs-ge ./cmd/osrs-ge

test:
	go test ./...

vet:
	go vet ./...

check: test vet

clean:
	rm -f osrs-ge


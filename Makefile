BIN ?= londonjourneycli

.PHONY: build test live-test fmt lint install clean

build:
	mkdir -p bin
	go build -o bin/$(BIN) ./cmd/londonjourneycli

test:
	go test ./...

live-test:
	LONDONJOURNEYCLI_LIVE_TFL=1 go test ./internal/tfl -run Live -count=1 -v

fmt:
	gofmt -w ./cmd ./internal

lint:
	go test ./...

install:
	go install ./cmd/londonjourneycli

clean:
	rm -rf bin dist coverage.out

BINARY     := fleetq-bridge
MODULE     := github.com/fleetq/fleetq-bridge
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE       := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w \
              -X $(MODULE)/internal/version.Version=$(VERSION) \
              -X $(MODULE)/internal/version.Commit=$(COMMIT) \
              -X $(MODULE)/internal/version.Date=$(DATE)

.PHONY: build build-systray run clean install lint test

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/fleetq-bridge

# Build with system tray icon (requires CGO_ENABLED=1 and platform libs)
build-systray:
	CGO_ENABLED=1 go build -tags systray -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/fleetq-bridge

run: build
	./$(BINARY)

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/fleetq-bridge

clean:
	rm -f $(BINARY)
	rm -rf dist/

lint:
	golangci-lint run ./...

test:
	go test ./...

tidy:
	go mod tidy

.DEFAULT_GOAL := build

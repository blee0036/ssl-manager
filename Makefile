.PHONY: all build build-web build-agent build-agent-release build-linux build-darwin release clean test lint

# Binary output directory
BIN_DIR := bin

# Binary names
WEB_BINARY := ssl-manager-web
AGENT_BINARY := ssl-manager-agent

# Version injection
VERSION ?= $(shell cat VERSION 2>/dev/null || echo "0.0.0")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# Build flags
LDFLAGS := -s -w
AGENT_LDFLAGS := -s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)
GOFLAGS := -buildvcs=false
CGO_ENABLED := 0

all: build

build: build-web build-agent build-agent-release

build-web:
	@echo "Building Web Backend..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(WEB_BINARY) ./cmd/web

build-agent:
	@echo "Building Agent..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(AGENT_BINARY) ./cmd/agent

clean:
	@echo "Cleaning..."
	@rm -rf $(BIN_DIR)

test:
	@echo "Running tests..."
	CGO_ENABLED=$(CGO_ENABLED) go test ./... -v

lint:
	@echo "Running linter..."
	golangci-lint run ./...

# Cross-compile for Linux (primary deployment target)
build-linux:
	@echo "Building for Linux amd64 and arm64..."
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(WEB_BINARY)-linux-amd64 ./cmd/web
	GOOS=linux GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(AGENT_LDFLAGS)" -o $(BIN_DIR)/$(AGENT_BINARY)-linux-amd64 ./cmd/agent
	GOOS=linux GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(WEB_BINARY)-linux-arm64 ./cmd/web
	GOOS=linux GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(AGENT_LDFLAGS)" -o $(BIN_DIR)/$(AGENT_BINARY)-linux-arm64 ./cmd/agent

# Cross-compile for macOS. The Agent installer is Linux-only, but the binaries
# are useful for local development and running the Web Backend on macOS.
build-darwin:
	@echo "Building for macOS amd64 and arm64..."
	@mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(WEB_BINARY)-darwin-amd64 ./cmd/web
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(AGENT_LDFLAGS)" -o $(BIN_DIR)/$(AGENT_BINARY)-darwin-amd64 ./cmd/agent
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(WEB_BINARY)-darwin-arm64 ./cmd/web
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(AGENT_LDFLAGS)" -o $(BIN_DIR)/$(AGENT_BINARY)-darwin-arm64 ./cmd/agent

# Build agent binaries with filenames expected by the install handler (/api/agent/binary)
# Injects VERSION and BUILD_TIME via ldflags and writes agent-version.txt for the VersionCache.
build-agent-release:
	@echo "Building Agent release v$(VERSION) for Linux and macOS amd64/arm64..."
	@mkdir -p $(BIN_DIR)
	@echo "$(VERSION)" > $(BIN_DIR)/agent-version.txt
	GOOS=linux GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(AGENT_LDFLAGS)" -o $(BIN_DIR)/$(AGENT_BINARY)-linux-amd64 ./cmd/agent
	GOOS=linux GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(AGENT_LDFLAGS)" -o $(BIN_DIR)/$(AGENT_BINARY)-linux-arm64 ./cmd/agent
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(AGENT_LDFLAGS)" -o $(BIN_DIR)/$(AGENT_BINARY)-darwin-amd64 ./cmd/agent
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -ldflags "$(AGENT_LDFLAGS)" -o $(BIN_DIR)/$(AGENT_BINARY)-darwin-arm64 ./cmd/agent

# Release build: produces all artifacts needed for deployment
release: build-linux build-darwin
	@echo "$(VERSION)" > $(BIN_DIR)/agent-version.txt
	@echo "Release build complete. Artifacts in $(BIN_DIR)/"

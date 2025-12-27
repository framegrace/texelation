# Build/test helper for Texelation project

BIN_DIR := bin
CACHE_DIR := $(CURDIR)/.cache
GO_ENV := CCACHE_DISABLE=1 GOCACHE=$(CACHE_DIR) CGO_ENABLED=0
CLIENT_PKG := ./client/cmd/texel-client
SERVER_PKG := ./cmd/texel-server

# Core apps needed for normal texelation operation
CORE_APPS := texelterm help

# All standalone app binaries in cmd/
ALL_APPS := texelterm help app-runner texel-stress texelui-demo texelui-demo2 colorpicker-demo

.PHONY: build install run test lint fmt tidy clean help server client release build-apps


build: ## Build texel-server, texel-client, texelation, and core app binaries into bin/
	@mkdir -p $(BIN_DIR) $(CACHE_DIR)
	$(GO_ENV) go build -o $(BIN_DIR)/texelterm ./cmd/texelterm
	$(GO_ENV) go build -o $(BIN_DIR)/help ./cmd/help
	$(GO_ENV) go build -o $(BIN_DIR)/texel-server $(SERVER_PKG)
	$(GO_ENV) go build -o $(BIN_DIR)/texel-client $(CLIENT_PKG)
	$(GO_ENV) go build -o $(BIN_DIR)/texelation ./cmd/texelation

build-apps: ## Build ALL app binaries into bin/
	@mkdir -p $(BIN_DIR) $(CACHE_DIR)
	$(GO_ENV) go build -o $(BIN_DIR)/texelterm ./cmd/texelterm
	$(GO_ENV) go build -o $(BIN_DIR)/help ./cmd/help
	$(GO_ENV) go build -o $(BIN_DIR)/app-runner ./cmd/app-runner
	$(GO_ENV) go build -o $(BIN_DIR)/texel-stress ./cmd/texel-stress
	$(GO_ENV) go build -o $(BIN_DIR)/texelui-demo ./cmd/texelui-demo
	$(GO_ENV) go build -o $(BIN_DIR)/texelui-demo2 ./cmd/texelui-demo2
	$(GO_ENV) go build -o $(BIN_DIR)/colorpicker-demo ./cmd/colorpicker-demo
	$(GO_ENV) go build -o $(BIN_DIR)/texel-server $(SERVER_PKG)
	$(GO_ENV) go build -o $(BIN_DIR)/texel-client $(CLIENT_PKG)
	$(GO_ENV) go build -o $(BIN_DIR)/texelation ./cmd/texelation

install: ## Install texel binaries into GOPATH/bin
	@mkdir -p $(CACHE_DIR)
	$(GO_ENV) go install $(SERVER_PKG) $(CLIENT_PKG) ./cmd/texelation

run: ## Launch texel-server (for manual smoke testing)
	@mkdir -p $(CACHE_DIR)
	$(GO_ENV) go run $(SERVER_PKG) --socket /tmp/texelation.sock

test: ## Execute all Go tests
	@mkdir -p $(CACHE_DIR)
	$(GO_ENV) go test ./...

lint: ## Run go vet for static analysis
	@mkdir -p $(CACHE_DIR)
	$(GO_ENV) go vet ./...

fmt: ## Format Go sources in the module
	gofmt -w $(shell go list -f '{{.Dir}}' ./...)

tidy: ## Sync go.mod with source imports
	@mkdir -p $(CACHE_DIR)
	$(GO_ENV) go mod tidy

server: ## Run texel-server harness
	@mkdir -p $(CACHE_DIR)
	$(GO_ENV) go run $(SERVER_PKG) --socket /tmp/texelation.sock --verbose-logs

client: ## Run remote texel client against socket
	@mkdir -p $(CACHE_DIR)
	$(GO_ENV) go run $(CLIENT_PKG) --socket /tmp/texelation.sock

release: ## Cross-compile binaries for common platforms into dist/
	./scripts/build-release.sh

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) $(CACHE_DIR) dist

help: ## Print available targets
	@grep -E '^[a-zA-Z_-]+:.*?##' Makefile | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "%-10s %s\n", $$1, $$2}'

# Build/test helper for Texelation project

BIN_DIR := bin
CACHE_DIR := $(CURDIR)/.cache
GO_ENV := CCACHE_DISABLE=1 GOCACHE=$(CACHE_DIR) CGO_ENABLED=0
CLIENT_PKG := ./client/cmd/texel-client
SERVER_PKG := ./cmd/texel-server

APPS := texelterm welcome

.PHONY: build install run test lint fmt tidy clean help server client release build-apps


build: ## Build texel-server and texel-client binaries into bin/

build-apps: ## Build standalone app binaries into bin/
	@mkdir -p $(BIN_DIR) $(CACHE_DIR)
	$(GO_ENV) go build -o $(BIN_DIR)/texelterm ./cmd/texelterm
	$(GO_ENV) go build -o $(BIN_DIR)/welcome ./cmd/welcome
	@mkdir -p $(BIN_DIR) $(CACHE_DIR)
	$(GO_ENV) go build -o $(BIN_DIR)/texel-server $(SERVER_PKG)
	$(GO_ENV) go build -o $(BIN_DIR)/texel-client $(CLIENT_PKG)

install: ## Install texel binaries into GOPATH/bin
	@mkdir -p $(CACHE_DIR)
	$(GO_ENV) go install $(SERVER_PKG) $(CLIENT_PKG)

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

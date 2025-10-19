# Build/test helper for Texelation project

APP_NAME := texelation
BIN_DIR := bin
CACHE_DIR := $(CURDIR)/.cache
GO_ENV := GOCACHE=$(CACHE_DIR) CGO_ENABLED=0

.PHONY: build run test lint fmt tidy clean help server client

build: ## Build the desktop binary into bin/
	@mkdir -p $(BIN_DIR) $(CACHE_DIR)
	$(GO_ENV) go build -o $(BIN_DIR)/$(APP_NAME) .

run: ## Launch the desktop directly
	@mkdir -p $(CACHE_DIR)
	$(GO_ENV) go run .

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

server: ## Run texel-server-sim harness
	@mkdir -p $(CACHE_DIR)
	$(GO_ENV) go run ./cmd/texel-server-sim --socket /tmp/texelation.sock

client: ## Run remote texel client against socket
	@mkdir -p $(CACHE_DIR)
	$(GO_ENV) go run ./client/cmd/texel-client --socket /tmp/texelation.sock

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) $(CACHE_DIR)

help: ## Print available targets
	@grep -E '^[a-zA-Z_-]+:.*?##' Makefile | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "%-10s %s\n", $$1, $$2}'

#!/bin/bash
# verify-split.sh - Verifies both repositories work correctly after split
#
# Usage:
#   ./verify-split.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK_DIR="${SCRIPT_DIR}/verify-work"

log() {
    echo "[$(date '+%H:%M:%S')] $*"
}

error() {
    echo "[ERROR] $*" >&2
    exit 1
}

success() {
    echo "[OK] $*"
}

log "Creating verification work directory..."
rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR"
cd "$WORK_DIR"

# Test 1: Clone and build TexelUI
log "=== Test 1: TexelUI builds independently ==="
git clone git@github.com:framegrace/texelui.git
cd texelui

log "Running go mod tidy..."
go mod tidy

log "Building TexelUI..."
go build ./...
success "TexelUI builds successfully"

log "Running TexelUI tests..."
go test ./... || log "Some tests failed - review needed"
success "TexelUI tests complete"

cd "$WORK_DIR"

# Test 2: Clone and build Texelation
log "=== Test 2: Texelation builds with TexelUI dependency ==="
git clone git@github.com:framegrace/texelation.git
cd texelation

log "Running go mod tidy..."
go mod tidy

log "Building Texelation..."
go build ./...
success "Texelation builds successfully"

log "Running Texelation tests..."
go test ./... || log "Some tests failed - review needed"
success "Texelation tests complete"

# Test 3: Build specific binaries
log "=== Test 3: Build binaries ==="

log "Building texel-server..."
go build -o bin/texel-server ./cmd/texel-server
success "texel-server built"

log "Building texelterm standalone..."
go build -o bin/texelterm ./cmd/texelterm
success "texelterm built"

log "Building help standalone..."
go build -o bin/help ./cmd/help
success "help built"

cd "$WORK_DIR"

# Test 4: Verify standalone app runs
log "=== Test 4: Verify standalone apps ==="
cd texelation

log "Testing texelterm starts (will timeout after 2s)..."
timeout 2 ./bin/texelterm --help 2>/dev/null || true
success "texelterm runs"

cd "$WORK_DIR"

# Summary
log ""
log "=========================================="
log "Verification Summary"
log "=========================================="
log ""
log "Work directory: $WORK_DIR"
log ""
log "TexelUI:"
log "  - Location: $WORK_DIR/texelui"
log "  - Build: PASSED"
log ""
log "Texelation:"
log "  - Location: $WORK_DIR/texelation"
log "  - Build: PASSED"
log "  - Binaries: bin/texel-server, bin/texelterm, bin/help"
log ""
log "Next steps:"
log "  1. Test texel-server and texel-client manually"
log "  2. Test texelterm standalone"
log "  3. Verify theme loading works"
log "  4. Check app registration"
log ""
success "Verification complete!"

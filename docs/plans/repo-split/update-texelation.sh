#!/bin/bash
# update-texelation.sh - Updates Texelation to use TexelUI as a dependency
#
# Prerequisites:
#   - TexelUI repo must exist at github.com/framegrace/texelui
#   - Run from within the texelation repository
#
# Usage:
#   ./update-texelation.sh [--dry-run]

set -euo pipefail

DRY_RUN=false
if [[ "${1:-}" == "--dry-run" ]]; then
    DRY_RUN=true
    echo "=== DRY RUN MODE ==="
fi

log() {
    echo "[$(date '+%H:%M:%S')] $*"
}

error() {
    echo "[ERROR] $*" >&2
    exit 1
}

# Must be run from repo root
[[ -f "go.mod" ]] || error "Must be run from repository root (where go.mod is)"
[[ -d "texel" ]] || error "texel directory not found"

# Check we're in the right repo
grep -q "^module texelation$" go.mod || error "This doesn't look like the texelation repo"

log "Creating feature branch..."
if [[ "$DRY_RUN" == "false" ]]; then
    git checkout -b feature/use-texelui-library || git checkout feature/use-texelui-library
fi

log "Updating go.mod..."
if [[ "$DRY_RUN" == "false" ]]; then
    cat > go.mod << 'EOF'
module github.com/framegrace/texelation

go 1.24.3

require (
	github.com/creack/pty v1.1.24
	github.com/framegrace/texelui v0.0.0
	github.com/gdamore/tcell/v2 v2.8.1
	github.com/google/uuid v1.6.0
	github.com/mattn/go-runewidth v0.0.16
	golang.org/x/term v0.28.0
)

require (
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/text v0.27.0 // indirect
)

replace github.com/veops/go-ansiterm => ./localmods/github.com/veops/go-ansiterm
EOF
fi

log "Removing files that moved to TexelUI..."
FILES_TO_REMOVE=(
    "texel/app.go"
    "texel/cell.go"
    "texel/control_bus.go"
    "texel/control_bus_test.go"
    "texel/storage.go"
    "texel/storage_test.go"
    "texel/cards"
    "texel/theme"
    "config"
    "texelui"
    "internal/devshell"
)

for path in "${FILES_TO_REMOVE[@]}"; do
    if [[ -e "$path" ]]; then
        log "  Removing: $path"
        if [[ "$DRY_RUN" == "false" ]]; then
            git rm -rf "$path"
        fi
    fi
done

log "Updating import paths in all Go files..."

# Function to update imports in a file
update_imports() {
    local file="$1"
    sed -i \
        -e 's|"texelation/texel"|"github.com/framegrace/texelation/texel"|g' \
        -e 's|"texelation/protocol"|"github.com/framegrace/texelation/protocol"|g' \
        -e 's|"texelation/registry"|"github.com/framegrace/texelation/registry"|g' \
        -e 's|"texelation/apps/|"github.com/framegrace/texelation/apps/|g' \
        -e 's|"texelation/client"|"github.com/framegrace/texelation/client"|g' \
        -e 's|"texelation/internal/|"github.com/framegrace/texelation/internal/|g' \
        "$file"
}

# Update imports to use texelui
update_texelui_imports() {
    local file="$1"
    sed -i \
        -e 's|"texelation/texel/theme"|"github.com/framegrace/texelui/theme"|g' \
        -e 's|"texelation/texel/cards"|"github.com/framegrace/texelui/cards"|g' \
        -e 's|"texelation/config"|"github.com/framegrace/texelui/config"|g' \
        -e 's|"texelation/texelui/core"|"github.com/framegrace/texelui/ui/core"|g' \
        -e 's|"texelation/texelui/widgets"|"github.com/framegrace/texelui/ui/widgets"|g' \
        -e 's|"texelation/texelui/scroll"|"github.com/framegrace/texelui/ui/scroll"|g' \
        -e 's|"texelation/texelui/layout"|"github.com/framegrace/texelui/ui/layout"|g' \
        -e 's|"texelation/texelui/primitives"|"github.com/framegrace/texelui/ui/primitives"|g' \
        -e 's|"texelation/texelui/adapter"|"github.com/framegrace/texelui/ui/adapter"|g' \
        -e 's|"texelation/texelui/color"|"github.com/framegrace/texelui/ui/color"|g' \
        -e 's|"texelation/internal/devshell"|"github.com/framegrace/texelui/runner"|g' \
        "$file"
}

# For files that reference texel.Cell, texel.App - need to add import alias
# texel package in texelation still exists but core types moved to texelui/core
update_core_references() {
    local file="$1"

    # Check if file uses texel.Cell, texel.App, etc.
    if grep -q 'texel\.Cell\|texel\.App\|texel\.ControlBus\|texel\.NewControlBus\|texel\.AppStorage\|texel\.StorageService' "$file"; then
        # Need to add texelui/core import and update references
        # This is complex - might need manual review

        # First, check if file already imports texelation/texel
        if grep -q '"texelation/texel"' "$file" || grep -q '"github.com/framegrace/texelation/texel"' "$file"; then
            # Add texelui/core import alongside
            sed -i \
                -e '/import/,/)/{
                    /^import/a\
	texelcore "github.com/framegrace/texelui/core"
                }' \
                "$file" 2>/dev/null || true

            # Update references to use texelcore
            sed -i \
                -e 's/texel\.Cell/texelcore.Cell/g' \
                -e 's/texel\.App/texelcore.App/g' \
                -e 's/texel\.ControlBus/texelcore.ControlBus/g' \
                -e 's/texel\.NewControlBus/texelcore.NewControlBus/g' \
                -e 's/texel\.AppStorage/texelcore.AppStorage/g' \
                -e 's/texel\.StorageService/texelcore.StorageService/g' \
                -e 's/texel\.StorageSetter/texelcore.StorageSetter/g' \
                -e 's/texel\.AppStorageSetter/texelcore.AppStorageSetter/g' \
                -e 's/texel\.PasteHandler/texelcore.PasteHandler/g' \
                -e 's/texel\.SelectionHandler/texelcore.SelectionHandler/g' \
                -e 's/texel\.SelectionDeclarer/texelcore.SelectionDeclarer/g' \
                -e 's/texel\.MouseWheelHandler/texelcore.MouseWheelHandler/g' \
                -e 's/texel\.MouseWheelDeclarer/texelcore.MouseWheelDeclarer/g' \
                -e 's/texel\.CloseRequester/texelcore.CloseRequester/g' \
                -e 's/texel\.CloseCallbackRequester/texelcore.CloseCallbackRequester/g' \
                -e 's/texel\.ControlBusProvider/texelcore.ControlBusProvider/g' \
                -e 's/texel\.PaneIDSetter/texelcore.PaneIDSetter/g' \
                -e 's/texel\.RenderPipeline/texelcore.RenderPipeline/g' \
                -e 's/texel\.PipelineProvider/texelcore.PipelineProvider/g' \
                -e 's/texel\.SnapshotProvider/texelcore.SnapshotProvider/g' \
                -e 's/texel\.SnapshotFactory/texelcore.SnapshotFactory/g' \
                "$file"
        fi
    fi
}

if [[ "$DRY_RUN" == "false" ]]; then
    # Update all Go files
    find . -name "*.go" -type f | while read -r file; do
        update_imports "$file"
        update_texelui_imports "$file"
        update_core_references "$file"
    done
else
    log "DRY RUN: Would update imports in all .go files"
fi

log "Files needing manual review (use core types from texel package):"
grep -rn 'texel\.Cell\|texel\.App\|texel\.ControlBus' --include="*.go" . 2>/dev/null | head -20 || true

log "Updating defaults directory reference in theme loader..."
# The theme loader may need to look for defaults in texelui package
# This will need manual attention based on how defaults are loaded

log "Running go mod tidy..."
if [[ "$DRY_RUN" == "false" ]]; then
    go mod tidy || log "go mod tidy failed - TexelUI may not be published yet"
fi

log "Running tests..."
if [[ "$DRY_RUN" == "false" ]]; then
    go test ./... || log "Tests failed - needs investigation"
fi

log "Summary of changes:"
if [[ "$DRY_RUN" == "false" ]]; then
    git status
fi

log ""
log "Next steps:"
log "1. Review the changes: git diff --staged"
log "2. Check for any remaining 'texelation/texel' imports that reference moved types"
log "3. Fix any compilation errors"
log "4. Run tests: make test"
log "5. Commit: git add -A && git commit -m 'Use TexelUI as external dependency'"
log "6. Create PR: git push -u origin feature/use-texelui-library"

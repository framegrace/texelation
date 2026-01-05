#!/bin/bash
# create-texelui.sh - Creates the TexelUI repository from texelation
#
# Prerequisites:
#   - git-filter-repo installed (pip install git-filter-repo)
#   - GitHub CLI (gh) installed and authenticated
#   - Write access to github.com/framegrace
#
# Usage:
#   ./create-texelui.sh [--dry-run]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK_DIR="${SCRIPT_DIR}/work"
SOURCE_REPO="git@github.com:framegrace/texelation.git"
TARGET_REPO="github.com/framegrace/texelui"
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

# Check prerequisites
command -v git-filter-repo >/dev/null 2>&1 || error "git-filter-repo not found. Install with: pip install git-filter-repo"
command -v gh >/dev/null 2>&1 || error "GitHub CLI (gh) not found. Install from: https://cli.github.com/"

log "Creating work directory..."
rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR"
cd "$WORK_DIR"

log "Cloning texelation repository..."
git clone --no-local "$SOURCE_REPO" texelui-extract
cd texelui-extract

log "Running git-filter-repo to extract TexelUI files..."
# Extract only the files that will become TexelUI
git filter-repo \
    --path texel/app.go \
    --path texel/cell.go \
    --path texel/control_bus.go \
    --path texel/control_bus_test.go \
    --path texel/storage.go \
    --path texel/storage_test.go \
    --path texel/cards/ \
    --path texel/theme/ \
    --path config/ \
    --path texelui/ \
    --path internal/devshell/ \
    --path defaults/ \
    --force

log "Reorganizing directory structure..."

# Create new directory structure
mkdir -p core cards theme ui runner

# Move core files
if [[ -f texel/app.go ]]; then
    mv texel/app.go core/
    mv texel/cell.go core/
    mv texel/control_bus.go core/
    [[ -f texel/control_bus_test.go ]] && mv texel/control_bus_test.go core/
    mv texel/storage.go core/
    [[ -f texel/storage_test.go ]] && mv texel/storage_test.go core/
fi

# Move cards
if [[ -d texel/cards ]]; then
    mv texel/cards/* cards/
    rmdir texel/cards
fi

# Move theme
if [[ -d texel/theme ]]; then
    mv texel/theme/* theme/
    rmdir texel/theme
fi

# Clean up empty texel directory
rmdir texel 2>/dev/null || true

# Move texelui to ui
if [[ -d texelui ]]; then
    mv texelui ui
fi

# Move devshell to runner
if [[ -d internal/devshell ]]; then
    mv internal/devshell/* runner/
    rm -rf internal
fi

log "Creating go.mod..."
cat > go.mod << 'EOF'
module github.com/framegrace/texelui

go 1.24.3

require (
	github.com/gdamore/tcell/v2 v2.8.1
	github.com/mattn/go-runewidth v0.0.16
)

require (
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/text v0.27.0 // indirect
)
EOF

log "Updating import paths in all Go files..."

# Function to update imports in a file
update_imports() {
    local file="$1"
    # Update various import patterns
    sed -i \
        -e 's|"texelation/texel"|"github.com/framegrace/texelui/core"|g' \
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

# Update all Go files
find . -name "*.go" -type f | while read -r file; do
    update_imports "$file"
done

# Update package declarations where needed
# core/ files should be package core
for file in core/*.go; do
    [[ -f "$file" ]] && sed -i 's/^package texel$/package core/' "$file"
done

# cards/ files should be package cards (already correct)
# theme/ files should be package theme (already correct)
# config/ files should be package config (already correct)

# runner/ files should be package runner
for file in runner/*.go; do
    [[ -f "$file" ]] && sed -i 's/^package devshell$/package runner/' "$file"
done

log "Updating type references..."
# In adapter and other files, update texel.Cell to core.Cell, texel.App to core.App
find . -name "*.go" -type f | while read -r file; do
    sed -i \
        -e 's/texel\.Cell/core.Cell/g' \
        -e 's/texel\.App/core.App/g' \
        -e 's/texel\.ControlBus/core.ControlBus/g' \
        -e 's/texel\.NewControlBus/core.NewControlBus/g' \
        -e 's/texel\.AppStorage/core.AppStorage/g' \
        -e 's/texel\.StorageService/core.StorageService/g' \
        "$file"
done

log "Creating README.md..."
cat > README.md << 'EOF'
# TexelUI

A terminal UI library for building text-based applications in Go.

## Features

- **Core primitives**: App interface, Cell type, ControlBus for event signaling
- **Theme system**: Semantic colors, palettes, app-specific overrides
- **Configuration**: JSON-based config with hot-reload support
- **Widget library**: Button, Input, Checkbox, ComboBox, TextArea, ColorPicker, etc.
- **Scroll support**: ScrollPane with smooth scrolling
- **Layout managers**: VBox, HBox for widget positioning
- **Cards pipeline**: Composable rendering stages with effects
- **Standalone runner**: Run apps outside the desktop environment

## Installation

```bash
go get github.com/framegrace/texelui
```

## Quick Start

```go
package main

import (
    "github.com/framegrace/texelui/core"
    "github.com/framegrace/texelui/runner"
    "github.com/gdamore/tcell/v2"
)

type MyApp struct {
    width, height int
    stopCh        chan struct{}
    refresh       chan<- bool
}

func (a *MyApp) Run() error        { <-a.stopCh; return nil }
func (a *MyApp) Stop()             { close(a.stopCh) }
func (a *MyApp) Resize(w, h int)   { a.width, a.height = w, h }
func (a *MyApp) GetTitle() string  { return "My App" }
func (a *MyApp) SetRefreshNotifier(ch chan<- bool) { a.refresh = ch }
func (a *MyApp) HandleKey(ev *tcell.EventKey) {}
func (a *MyApp) Render() [][]core.Cell {
    // Return a 2D buffer of cells
    buf := make([][]core.Cell, a.height)
    for y := range buf {
        buf[y] = make([]core.Cell, a.width)
        for x := range buf[y] {
            buf[y][x] = core.Cell{Ch: ' ', Style: tcell.StyleDefault}
        }
    }
    return buf
}

func main() {
    app := &MyApp{stopCh: make(chan struct{})}
    runner.Run(func(args []string) (core.App, error) {
        return app, nil
    }, nil)
}
```

## License

AGPL-3.0-or-later

## Related Projects

- [Texelation](https://github.com/framegrace/texelation) - Text desktop environment using TexelUI
EOF

log "Committing reorganization..."
git add -A
git commit -m "Reorganize as standalone TexelUI library

- Move core primitives (App, Cell, ControlBus, Storage) to core/
- Move cards pipeline to cards/
- Move theme system to theme/
- Move config system to config/
- Move widget library to ui/
- Move standalone runner to runner/
- Update all import paths to github.com/framegrace/texelui
- Update package declarations

This is the initial split from texelation to create an independent UI library.
" || true

if [[ "$DRY_RUN" == "true" ]]; then
    log "DRY RUN: Would create GitHub repo and push"
    log "Directory structure:"
    find . -type f -name "*.go" | head -30
    log "..."
else
    log "Creating GitHub repository..."
    gh repo create framegrace/texelui --public --description "Terminal UI library for Go" || true

    log "Setting remote and pushing..."
    git remote remove origin 2>/dev/null || true
    git remote add origin git@github.com:framegrace/texelui.git
    git branch -M main
    git push -u origin main --force
fi

log "Done! TexelUI repository created."
log "Work directory: $WORK_DIR/texelui-extract"

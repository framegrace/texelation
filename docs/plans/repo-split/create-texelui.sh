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
    --path texel/theme/ \
    --path texelui/ \
    --path apps/texeluicli/ \
    --path apps/texelui-demo/ \
    --path cmd/texelui/ \
    --path cmd/texelui-demo/ \
    --path docs/texelui/ \
    --path docs/TEXELUI_ARCHITECTURE_REVIEW.md \
    --path docs/TEXELUI_QUICKSTART.md \
    --path docs/TEXELUI_THEME.md \
    --path texelui_cli_demo.sh \
    --path LICENSE \
    --path CODE_OF_CONDUCT.md \
    --force

log "Reorganizing directory structure..."

# Create new directory structure
mkdir -p core theme

# Move core files
if [[ -f texel/app.go ]]; then
    mv texel/app.go core/
    mv texel/cell.go core/
    mv texel/control_bus.go core/
    [[ -f texel/control_bus_test.go ]] && mv texel/control_bus_test.go core/
    mv texel/storage.go core/
fi

# Move theme
if [[ -d texel/theme ]]; then
    mv texel/theme/* theme/
    rmdir texel/theme
fi

# Drop Texelation-only per-app overrides
rm -f theme/app_overrides.go

# Clean up empty texel directory
rmdir texel 2>/dev/null || true

# Flatten texelui/* packages into repository root
if [[ -d texelui ]]; then
    for dir in core widgets scroll layout primitives adapter color; do
        if [[ -d "texelui/$dir" ]]; then
            mkdir -p "$dir"
            mv "texelui/$dir"/* "$dir"/
            rmdir "texelui/$dir"
        fi
    done
    rmdir texelui 2>/dev/null || true
fi

log "Creating go.mod..."
cat > go.mod << 'EOF'
module github.com/framegrace/texelui

go 1.24.3

require (
	github.com/gdamore/tcell/v2 v2.8.1
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
        -e 's|"texelation/texelui/core"|"github.com/framegrace/texelui/core"|g' \
        -e 's|"texelation/texelui/widgets"|"github.com/framegrace/texelui/widgets"|g' \
        -e 's|"texelation/texelui/scroll"|"github.com/framegrace/texelui/scroll"|g' \
        -e 's|"texelation/texelui/layout"|"github.com/framegrace/texelui/layout"|g' \
        -e 's|"texelation/texelui/primitives"|"github.com/framegrace/texelui/primitives"|g' \
        -e 's|"texelation/texelui/adapter"|"github.com/framegrace/texelui/adapter"|g' \
        -e 's|"texelation/texelui/color"|"github.com/framegrace/texelui/color"|g' \
        -e 's|"texelation/texelui/widgets/colorpicker"|"github.com/framegrace/texelui/widgets/colorpicker"|g' \
        -e 's|"texelation/apps/texeluicli"|"github.com/framegrace/texelui/apps/texeluicli"|g' \
        -e 's|"texelation/apps/texelui-demo"|"github.com/framegrace/texelui/apps/texelui-demo"|g' \
        "$file"
}

# Update all Go files
find . -name "*.go" -type f | while read -r file; do
    update_imports "$file"
done

log "Deduping core imports..."
find . -name "*.go" -type f -print0 | xargs -0 perl -0pi -e 's/\n(\s*"github.com\/framegrace\/texelui\/core")\n\s*"github.com\/framegrace\/texelui\/core"/\n$1/g'

log "Updating docs import paths..."
if [[ -d docs ]]; then
    find docs -name "*.md" -type f | while read -r file; do
        sed -i \
            -e 's|texelation/texelui/|github.com/framegrace/texelui/|g' \
            -e 's|texelation/texel/theme|github.com/framegrace/texelui/theme|g' \
            -e 's|texelation/texel|github.com/framegrace/texelui/core|g' \
            "$file"
    done
fi
log "Reminder: update docs for standalone runner (standalone imports, RunUI, DisableMouse) and build instructions."

# Update package declarations where needed
# core/ files should be package core
for file in core/*.go; do
    [[ -f "$file" ]] && sed -i 's/^package texel$/package core/' "$file"
done

# theme/ files should be package theme (already correct)

log "Ensuring theme defaults are saved on first load..."
if [[ -f theme/theme.go ]] && ! grep -q "ApplyDefaults(instance)" theme/theme.go; then
    perl -0pi -e 's/(instance\.LoadStandardSemantics\(\)\n)/$1\n\t\tif loadErr == nil {\n\t\t\tApplyDefaults(instance)\n\t\t}\n/' theme/theme.go
fi

log "Updating type references..."
# Update texel.* references to core.* outside the core package
find . -name "*.go" -type f ! -path "./core/*" | while read -r file; do
    sed -i 's/texel\./core./g' "$file"
done

# Clean up core package self-references/imports
find core -name "*.go" -type f | while read -r file; do
    if grep -q '^package core$' "$file"; then
        sed -i \
            -e 's/core\.//g' \
            -e '/"github.com\/framegrace\/texelui\/core"/d' \
            "$file"
    fi
done

# Remove leftover texel.* qualifiers inside core package
find core -name "*.go" -type f | while read -r file; do
    sed -i 's/texel\.//g' "$file"
done

log "Adding standalone runner..."
mkdir -p standalone
cat > standalone/runner.go << 'EOF'
// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: standalone/runner.go
// Summary: Standalone runner for TexelUI apps without Texelation.

package standalone

import (
	"fmt"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/framegrace/texelui/adapter"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/theme"
)

// Builder constructs a core.App, optionally using CLI args.
type Builder func(args []string) (core.App, error)

// Options controls the standalone runner behavior.
type Options struct {
	ExitKey      tcell.Key
	DisableMouse bool
	OnInit       func(screen tcell.Screen)
	OnExit       func()
}

var (
	screenFactory = tcell.NewScreen
	registryMu    sync.RWMutex
	registry      = map[string]Builder{}

	exitMu     sync.Mutex
	activeExit chan struct{}
)

// Register adds a builder to the standalone registry.
func Register(name string, builder Builder) {
	if name == "" || builder == nil {
		return
	}
	registryMu.Lock()
	registry[name] = builder
	registryMu.Unlock()
}

// RunApp runs a registered app by name.
func RunApp(name string, args []string) error {
	registryMu.RLock()
	builder := registry[name]
	registryMu.RUnlock()
	if builder == nil {
		return fmt.Errorf("standalone: unknown app %q", name)
	}
	return RunWithOptions(builder, Options{}, args...)
}

// Run runs a core.App builder in a standalone terminal session.
func Run(builder Builder, args ...string) error {
	return RunWithOptions(builder, Options{}, args...)
}

// RunWithOptions runs a core.App builder with custom options.
func RunWithOptions(builder Builder, opts Options, args ...string) error {
	if builder == nil {
		return fmt.Errorf("standalone: nil builder")
	}
	app, err := builder(args)
	if err != nil {
		return err
	}
	return runApp(app, opts)
}

// RunUI runs a UIManager directly in a standalone terminal session.
func RunUI(ui *core.UIManager) error {
	return RunUIWithOptions(ui, Options{})
}

// RunUIWithOptions runs a UIManager with custom options.
func RunUIWithOptions(ui *core.UIManager, opts Options) error {
	app := adapter.NewUIApp("", ui)
	return runApp(app, opts)
}

// RequestExit signals the active runner (if any) to exit.
func RequestExit() {
	exitMu.Lock()
	ch := activeExit
	exitMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// SetScreenFactory overrides the screen factory used by the runner.
func SetScreenFactory(factory func() (tcell.Screen, error)) {
	if factory == nil {
		screenFactory = tcell.NewScreen
		return
	}
	screenFactory = factory
}

func normalizeOptions(opts Options) Options {
	if opts.ExitKey == 0 {
		opts.ExitKey = tcell.KeyEscape
	}
	return opts
}

func runApp(app core.App, opts Options) error {
	opts = normalizeOptions(opts)

	exitMu.Lock()
	activeExit = make(chan struct{}, 1)
	exitMu.Unlock()
	defer func() {
		exitMu.Lock()
		activeExit = nil
		exitMu.Unlock()
	}()

	screen, err := screenFactory()
	if err != nil {
		return fmt.Errorf("init screen: %w", err)
	}
	if err := screen.Init(); err != nil {
		return fmt.Errorf("screen init: %w", err)
	}
	defer screen.Fini()

	if opts.OnInit != nil {
		opts.OnInit(screen)
	}
	if !opts.DisableMouse {
		screen.EnableMouse(tcell.MouseMotionEvents)
		defer screen.DisableMouse()
	}
	screen.EnablePaste()

	_ = theme.Get()
	if err := theme.GetLoadError(); err != nil {
		return fmt.Errorf("theme: %w", err)
	}

	width, height := screen.Size()
	app.Resize(width, height)
	refreshCh := make(chan bool, 1)
	app.SetRefreshNotifier(refreshCh)

	draw := func() {
		screen.Clear()
		buffer := app.Render()
		if buffer != nil {
			for y := 0; y < len(buffer); y++ {
				row := buffer[y]
				for x := 0; x < len(row); x++ {
					cell := row[x]
					screen.SetContent(x, y, cell.Ch, nil, cell.Style)
				}
			}
		}
		screen.Show()
	}

	draw()

	runErr := make(chan error, 1)
	go func() {
		runErr <- app.Run()
	}()
	defer app.Stop()

	go func() {
		for range refreshCh {
			screen.PostEvent(tcell.NewEventInterrupt(nil))
		}
	}()

	var pasteBuffer []byte
	var inPaste bool

	for {
		select {
		case err := <-runErr:
			if opts.OnExit != nil {
				opts.OnExit()
			}
			return err
		case <-activeExit:
			if opts.OnExit != nil {
				opts.OnExit()
			}
			return nil
		default:
		}

		ev := screen.PollEvent()
		switch tev := ev.(type) {
		case *tcell.EventInterrupt:
			draw()
		case *tcell.EventResize:
			w, h := tev.Size()
			app.Resize(w, h)
			draw()
		case *tcell.EventPaste:
			if tev.Start() {
				inPaste = true
				pasteBuffer = nil
			} else if tev.End() {
				inPaste = false
				if ph, ok := app.(interface{ HandlePaste([]byte) }); ok && len(pasteBuffer) > 0 {
					ph.HandlePaste(pasteBuffer)
					draw()
				}
				pasteBuffer = nil
			}
		case *tcell.EventKey:
			if tev.Key() == opts.ExitKey || tev.Key() == tcell.KeyCtrlC {
				if opts.OnExit != nil {
					opts.OnExit()
				}
				return nil
			}
			if inPaste {
				if tev.Key() == tcell.KeyRune {
					pasteBuffer = append(pasteBuffer, []byte(string(tev.Rune()))...)
				} else if tev.Key() == tcell.KeyEnter || tev.Key() == 10 {
					pasteBuffer = append(pasteBuffer, '\n')
				}
			} else {
				app.HandleKey(tev)
				draw()
			}
		case *tcell.EventMouse:
			if mh, ok := app.(interface{ HandleMouse(*tcell.EventMouse) }); ok {
				mh.HandleMouse(tev)
				draw()
			}
		}
	}
}
EOF

log "Updating texelui-demo entrypoint..."
cat > cmd/texelui-demo/main.go << 'EOF'
package main

import (
	"flag"
	"log"

	"github.com/framegrace/texelui/apps/texelui-demo"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/standalone"
)

func main() {
	flag.Parse()
	standalone.Register("texelui-demo", func(args []string) (core.App, error) {
		return texeluidemo.New(), nil
	})
	if err := standalone.RunApp("texelui-demo", flag.Args()); err != nil {
		log.Fatalf("texelui-demo: %v", err)
	}
}
EOF

log "Cleaning duplicate core imports..."
if [[ -f adapter/texel_app.go ]]; then
    perl -0pi -e 's/\n\t\"github.com\/framegrace\/texelui\/core\"\n\t\"github.com\/framegrace\/texelui\/core\"/\n\t\"github.com\/framegrace\/texelui\/core\"/s' adapter/texel_app.go
fi

log "Updating texelui_cli_demo.sh instructions..."
if [[ -f texelui_cli_demo.sh ]]; then
    sed -i 's/make build-apps/go build -o bin\\/texelui .\\/cmd\\/texelui/' texelui_cli_demo.sh
fi

log "Creating README.md..."
cat > README.md << 'EOF'
# TexelUI

A terminal UI library for building text-based applications in Go.

## Features

- **Core primitives**: App interface, Cell type, ControlBus, storage interfaces
- **Theme system**: Semantic colors + palettes, shared config path (`~/.config/texelation/theme.json`)
- **Widget library**: Button, Input, Checkbox, ComboBox, TextArea, ColorPicker, etc.
- **Layouts + scrolling**: VBox, HBox, ScrollPane, primitives
- **Texelation integration**: UIApp adapter for embedding in the desktop
- **Standalone tools**: TexelUI CLI + bash adaptor, demo app

## Installation

```bash
go get github.com/framegrace/texelui
```

## Quick Start

```bash
# Run the widget showcase demo
go run ./cmd/texelui-demo

# Use the CLI (server + bash adaptor)
go run ./cmd/texelui --help
```

## Embedding in Texelation

```go
ui := core.NewUIManager()
app := adapter.NewUIApp("My App", ui)
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
- Move theme system to theme/
- Flatten texelui/* packages to repo root
- Move TexelUI CLI + demo into this repo
- Drop Texelation-only theme overrides
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

# Repository Split Plan: TexelUI + Texelation

**Created**: 2026-01-04
**Status**: Planned (not executed)

## Overview

Split the current `texelation` repository into two separate repositories:

1. **TexelUI** (`github.com/framegrace/texelui`) - UI library with app primitives
2. **Texelation** (`github.com/framegrace/texelation`) - Text desktop environment

TexelUI will be a dependency of Texelation, with its own development cycle.

## Goals

- Preserve git history in both repositories using `git-filter-repo`
- Clean separation of concerns
- TexelUI can be used independently of Texelation
- Apps can run standalone (via runner) or inside Texelation desktop

## Repository Structure After Split

### TexelUI (`github.com/framegrace/texelui`)

```
github.com/framegrace/texelui/
├── core/               # Core app primitives
│   ├── app.go          # App interface + optional interfaces
│   ├── cell.go         # Cell type (rune + tcell.Style)
│   ├── control_bus.go  # ControlBus for event signaling
│   └── storage.go      # Storage interfaces (StorageService, AppStorage)
├── cards/              # Rendering pipeline system
│   ├── card.go         # Card interface
│   ├── pipeline.go     # Pipeline implementation
│   ├── adapter.go      # App-to-Card adapter
│   ├── effect_card.go  # Effect cards
│   ├── rainbow_card.go
│   ├── alternating_card.go
│   └── buffer.go       # Buffer utilities
├── theme/              # Theme system
│   ├── theme.go        # Core theme loading
│   ├── color.go        # Color resolution
│   ├── palette.go      # Color palettes
│   ├── semantics.go    # Semantic colors
│   ├── defaults.go     # Default theme
│   ├── overrides.go    # Override system
│   └── app_overrides.go # Per-app overrides
├── config/             # Configuration system
│   ├── config.go       # Core config
│   ├── store.go        # Persistence
│   ├── paths.go        # Config paths
│   ├── types.go        # Type definitions
│   ├── clone.go        # Deep clone
│   ├── migrate.go      # Migration
│   ├── defaults.go     # Default values
│   └── embedded.go     # Embedded defaults
├── ui/                 # Widget library
│   ├── core/           # UIManager, Widget interface, focus
│   ├── widgets/        # Button, Input, Checkbox, etc.
│   ├── scroll/         # ScrollPane, ScrollState
│   ├── layout/         # VBox, HBox layout managers
│   ├── primitives/     # Low-level primitives
│   ├── adapter/        # UIApp adapter (UIManager → texel.App)
│   └── color/          # Color utilities (OKLCH)
├── runner/             # Standalone app runner
│   └── runner.go       # Run apps outside Texelation
├── defaults/           # Default configuration files
│   └── theme.json
└── go.mod              # module github.com/framegrace/texelui
```

### Texelation (`github.com/framegrace/texelation`)

```
github.com/framegrace/texelation/
├── texel/              # Desktop engine core
│   ├── desktop_engine_core.go
│   ├── desktop_engine_control_mode.go
│   ├── pane.go
│   ├── tree.go
│   ├── workspace.go
│   ├── dispatcher.go        # EventDispatcher + desktop events
│   ├── storage_service.go   # StorageService implementation
│   ├── buffer_store.go
│   ├── runtime_interfaces.go
│   ├── snapshot.go
│   ├── snapshot_restore.go
│   ├── layout_transitions.go
│   ├── overlay.go
│   ├── app_lifecycle.go
│   ├── focus_listener.go
│   ├── pane_state_listener.go
│   └── driver_tcell.go
├── apps/               # Built-in applications
│   ├── texelterm/      # Terminal emulator
│   ├── help/           # Help viewer
│   ├── launcher/       # App launcher
│   ├── statusbar/      # Status bar
│   ├── clock/          # Clock widget
│   └── configeditor/   # Config editor
├── protocol/           # Binary client/server protocol
│   ├── protocol.go
│   ├── messages.go
│   └── buffer_delta.go
├── internal/
│   └── runtime/        # Server/client runtime
│       ├── server/     # Server implementation
│       └── client/     # Client implementation
├── client/             # Client buffer cache
│   └── buffercache.go
├── registry/           # App registry
├── cmd/                # Entry points
│   ├── texel-server/
│   ├── texel-client/
│   ├── texelterm/
│   └── help/
└── go.mod              # module github.com/framegrace/texelation
```

## File Mapping

### Files Moving to TexelUI

| Current Path | New Path in TexelUI |
|--------------|---------------------|
| `texel/app.go` | `core/app.go` |
| `texel/cell.go` | `core/cell.go` |
| `texel/control_bus.go` | `core/control_bus.go` |
| `texel/control_bus_test.go` | `core/control_bus_test.go` |
| `texel/storage.go` | `core/storage.go` |
| `texel/storage_test.go` | `core/storage_test.go` |
| `texel/cards/` | `cards/` |
| `texel/theme/` | `theme/` |
| `config/` | `config/` |
| `texelui/core/` | `ui/core/` |
| `texelui/widgets/` | `ui/widgets/` |
| `texelui/scroll/` | `ui/scroll/` |
| `texelui/layout/` | `ui/layout/` |
| `texelui/primitives/` | `ui/primitives/` |
| `texelui/adapter/` | `ui/adapter/` |
| `texelui/color/` | `ui/color/` |
| `internal/devshell/` | `runner/` |
| `defaults/` | `defaults/` |

### Files Staying in Texelation

- `texel/desktop_engine_*.go`
- `texel/pane.go`
- `texel/tree.go`
- `texel/workspace.go`
- `texel/dispatcher.go`
- `texel/storage_service.go`
- `texel/buffer_store*.go`
- `texel/runtime_interfaces.go`
- `texel/snapshot*.go`
- `texel/layout_transitions.go`
- `texel/overlay.go`
- `texel/app_lifecycle.go`
- `texel/*_listener.go`
- `texel/driver_tcell.go`
- `apps/`
- `protocol/`
- `internal/runtime/`
- `client/`
- `registry/`
- `cmd/`

## Import Changes

### In TexelUI

Module path: `github.com/framegrace/texelui`

Internal imports will use:
- `github.com/framegrace/texelui/core`
- `github.com/framegrace/texelui/cards`
- `github.com/framegrace/texelui/theme`
- `github.com/framegrace/texelui/config`
- `github.com/framegrace/texelui/ui/core`
- `github.com/framegrace/texelui/ui/widgets`
- etc.

### In Texelation

Module path: `github.com/framegrace/texelation`

Import changes:
| Old Import | New Import |
|------------|------------|
| `texelation/texel` (for App, Cell) | `github.com/framegrace/texelui/core` |
| `texelation/texel/theme` | `github.com/framegrace/texelui/theme` |
| `texelation/config` | `github.com/framegrace/texelui/config` |
| `texelation/texelui/...` | `github.com/framegrace/texelui/ui/...` |
| `texelation/internal/devshell` | `github.com/framegrace/texelui/runner` |
| `texelation/texel/cards` | `github.com/framegrace/texelui/cards` |

## Dependencies

### TexelUI Dependencies
- `github.com/gdamore/tcell/v2` (terminal handling)
- Standard library only

### Texelation Dependencies
- `github.com/framegrace/texelui` (the UI library)
- `github.com/gdamore/tcell/v2`
- `github.com/creack/pty` (for texelterm)
- `github.com/google/uuid`
- `github.com/mattn/go-runewidth`
- `golang.org/x/term`

## Execution Steps

### Step 1: Create TexelUI Repository

Run `./create-texelui.sh`:
1. Clone texelation repo
2. Use git-filter-repo to extract relevant paths
3. Reorganize directory structure
4. Update go.mod and imports
5. Push to new GitHub repo

### Step 2: Update Texelation Repository

Run `./update-texelation.sh`:
1. Create feature branch
2. Add texelui dependency
3. Update imports throughout
4. Remove files that moved
5. Run tests
6. Create PR

### Step 3: Verification

Run `./verify-split.sh`:
1. Build TexelUI independently
2. Build Texelation with dependency
3. Run all tests
4. Test standalone app runner
5. Test full desktop

## Scripts

See the accompanying scripts in this directory:
- `create-texelui.sh` - Creates the TexelUI repository
- `update-texelation.sh` - Updates Texelation to use TexelUI
- `verify-split.sh` - Verifies both repos work correctly

## Rollback Plan

If issues are found:
1. TexelUI repo can be deleted (it's new)
2. Texelation changes are on a feature branch
3. Original main branch is untouched until PR merged

## Future Work

After this split:
1. **Split texelterm**: Could become its own repo (`github.com/framegrace/texelterm`)
2. **Protocol library**: If needed for other projects
3. **Effects library**: The effects system could be extracted

## Notes

- The `localmods/` directory with `go-ansiterm` stays in Texelation (used by texelterm)
- Test files move with their corresponding source files
- Documentation files (CLAUDE.md, README.md) need to be split appropriately

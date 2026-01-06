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
- TexelUI CLI/demo run standalone without Texelation runtime dependencies

## Repository Structure After Split

### TexelUI (`github.com/framegrace/texelui`)

```
github.com/framegrace/texelui/
├── core/               # App primitives + UIManager + widget core
│   ├── app.go          # App interface + optional interfaces
│   ├── cell.go         # Cell type (rune + tcell.Style)
│   ├── control_bus.go  # ControlBus for event signaling
│   ├── storage.go      # Storage interfaces (StorageService, AppStorage)
│   └── uimanager.go    # UIManager, focus, rendering core
├── theme/              # Theme system (shared config path)
│   ├── theme.go        # Core theme loading
│   ├── color.go        # Color resolution
│   ├── palette.go      # Color palettes
│   ├── semantics.go    # Semantic colors
│   ├── defaults.go     # Default theme
│   └── overrides.go    # Override system (no per-app overrides)
├── adapter/            # UIApp adapter (UIManager → core.App)
├── runtime/            # Runtime runner (UIManager/core.App)
├── widgets/            # Button, Input, Checkbox, etc.
├── scroll/             # ScrollPane, ScrollState
├── layout/             # VBox, HBox layout managers
├── primitives/         # Low-level primitives
├── color/              # Color utilities (OKLCH)
├── apps/               # TexelUI apps
│   ├── texeluicli/     # CLI server/client
│   └── texelui-demo/   # Standalone demo app
├── cmd/                # Entry points
│   ├── texelui/
│   └── texelui-demo/
├── docs/               # Documentation
│   ├── texelui/
│   └── TEXELUI_*.md
├── texelui_cli_demo.sh # Bash adaptor demo
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
├── config/             # System/app configuration
├── defaults/           # Embedded defaults (texelation.json + app configs)
├── protocol/           # Binary client/server protocol
│   ├── protocol.go
│   ├── messages.go
│   └── buffer_delta.go
├── internal/
│   ├── runtimeadapter/ # Texelation runtime adapter harness
│   ├── effects/        # Effect registry + implementations
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
| `texel/theme/` | `theme/` |
| `texelui/core/` | `core/` |
| `texelui/widgets/` | `widgets/` |
| `texelui/scroll/` | `scroll/` |
| `texelui/layout/` | `layout/` |
| `texelui/primitives/` | `primitives/` |
| `texelui/adapter/` | `adapter/` |
| `texelui/color/` | `color/` |
| `apps/texeluicli/` | `apps/texeluicli/` |
| `cmd/texelui/` | `cmd/texelui/` |
| `apps/texelui-demo/` | `apps/texelui-demo/` |
| `cmd/texelui-demo/` | `cmd/texelui-demo/` |
| `docs/texelui/` | `docs/texelui/` |
| `docs/TEXELUI_*.md` | `docs/TEXELUI_*.md` |
| `texelui_cli_demo.sh` | `texelui_cli_demo.sh` |

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
- `texel/cards/`
- `apps/`
- `config/`
- `defaults/`
- `protocol/`
- `internal/runtimeadapter/`
- `internal/effects/`
- `internal/runtime/`
- `client/`
- `registry/`
- `cmd/`

## Import Changes

### In TexelUI

Module path: `github.com/framegrace/texelui`

Internal imports will use:
- `github.com/framegrace/texelui/core`
- `github.com/framegrace/texelui/theme`
- `github.com/framegrace/texelui/widgets`
- `github.com/framegrace/texelui/layout`
- `github.com/framegrace/texelui/scroll`
- `github.com/framegrace/texelui/primitives`
- `github.com/framegrace/texelui/adapter`
- `github.com/framegrace/texelui/color`
- etc.

### In Texelation

Module path: `github.com/framegrace/texelation`

Import changes:
| Old Import | New Import |
|------------|------------|
| `texelation/texel` (for App, Cell) | `github.com/framegrace/texelui/core` |
| `texelation/texel/theme` | `github.com/framegrace/texelui/theme` (base theme API) |
| `texelation/texelui/...` | `github.com/framegrace/texelui/...` |
| `texelation/texel/theme.ForApp` | `texelation/internal/theming.ForApp` (per-app overrides) |

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
3. Reorganize directory structure (flatten texelui/ into root packages, move app primitives into core/)
4. Move theme package and drop per-app overrides
5. Update go.mod and imports to `github.com/framegrace/texelui/...`
6. Ensure CLI/demo are Texelation-independent
7. Push to new GitHub repo

### Step 2: Update Texelation Repository

Run `./update-texelation.sh`:
1. Create feature branch
2. Add texelui dependency
3. Update imports throughout
4. Remove files that moved
5. Add Texelation-only theming helper for per-app overrides (e.g., `internal/theming.ForApp`)
6. Update app call sites to use the Texelation-only theming helper
7. Run tests
8. Create PR

### Step 3: Verification

Run `./verify-split.sh`:
1. Build TexelUI independently
2. Build Texelation with dependency
3. Run all tests
4. Verify TexelUI CLI/demo run standalone (no runtime adapter/texel-app deps)
5. Test full desktop + theme defaults creation

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
- Theme defaults/palettes should exist in both repos and be copied/saved if the user theme file is missing
- Per-app theme overrides remain Texelation-only (not in TexelUI theme package)

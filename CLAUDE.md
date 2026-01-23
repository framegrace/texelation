# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Texelation is a modular text-based desktop environment running as a client/server pair. The server hosts apps and manages the pane graph, while the client renders buffers and routes user input over a binary protocol.

**External Dependency**: Core UI primitives (App, Cell, Widget, ControlBus) are provided by [TexelUI](https://github.com/framegrace/texelui) and re-exported via `texel/core_aliases.go`.

## CRITICAL: Git Workflow

**NEVER commit directly to main.** Always use feature branches and pull requests:

```bash
git checkout main && git pull
git checkout -b feature/my-feature   # or fix/, refactor/
# ... make changes ...
git add -A && git commit -m "Description"
git push -u origin feature/my-feature
# Create PR on GitHub to merge into main
```

This rule has no exceptions. All changes must go through PR review.

## Build and Development Commands

### Building
```bash
make build          # Build core binaries (texel-server, texel-client, texelation, texelterm, help)
make build-apps     # Build ALL standalone app binaries (includes config-editor, stress test, etc.)
make install        # Install binaries into GOPATH/bin
make release        # Cross-compile for Linux, macOS, Windows (amd64 + arm64) into dist/
```

### Running Locally
```bash
make server         # Start server on /tmp/texelation.sock
make client         # Launch remote client (run in separate terminal)
```

Both use shared build cache in `.cache/` to avoid polluting `$GOCACHE`.

### Testing
```bash
make test           # Run all unit tests (excludes integration tests)
go test -tags=integration ./internal/runtime/server -run TestOfflineRetentionAndResumeWithMemConn
                    # Run specific integration test with tag
```

### Other Commands
```bash
make fmt            # Format all Go sources
make lint           # Run go vet
make tidy           # Update go.mod dependencies
make clean          # Remove bin/, dist/, .cache/
```

## Architecture

### Client/Server Split
- **Server** (`internal/runtime/server`) - Authoritative pane tree and buffers; accepts connections, broadcasts diffs, persists snapshots
- **Client** (`client/` and `internal/runtime/client`) - tcell-based renderer; applies buffered diffs to local cache, handles input routing
- **Protocol** (`protocol/`) - Binary protocol with versioned framing (magic `0x54584c01`), CRC32 checksums, message types for handshake, snapshots, deltas, events

### Core Components
- **Desktop Engine** (`texel/desktop_engine_core.go`) - Central window manager; coordinates workspaces, status panes, animation system, effects pipeline, event dispatcher, clipboard
- **Workspace** (`texel/workspace.go`) - Workspace management with tiling compositor via tree-based pane layout
- **Pane** (`texel/pane.go`) - Window container hosting a single App; handles borders, focus, resize
- **App** (`texel/core_aliases.go`) - Interface re-exported from TexelUI; defines embeddable applications (Run, Stop, Resize, Render, HandleKey)
- **Tree** (`texel/tree.go`) - Recursive tiling compositor with hsplit/vsplit nodes and animated layout transitions
- **Effects** (`internal/effects/`) - Registry-based overlay system (fadeTint, rainbow, flash, zoom); wired through theme JSON bindings

### Apps
Located in `apps/`. TexelApps come in two forms:

**Embedded-only apps** - Run only inside texelation server, no standalone binary:
- **launcher** - App launcher dialog
- **statusbar** - Status display pane
- **clock** - Clock widget
- **configeditor** - Theme/config editor

**Standalone apps** - Can run inside texelation OR independently via `cmd/<app>` entrypoints:
- **texelterm** - Full terminal emulator with VT parser (`apps/texelterm/term.go`, `apps/texelterm/parser/`)
- **help** - Help viewer

Standalone apps have a corresponding `cmd/<appname>/main.go` that uses `github.com/framegrace/texelui/runtime` to run them outside texelation. Use `make build-apps` to build all standalone binaries.

### Session Management
- **DesktopPublisher** (`internal/runtime/server/desktop_publisher.go`) - Bridges Desktop events to protocol messages; generates buffer deltas
- **SnapshotStore** (`internal/runtime/server/snapshot_store.go`) - Periodic persistence of pane tree and metadata with integrity checks
- **Session** (`internal/runtime/server/session.go`) - Per-connection state; handles handshake, queues updates when offline, resumes with snapshot + diffs

### Key Interfaces
- **ScreenDriver** (`texel/runtime_interfaces.go`) - Abstracts rendering backend (tcell, headless test harness)
- **BufferStore** (`texel/runtime_interfaces.go`) - Stores pane buffers by ID; supports diff generation for client updates
- **AppLifecycleManager** (`texel/runtime_interfaces.go`) - Manages app start/stop, resize propagation
- **EventDispatcher** (`texel/dispatcher.go`) - Pub/sub for workspace/pane events wiring effects

## Effect System

Visual overlays are defined entirely through `theme.json`:
```json
"effects": {
  "bindings": [
    {"event": "pane.active", "target": "pane", "effect": "fadeTint"},
    {"event": "workspace.control", "target": "workspace", "effect": "rainbow"}
  ]
}
```

Effect implementations register themselves at import time via `effects.Register(id, factory)`. Supported effects:
- **fadeTint** - Fade tint overlay on pane state changes
- **rainbow** - Rainbow animation across workspace
- **flash** - Flash effect (used for visual bell in terminal)
- **zoom** - Zoom animation (legacy, needs migration)

## Protocol Design

### Message Flow
1. Client connects -> `MsgHello` (client version)
2. Server -> `MsgWelcome` (negotiated version)
3. Client -> `MsgConnectRequest` or `MsgResumeRequest` (with session ID + last ack'd sequence)
4. Server -> `MsgConnectAccept` + `MsgTreeSnapshot`
5. Ongoing: `MsgBufferDelta` (server -> client), `MsgKeyEvent`/`MsgMouseEvent` (client -> server)
6. Client disconnects -> server queues updates; on reconnect, client receives snapshot + accumulated diffs

### Snapshot & Delta Strategy
- **TreeSnapshot** - Full pane tree with IDs, rects, borders, focus state
- **BufferDelta** - Row-based patches with run-length encoded cell styles; applied to client's BufferCache
- Diffs queued server-side when no clients attached; retention limits prevent unbounded growth

## File Organization

- `cmd/texel-server/` - Production server harness with socket listener
- `cmd/texelation/` - Combined local desktop (no client/server split)
- `client/cmd/texel-client/` - tcell-based remote renderer
- `client/cmd/texel-headless/` - Headless client for testing
- `internal/runtime/server/` - Server runtime (connections, sessions, snapshots, desktop publisher)
- `internal/runtime/client/` - Client runtime (rendering loops, handshake, panic recovery)
- `internal/effects/` - Reusable effect implementations (fadeTint, rainbow, keyflash)
- `texel/` - Core desktop primitives (Desktop, Workspace, Pane, Tree, BufferStore, themes)
- `texel/cards/` - Card pipeline system for dialog overlays
- `apps/` - Embeddable applications shared by server/client
- `protocol/` - Binary protocol definitions and serialization

## Development Notes

- **Go Version**: 1.24.3
- **Testing**: Table-driven tests in `_test.go` files; integration tests under `integration` build tag
- **Formatting**: `gofmt` with tabs for indentation
- **Commit Style**: Short present-tense (e.g., "Fix backspace visual erase"), subject < 60 chars
- Always pass regression tests before confirming changes
- Commit after every successful change to enable quick rollback

## Testing Visual Bugs in Texelterm

Visual glitches in texelterm often pass unit tests because `Grid()` returns correct data, but the actual rendered output is wrong due to dirty line tracking issues.

### The Problem
The terminal only re-renders rows marked as "dirty". If a bug causes:
1. Content written to the wrong logical position
2. Which maps to a different physical row than the cursor
3. Only the cursor's row gets marked dirty
4. The affected row never gets re-rendered -> **visual glitch**

### Solution: Simulate the Render Flow
Tests must simulate the actual render path with dirty tracking:

```go
// Create render buffer (what user sees)
renderBuf := make([][]Cell, height)
for y := range renderBuf {
    renderBuf[y] = make([]Cell, width)
}

// Simulate render: ONLY update dirty rows
simulateRender := func() {
    dirtyLines, allDirty := v.DirtyLines()
    vtermGrid := v.Grid()
    if allDirty {
        for y := 0; y < height && y < len(vtermGrid); y++ {
            copy(renderBuf[y], vtermGrid[y])
        }
    } else {
        for y := range dirtyLines {
            if y >= 0 && y < height && y < len(vtermGrid) {
                copy(renderBuf[y], vtermGrid[y])
            }
        }
    }
    v.ClearDirty()
}

// After each action, verify renderBuf matches Grid()
simulateRender()
grid := v.Grid()
for y := 0; y < height; y++ {
    if cellsToString(renderBuf[y]) != cellsToString(grid[y]) {
        t.Errorf("Row %d: renderBuf != Grid (visual glitch!)", y)
    }
}
```

### Manual Debug Testing
When tests pass but visual bugs persist:
```bash
rm -f /tmp/texelterm-debug.log
TEXELTERM_DEBUG=1 ./bin/texelterm 2>/dev/null
# Reproduce the issue, then check:
cat /tmp/texelterm-debug.log | grep -E "(RENDER|LOGICALX)"
```

### Reference Tests
See `apps/texelterm/parser/display_buffer_integration_test.go`:
- `TestDisplayBuffer_BashReadlineWrapWithCR` - Wrap + CR behavior
- `TestDisplayBuffer_WrapDirtyTrackingRegression` - Step-by-step dirty tracking verification
- `TestDisplayBuffer_RenderFlowWithWrap` - Basic render flow simulation

### Terminal Comparison Framework (`apps/texelterm/testutil/`)

A comprehensive test framework for detecting visual bugs in texelterm. **The reference terminal comparison tool is the PRIMARY method for debugging visual issues.**

**Full documentation:** See `docs/VISUAL_DEBUGGING_GUIDE.md`

**Claude Command:** Use `/debug-visual <command>` to automatically debug visual issues.

**Files:**
- `reference.go` - **PRIMARY TOOL**: Reference terminal comparison using tmux as ground truth
- `ansi_parser.go` - Parses ANSI sequences from tmux to extract colors/attributes
- `json_output.go` - JSON serialization for machine-parseable output
- `live_capture.go` - Live capture via `TEXELTERM_CAPTURE` environment variable
- `recorder.go` - TXREC01 format, shell capture via `script` command
- `replayer.go` - VTerm replay engine with dirty tracking simulation
- `comparator.go` - Grid comparison, diff detection, issue reporting
- `format.go` - Grid/escape sequence formatting utilities

### Enhanced Reference Terminal Comparison (PRIMARY VISUAL TESTING METHOD)

Compares texelterm's output against tmux including **full color and attribute comparison**. **Use this first when debugging any visual bug.**

**Quick Usage with Full Color Support:**
```go
// Create a recording with the problematic escape sequences
rec := testutil.NewRecording(80, 24)
rec.AppendCSI("48;5;240m")  // Grey background
rec.AppendText("Working...")
rec.AppendCSI("0m")          // Reset

// Compare with full color support
cmp, err := testutil.NewReferenceComparator(rec)
result, err := cmp.CompareAtEndWithFullDiff()

if !result.Match {
    // Shows all differences including colors
    fmt.Println(testutil.FormatEnhancedResult(result))

    // Filter by type (char, fg, bg, attr, combined)
    colorDiffs := testutil.FilterDiffsByType(result.Differences,
        testutil.DiffTypeFG, testutil.DiffTypeBG)
    for _, diff := range colorDiffs {
        fmt.Printf("(%d,%d): %s\n", diff.X, diff.Y, diff.DiffDesc)
    }
}
```

**Get JSON for Automated Analysis:**
```go
jsonBytes, err := cmp.CompareWithFullDiffToJSON()
fmt.Println(string(jsonBytes))
// Output includes: match, summary, differences with positions, colors, attributes
```

**Finding First Divergence Point:**
```go
// For complex sequences, find exactly where output first diverges
divergence, err := cmp.FindFirstDivergenceWithFullDiff(50) // check every 50 bytes

if divergence != nil {
    fmt.Printf("Divergence at bytes %d-%d\n", divergence.ByteIndex, divergence.ByteEndIndex)
    fmt.Println(testutil.FormatEnhancedDivergence(divergence))
}
```

**Live Capture:**
```bash
# Capture a live session
TEXELTERM_CAPTURE=/tmp/session.txrec ./bin/texelterm
# Reproduce the bug, exit
# Analyze with comparison tools
```

**Key Functions:**
- `NewReferenceComparator(rec)` - Create comparator (requires tmux in PATH)
- `CompareAtEndWithFullDiff()` - Full comparison including colors/attributes
- `FindFirstDivergenceWithFullDiff(chunkSize)` - Find exact divergence point
- `CompareWithFullDiffToJSON()` - Get JSON output for automation
- `FilterDiffsByType(diffs, types...)` - Filter differences by type
- `FormatEnhancedResult(r)` - Human-readable result
- `FormatEnhancedDivergence(d)` - Human-readable divergence

**Reference Tests:** See `apps/texelterm/testutil/testutil_test.go` and `enhanced_comparison_test.go`:
- `TestReferenceCompareWithColorsBasic` - Color comparison
- `TestReferenceCompareGreyBackground` - Grey background handling
- `TestJSONOutputFormat` - JSON output validation
- `TestANSIParser*` - ANSI parsing tests

### Recording and Replay (Secondary Method)

For testing dirty tracking and render simulation without tmux:

**Recording Sessions:**
```go
// Capture a shell command's PTY output
rec, err := testutil.CaptureCommand("codex", 80, 24)
rec.Save("codex_session.txrec")

// Or load an existing recording
rec, err := testutil.LoadRecording("codex_session.txrec")
```

**Creating Synthetic Tests:**
```go
rec := testutil.NewRecording(80, 24)
rec.AppendText("Hello")
rec.AppendCSI("31m")  // Red foreground
rec.AppendText("Red")
rec.AppendCSI("0m")   // Reset
rec.AppendCRLF()
```

**Replay with Dirty Tracking:**
```go
replayer := testutil.NewReplayer(rec)
replayer.PlayAndRender()  // Simulates actual renderer behavior

// Detect visual bugs (Grid != RenderBuf)
if replayer.HasVisualMismatch() {
    mismatches := replayer.FindVisualMismatches()
    // Each mismatch shows: position, rendered cell, logical cell
}
```

**Comparison and Formatting:**
```go
// Compare two grids
result := testutil.CompareGrids(expected, actual)
fmt.Println(testutil.FormatLineByLine(result))

// Side-by-side diff
fmt.Println(testutil.FormatSideBySide(grid1, grid2, 40))

// Debug escape sequences
fmt.Println(testutil.EscapeSequenceLog(rec.Sequences))
```

## Documentation

- `docs/CLIENT_SERVER_ARCHITECTURE.md` - Client/server runtime, data flow
- `docs/EFFECTS_GUIDE.md` - How effects are implemented and configured
- `docs/TEXEL_APP_GUIDE.md` - How to build apps
- `docs/TERMINAL_PERSISTENCE_ARCHITECTURE.md` - Scrollback, environment, history persistence
- `docs/plans/LONG_LINE_EDITOR_PLAN.md` - Long line editor overlay (not started)

## Important Patterns

### Adding a New App
See `docs/TEXEL_APP_GUIDE.md` for the end-to-end workflow. In summary:
1. Create a package under `apps/<name>/` implementing the App interface (from texel package).
2. Return the app directly from `New()` - no pipeline wrapper needed.
3. If the app needs a ControlBus, create one with `texel.NewControlBus()` and implement `ControlBusProvider`.
4. If the app needs card pipelines (for dialogs, effects), implement `PipelineProvider` to expose the internal pipeline.
5. Register the factory in the server harness (`cmd/texel-server/main.go`).

**Simple app pattern** (most apps):
```go
func New() texel.App {
    return &MyApp{
        controlBus: texel.NewControlBus(),
    }
}
```

**Pipeline app pattern** (apps needing card interception):
```go
func New() texel.App {
    app := &MyApp{}
    app.pipeline = cards.NewPipeline(nil, cards.WrapApp(app), myDialogCard)
    return app
}

func (a *MyApp) Pipeline() texel.RenderPipeline { return a.pipeline }
```

**Making an app standalone** (runnable outside texelation):
1. Create `cmd/<appname>/main.go` that uses `github.com/framegrace/texelui/runtime`
2. Add the build target to `Makefile` under `build-apps`
3. The runtime package provides terminal setup, event handling, and lifecycle management

```go
// cmd/myapp/main.go
package main

import (
    "github.com/framegrace/texelation/apps/myapp"
    "github.com/framegrace/texelui/core"
    "github.com/framegrace/texelui/runtime"
)

func main() {
    builder := func(_ []string) (core.App, error) {
        return myapp.New(), nil
    }
    runtime.Run(builder)
}
```

### Adding a New Effect
Follow `docs/EFFECTS_GUIDE.md`. Highlights:
1. Implement `effects.Effect`, register it in `init()`.
2. Parse configuration via `EffectConfig`; support theme and card usage.
3. Reuse `timeline.go` for animations and `helpers.go` for tint blending.
4. Add unit tests and update documentation.

### Modifying Protocol
1. Add new `MessageType` constant in `protocol/protocol.go`
2. Define message struct in `protocol/messages.go`
3. Update encoder/decoder in protocol package
4. Handle in `internal/runtime/server/connection.go` (server) and client loop
5. Bump protocol version if breaking; maintain backward compatibility path

### Testing Client/Server Flow
Use `internal/runtime/server/testutil/memconn.go` for in-memory connection testing without Unix sockets. See integration tests in `internal/runtime/server/*_test.go` for patterns.

---

## Completed Features

### Layout Transitions (Server-Side)
Server-side animation system that animates SplitRatios over time, broadcasting tree snapshots at 60fps.

**Implementation**: `texel/layout_transitions.go` (~200 lines)

**How It Works**:
- **Split**: New pane starts at 1% of space, animates to final ratio
- **Close**: Closing pane shrinks to 1%, then removed
- Each frame: Updates ratios -> recalculateLayout() -> broadcasts tree snapshot
- Client receives snapshots and renders normally

**Configuration** (`theme.json`):
```json
"layout_transitions": {
  "duration_ms": 300,
  "easing": "smoothstep",
  "enabled": true
}
```

**Easing Functions**: `linear`, `smoothstep`, `ease-in-out`, `spring`

**Hot Reload**: Send `kill -HUP $(pidof texel-server)` after editing theme.

**Related Files**: `texel/layout_transitions.go`, `texel/desktop_engine_core.go`, `texel/workspace.go`

---

### Scrollback Reflow (Three-Level Architecture)
Separates storage from display for efficient reflow on resize. See `docs/TERMINAL_PERSISTENCE_ARCHITECTURE.md` for full architecture.

**Architecture**:
```
Disk History (TXHIST02) -> Scrollback History (~5000 lines) -> Display Buffer (viewport)
```

**Key Files**:
- `apps/texelterm/parser/disk_history.go` - TXHIST02 indexed format
- `apps/texelterm/parser/scrollback_history.go` - Memory window with disk backing
- `apps/texelterm/parser/display_buffer.go` - Physical lines at current width
- `apps/texelterm/parser/logical_line.go` - Width-independent line storage
- `apps/texelterm/parser/vterm_display_buffer.go` - VTerm integration

**Usage**:
```go
err := v.EnableDisplayBufferWithDisk(diskPath, DisplayBufferOptions{
    MaxMemoryLines: 5000,
    MarginAbove:    200,
    MarginBelow:    50,
})
```

**Performance**: Resize is O(viewport) not O(history).

## Related Projects

- [TexelUI](https://github.com/framegrace/texelui) - Terminal UI library providing core primitives (App, Cell, Widget, ControlBus)

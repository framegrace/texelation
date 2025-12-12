# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Texelation is a modular text-based desktop environment running as a client/server pair. The server hosts apps and manages the pane graph, while the client renders buffers and routes user input over a binary protocol.

## Build and Development Commands

### Building
```bash
make build          # Build texel-server and texel-client binaries into bin/
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
- **Server** (`internal/runtime/server`) – Authoritative pane tree and buffers; accepts connections, broadcasts diffs, persists snapshots
- **Client** (`client/` and `internal/runtime/client`) – tcell-based renderer; applies buffered diffs to local cache, handles input routing
- **Protocol** (`protocol/`) – Binary protocol with versioned framing (magic `0x54584c01`), CRC32 checksums, message types for handshake, snapshots, deltas, events

### Core Components
- **Desktop** (`texel/desktop.go`) – Central window manager; coordinates workspaces (Screens), status panes, animation system, effects pipeline, event dispatcher, clipboard
- **Screen** (`texel/screen.go`) – Workspace management with tiling compositor via tree-based pane layout
- **Pane** (`texel/pane.go`) – Window container hosting a single App; handles borders, focus, resize
- **App** (`texel/app.go`) – Interface for embeddable applications (Run, Stop, Resize, Render, HandleKey); returns `[][]Cell` buffers
- **Tree** (`texel/tree.go`) – Recursive tiling compositor with hsplit/vsplit nodes and animated layout transitions
- **Effects** (`internal/effects/`) – Registry-based overlay system (fadeTint, rainbow, flash, zoom); wired through theme JSON bindings

### Apps
Located in `apps/`:
- **texelterm** – Full terminal emulator with VT parser (`apps/texelterm/term.go`, `apps/texelterm/parser/`)
- **statusbar** – Status display pane
- **welcome** – Welcome screen
- **clock** – Clock widget

### Session Management
- **DesktopPublisher** (`internal/runtime/server/desktop_publisher.go`) – Bridges Desktop events to protocol messages; generates buffer deltas
- **SnapshotStore** (`internal/runtime/server/snapshot_store.go`) – Periodic persistence of pane tree and metadata with integrity checks
- **Session** (`internal/runtime/server/session.go`) – Per-connection state; handles handshake, queues updates when offline, resumes with snapshot + diffs

### Key Interfaces
- **ScreenDriver** (`texel/runtime_interfaces.go`) – Abstracts rendering backend (tcell, headless test harness)
- **BufferStore** (`texel/buffer_store.go`) – Stores pane buffers by ID; supports diff generation for client updates
- **AppLifecycleManager** (`texel/app_lifecycle.go`) – Manages app start/stop, resize propagation
- **EventDispatcher** (`texel/dispatcher.go`) – Pub/sub for workspace/pane events wiring effects

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
- **fadeTint** – Fade tint overlay on pane state changes
- **rainbow** – Rainbow animation across workspace
- **flash** – Flash effect (used for visual bell in terminal)
- **zoom** – Zoom animation (legacy, needs migration)

## Protocol Design

### Message Flow
1. Client connects → `MsgHello` (client version)
2. Server → `MsgWelcome` (negotiated version)
3. Client → `MsgConnectRequest` or `MsgResumeRequest` (with session ID + last ack'd sequence)
4. Server → `MsgConnectAccept` + `MsgTreeSnapshot`
5. Ongoing: `MsgBufferDelta` (server → client), `MsgKeyEvent`/`MsgMouseEvent` (client → server)
6. Client disconnects → server queues updates; on reconnect, client receives snapshot + accumulated diffs

### Snapshot & Delta Strategy
- **TreeSnapshot** – Full pane tree with IDs, rects, borders, focus state
- **BufferDelta** – Row-based patches with run-length encoded cell styles; applied to client's BufferCache
- Diffs queued server-side when no clients attached; retention limits prevent unbounded growth

## File Organization

- `cmd/texel-server/` – Production server harness with socket listener
- `client/cmd/texel-client/` – tcell-based remote renderer
- `client/cmd/texel-headless/` – Headless client for testing
- `internal/runtime/server/` – Server runtime (connections, sessions, snapshots, desktop publisher)
- `internal/runtime/client/` – Client runtime (rendering loops, handshake, panic recovery)
- `internal/effects/` – Reusable effect implementations (fadeTint, rainbow, keyflash)
- `texel/` – Core desktop primitives (Desktop, Screen, Pane, Tree, App interface, BufferStore, themes)
- `apps/` – Embeddable applications shared by server/client
- `protocol/` – Binary protocol definitions and serialization

## Development Notes

- **Go Version**: 1.24.3
- **Testing**: Table-driven tests in `_test.go` files; integration tests under `integration` build tag
- **Formatting**: `gofmt` with tabs for indentation
- **Commit Style**: Short present-tense (e.g., "Zoom working perfectly"), subject < 60 chars

## Current Branch: feature/fix-scrollback-reflow

For an architectural overview refer to:

- `docs/CLIENT_SERVER_ARCHITECTURE.md` – current client/server runtime, data flow, and open items.
- `docs/EFFECTS_GUIDE.md` – how effects are implemented and configured.
- `docs/TEXEL_APP_GUIDE.md` – how to build pipeline-based apps using cards and the control bus.

## Important Patterns

### Adding a New App
See `docs/TEXEL_APP_GUIDE.md` for the end-to-end workflow. In summary:
1. Create a package under `apps/<name>/` implementing `texel.App`.
2. Build a card pipeline (`cards.WrapApp`, `cards.NewEffectCard`, etc.).
3. Register the factory in the server harness (`cmd/texel-server/main.go`).

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
- Please remeber to always pass refression tests before confirming changes, And commit with an appropiate message (not mentioning claude nor LLMS) after every successful change.
- Commit as we go, to be able to quickly go back on experimets or dead ends.

## Planning Artifacts

- TexelUI plan: see `docs/plans/TEXELUI_PLAN.md`. When working on TexelUI, keep this plan up to date (checklist and sections) and commit changes to it alongside related code. Future sessions should consult and update this file as the source of truth for TexelUI scope, status, and next steps.
- TexelUI Architecture Review: see `docs/TEXELUI_ARCHITECTURE_REVIEW.md`. **[IMPORTANT - NEXT SESSION]** Comprehensive evaluation of TexelUI for form building completed 2025-11-18. Current state: solid low-level foundation but missing high-level primitives for productive form development. **Next steps:** Implement common widgets (Label, Button, Input, Checkbox), layout managers (VBox, HBox, Grid), and form helpers. Priority order and implementation details in review doc. Estimated 2-3 weeks for essential features.
- Long Line Editor plan: see `docs/plans/LONG_LINE_EDITOR_PLAN.md`. Phased implementation of overlay editor for long command lines in texelterm. Update progress and status as work proceeds.
- **Layout Transitions (Server-Side) - COMPLETE (2025-12-07)**:
  - **Status**: Fully implemented and working
  - **Architecture**: Server-side animation system that animates SplitRatios over time, broadcasting tree snapshots at 60fps
  - **Implementation**: `texel/layout_transitions.go` (~200 lines)
  - **Configuration**: Via `theme.json` under `layout_transitions` section (duration_ms, easing, enabled; min_threshold parsed but currently unused)
  - **How It Works**:
    - **Split**: New pane starts at 1% of space, existing panes at 99%, animates to final ratios (e.g., [0.99, 0.01] → [0.5, 0.5])
    - **Close**: Closing pane shrinks from current size to 1%, siblings grow to fill space, then pane is removed
    - Each animation frame: Updates ratios → calls recalculateLayout() → broadcasts tree snapshot
    - Client receives rapid snapshots and renders them normally with proper borders
    - Animation identical to manual resize operations (reuses same code path)
    - Callbacks execute after animation completes (for close, performs actual removal)
  - **Benefits**:
    - Borders render at correct positions (server renders buffers at current animated size)
    - Server controls authoritative tree state throughout animation
    - Client is stateless - just renders snapshots as received
    - Reuses existing snapshot broadcast mechanism
    - Can be disabled/configured per theme without code changes
  - **Related Files**:
    - `texel/layout_transitions.go` - Server-side animator with timeline, easing, 60fps ticker
    - `texel/desktop_engine_core.go` - Initializes manager, parses theme config
    - `texel/workspace.go` - PerformSplit hooks into animator
  - **Configuration Example**:
    ```json
    "layout_transitions": {
      "duration_ms": 300,
      "easing": "smoothstep",
      "enabled": true,
      "min_threshold": 3
    }
    ```
  - **Available Easing Functions**:
    - `linear` - Constant speed, no acceleration
    - `smoothstep` - Smooth acceleration and deceleration (default)
    - `ease-in-out` - Fast in the middle, slow at ends
    - `spring` - Physics-based overshoot and wobble (bouncy, fun!)
  - **Hot Reload**: Configuration is hot-reloadable on SIGHUP
    - Edit `~/.config/texelation/theme.json` (change duration, easing, or enabled)
    - Send `kill -HUP $(pidof texel-server)`
    - New settings apply immediately to future animations
    - Great for live-tuning the spring effect or trying different easings!
  - **Future Enhancements**:
    - Make animations interruptible (currently complete before next action)
    - Animate workspace switches (fade/slide transitions)
    - Animate pane swaps (visual exchange of positions)
    - Add more spring parameters (damping, frequency) to theme config

- **Scrollback Persistence - COMPLETE (2025-12-08)**:
  - **Status**: Fixed and working
  - **Implementation**: `apps/texelterm/parser/history.go`
  - **Issue Fixed**: Scrollback history was persisting empty lines only
  - **Root Cause**: Terminal content updates via `SetLine()` which modifies in-memory buffer but doesn't queue for disk write. Only empty lines from `AppendLine()` were being persisted.
  - **Solution**: Modified `Close()` to rewrite entire circular buffer to disk instead of relying on `pendingLines` queue
  - **Key Changes**:
    - `Close()` (lines 347-380) - Extracts all lines from circular buffer and rewrites history file
    - `rewriteHistoryFile()` (lines 382-416) - Deletes old file, creates new store, writes all lines
  - **Additional Fixes**:
    - Cursor positioning: Terminal now positions cursor at bottom when loading history (vterm.go lines 1149-1166)
    - Margin initialization: Fixed scrolling bug by ensuring margin reset code runs (lines 1214-1217)
    - Tree corruption: Added defensive bounds checking in tree.go resizeNode() to prevent crashes
  - **Related Files**:
    - `apps/texelterm/parser/history.go` - HistoryManager with write-on-close strategy
    - `apps/texelterm/parser/vterm.go` - Resize() with cursor positioning and margin init
    - `texel/tree.go` - Defensive bounds checking for SplitRatios array

- **Pane Loss During Server Restart - FIXED (2025-12-08)**:
  - **Status**: Fully fixed with multiple improvements
  - **Issues Fixed**:
    1. **Race condition in PerformSplit**: Animation was broadcasting tree snapshots while new panes had `app == nil`
    2. **0x0 resize during restore**: Apps were started before layout calculated, causing vterm to be created with 0 dimensions
    3. **Launcher creation conflict**: Launcher was created before snapshot restore, conflicting with restored panes
  - **Solutions**:
    - Moved app creation before `AnimateSplit()` in workspace.go
    - Added `PrepareAppForRestore()` and `StartPreparedApp()` in pane.go to defer app startup until after `recalculateLayout()`
    - Added snapshot existence check in main.go to skip initial Launcher creation
    - Added `ResetGracePeriod()` to layout transitions to skip animations during restore
  - **Key Code Changes**:
    - `texel/pane.go` - New `PrepareAppForRestore()` attaches app without starting; `StartPreparedApp()` starts with correct dimensions
    - `texel/snapshot_restore.go` - Uses deferred app startup pattern: prepare → build tree → layout → start
    - `cmd/texel-server/main.go` - Sets `InitAppName = ""` when snapshot exists
    - `texel/layout_transitions.go` - Added `ResetGracePeriod()` for snapshot restore
  - **Debug Logging**:
    - BOOT logs in server.go track snapshot load/apply flow
    - CaptureTree() warns when panes have nil apps
  - **How It Works Now**:
    1. Server starts, detects snapshot exists, sets InitAppName = ""
    2. SwitchToWorkspace(1) creates empty workspace (no app created)
    3. ApplyTreeCapture runs: prepares apps (no resize/start), builds tree, calculates layout, then starts all apps with correct dimensions
    4. Apps load history and render correctly in their sized panes

- **Scrollback Reflow - MOSTLY COMPLETE (2025-12-12)**:
  - **Status**: Core architecture implemented and working, on-demand loading from disk functional
  - **Full Plan**: `docs/plans/SCROLLBACK_REFLOW_PLAN.md`
  - **Problem Solved**: Separate logical (width-independent) lines from physical (wrapped) display

  - **Architecture Implemented**:
  ```
  ┌─────────────────────────────────────────┐
  │           SCROLLBACK HISTORY            │
  │   (Logical lines - width independent)   │
  │   In-memory cache: 5000 lines           │
  └─────────────────────────────────────────┘
                      │
                      │ Load on-demand from disk
                      ▼
  ┌─────────────────────────────────────────┐
  │         INDEXED HISTORY FILE            │
  │   (TXHIST02 format - random access)     │
  │   Unlimited logical lines on disk       │
  └─────────────────────────────────────────┘
                      │
                      │ Wrap to current width
                      ▼
  ┌─────────────────────────────────────────┐
  │            DISPLAY BUFFER               │
  │   (Physical lines at current width)     │
  │   Grows as needed when scrolling        │
  └─────────────────────────────────────────┘
  ```

  - **Key Components**:
    - `LogicalLine`: Width-independent line with `WrapToWidth(w)` method
    - `ScrollbackHistory`: In-memory cache (5000 lines), supports `PrependLines` for on-demand loading
    - `DisplayBuffer`: Physical lines array, manages viewport, loads more when scrolling
    - `IndexedHistoryFile`: TXHIST02 format with line offset index for O(1) random access
    - `HistoryLoader`: Interface for on-demand loading (`indexedFileLoader`, `historyManagerLoader`)

  - **Indexed History Format (TXHIST02)**:
    ```
    Header (32 bytes): Magic + Flags + LineCount + IndexOffset
    Line Data: CellCount(4) + Cells(16 each: rune+fg+bg+attr)
    Index: uint64 offsets for each line (enables random access)
    ```

  - **On-Demand Loading Flow**:
    1. On startup: Load last 5000 logical lines from indexed file into memory
    2. Create `indexedFileLoader` pointing to remaining lines on disk
    3. When scrolling past in-memory content, `loadFromDisk()` fetches more
    4. Loaded lines wrapped to physical and prepended to DisplayBuffer

  - **Files Created/Modified**:
    - `apps/texelterm/parser/logical_line.go` - LogicalLine with WrapToWidth
    - `apps/texelterm/parser/scrollback_history.go` - ScrollbackHistory cache
    - `apps/texelterm/parser/display_buffer.go` - DisplayBuffer with viewport management
    - `apps/texelterm/parser/history_indexed.go` - TXHIST02 format reader/writer
    - `apps/texelterm/parser/history_loader.go` - HistoryLoader interface and implementations
    - `apps/texelterm/parser/vterm_display_buffer.go` - VTerm integration
    - `apps/texelterm/parser/history.go` - Now writes indexed format
    - `apps/texelterm/parser/history_store.go` - Reads both TXHIST01 and TXHIST02

  - **Remaining Work**:
    - Hook up new terminal output (placeChar/lineFeed) to write to logical lines
    - Currently loads existing history but new output goes through legacy path
    - Test with very long lines (longer than terminal width)
    - Performance tuning for large history files

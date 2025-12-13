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
- **Commit Style**: Short present-tense (e.g., "Fix backspace visual erase"), subject < 60 chars
- Always pass regression tests before confirming changes
- Commit after every successful change to enable quick rollback

## Current Branch: feature/fix-scrollback-reflow

## Documentation

- `docs/CLIENT_SERVER_ARCHITECTURE.md` – Client/server runtime, data flow
- `docs/EFFECTS_GUIDE.md` – How effects are implemented and configured
- `docs/TEXEL_APP_GUIDE.md` – How to build pipeline-based apps using cards
- `docs/TERMINAL_PERSISTENCE_ARCHITECTURE.md` – Scrollback, environment, history persistence
- `docs/plans/TEXELUI_PLAN.md` – TexelUI widget library status
- `docs/plans/LONG_LINE_EDITOR_PLAN.md` – Long line editor overlay (not started)

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

---

## Completed Features

### Layout Transitions (Server-Side)
Server-side animation system that animates SplitRatios over time, broadcasting tree snapshots at 60fps.

**Implementation**: `texel/layout_transitions.go` (~200 lines)

**How It Works**:
- **Split**: New pane starts at 1% of space, animates to final ratio
- **Close**: Closing pane shrinks to 1%, then removed
- Each frame: Updates ratios → recalculateLayout() → broadcasts tree snapshot
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
Disk History (TXHIST02) → Scrollback History (~5000 lines) → Display Buffer (viewport)
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

---

### Pane Loss During Server Restart - Fixed
**Issues Fixed**:
1. Race condition in PerformSplit: Animation broadcasting while panes had nil apps
2. 0x0 resize during restore: Apps started before layout calculated
3. Launcher creation conflict with restored panes

**Solution**: Deferred app startup pattern - prepare apps → build tree → calculate layout → start apps with correct dimensions.

**Key Files**: `texel/pane.go` (PrepareAppForRestore, StartPreparedApp), `texel/snapshot_restore.go`, `cmd/texel-server/main.go`

---

### Backspace Visual Erase - Fixed (2025-12-13)
**Problem**: Pressing backspace moved cursor but didn't visually erase characters until typing a new character.

**Root Cause**: Bash uses BS + EL (Erase to End of Line) for backspace. The `displayBufferEraseToEndOfLine()` function was truncating the logical line but not rebuilding the physical representation.

**Fix**: Added `RebuildCurrentLine()` calls to all display buffer erase functions:
- `displayBufferEraseToEndOfLine()` - EL 0
- `displayBufferEraseFromStartOfLine()` - EL 1
- `displayBufferEraseLine()` - EL 2
- `displayBufferEraseCharacters()` - ECH
- `displayBufferDeleteCharacters()` - DCH

**Files Modified**: `apps/texelterm/parser/vterm_display_buffer.go`, `apps/texelterm/parser/display_buffer.go`, `apps/texelterm/parser/vterm_edit_char.go`

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

## Current Branch: client-server-split

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

- TexelUI plan: see `docs/TEXELUI_PLAN.md`. When working on TexelUI, keep this plan up to date (checklist and sections) and commit changes to it alongside related code. Future sessions should consult and update this file as the source of truth for TexelUI scope, status, and next steps.
- TexelUI Architecture Review: see `docs/TEXELUI_ARCHITECTURE_REVIEW.md`. **[IMPORTANT - NEXT SESSION]** Comprehensive evaluation of TexelUI for form building completed 2025-11-18. Current state: solid low-level foundation but missing high-level primitives for productive form development. **Next steps:** Implement common widgets (Label, Button, Input, Checkbox), layout managers (VBox, HBox, Grid), and form helpers. Priority order and implementation details in review doc. Estimated 2-3 weeks for essential features.
- Long Line Editor plan: see `docs/LONG_LINE_EDITOR_PLAN.md`. Phased implementation of overlay editor for long command lines in texelterm. Update progress and status as work proceeds.
- **Layout Transitions (Server-Side) - COMPLETE (2025-12-07)**:
  - **Status**: Fully implemented and working
  - **Architecture**: Server-side animation system that animates SplitRatios over time, broadcasting tree snapshots at 60fps
  - **Implementation**: `texel/layout_transitions.go` (~200 lines)
  - **Configuration**: Via `theme.json` under `layout_transitions` section (duration_ms, easing, enabled, min_threshold)
  - **How It Works**:
    - When a pane is split, new pane starts at 1% of space, existing panes at 99%
    - LayoutTransitionManager animates split ratios from initial to final (e.g., [0.99, 0.01] → [0.5, 0.5])
    - Each animation frame: Updates ratios → calls recalculateLayout() → broadcasts tree snapshot
    - Client receives rapid snapshots and renders them normally with proper borders
    - Animation identical to manual resize operations (reuses same code path)
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
  - **Future Enhancements**:
    - Add close animations (pane shrinks before removal)
    - Support different easing functions (ease-in, ease-out, etc.)
    - Make animations interruptible (currently complete before next action)

# Architecture Refactor Design

## Goal

Refactor the texelation client/server architecture for clarity, separation of concerns, and Go naming convention compliance. Split oversized files, remove dead code, and ensure consistent naming across all layers.

## Scope

Full refactor across all layers:
- Protocol cleanup (dead stubs, error exports)
- Server runtime file splits
- Desktop engine file splits (biggest change)
- Client runtime file splits
- Full naming audit (exported + internal)

## Constraints

- **Same-package splits only** — no new sub-packages, no import graph changes
- **No behavioral changes** — purely structural refactoring
- **Layer-by-layer PRs** — each PR independently reviewable and revertable
- **Naming PR last** — renames after structural splits to minimize conflicts

## Architecture Audit Summary

Conducted 2026-02-18 across 5 layers. Key findings:

| Layer | Files | Lines | Score | Issues |
|-------|-------|-------|-------|--------|
| Protocol | 3 | 1,942 | 8.5/10 | 3 dead message types, unexported errors |
| Server Runtime | 15 | ~4,500 | 8.2/10 | connection.go oversized (679L), boot logic tangled |
| Desktop Engine | 26 | 7,259 | 7.5/10 | God object (1,672L), workspace (1,182L), pane (846L) |
| Client Runtime | 13 | ~3,500 | 8.0/10 | buffercache (495L), client_state (569L), duplicate methods |
| Cmd Entrypoints | 8 | ~800 | 7.5/10 | Ad-hoc server wiring |

## PR Strategy

### PR 1: Protocol Cleanup

**Files:** `protocol/protocol.go`, `protocol/messages.go`, `protocol/codec.go`

- Remove dead message types: `MsgTreeDelta`, `MsgMetricUpdate`, and any other declared-but-unimplemented types
- Export error variables: `errUnknownMessage` -> `ErrUnknownMessage`
- Naming fixes for exported API

### PR 2: Server Runtime Splits

**Split `connection.go` (679L) into:**
- `connection.go` (~200L) — Core struct, constructor, lifecycle (Connect/Close)
- `connection_handler.go` (~250L) — Message handling (handleKeyEvent, handleMouseEvent, etc.)
- `connection_sync.go` (~200L) — State sync, pending queue, sendPending

**Extract from `server.go`:**
- `server_boot.go` — `applyBootCapture()`, `startSnapshotLoop()`, snapshot restore logic

### PR 3: Desktop Engine Splits

**Split `desktop_engine_core.go` (1,672L) into 6 files:**
- `desktop_engine_core.go` (~400L) — Core struct, constructor, Run() event loop, lifecycle
- `desktop_clipboard.go` (~100L) — Clipboard operations (Set/Get/Pop/Handle)
- `desktop_overlays.go` (~200L) — Overlay management, layer stack
- `desktop_listeners.go` (~150L) — Event listener registration, dispatch helpers
- `desktop_input.go` (~250L) — InjectKeyEvent, InjectMouseEvent, key bindings, mouse routing
- `desktop_status.go` (~200L) — Status pane management, getMainArea(), statusbar layout

**Split `workspace.go` (1,182L) into 3 files:**
- `workspace.go` (~400L) — Core workspace struct, constructor, app management
- `workspace_layout.go` (~400L) — Resize, recalculate, split operations
- `workspace_navigation.go` (~300L) — Focus cycling, pane selection, directional movement

**Split `pane.go` (846L) into 3 files:**
- `pane.go` (~300L) — Core pane struct, constructor, lifecycle
- `pane_render.go` (~300L) — renderBuffer(), dirty tracking, buffer allocation
- `pane_input.go` (~200L) — HandleKey delegation, mouse forwarding

### PR 4: Client Runtime Splits

**Split `buffercache.go` (495L):**
- Remove duplicate `Pane()` method (keep `PaneByID()` only)
- Trim to ~300L

**Split `client_state.go` (569L):**
- `client_state.go` (~300L) — State management
- `client_render.go` (~250L) — Render loop logic

### PR 5: Full Naming Audit

Sweep all packages for:
- Get prefix violations (NAME-4): `GetFoo()` -> `Foo()`
- Abbreviation consistency: `ID` not `Id`, `URL` not `Url`
- Internal camelCase consistency
- Any remaining snake_case violations

## Risk Assessment

- **Low risk**: Protocol cleanup, naming audit (no structural changes)
- **Medium risk**: Server/client splits (file moves, but same package)
- **Higher risk**: Desktop engine splits (largest file, most method interdependency)

All PRs are zero-behavioral-change — tests must pass unchanged after each PR.

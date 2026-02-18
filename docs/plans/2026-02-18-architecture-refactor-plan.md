# Architecture Refactor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor the texelation client/server architecture for clarity, separation of concerns, and Go naming convention compliance across 5 layer-by-layer PRs.

**Architecture:** Same-package file splits only — no new sub-packages, no import graph changes. Each PR is independently reviewable/revertable. All structural, zero behavioral changes.

**Tech Stack:** Go 1.24.3, `make build`, `make test`

---

## PR 1: Protocol Cleanup

### Task 1: Remove dead message types from protocol.go

**Files:**
- Modify: `protocol/protocol.go:35-64`

**Step 1: Verify dead types are truly unused**

Run:
```bash
cd /home/marc/projects/texel/texelation && grep -rn 'MsgTreeDelta\|MsgMetricUpdate' --include='*.go' | grep -v '_test.go' | grep -v 'protocol.go'
```
Expected: No references outside protocol.go (if references exist, STOP and report).

**Step 2: Remove MsgTreeDelta and MsgMetricUpdate**

In `protocol/protocol.go`, the iota-based enum currently has:
```go
MsgTreeSnapshot   // iota 9
MsgTreeDelta      // iota 10 — DEAD
MsgBufferDelta    // iota 11
...
MsgError          // iota 18
MsgMetricUpdate   // iota 19 — DEAD
MsgClipboardData  // iota 20
```

**CRITICAL:** Removing entries from an iota block shifts all subsequent values. Since this is a binary protocol, changing wire values would be a breaking change.

Instead of removing, replace with explicitly-named placeholders that preserve the iota numbering:
```go
MsgTreeSnapshot
_               // was MsgTreeDelta (unused, placeholder preserves iota numbering)
MsgBufferDelta
...
MsgError
_               // was MsgMetricUpdate (unused, placeholder preserves iota numbering)
MsgClipboardData
```

**Step 3: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go build ./... && make test`
Expected: All tests pass, all packages build.

**Step 4: Commit**

```bash
git add protocol/protocol.go
git commit -m "protocol: replace dead message types with iota placeholders"
```

### Task 2: Export internal error variables

**Files:**
- Modify: `protocol/messages.go:18-22`
- Modify: `protocol/buffer_delta.go:75-78`

**Step 1: Export errors in messages.go**

Change:
```go
var (
	errStringTooLong = errors.New("protocol: string exceeds 64KB limit")
	errPayloadShort  = errors.New("protocol: payload too short")
	errExtraBytes    = errors.New("protocol: payload has trailing data")
)
```
To:
```go
var (
	ErrStringTooLong = errors.New("protocol: string exceeds 64KB limit")
	ErrPayloadShort  = errors.New("protocol: payload too short")
	ErrExtraBytes    = errors.New("protocol: payload has trailing data")
)
```

Then rename all references within `messages.go` and `buffer_delta.go` (grep for `errStringTooLong`, `errPayloadShort`, `errExtraBytes`, `errInvalidSpan`).

**Step 2: Export errInvalidSpan in buffer_delta.go**

Change:
```go
errInvalidSpan = errors.New("protocol: invalid span")
```
To:
```go
ErrInvalidSpan = errors.New("protocol: invalid span")
```

**Step 3: Update all references across the codebase**

Run:
```bash
grep -rn 'errStringTooLong\|errPayloadShort\|errExtraBytes\|errInvalidSpan' --include='*.go'
```
Update each occurrence to use the exported name.

**Step 4: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go build ./... && make test`
Expected: All pass.

**Step 5: Commit**

```bash
git add protocol/messages.go protocol/buffer_delta.go
git commit -m "protocol: export internal error variables"
```

### Task 3: Create PR 1

```bash
git push -u origin refactor/architecture-cleanup
gh pr create --title "refactor: protocol cleanup — remove dead types, export errors" --body "$(cat <<'EOF'
## Summary
- Replace dead message types (MsgTreeDelta, MsgMetricUpdate) with iota placeholders to preserve wire numbering
- Export internal error variables (ErrPayloadShort, ErrStringTooLong, ErrExtraBytes, ErrInvalidSpan)

## Test plan
- [x] `go build ./...` passes
- [x] `make test` passes
- [x] No behavioral changes — purely structural

Part 1/5 of architecture refactor (see docs/plans/2026-02-18-architecture-refactor-design.md)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## PR 2: Server Runtime Splits

### Task 4: Split connection.go into connection_handler.go

**Files:**
- Modify: `internal/runtime/server/connection.go`
- Create: `internal/runtime/server/connection_handler.go`

**Step 1: Read the file to identify exact cut points**

Read `internal/runtime/server/connection.go` fully. Identify:
- **Keep in connection.go**: `connection` struct, `newConnection()`, `serve()`, `Close()`, `sendMessage()`, `readMessage()` — core lifecycle
- **Move to connection_handler.go**: `handleMessage()` (the big switch), `handleKeyEvent()`, `handleMouseEvent()`, `handlePaste()`, `handleClipboardSet()`, `handleClipboardGet()`, `handleResize()`, `handleClientReady()` — message dispatch

**Step 2: Create connection_handler.go**

Create `internal/runtime/server/connection_handler.go` with:
- Same `package server` declaration
- Same copyright header
- Move all handler methods (they're methods on `*connection`, same package, no import issues)
- Add necessary imports (only those used by the moved methods)

**Step 3: Remove moved methods from connection.go**

Cut the moved methods from `connection.go`. Remove any imports that are no longer used.

**Step 4: Verify build and tests**

Run: `cd /home/marc/projects/texel/texelation && go build ./internal/runtime/server/... && make test`
Expected: All pass — same package, methods don't change.

**Step 5: Commit**

```bash
git add internal/runtime/server/connection.go internal/runtime/server/connection_handler.go
git commit -m "server: extract message handlers to connection_handler.go"
```

### Task 5: Split connection.go into connection_sync.go

**Files:**
- Modify: `internal/runtime/server/connection.go`
- Create: `internal/runtime/server/connection_sync.go`

**Step 1: Identify sync methods to move**

Move: `sendPending()`, `sendStateUpdate()`, `sendPaneState()`, `sendTreeSnapshot()`, `geometryOnlySnapshot()`, `snapshotMergedPaneStates()` — state sync and snapshot helpers.

**Step 2: Create connection_sync.go**

Same pattern as Task 4: create file, move methods, adjust imports.

**Step 3: Remove moved methods from connection.go**

**Step 4: Verify build and tests**

Run: `cd /home/marc/projects/texel/texelation && go build ./internal/runtime/server/... && make test`

**Step 5: Commit**

```bash
git add internal/runtime/server/connection.go internal/runtime/server/connection_sync.go
git commit -m "server: extract state sync to connection_sync.go"
```

### Task 6: Extract server_boot.go from server.go

**Files:**
- Modify: `internal/runtime/server/server.go`
- Create: `internal/runtime/server/server_boot.go`

**Step 1: Identify boot/snapshot methods**

Move: `loadBootSnapshot()`, `applyBootCapture()`, `applyBootSnapshot()`, `startSnapshotLoop()`, `persistSnapshot()` — boot and persistence logic.

**Step 2: Create server_boot.go, move methods, adjust imports**

**Step 3: Verify build and tests**

Run: `cd /home/marc/projects/texel/texelation && go build ./internal/runtime/server/... && make test`

**Step 4: Commit**

```bash
git add internal/runtime/server/server.go internal/runtime/server/server_boot.go
git commit -m "server: extract boot and snapshot logic to server_boot.go"
```

### Task 7: Create PR 2

```bash
git push origin refactor/architecture-cleanup
gh pr create --title "refactor: server runtime file splits" --body "$(cat <<'EOF'
## Summary
- Split `connection.go` (679L) into 3 focused files:
  - `connection.go` — core struct, lifecycle
  - `connection_handler.go` — message handling (handleMessage switch)
  - `connection_sync.go` — state sync, pending queue, snapshot helpers
- Extract `server_boot.go` from `server.go` — boot capture, snapshot loop

## Test plan
- [x] `go build ./internal/runtime/server/...` passes
- [x] `make test` passes
- [x] No behavioral changes — purely structural file splits within same package

Part 2/5 of architecture refactor
EOF
)"
```

---

## PR 3: Desktop Engine Splits

### Task 8: Extract desktop_clipboard.go

**Files:**
- Modify: `texel/desktop_engine_core.go`
- Create: `texel/desktop_clipboard.go`

**Step 1: Identify clipboard methods**

Move: `SetClipboard()`, `GetClipboard()`, `HandleClipboardSet()`, `HandleClipboardGet()`, `PopPendingClipboard()`, `HandlePaste()`, `handlePasteInternal()` — all clipboard-related methods on `*DesktopEngine`.

**Step 2: Create desktop_clipboard.go, move methods**

**Step 3: Verify**

Run: `cd /home/marc/projects/texel/texelation && go build ./texel/... && make test`

**Step 4: Commit**

```bash
git add texel/desktop_engine_core.go texel/desktop_clipboard.go
git commit -m "texel: extract clipboard operations to desktop_clipboard.go"
```

### Task 9: Extract desktop_overlays.go

**Files:**
- Modify: `texel/desktop_engine_core.go`
- Create: `texel/desktop_overlays.go`

**Step 1: Identify overlay methods**

Move: `ShowFloatingPanel()`, `CloseFloatingPanel()`, `closeFloatingPanelByApp()`, `launchLauncherOverlay()`, `launchHelpOverlay()`, `launchConfigEditorOverlay()`, `handleConfigEditorApply()` — overlay management and launchers.

**Step 2: Create desktop_overlays.go, move methods**

**Step 3: Verify**

Run: `cd /home/marc/projects/texel/texelation && go build ./texel/... && make test`

**Step 4: Commit**

```bash
git add texel/desktop_engine_core.go texel/desktop_overlays.go
git commit -m "texel: extract overlay management to desktop_overlays.go"
```

### Task 10: Extract desktop_listeners.go

**Files:**
- Modify: `texel/desktop_engine_core.go`
- Create: `texel/desktop_listeners.go`

**Step 1: Identify listener methods**

Move: `RegisterFocusListener()`, `UnregisterFocusListener()`, `RegisterPaneStateListener()`, `UnregisterPaneStateListener()`, `notifyFocus()`, `notifyPaneState()`, `notifyFocusActive()`, `notifyFocusNode()` — event listener registration and dispatch.

**Step 2: Create desktop_listeners.go, move methods**

**Step 3: Verify**

Run: `cd /home/marc/projects/texel/texelation && go build ./texel/... && make test`

**Step 4: Commit**

```bash
git add texel/desktop_engine_core.go texel/desktop_listeners.go
git commit -m "texel: extract event listeners to desktop_listeners.go"
```

### Task 11: Extract desktop_input.go

**Files:**
- Modify: `texel/desktop_engine_core.go`
- Create: `texel/desktop_input.go`

**Step 1: Identify input methods**

Move: `InjectKeyEvent()`, `InjectMouseEvent()`, `handleEvent()` (key binding handler), `handleMouseEvent()`, `processMouseEvent()`, `paneAtCoordinates()`, `activatePaneAt()` — input routing and key/mouse handling.

**Step 2: Create desktop_input.go, move methods**

**Step 3: Verify**

Run: `cd /home/marc/projects/texel/texelation && go build ./texel/... && make test`

**Step 4: Commit**

```bash
git add texel/desktop_engine_core.go texel/desktop_input.go
git commit -m "texel: extract input routing to desktop_input.go"
```

### Task 12: Extract desktop_status.go

**Files:**
- Modify: `texel/desktop_engine_core.go`
- Create: `texel/desktop_status.go`

**Step 1: Identify status pane methods**

Move: `AddStatusPane()`, `getMainArea()`, `recalculateLayout()` and the `StatusPane`, `Side` type definitions (`SideTop`, `SideBottom`, `SideLeft`, `SideRight` constants) — status bar layout management.

**Step 2: Create desktop_status.go, move methods and types**

**Step 3: Verify**

Run: `cd /home/marc/projects/texel/texelation && go build ./texel/... && make test`

**Step 4: Commit**

```bash
git add texel/desktop_engine_core.go texel/desktop_status.go
git commit -m "texel: extract status pane management to desktop_status.go"
```

### Task 13: Split workspace.go into workspace_layout.go

**Files:**
- Modify: `texel/workspace.go`
- Create: `texel/workspace_layout.go`

**Step 1: Identify layout methods**

Move: `recalculateLayout()`, `PerformSplit()`, `removeNode()`, `doRemoveNode()`, `CloseActivePane()`, `setArea()`, border resize methods: `adjustBorderToX()`, `adjustBorderToY()`, `applyBorderRatios()`, `handleInteractiveResize()`, `adjustBorder()` — all layout and resize logic.

**Step 2: Create workspace_layout.go, move methods**

**Step 3: Verify**

Run: `cd /home/marc/projects/texel/texelation && go build ./texel/... && make test`

**Step 4: Commit**

```bash
git add texel/workspace.go texel/workspace_layout.go
git commit -m "texel: extract workspace layout to workspace_layout.go"
```

### Task 14: Split workspace.go into workspace_navigation.go

**Files:**
- Modify: `texel/workspace.go`
- Create: `texel/workspace_navigation.go`

**Step 1: Identify navigation methods**

Move: `moveActivePane()`, `activateLeaf()`, `nodeAt()`, `borderForNeighbor()`, `borderAt()`, `SwapActivePane()`, mouse resize: `handleMouseResize()`, `startMouseResize()`, `finishMouseResize()`, `updateMouseResize()` — focus cycling, pane selection, directional movement, mouse resize.

**Step 2: Create workspace_navigation.go, move methods**

**Step 3: Verify**

Run: `cd /home/marc/projects/texel/texelation && go build ./texel/... && make test`

**Step 4: Commit**

```bash
git add texel/workspace.go texel/workspace_navigation.go
git commit -m "texel: extract workspace navigation to workspace_navigation.go"
```

### Task 15: Split pane.go into pane_render.go

**Files:**
- Modify: `texel/pane.go`
- Create: `texel/pane_render.go`

**Step 1: Identify render methods**

Move: `markDirty()`, `setupRefreshForwarder()`, `Render()`, `renderBuffer()`, `Width()`, `Height()`, `drawableWidth()`, `drawableHeight()`, `setDimensions()`, `contains()` — rendering, dirty tracking, geometry.

**Step 2: Create pane_render.go, move methods**

**Step 3: Verify**

Run: `cd /home/marc/projects/texel/texelation && go build ./texel/... && make test`

**Step 4: Commit**

```bash
git add texel/pane.go texel/pane_render.go
git commit -m "texel: extract pane rendering to pane_render.go"
```

### Task 16: Split pane.go into pane_input.go

**Files:**
- Modify: `texel/pane.go`
- Create: `texel/pane_input.go`

**Step 1: Identify input/state methods**

Move: `handlePaste()`, `handleMouse()`, `handlesMouseEvents()`, `contentLocalCoords()`, state methods: `SetActive()`, `SetResizing()`, `notifyStateChange()`, `SetZOrder()`, `GetZOrder()`, `BringToFront()`, `SendToBack()`, `SetAsDialog()` — input delegation and pane state.

**Step 2: Create pane_input.go, move methods**

**Step 3: Verify**

Run: `cd /home/marc/projects/texel/texelation && go build ./texel/... && make test`

**Step 4: Commit**

```bash
git add texel/pane.go texel/pane_input.go
git commit -m "texel: extract pane input/state to pane_input.go"
```

### Task 17: Create PR 3

```bash
git push origin refactor/architecture-cleanup
gh pr create --title "refactor: desktop engine file splits" --body "$(cat <<'EOF'
## Summary
Split the three largest desktop engine files:

**desktop_engine_core.go** (1,672L → 6 files):
- `desktop_engine_core.go` — core struct, constructor, event loop, lifecycle
- `desktop_clipboard.go` — clipboard operations
- `desktop_overlays.go` — overlay management, launcher/help/config dialogs
- `desktop_listeners.go` — event listener registration and dispatch
- `desktop_input.go` — key/mouse routing, input handling
- `desktop_status.go` — status pane management, layout

**workspace.go** (1,182L → 3 files):
- `workspace.go` — core struct, constructor, app management
- `workspace_layout.go` — resize, split operations, border adjustment
- `workspace_navigation.go` — focus cycling, pane selection, mouse resize

**pane.go** (846L → 3 files):
- `pane.go` — core struct, constructor, lifecycle
- `pane_render.go` — rendering, dirty tracking, geometry
- `pane_input.go` — input delegation, pane state

## Test plan
- [x] `go build ./texel/...` passes
- [x] `make test` passes
- [x] No behavioral changes — purely same-package file splits

Part 3/5 of architecture refactor
EOF
)"
```

---

## PR 4: Client Runtime Splits

### Task 18: Remove duplicate Pane() method from buffercache.go

**Files:**
- Modify: `client/buffercache.go:228-233`

**Step 1: Verify Pane() and PaneByID() are identical**

Read lines 228-248 of `client/buffercache.go`. Confirm both methods:
- Take `id [16]byte`
- Return `*PaneState`
- RLock/RUnlock and return `c.panes[id]`

**Step 2: Find all callers of Pane()**

Run:
```bash
grep -rn '\.Pane(' --include='*.go' | grep -v 'PaneByID\|PaneAt\|PaneState\|AllPanes\|SortedPanes\|ForEachPaneSorted\|LatestPane\|SetPaneFlags'
```

Replace each `Pane(` call with `PaneByID(` in the calling code.

**Step 3: Remove Pane() method**

Delete the `Pane()` method (lines 228-233 of `client/buffercache.go`).

**Step 4: Verify**

Run: `cd /home/marc/projects/texel/texelation && go build ./... && make test`

**Step 5: Commit**

```bash
git add client/buffercache.go
# Also add any files where Pane() was renamed to PaneByID()
git commit -m "client: remove duplicate Pane() method, keep PaneByID()"
```

### Task 19: Extract client_selection.go from client_state.go

**Files:**
- Modify: `internal/runtime/client/client_state.go`
- Create: `internal/runtime/client/client_selection.go`

**Step 1: Identify selection types and methods**

Move: `selectionRect` struct and `clamp()`, `selectionState` struct and all its methods (`clear`, `begin`, `updateCurrent`, `finish`, `isVisible`, `bounds`, `consumePendingCopy`), and the `clientState` selection methods (`handleSelectionMouse`, `clearSelection`, `selectionBounds`, `selectionClipboardData`).

**Step 2: Create client_selection.go, move types and methods**

**Step 3: Verify**

Run: `cd /home/marc/projects/texel/texelation && go build ./internal/runtime/client/... && make test`

**Step 4: Commit**

```bash
git add internal/runtime/client/client_state.go internal/runtime/client/client_selection.go
git commit -m "client: extract selection state to client_selection.go"
```

### Task 20: Create PR 4

```bash
git push origin refactor/architecture-cleanup
gh pr create --title "refactor: client runtime cleanup" --body "$(cat <<'EOF'
## Summary
- Remove duplicate `Pane()` method from `buffercache.go` (keep `PaneByID()` only)
- Extract selection state management from `client_state.go` to `client_selection.go`

## Test plan
- [x] `go build ./...` passes
- [x] `make test` passes
- [x] No behavioral changes

Part 4/5 of architecture refactor
EOF
)"
```

---

## PR 5: Full Naming Audit

### Task 21: Fix Get prefix violations (NAME-4)

**Files:** Multiple across all packages

**Step 1: Find all Get prefix violations**

Run:
```bash
grep -rn 'func.*\bGet[A-Z]' --include='*.go' | grep -v '_test.go' | grep -v vendor | grep -v '.cache'
```

For each match, check if it's a simple getter (returns a field value) and rename:
- `GetFoo()` → `Foo()` (if not conflicting with field name)
- Document each rename

**IMPORTANT**: Some `Get` prefixes are legitimate (e.g., `GetContentText` that does computation). Only rename pure getters.

**Step 2: Update all callers for each renamed method**

For each rename, grep for callers and update them.

**Step 3: Verify**

Run: `cd /home/marc/projects/texel/texelation && go build ./... && make test`

**Step 4: Commit**

```bash
git add -A
git commit -m "naming: remove Get prefix from simple getters (NAME-4)"
```

### Task 22: Fix abbreviation inconsistencies

**Files:** Multiple across all packages

**Step 1: Find ID/Id inconsistencies**

Run:
```bash
grep -rn '[a-z]Id\b' --include='*.go' | grep -v '_test.go' | grep -v vendor
```

Rename: `sessionId` → `sessionID`, `paneId` → `paneID`, etc. per Go convention (common acronyms are all-caps).

**Step 2: Find URL/Url inconsistencies**

Run:
```bash
grep -rn '[a-z]Url\b' --include='*.go' | grep -v '_test.go' | grep -v vendor
```

**Step 3: Update all references**

**Step 4: Verify**

Run: `cd /home/marc/projects/texel/texelation && go build ./... && make test`

**Step 5: Commit**

```bash
git add -A
git commit -m "naming: fix abbreviation casing (ID not Id, URL not Url)"
```

### Task 23: Fix remaining naming issues

**Files:** Multiple across all packages

**Step 1: Find snake_case violations**

Run:
```bash
grep -rn '_[a-z]' --include='*.go' | grep -v '_test.go' | grep -v vendor | grep -v 'import\|//' | head -50
```

Convert any `my_var` to `myVar`.

**Step 2: Review internal camelCase consistency**

Scan key files for inconsistent naming patterns and fix.

**Step 3: Verify**

Run: `cd /home/marc/projects/texel/texelation && go build ./... && make test`

**Step 4: Commit**

```bash
git add -A
git commit -m "naming: fix remaining snake_case and casing inconsistencies"
```

### Task 24: Create PR 5

```bash
git push origin refactor/architecture-cleanup
gh pr create --title "refactor: full naming audit — Go conventions" --body "$(cat <<'EOF'
## Summary
- Remove `Get` prefix from simple getters (NAME-4)
- Fix abbreviation casing: `ID` not `Id`, `URL` not `Url`
- Fix remaining snake_case violations
- Ensure consistent camelCase throughout

## Test plan
- [x] `go build ./...` passes
- [x] `make test` passes
- [x] No behavioral changes — purely naming renames

Part 5/5 of architecture refactor
EOF
)"
```

---

## Execution Notes

### Branch Strategy

All 5 PRs are on the same branch `refactor/architecture-cleanup`. After each PR's commits:
1. Push to remote
2. Create PR
3. Get it reviewed and merged
4. Pull main
5. Rebase remaining work onto main (or continue on same branch if doing them sequentially)

**Alternative**: If doing all at once, keep all commits on one branch and create a single large PR. But the design specifies independent PRs.

### Risk Mitigation

- **PR 3 is highest risk** (desktop engine, largest files, most method interdependency). Do it carefully, one file at a time, verifying build after each extraction.
- **PR 5 (naming) depends on PRs 1-4** being merged first to avoid conflicts.
- Each file split follows the same pattern: create new file with same package declaration → cut methods → paste → verify build → verify tests → commit.

### Verification Command

After every structural change:
```bash
cd /home/marc/projects/texel/texelation && go build ./... && make test
```

This catches:
- Missing imports in new files
- Accidentally moved package-level vars that other methods depend on
- Any compile errors from cut/paste mistakes

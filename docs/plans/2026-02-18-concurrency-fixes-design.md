# Design: Concurrency Architecture Fixes

## Context

The audit documented in `docs/CONCURRENCY_ARCHITECTURE.md` (2026-02-18) found three
concurrency issues. This design addresses all three plus updates the architecture doc.

## Issue 1: Tree Structure Has No Formal Synchronization (Medium)

### Problem

Three goroutines access the pane tree without synchronization:

1. **Connection goroutine** (`connection.serve`): Modifies tree via `InjectKeyEvent` →
   `handleEvent` → split/remove/move. Also reads tree via synchronous `Publish()` →
   `SnapshotBuffers()`.
2. **Animation ticker** (`LayoutTransitionManager.startAnimationLoop`): Modifies
   `Node.SplitRatios`, calls `recalculateLayout()` (writes pane dimensions), calls
   `broadcastTreeChanged()`.
3. **Refresh monitor** (`Workspace.startRefreshMonitor`): Reads tree via async
   `publish()` → `DesktopPublisher.Publish()` → `SnapshotBuffers()` → tree traversal.

### Root Cause

The desktop engine is purely event-driven with no central event loop. Events are injected
synchronously from the connection goroutine. Animation and refresh run on separate
goroutines that touch tree state without coordination.

### Fix: Desktop Event Loop

Create a `Run()` method on `DesktopEngine` that runs a goroutine with a `select` loop.
This goroutine is the **sole accessor** of tree state.

```go
func (d *DesktopEngine) Run() {
    for {
        select {
        case ev := <-d.eventCh:       // Key/mouse/paste from connection
            d.handleEvent(ev)
            d.publishIfDirty()
        case frame := <-d.animCh:     // Animation ratio updates
            d.applyAnimationFrame(frame)
            d.recalculateLayout()
            d.publishIfDirty()
        case <-d.refreshCh:           // Pane dirty signals
            d.publishIfDirty()
        case <-d.quit:
            return
        }
    }
}
```

#### Event Injection (Async)

`InjectKeyEvent()`, `InjectMouseEvent()`, `HandlePaste()` send to `d.eventCh` instead
of calling `handleEvent()` directly. The connection goroutine does not wait — diffs are
enqueued to the session by the event loop and the connection is nudged via
`session.nudge()`.

Channel: `eventCh chan desktopEvent` (buffered, capacity 64).

Event types (tagged union):

```go
type desktopEvent struct {
    kind     eventKind // keyEvent, mouseEvent, pasteEvent, resizeEvent
    key      tcell.Key
    rune     rune
    mod      tcell.ModMask
    mx, my   int
    buttons  tcell.ButtonMask
    paste    []byte
    width, height int
}
```

#### Animation (Channel-Based)

The animation ticker no longer writes to `Node.SplitRatios` or calls
`recalculateLayout()` directly. Instead:

1. Ticker computes interpolated ratios (under `m.mu`).
2. Sends `animationFrame{node *Node, ratios []float64, done bool, callback func()}`
   to `d.animCh`.
3. Event loop applies ratios to `Node.SplitRatios`, calls `recalculateLayout()`,
   publishes.

When animation completes (`done == true`), the event loop calls the callback (e.g.,
remove a closed pane).

Channel: `animCh chan animationFrame` (buffered, capacity 16).

#### Refresh (Absorbed by Event Loop)

The workspace refresh monitor goroutine is eliminated. Its role is absorbed by the
`refreshCh` case in the event loop.

Pane dirty signal flow:
```
App writes → VTerm marks dirty → pane.markDirty() (atomic renderGen++)
  → pane forwarder sends on d.refreshCh (non-blocking)
  → event loop receives, calls publishIfDirty()
```

`pane.setupRefreshForwarder()` is updated to send to `d.refreshCh` instead of
`workspace.refreshChan`.

Channel: `refreshCh chan struct{}` (buffered, capacity 16).

#### Publishing

`publishIfDirty()` is called from the event loop after processing each event,
animation frame, or refresh signal. Since the event loop is the sole tree accessor:

- `SnapshotBuffers()` reads tree safely (single goroutine).
- `SnapshotForClient()` reads tree safely.
- `recalculateLayout()` writes pane dimensions safely.

The synchronous `Publish()` call in `HandleKeyEvent` (desktop_sink.go:40) is removed.
Instead, `handleEvent` within the event loop calls `publishIfDirty()`.

#### Server Integration

`DesktopSink` methods become channel senders:

```go
func (d *DesktopSink) HandleKeyEvent(session *Session, event protocol.KeyEvent) {
    d.desktop.SendEvent(desktopEvent{kind: keyEvent, key: ..., rune: ...})
}
```

`DesktopSink.SetPublisher` wires the publish callback so the event loop can call it.

#### Standalone/Combined Mode

In standalone mode (`cmd/texelation`), the desktop event loop goroutine runs alongside
the tcell event loop. The tcell event loop sends events via `d.eventCh`.

In standalone texelterm (`cmd/texelterm`), there is no desktop engine. Not affected.

#### Latency

A channel hop adds ~1-2μs per keystroke. The connection goroutine sends the event, the
event loop processes it (handle + publish), enqueues diffs to the session, and nudges
the connection. Total added latency: ~5-10μs. Negligible for terminal use.

### Files to Modify

| File | Change |
|------|--------|
| `texel/desktop_engine_core.go` | Add `Run()`, `eventCh`, `animCh`, `refreshCh`. Make `InjectKeyEvent`/`InjectMouseEvent` async. Remove direct `handleEvent` calls. |
| `texel/workspace.go` | Remove `startRefreshMonitor()`, `refreshMonitorOnce`, `refreshChan`. |
| `texel/pane.go` | Update `setupRefreshForwarder()` to send to `d.refreshCh`. |
| `texel/layout_transitions.go` | `updateAnimations()` sends to `d.animCh` instead of direct tree modification. |
| `internal/runtime/server/desktop_sink.go` | `HandleKeyEvent` etc. become channel senders. Remove synchronous `Publish()` call. |
| `cmd/texelation/main.go` | Start `desktop.Run()` goroutine. |
| `cmd/texel-server/main.go` | Start `desktop.Run()` goroutine. |

---

## Issue 2: DesktopSink.SetPublisher Unsynchronized Write (Low)

### Problem

`SetPublisher()` writes `d.publisher` without `d.mu`. Other methods read `d.publisher`
without `d.mu`. Only `publish()` correctly uses `d.mu`.

### Fix

All reads and writes of `d.publisher` go through `d.mu`:

- `SetPublisher()`: `d.mu.Lock(); d.publisher = publisher; d.mu.Unlock()`
- `HandleKeyEvent`, `HandleMouseEvent`, `HandlePaste`, `Publish`: snapshot publisher
  under `d.mu` (same pattern `publish()` already uses)

### Files to Modify

| File | Change |
|------|--------|
| `internal/runtime/server/desktop_sink.go` | Add `d.mu.Lock/Unlock` to `SetPublisher` and snapshot pattern to all methods reading `d.publisher` |

---

## Issue 3: TexelTerm Clipboard Callback Deadlock (Real Bug)

### Problem

OSC 52 clipboard handlers fire **synchronously** during `Parse()`. The PTY reader
holds `a.mu` during `Parse()`. The handlers try to re-acquire `a.mu` → deadlock
(Go mutexes are not reentrant).

```
PTY goroutine:
  a.mu.Lock()
  a.parser.Parse(r)
    → handleOSC52()
      → OnClipboardSet callback
        → a.mu.Lock()  // DEADLOCK: same goroutine, same mutex
```

### Fix

Add a dedicated `clipboardMu sync.Mutex` that protects only `a.clipboard`. The OSC 52
handlers lock `clipboardMu` instead of `a.mu`, eliminating the re-entrant lock attempt.

```go
// In TexelTerm struct:
clipboardMu sync.Mutex

// SetClipboardService:
func (a *TexelTerm) SetClipboardService(clipboard ClipboardService) {
    a.clipboardMu.Lock()
    a.clipboard = clipboard
    a.clipboardMu.Unlock()
}

// OSC 52 handler (OnClipboardSet):
func(data []byte) {
    a.clipboardMu.Lock()
    clipboard := a.clipboard
    a.clipboardMu.Unlock()
    if clipboard != nil {
        clipboard.SetClipboard("text/plain", data)
    }
}
```

Lock ordering: `a.mu` → `a.clipboardMu` (but they are never nested — `clipboardMu` is
only acquired when `a.mu` is NOT held, specifically inside Parse callbacks).

### Files to Modify

| File | Change |
|------|--------|
| `apps/texelterm/term.go` | Add `clipboardMu`, update `SetClipboardService`, update OSC 52 handler installation in `initializeVTermFirstRun` |

---

## Architecture Doc Update

Update `docs/CONCURRENCY_ARCHITECTURE.md` to reflect:

- New desktop event loop goroutine in goroutine inventory
- Removal of refresh monitor goroutine
- Updated data flow diagram
- Updated Layer 3 (Desktop Engine) section
- Updated Rule #2: "The tree is owned by the desktop event loop goroutine."
- Issue #1 marked as fixed
- Issue #2 marked as fixed
- Issue #3 marked as fixed
- New `clipboardMu` in Layer 2 lock inventory

---

## Verification

1. `make build` — all binaries compile
2. `go test ./texel/... -race -count=1` — desktop engine tests
3. `go test ./internal/runtime/server/... -race -count=1` — server runtime tests
4. `go test ./apps/texelterm/... -race -count=1` — texelterm tests
5. Manual: open texelterm in texelation, resize panes, split/close with animation,
   type in terminal — verify no regressions

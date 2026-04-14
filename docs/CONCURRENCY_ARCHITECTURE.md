# Concurrency Architecture

This document maps every synchronization primitive in the codebase, establishes
lock ordering rules, and catalogues known issues. Use it as a reference when
adding new locks, channels, or goroutines.

**Last audited**: 2026-02-18

> **STALE SECTIONS (2026-04-14):** The `MemoryBuffer.mu` and `ViewportWindow.mu`
> sections below pre-date the sparse-viewport cutover (PR #179). The sparse
> terminal exposes three separate mutexes (`sparse.Store.mu` RWMutex,
> `sparse.WriteWindow.mu` Mutex, `sparse.ViewWindow.mu` Mutex) that replace
> those two entries. The RLock-upgrade caveat that used to apply to
> ViewportWindow is moot — sparse's view layer has no lazy initialization.
> The rest of this document (session/publisher/effect locks) is unchanged.
> A full re-audit is pending.

---

## Threading Model Overview

Texelation uses a **single-threaded event loop** for the desktop engine with
explicit cross-thread handoffs via channels and mutexes. Most state is owned
by the event loop and accessed without locks; the exceptions are documented
below.

### Goroutine Inventory

| Goroutine | Owner | Purpose |
|-----------|-------|---------|
| Event loop | `DesktopEngine` | Processes key/mouse/paste events, animation frames, pane refresh signals |
| Animation ticker | `LayoutTransitionManager` | Interpolates split ratios at 60 fps |
| Per-pane forwarder | `pane.setupRefreshForwarder` | Relays app refresh signals to workspace |
| App goroutine | `AppLifecycleManager.StartApp` | Runs the embedded app (e.g. texelterm) |
| Connection reader | `connection.readMessages` | Reads protocol frames from client |
| Connection server | `connection.serve` | Dispatches incoming messages, sends pending diffs |
| Ping loop | Client `pingLoop` | Sends periodic pings to server |
| ACK loop | Client `ackLoop` | Coalesces and sends buffer ACKs |
| Idle monitor | `AdaptivePersistence` | Flushes dirty lines after idle threshold |
| Debounce timer | `AdaptivePersistence` | Schedules delayed flushes |
| Checkpoint timer | `WriteAheadLog` | Periodic WAL→PageStore compaction |
| Snapshot loop | `Server.startSnapshotLoop` | Periodic tree persistence |

### Desktop Event Loop (`DesktopEngine.Run`)
- **One per** DesktopEngine
- **Started by** `go desktop.Run()` in server/standalone main
- **Stopped by** `desktop.Close()` (sends to `quit` channel)
- **Purpose**: Sole accessor of tree state. Processes key/mouse/paste events, animation frames, and pane refresh signals via select on eventCh, animCh, refreshCh, quit.
- **Channels**: eventCh (cap 64), animCh (cap 16), refreshCh (cap 16)

### Data Flow (Server → Client)

```
App writes to VTerm
  → VTerm marks dirty lines
  → pane.markDirty() increments renderGen (atomic)
  → pane forwarder sends on desktop.refreshChan
  → event loop receives on refreshCh
  → DesktopSink.publish()
  → DesktopPublisher.Publish()  [publisher.mu]
    → generates row diffs per pane
    → session.EnqueueDiff()     [session.mu]
  → connection.nudge()          [pending chan]
  → connection.sendPending()
    → connection.writeMessage() [connection.writeMu]
```

---

## Layer 1: Parser (`apps/texelterm/parser/`)

### Lock Inventory

| Lock | Type | File | Protects |
|------|------|------|----------|
| `VTerm.mu` | `sync.RWMutex` | `vterm.go` | Width, height, cursor, alt/main buffers during resize |
| `MemoryBuffer.mu` | `sync.RWMutex` | `memory_buffer.go` | Ring buffer, dirty tracker, content version |
| `ViewportWindow.mu` | `sync.RWMutex` | `viewport_window.go` | Scroll position, cache, dimensions |
| `WriteAheadLog.mu` | `sync.Mutex` | `write_ahead_log.go` | WAL entries, metadata, PageStore cursor |
| `PageStore.mu` | `sync.RWMutex` | `page_store.go` | Page files, line index, total count |
| `AdaptivePersistence.mu` | `sync.Mutex` | `adaptive_persistence.go` | Pending lines, mode, metrics, timers |
| `ShellIntegration.mu` | `sync.Mutex` | `shell_integration.go` | Shell state tracking |

### Lock Ordering

```
VTerm.mu  →  ViewportWindow.mu
              (Grid → memoryBufferGrid → viewport.GetVisibleGrid)
              (Resize → memoryBufferResize → viewport.Resize)

AdaptivePersistence.mu  →  WriteAheadLog.mu
                            (flushLineLocked → wal.Append)

AdaptivePersistence.mu  →  MemoryBuffer.mu
                            (flushLineLocked → memBuf.GetLine [RLock])
                            (flushLineLocked → memBuf.ClearDirty [Lock])

WriteAheadLog.mu  →  PageStore.mu
                      (checkpointLocked → pageStore.AppendLine)
```

**Rule**: Always acquire outer lock first. Never reverse the ordering.

### Key Patterns

- **ViewportWindow RLock caveat**: Several "read" methods (`GetVisibleGrid`,
  `CanScrollUp`, `TotalPhysicalLines`, `ViewportToContent`, `ContentToViewport`)
  use **write Lock** because they call `ensureIndexValid()` which lazily mutates
  the physical line index. See memory note from 2026-02-15.

- **Line cloning before background flush**: `flushLineLocked()` clones the
  `LogicalLine` before encoding to prevent a data race with the main goroutine
  writing to the same line's cells.

- **WAL metadata sync**: `checkpointLocked()` calls `walFile.Sync()` after
  re-writing metadata. Without this, metadata can be lost on crash (Linux
  `close()` does not guarantee sync).

---

## Layer 2: TexelTerm App (`apps/texelterm/`)

### Lock Inventory

| Lock | Type | File | Protects |
|------|------|------|----------|
| `TexelTerm.mu` | `sync.Mutex` | `term.go` | VTerm, parser, dimensions, PTY, process, palette, buf |
| `TexelTerm.clipboardMu` | `sync.Mutex` | `term.go` | Protects `a.clipboard` field. Used by OSC 52 handlers (inside Parse callbacks) and SetClipboardService. Never nested with `a.mu`. |
| `TexelTerm.stopOnce` | `sync.Once` | `term.go` | Ensures `stop` channel closed once |
| `TexelTerm.closeOnce` | `sync.Once` | `term.go` | Ensures `closeCh` closed once |
| `TexelTerm.wg` | `sync.WaitGroup` | `term.go` | Tracks PTY reader goroutine |
| `ScrollBar.mu` | `sync.Mutex` | `scrollbar.go` | Minimap cache, visibility, compute state |
| `HistoryNavigator.mu` | `sync.Mutex` | `history_navigator.go` | Visibility, search results, dimensions |
| `HistoryNavigator.timerMu` | `sync.Mutex` | `history_navigator.go` | Search debounce timer only |
| `HistoryNavigator.animMu` | `sync.Mutex` | `history_navigator.go` | Animation stop channel, animating flag |
| `MouseCoordinator.mu` | `sync.Mutex` | `mouse_coordinator.go` | Selection state, click detector, dimensions |
| `AutoScrollManager.mu` | `sync.Mutex` | `auto_scroll.go` | Active flag, stop channel, callbacks |

### Channels

| Channel | File | Buffer | Purpose |
|---------|------|--------|---------|
| `stop` | `term.go` | 0 | External shutdown signal |
| `closeCh` | `term.go` | 0 | User confirmed close dialog |
| `restartCh` | `term.go` | 1 | User declined close, restart shell |
| `refreshChan` | `term.go` | write-only | Forward refresh to desktop |

### Threading Model

- `runShell()` starts a PTY reader goroutine that feeds bytes into
  `Parser.Parse()` → VTerm state changes.
- The PTY reader goroutine is the **only writer** to VTerm (except resize).
- `Render()` is called from the desktop event loop and takes `mu.Lock`.
- `Resize()` takes `mu.Lock` and calls `VTerm.Resize()`.

**Rule**: PTY reader and Resize must not run concurrently. `TexelTerm.mu`
serializes them.

### Lock Ordering

```
TexelTerm.mu  →  ScrollBar.mu        (Render → scrollbar.Render)
TexelTerm.mu  →  HistoryNavigator.mu (Render → historyNavigator.Render)
TexelTerm.mu  →  VTerm.mu            (Resize → vterm.Resize)
MouseCoordinator.mu  →  TexelTerm.mu (HandleMouse drops mu before calling wheelHandler)
```

**Rule**: `MouseCoordinator` correctly drops its own `mu` before calling
into `TexelTerm` methods that acquire `TexelTerm.mu`.

### Known App-Layer Races (Low Severity)

These are technically data races but have no practical impact due to usage
patterns. Documented here for awareness:

- `a.vterm.InSynchronizedUpdate` read without `a.mu` after unlock in PTY
  reader loop (`term.go` ~line 1654).
- `a.pty` nil-check in `HandleKey()` without `a.mu` (`term.go` ~line 851).
  `Stop()` sets it to nil under `a.mu`.
- `a.bracketedPasteMode` read in `HandlePaste()` without `a.mu`. Comment
  says "bool reads are atomic" but Go memory model does not guarantee this.
- `a.refreshChan` assigned in `SetRefreshNotifier()` without `a.mu`.
  Safe because it is set once before goroutines start.

### Async Goroutines

| Goroutine | Lifecycle | Cancellation |
|-----------|-----------|-------------|
| PTY reader | Until PTY EOF | `a.wg`, PTY close |
| Minimap computation | Self-terminating | Generation counter |
| ScrollBar debounce | `time.AfterFunc` | Timer stop |
| History navigator animation | Ticker-driven | `animStopCh` |
| Auto-scroll loop | `stopChan`-cancellable | `wg.Wait()` |
| Scroll animation (term.go) | Timer-driven | Self-terminating (no cancel) |

---

## Layer 3: Desktop Engine (`texel/`)

### Lock Inventory

| Lock | Type | File | Protects |
|------|------|------|----------|
| `clipboardMu` | `sync.Mutex` | `desktop_engine_core.go` | Clipboard map and pending flag |
| `focusMu` | `sync.RWMutex` | `desktop_engine_core.go` | Focus listener slice |
| `paneStateMu` | `sync.RWMutex` | `desktop_engine_core.go` | Pane state listener slice |
| `viewportMu` | `sync.RWMutex` | `desktop_engine_core.go` | Viewport dimensions |
| `pendingAppStartsMu` | `sync.Mutex` | `desktop_engine_core.go` | Deferred app start queue |
| `stateMu` | `sync.Mutex` | `desktop_engine_core.go` | Last broadcast state payload |
| `refreshMu` | `sync.RWMutex` | `desktop_engine_core.go` | Refresh handler function pointer |
| `closeOnce` | `sync.Once` | `desktop_engine_core.go` | Desktop shutdown |
| `lastPublishNanos` | `atomic.Int64` | `desktop_engine_core.go` | Publish duration metric |
| `renderGen` | `atomic.Int32` | `pane.go` | Per-pane dirty generation counter |
| `lastRendered` | `int32` | `pane.go` | Last rendered generation (event-loop only) |
| `transitions.mu` | `sync.Mutex` | `layout_transitions.go` | Animation state map |
| `dispatcher.mu` | `sync.RWMutex` | `dispatcher.go` | Event listener slice |

### Channels

| Channel | File | Buffer | Purpose |
|---------|------|--------|---------|
| `eventCh` | `desktop_engine_core.go` | 64 | Key/mouse/paste events routed to event loop |
| `animCh` | `desktop_engine_core.go` | 16 | Animation frame signals routed to event loop |
| `refreshCh` | `desktop_engine_core.go` | 16 | Pane refresh signals routed to event loop |
| `quit` | `desktop_engine_core.go` | 0 | Desktop shutdown signal |
| `refreshStop` | `pane.go` | 0 | Per-pane forwarder shutdown |
| `stopCh` | `layout_transitions.go` | 0 | Animation loop shutdown |

### Event Loop Ownership

The desktop event loop (`DesktopEngine.Run`) is the **sole owner** of these
unprotected structures:

- **Tree** (`tree.go`): `Root`, `ActiveLeaf`, `Node` pointers, `SplitRatios`
- **Pane fields**: `absX0/Y0/X1/Y1`, `app`, `name`, `prevBuf`, `IsActive`
- **Workspace fields**: `tree`, `resizeSelection`, `mouseResizeBorder`

All access to these structures is serialized through the event loop. Other
goroutines interact with the tree exclusively via channels:

- **Key/mouse/paste events** are sent to `eventCh` by connection goroutines.
  The event loop receives them and dispatches to the appropriate pane/app.

- **Animation frames** are sent to `animCh` by the `LayoutTransitionManager`
  ticker. The event loop applies ratio updates, recalculates layout, and
  broadcasts tree snapshots. This eliminates the former race between the
  animation goroutine and the event loop.

- **Pane refresh signals** are sent to `refreshCh` by per-pane forwarders.
  The event loop receives them and calls `Publish()`. This replaces the
  former per-workspace refresh monitor goroutine.

### Cross-Thread Touchpoints

1. **Per-pane forwarder** only touches `renderGen` (atomic) and `refreshCh`
   (channel). No shared mutable state.

### Former Dead Code (Cleaned Up)

- `drawChan` was removed from `workspace.go` as part of this audit. It was
  declared and initialized but never read or written.

---

## Layer 4: Server Runtime (`internal/runtime/server/`)

### Lock Inventory

| Lock | Type | File | Protects |
|------|------|------|----------|
| `DesktopPublisher.mu` | `sync.Mutex` | `desktop_publisher.go` | Revisions map, prev buffers, notify callback |
| `DesktopSink.mu` | `sync.Mutex` | `desktop_sink.go` | Publisher pointer (all reads and writes) |
| `Session.mu` | `sync.Mutex` | `session.go` | Diff queue, sequence counter, closed flag |
| `Connection.writeMu` | `sync.Mutex` | `connection.go` | Network write serialization |
| `Manager.mu` | `sync.RWMutex` | `manager.go` | Sessions map |
| `Server.bootSnapshotMu` | `sync.RWMutex` | `server.go` | Boot snapshot for new connections |
| `SnapshotStore.mu` | `sync.Mutex` | `snapshot_store.go` | Snapshot file I/O |
| `FocusMetrics.mu` | `sync.Mutex` | `focus_metrics.go` | Focus counter and last pane |

### Channels

| Channel | File | Buffer | Purpose |
|---------|------|--------|---------|
| `incoming` | `connection.go` | 32 | Read loop → serve loop messages |
| `readErr` | `connection.go` | 1 | Read error notification |
| `pending` | `connection.go` | 1 | Nudge signal to send pending diffs |
| `stop` | `connection.go` | 0 | Connection shutdown |
| `quit` | `server.go` | 0 | Server shutdown |
| `snapshotQuit` | `server.go` | 0 | Snapshot loop shutdown |

### Lock Ordering

```
Manager.mu  →  Session.mu
                (SetDiffRetentionLimit iterates sessions)
                (Close calls session.Close)

DesktopPublisher.mu  →  Session.mu
                        (Publish → session.EnqueueDiff)

Dispatcher.mu (RLock)  →  (event handler callbacks)
                          Callbacks must not acquire Dispatcher.mu
```

**Rule**: Manager and Publisher are outer locks; Session is inner. Never
acquire Manager.mu or Publisher.mu while holding Session.mu.

### Former Issue: DesktopSink.SetPublisher Race -- FIXED

All publisher access now goes through `d.mu` (snapshot-under-lock pattern).
Both `SetPublisher()` writes and method reads of `d.publisher` are protected.

---

## Layer 5: Client Runtime (`internal/runtime/client/`, `client/`)

### Lock Inventory

| Lock | Type | File | Protects |
|------|------|------|----------|
| `BufferCache.mu` | `sync.RWMutex` | `buffercache.go` | Panes map, ordering slice |
| `PaneState.rowsMu` | `sync.RWMutex` | `buffercache.go` | Per-pane row data |
| `clientState.clipboardMu` | `sync.Mutex` | `client_state.go` | Clipboard state |
| `clientState.resizeMu` | `sync.Mutex` | `client_state.go` | Pending resize |
| `writeMu` | `sync.Mutex` | `app.go` | Network write serialization |
| `PanicLogger.mu` | `sync.Mutex` | `panic_logger.go` | Crash log file |

### Atomics

| Variable | Type | File | Purpose |
|----------|------|------|---------|
| `pendingAck` | `atomic.Uint64` | `app.go` | Last sequence needing ACK |
| `lastAck` | `atomic.Uint64` | `app.go` | Last ACK actually sent |

### Lock Ordering

```
BufferCache.mu  →  PaneState.rowsMu
                    (ApplyDelta, ApplySnapshot hold outer, acquire inner)
```

**Rule**: Always acquire `BufferCache.mu` before `PaneState.rowsMu`. The
inner lock is only acquired while the outer lock is already held.

### Channels

| Channel | File | Buffer | Purpose |
|---------|------|--------|---------|
| `renderCh` | `app.go` | 64 | Trigger render cycle |
| `doneCh` | `app.go` | 0 | Shutdown from read loop |
| `stopEvents` | `app.go` | 0 | Stop tcell event polling |
| `events` | `app.go` | 32 | Incoming tcell events |
| `ackSignal` | `app.go` | 1 | Trigger ACK send |
| `pingStop` | `app.go` | 0 | Stop ping loop |

---

## Known Issues (Severity-Ranked)

### 1. Tree Structure Has No Formal Synchronization -- FIXED

**Location**: `texel/tree.go`, `texel/workspace.go`, `texel/layout_transitions.go`

**Was**: The pane tree was accessed from three goroutines (event loop,
animation ticker, refresh monitor) without locks, relying on implicit
mutual exclusion invariants.

**Fix**: The desktop event loop (`DesktopEngine.Run`) now serializes all
tree access. Animation frames arrive via `animCh`, refresh signals via
`refreshCh`, and events via `eventCh`. No goroutine other than the event
loop touches tree state.

### 2. DesktopSink.SetPublisher Unsynchronized Write -- FIXED

**Location**: `internal/runtime/server/desktop_sink.go`

**Was**: `SetPublisher()` wrote `d.publisher` without `d.mu`. Other methods
read it without `d.mu`.

**Fix**: All publisher access now goes through `d.mu` (snapshot-under-lock
pattern). Both reads and writes are protected.

### 3. TexelTerm Clipboard Callback May Deadlock -- FIXED

**Location**: `apps/texelterm/term.go`

**Was**: OSC 52 clipboard handlers called `a.mu.Lock()`, but the PTY reader
already held `a.mu` when calling `Parse()`. If handlers fired inline during
`Parse()`, this was a deadlock.

**Fix**: A dedicated `clipboardMu` protects the `a.clipboard` field. OSC 52
handlers no longer touch `a.mu`. `clipboardMu` is never nested with `a.mu`.

### 4. Workspace drawChan Dead Code (Fixed)

Removed in a prior commit. Was declared and initialized but never used.

---

## Rules for Future Development

1. **Lock ordering is non-negotiable.** If you need to hold two locks, acquire
   the outer lock first per the diagrams above. If the ordering doesn't work
   for your use case, refactor rather than invert.

2. **The tree is owned by the desktop event loop goroutine.**
   Never access tree state (Root, ActiveLeaf, Node.Children, Node.SplitRatios,
   pane dimensions) from any goroutine other than the event loop. Use
   SendEvent/SendRefresh/SendAnimationFrame to route work to the event loop.

3. **Channels for signaling, mutexes for shared state.** Use channels for
   "something happened" notifications (refresh, shutdown, nudge). Use mutexes
   only when multiple goroutines must read/write the same data structure.

4. **Clone before background flush.** Any line data passed to a background
   goroutine must be cloned first. The main goroutine continues writing to
   the original.

5. **WAL metadata must be synced.** After every `checkpointLocked()` or
   metadata re-write, call `walFile.Sync()`. See bug history in MEMORY.md.

6. **RLock is only safe for pure reads.** If a "read" method calls code with
   lazy initialization side effects, it needs a write Lock. The ViewportWindow
   fix (2026-02-15) is the canonical example.

7. **Test with `-race`.** Run `go test -race ./...` before merging any change
   that touches synchronization code.

8. **`pane.setupRefreshForwarder()` is the only way to wire refresh.**
   Never set `SetRefreshNotifier` directly. The forwarder increments
   `renderGen`; bypassing it means `renderBuffer()` always returns stale cache.

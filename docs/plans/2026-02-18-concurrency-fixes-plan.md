# Concurrency Architecture Fixes — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix all 3 concurrency issues found in the architecture audit (tree synchronization, SetPublisher race, clipboard deadlock) and update the architecture doc.

**Architecture:** Create a desktop event loop goroutine that is the sole accessor of tree state, fix the SetPublisher mutex race, add a dedicated clipboardMu for OSC 52 handlers.

**Tech Stack:** Go 1.24.3, channels, sync.Mutex, atomic operations

---

## Task 1: Fix Issue 3 — TexelTerm Clipboard Deadlock

Smallest, most isolated fix. No dependencies on the event loop work.

**Files:**
- Modify: `apps/texelterm/term.go:1021-1028` (SetClipboardService)
- Modify: `apps/texelterm/term.go:1313-1320` (OnClipboardSet handler)

### Step 1: Write the failing test

Create a test that demonstrates the deadlock by calling SetClipboardService while Parse is running. Since actual deadlock would hang the test, use `-timeout` and verify the clipboard value is set correctly.

Add to `apps/texelterm/term_test.go` (or create if needed — check first):

```go
func TestClipboardServiceNoDeadlock(t *testing.T) {
    // The OSC 52 handler fires during Parse(), which holds a.mu.
    // If SetClipboardService also uses a.mu, setting clipboard from
    // the handler would deadlock. This test verifies it completes.
    tt := New("test", "echo hello")

    var gotData []byte
    mock := &mockClipboardService{
        setFn: func(mime string, data []byte) {
            gotData = data
        },
    }
    tt.SetClipboardService(mock)

    // Simulate OSC 52 set clipboard: ESC ] 52 ; c ; <base64> BEL
    // "hello" in base64 = "aGVsbG8="
    input := "\x1b]52;c;aGVsbG8=\x07"
    // This must not deadlock — Parse fires OnClipboardSet synchronously
    // while the PTY reader would normally hold a.mu
    tt.mu.Lock()
    if tt.vterm != nil && tt.parser != nil {
        for _, r := range input {
            tt.parser.Parse(r)
        }
    }
    tt.mu.Unlock()

    if string(gotData) != "hello" {
        t.Errorf("clipboard data: got %q, want %q", gotData, "hello")
    }
}
```

### Step 2: Run test to verify it fails (or deadlocks)

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/ -run TestClipboardServiceNoDeadlock -timeout 5s -count=1`

Expected: Deadlock (test hangs and times out) or fails because the handler tries to re-acquire `a.mu`.

### Step 3: Implement the fix

In `apps/texelterm/term.go`, add a `clipboardMu sync.Mutex` field to the TexelTerm struct (near the existing `clipboard` field).

Update `SetClipboardService` (line 1023):

```go
func (a *TexelTerm) SetClipboardService(clipboard texelcore.ClipboardService) {
    a.clipboardMu.Lock()
    a.clipboard = clipboard
    a.clipboardMu.Unlock()
    log.Printf("CLIPBOARD DEBUG: %s SetClipboardService called: service=%v", a.title, clipboard != nil)
}
```

Update the OnClipboardSet handler (line 1313, in `initializeVTermForRestart`):

```go
a.vterm.OnClipboardSet = func(data []byte) {
    a.clipboardMu.Lock()
    clipboard := a.clipboard
    a.clipboardMu.Unlock()
    if clipboard != nil {
        clipboard.SetClipboard("text/plain", data)
    }
}
a.vterm.OnClipboardGet = func() []byte {
    a.clipboardMu.Lock()
    clipboard := a.clipboard
    a.clipboardMu.Unlock()
    if clipboard != nil {
        _, data, ok := clipboard.GetClipboard()
        if ok {
            return data
        }
    }
    return nil
}
```

Also find the OnClipboardSet/OnClipboardGet handler in `initializeVTermFirstRun` and apply the same pattern (use `clipboardMu` instead of `a.mu`).

### Step 4: Run test to verify it passes

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/ -run TestClipboardServiceNoDeadlock -timeout 10s -count=1`

Expected: PASS

### Step 5: Run full texelterm test suite

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/... -race -count=1 -timeout 120s`

Expected: All tests pass, no race conditions detected.

### Step 6: Commit

```bash
git add apps/texelterm/term.go apps/texelterm/term_test.go
git commit -m "fix: use dedicated clipboardMu to prevent OSC 52 deadlock in texelterm

OSC 52 clipboard handlers fire synchronously during Parse() while
a.mu is held. Using a.mu inside the handler causes deadlock since
Go mutexes are not reentrant. Add clipboardMu that protects only
the clipboard field, eliminating the re-entrant lock attempt."
```

---

## Task 2: Fix Issue 2 — DesktopSink.SetPublisher Race

Small, isolated fix. Only touches `desktop_sink.go`.

**Files:**
- Modify: `internal/runtime/server/desktop_sink.go:32-42` (HandleKeyEvent)
- Modify: `internal/runtime/server/desktop_sink.go:44-52` (HandleMouseEvent)
- Modify: `internal/runtime/server/desktop_sink.go:84-92` (HandlePaste)
- Modify: `internal/runtime/server/desktop_sink.go:98-100` (Publisher)
- Modify: `internal/runtime/server/desktop_sink.go:102-112` (SetPublisher)
- Modify: `internal/runtime/server/desktop_sink.go:114-118` (Publish)

### Step 1: Write the failing test

Add a race-detection test to `internal/runtime/server/desktop_sink_test.go`:

```go
func TestDesktopSink_SetPublisherConcurrent(t *testing.T) {
    // This test should be run with -race to detect the data race
    // on d.publisher between SetPublisher and HandleKeyEvent/Publish.
    driver := &texel.HeadlessDriver{}
    shellFactory := func() texelcore.App { return &noopApp{} }
    lifecycle := &texel.DefaultLifecycle{}
    desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
    if err != nil {
        t.Fatalf("NewDesktopEngineWithDriver: %v", err)
    }
    defer desktop.Close()
    desktop.SwitchToWorkspace(1)

    sink := NewDesktopSink(desktop)

    var wg sync.WaitGroup
    wg.Add(2)

    // Goroutine 1: repeatedly set publisher
    go func() {
        defer wg.Done()
        for i := 0; i < 100; i++ {
            sink.SetPublisher(nil)
        }
    }()

    // Goroutine 2: repeatedly call Publish (reads d.publisher)
    go func() {
        defer wg.Done()
        for i := 0; i < 100; i++ {
            sink.Publish()
        }
    }()

    wg.Wait()
}
```

### Step 2: Run test to verify race is detected

Run: `cd /home/marc/projects/texel/texelation && go test ./internal/runtime/server/ -run TestDesktopSink_SetPublisherConcurrent -race -count=1 -timeout 30s`

Expected: FAIL with data race warning from `-race`.

### Step 3: Implement the fix

In `desktop_sink.go`, add `d.mu.Lock()`/`d.mu.Unlock()` to `SetPublisher` and use snapshot-under-lock pattern in all methods that read `d.publisher`:

**SetPublisher** (line 102):
```go
func (d *DesktopSink) SetPublisher(publisher *DesktopPublisher) {
    d.mu.Lock()
    d.publisher = publisher
    d.mu.Unlock()
    if d.desktop == nil {
        return
    }
    if publisher == nil {
        d.desktop.SetRefreshHandler(nil)
        return
    }
    d.desktop.SetRefreshHandler(d.publish)
}
```

**HandleKeyEvent** (line 32):
```go
func (d *DesktopSink) HandleKeyEvent(session *Session, event protocol.KeyEvent) {
    if d.desktop == nil {
        return
    }
    key := tcell.Key(event.KeyCode)
    mod := tcell.ModMask(event.Modifiers)
    d.desktop.InjectKeyEvent(key, event.RuneValue, mod)
    d.mu.Lock()
    publisher := d.publisher
    d.mu.Unlock()
    if publisher != nil {
        _ = publisher.Publish()
    }
}
```

Apply the same snapshot pattern to **HandleMouseEvent** (line 44), **HandlePaste** (line 84), **Publish** (line 114), and **Publisher** (line 98).

### Step 4: Run test to verify race is gone

Run: `cd /home/marc/projects/texel/texelation && go test ./internal/runtime/server/ -run TestDesktopSink_SetPublisherConcurrent -race -count=1 -timeout 30s`

Expected: PASS with no race warnings.

### Step 5: Run full server test suite

Run: `cd /home/marc/projects/texel/texelation && go test ./internal/runtime/server/... -race -count=1 -timeout 120s`

Expected: All tests pass.

### Step 6: Commit

```bash
git add internal/runtime/server/desktop_sink.go internal/runtime/server/desktop_sink_test.go
git commit -m "fix: synchronize all DesktopSink.publisher access with mu

SetPublisher wrote d.publisher without d.mu while HandleKeyEvent,
HandleMouseEvent, HandlePaste, and Publish read it without d.mu.
Apply snapshot-under-lock pattern to all publisher access."
```

---

## Task 3: Add Event Loop Infrastructure to DesktopEngine

Core of Issue 1. Add channels, event types, and the `Run()` method.

**Files:**
- Modify: `texel/desktop_engine_core.go`

### Step 1: Define event types and add channels to struct

Add to `texel/desktop_engine_core.go`, before the DesktopEngine struct or in a new section after the struct:

```go
// eventKind identifies the type of desktop event.
type eventKind int

const (
    keyEventKind eventKind = iota
    mouseEventKind
    pasteEventKind
    resizeEventKind
)

// desktopEvent is a tagged union for all events processed by the desktop event loop.
type desktopEvent struct {
    kind    eventKind
    key     tcell.Key
    ch      rune
    mod     tcell.ModMask
    mx, my  int
    buttons tcell.ButtonMask
    paste   []byte
    width   int
    height  int
}

// animationFrame carries interpolated ratios from the animation ticker to the event loop.
type animationFrame struct {
    node       *Node
    ratios     []float64
    done       bool
    onComplete func()
}
```

Add channels to the DesktopEngine struct (near the existing `quit` channel):

```go
    eventCh   chan desktopEvent    // Key/mouse/paste/resize events
    animCh    chan animationFrame  // Animation ratio updates
    refreshCh chan struct{}        // Pane dirty signals
```

### Step 2: Initialize channels in NewDesktopEngineWithDriver

In `NewDesktopEngineWithDriver` (line 174), add channel initialization alongside the existing `quit: make(chan struct{})`:

```go
    eventCh:   make(chan desktopEvent, 64),
    animCh:    make(chan animationFrame, 16),
    refreshCh: make(chan struct{}, 16),
```

### Step 3: Implement Run() method

Add the `Run()` method:

```go
// Run starts the desktop event loop. This goroutine is the sole accessor of
// tree state (Root, ActiveLeaf, SplitRatios, pane dimensions). Must be called
// after NewDesktopEngine and before events are injected.
func (d *DesktopEngine) Run() {
    for {
        select {
        case ev := <-d.eventCh:
            d.processDesktopEvent(ev)
            d.publishIfDirty()
        case frame := <-d.animCh:
            d.applyAnimationFrame(frame)
            d.publishIfDirty()
        case <-d.refreshCh:
            d.publishIfDirty()
        case <-d.quit:
            return
        }
    }
}

// processDesktopEvent dispatches a desktopEvent to the appropriate handler.
func (d *DesktopEngine) processDesktopEvent(ev desktopEvent) {
    switch ev.kind {
    case keyEventKind:
        tcellEvent := tcell.NewEventKey(ev.key, ev.ch, ev.mod)
        d.handleEvent(tcellEvent)
    case mouseEventKind:
        d.processMouseEvent(ev.mx, ev.my, ev.buttons, ev.mod)
    case pasteEventKind:
        d.handlePasteInternal(ev.paste)
    case resizeEventKind:
        d.handleResizeInternal(ev.width, ev.height)
    }
}

// applyAnimationFrame applies interpolated ratios from the animation ticker.
func (d *DesktopEngine) applyAnimationFrame(frame animationFrame) {
    if frame.node != nil {
        frame.node.SplitRatios = frame.ratios
    }
    d.recalculateLayout()
    d.broadcastTreeChanged()
    if frame.done && frame.onComplete != nil {
        frame.onComplete()
    }
}

// publishIfDirty calls the refresh handler if one is set.
func (d *DesktopEngine) publishIfDirty() {
    if handler := d.refreshHandlerFunc(); handler != nil {
        handler()
    }
}

// SendEvent enqueues an event for the event loop to process.
// Non-blocking: drops if the event channel is full (unlikely at capacity 64).
func (d *DesktopEngine) SendEvent(ev desktopEvent) {
    select {
    case d.eventCh <- ev:
    default:
        log.Printf("[DESKTOP] Event channel full, dropping event kind=%d", ev.kind)
    }
}

// SendRefresh signals the event loop that a pane has dirty content.
func (d *DesktopEngine) SendRefresh() {
    select {
    case d.refreshCh <- struct{}{}:
    default:
    }
}
```

Add internal helpers for paste and resize that don't go through the event channel:

```go
// handlePasteInternal routes paste data to the active pane (called from event loop).
func (d *DesktopEngine) handlePasteInternal(data []byte) {
    if len(data) == 0 || d.inControlMode {
        return
    }
    if d.zoomedPane != nil && d.zoomedPane.Pane != nil {
        d.zoomedPane.Pane.handlePaste(data)
        return
    }
    if d.activeWorkspace != nil {
        d.activeWorkspace.handlePaste(data)
    }
}

// handleResizeInternal processes resize events (called from event loop).
func (d *DesktopEngine) handleResizeInternal(width, height int) {
    d.recalculateLayout()
}
```

### Step 4: Verify it compiles

Run: `cd /home/marc/projects/texel/texelation && go build ./texel/...`

Expected: Compiles without errors. (Run() is not called yet; existing code paths are unchanged.)

### Step 5: Commit

```bash
git add texel/desktop_engine_core.go
git commit -m "feat: add desktop event loop infrastructure (channels, Run, event types)

Add eventCh, animCh, refreshCh channels and the Run() event loop
goroutine that will become the sole accessor of tree state. Also
add desktopEvent tagged union, animationFrame struct, and helper
methods. Existing code paths are unchanged — migration happens in
subsequent commits."
```

---

## Task 4: Convert Event Injection to Async (InjectKeyEvent, InjectMouseEvent, HandlePaste)

Wire the public API methods to send through the event channel.

**Files:**
- Modify: `texel/desktop_engine_core.go:955-966` (InjectKeyEvent)
- Modify: `texel/desktop_engine_core.go:849-851` (InjectMouseEvent)
- Modify: `texel/desktop_engine_core.go:916-927` (HandlePaste)

### Step 1: Convert InjectKeyEvent

Replace `d.handleEvent(event)` with channel send:

```go
func (d *DesktopEngine) InjectKeyEvent(key tcell.Key, ch rune, modifiers tcell.ModMask) {
    if key == tcell.KeyRune {
        switch ch {
        case '\n', '\r':
            key = tcell.KeyEnter
        case '\t':
            key = tcell.KeyTab
        }
    }
    d.SendEvent(desktopEvent{kind: keyEventKind, key: key, ch: ch, mod: modifiers})
}
```

### Step 2: Convert InjectMouseEvent

```go
func (d *DesktopEngine) InjectMouseEvent(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) {
    d.SendEvent(desktopEvent{kind: mouseEventKind, mx: x, my: y, buttons: buttons, mod: modifiers})
}
```

### Step 3: Convert HandlePaste

```go
func (d *DesktopEngine) HandlePaste(data []byte) {
    if len(data) == 0 {
        return
    }
    // Copy data since the caller may reuse the slice
    copied := make([]byte, len(data))
    copy(copied, data)
    d.SendEvent(desktopEvent{kind: pasteEventKind, paste: copied})
}
```

### Step 4: Verify it compiles

Run: `cd /home/marc/projects/texel/texelation && go build ./texel/...`

Expected: Compiles.

### Step 5: Commit

```bash
git add texel/desktop_engine_core.go
git commit -m "refactor: route InjectKeyEvent/InjectMouseEvent/HandlePaste through event channel

Connection goroutine no longer calls handleEvent/processMouseEvent
directly. Events are sent to eventCh and processed by Run()."
```

---

## Task 5: Convert Animation Ticker to Channel-Based

The animation loop sends frames to `animCh` instead of writing tree state directly.

**Files:**
- Modify: `texel/layout_transitions.go:92-106` (startAnimationLoop)
- Modify: `texel/layout_transitions.go:220-291` (updateAnimations)

### Step 1: Replace direct tree writes in updateAnimations

In `updateAnimations()`, instead of writing to `node.SplitRatios` and calling `m.desktop.recalculateLayout()` / `m.desktop.broadcastTreeChanged()`, send frames through the animation channel.

Replace the body of `updateAnimations()`:

```go
func (m *LayoutTransitionManager) updateAnimations() {
    m.mu.Lock()
    defer m.mu.Unlock()

    if len(m.animating) == 0 {
        return
    }

    now := time.Now()
    completed := make([]*Node, 0)

    for node, state := range m.animating {
        elapsed := now.Sub(state.startTime)
        progress := float64(elapsed) / float64(state.duration)

        var ratios []float64
        var done bool
        var callback func()

        if progress >= 1.0 {
            ratios = state.targetRatios
            done = true
            callback = state.onComplete
            completed = append(completed, node)
            log.Printf("LayoutTransitionManager: Animation complete for node (final ratios: %v)", state.targetRatios)
        } else {
            t := m.applyEasing(progress, state.easing)
            ratios = make([]float64, len(state.startRatios))
            for i := range ratios {
                ratios[i] = state.startRatios[i] + (state.targetRatios[i]-state.startRatios[i])*t
            }

            // Early completion for removal animations
            if state.onComplete != nil {
                for i, ratio := range ratios {
                    if state.targetRatios[i] < 0.01 && ratio < 0.005 {
                        ratios = state.targetRatios
                        done = true
                        callback = state.onComplete
                        completed = append(completed, node)
                        log.Printf("LayoutTransitionManager: Early completion for removal (ratio %v reached)", ratio)
                        break
                    }
                }
            }
        }

        // Send frame to event loop for tree-safe application
        m.desktop.SendAnimationFrame(animationFrame{
            node:       node,
            ratios:     ratios,
            done:       done,
            onComplete: callback,
        })
    }

    for _, node := range completed {
        delete(m.animating, node)
    }
}
```

### Step 2: Add SendAnimationFrame to DesktopEngine

In `desktop_engine_core.go`:

```go
// SendAnimationFrame sends an animation frame to the event loop for tree-safe application.
func (d *DesktopEngine) SendAnimationFrame(frame animationFrame) {
    select {
    case d.animCh <- frame:
    default:
        // Channel full — apply directly as fallback (animation may stutter)
        log.Printf("[DESKTOP] Animation channel full, dropping frame")
    }
}
```

### Step 3: Verify it compiles

Run: `cd /home/marc/projects/texel/texelation && go build ./texel/...`

Expected: Compiles.

### Step 4: Commit

```bash
git add texel/layout_transitions.go texel/desktop_engine_core.go
git commit -m "refactor: route animation frames through animCh instead of direct tree writes

Animation ticker no longer writes Node.SplitRatios or calls
recalculateLayout() directly. Frames are sent to animCh and
applied by the event loop goroutine."
```

---

## Task 6: Eliminate Refresh Monitor, Wire Pane Refresh to Event Loop

Remove the per-workspace refresh monitor goroutine and route pane dirty signals through `refreshCh`.

**Files:**
- Modify: `texel/workspace.go:85,137-180` (refreshChan, Refresh, startRefreshMonitor)
- Modify: `texel/pane.go:117-145` (setupRefreshForwarder)
- Modify: `texel/desktop_engine_core.go:1077-1091` (SetRefreshHandler)

### Step 1: Update setupRefreshForwarder to use desktop refreshCh

In `pane.go`, change `setupRefreshForwarder` to accept `chan<- struct{}` instead of `chan<- bool`:

```go
func (p *pane) setupRefreshForwarder(target chan<- struct{}) chan<- bool {
    if p.refreshStop != nil {
        close(p.refreshStop)
    }

    ch := make(chan bool, 1)
    stop := make(chan struct{})
    p.refreshStop = stop

    go func() {
        for {
            select {
            case <-stop:
                return
            case _, ok := <-ch:
                if !ok {
                    return
                }
                atomic.AddInt32(&p.renderGen, 1)
                select {
                case target <- struct{}{}:
                default:
                }
            }
        }
    }()

    return ch
}
```

### Step 2: Update all callers of setupRefreshForwarder

Search for calls to `setupRefreshForwarder` across the codebase. They currently pass `ws.refreshChan` (a `chan bool`). Update them to pass `d.refreshCh` (a `chan struct{}`).

Key callers are in `workspace.go` (when adding panes) and `snapshot_restore.go` (when restoring panes). Update each to use `p.screen.refreshCh` (where `p.screen` is the `DesktopEngine`).

### Step 3: Remove startRefreshMonitor and refreshChan from Workspace

In `workspace.go`:
- Remove `refreshChan` field from Workspace struct initialization (line 85)
- Remove `refreshMonitorOnce sync.Once` field
- Remove `startRefreshMonitor()` method (lines 144-180)
- Remove `Refresh()` method (lines 137-142) — or repurpose it to send to `d.refreshCh`

### Step 4: Update SetRefreshHandler

In `desktop_engine_core.go:1077`, remove the loop that starts refresh monitors:

```go
func (d *DesktopEngine) SetRefreshHandler(handler func()) {
    d.refreshMu.Lock()
    d.refreshHandler = func() {
        d.broadcastStateUpdate()
        if handler != nil {
            handler()
        }
    }
    d.refreshMu.Unlock()
}
```

### Step 5: Verify it compiles

Run: `cd /home/marc/projects/texel/texelation && go build ./...`

Expected: Compiles. May need to fix additional callers found during compilation.

### Step 6: Run tests

Run: `cd /home/marc/projects/texel/texelation && go test ./texel/... -race -count=1 -timeout 120s`

Expected: All tests pass. Some tests may need updating if they relied on `Refresh()` or `refreshChan`.

### Step 7: Commit

```bash
git add texel/workspace.go texel/pane.go texel/desktop_engine_core.go
git commit -m "refactor: eliminate per-workspace refresh monitor goroutine

Pane dirty signals now route through desktop.refreshCh instead of
workspace.refreshChan. The event loop's refreshCh case handles
publishing. Removes startRefreshMonitor() and refreshMonitorOnce."
```

---

## Task 7: Wire DesktopSink to Use Event Channel

Update `desktop_sink.go` so HandleKeyEvent/HandleMouseEvent/HandlePaste send through the event channel instead of calling desktop methods directly.

**Files:**
- Modify: `internal/runtime/server/desktop_sink.go:32-42,44-52,84-92`

### Step 1: Remove synchronous Publish from HandleKeyEvent

The event loop now calls `publishIfDirty()` after each event, so the synchronous `d.publisher.Publish()` in HandleKeyEvent/HandleMouseEvent/HandlePaste is no longer needed.

```go
func (d *DesktopSink) HandleKeyEvent(session *Session, event protocol.KeyEvent) {
    if d.desktop == nil {
        return
    }
    key := tcell.Key(event.KeyCode)
    mod := tcell.ModMask(event.Modifiers)
    d.desktop.InjectKeyEvent(key, event.RuneValue, mod)
    // No explicit Publish() — the event loop publishes after handling.
}

func (d *DesktopSink) HandleMouseEvent(session *Session, event protocol.MouseEvent) {
    if d.desktop == nil {
        return
    }
    d.desktop.InjectMouseEvent(int(event.X), int(event.Y), tcell.ButtonMask(event.ButtonMask), tcell.ModMask(event.Modifiers))
}

func (d *DesktopSink) HandlePaste(session *Session, paste protocol.Paste) {
    if d.desktop == nil || len(paste.Data) == 0 {
        return
    }
    d.desktop.HandlePaste(paste.Data)
}
```

### Step 2: Verify it compiles

Run: `cd /home/marc/projects/texel/texelation && go build ./internal/runtime/server/...`

Expected: Compiles.

### Step 3: Run server tests

Run: `cd /home/marc/projects/texel/texelation && go test ./internal/runtime/server/... -race -count=1 -timeout 120s`

Expected: Tests pass. Some may need adjustment if they expected synchronous publish after event injection.

### Step 4: Commit

```bash
git add internal/runtime/server/desktop_sink.go
git commit -m "refactor: remove synchronous Publish from DesktopSink event handlers

Event loop now publishes after handling each event. The connection
goroutine no longer needs to call Publish() explicitly."
```

---

## Task 8: Start Event Loop in Server and Standalone Entry Points

Wire `desktop.Run()` as a goroutine in the server and standalone binaries.

**Files:**
- Modify: `cmd/texel-server/main.go` (~line 105, after desktop creation)
- Modify: `cmd/texelation/main.go` (or `cmd/texelation/lifecycle/` — find where desktop is created)

### Step 1: Add `go desktop.Run()` in texel-server

In `cmd/texel-server/main.go`, after line 105 (`desktop.SwitchToWorkspace(1)`):

```go
go desktop.Run()
```

### Step 2: Add `go desktop.Run()` in texelation (combined mode)

Find where the desktop is created in the combined binary and add `go desktop.Run()` after it.

### Step 3: Verify it compiles

Run: `cd /home/marc/projects/texel/texelation && make build`

Expected: All binaries compile.

### Step 4: Run full test suite

Run: `cd /home/marc/projects/texel/texelation && go test ./... -race -count=1 -timeout 300s`

Expected: All tests pass. Tests that create DesktopEngine directly may need `go desktop.Run()` added.

### Step 5: Commit

```bash
git add cmd/texel-server/main.go cmd/texelation/
git commit -m "feat: start desktop event loop goroutine in server and standalone binaries

desktop.Run() is launched as a goroutine after desktop creation.
All tree access is now serialized through the event loop."
```

---

## Task 9: Fix Tests That Create DesktopEngine Directly

Many tests create DesktopEngine without calling `Run()`. They need updating.

**Files:**
- Modify: `texel/desktop_engine_test.go`
- Modify: `texel/desktop_engine_integration_test.go`
- Modify: `internal/runtime/server/desktop_sink_test.go`
- Modify: `internal/runtime/server/desktop_publisher_test.go`
- Modify: Other test files that call `NewDesktopEngineWithDriver`

### Step 1: Find all test files that create DesktopEngine

Search output from earlier grep shows ~20 test files. Each needs `go desktop.Run()` after creation.

### Step 2: Add event loop startup to each test

For each test function that creates a DesktopEngine, add `go desktop.Run()` after creation and ensure `desktop.Close()` is called (which sends to `quit` channel to stop Run).

### Step 3: Run full test suite with race detector

Run: `cd /home/marc/projects/texel/texelation && go test ./texel/... ./internal/runtime/server/... -race -count=1 -timeout 300s`

Expected: All tests pass, no race conditions.

### Step 4: Commit

```bash
git add texel/*_test.go internal/runtime/server/*_test.go
git commit -m "test: start event loop in all tests that create DesktopEngine

Add go desktop.Run() after NewDesktopEngineWithDriver calls in
tests so events are processed through the event loop."
```

---

## Task 10: Update Architecture Documentation

Update `docs/CONCURRENCY_ARCHITECTURE.md` to reflect all fixes.

**Files:**
- Modify: `docs/CONCURRENCY_ARCHITECTURE.md`

### Step 1: Update goroutine inventory

- Add "Desktop Event Loop" goroutine entry (one per DesktopEngine, runs `Run()`)
- Remove "Refresh Monitor" goroutine entry
- Note that animation ticker still exists but no longer writes tree state

### Step 2: Update Layer 3 (Desktop Engine) section

- Add event loop description with channel types and capacities
- Document the tree ownership rule: "The event loop goroutine is the sole accessor of tree state"
- Document the new data flow: connection → eventCh → event loop → handleEvent → publishIfDirty

### Step 3: Update lock inventory

- Add `clipboardMu` in Layer 2 (TexelTerm) section
- Note that `refreshMu` scope is now simpler (no refresh monitor to coordinate with)

### Step 4: Mark issues as fixed

- Issue #1 (Tree sync): Fixed — event loop serializes all tree access
- Issue #2 (SetPublisher): Fixed — all publisher access through mu
- Issue #3 (Clipboard deadlock): Fixed — dedicated clipboardMu

### Step 5: Update Rule #2

Change from "Never add a new mutex to protect tree state" to:
"The tree is owned by the desktop event loop goroutine. Never access tree state from other goroutines."

### Step 6: Commit

```bash
git add docs/CONCURRENCY_ARCHITECTURE.md
git commit -m "docs: update concurrency architecture for event loop and fixes

Mark all 3 issues as fixed. Add event loop goroutine to inventory,
remove refresh monitor, update Layer 3 section, add clipboardMu."
```

---

## Task 11: Final Build and Race Detector Verification

### Step 1: Full build

Run: `cd /home/marc/projects/texel/texelation && make build`

Expected: All binaries compile.

### Step 2: Full test suite with race detector

Run: `cd /home/marc/projects/texel/texelation && go test ./... -race -count=1 -timeout 300s`

Expected: All tests pass, no race conditions detected.

### Step 3: Targeted race tests

Run: `cd /home/marc/projects/texel/texelation && go test ./texel/... -race -count=3 -timeout 300s`
Run: `cd /home/marc/projects/texel/texelation && go test ./internal/runtime/server/... -race -count=3 -timeout 300s`
Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/... -race -count=3 -timeout 300s`

Expected: All pass on all 3 runs.

---

## Verification Checklist

1. `make build` — all binaries compile
2. `go test ./texel/... -race -count=1` — desktop engine tests
3. `go test ./internal/runtime/server/... -race -count=1` — server runtime tests
4. `go test ./apps/texelterm/... -race -count=1` — texelterm tests
5. No new data races detected by `-race` flag
6. Architecture doc accurately reflects current state

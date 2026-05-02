# Client Event-Loop Fairness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop the client's main event loop from making input lose select coin flips against `renderCh` ticks under heavy server traffic. Adds two pure helpers (`coalesceRenderCh`, `drainScreenEvents`) and wires them into the main loop so a delta storm collapses into one render and queued events always run before the next select.

**Architecture:** Single-file change in `internal/runtime/client/app.go`. Two new package-level helpers — `coalesceRenderCh(ch <-chan struct{})` non-blocking-drains all queued ticks, and `drainScreenEvents(events <-chan tcell.Event, handle func(tcell.Event) bool) (int, bool)` pulls every queued event and dispatches via the supplied callback (the callback is a closure capturing state/screen/sessionID/writer at the call site, so the helper itself is pure and unit-testable). The loop body gets one new priority drain at the top and a `coalesceRenderCh(renderCh)` call inside the existing `case <-renderCh` arm.

**Tech Stack:** Go 1.24.3, `github.com/gdamore/tcell/v2` for `tcell.Event`. No new imports.

**Spec:** `docs/superpowers/specs/2026-05-03-client-event-loop-fairness-design.md`

---

## File Structure

| Path | Status | Responsibility |
|------|--------|----------------|
| `internal/runtime/client/app.go` | Modify | Add `coalesceRenderCh` + `drainScreenEvents` helpers; tweak main loop body to call them |
| `internal/runtime/client/event_loop_fairness_test.go` | Create | Five unit tests covering both helpers' contracts |

No other files touched. No protocol changes. No new exported APIs.

---

## Task 1: Failing test for `coalesceRenderCh`

Establishes the TDD baseline — test references the helper that doesn't exist yet, fails to compile.

**Files:**
- Create: `internal/runtime/client/event_loop_fairness_test.go`

- [ ] **Step 1: Write the failing test**

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/event_loop_fairness_test.go
// Summary: Tests for the priority-drain + coalesce helpers that
// keep the client's main event loop fair under heavy server
// traffic.

package clientruntime

import (
	"testing"
)

// TestCoalesceRenderCh_DrainsAllQueuedTicks verifies the helper
// non-blocking-drains every queued tick on the channel. Without
// this, a burst of N back-to-back signals would each trigger a
// separate render call.
func TestCoalesceRenderCh_DrainsAllQueuedTicks(t *testing.T) {
	ch := make(chan struct{}, 8)
	for i := 0; i < 5; i++ {
		ch <- struct{}{}
	}
	if got := len(ch); got != 5 {
		t.Fatalf("setup: ch len = %d, want 5", got)
	}

	coalesceRenderCh(ch)

	if got := len(ch); got != 0 {
		t.Errorf("after coalesce: ch len = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/client/ -run TestCoalesceRenderCh_DrainsAllQueuedTicks -count=1 -v`

Expected: FAIL with `undefined: coalesceRenderCh`. Test references the helper that doesn't exist yet — the next task adds it.

- [ ] **Step 3: Commit the failing test**

```bash
git add internal/runtime/client/event_loop_fairness_test.go
git commit -m "Test: failing coalesceRenderCh assertion"
```

The failing-test commit cleanly separates from the implementation that flips it to passing.

---

## Task 2: Implement `coalesceRenderCh` + add second test

Adds the helper and the empty-channel non-blocking test (which passes immediately against the new implementation).

**Files:**
- Modify: `internal/runtime/client/app.go` (add helper before `Run` or at end of file — pick the cleaner spot near other top-level helpers)
- Modify: `internal/runtime/client/event_loop_fairness_test.go` (append second test)

- [ ] **Step 1: Add the helper to `app.go`**

Add at the end of the file (after the `Run` function, before any existing helpers like `loadKeybindings`):

```go
// coalesceRenderCh non-blocks-drains every queued tick on ch. The
// client's main event loop calls this after consuming a single
// renderCh tick from its select so a burst of N signals (heavy
// server traffic flooding readLoop's signalRender path) collapses
// into one render call. With render frequency tracking incoming
// delta rate, the loop spent most of its time re-rendering against
// transient intermediate state; coalescing makes render frequency
// bounded by render duration itself.
//
// The function logs (at debuglog level) when it coalesces 2+ ticks
// in one call. Pre-coalesce, the renderCh channel filling its 64
// buffer was the de-facto canary that render() was lagging behind
// readLoop. Coalescing collapses bursts before the buffer can fill,
// so this log line preserves the same diagnostic signal — a tail
// of the debug log shows post-hoc whether bursts were arriving.
func coalesceRenderCh(ch <-chan struct{}) {
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			if count > 1 {
				debuglog.Printf("event loop: coalesced %d renderCh ticks", count)
			}
			return
		}
	}
}
```

The `debuglog` import already exists in `app.go` (used by other code in the file); no new import needed.

- [ ] **Step 2: Verify the failing test from Task 1 now passes**

Run: `go test ./internal/runtime/client/ -run TestCoalesceRenderCh_DrainsAllQueuedTicks -count=1 -v`

Expected: PASS.

- [ ] **Step 3: Append the second test to `event_loop_fairness_test.go`**

This test uses `time.After`, so first add `"time"` to the import block at the top of the file:

```go
import (
	"testing"
	"time"
)
```

Then append at the end of the file (after the closing `}` of `TestCoalesceRenderCh_DrainsAllQueuedTicks`):

```go
// TestCoalesceRenderCh_NonBlockingOnEmpty verifies the helper
// returns promptly when the channel is empty. A blocking call
// here would freeze the main loop's renderCh case.
func TestCoalesceRenderCh_NonBlockingOnEmpty(t *testing.T) {
	ch := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		coalesceRenderCh(ch)
		close(done)
	}()
	select {
	case <-done:
		// returned promptly — good
	case <-time.After(100 * time.Millisecond):
		t.Fatal("coalesceRenderCh blocked on empty channel")
	}
}
```

- [ ] **Step 4: Run both coalesce tests**

Run: `go test ./internal/runtime/client/ -run TestCoalesceRenderCh -count=1 -v`

Expected: 2/2 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/client/app.go internal/runtime/client/event_loop_fairness_test.go
git commit -m "Add coalesceRenderCh helper for client main-loop fairness"
```

---

## Task 3: Failing test for `drainScreenEvents`

References the helper that doesn't exist yet — fails to compile.

**Files:**
- Modify: `internal/runtime/client/event_loop_fairness_test.go` (append)

- [ ] **Step 1: Append the failing test**

Append at the end of the file:

```go
// drainScreenEvents helper expectations
//
// The helper has the signature:
//   func drainScreenEvents(events <-chan tcell.Event, handle func(tcell.Event) bool) (drained int, ok bool)
// Pulled events are dispatched via handle. ok=false means either the
// channel is closed OR handle returned false (signalling exit).

// TestDrainScreenEvents_ReturnsCountAndDispatches verifies the
// helper pulls every queued event and dispatches each via the
// supplied callback in order, returning the total count and
// ok=true for the empty-after-drain case.
//
// We tag events via NewEventInterrupt(int) and compare the int
// payload (via .Data()) rather than pointer-equal the events
// themselves — tcell.Event is an interface and pointer equality
// would silently break if tcell ever pooled or copied events.
func TestDrainScreenEvents_ReturnsCountAndDispatches(t *testing.T) {
	ch := make(chan tcell.Event, 4)
	want := []int{10, 20, 30}
	for _, id := range want {
		ch <- tcell.NewEventInterrupt(id)
	}

	var dispatched []int
	handle := func(ev tcell.Event) bool {
		dispatched = append(dispatched, ev.(*tcell.EventInterrupt).Data().(int))
		return true
	}

	drained, ok := drainScreenEvents(ch, handle)

	if !ok {
		t.Errorf("ok = false, want true on clean drain")
	}
	if drained != len(want) {
		t.Errorf("drained = %d, want %d", drained, len(want))
	}
	if len(dispatched) != len(want) {
		t.Fatalf("dispatched len = %d, want %d", len(dispatched), len(want))
	}
	for i, id := range want {
		if dispatched[i] != id {
			t.Errorf("dispatch[%d]: got %d, want %d", i, dispatched[i], id)
		}
	}
}
```

The new test imports `tcell.Event` directly — add `"github.com/gdamore/tcell/v2"` to the existing import block at the top of `event_loop_fairness_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/client/ -run TestDrainScreenEvents_ReturnsCountAndDispatches -count=1 -v`

Expected: FAIL with `undefined: drainScreenEvents`. Test references the helper that doesn't exist yet.

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/client/event_loop_fairness_test.go
git commit -m "Test: failing drainScreenEvents assertion"
```

---

## Task 4: Implement `drainScreenEvents` + add remaining tests

Adds the helper and the two remaining tests (empty channel, closed channel) which pass immediately against the new implementation.

**Files:**
- Modify: `internal/runtime/client/app.go` (append helper after `coalesceRenderCh`)
- Modify: `internal/runtime/client/event_loop_fairness_test.go` (append two tests)

- [ ] **Step 1: Add the helper to `app.go`**

Add immediately after `coalesceRenderCh`:

```go
// drainScreenEvents pulls every queued tcell event from events and
// dispatches each via handle. Returns the count drained and ok=false
// if either the channel closed (run-loop exit signal) or the handle
// returned false (also a run-loop exit signal). Non-blocking: empty
// channel returns (0, true) immediately and the caller's select can
// block on the next signal.
//
// The handle callback is the call site's closure over
// handleScreenEvent + state/screen/sessionID/writer; passing it as
// a parameter keeps this helper pure and unit-testable without a
// full clientState fixture.
func drainScreenEvents(events <-chan tcell.Event, handle func(tcell.Event) bool) (drained int, ok bool) {
	for {
		select {
		case ev, chOK := <-events:
			if !chOK {
				return drained, false
			}
			if !handle(ev) {
				return drained, false
			}
			drained++
		default:
			return drained, true
		}
	}
}
```

- [ ] **Step 2: Verify the failing test from Task 3 now passes**

Run: `go test ./internal/runtime/client/ -run TestDrainScreenEvents_ReturnsCountAndDispatches -count=1 -v`

Expected: PASS.

- [ ] **Step 3: Append four more tests to `event_loop_fairness_test.go`**

Append at the end:

```go
// TestDrainScreenEvents_EmptyChannelReturnsZero verifies the helper
// is a fast no-op when nothing is queued. A blocking implementation
// would freeze the main loop's per-iteration priority drain.
func TestDrainScreenEvents_EmptyChannelReturnsZero(t *testing.T) {
	ch := make(chan tcell.Event, 4)
	handle := func(ev tcell.Event) bool {
		t.Errorf("handle should not be called on empty channel; got %v", ev)
		return true
	}

	done := make(chan struct {
		drained int
		ok      bool
	}, 1)
	go func() {
		drained, ok := drainScreenEvents(ch, handle)
		done <- struct {
			drained int
			ok      bool
		}{drained, ok}
	}()

	select {
	case res := <-done:
		if res.drained != 0 {
			t.Errorf("drained = %d, want 0", res.drained)
		}
		if !res.ok {
			t.Errorf("ok = false, want true on empty drain")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("drainScreenEvents blocked on empty channel")
	}
}

// TestDrainScreenEvents_ChannelClosedReturnsNotOK verifies the
// helper returns ok=false when the channel is closed, so the main
// loop's caller can return nil and exit cleanly.
func TestDrainScreenEvents_ChannelClosedReturnsNotOK(t *testing.T) {
	ch := make(chan tcell.Event, 1)
	close(ch)
	handle := func(ev tcell.Event) bool {
		t.Errorf("handle should not be called when channel closed; got %v", ev)
		return true
	}

	drained, ok := drainScreenEvents(ch, handle)

	if ok {
		t.Error("ok = true, want false for closed channel")
	}
	if drained != 0 {
		t.Errorf("drained = %d, want 0 (channel closed before any event)", drained)
	}
}

// TestDrainScreenEvents_DrainsThenChannelCloses covers the realistic
// shutdown shape: events are queued, then the producer closes the
// channel. The helper must drain the buffered events first and only
// then observe the close. Without this case, a future regression
// where the close-detection logic short-circuits before draining
// the buffer would silently lose user input.
func TestDrainScreenEvents_DrainsThenChannelCloses(t *testing.T) {
	ch := make(chan tcell.Event, 4)
	want := []int{1, 2}
	for _, id := range want {
		ch <- tcell.NewEventInterrupt(id)
	}
	close(ch)

	var dispatched []int
	handle := func(ev tcell.Event) bool {
		dispatched = append(dispatched, ev.(*tcell.EventInterrupt).Data().(int))
		return true
	}

	drained, ok := drainScreenEvents(ch, handle)

	if ok {
		t.Error("ok = true, want false (channel closed after drain)")
	}
	if drained != len(want) {
		t.Errorf("drained = %d, want %d", drained, len(want))
	}
	if len(dispatched) != len(want) {
		t.Fatalf("dispatched %d events, want %d", len(dispatched), len(want))
	}
	for i, id := range want {
		if dispatched[i] != id {
			t.Errorf("dispatch[%d]: got %d, want %d", i, dispatched[i], id)
		}
	}
}

// TestDrainScreenEvents_HandleReturnsFalseStopsDrain covers the
// production exit signal: handleScreenEvent returns false to mean
// "the run loop should exit" (e.g. ctrl-Q, session disconnect).
// The helper must propagate that immediately — drained must reflect
// only events whose handler ran successfully (excluding the one
// that returned false), and any remaining queued events stay in
// the channel. Without this case, an off-by-one regression in the
// helper (count++ before vs. after the handle check, or draining
// one extra event after the false return) would silently leak
// events past the exit signal.
func TestDrainScreenEvents_HandleReturnsFalseStopsDrain(t *testing.T) {
	ch := make(chan tcell.Event, 4)
	for _, id := range []int{1, 2, 3} {
		ch <- tcell.NewEventInterrupt(id)
	}

	var dispatched []int
	handle := func(ev tcell.Event) bool {
		id := ev.(*tcell.EventInterrupt).Data().(int)
		dispatched = append(dispatched, id)
		return id != 2 // signal exit on the second event
	}

	drained, ok := drainScreenEvents(ch, handle)

	if ok {
		t.Error("ok = true, want false when handle returned false")
	}
	if drained != 1 {
		t.Errorf("drained = %d, want 1 (only the first successful event counts)", drained)
	}
	if len(dispatched) != 2 {
		t.Errorf("dispatched %d events, want 2 (the handler ran for events 1 and 2)", len(dispatched))
	}
	// The third event must remain in the channel — exit-on-false
	// means the helper stops AT the false event, not after it.
	if got := len(ch); got != 1 {
		t.Errorf("ch len after exit = %d, want 1 (third event should not have been drained)", got)
	}
}
```

- [ ] **Step 4: Run all seven tests**

Run: `go test ./internal/runtime/client/ -run "TestCoalesceRenderCh|TestDrainScreenEvents" -count=1 -v`

Expected: 7/7 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/client/app.go internal/runtime/client/event_loop_fairness_test.go
git commit -m "Add drainScreenEvents helper for client main-loop fairness"
```

---

## Task 5: Wire helpers into the main loop

Tweaks the loop body in `Run` to call the new helpers. This is the user-visible behaviour change.

**Files:**
- Modify: `internal/runtime/client/app.go` (the `for { ... }` body inside `Run`, currently around lines 403-492)

- [ ] **Step 1: Apply the loop-body changes**

Inside `Run`, locate the main `for { ... }` body that begins with `// Start or stop the unified ticker.` (around line 404). Make two changes:

**Change A — add the priority drain before the main `select`.** Insert this block after the animation ticker setup (after the line `tickCh = ticker.C` block, before the line `select {`):

```go
		// PRIORITY DRAIN: process every queued tcell event before
		// blocking on the main select. Without this, Go's uniform-
		// random select pick lets renderCh win most rounds when
		// it's saturated by heavy server traffic — input lags
		// behind every queued render. drainScreenEvents is
		// non-blocking when the events channel is empty, so the
		// hot path is one default-case channel receive.
		drained, ok := drainScreenEvents(events, func(ev tcell.Event) bool {
			return handleScreenEvent(ev, state, screen, sessionID, writer)
		})
		if !ok {
			return nil
		}
		if drained > 0 {
			// Events advanced state; loop back so the next
			// iteration re-evaluates the animation ticker
			// condition before committing to a select. Removing
			// this `continue` would let the loop block on a
			// renderCh / tickCh tick that an event might have
			// invalidated (e.g. a key toggling animations off).
			continue
		}
```

**Change B — coalesce queued renderCh ticks at the top of the `case <-renderCh:` arm.** Replace the existing `case <-renderCh:` block (currently at app.go ~line 443):

```go
		case <-renderCh:
			// Data-driven render: delta/snapshot arrived. Render immediately, no time advance.
			state.frameDT = 0
			if state.effects != nil {
				state.effects.Update(0)
			}
			render(state, screen)
			// Splash handoff: stop only once a real TreeSnapshot has
			// ... (large comment block)
			if !firstContentRendered && state.bootHandoffReady() {
				firstContentRendered = true
				emitStatus(StageReady, "")
			}
```

…with this version (one new line + an inline comment; the rest is unchanged):

```go
		case <-renderCh:
			// Data-driven render: delta/snapshot arrived. Render immediately, no time advance.
			// Coalesce any further renderCh ticks that readLoop has
			// queued during the previous select round so a burst of
			// N BufferDelta signals collapses into one render of
			// the final state. Without this, render frequency
			// tracks incoming delta rate.
			coalesceRenderCh(renderCh)
			state.frameDT = 0
			if state.effects != nil {
				state.effects.Update(0)
			}
			render(state, screen)
			// Splash handoff: stop only once a real TreeSnapshot has
			// been applied AND a content-bearing delta has landed.
			//
			// On rehydrated cold starts the server defers
			// TreeSnapshot to handleClientReady (so it lands at the
			// client's real dimensions), and that handler runs the
			// slow SetViewportSize → Snapshot chain. Before that,
			// the publisher emits decor-only BufferDeltas that fire
			// renderCh while the panes are still hydrating.
			//
			// Without the treeSnapshotApplied gate the splash hands
			// off to a workspace that has no real content yet. The
			// race: on the first renderCh tick, ensureBuffers
			// returns resized=true (so fullRender runs and
			// fullRenderHappened flips) and a decor-only BufferDelta
			// already in flight can flip firstContentDelta during
			// render(). Both flags would pass, the splash would
			// stop, and the user would see only borders while
			// handleClientReady's slow SetViewportSize → Snapshot
			// chain finished.
			if !firstContentRendered && state.bootHandoffReady() {
				firstContentRendered = true
				emitStatus(StageReady, "")
			}
```

Leave the existing `case ev, ok := <-events:`, `case <-doneCh:`, and the post-select clipboard sync block untouched. The `events` case in the main select is the fallback for events that arrive *after* the priority drain ran but before the loop blocks — without it, a fresh event arriving mid-block would have to wait for an unrelated wake-up.

- [ ] **Step 2: Build the package**

Run: `go build ./internal/runtime/client/...`

Expected: clean build.

- [ ] **Step 3: Run the existing client-runtime tests for regression**

Run: `go test ./internal/runtime/client/... -count=1 -race -v`

Expected: PASS for everything that already passed on `main`. The loop-wiring change should not affect any existing test — the helpers are pure and the loop semantics are equivalent on idle / single-event paths.

If any test fails, especially anything in `viewport_tracker_*_test.go`, `pane_cache_dispatch_test.go`, or `boot_handoff_test.go` (the suites that exercise channels and state mutations the loop interacts with), STOP and report — do not paper over by adjusting expectations. Compare the failing test against `main` to confirm the failure is introduced by the loop changes.

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/client/app.go
git commit -m "$(cat <<'EOF'
Wire priority drain + render coalesce into client event loop

The client's main loop in Run() previously used a flat 4-way select
with Go's uniform-random pick. Heavy server traffic flooded
renderCh, so events lost most select rounds and each render ran
against transient intermediate state. Drain queued events before
blocking on the main select (priority); coalesce remaining
renderCh ticks before each data-driven render. Combined: render
frequency is bounded by render duration itself, not delta rate;
events always win when ready.
EOF
)"
```

---

## Task 6: Race detector + final verification

Confirms no goroutine races and the broader client suite still passes.

- [ ] **Step 1: Race detector on the new tests**

Run: `go test ./internal/runtime/client/ -run "TestCoalesceRenderCh|TestDrainScreenEvents" -count=10 -race`

Expected: PASS, no race warnings.

- [ ] **Step 2: Full client-runtime test suite under race**

Run: `go test ./internal/runtime/client/... -count=1 -race`

Expected: PASS.

- [ ] **Step 3: Build the full repo**

Run: `go build ./...`

Expected: clean build, no warnings.

- [ ] **Step 4: gofmt check**

Run: `gofmt -l internal/runtime/client/app.go internal/runtime/client/event_loop_fairness_test.go`

Expected: no output (no drift).

If `gofmt` reports drift, run `gofmt -w` on the listed files and amend the previous commit:

```bash
gofmt -w internal/runtime/client/app.go internal/runtime/client/event_loop_fairness_test.go
git add -u internal/runtime/client/
git commit --amend --no-edit
```

- [ ] **Step 5: Manual cold-start test**

This is the user-visible regression test the unit tests can't capture. Steps:

```bash
make build
./bin/texelation --stop
sleep 2
./bin/texelation
```

In the running texelation, open two panes (`Ctrl-A` then split). In one pane, run `ls -lR /usr` (or any heavy-output command). While it's running, type into the other pane / select panes / toggle control mode. Input should remain responsive throughout — keystrokes should land within one render frame (~16–50 ms), not delayed by hundreds of milliseconds as before.

Document the observed behaviour in the PR description.

- [ ] **Step 6: No commit needed if everything is clean**

If steps 1–4 pass with no edits, this task ships nothing new — it's a verification gate. Move on to opening the PR.

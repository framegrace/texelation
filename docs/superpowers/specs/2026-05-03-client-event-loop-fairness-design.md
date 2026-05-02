# Client Event-Loop Fairness — Design

**Date:** 2026-05-03
**Status:** Approved for plan
**Scope:** Client-side `internal/runtime/client/app.go` main event loop

## Problem

When one pane has heavy output (`ls -lR`, build logs, `cat large_file`) the
**entire client** becomes laggy — selecting panes, control-mode toggles, key
input, mouse — every interactive action feels like it's stuck behind something.

The server-side fix landed earlier (chunked `sendPending`) bounded how many
diffs the server flushes per loop iteration; that's necessary but not
sufficient. The visible lag is downstream in the **client's main event loop**.

## Root cause

The client's main loop in `internal/runtime/client/app.go`:

```go
for {
    // ... animation ticker setup ...
    select {
    case <-tickCh:    /* render */
    case <-renderCh:  /* render */
    case ev := <-events: /* handle */
    case <-doneCh:    return
    }
}
```

`readLoop` runs in a separate goroutine and signals `renderCh` on every
applied `BufferDelta` (or other content message). Under heavy server traffic
the `renderCh` channel (cap 64) stays full of fresh ticks. Two problems
compound:

1. **Render frequency tracks delta rate.** Each loop iteration consumes one
   `renderCh` tick and runs a full `render()` (cache diff + `tcell.Show()`).
   A burst of 50 `BufferDelta`s = 50 renders, even though one render covering
   the final state would suffice.

2. **Random `select` fairness.** Go's `select` picks uniformly at random when
   multiple cases are ready. With renderCh constantly ready (refilling between
   iterations) and events occasionally ready, the events case loses most of
   the coin flips — an interactive keystroke is dispatched only after several
   renders that didn't need to happen.

The two failure modes together explain "the WHOLE client becomes laggy" — it
isn't a network bottleneck, it's the main loop spending nearly all its time
re-rendering against transient intermediate states while the user's input
sits in the events buffer.

## Out of scope

- **60 Hz / wall-clock rate-limit on renders** (Option C from the brainstorm).
  Worth adding only if measurement shows render itself is the bottleneck, not
  the loop's pick discipline. The renderCh case is the only place data-driven
  renders fire; adding a `lastRenderAt time.Time` guard there is local and
  additive whenever we want it. Ship without it for now.
- **Render in a separate goroutine.** Eliminates the in-flight-render-blocks-
  events worst case, but doubles the rendering complexity (locking around
  shared `state.cache`, double-buffer ownership, etc.). Out of scope until
  measurement says it's needed.
- **Server-side chunking** (already shipped on a sibling branch). Different
  layer, complementary fix. This spec assumes nothing about the server's
  flush behaviour — the client fix stands on its own.
- **Animation tick coalescing.** The `tickCh` cadence is intentional — effects
  expect a steady frame rate. Coalescing applies only to the data-driven
  `renderCh` case.

## Design

### Component

Two new helpers in `internal/runtime/client/app.go`, plus a small structural
change to the main loop body. No new files, no new exported APIs.

### Helpers

```go
// drainScreenEvents pulls every queued tcell event from `events` and
// dispatches it via handleScreenEvent. Returns the number drained
// and ok=false if the channel closed (signal that the run loop
// should exit). Non-blocking: empty channel returns (0, true)
// immediately and the caller falls through to the main select.
func drainScreenEvents(
    events <-chan tcell.Event,
    state *clientState,
    screen tcell.Screen,
    sessionID [16]byte,
    writer *messageWriter,
) (drained int, ok bool) {
    for {
        select {
        case ev, chOK := <-events:
            if !chOK {
                return drained, false
            }
            if !handleScreenEvent(ev, state, screen, sessionID, writer) {
                return drained, false
            }
            drained++
        default:
            return drained, true
        }
    }
}

// coalesceRenderCh non-blocks-drains every queued tick on renderCh.
// Used after a single tick is consumed by the main select so a
// burst of N back-to-back signals (heavy server traffic) collapses
// into one render call.
func coalesceRenderCh(ch <-chan struct{}) {
    for {
        select {
        case <-ch:
        default:
            return
        }
    }
}
```

### Loop

```go
for {
    // Animation ticker setup (unchanged) ...

    // PRIORITY DRAIN: process every queued input event before
    // waiting on anything else. User-driven event rates are well
    // below the events channel cap (32), so this loop always
    // exits in a handful of iterations.
    drained, ok := drainScreenEvents(events, state, screen, sessionID, writer)
    if !ok {
        return nil
    }
    if drained > 0 {
        // Events advanced state; loop back so the next iteration
        // re-evaluates the animation ticker condition before
        // committing to a select.
        continue
    }

    select {
    case <-tickCh:
        // Animation tick (unchanged).
        ...
        render(state, screen)
        state.frameDT = 0

    case <-renderCh:
        // Data-driven render. Coalesce remaining ticks so a
        // delta storm collapses into a single render of the
        // final state.
        coalesceRenderCh(renderCh)
        state.frameDT = 0
        if state.effects != nil {
            state.effects.Update(0)
        }
        render(state, screen)
        // Splash handoff check (unchanged).
        if !firstContentRendered && state.bootHandoffReady() {
            firstContentRendered = true
            emitStatus(StageReady, "")
        }

    case ev, ok := <-events:
        if !ok {
            return nil
        }
        if !handleScreenEvent(ev, state, screen, sessionID, writer) {
            return nil
        }

    case <-doneCh:
        fmt.Println("Connection closed")
        return nil
    }
}
```

The events case in the main select stays — it handles the case where the
loop blocked in `select` (no events drained at the top) and an event arrives
mid-block. Without that fallback, an event arriving between the priority
drain and the select would have to wait for a renderCh / tickCh / doneCh tick
to wake the select.

### Why this is sufficient

- **Coalescing alone wouldn't fix events.** With `readLoop` faster than
  `render()`, `renderCh` refills continuously between iterations. Random
  `select` keeps picking renderCh as long as it's ready alongside events.

- **Event-priority drain alone wouldn't slow renders.** It guarantees events
  are handled when ready, but per-iteration we'd still call `render()` once
  per renderCh tick — heavy `BufferDelta` storms still produce hundreds of
  renders per second, each doing the diff + `tcell.Show()`.

- **Combined**: render frequency is bounded by render duration itself (each
  render covers all pending state; the next renderCh tick has to arrive after
  render returns). Events are bounded by `handleScreenEvent` duration
  (microseconds). Worst-case event latency = one in-flight render's duration
  (~5–50 ms depending on workspace size), independent of incoming delta rate.

### Forward-compat for Option C (60 Hz rate-limit)

The renderCh case is the only place data-driven renders fire. Adding a
`lastRenderAt time.Time` and a guard

```go
if since := time.Since(lastRenderAt); since < frameInterval {
    // Re-arm renderCh and let timer fire next.
    return
}
```

is local and additive. We don't need to change anything in this spec to
keep that door open.

## Testing

A new test file `internal/runtime/client/event_loop_fairness_test.go`. Helpers
are pure functions, so unit tests can exercise them without a full
`clientState` + `tcell.Screen` setup.

1. **`TestCoalesceRenderCh_DrainsAllQueuedTicks`** — push 5 ticks into a
   buffered chan, call `coalesceRenderCh`, assert `len(ch) == 0` after.

2. **`TestCoalesceRenderCh_NonBlockingOnEmpty`** — call on empty chan inside
   a goroutine with a 100 ms `time.After` deadline; assert it returns within
   the deadline (proves the loop's `default` case is hit).

3. **`TestDrainScreenEvents_ReturnsCountAndDispatches`** — build a mock
   event channel and a hookable handler counter (via a test-only function
   pointer or a wrapper), push 3 events, call `drainScreenEvents`, assert all
   3 dispatched and returned count == 3, ok == true.

4. **`TestDrainScreenEvents_EmptyChannelReturnsZero`** — empty chan returns
   (0, true) without blocking.

5. **`TestDrainScreenEvents_ChannelClosedReturnsNotOK`** — close the chan,
   call `drainScreenEvents`, assert ok == false so the main loop exits.

The integration behaviour ("events beat renderCh under heavy traffic") is
hard to test without flakiness in a synchronous test — we rely on the
manual `ls -lR` cold-start test the user runs, plus the helper-level unit
tests pinning the contracts.

## Risks

- **Render starvation under pathological event rate.** A script
  `xdotool`-spamming thousands of keys per second could keep the priority
  drain busy and starve renders. Real human input rates are ≤100/sec and
  each event dispatches in microseconds, so the drain returns in
  microseconds. Not a real risk.

- **Coalescing hides a backpressure signal.** Today, `signalRender` drops
  signals when `renderCh` is full (non-blocking send). With coalescing the
  buffer drains faster, so dropped signals decrease. That's a *good* side
  effect — no actual risk.

- **The events drain at the top of the loop runs even when no input is
  pending.** Cost is one non-blocking channel receive (default case)
  per iteration — single-digit nanoseconds. Negligible.

- **`handleScreenEvent` returning false is the exit signal.** The drain
  helper returns ok=false in that case, and the main loop returns nil.
  Same as today's inline behaviour; just routed through the helper.

- **Loop regression: someone removes `continue` after `drained > 0`.**
  Without it, after draining events the loop falls into `select` and may
  block on a renderCh tick that the drained events should have invalidated
  (e.g. an animation toggled off by a key). The unit tests don't catch
  this; a comment in the loop body marks it explicitly.

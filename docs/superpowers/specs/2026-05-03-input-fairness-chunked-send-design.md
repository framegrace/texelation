# Input Fairness via Chunked sendPending — Design

**Date:** 2026-05-03
**Status:** Approved for plan
**Scope:** Server-side `internal/runtime/server`

## Problem

When one pane generates heavy output (`ls -lR`, build logs, `cat large_file`) the
texelation client experiences pronounced input lag on *other* panes — keystrokes
visibly delay by hundreds of milliseconds before reaching the target pane. This
contradicts user expectation since the wire architecture (issue #199 Plan A) already
clips deltas to each client's viewport: the server is "just printing text" the client
already needs.

## Root cause

The server's per-connection `serve()` loop is structured:

```go
for {
    sendPending()        // drains the entire queued diff backlog
    select {
    case <-c.pending:    // wake-up signal
    case err := <-c.readErr: ...
    case msg := <-c.incoming: handleMessage(msg)
    }
}
```

`sendPending()` writes **every** queued diff before returning. A heavy publish pass
queues hundreds of `BufferDelta` messages in `Session.diffs`; the entire backlog goes
to the wire before the next `select` iteration, so an incoming `MsgKeyEvent` sitting
in `c.incoming` (32-deep buffer) cannot be dispatched until the flush completes.

The contention is purely structural — `sendPending` and `handleMessage` share no
lock — and the wire itself isn't saturated. The fix is at the connection layer.

## Out of scope

- **Priority routing of input messages over output diffs** in the queue (called
  "Option B" in the brainstorm). Deferred — the chunk-and-yield in this design
  gives Go's `select` a fair chance to pick input on each loop iteration. Add
  later if observed input latency under chunked flush still feels bad.
- **Per-pane parallelism of `bufferToDelta` in the publisher** (called "Option C").
  The publisher serialises all panes' encode work under one lock; that's a
  separate fairness concern at a different layer. The brainstorm trace concluded
  the connection-flush bottleneck is the larger user-visible offender, so we ship
  the chunk-and-yield first and revisit the publisher only if symptoms persist.
- **Client-side render loop fairness.** The Explore agent's trace confirmed the
  client's main `select` already handles tickCh / renderCh / events / doneCh
  cleanly; lag did not originate there.
- **Wire-protocol changes.** No new message types, no version bump.

## Design

### Component

A single new constant and a six-line change to one function in
`internal/runtime/server/connection_sync.go`.

### Behaviour

`sendPending()` becomes chunked. After it has written `sendChunkSize = 32` diffs
in a single invocation, it stops, calls `c.nudge()` to schedule itself for the
next iteration of the connection's `select` loop, and returns. On that next
iteration the select picks one of:

- `c.pending` (re-arm from the self-nudge or from a publisher-side new diff) —
  go straight back into `sendPending` for the next chunk.
- `c.incoming` (a queued `MsgKeyEvent`, `MsgMouseEvent`, etc.) —
  `handleMessage` dispatches the input to the desktop, then the loop comes back
  through `sendPending` and continues draining.

Go's `select` picks randomly when multiple cases are ready. With a heavy
backlog and a queued keystroke, that means the keystroke gets ≈50% odds on
each iteration and is dispatched within a small handful of chunks (≤2–3 in
expectation, bounded above by however many select iterations the user is
willing to wait — but each iteration's wire work is bounded by
`sendChunkSize × per-write cost`, so the worst case is a few milliseconds, not
seconds).

### Why 32

- Small enough that even a slow socket clears one chunk in well under a frame
  (32 small viewport-clipped deltas are typically < 16 KB total).
- Large enough to amortise the goroutine-scheduling and select roundtrip when
  the backlog is large (the select round-trip is sub-microsecond; doing it once
  per diff would add measurable overhead on a 10000-diff burst, doing it every
  32 diffs is noise).
- Picked as a constant rather than an env var or supervisor option (YAGNI —
  no need for per-deployment tuning yet, easy to expose later).

### Code

```go
// In internal/runtime/server/connection_sync.go.

// sendChunkSize bounds how many diffs sendPending writes in a single
// invocation before yielding back to the connection's select loop. A
// heavy publish run can queue thousands of diffs in c.session; without
// this bound, sendPending would drain the entire backlog before the
// next handleMessage() call, blocking input dispatch (a keystroke for
// pane B sits in c.incoming until pane A's output finishes flushing).
//
// 32 is small enough that even a slow socket clears a chunk in well
// under a frame, large enough to amortise the loop / select roundtrip
// when the backlog is enormous.
const sendChunkSize = 32

func (c *connection) sendPending() error {
    if c.awaitResume {
        return nil
    }
    pending := c.session.Pending(c.lastAcked)
    sent := 0
    for _, diff := range pending {
        if diff.Sequence <= c.lastSent {
            continue
        }
        if sent >= sendChunkSize {
            // Backlog remains. Nudge ourselves so the next select
            // iteration re-enters sendPending; in the meantime the
            // select can pick up an incoming input message instead.
            c.nudge()
            return nil
        }
        header := diff.Message
        header.Sequence = diff.Sequence
        header.SessionID = c.session.ID()
        if err := c.writeMessage(header, diff.Payload); err != nil {
            return err
        }
        c.lastSent = diff.Sequence
        sent++
    }
    return nil
}
```

The `serve()` loop body is unchanged — the only behavioural difference is that
`c.pending` may be ticked by `sendPending` itself rather than only by the
publisher.

### State invariants

- `c.lastSent` is mutated only by `serve()` (single goroutine). The chunk-resume
  guard `if diff.Sequence <= c.lastSent` is therefore race-free.
- `c.nudge()` is non-blocking (buffered `chan struct{}` with capacity 1, send
  inside `select { default }`). A self-nudge after a publisher nudge is dropped
  by the buffer; the next select iteration drains the channel and the loop
  re-enters `sendPending`. No double-wakeup, no missed wakeup.
- `c.session.Pending(c.lastAcked)` returns the same slice modulo ack progress;
  partial drain + re-call resumes from the next sequence after `c.lastSent`. No
  state bookkeeping is added to `connection`.

### Error handling

Mid-chunk write failure returns the error from `sendPending`, the serve loop
bails the connection — same as today. Encoding the chunk does not introduce
new failure modes.

## Testing

New file: `internal/runtime/server/connection_sync_test.go`.

1. **`TestSendPending_ChunksAtBoundary`** — enqueue 100 diffs in a `Session`,
   wire a connection to an in-memory `net.Pipe`, call `sendPending()` once,
   assert exactly 32 messages landed on the wire and `c.pending` is non-empty
   (re-nudged).

2. **`TestSendPending_DrainsAcrossCalls`** — same setup, call `sendPending()`
   four times, assert all 100 diffs arrive in monotonic sequence, and the last
   call leaves `c.pending` empty.

3. **`TestSendPending_EmptyQueueNoNudge`** — empty queue, `sendPending()` is a
   no-op and does NOT consume / produce anything on `c.pending`.

4. **`TestServeLoop_InputInterleaves`** — integration-shape test on a `serve()`
   goroutine: enqueue 200 diffs into `Session`, then push one
   `MsgKeyEvent` onto `c.incoming`, observe via a hooked `handleMessage` that
   the event was dispatched before all 200 diffs flushed (i.e. less than 200
   diffs had been written at the moment the key was processed). Bounds the
   chunk-and-yield correctness empirically.

Race detector: `go test -race -count=10 ./internal/runtime/server/ -run "TestSendPending|TestServeLoop_InputInterleaves"` clean.

## Risks

- **Latency tax on a single long burst.** A 1000-diff backlog now takes ~31
  select roundtrips instead of one monolithic call. Each roundtrip is
  sub-microsecond goroutine scheduling — total overhead well under 1 ms versus
  tens of ms for the diff writes themselves. Negligible.

- **Existing connection tests.** `TestConnection_NonRehydratedResume_*` and
  related tests have small queues that fit in one chunk; chunking is invisible
  to them. Verified by running the existing suite.

- **Future regression: someone removes the self-nudge.** If a refactor drops
  `c.nudge()` from the chunk-yield path, the backlog will stop draining until
  the publisher emits a fresh nudge — input becomes responsive but throughput
  collapses. The unit test `TestSendPending_ChunksAtBoundary` asserts the
  re-nudge explicitly so this regression surfaces immediately.

## Forward-compatibility

The design intentionally keeps the door open for the deferred options:

- **Option B** (priority queue for input messages) drops in at the `serve()`
  `select`: replace the random pick with `if c.incoming has work, prefer it;
  else c.pending`. No change to `sendPending`.
- **Option C** (parallel `bufferToDelta`) is at a different layer
  (`desktop_publisher.publishSnapshotsLocked`) and orthogonal to this design;
  it would feed the same `Session.diffs` queue, and the chunked `sendPending`
  would drain it the same way.

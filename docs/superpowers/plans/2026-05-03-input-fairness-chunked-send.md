# Input Fairness via Chunked sendPending — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop the server's connection-flush layer from blocking input dispatch when one pane has heavy output. `sendPending()` becomes chunked at 32 diffs per call and self-nudges if more remain, so the connection's `select` can interleave queued input messages between flush batches.

**Architecture:** One file modified, one file added. `internal/runtime/server/connection_sync.go::sendPending()` gains a `sent` counter and a chunk check that returns early + calls `c.nudge()` when the chunk is full. `internal/runtime/server/connection_sync_test.go` (new) carries four tests: chunk-boundary, drains-across-calls, empty-queue no-op, and an integration test that proves a queued input message gets dispatched mid-backlog instead of waiting for a full drain.

**Tech Stack:** Go 1.24.3, standard library `net.Pipe` for in-memory connection wiring, `github.com/framegrace/texelation/protocol` for `BufferDelta` framing. Tests follow the patterns in `internal/runtime/server/connection_rehydrate_resume_test.go` (same-package access to `connection` internals, `net.Pipe` for synchronous client/server channels, `protocol.ReadMessage` to count framed messages on the client side).

**Spec:** `docs/superpowers/specs/2026-05-03-input-fairness-chunked-send-design.md`

---

## File Structure

| Path | Status | Responsibility |
|------|--------|----------------|
| `internal/runtime/server/connection_sync.go` | Modify | Add `sendChunkSize` constant; change `sendPending()` to chunk + self-nudge |
| `internal/runtime/server/connection_sync_test.go` | Create | Four tests covering the chunk boundary, multi-call drain, empty-queue no-op, and serve-loop input interleaving |

No other files touched. No protocol changes. No new exported APIs.

---

## Task 1: Failing chunk-boundary test

Establishes a TDD baseline — the test asserts the chunk behaviour that doesn't exist yet, runs, and fails. Sets up the helper shape that the next three tests reuse.

**Files:**
- Create: `internal/runtime/server/connection_sync_test.go`

- [ ] **Step 1: Write the failing test**

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/connection_sync_test.go
// Summary: Tests for chunked sendPending — bounds per-call writes and
// self-nudges so input messages can interleave between flush batches.

package server

import (
	"net"
	"testing"
	"time"

	"github.com/framegrace/texelation/protocol"
)

// newPipedSendingConnection wires a Session containing the requested
// number of pre-enqueued BufferDeltas to a *connection backed by an
// in-memory net.Pipe, with awaitResume=false so sendPending will
// actually run. Returns (conn, clientConn) — the caller closes
// clientConn at end-of-test to unblock any pending writes.
func newPipedSendingConnection(t *testing.T, queueSize int) (*connection, net.Conn) {
	t.Helper()
	sessionID := [16]byte{0xaa}
	session := NewSession(sessionID, 64)

	// Seed the session with queueSize trivial BufferDeltas so
	// sendPending has a backlog to drain. PaneID + Rows are
	// minimally populated — the test only cares about message
	// count on the wire, not content.
	var paneID [16]byte
	paneID[0] = 1
	for i := 0; i < queueSize; i++ {
		delta := protocol.BufferDelta{
			PaneID:  paneID,
			RowBase: int64(i),
			Styles:  []protocol.StyleEntry{{AttrFlags: 0}},
			Rows: []protocol.RowDelta{
				{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "x", StyleIndex: 0}}},
			},
		}
		if err := session.EnqueueDiff(delta); err != nil {
			t.Fatalf("EnqueueDiff[%d]: %v", i, err)
		}
	}

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })
	t.Cleanup(func() { _ = serverConn.Close() })

	conn := newConnection(serverConn, session, nopSink{}, false /*awaitResume*/, false /*rehydrated*/)
	return conn, clientConn
}

// readN reads exactly n framed messages from clientConn or fails the
// test. A short deadline keeps the test from hanging if sendPending
// returns early without writing the expected count.
func readN(t *testing.T, clientConn net.Conn, n int) {
	t.Helper()
	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	defer func() { _ = clientConn.SetReadDeadline(time.Time{}) }()
	for i := 0; i < n; i++ {
		if _, _, err := protocol.ReadMessage(clientConn); err != nil {
			t.Fatalf("ReadMessage[%d]: %v", i, err)
		}
	}
}

// pendingChannelTicked reports whether c.pending currently has a
// queued tick. Non-blocking — used to assert sendPending's self-nudge
// after a partial drain.
func pendingChannelTicked(c *connection) bool {
	select {
	case <-c.pending:
		return true
	default:
		return false
	}
}

// TestSendPending_ChunksAtBoundary verifies a single sendPending call
// stops at sendChunkSize when the queue is larger and re-nudges
// c.pending so the connection's select loop comes back for more.
// Without the chunk bound, the entire backlog would flush in one
// call and block input dispatch for the duration.
func TestSendPending_ChunksAtBoundary(t *testing.T) {
	const queued = 100 // >> sendChunkSize so we definitely chunk

	conn, clientConn := newPipedSendingConnection(t, queued)

	// Drain c.pending first so we can detect a fresh nudge from
	// sendPending. newConnection's constructor calls c.nudge() once
	// during setup; without this drain we'd see that prior tick
	// rather than the chunk-boundary self-nudge.
	for pendingChannelTicked(conn) {
	}

	// Run sendPending in a goroutine because writes to net.Pipe
	// block until the read side consumes; the test consumes
	// exactly sendChunkSize messages then asserts.
	errCh := make(chan error, 1)
	go func() { errCh <- conn.sendPending() }()

	readN(t, clientConn, sendChunkSize)

	// sendPending should now have returned (after writing the chunk
	// and re-nudging). Give it a beat to land.
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("sendPending: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sendPending did not return after chunk boundary")
	}

	if !pendingChannelTicked(conn) {
		t.Error("sendPending did not re-nudge c.pending after chunk boundary")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/runtime/server/ -run TestSendPending_ChunksAtBoundary -count=1 -v
```

Expected: FAIL with the test hanging on `readN` or with `sendPending did not re-nudge c.pending` — depending on which symptom of the un-chunked behaviour fires first. The current `sendPending` writes ALL 100 messages, but the test only reads `sendChunkSize` (32), so the goroutine blocks on the 33rd write and `errCh` never receives. The 2-second deadline in `readN` then fails first; if the read side is fast enough that `sendPending` finishes before assertion, the second check fires because the unmodified `sendPending` doesn't call `nudge()`. Either way, fails.

- [ ] **Step 3: Commit the failing test**

```bash
git add internal/runtime/server/connection_sync_test.go
git commit -m "Test: failing chunk-boundary assertion for sendPending"
```

The failing test is committed so the next task's diff cleanly shows the implementation flipping it to passing.

---

## Task 2: Implement chunked sendPending

Six-line change: a constant, a counter, a chunk check + early return + self-nudge.

**Files:**
- Modify: `internal/runtime/server/connection_sync.go:18-36`

- [ ] **Step 1: Apply the chunked sendPending implementation**

Replace the existing `sendPending` function (lines 18-36) and add the constant immediately above it:

```go
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

- [ ] **Step 2: Run the failing test to verify it now passes**

```bash
go test ./internal/runtime/server/ -run TestSendPending_ChunksAtBoundary -count=1 -v
```

Expected: PASS. The test reads exactly 32 messages, sendPending returns nil after the chunk boundary, and `c.pending` carries the self-nudge tick.

- [ ] **Step 3: Run the existing connection_rehydrate_resume tests to confirm no regression**

```bash
go test ./internal/runtime/server/ -run TestConnection -count=1 -race
```

Expected: all PASS. Those tests use small queues that fit in one chunk; chunking is invisible to them.

- [ ] **Step 4: Commit the implementation**

```bash
git add internal/runtime/server/connection_sync.go
git commit -m "$(cat <<'EOF'
Chunk sendPending so input can interleave with heavy backlog

Server-side connection.serve() called sendPending() at the top of
every loop iteration, draining the entire pending diff backlog
before the next select pick — so a heavy publish pass blocked
input dispatch (keystrokes for pane B sat in c.incoming until
pane A's output finished flushing). Bound the loop to
sendChunkSize=32 writes per call and self-nudge c.pending if more
remain; the next select iteration can now pick up an incoming
input message instead.
EOF
)"
```

---

## Task 3: Multi-call drain test

Verifies that repeated sendPending calls on the same backlog correctly resume from `c.lastSent` — no diffs duplicated, no diffs skipped, no off-by-one at the chunk boundary.

**Files:**
- Modify: `internal/runtime/server/connection_sync_test.go` (append)

- [ ] **Step 1: Append the test**

```go
// TestSendPending_DrainsAcrossCalls verifies that calling
// sendPending repeatedly until the backlog is empty delivers every
// diff in monotonic sequence. The chunk-resume guard
// (`if diff.Sequence <= c.lastSent`) is the load-bearing piece —
// off-by-one would either skip diffs or replay them.
func TestSendPending_DrainsAcrossCalls(t *testing.T) {
	const queued = 100

	conn, clientConn := newPipedSendingConnection(t, queued)

	// Reader goroutine: collects every framed BufferDelta into
	// seqs so the test can assert monotonic sequence at the end.
	type readResult struct {
		seqs []uint64
		err  error
	}
	resCh := make(chan readResult, 1)
	go func() {
		var seqs []uint64
		_ = clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			hdr, _, err := protocol.ReadMessage(clientConn)
			if err != nil {
				resCh <- readResult{seqs: seqs, err: err}
				return
			}
			seqs = append(seqs, hdr.Sequence)
			if len(seqs) == queued {
				resCh <- readResult{seqs: seqs}
				return
			}
		}
	}()

	// Drive sendPending repeatedly until everything is sent. The
	// number of calls needed is ceil(queued / sendChunkSize).
	for i := 0; i < (queued+sendChunkSize-1)/sendChunkSize; i++ {
		if err := conn.sendPending(); err != nil {
			t.Fatalf("sendPending[%d]: %v", i, err)
		}
	}

	res := <-resCh
	if res.err != nil {
		t.Fatalf("reader: %v (got %d/%d messages)", res.err, len(res.seqs), queued)
	}
	if len(res.seqs) != queued {
		t.Fatalf("got %d messages, want %d", len(res.seqs), queued)
	}
	for i, seq := range res.seqs {
		want := uint64(i + 1) // EnqueueDiff assigns seq=1..queued
		if seq != want {
			t.Errorf("message %d: seq=%d, want %d", i, seq, want)
		}
	}
}

// TestSendPending_EmptyQueueIsSilentNoOp verifies that sendPending
// with no pending diffs is a no-op AND does not produce a spurious
// self-nudge (which would burn CPU spinning the serve loop).
func TestSendPending_EmptyQueueIsSilentNoOp(t *testing.T) {
	conn, _ := newPipedSendingConnection(t, 0)

	// Drain the constructor's nudge so the test sees a clean
	// channel before sendPending runs.
	for pendingChannelTicked(conn) {
	}

	if err := conn.sendPending(); err != nil {
		t.Fatalf("sendPending: %v", err)
	}

	if pendingChannelTicked(conn) {
		t.Error("sendPending nudged c.pending on empty queue (would spin the serve loop)")
	}
}
```

- [ ] **Step 2: Run both new tests**

```bash
go test ./internal/runtime/server/ -run "TestSendPending_DrainsAcrossCalls|TestSendPending_EmptyQueueIsSilentNoOp" -count=1 -v
```

Expected: both PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/server/connection_sync_test.go
git commit -m "Test: chunked sendPending drains across calls + empty-queue no-op"
```

---

## Task 4: Serve-loop input-interleave integration test

Drives a full `serve()` goroutine to prove the user-visible behaviour: an input message queued behind a heavy diff backlog gets dispatched before the entire backlog flushes. This is the regression guard for the bug the spec describes.

**Files:**
- Modify: `internal/runtime/server/connection_sync_test.go` (append)

- [ ] **Step 1: Append the test**

```go
// TestServeLoop_InputInterleaves drives a serve() goroutine with a
// heavy diff backlog AND a queued input message, asserting that
// the input gets dispatched before the entire backlog flushes.
// Without the chunk-and-yield in sendPending, the input would wait
// behind every queued diff — the user-visible bug this spec fixes.
//
// Approach: count diffs the client reads while sniffing handleMessage
// via a sink that records each MsgKeyEvent it sees. The test passes
// if the recorded "diffs read at the time of dispatch" is strictly
// less than the full backlog.
func TestServeLoop_InputInterleaves(t *testing.T) {
	const queued = 200

	conn, clientConn := newPipedSendingConnection(t, queued)

	// Replace the nopSink with one that signals via channel when a
	// MouseEvent is dispatched. Mouse rather than Key avoids needing
	// a focus-bearing pane in the desktop tree.
	dispatched := make(chan struct{}, 1)
	conn.sink = &recordingSink{onMouse: func() {
		select {
		case dispatched <- struct{}{}:
		default:
		}
	}}

	// Make resume not block input handling — connection ctor sets
	// awaitResume=false in our helper, but be explicit.
	conn.awaitResume = false

	// Start serve(). It will begin draining diffs in chunks and
	// process incoming messages between chunks.
	serveErr := make(chan error, 1)
	go func() { serveErr <- conn.serve() }()

	// Reader goroutine: counts every BufferDelta as it arrives.
	// Stops when the dispatched signal fires (at which point the
	// count is "diffs flushed before input dispatch") OR when the
	// full backlog is drained.
	type counter struct {
		atDispatch int
		final      int
	}
	cntCh := make(chan counter, 1)
	go func() {
		_ = clientConn.SetReadDeadline(time.Now().Add(10 * time.Second))
		count := 0
		atDispatch := -1
		for count < queued {
			if _, _, err := protocol.ReadMessage(clientConn); err != nil {
				cntCh <- counter{atDispatch: atDispatch, final: count}
				return
			}
			count++
			if atDispatch < 0 {
				select {
				case <-dispatched:
					atDispatch = count
				default:
				}
			}
		}
		cntCh <- counter{atDispatch: atDispatch, final: count}
	}()

	// Inject a MsgMouseEvent onto the connection's incoming
	// channel — bypasses the wire-side reader so we don't have to
	// race with backlog reads on the same pipe direction.
	mousePayload, err := protocol.EncodeMouseEvent(protocol.MouseEvent{X: 0, Y: 0})
	if err != nil {
		t.Fatalf("encode mouse: %v", err)
	}
	mouseHdr := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgMouseEvent,
		Flags:     protocol.FlagChecksum,
		SessionID: conn.session.ID(),
	}
	conn.incoming <- protocolMessage{header: mouseHdr, payload: mousePayload}

	// Wait for the dispatch signal. If it never fires the input
	// was starved — that's the bug.
	select {
	case <-dispatched:
		// Re-fire the channel for the reader's later check (the
		// reader's select on `dispatched` may race with this main
		// goroutine's wait; signalling twice is harmless because
		// the receive above drained one tick).
		select {
		case dispatched <- struct{}{}:
		default:
		}
	case <-time.After(5 * time.Second):
		t.Fatal("input was never dispatched — sendPending starved handleMessage")
	}

	// Close the client side to unblock serve()'s pipe writes if
	// any are still in flight, then drain results.
	_ = clientConn.Close()

	cnt := <-cntCh
	<-serveErr // discard

	if cnt.atDispatch < 0 {
		t.Fatal("reader never observed dispatch signal")
	}
	if cnt.atDispatch >= queued {
		t.Errorf("input dispatched after %d diffs (≥ %d backlog) — chunking did not yield",
			cnt.atDispatch, queued)
	}
	t.Logf("input dispatched after %d diffs (backlog %d)", cnt.atDispatch, queued)
}

// recordingSink fires onMouse for every HandleMouseEvent dispatched
// to it; everything else delegates to embedded nopSink. Used to
// observe input dispatch from a serve() goroutine without spinning
// up a DesktopSink.
type recordingSink struct {
	nopSink
	onMouse func()
}

func (s *recordingSink) HandleMouseEvent(sess *Session, ev protocol.MouseEvent) {
	s.onMouse()
}
```

- [ ] **Step 2: Run the new test**

```bash
go test ./internal/runtime/server/ -run TestServeLoop_InputInterleaves -count=1 -v
```

Expected: PASS. The test logs the diff-count-at-dispatch, which should be a small multiple of `sendChunkSize` (e.g. ~32–64) — well below the 200-diff backlog.

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/server/connection_sync_test.go
git commit -m "Test: serve loop interleaves input dispatch with chunked flush"
```

---

## Task 5: Race detector + final verification

Confirms no goroutine races introduced by the self-nudge + concurrent serve loop, and that the broader test suite still passes.

- [ ] **Step 1: Race detector on the new tests**

```bash
go test ./internal/runtime/server/ -run "TestSendPending|TestServeLoop_InputInterleaves" -count=10 -race
```

Expected: PASS, no race warnings.

- [ ] **Step 2: Full server-package test suite**

```bash
go test ./internal/runtime/server/ -count=1 -race
```

Expected: PASS for everything that already passed on main. (`apps/configeditor` has a pre-existing race that is not in this package — ignore.)

- [ ] **Step 3: Build the full repo**

```bash
go build ./...
```

Expected: clean build, no warnings.

- [ ] **Step 4: gofmt check**

```bash
gofmt -l internal/runtime/server/connection_sync.go internal/runtime/server/connection_sync_test.go
```

Expected: no output (no drift).

If `gofmt` reports drift, run `gofmt -w` on the listed files and amend the previous commit:

```bash
gofmt -w internal/runtime/server/connection_sync.go internal/runtime/server/connection_sync_test.go
git add -u internal/runtime/server/
git commit --amend --no-edit
```

- [ ] **Step 5: No commit needed if everything is clean**

If steps 1–4 pass with no edits, this task ships nothing new — it's a verification gate. Move on to opening the PR.

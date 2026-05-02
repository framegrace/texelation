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
	maxDiffs := 64
	if queueSize > maxDiffs {
		maxDiffs = queueSize
	}
	session := NewSession(sessionID, maxDiffs)

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

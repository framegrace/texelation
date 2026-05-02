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

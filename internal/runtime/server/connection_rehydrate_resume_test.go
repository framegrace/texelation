// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/connection_rehydrate_resume_test.go
// Summary: Unit tests for the c.rehydrated=true branches in
// connection_handler.go's MsgResumeRequest path. These branches were
// added to fix three bugs found during Plan D2 manual e2e (see commit
// 1b62b81): a stale LastSequence from a prior daemon's lifetime, an
// initialSnapshotSent flip that bypassed handleClientReady, and an
// early snapshot+publish at boot dims that produced a dual-snapshot
// rendering glitch. Without these tests a future refactor of the
// MsgResumeRequest handler could re-introduce any of the three.

package server

import (
	"net"
	"testing"
	"time"

	"github.com/framegrace/texelation/protocol"
)

// driveResumeRequest writes a MsgResumeRequest with the given fields
// onto clientConn, then closes clientConn so the connection's serve
// loop sees EOF and returns. Returns the channel that will receive
// serve()'s exit error so callers can wait for processing to finish
// before asserting on connection state.
func driveResumeRequest(t *testing.T, conn *connection, clientConn net.Conn, req protocol.ResumeRequest) <-chan error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		errCh <- conn.serve()
	}()

	payload, err := protocol.EncodeResumeRequest(req)
	if err != nil {
		t.Fatalf("encode resume request: %v", err)
	}
	if err := protocol.WriteMessage(clientConn, protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgResumeRequest,
		Flags:     protocol.FlagChecksum,
		SessionID: req.SessionID,
	}, payload); err != nil {
		t.Fatalf("write resume request: %v", err)
	}
	return errCh
}

// TestConnection_RehydratedResume_ZerosLastAcked verifies that a
// rehydrated connection treats the client's LastSequence as
// meaningless (the value is from a prior daemon's lifetime) and zeros
// c.lastAcked instead. Without this, Session.Pending(after:LastSequence)
// would filter out every fresh delta (sequences restart at 1 in the
// new daemon) and the client would appear frozen.
func TestConnection_RehydratedResume_ZerosLastAcked(t *testing.T) {
	sessionID := [16]byte{0xb0, 0x01}
	session := NewSession(sessionID, 64)

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	conn := newConnection(serverConn, session, nopSink{}, true /*awaitResume*/, true /*rehydrated*/)

	errCh := driveResumeRequest(t, conn, clientConn, protocol.ResumeRequest{
		SessionID:    sessionID,
		LastSequence: 999, // stale value from prior daemon's lifetime
	})

	// Close the client side so serve() sees EOF and returns. Without
	// this the test would hang waiting for the next read.
	_ = clientConn.Close()

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not exit within 2s after EOF")
	}

	if conn.lastAcked != 0 {
		t.Fatalf("rehydrated lastAcked: got %d, want 0 (stale 999 must be ignored)", conn.lastAcked)
	}
}

// TestConnection_NonRehydratedResume_HonorsLastAcked is the
// counterpart that proves the in-process resume path STILL honors
// the client's LastSequence. An in-process resume (live cache hit)
// has its diff queue intact, so honoring LastSequence is correct —
// it prevents replaying already-acked diffs.
func TestConnection_NonRehydratedResume_HonorsLastAcked(t *testing.T) {
	sessionID := [16]byte{0xb0, 0x02}
	session := NewSession(sessionID, 64)

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	conn := newConnection(serverConn, session, nopSink{}, true /*awaitResume*/, false /*rehydrated*/)

	errCh := driveResumeRequest(t, conn, clientConn, protocol.ResumeRequest{
		SessionID:    sessionID,
		LastSequence: 42,
	})

	// Drain whatever the resume branch writes (a TreeSnapshot for the
	// non-rehydrated path) so the pipe doesn't block, then close.
	_ = clientConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	_, _, _ = protocol.ReadMessage(clientConn)
	_ = clientConn.SetReadDeadline(time.Time{})
	_ = clientConn.Close()

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not exit within 2s after EOF")
	}

	if conn.lastAcked != 42 {
		t.Fatalf("non-rehydrated lastAcked: got %d, want 42 (in-process resume must honor LastSequence)", conn.lastAcked)
	}
}

// TestConnection_RehydratedResume_LeavesInitialSnapshotSentFalse
// proves that for a rehydrated connection the resume handler does
// NOT set c.initialSnapshotSent, so handleClientReady will run when
// MsgClientReady arrives. Without this, the desktop would never get
// SetViewportSize called with the client's actual dimensions and the
// tree resize / per-pane sendPaneState loop / statusbar layout pass
// would never run — leaving the client with no pane focus, no
// borders, and publishes that emit against 0×0 buffers.
func TestConnection_RehydratedResume_LeavesInitialSnapshotSentFalse(t *testing.T) {
	sessionID := [16]byte{0xb0, 0x03}
	session := NewSession(sessionID, 64)

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	conn := newConnection(serverConn, session, nopSink{}, true /*awaitResume*/, true /*rehydrated*/)

	errCh := driveResumeRequest(t, conn, clientConn, protocol.ResumeRequest{
		SessionID: sessionID,
	})

	_ = clientConn.Close()

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not exit within 2s after EOF")
	}

	if conn.initialSnapshotSent {
		t.Fatal("rehydrated initialSnapshotSent: got true, want false (handleClientReady must run on MsgClientReady)")
	}
}

// TestConnection_NonRehydratedResume_FlipsInitialSnapshotSent is the
// counterpart proving the in-process resume path DOES flip the flag.
// In that case the resume branch already shipped a usable snapshot
// from a well-dimensioned desktop, so handleClientReady's repeat
// work would be wasteful.
func TestConnection_NonRehydratedResume_FlipsInitialSnapshotSent(t *testing.T) {
	sessionID := [16]byte{0xb0, 0x04}
	session := NewSession(sessionID, 64)

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	conn := newConnection(serverConn, session, nopSink{}, true /*awaitResume*/, false /*rehydrated*/)

	errCh := driveResumeRequest(t, conn, clientConn, protocol.ResumeRequest{
		SessionID: sessionID,
	})

	// Drain the TreeSnapshot the non-rehydrated branch writes.
	_ = clientConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	_, _, _ = protocol.ReadMessage(clientConn)
	_ = clientConn.SetReadDeadline(time.Time{})
	_ = clientConn.Close()

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not exit within 2s after EOF")
	}

	if !conn.initialSnapshotSent {
		t.Fatal("non-rehydrated initialSnapshotSent: got false, want true (in-process resume already shipped snapshot)")
	}
}

// TestConnection_RehydratedResume_SkipsEarlySnapshot verifies the
// rehydrated branch does NOT write a MsgTreeSnapshot during the
// MsgResumeRequest handler. Sending one at boot dims (the new daemon
// hasn't received the client's viewport yet) would force the client
// to render the pane twice — once at the wrong dims, then again from
// handleClientReady at correct dims — producing the dual-snapshot
// glitch with missing top/bottom borders that the user reported
// during e2e (see commit 68437d3).
//
// We assert this by reading from the client side with a deadline
// after the resume request was processed; any data on the wire would
// be a snapshot the resume branch should have skipped.
func TestConnection_RehydratedResume_SkipsEarlySnapshot(t *testing.T) {
	sessionID := [16]byte{0xb0, 0x05}
	session := NewSession(sessionID, 64)

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	conn := newConnection(serverConn, session, nopSink{}, true /*awaitResume*/, true /*rehydrated*/)

	_ = driveResumeRequest(t, conn, clientConn, protocol.ResumeRequest{
		SessionID: sessionID,
	})

	// Read with a short deadline. The rehydrated branch must NOT have
	// written a TreeSnapshot in response to MsgResumeRequest, so we
	// expect this read to time out / return io.EOF rather than yield
	// a message.
	_ = clientConn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	hdr, _, err := protocol.ReadMessage(clientConn)
	if err == nil {
		t.Fatalf("rehydrated resume wrote unexpected message type=%d; want no message before MsgClientReady", hdr.Type)
	}
}

// TestConnection_NonRehydratedResume_SendsEarlySnapshot is the
// counterpart proving the in-process path still ships its snapshot
// during the resume handler (because for an in-process resume the
// desktop is already at correct dims and the snapshot is useful).
func TestConnection_NonRehydratedResume_SendsEarlySnapshot(t *testing.T) {
	sessionID := [16]byte{0xb0, 0x06}
	session := NewSession(sessionID, 64)

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	conn := newConnection(serverConn, session, nopSink{}, true /*awaitResume*/, false /*rehydrated*/)

	_ = driveResumeRequest(t, conn, clientConn, protocol.ResumeRequest{
		SessionID: sessionID,
	})

	// In-process resume: expect a MsgTreeSnapshot on the wire within
	// the deadline. (nopSink.Snapshot returns an empty TreeSnapshot,
	// so the message is short but present.)
	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	hdr, _, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatalf("non-rehydrated resume: expected MsgTreeSnapshot, got read error: %v", err)
	}
	if hdr.Type != protocol.MsgTreeSnapshot {
		t.Fatalf("non-rehydrated resume: wrote message type=%d, want MsgTreeSnapshot (%d)", hdr.Type, protocol.MsgTreeSnapshot)
	}
}

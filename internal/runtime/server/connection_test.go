// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/connection_test.go
// Summary: Exercises connection behaviour to ensure the server runtime remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"io"
	"net"
	"testing"
	"time"

	"texelation/protocol"
)

func TestConnectionFlushesPendingDiffsOnNudge(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	sessionID := [16]byte{1}
	session := NewSession(sessionID, 16)

	conn := newConnection(serverConn, session, nopSink{}, false)

	errCh := make(chan error, 1)
	go func() {
		errCh <- conn.serve()
	}()

	time.Sleep(10 * time.Millisecond)

	delta := protocol.BufferDelta{PaneID: [16]byte{2}, Revision: 1}
	if err := session.EnqueueDiff(delta); err != nil {
		t.Fatalf("enqueue diff: %v", err)
	}
	conn.nudge()

	_ = clientConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	header, _, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	if header.Type != protocol.MsgBufferDelta {
		t.Fatalf("expected buffer delta, got %d", header.Type)
	}

	ackPayload, err := protocol.EncodeBufferAck(protocol.BufferAck{Sequence: header.Sequence})
	if err != nil {
		t.Fatalf("encode ack: %v", err)
	}
	ackHeader := protocol.Header{Version: protocol.Version, Type: protocol.MsgBufferAck, Flags: protocol.FlagChecksum, SessionID: sessionID}
	if err := protocol.WriteMessage(clientConn, ackHeader, ackPayload); err != nil {
		t.Fatalf("write ack: %v", err)
	}

	clientConn.Close()

	select {
	case err := <-errCh:
		if err != nil && err != io.EOF {
			t.Fatalf("serve returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("connection serve did not exit")
	}
}

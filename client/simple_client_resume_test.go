// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package client

import (
	"bytes"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/framegrace/texelation/protocol"
)

// captureConn buffers writes so tests can parse the frames the client sent.
// Read returns immediately with net.ErrClosed to unblock RequestResume after
// the write side is exercised (it calls ReadMessage after writing).
type captureConn struct {
	out bytes.Buffer
}

func (c *captureConn) Read(p []byte) (int, error)         { return 0, net.ErrClosed }
func (c *captureConn) Write(p []byte) (int, error)        { return c.out.Write(p) }
func (c *captureConn) Close() error                       { return nil }
func (c *captureConn) LocalAddr() net.Addr                { return nil }
func (c *captureConn) RemoteAddr() net.Addr               { return nil }
func (c *captureConn) SetDeadline(t time.Time) error      { return nil }
func (c *captureConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *captureConn) SetWriteDeadline(t time.Time) error { return nil }

func TestRequestResume_EncodesPaneViewports(t *testing.T) {
	c := &SimpleClient{}
	conn := &captureConn{}
	sessionID := [16]byte{1, 2, 3}
	viewports := []protocol.PaneViewportState{
		{PaneID: [16]byte{9}, ViewBottomIdx: 42, AutoFollow: false, ViewportRows: 24, ViewportCols: 80},
	}
	// RequestResume returns an error because Read returns ErrClosed after the
	// write. We only care about the bytes written.
	_, _, err := c.RequestResume(conn, sessionID, 7, viewports)
	if err == nil {
		t.Fatalf("expected read error after write, got nil")
	}
	if !errors.Is(err, net.ErrClosed) {
		// Accept io.EOF too in case the framing layer wraps differently.
		// Just ensure we failed at read-time, not encode-time.
		t.Logf("RequestResume returned: %v", err)
	}

	body := conn.out.Bytes()
	if len(body) == 0 {
		t.Fatalf("no bytes written")
	}
	r := bytes.NewReader(body)
	hdr, payload, err := protocol.ReadMessage(r)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if hdr.Type != protocol.MsgResumeRequest {
		t.Fatalf("msg type: got %v want MsgResumeRequest", hdr.Type)
	}
	req, err := protocol.DecodeResumeRequest(payload)
	if err != nil {
		t.Fatalf("DecodeResumeRequest: %v", err)
	}
	if req.LastSequence != 7 {
		t.Fatalf("LastSequence: got %d want 7", req.LastSequence)
	}
	if req.SessionID != sessionID {
		t.Fatalf("SessionID: got %v want %v", req.SessionID, sessionID)
	}
	if len(req.PaneViewports) != 1 {
		t.Fatalf("PaneViewports len: got %d want 1", len(req.PaneViewports))
	}
	if req.PaneViewports[0].ViewBottomIdx != 42 {
		t.Fatalf("ViewBottomIdx: got %d want 42", req.PaneViewports[0].ViewBottomIdx)
	}
	if req.PaneViewports[0].ViewportRows != 24 {
		t.Fatalf("ViewportRows: got %d want 24", req.PaneViewports[0].ViewportRows)
	}
}

func TestRequestResume_EmptyPaneViewports(t *testing.T) {
	// Nil/empty viewports should be encoded with count=0 and decode without errors.
	c := &SimpleClient{}
	conn := &captureConn{}
	sessionID := [16]byte{5}
	_, _, err := c.RequestResume(conn, sessionID, 0, nil)
	if err == nil {
		t.Fatalf("expected read error, got nil")
	}
	body := conn.out.Bytes()
	if len(body) == 0 {
		t.Fatalf("no bytes written")
	}
	r := bytes.NewReader(body)
	hdr, payload, err := protocol.ReadMessage(r)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if hdr.Type != protocol.MsgResumeRequest {
		t.Fatalf("msg type: got %v want MsgResumeRequest", hdr.Type)
	}
	req, err := protocol.DecodeResumeRequest(payload)
	if err != nil {
		t.Fatalf("DecodeResumeRequest: %v", err)
	}
	if len(req.PaneViewports) != 0 {
		t.Fatalf("empty viewports: got %d want 0", len(req.PaneViewports))
	}
}

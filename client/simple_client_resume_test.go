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
	_, _, err := c.RequestResume(conn, sessionID, 7, viewports, nil)
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
	_, _, err := c.RequestResume(conn, sessionID, 0, nil, nil)
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

// scriptedConn delivers a pre-encoded sequence of frames on Read in
// order, then returns io.EOF. Writes are buffered so tests can also
// inspect what RequestResume sent. Used to drive the progress-loop
// path: server emits N MsgBootProgress messages then a non-progress
// reply (MsgTreeSnapshot here), and we verify (a) every progress
// message reached onProgress in order and (b) the non-progress reply
// is what RequestResume returns.
type scriptedConn struct {
	out      bytes.Buffer
	readBuf  bytes.Buffer
	deadline time.Time
}

func newScriptedConn(frames [][]byte) *scriptedConn {
	c := &scriptedConn{}
	for _, f := range frames {
		c.readBuf.Write(f)
	}
	return c
}

func (c *scriptedConn) Read(p []byte) (int, error) {
	if c.readBuf.Len() == 0 {
		return 0, net.ErrClosed
	}
	return c.readBuf.Read(p)
}
func (c *scriptedConn) Write(p []byte) (int, error)        { return c.out.Write(p) }
func (c *scriptedConn) Close() error                       { return nil }
func (c *scriptedConn) LocalAddr() net.Addr                { return nil }
func (c *scriptedConn) RemoteAddr() net.Addr               { return nil }
func (c *scriptedConn) SetDeadline(t time.Time) error      { return nil }
func (c *scriptedConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *scriptedConn) SetWriteDeadline(t time.Time) error { return nil }

// frameMessage encodes a header + payload in the framing protocol.WriteMessage uses.
func frameMessage(t *testing.T, msgType protocol.MessageType, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	hdr := protocol.Header{Version: protocol.Version, Type: msgType, Flags: protocol.FlagChecksum}
	if err := protocol.WriteMessage(&buf, hdr, payload); err != nil {
		t.Fatalf("frameMessage: %v", err)
	}
	return buf.Bytes()
}

func TestRequestResume_ProgressLoopForwardsAndReturnsResponse(t *testing.T) {
	// Scripted server: two MsgBootProgress frames followed by a
	// MsgTreeSnapshot. RequestResume must forward both progress
	// messages to onProgress (in order) and return the tree
	// snapshot as the response.
	progPayload1, err := protocol.EncodeBootProgress(protocol.BootProgress{Message: "Validating session…"})
	if err != nil {
		t.Fatalf("encode progress 1: %v", err)
	}
	progPayload2, err := protocol.EncodeBootProgress(protocol.BootProgress{Message: "Restoring pane 1 of 3…"})
	if err != nil {
		t.Fatalf("encode progress 2: %v", err)
	}
	snapPayload, err := protocol.EncodeTreeSnapshot(protocol.TreeSnapshot{})
	if err != nil {
		t.Fatalf("encode snapshot: %v", err)
	}

	frames := [][]byte{
		frameMessage(t, protocol.MsgBootProgress, progPayload1),
		frameMessage(t, protocol.MsgBootProgress, progPayload2),
		frameMessage(t, protocol.MsgTreeSnapshot, snapPayload),
	}
	conn := newScriptedConn(frames)

	c := &SimpleClient{}
	var got []string
	hdr, body, err := c.RequestResume(conn, [16]byte{1}, 0, nil, func(m string) {
		got = append(got, m)
	})
	if err != nil {
		t.Fatalf("RequestResume: %v", err)
	}
	if hdr.Type != protocol.MsgTreeSnapshot {
		t.Fatalf("response type: got %v, want MsgTreeSnapshot", hdr.Type)
	}
	if len(body) == 0 {
		t.Error("response body is empty")
	}
	want := []string{"Validating session…", "Restoring pane 1 of 3…"}
	if len(got) != len(want) {
		t.Fatalf("forwarded %d progress messages, want %d: %#v", len(got), len(want), got)
	}
	for i, m := range want {
		if got[i] != m {
			t.Errorf("progress[%d]: got %q want %q", i, got[i], m)
		}
	}
}

func TestRequestResume_ProgressLoopNilCallbackStillDrains(t *testing.T) {
	// When onProgress is nil, RequestResume must still drain
	// progress frames (otherwise the response would never be
	// reached). The actual behaviour should be: skip the frame and
	// keep reading.
	progPayload, err := protocol.EncodeBootProgress(protocol.BootProgress{Message: "Validating session…"})
	if err != nil {
		t.Fatalf("encode progress: %v", err)
	}
	snapPayload, err := protocol.EncodeTreeSnapshot(protocol.TreeSnapshot{})
	if err != nil {
		t.Fatalf("encode snapshot: %v", err)
	}
	frames := [][]byte{
		frameMessage(t, protocol.MsgBootProgress, progPayload),
		frameMessage(t, protocol.MsgTreeSnapshot, snapPayload),
	}
	conn := newScriptedConn(frames)

	c := &SimpleClient{}
	hdr, _, err := c.RequestResume(conn, [16]byte{1}, 0, nil, nil)
	if err != nil {
		t.Fatalf("RequestResume: %v", err)
	}
	if hdr.Type != protocol.MsgTreeSnapshot {
		t.Errorf("response type: got %v, want MsgTreeSnapshot", hdr.Type)
	}
}

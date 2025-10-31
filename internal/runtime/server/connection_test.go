// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/connection_test.go
// Summary: Exercises connection behaviour to ensure the server runtime remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/protocol"
	"texelation/texel"
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

type connectionTestDriver struct {
	width  int
	height int
}

func (connectionTestDriver) Init() error                                    { return nil }
func (connectionTestDriver) Fini()                                          {}
func (d connectionTestDriver) Size() (int, int)                             { return d.width, d.height }
func (connectionTestDriver) SetStyle(tcell.Style)                           {}
func (connectionTestDriver) HideCursor()                                    {}
func (connectionTestDriver) Show()                                          {}
func (connectionTestDriver) PollEvent() tcell.Event                         { return nil }
func (connectionTestDriver) SetContent(int, int, rune, []rune, tcell.Style) {}
func (connectionTestDriver) GetContent(int, int) (rune, []rune, tcell.Style, int) {
	return ' ', nil, tcell.StyleDefault, 1
}

type staticApp struct {
	title string
	cols  int
	rows  int
}

func (s *staticApp) Run() error                     { return nil }
func (s *staticApp) Stop()                          {}
func (s *staticApp) Resize(cols, rows int)          { s.cols, s.rows = cols, rows }
func (s *staticApp) Render() [][]texel.Cell         { return makeBuffer(s.cols, s.rows) }
func (s *staticApp) GetTitle() string               { return s.title }
func (s *staticApp) HandleKey(*tcell.EventKey)      {}
func (s *staticApp) SetRefreshNotifier(chan<- bool) {}

func makeBuffer(cols, rows int) [][]texel.Cell {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	buf := make([][]texel.Cell, rows)
	for y := 0; y < rows; y++ {
		line := make([]texel.Cell, cols)
		for x := 0; x < cols; x++ {
			line[x] = texel.Cell{Ch: ' '}
		}
		buf[y] = line
	}
	return buf
}

func drainInitialMessages(conn net.Conn, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		if _, _, err := protocol.ReadMessage(conn); err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
	}
}

func newDesktopSink(t *testing.T) (*DesktopSink, *texel.DesktopEngine, func()) {
	t.Helper()
	driver := connectionTestDriver{width: 80, height: 24}
	lifecycle := texel.NoopAppLifecycle{}
	shellFactory := func() texel.App { return &staticApp{title: "shell"} }
	welcomeFactory := func() texel.App { return &staticApp{title: "welcome"} }

	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}
	sink := NewDesktopSink(desktop)
	cleanup := func() {
		desktop.Close()
	}
	return sink, desktop, cleanup
}

func TestConnectionHandlesResizeBroadcastsSnapshot(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	sessionID := [16]byte{3}
	session := NewSession(sessionID, 64)

	sink, desktop, cleanup := newDesktopSink(t)
	defer cleanup()

	stopInitial := make(chan struct{})
	var initialWG sync.WaitGroup
	initialWG.Add(1)
	go func() {
		defer initialWG.Done()
		drainInitialMessages(clientConn, stopInitial)
	}()

	conn := newConnection(serverConn, session, sink, false)

	close(stopInitial)
	initialWG.Wait()
	_ = clientConn.SetReadDeadline(time.Time{})

	errCh := make(chan error, 1)
	go func() {
		errCh <- conn.serve()
	}()

	preSnapshot, err := sink.Snapshot()
	if err != nil {
		t.Fatalf("pre-snapshot failed: %v", err)
	}
	if len(preSnapshot.Panes) == 0 {
		t.Fatal("precondition failed: sink snapshot had no panes")
	}

	payload, err := protocol.EncodeResize(protocol.Resize{Cols: 40, Rows: 12})
	if err != nil {
		t.Fatalf("encode resize: %v", err)
	}
	header := protocol.Header{Version: protocol.Version, Type: protocol.MsgResize, Flags: protocol.FlagChecksum, SessionID: sessionID}
	if err := protocol.WriteMessage(clientConn, header, payload); err != nil {
		t.Fatalf("write resize: %v", err)
	}

	var snapshot protocol.TreeSnapshot
	deadline := time.Now().Add(2 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for tree snapshot after resize")
		}
		_ = clientConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		hdr, data, err := protocol.ReadMessage(clientConn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			t.Fatalf("read message: %v", err)
		}
		if hdr.Type != protocol.MsgTreeSnapshot {
			continue
		}
		snapshot, err = protocol.DecodeTreeSnapshot(data)
		if err != nil {
			t.Fatalf("decode snapshot: %v", err)
		}
		break
	}

	if len(snapshot.Panes) == 0 {
		t.Fatalf("expected panes in snapshot after resize")
	}
	if snapshot.Panes[0].Width != 40 {
		t.Fatalf("expected pane width 40, got %d", snapshot.Panes[0].Width)
	}
	if snapshot.Panes[0].Height != 12 {
		t.Fatalf("expected pane height 12, got %d", snapshot.Panes[0].Height)
	}

	capture := desktop.CaptureTree()
	if len(capture.Panes) == 0 {
		t.Fatalf("desktop capture missing panes")
	}
	if capture.Panes[0].Rect.Width != 40 || capture.Panes[0].Rect.Height != 12 {
		t.Fatalf("desktop dimensions not updated: got %dx%d", capture.Panes[0].Rect.Width, capture.Panes[0].Rect.Height)
	}

	clientConn.Close()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("serve returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("connection serve did not exit")
	}
}

func TestConnectionClipboardRoundTrip(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	sessionID := [16]byte{4}
	session := NewSession(sessionID, 64)

	sink, _, cleanup := newDesktopSink(t)
	defer cleanup()

	stopInitial := make(chan struct{})
	var initialWG sync.WaitGroup
	initialWG.Add(1)
	go func() {
		defer initialWG.Done()
		drainInitialMessages(clientConn, stopInitial)
	}()

	conn := newConnection(serverConn, session, sink, false)

	close(stopInitial)
	initialWG.Wait()
	_ = clientConn.SetReadDeadline(time.Time{})

	errCh := make(chan error, 1)
	go func() {
		errCh <- conn.serve()
	}()

	payload, err := protocol.EncodeClipboardSet(protocol.ClipboardSet{MimeType: "text/plain", Data: []byte("hello")})
	if err != nil {
		t.Fatalf("encode clipboard set: %v", err)
	}
	header := protocol.Header{Version: protocol.Version, Type: protocol.MsgClipboardSet, Flags: protocol.FlagChecksum, SessionID: sessionID}
	if err := protocol.WriteMessage(clientConn, header, payload); err != nil {
		t.Fatalf("write clipboard set: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for clipboard data response")
		}
		_ = clientConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		hdr, data, err := protocol.ReadMessage(clientConn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			t.Fatalf("read message: %v", err)
		}
		if hdr.Type != protocol.MsgClipboardData {
			continue
		}
		decoded, err := protocol.DecodeClipboardData(data)
		if err != nil {
			t.Fatalf("decode clipboard data: %v", err)
		}
		if decoded.MimeType != "text/plain" {
			t.Fatalf("unexpected mime type: %s", decoded.MimeType)
		}
		if string(decoded.Data) != "hello" {
			t.Fatalf("unexpected clipboard payload: %q", decoded.Data)
		}
		break
	}

	clientConn.Close()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("serve returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("connection serve did not exit")
	}
}

func TestConnectionResumeFlushesPendingDiffs(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	sessionID := [16]byte{5}
	session := NewSession(sessionID, 64)
	if err := session.EnqueueDiff(protocol.BufferDelta{PaneID: [16]byte{7}, Revision: 1}); err != nil {
		t.Fatalf("enqueue diff: %v", err)
	}

	sink, _, cleanup := newDesktopSink(t)
	defer cleanup()

	conn := newConnection(serverConn, session, sink, true)

	errCh := make(chan error, 1)
	go func() {
		errCh <- conn.serve()
	}()

	_ = clientConn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if _, _, err := protocol.ReadMessage(clientConn); err == nil {
		t.Fatal("expected no messages before resume")
	}

	payload, err := protocol.EncodeResumeRequest(protocol.ResumeRequest{SessionID: sessionID, LastSequence: 0})
	if err != nil {
		t.Fatalf("encode resume request: %v", err)
	}
	header := protocol.Header{Version: protocol.Version, Type: protocol.MsgResumeRequest, Flags: protocol.FlagChecksum, SessionID: sessionID}
	if err := protocol.WriteMessage(clientConn, header, payload); err != nil {
		t.Fatalf("write resume request: %v", err)
	}

	gotSnapshot := false
	gotDiff := false
	deadline := time.Now().Add(2 * time.Second)
	for !gotSnapshot || !gotDiff {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for resume data (snapshot=%v, diff=%v)", gotSnapshot, gotDiff)
		}
		_ = clientConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		hdr, _, err := protocol.ReadMessage(clientConn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			t.Fatalf("read message: %v", err)
		}
		switch hdr.Type {
		case protocol.MsgTreeSnapshot:
			gotSnapshot = true
		case protocol.MsgBufferDelta:
			gotDiff = true
			if hdr.Sequence == 0 {
				t.Fatal("buffer delta missing sequence after resume")
			}
		default:
			continue
		}
	}

	if conn.awaitResume {
		t.Fatal("connection still awaiting resume after processing request")
	}

	clientConn.Close()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("serve returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("connection serve did not exit")
	}
}

func TestConnectionAwaitReadErrorNil(t *testing.T) {
	session := NewSession([16]byte{6}, 0)
	conn := &connection{
		conn:     fakeNetConn{},
		session:  session,
		incoming: make(chan protocolMessage),
		readErr:  make(chan error, 1),
		pending:  make(chan struct{}, 1),
		stop:     make(chan struct{}),
		sink:     nopSink{},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- conn.serve()
	}()

	close(conn.incoming)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected nil error when no read error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve did not exit after incoming closed")
	}
}

func TestConnectionAwaitReadErrorReturnsError(t *testing.T) {
	session := NewSession([16]byte{7}, 0)
	conn := &connection{
		conn:     fakeNetConn{},
		session:  session,
		incoming: make(chan protocolMessage),
		readErr:  make(chan error, 1),
		pending:  make(chan struct{}, 1),
		stop:     make(chan struct{}),
		sink:     nopSink{},
	}

	conn.readErr <- io.ErrUnexpectedEOF
	if err := conn.awaitReadError(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected unexpected EOF, got %v", err)
	}

	conn.readErr <- io.ErrUnexpectedEOF
	close(conn.incoming)

	errCh := make(chan error, 1)
	go func() {
		errCh <- conn.serve()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("serve returned %v, want unexpected EOF", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve did not exit after read error")
	}
}

type fakeNetConn struct{}

func (fakeNetConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (fakeNetConn) Write(b []byte) (int, error)      { return len(b), nil }
func (fakeNetConn) Close() error                     { return nil }
func (fakeNetConn) LocalAddr() net.Addr              { return fakeAddr("local") }
func (fakeNetConn) RemoteAddr() net.Addr             { return fakeAddr("remote") }
func (fakeNetConn) SetDeadline(time.Time) error      { return nil }
func (fakeNetConn) SetReadDeadline(time.Time) error  { return nil }
func (fakeNetConn) SetWriteDeadline(time.Time) error { return nil }

type fakeAddr string

func (a fakeAddr) Network() string { return string(a) }
func (a fakeAddr) String() string  { return string(a) }

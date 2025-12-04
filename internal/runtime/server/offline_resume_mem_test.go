//go:build integration
// +build integration

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/offline_resume_mem_test.go
// Summary: Exercises offline resume mem behaviour to ensure the server runtime remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"io"
	"sync"
	"testing"

	"github.com/gdamore/tcell/v2"

	"texelation/internal/runtime/server/testutil"
	"texelation/protocol"
	"texelation/texel"
)

type offlineScreenDriver struct{}

func (offlineScreenDriver) Init() error                                    { return nil }
func (offlineScreenDriver) Fini()                                          {}
func (offlineScreenDriver) Size() (int, int)                               { return 80, 24 }
func (offlineScreenDriver) SetStyle(tcell.Style)                           {}
func (offlineScreenDriver) HideCursor()                                    {}
func (offlineScreenDriver) Show()                                          {}
func (offlineScreenDriver) PollEvent() tcell.Event                         { return nil }
func (offlineScreenDriver) SetContent(int, int, rune, []rune, tcell.Style) {}
func (offlineScreenDriver) GetContent(int, int) (rune, []rune, tcell.Style, int) {
	return ' ', nil, tcell.StyleDefault, 1
}

type offlineApp struct{ title string }

func (o *offlineApp) Run() error            { return nil }
func (o *offlineApp) Stop()                 {}
func (o *offlineApp) Resize(cols, rows int) {}
func (o *offlineApp) Render() [][]texel.Cell {
	return [][]texel.Cell{{{Ch: 'z', Style: tcell.StyleDefault}}}
}
func (o *offlineApp) GetTitle() string               { return o.title }
func (o *offlineApp) HandleKey(ev *tcell.EventKey)   {}
func (o *offlineApp) SetRefreshNotifier(chan<- bool) {}

func TestOfflineRetentionAndResumeWithMemConn(t *testing.T) {
	mgr := NewManager()
	mgr.SetDiffRetentionLimit(2)

	lifecycle := texel.NoopAppLifecycle{}
	app := &offlineApp{title: "welcome"}
	shellFactory := func() texel.App { return app }

	sink := NewDesktopSink(desktop)
	srv := &Server{manager: mgr, sink: sink, desktopSink: sink}

	var publisherMu sync.Mutex
	var publisher *DesktopPublisher

	session := initialHandshake(t, srv, sink, desktop, &publisherMu, &publisher)
	lastSeq := initialClientFlow(t, session)

	publisherMu.Lock()
	pub := publisher
	publisherMu.Unlock()
	if pub == nil {
		t.Fatalf("publisher not set after initial handshake")
	}

	for i := 0; i < 4; i++ {
		if err := pub.Publish(); err != nil {
			t.Fatalf("offline publish failed: %v", err)
		}
	}

	if pending := session.Pending(0); len(pending) != 2 {
		t.Fatalf("expected retention limit of 2 diffs, got %d", len(pending))
	}

	resumeClientFlow(t, srv, sink, desktop, session, lastSeq)

	stats := session.Stats()
	if stats.PendingCount != 0 {
		t.Fatalf("expected pending queue flushed, got %d", stats.PendingCount)
	}
	if stats.DroppedDiffs == 0 {
		t.Fatalf("expected drop stats recorded")
	}
}

func initialHandshake(t *testing.T, srv *Server, sink *DesktopSink, desktop *texel.DesktopEngine, publisherMu *sync.Mutex, publisher **DesktopPublisher) *Session {
	serverConn, clientConn := testutil.NewMemPipe(32)
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	sessCh := make(chan *Session, 1)
	errCh := make(chan error, 1)

	go func() {
		defer serverConn.Close()
		sess, resuming, err := handleHandshake(serverConn, srv.manager)
		if err != nil {
			errCh <- err
			return
		}
		pub := NewDesktopPublisher(desktop, sess)
		publisherMu.Lock()
		*publisher = pub
		publisherMu.Unlock()
		sink.SetPublisher(pub)
		_ = pub.Publish()
		srv.sendSnapshot(serverConn, sess)
		sessCh <- sess
		conn := newConnection(serverConn, sess, sink, resuming)
		errCh <- conn.serve()
	}()

	helloPayload, _ := protocol.EncodeHello(protocol.Hello{ClientName: "client"})
	if err := protocol.WriteMessage(clientConn, protocol.Header{Version: protocol.Version, Type: protocol.MsgHello, Flags: protocol.FlagChecksum}, helloPayload); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	if _, _, err := protocol.ReadMessage(clientConn); err != nil {
		t.Fatalf("read welcome: %v", err)
	}

	connectReq, _ := protocol.EncodeConnectRequest(protocol.ConnectRequest{})
	if err := protocol.WriteMessage(clientConn, protocol.Header{Version: protocol.Version, Type: protocol.MsgConnectRequest, Flags: protocol.FlagChecksum}, connectReq); err != nil {
		t.Fatalf("write connect: %v", err)
	}

	hdr, payload, err := readMessageSkippingFocus(clientConn)
	if err != nil {
		t.Fatalf("read connect accept: %v", err)
	}
	if hdr.Type != protocol.MsgConnectAccept {
		t.Fatalf("expected connect accept, got %v", hdr.Type)
	}
	if _, err := protocol.DecodeConnectAccept(payload); err != nil {
		t.Fatalf("decode connect accept: %v", err)
	}
	hdr, payload, err = readMessageSkippingFocus(clientConn)
	if err != nil {
		t.Fatalf("read initial snapshot: %v", err)
	}
	if hdr.Type != protocol.MsgTreeSnapshot {
		t.Fatalf("expected snapshot, got %v", hdr.Type)
	}
	if _, err := protocol.DecodeTreeSnapshot(payload); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	hdr, payload, err = readMessageSkippingFocus(clientConn)
	if err != nil {
		t.Fatalf("read initial delta: %v", err)
	}
	if hdr.Type != protocol.MsgBufferDelta {
		t.Fatalf("expected buffer delta, got %v", hdr.Type)
	}
	if _, err := protocol.DecodeBufferDelta(payload); err != nil {
		t.Fatalf("decode delta: %v", err)
	}

	_ = clientConn.Close()

	var serveErr error
	select {
	case serveErr = <-errCh:
	default:
	}
	if serveErr != nil && serveErr != io.EOF {
		t.Fatalf("connection serve err: %v", serveErr)
	}

	select {
	case sess := <-sessCh:
		return sess
	default:
		t.Fatalf("session handshake failed")
		return nil
	}
}

func initialClientFlow(t *testing.T, session *Session) uint64 {
	pending := session.Pending(0)
	if len(pending) == 0 {
		t.Fatalf("expected initial diff queued")
	}
	first := pending[len(pending)-1]
	session.Ack(first.Sequence)
	return first.Sequence
}

func resumeClientFlow(t *testing.T, srv *Server, sink *DesktopSink, desktop *texel.DesktopEngine, session *Session, lastSeq uint64) {
	serverConn, clientConn := testutil.NewMemPipe(32)
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	errCh := make(chan error, 1)

	go func() {
		defer serverConn.Close()
		sess, resuming, err := handleHandshake(serverConn, srv.manager)
		if err != nil {
			errCh <- err
			return
		}
		pub := NewDesktopPublisher(desktop, sess)
		sink.SetPublisher(pub)
		errCh <- newConnection(serverConn, sess, sink, resuming).serve()
	}()

	helloPayload, _ := protocol.EncodeHello(protocol.Hello{ClientName: "client"})
	if err := protocol.WriteMessage(clientConn, protocol.Header{Version: protocol.Version, Type: protocol.MsgHello, Flags: protocol.FlagChecksum}, helloPayload); err != nil {
		t.Fatalf("resume write hello: %v", err)
	}
	if _, _, err := readMessageSkippingFocus(clientConn); err != nil {
		t.Fatalf("resume read welcome: %v", err)
	}

	resumeConnect, _ := protocol.EncodeConnectRequest(protocol.ConnectRequest{SessionID: session.ID()})
	if err := protocol.WriteMessage(clientConn, protocol.Header{Version: protocol.Version, Type: protocol.MsgConnectRequest, Flags: protocol.FlagChecksum}, resumeConnect); err != nil {
		t.Fatalf("resume write connect: %v", err)
	}

	if _, _, err := readMessageSkippingFocus(clientConn); err != nil {
		t.Fatalf("resume read accept: %v", err)
	}

	resumePayload, _ := protocol.EncodeResumeRequest(protocol.ResumeRequest{SessionID: session.ID(), LastSequence: lastSeq})
	if err := protocol.WriteMessage(clientConn, protocol.Header{Version: protocol.Version, Type: protocol.MsgResumeRequest, Flags: protocol.FlagChecksum, SessionID: session.ID()}, resumePayload); err != nil {
		t.Fatalf("resume write request: %v", err)
	}

	snapshotReceived := false
	deltasReceived := 0
	expectedDeltas := 2 // Retention limit set at line 54

	for {
		// Check if we're done - received snapshot and all expected deltas
		if snapshotReceived && deltasReceived >= expectedDeltas {
			_ = clientConn.Close()
			break
		}

		hdr, payload, err := readMessageSkippingFocus(clientConn)
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("resume read message: %v", err)
		}
		switch hdr.Type {
		case protocol.MsgTreeSnapshot:
			if _, err := protocol.DecodeTreeSnapshot(payload); err != nil {
				t.Fatalf("resume decode snapshot: %v", err)
			}
			snapshotReceived = true
		case protocol.MsgBufferDelta:
			if _, err := protocol.DecodeBufferDelta(payload); err != nil {
				t.Fatalf("resume decode delta: %v", err)
			}
			deltasReceived++
			// Send ACK for this delta
			ackPayload, _ := protocol.EncodeBufferAck(protocol.BufferAck{Sequence: hdr.Sequence})
			if err := protocol.WriteMessage(clientConn, protocol.Header{
				Version:   protocol.Version,
				Type:      protocol.MsgBufferAck,
				Flags:     protocol.FlagChecksum,
				SessionID: session.ID(),
			}, ackPayload); err != nil {
				t.Fatalf("resume write ack: %v", err)
			}
		default:
			// Skip other message types (PaneFocus, StateUpdate, etc.)
			continue
		}
	}

	if err := <-errCh; err != nil && err != io.EOF {
		t.Fatalf("resume connection err: %v", err)
	}
	if pending := session.Pending(0); len(pending) > 0 {
		last := pending[len(pending)-1]
		session.Ack(last.Sequence)
	}
}

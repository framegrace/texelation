//go:build integration
// +build integration

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/server_desktop_integration_test.go
// Summary: Exercises server desktop integration behaviour to ensure the server runtime remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	texelcore "github.com/framegrace/texelui/core"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/protocol"
	"github.com/framegrace/texelation/texel"
)

type integrationScreenDriver struct{}

func (integrationScreenDriver) Init() error                                    { return nil }
func (integrationScreenDriver) Fini()                                          {}
func (integrationScreenDriver) Size() (int, int)                               { return 80, 24 }
func (integrationScreenDriver) SetStyle(tcell.Style)                           {}
func (integrationScreenDriver) HideCursor()                                    {}
func (integrationScreenDriver) Show()                                          {}
func (integrationScreenDriver) PollEvent() tcell.Event                         { return nil }
func (integrationScreenDriver) SetContent(int, int, rune, []rune, tcell.Style) {}
func (integrationScreenDriver) GetContent(int, int) (rune, []rune, tcell.Style, int) {
	return ' ', nil, tcell.StyleDefault, 1
}

type deterministicApp struct {
	title  string
	keyLog []rune
}

func (d *deterministicApp) Run() error            { return nil }
func (d *deterministicApp) Stop()                 {}
func (d *deterministicApp) Resize(cols, rows int) {}
func (d *deterministicApp) Render() [][]texelcore.Cell {
	return [][]texelcore.Cell{{{Ch: 'x', Style: tcell.StyleDefault}}}
}
func (d *deterministicApp) GetTitle() string               { return d.title }
func (d *deterministicApp) HandleKey(ev *tcell.EventKey)   { d.keyLog = append(d.keyLog, ev.Rune()) }
func (d *deterministicApp) SetRefreshNotifier(chan<- bool) {}

func TestServerDesktopIntegrationProducesDiffsAndHandlesKeys(t *testing.T) {
	mgr := NewManager()
	srvClient, srvConn := net.Pipe()
	defer srvClient.Close()

	driver := integrationScreenDriver{}
	lifecycle := texel.NoopAppLifecycle{}
	app := &deterministicApp{title: "welcome"}
	shellFactory := func() texelcore.App { return app }
	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, "", &lifecycle)
	if err != nil {
		t.Fatalf("failed to create desktop: %v", err)
	}
	defer desktop.Close()

	sink := NewDesktopSink(desktop)

	errCh := make(chan error, 1)
	sessionCh := make(chan *Session, 1)
	go func() {
		defer srvConn.Close()
		session, resuming, err := handleHandshake(srvConn, mgr)
		if err != nil {
			errCh <- err
			return
		}
		sessionCh <- session

		publisher := NewDesktopPublisher(desktop, session)
		sink.SetPublisher(publisher)
		snapshot, err := sink.Snapshot()
		if err == nil && len(snapshot.Panes) > 0 {
			if payload, err := protocol.EncodeTreeSnapshot(snapshot); err == nil {
				header := protocol.Header{Version: protocol.Version, Type: protocol.MsgTreeSnapshot, Flags: protocol.FlagChecksum, SessionID: session.ID()}
				_ = protocol.WriteMessage(srvConn, header, payload)
			}
		}
		_ = publisher.Publish()

		conn := newConnection(srvConn, session, sink, resuming)
		errCh <- conn.serve()
	}()

	helloPayload, err := protocol.EncodeHello(protocol.Hello{ClientName: "client"})
	if err != nil {
		t.Fatalf("encode hello: %v", err)
	}
	helloHeader := protocol.Header{Version: protocol.Version, Type: protocol.MsgHello, Flags: protocol.FlagChecksum}
	if err := protocol.WriteMessage(srvClient, helloHeader, helloPayload); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	if _, _, err := protocol.ReadMessage(srvClient); err != nil {
		t.Fatalf("read welcome: %v", err)
	}

	connectPayload, err := protocol.EncodeConnectRequest(protocol.ConnectRequest{})
	if err != nil {
		t.Fatalf("encode connect: %v", err)
	}
	connectHeader := protocol.Header{Version: protocol.Version, Type: protocol.MsgConnectRequest, Flags: protocol.FlagChecksum}
	if err := protocol.WriteMessage(srvClient, connectHeader, connectPayload); err != nil {
		t.Fatalf("write connect: %v", err)
	}

	hdr, payload, err := protocol.ReadMessage(srvClient)
	if err != nil {
		t.Fatalf("read connect accept: %v", err)
	}
	accept, err := protocol.DecodeConnectAccept(payload)
	if err != nil {
		t.Fatalf("decode connect accept: %v", err)
	}

	session := <-sessionCh

	hdr, payload, err = readMessageSkippingFocus(srvClient)
	if err != nil {
		t.Fatalf("read tree snapshot: %v", err)
	}
	if hdr.Type != protocol.MsgTreeSnapshot {
		t.Fatalf("expected tree snapshot, got %v", hdr.Type)
	}
	if _, err := protocol.DecodeTreeSnapshot(payload); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}

	hdr, payload, err = readMessageSkippingFocus(srvClient)
	if err != nil {
		t.Fatalf("read initial delta: %v", err)
	}
	if hdr.Type != protocol.MsgBufferDelta {
		t.Fatalf("expected buffer delta, got %v", hdr.Type)
	}

	ackPayload, _ := protocol.EncodeBufferAck(protocol.BufferAck{Sequence: hdr.Sequence})
	ackHeader := protocol.Header{Version: protocol.Version, Type: protocol.MsgBufferAck, Flags: protocol.FlagChecksum, SessionID: accept.SessionID}
	if err := protocol.WriteMessage(srvClient, ackHeader, ackPayload); err != nil {
		t.Fatalf("write ack: %v", err)
	}

	keyPayload, _ := protocol.EncodeKeyEvent(protocol.KeyEvent{KeyCode: uint32(tcell.KeyRune), RuneValue: 'z', Modifiers: 0})
	keyHeader := protocol.Header{Version: protocol.Version, Type: protocol.MsgKeyEvent, Flags: protocol.FlagChecksum, SessionID: accept.SessionID}
	if err := protocol.WriteMessage(srvClient, keyHeader, keyPayload); err != nil {
		t.Fatalf("write key event: %v", err)
	}

	// Give server time to process key event and drain any responses
	srvClient.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	for {
		_, _, err := readMessageSkippingFocus(srvClient)
		if err != nil {
			break // Timeout or EOF - done reading
		}
	}

	srvClient.Close()

	if err := <-errCh; err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("connection serve error: %v", err)
	}

	if len(app.keyLog) == 0 || app.keyLog[0] != 'z' {
		t.Fatalf("expected key 'z' to be recorded")
	}

	if len(session.Pending(0)) == 0 {
		t.Fatalf("expected buffered diffs after key input")
	}
}

//go:build integration
// +build integration

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/client_integration_test.go
// Summary: Exercises client integration behaviour to ensure the server runtime remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/protocol"
	"texelation/texel"
)

func TestClientResumeReceivesSnapshot(t *testing.T) {
	mgr := NewManager()

	driver := resumeScreenDriver{}
	lifecycle := &texel.NoopAppLifecycle{}
	app := &resumeApp{title: "welcome"}
	shellFactory := func() texel.App { return app }
	welcomeFactory := func() texel.App { return app }

	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}
	defer desktop.Close()

	sink := NewDesktopSink(desktop)

	firstClient, firstServer := net.Pipe()
	defer firstClient.Close()

	firstErr := make(chan error, 1)
	go func() {
		defer firstServer.Close()
		session, resuming, err := handleHandshake(firstServer, mgr)
		if err != nil {
			firstErr <- err
			return
		}
		publisher := NewDesktopPublisher(desktop, session)
		sink.SetPublisher(publisher)

		snapshot, err := sink.Snapshot()
		if err == nil && len(snapshot.Panes) > 0 {
			if payload, err := protocol.EncodeTreeSnapshot(snapshot); err == nil {
				header := protocol.Header{Version: protocol.Version, Type: protocol.MsgTreeSnapshot, Flags: protocol.FlagChecksum, SessionID: session.ID()}
				_ = protocol.WriteMessage(firstServer, header, payload)
			}
		}
		if err := publisher.Publish(); err != nil {
			firstErr <- err
			return
		}
		conn := newConnection(firstServer, session, sink, resuming)
		firstErr <- conn.serve()
	}()

	helloPayload, _ := protocol.EncodeHello(protocol.Hello{ClientName: "client"})
	if err := protocol.WriteMessage(firstClient, protocol.Header{Version: protocol.Version, Type: protocol.MsgHello, Flags: protocol.FlagChecksum}, helloPayload); err != nil {
		t.Fatalf("write hello failed: %v", err)
	}

	t.Log("client: waiting for welcome")
	hdr, payload, err := protocol.ReadMessage(firstClient)
	if err != nil {
		t.Fatalf("read welcome failed: %v", err)
	}
	if hdr.Type != protocol.MsgWelcome {
		t.Fatalf("expected welcome, got %v", hdr.Type)
	}

	connectPayload, _ := protocol.EncodeConnectRequest(protocol.ConnectRequest{})
	if err := protocol.WriteMessage(firstClient, protocol.Header{Version: protocol.Version, Type: protocol.MsgConnectRequest, Flags: protocol.FlagChecksum}, connectPayload); err != nil {
		t.Fatalf("write connect failed: %v", err)
	}

	t.Log("client: waiting for connect accept")
	hdr, payload, err = readMessageSkippingFocus(firstClient)
	if err != nil {
		t.Fatalf("read connect accept failed: %v", err)
	}
	if hdr.Type != protocol.MsgConnectAccept {
		t.Fatalf("expected connect accept, got %v", hdr.Type)
	}
	accept, err := protocol.DecodeConnectAccept(payload)
	if err != nil {
		t.Fatalf("decode connect accept failed: %v", err)
	}

	t.Log("client: waiting for snapshot")
	hdr, payload, err = readMessageSkippingFocus(firstClient)
	if err != nil {
		t.Fatalf("read snapshot failed: %v", err)
	}
	if hdr.Type != protocol.MsgTreeSnapshot {
		t.Fatalf("expected tree snapshot, got %v", hdr.Type)
	}
	if _, err := protocol.DecodeTreeSnapshot(payload); err != nil {
		t.Fatalf("decode tree snapshot failed: %v", err)
	}

	t.Log("client: waiting for buffer delta")
	hdr, payload, err = readMessageSkippingFocus(firstClient)
	if err != nil {
		t.Fatalf("read buffer delta failed: %v", err)
	}
	if hdr.Type != protocol.MsgBufferDelta {
		t.Fatalf("expected buffer delta, got %v", hdr.Type)
	}
	if _, err := protocol.DecodeBufferDelta(payload); err != nil {
		t.Fatalf("decode buffer delta failed: %v", err)
	}
	lastSequence := hdr.Sequence

	ackPayload, _ := protocol.EncodeBufferAck(protocol.BufferAck{Sequence: lastSequence})
	if err := protocol.WriteMessage(firstClient, protocol.Header{Version: protocol.Version, Type: protocol.MsgBufferAck, Flags: protocol.FlagChecksum, SessionID: accept.SessionID}, ackPayload); err != nil {
		t.Fatalf("write ack failed: %v", err)
	}

	t.Log("client: closing first session")
	firstClient.Close()
	<-firstErr

	resumeClient, resumeServer := net.Pipe()
	defer resumeClient.Close()

	resumeErr := make(chan error, 1)
	go func() {
		defer resumeServer.Close()
		session, resuming, err := handleHandshake(resumeServer, mgr)
		if err != nil {
			resumeErr <- err
			return
		}
		publisher := NewDesktopPublisher(desktop, session)
		sink.SetPublisher(publisher)
		conn := newConnection(resumeServer, session, sink, resuming)
		resumeErr <- conn.serve()
	}()

	t.Log("client: sending resume hello")
	if err := protocol.WriteMessage(resumeClient, protocol.Header{Version: protocol.Version, Type: protocol.MsgHello, Flags: protocol.FlagChecksum}, helloPayload); err != nil {
		t.Fatalf("write resume hello failed: %v", err)
	}

	t.Log("client: waiting for resume welcome")
	hdr, payload, err = readMessageSkippingFocus(resumeClient)
	if err != nil {
		t.Fatalf("read resume welcome failed: %v", err)
	}
	if hdr.Type != protocol.MsgWelcome {
		t.Fatalf("expected resume welcome, got %v", hdr.Type)
	}

	resumeConnectPayload, _ := protocol.EncodeConnectRequest(protocol.ConnectRequest{SessionID: accept.SessionID})
	if err := protocol.WriteMessage(resumeClient, protocol.Header{Version: protocol.Version, Type: protocol.MsgConnectRequest, Flags: protocol.FlagChecksum}, resumeConnectPayload); err != nil {
		t.Fatalf("write resume connect failed: %v", err)
	}

	t.Log("client: waiting for resume accept")
	if _, _, err = readMessageSkippingFocus(resumeClient); err != nil {
		t.Fatalf("read resume accept failed: %v", err)
	}

	resumePayload, _ := protocol.EncodeResumeRequest(protocol.ResumeRequest{SessionID: accept.SessionID, LastSequence: lastSequence})
	t.Log("client: sending resume request")
	if err := protocol.WriteMessage(resumeClient, protocol.Header{Version: protocol.Version, Type: protocol.MsgResumeRequest, Flags: protocol.FlagChecksum, SessionID: accept.SessionID}, resumePayload); err != nil {
		t.Fatalf("write resume request failed: %v", err)
	}

	t.Log("client: waiting for resume snapshot")
	hdr, payload, err = readMessageSkippingFocus(resumeClient)
	if err != nil {
		t.Fatalf("read resume snapshot failed: %v", err)
	}
	if hdr.Type != protocol.MsgTreeSnapshot {
		t.Fatalf("expected resume snapshot, got %v", hdr.Type)
	}
	if _, err := protocol.DecodeTreeSnapshot(payload); err != nil {
		t.Fatalf("decode resume snapshot failed: %v", err)
	}

	resumeClient.Close()
	select {
	case err := <-resumeErr:
		// Accept nil, EOF, and closed pipe errors as success - these are expected when client closes
		if err != nil && err != io.EOF && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("resume server error: %v", err)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("resume server did not finish")
	}
}

type resumeScreenDriver struct{}

func (resumeScreenDriver) Init() error                                    { return nil }
func (resumeScreenDriver) Fini()                                          {}
func (resumeScreenDriver) Size() (int, int)                               { return 80, 24 }
func (resumeScreenDriver) SetStyle(tcell.Style)                           {}
func (resumeScreenDriver) HideCursor()                                    {}
func (resumeScreenDriver) Show()                                          {}
func (resumeScreenDriver) PollEvent() tcell.Event                         { return nil }
func (resumeScreenDriver) SetContent(int, int, rune, []rune, tcell.Style) {}
func (resumeScreenDriver) GetContent(int, int) (rune, []rune, tcell.Style, int) {
	return ' ', nil, tcell.StyleDefault, 1
}

type resumeApp struct {
	title string
}

func (r *resumeApp) Run() error            { return nil }
func (r *resumeApp) Stop()                 {}
func (r *resumeApp) Resize(cols, rows int) {}
func (r *resumeApp) Render() [][]texel.Cell {
	return [][]texel.Cell{{{Ch: 'x', Style: tcell.StyleDefault}}}
}
func (r *resumeApp) GetTitle() string               { return r.title }
func (r *resumeApp) HandleKey(ev *tcell.EventKey)   {}
func (r *resumeApp) SetRefreshNotifier(chan<- bool) {}

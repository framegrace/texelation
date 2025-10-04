package server

import (
	"io"
	"testing"

	"github.com/gdamore/tcell/v2"

	"texelation/protocol"
	"texelation/server/testutil"
	"texelation/texel"
	"texelation/texel/theme"
)

type signalScreenDriver struct{}

func (signalScreenDriver) Init() error                                    { return nil }
func (signalScreenDriver) Fini()                                          {}
func (signalScreenDriver) Size() (int, int)                               { return 80, 24 }
func (signalScreenDriver) SetStyle(tcell.Style)                           {}
func (signalScreenDriver) HideCursor()                                    {}
func (signalScreenDriver) Show()                                          {}
func (signalScreenDriver) PollEvent() tcell.Event                         { return nil }
func (signalScreenDriver) SetContent(int, int, rune, []rune, tcell.Style) {}
func (signalScreenDriver) GetContent(int, int) (rune, []rune, tcell.Style, int) {
	return ' ', nil, tcell.StyleDefault, 1
}

type signalApp struct{ title string }

func (s *signalApp) Run() error            { return nil }
func (s *signalApp) Stop()                 {}
func (s *signalApp) Resize(cols, rows int) {}
func (s *signalApp) Render() [][]texel.Cell {
	return [][]texel.Cell{{{Ch: 'x', Style: tcell.StyleDefault}}}
}
func (s *signalApp) GetTitle() string               { return s.title }
func (s *signalApp) HandleKey(ev *tcell.EventKey)   {}
func (s *signalApp) SetRefreshNotifier(chan<- bool) {}

func TestClipboardAndThemeRoundTrip(t *testing.T) {
	mgr := NewManager()
	lifecycle := texel.NoopAppLifecycle{}
	app := &signalApp{title: "welcome"}
	shellFactory := func() texel.App { return app }
	welcomeFactory := func() texel.App { return app }

	desktop, err := texel.NewDesktopWithDriver(signalScreenDriver{}, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	sink := NewDesktopSink(desktop)

	serverConn, clientConn := testutil.NewMemPipe(32)
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	errCh := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		sess, resuming, err := handleHandshake(serverConn, mgr)
		if err != nil {
			errCh <- err
			return
		}
		pub := NewDesktopPublisher(desktop, sess)
		sink.SetPublisher(pub)
		_ = pub.Publish()
		srv := &Server{manager: mgr, sink: sink, desktopSink: sink}
		srv.sendSnapshot(serverConn, sess)
		errCh <- newConnection(serverConn, sess, sink, resuming).serve()
	}()

	// initial handshake
	helloPayload, _ := protocol.EncodeHello(protocol.Hello{ClientName: "client"})
	if err := protocol.WriteMessage(clientConn, protocol.Header{Version: protocol.Version, Type: protocol.MsgHello, Flags: protocol.FlagChecksum}, helloPayload); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	if _, _, err := protocol.ReadMessage(clientConn); err != nil {
		t.Fatalf("read welcome: %v", err)
	}

	connectPayload, _ := protocol.EncodeConnectRequest(protocol.ConnectRequest{})
	if err := protocol.WriteMessage(clientConn, protocol.Header{Version: protocol.Version, Type: protocol.MsgConnectRequest, Flags: protocol.FlagChecksum}, connectPayload); err != nil {
		t.Fatalf("write connect: %v", err)
	}

	hdr, payload, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatalf("read connect accept: %v", err)
	}
	if hdr.Type != protocol.MsgConnectAccept {
		t.Fatalf("expected connect accept, got %v", hdr.Type)
	}
	accept, err := protocol.DecodeConnectAccept(payload)
	if err != nil {
		t.Fatalf("decode connect accept: %v", err)
	}
	sessionID := accept.SessionID
	if _, _, err := readMessageSkippingFocus(clientConn); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	hdr, payload, err = readMessageSkippingFocus(clientConn)
	if err != nil {
		t.Fatalf("read delta: %v", err)
	}
	if hdr.Type != protocol.MsgBufferDelta {
		t.Fatalf("expected buffer delta, got %v", hdr.Type)
	}
	ackPayload, _ := protocol.EncodeBufferAck(protocol.BufferAck{Sequence: hdr.Sequence})
	if err := protocol.WriteMessage(clientConn, protocol.Header{Version: protocol.Version, Type: protocol.MsgBufferAck, Flags: protocol.FlagChecksum, SessionID: sessionID}, ackPayload); err != nil {
		t.Fatalf("write ack: %v", err)
	}

	// Store clipboard so subsequent set/get have data
	desktop.HandleClipboardSet("text/plain", []byte("initial"))

	// Clipboard set should trigger clipboard data response
	clipSet := protocol.ClipboardSet{MimeType: "text/plain", Data: []byte("client-data")}
	payloadSet, _ := protocol.EncodeClipboardSet(clipSet)
	if err := protocol.WriteMessage(clientConn, protocol.Header{Version: protocol.Version, Type: protocol.MsgClipboardSet, Flags: protocol.FlagChecksum, SessionID: sessionID}, payloadSet); err != nil {
		t.Fatalf("write clipboard set: %v", err)
	}

	hdr, payload, err = protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatalf("read clipboard data: %v", err)
	}
	if hdr.Type != protocol.MsgClipboardData {
		t.Fatalf("expected clipboard data, got %v", hdr.Type)
	}
	dataMsg, err := protocol.DecodeClipboardData(payload)
	if err != nil {
		t.Fatalf("decode clipboard data: %v", err)
	}
	if dataMsg.MimeType != clipSet.MimeType || string(dataMsg.Data) != "client-data" {
		t.Fatalf("unexpected clipboard data: %+v", dataMsg)
	}

	// Clipboard get should return stored value
	clipGet := protocol.ClipboardGet{MimeType: "text/plain"}
	payloadGet, _ := protocol.EncodeClipboardGet(clipGet)
	if err := protocol.WriteMessage(clientConn, protocol.Header{Version: protocol.Version, Type: protocol.MsgClipboardGet, Flags: protocol.FlagChecksum, SessionID: sessionID}, payloadGet); err != nil {
		t.Fatalf("write clipboard get: %v", err)
	}

	hdr, payload, err = protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatalf("read clipboard response: %v", err)
	}
	if hdr.Type != protocol.MsgClipboardData {
		t.Fatalf("expected clipboard data, got %v", hdr.Type)
	}
	dataMsg, err = protocol.DecodeClipboardData(payload)
	if err != nil {
		t.Fatalf("decode clipboard data: %v", err)
	}
	if string(dataMsg.Data) != "client-data" {
		t.Fatalf("unexpected clipboard content: %q", dataMsg.Data)
	}

	// Theme update should produce ack and update theme config
	cfg := theme.Get()
	origSection := cfg["pane"]
	origValue, hadValue := origSection["fg"]
	hadSection := origSection != nil
	t.Cleanup(func() {
		if !hadSection {
			delete(cfg, "pane")
			return
		}
		if hadValue {
			cfg["pane"]["fg"] = origValue
		} else {
			delete(cfg["pane"], "fg")
		}
	})

	themeUpdate := protocol.ThemeUpdate{Section: "pane", Key: "fg", Value: "#123456"}
	payloadTheme, _ := protocol.EncodeThemeUpdate(themeUpdate)
	if err := protocol.WriteMessage(clientConn, protocol.Header{Version: protocol.Version, Type: protocol.MsgThemeUpdate, Flags: protocol.FlagChecksum, SessionID: sessionID}, payloadTheme); err != nil {
		t.Fatalf("write theme update: %v", err)
	}

	hdr, payload, err = protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatalf("read theme ack: %v", err)
	}
	if hdr.Type != protocol.MsgThemeAck {
		t.Fatalf("expected theme ack, got %v", hdr.Type)
	}
	ackMsg, err := protocol.DecodeThemeAck(payload)
	if err != nil {
		t.Fatalf("decode theme ack: %v", err)
	}
	if ackMsg.Value != themeUpdate.Value {
		t.Fatalf("unexpected theme ack: %+v", ackMsg)
	}

	if theme.Get()[themeUpdate.Section][themeUpdate.Key] != themeUpdate.Value {
		t.Fatalf("theme not applied")
	}

	_ = clientConn.Close()
	err = <-errCh
	if err != nil && err != io.EOF {
		t.Fatalf("connection serve error: %v", err)
	}
}

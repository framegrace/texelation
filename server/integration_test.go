package server

import (
	"net"
	"testing"
	"time"

	"texelation/protocol"
)

type recordingSink struct {
	events []protocol.KeyEvent
}

func (r *recordingSink) HandleKeyEvent(session *Session, event protocol.KeyEvent) {
	r.events = append(r.events, event)
}

func (r *recordingSink) HandleMouseEvent(session *Session, event protocol.MouseEvent) {}

func (r *recordingSink) HandleClipboardSet(session *Session, event protocol.ClipboardSet) {}

func (r *recordingSink) HandleClipboardGet(session *Session, event protocol.ClipboardGet) []byte {
	return nil
}

func (r *recordingSink) HandleThemeUpdate(session *Session, event protocol.ThemeUpdate) {}

func (r *recordingSink) HandlePaneFocus(session *Session, focus protocol.PaneFocus) {}

func TestConnectionSendsDiffProcessesAckAndKeyEvents(t *testing.T) {
	mgr := NewManager()
	client, srv := net.Pipe()
	defer client.Close()

	sink := &recordingSink{}
	errCh := make(chan error, 1)
	go func() {
		defer srv.Close()
		session, resuming, err := handleHandshake(srv, mgr)
		if err != nil {
			errCh <- err
			return
		}

		delta := protocol.BufferDelta{
			PaneID:   session.ID(),
			Revision: 1,
			Styles:   []protocol.StyleEntry{{AttrFlags: 0, FgModel: protocol.ColorModelDefault, FgValue: 0, BgModel: protocol.ColorModelDefault, BgValue: 0}},
			Rows:     []protocol.RowDelta{{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "hi", StyleIndex: 0}}}},
		}
		if err := session.EnqueueDiff(delta); err != nil {
			errCh <- err
			return
		}

		conn := newConnection(srv, session, sink, resuming)
		errCh <- conn.serve()
	}()

	helloPayload, err := protocol.EncodeHello(protocol.Hello{ClientName: "client"})
	if err != nil {
		t.Fatalf("encode hello: %v", err)
	}
	helloHeader := protocol.Header{Version: protocol.Version, Type: protocol.MsgHello, Flags: protocol.FlagChecksum}
	if err := protocol.WriteMessage(client, helloHeader, helloPayload); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	hdr, payload, err := protocol.ReadMessage(client)
	if err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if hdr.Type != protocol.MsgWelcome {
		t.Fatalf("expected welcome")
	}
	if _, err := protocol.DecodeWelcome(payload); err != nil {
		t.Fatalf("decode welcome: %v", err)
	}

	connectPayload, err := protocol.EncodeConnectRequest(protocol.ConnectRequest{})
	if err != nil {
		t.Fatalf("encode connect: %v", err)
	}
	connectHeader := protocol.Header{Version: protocol.Version, Type: protocol.MsgConnectRequest, Flags: protocol.FlagChecksum}
	if err := protocol.WriteMessage(client, connectHeader, connectPayload); err != nil {
		t.Fatalf("write connect: %v", err)
	}

	hdr, payload, err = protocol.ReadMessage(client)
	if err != nil {
		t.Fatalf("read connect accept: %v", err)
	}
	if hdr.Type != protocol.MsgConnectAccept {
		t.Fatalf("expected connect accept")
	}
	accept, err := protocol.DecodeConnectAccept(payload)
	if err != nil {
		t.Fatalf("decode connect accept: %v", err)
	}

	hdr, payload, err = readMessageSkippingFocus(client)
	if err != nil {
		t.Fatalf("read buffer delta: %v", err)
	}
	if hdr.Type != protocol.MsgBufferDelta {
		t.Fatalf("expected buffer delta, got %v", hdr.Type)
	}
	if _, err := protocol.DecodeBufferDelta(payload); err != nil {
		t.Fatalf("decode buffer delta: %v", err)
	}

	ackPayload, err := protocol.EncodeBufferAck(protocol.BufferAck{Sequence: hdr.Sequence})
	if err != nil {
		t.Fatalf("encode ack: %v", err)
	}
	ackHeader := protocol.Header{Version: protocol.Version, Type: protocol.MsgBufferAck, Flags: protocol.FlagChecksum, SessionID: accept.SessionID}
	if err := protocol.WriteMessage(client, ackHeader, ackPayload); err != nil {
		t.Fatalf("write ack: %v", err)
	}

	keyPayload, err := protocol.EncodeKeyEvent(protocol.KeyEvent{KeyCode: 13, RuneValue: '\n', Modifiers: 1})
	if err != nil {
		t.Fatalf("encode key: %v", err)
	}
	keyHeader := protocol.Header{Version: protocol.Version, Type: protocol.MsgKeyEvent, Flags: protocol.FlagChecksum, SessionID: accept.SessionID}
	if err := protocol.WriteMessage(client, keyHeader, keyPayload); err != nil {
		t.Fatalf("write key: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	client.Close()

	if err := <-errCh; err != nil && err != net.ErrClosed {
		t.Fatalf("connection serve returned error: %v", err)
	}

	session, err := mgr.Lookup(accept.SessionID)
	if err != nil {
		t.Fatalf("lookup session: %v", err)
	}
	if pending := session.Pending(0); len(pending) != 0 {
		t.Fatalf("expected pending diffs to be cleared, got %d", len(pending))
	}

	if len(sink.events) != 1 {
		t.Fatalf("expected 1 key event, got %d", len(sink.events))
	}
	if sink.events[0].KeyCode != 13 {
		t.Fatalf("unexpected key event %+v", sink.events[0])
	}
}

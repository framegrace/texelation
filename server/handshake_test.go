package server

import (
	"net"
	"testing"

	"texelation/protocol"
)

func TestHandleHandshakeCreatesSession(t *testing.T) {
	mgr := NewManager()
	client, srv := net.Pipe()
	defer client.Close()

	done := make(chan *Session, 1)
	go func() {
		defer srv.Close()
		session, err := handleHandshake(srv, mgr)
		if err != nil {
			t.Errorf("handshake failed: %v", err)
			done <- nil
			return
		}
		done <- session
	}()

	helloPayload, err := protocol.EncodeHello(protocol.Hello{ClientName: "test-client"})
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
		t.Fatalf("expected welcome, got %v", hdr.Type)
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
		t.Fatalf("expected connect accept, got %v", hdr.Type)
	}
	accept, err := protocol.DecodeConnectAccept(payload)
	if err != nil {
		t.Fatalf("decode connect accept: %v", err)
	}
	zero := [16]byte{}
	if accept.SessionID == zero {
		t.Fatalf("expected non-zero session id")
	}

	session := <-done
	if session == nil {
		t.Fatalf("handshake goroutine failed")
	}
	if mgr.ActiveSessions() != 1 {
		t.Fatalf("expected 1 active session, got %d", mgr.ActiveSessions())
	}
}

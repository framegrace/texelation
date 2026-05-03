// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package boot

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/framegrace/texelation/protocol"
)

// TestSocketPickerClient_ListSessions wires a fake server-side
// listener that speaks just enough protocol to validate the picker
// client's round-trip.
func TestSocketPickerClient_ListSessions(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		hdr, _, err := protocol.ReadMessage(conn)
		if err != nil || hdr.Type != protocol.MsgHello {
			return
		}
		welBody, _ := protocol.EncodeWelcome(protocol.Welcome{SessionID: [16]byte{0xAA}, ServerName: "test"})
		welHdr := protocol.Header{Version: protocol.Version, Type: protocol.MsgWelcome, Flags: protocol.FlagChecksum, SessionID: [16]byte{0xAA}}
		_ = protocol.WriteMessage(conn, welHdr, welBody)

		_, _, _ = protocol.ReadMessage(conn)
		respBody, _ := protocol.EncodeListSessionsResponse(protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{{SessionID: [16]byte{0xBB}, Label: "test", LastActive: 1, PaneCount: 1}},
		})
		respHdr := protocol.Header{Version: protocol.Version, Type: protocol.MsgListSessionsResponse, Flags: protocol.FlagChecksum, SessionID: [16]byte{0xAA}}
		_ = protocol.WriteMessage(conn, respHdr, respBody)
	}()

	time.Sleep(20 * time.Millisecond)

	c, err := NewSocketPickerClient(socket)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer c.Close()

	resp, err := c.ListSessions()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Stored) != 1 || resp.Stored[0].Label != "test" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

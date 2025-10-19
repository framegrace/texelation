package server

import (
	"net"

	"texelation/protocol"
)

func readMessageSkippingFocus(conn net.Conn) (protocol.Header, []byte, error) {
	for {
		hdr, payload, err := protocol.ReadMessage(conn)
		if err != nil {
			return hdr, payload, err
		}
		if hdr.Type == protocol.MsgPaneFocus || hdr.Type == protocol.MsgStateUpdate || hdr.Type == protocol.MsgPaneState {
			continue
		}
		return hdr, payload, nil
	}
}

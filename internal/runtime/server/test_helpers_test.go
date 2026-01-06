// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/test_helpers_test.go
// Summary: Exercises test helpers behaviour to ensure the server runtime remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"net"

	"github.com/framegrace/texelation/protocol"
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

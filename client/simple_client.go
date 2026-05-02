// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: client/simple_client.go
// Summary: Implements simple client capabilities for the client runtime support library.
// Usage: Imported by the remote renderer to manage simple client during live sessions.
// Notes: Shared across multiple client binaries and tests.

package client

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/framegrace/texelation/protocol"
)

// SimpleClient handles connection to the simple texel server for tree persistence
type SimpleClient struct {
	socketPath string
}

// NewSimpleClient creates a new simple client
func NewSimpleClient(socketPath string) *SimpleClient {
	return &SimpleClient{
		socketPath: socketPath,
	}
}

// Connect performs the protocol handshake. If sessionID is nil or zeroed, the
// server will allocate a fresh session.
func (c *SimpleClient) Connect(sessionID *[16]byte) (*protocol.ConnectAccept, net.Conn, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("dial failed: %w", err)
	}

	helloPayload, err := protocol.EncodeHello(protocol.Hello{ClientName: "simple-client"})
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if err := protocol.WriteMessage(conn, protocol.Header{Version: protocol.Version, Type: protocol.MsgHello, Flags: protocol.FlagChecksum}, helloPayload); err != nil {
		conn.Close()
		return nil, nil, err
	}

	hdr, payload, err := protocol.ReadMessage(conn)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if hdr.Type != protocol.MsgWelcome {
		conn.Close()
		return nil, nil, fmt.Errorf("unexpected message %v", hdr.Type)
	}

	var req protocol.ConnectRequest
	if sessionID != nil {
		req.SessionID = *sessionID
	}
	connectPayload, err := protocol.EncodeConnectRequest(req)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if err := protocol.WriteMessage(conn, protocol.Header{Version: protocol.Version, Type: protocol.MsgConnectRequest, Flags: protocol.FlagChecksum}, connectPayload); err != nil {
		conn.Close()
		return nil, nil, err
	}

	hdr, payload, err = protocol.ReadMessage(conn)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if hdr.Type != protocol.MsgConnectAccept {
		conn.Close()
		return nil, nil, fmt.Errorf("unexpected message %v", hdr.Type)
	}

	accept, err := protocol.DecodeConnectAccept(payload)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if sessionID != nil {
		*sessionID = accept.SessionID
	}

	return &accept, conn, nil
}

// RequestResume sends a RESUME_REQUEST and returns the server response
// header/payload. paneViewports carries per-pane scrollback state so the
// server can re-seat each pane's ViewWindow at the client's saved position
// (#199 Plan B). Pass nil or an empty slice for fresh-connect semantics.
//
// onProgress, when non-nil, is invoked synchronously for each
// MsgBootProgress the server emits between the request and the
// response. Server-side resume processing can take many seconds when
// it reloads scrollback from the WAL; without a callback the client
// would block silently the whole time. nil-safe — passing nil
// preserves the legacy "block until response" behaviour.
func (c *SimpleClient) RequestResume(conn net.Conn, sessionID [16]byte, sequence uint64, paneViewports []protocol.PaneViewportState, onProgress func(string)) (protocol.Header, []byte, error) {
	req := protocol.ResumeRequest{SessionID: sessionID, LastSequence: sequence, PaneViewports: paneViewports}
	payload, err := protocol.EncodeResumeRequest(req)
	if err != nil {
		return protocol.Header{}, nil, err
	}
	if err := protocol.WriteMessage(conn, protocol.Header{Version: protocol.Version, Type: protocol.MsgResumeRequest, Flags: protocol.FlagChecksum, SessionID: sessionID}, payload); err != nil {
		return protocol.Header{}, nil, err
	}
	for {
		hdr, body, err := protocol.ReadMessage(conn)
		if err != nil {
			return hdr, body, err
		}
		if hdr.Type != protocol.MsgBootProgress {
			return hdr, body, nil
		}
		bp, decErr := protocol.DecodeBootProgress(body)
		if decErr != nil {
			// Decode failure after the type byte already matched
			// MsgBootProgress signals wire-format drift (server/
			// client version skew, transport corruption). Skip the
			// frame so a cosmetic message doesn't tank an otherwise-
			// healthy resume, but log the breadcrumb — without it,
			// "splash never updates" has no traceable cause.
			log.Printf("simple-client: malformed MsgBootProgress payload: %v", decErr)
			continue
		}
		if onProgress != nil {
			onProgress(bp.Message)
		}
	}
}

// SaveTree saves the tree data to the server
func (c *SimpleClient) SaveTree(sessionID string, treeData interface{}) error {
	// existing code omitted for brevity
	return fmt.Errorf("deprecated; use Connect + protocol writers")
}

// RestoreTree restores tree data from the server
func (c *SimpleClient) RestoreTree(sessionID string, treeData interface{}) error {
	return fmt.Errorf("deprecated; use Connect + protocol readers")
}

// FormatUUID returns the session ID as a human readable string.
func FormatUUID(id [16]byte) string {
	var buf bytes.Buffer
	for i, b := range id {
		buf.WriteString(hex.EncodeToString([]byte{b}))
		switch i {
		case 3, 5, 7, 9:
			buf.WriteByte('-')
		}
	}
	return buf.String()
}

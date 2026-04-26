// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/handshake.go
// Summary: Implements handshake capabilities for the server runtime.
// Usage: Used by texel-server to coordinate handshake when hosting apps and sessions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"bytes"
	"errors"
	"io"

	"github.com/framegrace/texelation/protocol"
)

var (
	errUnexpectedMessage = errors.New("server: unexpected message type")
)

// handleHandshake performs the initial client/server negotiation.
//
// Returns:
//   - session:    the live or rehydrated Session for this connection.
//   - resuming:   true when the client supplied a non-zero SessionID
//     (regardless of whether it matched a live or persisted
//     entry). Used to gate the connection's awaitResume mode.
//   - rehydrated: true when the session was reconstructed from disk (a
//     daemon-restart resume). False when a live cache hit
//     returned an existing session, or when a fresh session
//     was created. Plan D2 callers use this to decide
//     whether to clear stale per-connection state (e.g.
//     c.lastAcked from the prior daemon's lifetime).
func handleHandshake(rw io.ReadWriter, mgr *Manager) (*Session, bool, bool, error) {
	hdr, payload, err := protocol.ReadMessage(rw)
	if err != nil {
		return nil, false, false, err
	}
	if hdr.Type != protocol.MsgHello {
		return nil, false, false, errUnexpectedMessage
	}
	if _, err := protocol.DecodeHello(payload); err != nil {
		return nil, false, false, err
	}

	welcomePayload, err := protocol.EncodeWelcome(protocol.Welcome{ServerName: "texelation-server"})
	if err != nil {
		return nil, false, false, err
	}
	welcomeHeader := protocol.Header{
		Version: protocol.Version,
		Type:    protocol.MsgWelcome,
		Flags:   protocol.FlagChecksum,
	}
	if err := protocol.WriteMessage(rw, welcomeHeader, welcomePayload); err != nil {
		return nil, false, false, err
	}

	hdr, payload, err = protocol.ReadMessage(rw)
	if err != nil {
		return nil, false, false, err
	}
	if hdr.Type != protocol.MsgConnectRequest {
		return nil, false, false, errUnexpectedMessage
	}
	connectReq, err := protocol.DecodeConnectRequest(payload)
	if err != nil {
		return nil, false, false, err
	}

	var session *Session
	var rehydrated bool
	zeroID := [16]byte{}
	resuming := !bytes.Equal(connectReq.SessionID[:], zeroID[:])
	if bytes.Equal(connectReq.SessionID[:], zeroID[:]) {
		session, err = mgr.NewSession()
		if err != nil {
			return nil, false, false, err
		}
	} else {
		session, rehydrated, err = mgr.LookupOrRehydrate(connectReq.SessionID)
		if err != nil {
			return nil, false, false, err
		}
	}

	connectPayload, err := protocol.EncodeConnectAccept(protocol.ConnectAccept{SessionID: session.ID(), ResumeSupported: true})
	if err != nil {
		return nil, false, false, err
	}

	connectHeader := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgConnectAccept,
		Flags:     protocol.FlagChecksum,
		SessionID: session.ID(),
		Sequence:  1,
	}
	if err := protocol.WriteMessage(rw, connectHeader, connectPayload); err != nil {
		return nil, false, false, err
	}

	return session, resuming, rehydrated, nil
}

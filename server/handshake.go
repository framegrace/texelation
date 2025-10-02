package server

import (
	"bytes"
	"errors"
	"io"

	"texelation/protocol"
)

var (
	errUnexpectedMessage = errors.New("server: unexpected message type")
)

// handleHandshake performs the initial client/server negotiation.
func handleHandshake(rw io.ReadWriter, mgr *Manager) (*Session, error) {
	hdr, payload, err := protocol.ReadMessage(rw)
	if err != nil {
		return nil, err
	}
	if hdr.Type != protocol.MsgHello {
		return nil, errUnexpectedMessage
	}
	if _, err := protocol.DecodeHello(payload); err != nil {
		return nil, err
	}

	welcomePayload, err := protocol.EncodeWelcome(protocol.Welcome{ServerName: "texelation-server"})
	if err != nil {
		return nil, err
	}
	welcomeHeader := protocol.Header{
		Version: protocol.Version,
		Type:    protocol.MsgWelcome,
		Flags:   protocol.FlagChecksum,
	}
	if err := protocol.WriteMessage(rw, welcomeHeader, welcomePayload); err != nil {
		return nil, err
	}

	hdr, payload, err = protocol.ReadMessage(rw)
	if err != nil {
		return nil, err
	}
	if hdr.Type != protocol.MsgConnectRequest {
		return nil, errUnexpectedMessage
	}
	connectReq, err := protocol.DecodeConnectRequest(payload)
	if err != nil {
		return nil, err
	}

	var session *Session
	zeroID := [16]byte{}
	if bytes.Equal(connectReq.SessionID[:], zeroID[:]) {
		session, err = mgr.NewSession()
		if err != nil {
			return nil, err
		}
	} else {
		session, err = mgr.Lookup(connectReq.SessionID)
		if err != nil {
			return nil, err
		}
	}

	connectPayload, err := protocol.EncodeConnectAccept(protocol.ConnectAccept{SessionID: session.ID(), ResumeSupported: true})
	if err != nil {
		return nil, err
	}

	connectHeader := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgConnectAccept,
		Flags:     protocol.FlagChecksum,
		SessionID: session.ID(),
		Sequence:  1,
	}
	if err := protocol.WriteMessage(rw, connectHeader, connectPayload); err != nil {
		return nil, err
	}

	return session, nil
}

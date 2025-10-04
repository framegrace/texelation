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
func handleHandshake(rw io.ReadWriter, mgr *Manager) (*Session, bool, error) {
	hdr, payload, err := protocol.ReadMessage(rw)
	if err != nil {
		return nil, false, err
	}
	if hdr.Type != protocol.MsgHello {
		return nil, false, errUnexpectedMessage
	}
	if _, err := protocol.DecodeHello(payload); err != nil {
		return nil, false, err
	}

	welcomePayload, err := protocol.EncodeWelcome(protocol.Welcome{ServerName: "texelation-server"})
	if err != nil {
		return nil, false, err
	}
	welcomeHeader := protocol.Header{
		Version: protocol.Version,
		Type:    protocol.MsgWelcome,
		Flags:   protocol.FlagChecksum,
	}
	if err := protocol.WriteMessage(rw, welcomeHeader, welcomePayload); err != nil {
		return nil, false, err
	}

	hdr, payload, err = protocol.ReadMessage(rw)
	if err != nil {
		return nil, false, err
	}
	if hdr.Type != protocol.MsgConnectRequest {
		return nil, false, errUnexpectedMessage
	}
	connectReq, err := protocol.DecodeConnectRequest(payload)
	if err != nil {
		return nil, false, err
	}

	var session *Session
	zeroID := [16]byte{}
	resuming := !bytes.Equal(connectReq.SessionID[:], zeroID[:])
	if bytes.Equal(connectReq.SessionID[:], zeroID[:]) {
		session, err = mgr.NewSession()
		if err != nil {
			return nil, false, err
		}
	} else {
		session, err = mgr.Lookup(connectReq.SessionID)
		if err != nil {
			return nil, false, err
		}
	}

	connectPayload, err := protocol.EncodeConnectAccept(protocol.ConnectAccept{SessionID: session.ID(), ResumeSupported: true})
	if err != nil {
		return nil, false, err
	}

	connectHeader := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgConnectAccept,
		Flags:     protocol.FlagChecksum,
		SessionID: session.ID(),
		Sequence:  1,
	}
	if err := protocol.WriteMessage(rw, connectHeader, connectPayload); err != nil {
		return nil, false, err
	}

	return session, resuming, nil
}

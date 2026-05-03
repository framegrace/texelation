// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/boot/picker_client.go
// Summary: Socket-backed PickerClient + ProbeStoredSessions. Performs
// the protocol Hello/Welcome handshake and round-trips picker
// messages over a unix socket. Modelled on client/simple_client.go's
// dial-and-handshake helper.

package boot

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/framegrace/texelation/protocol"
)

// dialAndHandshake opens a unix socket connection to the texelation
// daemon and runs the full Hello/Welcome/ConnectRequest/ConnectAccept
// handshake. Returns the conn + the assigned sessionID. The caller
// closes the conn.
//
// We complete the full handshake (not just Hello/Welcome) because the
// server's handleHandshake expects MsgConnectRequest after Welcome —
// anything else closes the connection with errUnexpectedMessage.
func dialAndHandshake(socket string) (net.Conn, [16]byte, error) {
	var sid [16]byte
	conn, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		return nil, sid, fmt.Errorf("dial %s: %w", socket, err)
	}
	var clientID [16]byte
	if _, err := rand.Read(clientID[:]); err != nil {
		conn.Close()
		return nil, sid, fmt.Errorf("rand client id: %w", err)
	}
	helloBody, err := protocol.EncodeHello(protocol.Hello{ClientID: clientID, ClientName: "texelation-picker"})
	if err != nil {
		conn.Close()
		return nil, sid, fmt.Errorf("encode hello: %w", err)
	}
	helloHdr := protocol.Header{
		Version: protocol.Version,
		Type:    protocol.MsgHello,
		Flags:   protocol.FlagChecksum,
	}
	if err := protocol.WriteMessage(conn, helloHdr, helloBody); err != nil {
		conn.Close()
		return nil, sid, fmt.Errorf("write hello: %w", err)
	}
	hdr, _, err := protocol.ReadMessage(conn)
	if err != nil {
		conn.Close()
		return nil, sid, fmt.Errorf("read welcome: %w", err)
	}
	if hdr.Type != protocol.MsgWelcome {
		conn.Close()
		return nil, sid, fmt.Errorf("expected welcome, got message type %d", hdr.Type)
	}

	// Send ConnectRequest with zero SessionID — the server creates a
	// fresh ephemeral session for this picker connection. We never
	// resume; the picker's role is to LIST sessions, not attach to one.
	connectBody, err := protocol.EncodeConnectRequest(protocol.ConnectRequest{})
	if err != nil {
		conn.Close()
		return nil, sid, fmt.Errorf("encode connect: %w", err)
	}
	connectHdr := protocol.Header{
		Version: protocol.Version,
		Type:    protocol.MsgConnectRequest,
		Flags:   protocol.FlagChecksum,
	}
	if err := protocol.WriteMessage(conn, connectHdr, connectBody); err != nil {
		conn.Close()
		return nil, sid, fmt.Errorf("write connect: %w", err)
	}
	hdr, payload, err := protocol.ReadMessage(conn)
	if err != nil {
		conn.Close()
		return nil, sid, fmt.Errorf("read connect-accept: %w", err)
	}
	if hdr.Type != protocol.MsgConnectAccept {
		conn.Close()
		return nil, sid, fmt.Errorf("expected connect-accept, got message type %d", hdr.Type)
	}
	accept, err := protocol.DecodeConnectAccept(payload)
	if err != nil {
		conn.Close()
		return nil, sid, fmt.Errorf("decode connect-accept: %w", err)
	}
	return conn, accept.SessionID, nil
}

// ProbeStoredSessions performs Hello/Welcome + MsgListSessions and
// returns the count of stored sessions. The ephemeral session created
// by the handshake is left dangling on the server's side and gets
// reaped via the daemon's normal idle-session cleanup.
func ProbeStoredSessions(socket string) (int, error) {
	conn, _, err := dialAndHandshake(socket)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	body, _ := protocol.EncodeListSessionsRequest(protocol.ListSessionsRequest{})
	hdr := protocol.Header{
		Version: protocol.Version,
		Type:    protocol.MsgListSessions,
		Flags:   protocol.FlagChecksum,
	}
	if err := protocol.WriteMessage(conn, hdr, body); err != nil {
		return 0, fmt.Errorf("write list-sessions: %w", err)
	}
	respHdr, respPayload, err := protocol.ReadMessage(conn)
	if err != nil {
		return 0, fmt.Errorf("read list-sessions: %w", err)
	}
	if respHdr.Type != protocol.MsgListSessionsResponse {
		return 0, fmt.Errorf("expected list-sessions response, got %d", respHdr.Type)
	}
	resp, err := protocol.DecodeListSessionsResponse(respPayload)
	if err != nil {
		return 0, fmt.Errorf("decode list-sessions: %w", err)
	}
	return len(resp.Stored), nil
}

// SocketPickerClient implements PickerClient against a live socket
// for the duration of the picker's UI session. One connection per
// picker invocation; closed when the picker exits.
type SocketPickerClient struct {
	conn      net.Conn
	sessionID [16]byte

	mu        sync.Mutex
	closeOnce sync.Once
}

// NewSocketPickerClient dials the socket and runs the handshake.
// Returns a client ready for use; caller must Close() when done.
func NewSocketPickerClient(socket string) (*SocketPickerClient, error) {
	conn, sid, err := dialAndHandshake(socket)
	if err != nil {
		return nil, err
	}
	return &SocketPickerClient{conn: conn, sessionID: sid}, nil
}

func (s *SocketPickerClient) roundTrip(reqType protocol.MessageType, body []byte) (protocol.Header, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      reqType,
		Flags:     protocol.FlagChecksum,
		SessionID: s.sessionID,
	}
	if err := protocol.WriteMessage(s.conn, hdr, body); err != nil {
		return protocol.Header{}, nil, fmt.Errorf("write %d: %w", reqType, err)
	}
	respHdr, respPayload, err := protocol.ReadMessage(s.conn)
	if err != nil {
		return protocol.Header{}, nil, fmt.Errorf("read response: %w", err)
	}
	return respHdr, respPayload, nil
}

func (s *SocketPickerClient) ListSessions() (protocol.ListSessionsResponse, error) {
	body, _ := protocol.EncodeListSessionsRequest(protocol.ListSessionsRequest{})
	hdr, payload, err := s.roundTrip(protocol.MsgListSessions, body)
	if err != nil {
		return protocol.ListSessionsResponse{}, err
	}
	if hdr.Type != protocol.MsgListSessionsResponse {
		return protocol.ListSessionsResponse{}, fmt.Errorf("unexpected response type %d", hdr.Type)
	}
	return protocol.DecodeListSessionsResponse(payload)
}

func (s *SocketPickerClient) RecoverSession(id [16]byte, newLabel string) error {
	body, err := protocol.EncodeRecoverSessionRequest(protocol.RecoverSessionRequest{SessionID: id, NewLabel: newLabel})
	if err != nil {
		return err
	}
	hdr, payload, err := s.roundTrip(protocol.MsgRecoverSession, body)
	if err != nil {
		return err
	}
	switch hdr.Type {
	case protocol.MsgConnectAccept:
		return nil
	case protocol.MsgError:
		ef, _ := protocol.DecodeErrorFrame(payload)
		return errors.New(ef.Message)
	default:
		return fmt.Errorf("unexpected response %d", hdr.Type)
	}
}

func (s *SocketPickerClient) RenameSession(id [16]byte, newLabel string) error {
	body, err := protocol.EncodeRenameSessionRequest(protocol.RenameSessionRequest{SessionID: id, NewLabel: newLabel})
	if err != nil {
		return err
	}
	return s.opRoundTrip(protocol.MsgRenameSession, body, protocol.OpRename)
}

func (s *SocketPickerClient) DeleteSession(id [16]byte) error {
	body, err := protocol.EncodeDeleteSessionRequest(protocol.DeleteSessionRequest{SessionID: id})
	if err != nil {
		return err
	}
	return s.opRoundTrip(protocol.MsgDeleteSession, body, protocol.OpDelete)
}

func (s *SocketPickerClient) opRoundTrip(reqType protocol.MessageType, body []byte, expectOp protocol.SessionOpKind) error {
	hdr, payload, err := s.roundTrip(reqType, body)
	if err != nil {
		return err
	}
	if hdr.Type != protocol.MsgSessionOpResponse {
		return fmt.Errorf("unexpected response %d", hdr.Type)
	}
	resp, err := protocol.DecodeSessionOpResponse(payload)
	if err != nil {
		return err
	}
	if resp.OpType != expectOp {
		return fmt.Errorf("op mismatch: got %d, want %d", resp.OpType, expectOp)
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	return nil
}

func (s *SocketPickerClient) FetchThumbnail(id [16]byte) ([]byte, error) {
	body, err := protocol.EncodeFetchThumbnailRequest(protocol.FetchThumbnailRequest{SessionID: id})
	if err != nil {
		return nil, err
	}
	hdr, payload, err := s.roundTrip(protocol.MsgFetchThumbnail, body)
	if err != nil {
		return nil, err
	}
	if hdr.Type != protocol.MsgFetchThumbnailResponse {
		return nil, fmt.Errorf("unexpected response %d", hdr.Type)
	}
	resp, err := protocol.DecodeFetchThumbnailResponse(payload)
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, errors.New(resp.Error)
	}
	return resp.PNG, nil
}

// StartFreshSession is a no-op for the socket client — the picker
// simply exits with choiceFresh and main.go takes the ordinary
// connect path (zero sessionID).
func (s *SocketPickerClient) StartFreshSession() {}

// Close shuts down the underlying socket. Safe to call multiple times.
func (s *SocketPickerClient) Close() error {
	var err error
	s.closeOnce.Do(func() {
		err = s.conn.Close()
	})
	return err
}

package client

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"net"
	"time"

	"texelation/protocol"
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

// RequestResume sends a RESUME_REQUEST and returns the server response header/payload.
func (c *SimpleClient) RequestResume(conn net.Conn, sessionID [16]byte, sequence uint64) (protocol.Header, []byte, error) {
	req := protocol.ResumeRequest{SessionID: sessionID, LastSequence: sequence}
	payload, err := protocol.EncodeResumeRequest(req)
	if err != nil {
		return protocol.Header{}, nil, err
	}
	if err := protocol.WriteMessage(conn, protocol.Header{Version: protocol.Version, Type: protocol.MsgResumeRequest, Flags: protocol.FlagChecksum, SessionID: sessionID}, payload); err != nil {
		return protocol.Header{}, nil, err
	}
	return protocol.ReadMessage(conn)
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

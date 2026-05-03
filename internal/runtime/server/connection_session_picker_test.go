// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/framegrace/texelation/protocol"
)

// pickerTestConn wires a *connection to a net.Pipe so the test can
// drive handleMessage synchronously and read responses off the client
// side.
type pickerTestConn struct {
	t         *testing.T
	conn      *connection
	clientEnd net.Conn
	serverEnd net.Conn
	closeOnce sync.Once
}

func newPickerTestConn(t *testing.T, m *Manager) *pickerTestConn {
	t.Helper()
	clientEnd, serverEnd := net.Pipe()
	sess, err := m.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	c := newConnection(serverEnd, sess, nopSink{}, m, false, false)
	return &pickerTestConn{
		t:         t,
		conn:      c,
		clientEnd: clientEnd,
		serverEnd: serverEnd,
	}
}

// send invokes handleMessage with the supplied message type + payload
// on a goroutine — handleMessage may write a response that blocks on
// the pipe until the test reads it.
func (p *pickerTestConn) send(typ protocol.MessageType, payload []byte) <-chan error {
	p.t.Helper()
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      typ,
		Flags:     protocol.FlagChecksum,
		SessionID: p.conn.session.ID(),
	}
	done := make(chan error, 1)
	go func() {
		done <- p.conn.handleMessage("test", hdr, payload)
	}()
	return done
}

// expectResponse reads the next protocol message off the client end
// of the pipe and asserts its type matches `want`. Returns the payload.
func (p *pickerTestConn) expectResponse(t *testing.T, want protocol.MessageType) []byte {
	t.Helper()
	hdr, payload, err := protocol.ReadMessage(p.clientEnd)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if hdr.Type != want {
		t.Fatalf("response type = %d, want %d", hdr.Type, want)
	}
	return payload
}

func (p *pickerTestConn) cleanup() {
	p.closeOnce.Do(func() {
		_ = p.clientEnd.Close()
		_ = p.serverEnd.Close()
	})
}

func mustEncodeListSessionsRequest(t *testing.T) []byte {
	t.Helper()
	out, err := protocol.EncodeListSessionsRequest(protocol.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return out
}

func TestHandleListSessions_Empty(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn := newPickerTestConn(t, m)
	defer conn.cleanup()
	doneCh := conn.send(protocol.MsgListSessions, mustEncodeListSessionsRequest(t))
	resp := conn.expectResponse(t, protocol.MsgListSessionsResponse)
	if err := <-doneCh; err != nil {
		t.Fatalf("handler: %v", err)
	}
	got, err := protocol.DecodeListSessionsResponse(resp)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Stored) != 0 || len(got.Live) != 0 {
		t.Fatalf("expected empty, got Stored=%d Live=%d", len(got.Stored), len(got.Live))
	}
}

func TestHandleListSessions_StoredPopulated(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, SessionsDirName)
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id1 := [16]byte{0x01}
	id2 := [16]byte{0x02}
	for i, fixture := range []*StoredSession{
		{SchemaVersion: StoredSessionSchemaVersion, SessionID: id1, LastActive: time.Unix(100, 0), Label: "older", PaneCount: 1},
		{SchemaVersion: StoredSessionSchemaVersion, SessionID: id2, LastActive: time.Unix(200, 0), Label: "newer", PaneCount: 2},
	} {
		data, _ := json.Marshal(fixture)
		path := filepath.Join(sessions, hex.EncodeToString(fixture.SessionID[:])+".json")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn := newPickerTestConn(t, m)
	defer conn.cleanup()
	doneCh := conn.send(protocol.MsgListSessions, mustEncodeListSessionsRequest(t))
	resp := conn.expectResponse(t, protocol.MsgListSessionsResponse)
	if err := <-doneCh; err != nil {
		t.Fatalf("handler: %v", err)
	}
	got, err := protocol.DecodeListSessionsResponse(resp)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Stored) != 2 {
		t.Fatalf("expected 2 stored, got %d", len(got.Stored))
	}
	if got.Stored[0].Label != "newer" {
		t.Errorf("expected newer first, got %q", got.Stored[0].Label)
	}
}

func TestHandleRecoverSession_HappyPath(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, SessionsDirName)
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := [16]byte{0xAA}
	stored := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Unix(100, 0),
		Label:         "rec-me",
		PaneCount:     1,
	}
	data, _ := json.Marshal(stored)
	if err := os.WriteFile(filepath.Join(sessions, hex.EncodeToString(id[:])+".json"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn := newPickerTestConn(t, m)
	defer conn.cleanup()
	body, err := protocol.EncodeRecoverSessionRequest(protocol.RecoverSessionRequest{SessionID: id})
	if err != nil {
		t.Fatalf("encode req: %v", err)
	}
	doneCh := conn.send(protocol.MsgRecoverSession, body)
	resp := conn.expectResponse(t, protocol.MsgConnectAccept)
	if err := <-doneCh; err != nil {
		t.Fatalf("handler: %v", err)
	}
	accept, err := protocol.DecodeConnectAccept(resp)
	if err != nil {
		t.Fatalf("decode accept: %v", err)
	}
	if accept.SessionID != id {
		t.Fatalf("accept SessionID = %x, want %x", accept.SessionID, id)
	}
}

func TestHandleRenameSession_HappyPath(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, SessionsDirName)
	os.MkdirAll(sessions, 0o755)
	id := [16]byte{0x10}
	stored := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Unix(100, 0),
		Label:         "before",
		PaneCount:     1,
	}
	data, _ := json.Marshal(stored)
	os.WriteFile(filepath.Join(sessions, hex.EncodeToString(id[:])+".json"), data, 0o644)
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn := newPickerTestConn(t, m)
	defer conn.cleanup()
	body, _ := protocol.EncodeRenameSessionRequest(protocol.RenameSessionRequest{SessionID: id, NewLabel: "after"})
	doneCh := conn.send(protocol.MsgRenameSession, body)
	resp := conn.expectResponse(t, protocol.MsgSessionOpResponse)
	if err := <-doneCh; err != nil {
		t.Fatalf("handler: %v", err)
	}
	got, err := protocol.DecodeSessionOpResponse(resp)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.OpType != protocol.OpRename || !got.OK {
		t.Fatalf("expected OpRename OK=true, got %#v", got)
	}
	doneCh = conn.send(protocol.MsgListSessions, mustEncodeListSessionsRequest(t))
	listResp := conn.expectResponse(t, protocol.MsgListSessionsResponse)
	if err := <-doneCh; err != nil {
		t.Fatalf("list handler: %v", err)
	}
	listGot, _ := protocol.DecodeListSessionsResponse(listResp)
	if len(listGot.Stored) != 1 || listGot.Stored[0].Label != "after" {
		t.Fatalf("expected rename applied; got %#v", listGot.Stored)
	}
}

func TestHandleRenameSession_UnknownID(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755)
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn := newPickerTestConn(t, m)
	defer conn.cleanup()
	body, _ := protocol.EncodeRenameSessionRequest(protocol.RenameSessionRequest{SessionID: [16]byte{0xFF}, NewLabel: "x"})
	doneCh := conn.send(protocol.MsgRenameSession, body)
	resp := conn.expectResponse(t, protocol.MsgSessionOpResponse)
	if err := <-doneCh; err != nil {
		t.Fatalf("handler: %v", err)
	}
	got, _ := protocol.DecodeSessionOpResponse(resp)
	if got.OK || got.Error == "" {
		t.Errorf("expected OK=false with error, got %#v", got)
	}
	if got.OpType != protocol.OpRename {
		t.Errorf("OpType = %d, want OpRename", got.OpType)
	}
}

func TestHandleDeleteSession_HappyPath(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, SessionsDirName)
	os.MkdirAll(sessions, 0o755)
	id := [16]byte{0x20}
	stored := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Unix(100, 0),
		PaneCount:     1,
	}
	data, _ := json.Marshal(stored)
	os.WriteFile(filepath.Join(sessions, hex.EncodeToString(id[:])+".json"), data, 0o644)
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn := newPickerTestConn(t, m)
	defer conn.cleanup()
	body, _ := protocol.EncodeDeleteSessionRequest(protocol.DeleteSessionRequest{SessionID: id})
	doneCh := conn.send(protocol.MsgDeleteSession, body)
	resp := conn.expectResponse(t, protocol.MsgSessionOpResponse)
	if err := <-doneCh; err != nil {
		t.Fatalf("handler: %v", err)
	}
	got, _ := protocol.DecodeSessionOpResponse(resp)
	if got.OpType != protocol.OpDelete || !got.OK {
		t.Fatalf("expected OpDelete OK=true, got %#v", got)
	}
	doneCh = conn.send(protocol.MsgListSessions, mustEncodeListSessionsRequest(t))
	listResp := conn.expectResponse(t, protocol.MsgListSessionsResponse)
	if err := <-doneCh; err != nil {
		t.Fatalf("list handler: %v", err)
	}
	listGot, _ := protocol.DecodeListSessionsResponse(listResp)
	if len(listGot.Stored) != 0 {
		t.Fatalf("expected empty after delete; got %d", len(listGot.Stored))
	}
}

func TestHandleFetchThumbnail_HappyPath(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, SessionsDirName)
	os.MkdirAll(sessions, 0o755)
	id := [16]byte{0x30}
	stored := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Unix(100, 0),
		PaneCount:     1,
	}
	data, _ := json.Marshal(stored)
	os.WriteFile(filepath.Join(sessions, hex.EncodeToString(id[:])+".json"), data, 0o644)
	pngPath := filepath.Join(sessions, hex.EncodeToString(id[:])+".png")
	pngContent := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	os.WriteFile(pngPath, pngContent, 0o644)

	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn := newPickerTestConn(t, m)
	defer conn.cleanup()
	body, _ := protocol.EncodeFetchThumbnailRequest(protocol.FetchThumbnailRequest{SessionID: id})
	doneCh := conn.send(protocol.MsgFetchThumbnail, body)
	resp := conn.expectResponse(t, protocol.MsgFetchThumbnailResponse)
	if err := <-doneCh; err != nil {
		t.Fatalf("handler: %v", err)
	}
	got, err := protocol.DecodeFetchThumbnailResponse(resp)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.OK {
		t.Fatalf("expected OK=true, got Error=%q", got.Error)
	}
	if string(got.PNG) != string(pngContent) {
		t.Fatalf("PNG mismatch")
	}
}

func TestHandleFetchThumbnail_Missing(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755)
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn := newPickerTestConn(t, m)
	defer conn.cleanup()
	body, _ := protocol.EncodeFetchThumbnailRequest(protocol.FetchThumbnailRequest{SessionID: [16]byte{0x99}})
	doneCh := conn.send(protocol.MsgFetchThumbnail, body)
	resp := conn.expectResponse(t, protocol.MsgFetchThumbnailResponse)
	if err := <-doneCh; err != nil {
		t.Fatalf("handler: %v", err)
	}
	got, _ := protocol.DecodeFetchThumbnailResponse(resp)
	if got.OK {
		t.Errorf("expected OK=false for missing PNG")
	}
}

func TestHandleFetchThumbnail_RefusesOversize(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, SessionsDirName)
	os.MkdirAll(sessions, 0o755)
	id := [16]byte{0x40}
	pngPath := filepath.Join(sessions, hex.EncodeToString(id[:])+".png")
	if err := os.WriteFile(pngPath, make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatalf("write big png: %v", err)
	}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn := newPickerTestConn(t, m)
	defer conn.cleanup()
	body, _ := protocol.EncodeFetchThumbnailRequest(protocol.FetchThumbnailRequest{SessionID: id})
	doneCh := conn.send(protocol.MsgFetchThumbnail, body)
	resp := conn.expectResponse(t, protocol.MsgFetchThumbnailResponse)
	if err := <-doneCh; err != nil {
		t.Fatalf("handler: %v", err)
	}
	got, _ := protocol.DecodeFetchThumbnailResponse(resp)
	if got.OK {
		t.Errorf("expected OK=false for oversized PNG, got OK=true with %d bytes", len(got.PNG))
	}
}

func TestHandleRecoverSession_UnknownID(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn := newPickerTestConn(t, m)
	defer conn.cleanup()
	body, _ := protocol.EncodeRecoverSessionRequest(protocol.RecoverSessionRequest{SessionID: [16]byte{0xFF}})
	doneCh := conn.send(protocol.MsgRecoverSession, body)
	resp := conn.expectResponse(t, protocol.MsgError)
	if err := <-doneCh; err != nil {
		t.Fatalf("handler: %v", err)
	}
	got, err := protocol.DecodeErrorFrame(resp)
	if err != nil {
		t.Fatalf("decode error frame: %v", err)
	}
	if got.Message == "" {
		t.Errorf("expected non-empty error message")
	}
}

// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/framegrace/texelation/internal/runtime/server/testutil"
	"github.com/framegrace/texelation/protocol"
)

func TestD2_ResumeRehydratesUnknownSession(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0x12, 0x34}
	stored := StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Now().UTC(),
		PaneViewports: []StoredPaneViewport{{
			PaneID:        [16]byte{0x77},
			ViewBottomIdx: 9000,
			Rows:          24,
			Cols:          80,
		}},
	}
	path := SessionFilePath(dir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(&stored)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	clientToServer, serverToClient := testutil.NewMemPipe(64)
	defer clientToServer.Close()
	defer serverToClient.Close()

	go func() {
		_ = protocol.WriteMessage(clientToServer, protocol.Header{Version: protocol.Version, Type: protocol.MsgHello}, mustEncodeHelloD2(t))
		_, _, _ = protocol.ReadMessage(clientToServer)
		payload, _ := protocol.EncodeConnectRequest(protocol.ConnectRequest{SessionID: id})
		_ = protocol.WriteMessage(clientToServer, protocol.Header{Version: protocol.Version, Type: protocol.MsgConnectRequest}, payload)
		_, _, _ = protocol.ReadMessage(clientToServer)
	}()

	sess, resuming, err := handleHandshake(serverToClient, mgr)
	if err != nil {
		t.Fatalf("handleHandshake: %v", err)
	}
	if !resuming {
		t.Fatalf("expected resuming=true for known sessionID")
	}
	if sess.ID() != id {
		t.Fatalf("expected rehydrated session %x, got %x", id, sess.ID())
	}
	vp, ok := sess.Viewport([16]byte{0x77})
	if !ok || vp.ViewBottomIdx != 9000 {
		t.Fatalf("rehydrated viewport missing or wrong: ok=%v vp=%+v", ok, vp)
	}
}

func mustEncodeHelloD2(t *testing.T) []byte {
	t.Helper()
	out, err := protocol.EncodeHello(protocol.Hello{
		ClientID:     [16]byte{},
		ClientName:   "test",
		Capabilities: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

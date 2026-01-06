// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/server_boot_test.go
// Summary: Exercises server boot behaviour to ensure the server runtime remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	texelcore "github.com/framegrace/texelui/core"
	"net"
	"path/filepath"
	"testing"

	"github.com/framegrace/texelation/protocol"
	"github.com/framegrace/texelation/texel"
)

func TestServerSendsBootSnapshotFallback(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewSnapshotStore(filepath.Join(tmpDir, "snapshot.json"))

	var paneID [16]byte
	paneID[0] = 1
	snapshot := []texel.PaneSnapshot{{
		ID:    paneID,
		Title: "pane",
		Buffer: [][]texelcore.Cell{
			{{Ch: 'h'}, {Ch: 'i'}},
		},
		Rect: texel.Rectangle{X: 2, Y: 3, Width: 10, Height: 4},
	}}
	capture := texel.TreeCapture{
		Panes: snapshot,
		Root:  &texel.TreeNodeCapture{PaneIndex: 0},
	}

	if err := store.Save(&capture); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	srv := NewServer(filepath.Join(tmpDir, "sock"), nil)
	srv.SetSnapshotStore(store, 0)
	srv.loadBootSnapshot()

	client, serverConn := net.Pipe()
	defer client.Close()
	defer serverConn.Close()

	sess := NewSession(paneID, 10)

	go srv.sendSnapshot(serverConn, sess)

	header, payload, err := protocol.ReadMessage(client)
	if err != nil {
		t.Fatalf("read message failed: %v", err)
	}
	if header.Type != protocol.MsgTreeSnapshot {
		t.Fatalf("expected tree snapshot, got %v", header.Type)
	}

	snap, err := protocol.DecodeTreeSnapshot(payload)
	if err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if len(snap.Panes) != 1 {
		t.Fatalf("expected 1 pane, got %d", len(snap.Panes))
	}
	pane := snap.Panes[0]
	if pane.X != 2 || pane.Y != 3 || pane.Width != 10 || pane.Height != 4 {
		t.Fatalf("unexpected geometry: %+v", pane)
	}
	if len(pane.Rows) != 1 || pane.Rows[0] != "hi" {
		t.Fatalf("unexpected rows: %v", pane.Rows)
	}
}

package server

import (
	"net"
	"path/filepath"
	"testing"

	"texelation/protocol"
	"texelation/texel"
)

func TestServerSendsBootSnapshotFallback(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewSnapshotStore(filepath.Join(tmpDir, "snapshot.json"))

	var paneID [16]byte
	paneID[0] = 1
	snapshot := []texel.PaneSnapshot{{
		ID:    paneID,
		Title: "pane",
		Buffer: [][]texel.Cell{
			{{Ch: 'h'}, {Ch: 'i'}},
		},
		Rect: texel.Rectangle{X: 2, Y: 3, Width: 10, Height: 4},
	}}

	if err := store.Save(snapshot); err != nil {
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

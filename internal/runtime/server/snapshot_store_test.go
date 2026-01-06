// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/snapshot_store_test.go
// Summary: Exercises snapshot store behaviour to ensure the server runtime remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	texelcore "github.com/framegrace/texelui/core"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/framegrace/texelation/protocol"
	"github.com/framegrace/texelation/texel"
)

func TestSnapshotStoreSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	store := NewSnapshotStore(path)

	pane := texel.PaneSnapshot{
		Title:  "pane",
		Buffer: [][]texelcore.Cell{{{Ch: 'A'}, {Ch: 'B'}}},
		Rect:   texel.Rectangle{X: 1, Y: 2, Width: 10, Height: 5},
	}
	pane.AppType = "test"
	pane.AppConfig = map[string]interface{}{"msg": "hello"}
	capture := texel.TreeCapture{
		Panes: []texel.PaneSnapshot{pane},
		Root:  &texel.TreeNodeCapture{PaneIndex: 0},
	}

	if err := store.Save(&capture); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected snapshot file: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("expected non-empty snapshot file")
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(loaded.Panes) != 1 {
		t.Fatalf("expected 1 pane, got %d", len(loaded.Panes))
	}
	if loaded.Panes[0].Title != "pane" {
		t.Fatalf("unexpected title %s", loaded.Panes[0].Title)
	}
	if loaded.Panes[0].X != 1 || loaded.Panes[0].Width != 10 {
		t.Fatalf("unexpected geometry %+v", loaded.Panes[0])
	}
	if loaded.Panes[0].AppType != "test" {
		t.Fatalf("expected app type, got %s", loaded.Panes[0].AppType)
	}
	if msg, ok := loaded.Panes[0].AppConfig["msg"].(string); !ok || msg != "hello" {
		t.Fatalf("expected app config to be stored, got %+v", loaded.Panes[0].AppConfig)
	}
	if loaded.Tree.PaneIndex != 0 {
		t.Fatalf("expected root pane index 0, got %d", loaded.Tree.PaneIndex)
	}
	if loaded.Hash == "" {
		t.Fatalf("expected hash to be populated")
	}
}

func TestStoredPaneConversionsPreserveContent(t *testing.T) {
	idBytes := [16]byte{0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0xba, 0xbe, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xcd, 0xef}
	stored := StoredPane{
		ID:    hex.EncodeToString(idBytes[:]),
		Title: "pane",
		Rows:  []string{"abc", "def"},
		X:     2, Y: 3, Width: 4, Height: 2,
		AppType:   "app",
		AppConfig: map[string]interface{}{"flag": true},
	}

	pane := stored.ToPaneSnapshot()
	if pane.ID != idBytes {
		t.Fatalf("expected ID %x, got %x", idBytes, pane.ID)
	}
	if pane.Rect.X != 2 || pane.Rect.Y != 3 || pane.Rect.Width != 4 || pane.Rect.Height != 2 {
		t.Fatalf("unexpected rect %#v", pane.Rect)
	}
	if len(pane.Buffer) != 2 || len(pane.Buffer[0]) != 3 {
		t.Fatalf("unexpected buffer dimensions")
	}
	if pane.Buffer[0][0].Ch != 'a' || pane.Buffer[1][2].Ch != 'f' {
		t.Fatalf("buffer contents not preserved")
	}
	if pane.AppType != "app" {
		t.Fatalf("app type not preserved")
	}
	pane.AppConfig["flag"] = false
	if stored.AppConfig["flag"] != true {
		t.Fatalf("app config should be cloned")
	}

	proto := stored.toProtocolPane()
	if proto.PaneID != idBytes {
		t.Fatalf("expected proto ID %x, got %x", idBytes, proto.PaneID)
	}
	if proto.Width != 4 || proto.Height != 2 {
		t.Fatalf("unexpected proto geometry %#v", proto)
	}
	if len(proto.Rows) != 2 || proto.Rows[0] != "abc" {
		t.Fatalf("rows not copied")
	}
	stored.Rows[0] = "zzz"
	if proto.Rows[0] != "abc" {
		t.Fatalf("proto rows should be independent copy")
	}
}

func TestStoredSnapshotToTreeSnapshot(t *testing.T) {
	idA := [16]byte{1}
	idB := [16]byte{2}
	snapshot := StoredSnapshot{
		Panes: []StoredPane{
			{ID: hex.EncodeToString(idA[:]), Title: "A", Rows: []string{"aa"}, X: 0, Y: 0, Width: 5, Height: 1},
			{ID: hex.EncodeToString(idB[:]), Title: "B", Rows: []string{"bb"}, X: 5, Y: 0, Width: 5, Height: 1},
		},
		Tree: StoredNode{
			PaneIndex: -1,
			Split:     "horizontal",
			Ratios:    []float64{0.5, 0.5},
			Children: []StoredNode{
				{PaneIndex: 0, Split: "none"},
				{PaneIndex: 1, Split: "none"},
			},
		},
	}

	proto := snapshot.ToTreeSnapshot()
	if len(proto.Panes) != 2 {
		t.Fatalf("expected two panes, got %d", len(proto.Panes))
	}
	if proto.Panes[0].PaneID != idA || proto.Panes[1].PaneID != idB {
		t.Fatalf("pane IDs not preserved")
	}
	if proto.Root.Split != protocol.SplitHorizontal {
		t.Fatalf("expected horizontal split, got %d", proto.Root.Split)
	}
	if len(proto.Root.Children) != 2 {
		t.Fatalf("expected two children, got %d", len(proto.Root.Children))
	}
	if proto.Root.SplitRatios[0] != 0.5 {
		t.Fatalf("expected ratio 0.5, got %f", proto.Root.SplitRatios[0])
	}
}

func TestEncodeStoredConfigHandlesErrors(t *testing.T) {
	cfg := map[string]interface{}{"invalid": make(chan struct{})}
	if got := encodeStoredConfig(cfg); got != "" {
		t.Fatalf("expected empty string for invalid config, got %q", got)
	}
	if got := encodeStoredConfig(nil); got != "" {
		t.Fatalf("expected empty string for nil config, got %q", got)
	}
}

func TestStoreTreeNodeRoundTrip(t *testing.T) {
	root := &texel.TreeNodeCapture{
		PaneIndex:   -1,
		Split:       texel.Vertical,
		SplitRatios: []float64{0.25, 0.75},
		Children: []*texel.TreeNodeCapture{
			{PaneIndex: 0},
			{
				PaneIndex:   -1,
				Split:       texel.Horizontal,
				SplitRatios: []float64{0.6, 0.4},
				Children: []*texel.TreeNodeCapture{
					{PaneIndex: 1},
					{PaneIndex: 2},
				},
			},
		},
	}

	stored := storeTreeNode(root)
	if stored.Split != "vertical" {
		t.Fatalf("expected vertical split, got %q", stored.Split)
	}
	if len(stored.Children) != 2 {
		t.Fatalf("expected two children")
	}
	if stored.Children[1].Split != "horizontal" {
		t.Fatalf("expected nested horizontal split")
	}

	proto := stored.toProtocolNode()
	if proto.Split != protocol.SplitVertical {
		t.Fatalf("expected protocol vertical split, got %d", proto.Split)
	}
	if len(proto.Children) != 2 || proto.Children[1].Split != protocol.SplitHorizontal {
		t.Fatalf("expected nested horizontal split in protocol")
	}
	if proto.Children[1].Children[0].PaneIndex != 1 {
		t.Fatalf("expected pane index 1, got %d", proto.Children[1].Children[0].PaneIndex)
	}

	empty := storeTreeNode(nil)
	if empty.Split != "none" || empty.PaneIndex != -1 {
		t.Fatalf("expected empty node defaults, got %+v", empty)
	}
}

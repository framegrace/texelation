package server

import (
	"os"
	"path/filepath"
	"testing"

	"texelation/texel"
)

func TestSnapshotStoreSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	store := NewSnapshotStore(path)

	pane := texel.PaneSnapshot{
		Title:  "pane",
		Buffer: [][]texel.Cell{{{Ch: 'A'}, {Ch: 'B'}}},
		Rect:   texel.Rectangle{X: 1, Y: 2, Width: 10, Height: 5},
	}
	pane.AppType = "test"
	pane.AppConfig = map[string]interface{}{"msg": "hello"}
	capture := texel.TreeCapture{
		Panes: []texel.PaneSnapshot{pane},
		Root:  &texel.TreeNodeCapture{PaneIndex: 0},
	}

	if err := store.Save(capture); err != nil {
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

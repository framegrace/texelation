package sparse

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestPersistence_FlushLinesToPageStore(t *testing.T) {
	dir := t.TempDir()
	cfg := parser.DefaultPageStoreConfig(dir, "unit-test")
	ps, err := parser.CreatePageStore(cfg)
	if err != nil {
		t.Fatalf("CreatePageStore: %v", err)
	}
	defer ps.Close()

	adapter := NewPersistence(ps)

	// Write three lines to the sparse side.
	store := NewStore(10)
	store.SetLine(0, []parser.Cell{{Rune: 'a'}})
	store.SetLine(1, []parser.Cell{{Rune: 'b'}})
	store.SetLine(2, []parser.Cell{{Rune: 'c'}})

	if err := adapter.FlushLines(store, []int64{0, 1, 2}); err != nil {
		t.Fatalf("FlushLines: %v", err)
	}
	if err := ps.Flush(); err != nil {
		t.Fatalf("ps.Flush: %v", err)
	}

	// Read back through PageStore.
	line, err := ps.ReadLine(1)
	if err != nil {
		t.Fatalf("ReadLine(1): %v", err)
	}
	if len(line.Cells) == 0 || line.Cells[0].Rune != 'b' {
		t.Errorf("ReadLine(1) first rune = %q, want b", line.Cells[0].Rune)
	}

	// Ensure the temp dir was actually written to.
	if _, err := os.Stat(filepath.Join(dir, "terminals")); err != nil {
		t.Errorf("expected terminal dir under %s: %v", dir, err)
	}
}

func TestPersistence_SnapshotTerminal(t *testing.T) {
	tm := NewTerminal(80, 24)
	tm.WriteCell(parser.Cell{Rune: 'a'})
	tm.Newline()
	tm.WriteCell(parser.Cell{Rune: 'b'})

	state := SnapshotState(tm)
	if state.WriteTop != 0 {
		t.Errorf("WriteTop = %d, want 0", state.WriteTop)
	}
	if state.ContentEnd != 1 {
		t.Errorf("ContentEnd = %d, want 1 (two rows written)", state.ContentEnd)
	}
	if state.CursorGlobalIdx != 1 || state.CursorCol != 1 {
		t.Errorf("Cursor = (%d,%d), want (1,1)",
			state.CursorGlobalIdx, state.CursorCol)
	}
}

func TestPersistence_RestoreTerminal(t *testing.T) {
	state := parser.MainScreenState{
		WriteTop:        50,
		ContentEnd:      70,
		CursorGlobalIdx: 65,
		CursorCol:       3,
	}
	tm := NewTerminal(80, 24)
	RestoreState(tm, state)

	if got := tm.WriteTop(); got != 50 {
		t.Errorf("restored WriteTop = %d, want 50", got)
	}
	gi, col := tm.Cursor()
	if gi != 65 || col != 3 {
		t.Errorf("restored Cursor = (%d,%d), want (65,3)", gi, col)
	}
	if !tm.IsFollowing() {
		t.Error("restored Terminal should be in autoFollow mode by default")
	}
}

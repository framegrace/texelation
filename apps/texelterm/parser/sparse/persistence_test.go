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

package sparse

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestWriteWindow_NewInitialState(t *testing.T) {
	store := NewStore(80)
	ww := NewWriteWindow(store, 80, 24)
	if got := ww.Width(); got != 80 {
		t.Errorf("Width() = %d, want 80", got)
	}
	if got := ww.Height(); got != 24 {
		t.Errorf("Height() = %d, want 24", got)
	}
	if got := ww.WriteTop(); got != 0 {
		t.Errorf("WriteTop() = %d, want 0 (fresh WriteWindow)", got)
	}
	if got := ww.WriteBottom(); got != 23 {
		t.Errorf("WriteBottom() = %d, want 23", got)
	}
	gi, col := ww.Cursor()
	if gi != 0 || col != 0 {
		t.Errorf("Cursor() = (%d,%d), want (0,0)", gi, col)
	}
}

func TestWriteWindow_WriteCellAdvancesCol(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	ww.WriteCell(parser.Cell{Rune: 'h'})
	ww.WriteCell(parser.Cell{Rune: 'i'})

	gi, col := ww.Cursor()
	if gi != 0 || col != 2 {
		t.Errorf("Cursor() after 2 writes = (%d,%d), want (0,2)", gi, col)
	}
	if got := store.Get(0, 0).Rune; got != 'h' {
		t.Errorf("store[0][0] = %q, want h", got)
	}
	if got := store.Get(0, 1).Rune; got != 'i' {
		t.Errorf("store[0][1] = %q, want i", got)
	}
}

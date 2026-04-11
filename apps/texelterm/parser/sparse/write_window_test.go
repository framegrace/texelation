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

func TestWriteWindow_CarriageReturn(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	ww.WriteCell(parser.Cell{Rune: 'h'})
	ww.WriteCell(parser.Cell{Rune: 'i'})
	ww.CarriageReturn()
	gi, col := ww.Cursor()
	if gi != 0 || col != 0 {
		t.Errorf("after CR, Cursor() = (%d,%d), want (0,0)", gi, col)
	}
}

func TestWriteWindow_SetCursorRelative(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 10)
	ww.SetCursor(3, 7) // row 3, col 7
	gi, col := ww.Cursor()
	if gi != 3 || col != 7 {
		t.Errorf("SetCursor(3,7): Cursor() = (%d,%d), want (3,7)", gi, col)
	}
	if got := ww.CursorRow(); got != 3 {
		t.Errorf("CursorRow() = %d, want 3", got)
	}
}

func TestWriteWindow_SetCursorClampsToWindow(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	ww.SetCursor(100, 100) // way out of range
	gi, col := ww.Cursor()
	// Clamp row to [0, height-1] and col to [0, width-1].
	if gi != 4 {
		t.Errorf("row clamp: gi = %d, want 4", gi)
	}
	if col != 9 {
		t.Errorf("col clamp: col = %d, want 9", col)
	}
}

func TestWriteWindow_NewlineAdvancesCursor(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	ww.WriteCell(parser.Cell{Rune: 'a'})
	ww.Newline()

	gi, col := ww.Cursor()
	if gi != 1 || col != 0 {
		t.Errorf("after Newline from row 0, Cursor() = (%d,%d), want (1,0)", gi, col)
	}
	if got := ww.WriteTop(); got != 0 {
		t.Errorf("WriteTop() should not move; got %d", got)
	}
}

func TestWriteWindow_NewlineAtBottomAdvancesWriteTop(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 3)
	// Park cursor at last row.
	ww.SetCursor(2, 0)
	ww.Newline()

	if got := ww.WriteTop(); got != 1 {
		t.Errorf("WriteTop() after LF at bottom = %d, want 1 (scrolled up)", got)
	}
	if got := ww.WriteBottom(); got != 3 {
		t.Errorf("WriteBottom() = %d, want 3", got)
	}
	gi, col := ww.Cursor()
	if gi != 3 || col != 0 {
		t.Errorf("Cursor() = (%d,%d), want (3,0)", gi, col)
	}
}

func TestWriteWindow_NewlinePreservesContent(t *testing.T) {
	// Content at oldWriteTop (row 0) must stay in the store even after the
	// window moves — that's the whole "scrollback is a windowing concept" principle.
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 3)
	ww.WriteCell(parser.Cell{Rune: 'H'})  // row 0
	ww.SetCursor(2, 0)
	ww.Newline() // scrolls

	if got := store.Get(0, 0).Rune; got != 'H' {
		t.Errorf("after scroll-up, store[0][0] = %q, want H (content survives)", got)
	}
}

func TestWriteWindow_ResizeGrowRetreatsWriteTop(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	// Scroll down 10 times so writeTop is at 10.
	for i := 0; i < 10; i++ {
		ww.SetCursor(4, 0)
		ww.Newline()
	}
	if got := ww.WriteTop(); got != 10 {
		t.Fatalf("setup: WriteTop = %d, want 10", got)
	}

	// Grow from 5 to 8. writeTop should retreat by 3 to keep writeBottom pinned.
	ww.Resize(10, 8)
	if got := ww.WriteTop(); got != 7 {
		t.Errorf("after grow 5->8, WriteTop = %d, want 7", got)
	}
	if got := ww.WriteBottom(); got != 14 {
		t.Errorf("after grow, WriteBottom = %d, want 14 (unchanged)", got)
	}
	if got := ww.Height(); got != 8 {
		t.Errorf("Height = %d, want 8", got)
	}
}

func TestWriteWindow_ResizeGrowClampsAtZero(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	// writeTop = 0. Grow to 10 — shallow scrollback case.
	ww.Resize(10, 10)
	if got := ww.WriteTop(); got != 0 {
		t.Errorf("after grow from 0, WriteTop = %d, want 0 (clamped)", got)
	}
	if got := ww.WriteBottom(); got != 9 {
		t.Errorf("WriteBottom = %d, want 9 (extended past oldWriteBottom=4)", got)
	}
}

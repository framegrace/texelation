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
	ww.WriteCell(parser.Cell{Rune: 'H'}) // row 0
	ww.SetCursor(2, 0)
	ww.Newline() // scrolls

	if got := store.Get(0, 0).Rune; got != 'H' {
		t.Errorf("after scroll-up, store[0][0] = %q, want H (content survives)", got)
	}
}

func TestWriteWindow_ResizeGrowAnchorsBottom(t *testing.T) {
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
	// writeBottom = 10 + 5 - 1 = 14.

	// Grow from 5 to 8. writeBottom stays at 14; writeTop retreats to reveal history.
	ww.Resize(10, 8)
	if got := ww.WriteBottom(); got != 14 {
		t.Errorf("after grow, WriteBottom = %d, want 14 (anchored)", got)
	}
	if got := ww.WriteTop(); got != 7 {
		t.Errorf("after grow 5->8, WriteTop = %d, want 7 (retreated)", got)
	}
	if got := ww.Height(); got != 8 {
		t.Errorf("Height = %d, want 8", got)
	}
}

func TestWriteWindow_ResizeGrowFreshTerminal(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	// writeTop = 0, writeBottom = 4. Grow to 10.
	// Bottom-anchor: writeTop = 4 - 10 + 1 = -5, clamped to 0.
	// No history to reveal, so bottom extends to 9.
	ww.Resize(10, 10)
	if got := ww.WriteTop(); got != 0 {
		t.Errorf("after grow from 0, WriteTop = %d, want 0 (clamped)", got)
	}
	if got := ww.WriteBottom(); got != 9 {
		t.Errorf("WriteBottom = %d, want 9", got)
	}
}

func TestWriteWindow_ResizeShrinkShellCase(t *testing.T) {
	// Shell case: cursor at bottom row (39). Shrink 40→20.
	// writeBottom anchored at 39. writeTop advances from 0 to 20.
	// Cursor at gi=39 stays (within new window [20,39]).
	// Rows survive in store — TUI/shell redraws after SIGWINCH.
	store := NewStore(80)
	ww := NewWriteWindow(store, 80, 40)
	for i := 0; i < 40; i++ {
		store.SetLine(int64(i), []parser.Cell{{Rune: 'L'}})
	}
	ww.SetCursor(39, 5)

	ww.Resize(80, 20)

	if got := ww.WriteTop(); got != 20 {
		t.Errorf("shell shrink 40->20: WriteTop = %d, want 20 (bottom-anchored)", got)
	}
	if got := ww.WriteBottom(); got != 39 {
		t.Errorf("WriteBottom = %d, want 39 (anchored)", got)
	}
	gi, col := ww.Cursor()
	if gi != 39 || col != 5 {
		t.Errorf("cursor: (%d,%d), want (39,5) — within new window", gi, col)
	}
	// All rows survive in store.
	if got := store.Get(0, 0).Rune; got != 'L' {
		t.Errorf("row 0 should survive in store: %q", got)
	}
	if got := store.Get(39, 0).Rune; got != 'L' {
		t.Errorf("row 39 should survive in store: %q", got)
	}
}

func TestWriteWindow_ResizeShrinkCursorNearTop(t *testing.T) {
	// Cursor at row 2 (gi=2). Shrink from 40 to 20.
	// Cursor fits (2 < 20), so writeTop stays at 0. Shrink eats empty
	// space from bottom: writeBottom drops from 39 to 19.
	store := NewStore(80)
	ww := NewWriteWindow(store, 80, 40)
	for i := 0; i < 40; i++ {
		store.SetLine(int64(i), []parser.Cell{{Rune: 'L'}})
	}
	ww.SetCursor(2, 0)

	ww.Resize(80, 20)

	if got := ww.WriteTop(); got != 0 {
		t.Errorf("top-cursor shrink: WriteTop = %d, want 0 (cursor fits, no advance)", got)
	}
	if got := ww.WriteBottom(); got != 19 {
		t.Errorf("WriteBottom = %d, want 19 (shrunk from bottom)", got)
	}
	gi, _ := ww.Cursor()
	if gi != 2 {
		t.Errorf("cursor globalIdx: %d, want 2 (unchanged)", gi)
	}
	// All rows survive in store.
	if got := store.Get(20, 0).Rune; got != 'L' {
		t.Errorf("row 20 should survive: %q", got)
	}
	if got := store.Get(39, 0).Rune; got != 'L' {
		t.Errorf("row 39 should survive: %q", got)
	}
	if got := store.Get(0, 0).Rune; got != 'L' {
		t.Errorf("row 0 should survive: %q", got)
	}
}

func TestWriteWindow_ResizeShrinkCursorClamped(t *testing.T) {
	// Cursor at row 30 (gi=30) of h=40. Shrink to h=20.
	// Cursor doesn't fit (30 >= 20). writeTop advances to keep cursor
	// at the bottom: writeTop = 30 - 20 + 1 = 11. writeBottom = 30.
	store := NewStore(80)
	ww := NewWriteWindow(store, 80, 40)
	ww.SetCursor(30, 0)

	ww.Resize(80, 20)

	if got := ww.WriteTop(); got != 11 {
		t.Errorf("shrink: WriteTop = %d, want 11 (cursor-anchored)", got)
	}
	if got := ww.WriteBottom(); got != 30 {
		t.Errorf("WriteBottom = %d, want 30", got)
	}
	gi, _ := ww.Cursor()
	if gi != 30 {
		t.Errorf("cursor globalIdx: %d, want 30 (at window bottom)", gi)
	}
	if got := ww.CursorRow(); got != 19 {
		t.Errorf("CursorRow = %d, want 19 (bottom of new window)", got)
	}
}

func TestWriteWindow_EraseDisplayClearsWindow(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	// Fill store [0..9] with content, window covers [0..4].
	for i := int64(0); i < 10; i++ {
		store.SetLine(i, []parser.Cell{{Rune: 'X'}})
	}
	ww.EraseDisplay()
	// [0..4] cleared; [5..9] preserved.
	for i := int64(0); i <= 4; i++ {
		if got := store.GetLine(i); got != nil && len(got) > 0 && got[0].Rune != 0 {
			t.Errorf("row %d should be cleared, got %v", i, got)
		}
	}
	for i := int64(5); i <= 9; i++ {
		if got := store.Get(i, 0).Rune; got != 'X' {
			t.Errorf("row %d should be preserved, got %q", i, got)
		}
	}
}

func TestWriteWindow_EraseLineClearsCurrentRow(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	store.SetLine(2, []parser.Cell{{Rune: 'A'}, {Rune: 'B'}, {Rune: 'C'}})
	ww.SetCursor(2, 0)
	ww.EraseLine()
	if got := store.GetLine(2); got != nil && len(got) > 0 && got[0].Rune != 0 {
		t.Errorf("row 2 should be cleared, got %v", got)
	}
}

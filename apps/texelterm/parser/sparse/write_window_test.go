package sparse

import (
	"bytes"
	"log"
	"strings"
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

func TestWriteWindow_RewindWriteTop(t *testing.T) {
	// Simulate a non-alt-screen TUI doing ESC[2J + 5 newlines in a height=3
	// window. WriteTop naturally advances past the prompt anchor (globalIdx=0).
	// After a rewind back to 0, writeTop is at 0, HWM remains monotonic,
	// cursor is clamped into the new window.
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 3)
	// Fill past the bottom to force writeTop advancement.
	for i := 0; i < 5; i++ {
		ww.SetCursor(2, 0) // park at last row
		ww.Newline()       // scroll: writeTop += 1
	}
	if ww.WriteTop() < 5 {
		t.Fatalf("setup: expected writeTop >= 5 after 5 scrolls, got %d", ww.WriteTop())
	}
	hwmBefore := ww.WriteBottomHWM()

	ww.RewindWriteTop(0)

	if got := ww.WriteTop(); got != 0 {
		t.Errorf("after Rewind(0), WriteTop = %d, want 0", got)
	}
	if got := ww.WriteBottomHWM(); got != hwmBefore {
		t.Errorf("HWM must be monotonic: before=%d after=%d", hwmBefore, got)
	}
	gi, _ := ww.Cursor()
	if gi < 0 || gi > 2 {
		t.Errorf("cursor after rewind must be in new window [0,2]; gi=%d", gi)
	}
}

func TestWriteWindow_RewindWriteTopNoOpIfAhead(t *testing.T) {
	// Rewinding to a value >= current writeTop must be a no-op. Callers pass
	// the last-prompt globalIdx; if the window hasn't drifted past it yet,
	// nothing should move.
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 3)
	ww.SetCursor(1, 3)
	before := ww.WriteTop()
	beforeGi, beforeCol := ww.Cursor()

	ww.RewindWriteTop(5) // ahead of writeTop
	if got := ww.WriteTop(); got != before {
		t.Errorf("Rewind(5) when writeTop=%d should be no-op; got %d", before, got)
	}
	gi, col := ww.Cursor()
	if gi != beforeGi || col != beforeCol {
		t.Errorf("cursor should be unchanged; before=(%d,%d) after=(%d,%d)",
			beforeGi, beforeCol, gi, col)
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

// TestWriteWindow_ResizeShrinkThenExpandAnchorsOnHWM pins down the reason
// writeBottomHWM exists in the first place: on expand we re-anchor against
// the historical maximum writeBottom, not the current one. Without the
// HWM, a "shrink while cursor is near the top → expand back" round-trip
// would let writeTop retreat into scrollback, destroying history that a
// TUI's ESC[2J would blank on its SIGWINCH redraw.
//
// Replacing `writeBottomHWM` with `writeBottom` in the expand formula
// passes every other existing test (they all measure HWM on the same
// row as writeBottom at shrink time); this test is the one that flips.
func TestWriteWindow_ResizeShrinkThenExpandAnchorsOnHWM(t *testing.T) {
	store := NewStore(80)
	ww := NewWriteWindow(store, 80, 40)

	// Scroll the window so HWM climbs beyond the initial height.
	for i := 0; i < 100; i++ {
		ww.Newline()
	}
	// State: writeTop=61, cursor=100, writeBottom=100, HWM=100.
	if got := ww.WriteTop(); got != 61 {
		t.Fatalf("pre-shrink WriteTop = %d, want 61", got)
	}
	if got := ww.WriteBottomHWM(); got != 100 {
		t.Fatalf("pre-shrink HWM = %d, want 100", got)
	}

	// Move cursor near the top of the window so a shrink fits it without
	// advancing writeTop. This is what causes writeBottom to drop below
	// HWM on the shrink.
	ww.SetCursor(2, 0)

	ww.Resize(80, 20)
	// State: writeTop still 61 (cursor fit), writeBottom=80, HWM still 100.
	if got := ww.WriteTop(); got != 61 {
		t.Fatalf("shrink kept-cursor: WriteTop = %d, want 61 (stayed)", got)
	}
	if got := ww.WriteBottom(); got != 80 {
		t.Fatalf("shrink kept-cursor: WriteBottom = %d, want 80", got)
	}
	if got := ww.WriteBottomHWM(); got != 100 {
		t.Fatalf("HWM drifted during shrink: %d, want 100 (monotonic)", got)
	}

	// Expand back. Must re-anchor on HWM (100), not on the current
	// writeBottom (80). writeTop = 100 - 40 + 1 = 61.
	ww.Resize(80, 40)
	if got := ww.WriteTop(); got != 61 {
		t.Errorf("expand: WriteTop = %d, want 61 (anchored on HWM=100). "+
			"If this is 41 the expand formula used writeBottom instead of HWM "+
			"and history between 41..60 would be destroyed by a TUI redraw.", got)
	}
	if got := ww.WriteBottom(); got != 100 {
		t.Errorf("expand: WriteBottom = %d, want 100", got)
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

// The following three tests pin down the defensive HWM bump in IL/DL/NIR.
// Normal callers pass marginBottom < height, and HWM never drifts. But if a
// caller violates that invariant (parser bug, misuse), the operation still
// touches rows past the nominal writeBottom — and HWM must catch up or a
// later expand-resize will anchor against a stale value. The bump is cheap
// enough to run unconditionally, so we do.

func TestWriteWindow_InsertLinesExtendsHWMOnOutOfBoundsMargin(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	if got := ww.WriteBottomHWM(); got != 4 {
		t.Fatalf("initial HWM = %d, want 4 (height-1)", got)
	}
	// Call with marginBottom = 20, well past height-1 = 4.
	ww.InsertLines(1, 0, 0, 20)
	if got := ww.WriteBottomHWM(); got != 20 {
		t.Errorf("HWM after IL(marginBottom=20) = %d, want 20 (defensive bump)", got)
	}
}

func TestWriteWindow_DeleteLinesExtendsHWMOnOutOfBoundsMargin(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	ww.DeleteLines(1, 0, 0, 30)
	if got := ww.WriteBottomHWM(); got != 30 {
		t.Errorf("HWM after DL(marginBottom=30) = %d, want 30 (defensive bump)", got)
	}
}

func TestWriteWindow_NewlineInRegionExtendsHWMOnOutOfBoundsMargin(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	ww.NewlineInRegion(0, 12)
	if got := ww.WriteBottomHWM(); got != 12 {
		t.Errorf("HWM after NIR(marginBottom=12) = %d, want 12 (defensive bump)", got)
	}
}

// And the happy path: with marginBottom < height, HWM doesn't move — the
// bump is a no-op because writeTop+marginBottom stays inside [writeTop,
// writeBottom] which is at or below HWM.
func TestWriteWindow_InsertLinesDoesNotDriftHWMOnValidMargin(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)
	ww.InsertLines(1, 0, 0, 4)
	if got := ww.WriteBottomHWM(); got != 4 {
		t.Errorf("HWM moved on in-window IL: got %d, want 4", got)
	}
}

// TestWriteWindow_ResizeZeroDimensionsLogs covers the silent-early-return
// diagnostic: a broken SIGWINCH that calls Resize with 0 cols or rows must
// leave a trail in the log so the symptom can be pinned to this site.
func TestWriteWindow_ResizeZeroDimensionsLogs(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	ww.Resize(0, 5)
	if !strings.Contains(buf.String(), "Resize ignored") {
		t.Errorf("Resize(0,5) produced no diagnostic log; got %q", buf.String())
	}
	// State unchanged after the ignored call.
	if got := ww.Width(); got != 10 {
		t.Errorf("Resize(0,5) mutated width: got %d, want 10", got)
	}
	if got := ww.Height(); got != 5 {
		t.Errorf("Resize(0,5) mutated height: got %d, want 5", got)
	}

	buf.Reset()
	ww.Resize(10, 0)
	if !strings.Contains(buf.String(), "Resize ignored") {
		t.Errorf("Resize(10,0) produced no diagnostic log; got %q", buf.String())
	}
}

// TestWriteWindow_RestoreStateClampsNegativeWriteTop covers the corrupt-WAL
// path where writeTop slips past the basic MainScreenState.Validate check
// (or arrives from a caller that bypasses decode). A negative writeTop
// would push the write window into a phantom region; RestoreState must
// clamp to 0 and log so the symptom is visible.
func TestWriteWindow_RestoreStateClampsNegativeWriteTop(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	ww.RestoreState(-7, 0, 0, -1)

	if got := ww.WriteTop(); got != 0 {
		t.Errorf("writeTop not clamped to 0: got %d", got)
	}
	if !strings.Contains(buf.String(), "writeTop=-7 clamped to 0") {
		t.Errorf("no diagnostic for negative writeTop; log = %q", buf.String())
	}
}

// TestWriteWindow_RestoreStateClampsCursorOutsideWindow covers the case
// where cursorGlobalIdx lands above the window bottom. Validate rejects
// cursors below writeTop; the above-bottom case is width/height-dependent
// and only reachable here.
func TestWriteWindow_RestoreStateClampsCursorOutsideWindow(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	// Window is [100, 104]. Cursor at 999 should clamp to 104.
	ww.RestoreState(100, 999, 0, -1)

	gi, _ := ww.Cursor()
	if gi != 104 {
		t.Errorf("cursorGlobalIdx not clamped to writeBottom: got %d, want 104", gi)
	}
	if !strings.Contains(buf.String(), "above writeBottom") {
		t.Errorf("no diagnostic for cursor above bottom; log = %q", buf.String())
	}
}

// TestWriteWindow_RestoreStateClampsCursorCol covers out-of-range
// cursorCol. The WAL-layer Validate accepts any non-negative CursorCol
// because it doesn't know the window width, so clamping against width
// has to happen here.
func TestWriteWindow_RestoreStateClampsCursorCol(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	// width=10 — cursorCol=50 should clamp to 9.
	ww.RestoreState(0, 0, 50, -1)

	_, col := ww.Cursor()
	if col != 9 {
		t.Errorf("cursorCol not clamped to width-1: got %d, want 9", col)
	}
	if !strings.Contains(buf.String(), "cursorCol=50") {
		t.Errorf("no diagnostic for cursorCol above width; log = %q", buf.String())
	}

	// Negative cursorCol should also clamp (to 0) and log.
	buf.Reset()
	ww.RestoreState(0, 0, -3, -1)
	_, col = ww.Cursor()
	if col != 0 {
		t.Errorf("negative cursorCol not clamped to 0: got %d", col)
	}
	if !strings.Contains(buf.String(), "cursorCol=-3") {
		t.Errorf("no diagnostic for negative cursorCol; log = %q", buf.String())
	}
}

// TestWriteWindow_RestoreStateValidInputsNoLog confirms the silent path:
// well-formed restore inputs must not produce any diagnostic log, so the
// warnings above actually mean something when they appear.
func TestWriteWindow_RestoreStateValidInputsNoLog(t *testing.T) {
	store := NewStore(10)
	ww := NewWriteWindow(store, 10, 5)

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	// Cursor at writeTop+2 in a height-5 window, col 3 in width 10.
	ww.RestoreState(100, 102, 3, 200)

	if buf.Len() != 0 {
		t.Errorf("valid RestoreState produced diagnostic log: %q", buf.String())
	}
	if got := ww.WriteTop(); got != 100 {
		t.Errorf("WriteTop: got %d, want 100", got)
	}
	gi, col := ww.Cursor()
	if gi != 102 || col != 3 {
		t.Errorf("Cursor: got (%d,%d), want (102,3)", gi, col)
	}
	if got := ww.WriteBottomHWM(); got != 200 {
		t.Errorf("WriteBottomHWM: got %d, want 200", got)
	}
}

// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import "github.com/framegrace/texelation/apps/texelterm/parser"

// ClearNotifier is an alias for parser.ClearNotifier, re-exported from the
// sparse package so callers that work exclusively with sparse types do not need
// to import the parser package just to pass a notifier. Both types are
// structurally identical; Go's structural typing means any value that satisfies
// one also satisfies the other.
//
// Deprecated: prefer parser.ClearNotifier in new code.
type ClearNotifier = parser.ClearNotifier

// Terminal is a thin composition of Store, WriteWindow, and ViewWindow. It
// exposes the API that VTerm's main-screen path calls into.
//
// Construction is eager: all three underlying types are created up-front so
// that no method has to lazy-init anything. This keeps the locking strategy
// simple — reads never upgrade to writes.
type Terminal struct {
	store    *Store
	write    *WriteWindow
	view     *ViewWindow
	notifier parser.ClearNotifier
}

// NewTerminal creates a Terminal with the given dimensions. ViewWindow starts
// in autoFollow mode with viewBottom = height - 1.
func NewTerminal(width, height int) *Terminal {
	store := NewStore(width)
	write := NewWriteWindow(store, width, height)
	view := NewViewWindow(width, height)
	return &Terminal{store: store, write: write, view: view}
}

// Width returns the terminal width.
func (t *Terminal) Width() int { return t.write.Width() }

// Height returns the terminal height.
func (t *Terminal) Height() int { return t.write.Height() }

// IsFollowing reports whether the view is auto-following the write window.
func (t *Terminal) IsFollowing() bool { return t.view.IsFollowing() }

// ContentEnd returns the highest globalIdx ever written, or -1 if nothing
// has been written yet.
func (t *Terminal) ContentEnd() int64 { return t.store.Max() }

// WriteCell writes one cell at the cursor position. The view is NOT
// notified — WriteCell never creates new rows, so viewBottom is unaffected.
func (t *Terminal) WriteCell(cell parser.Cell) {
	t.write.WriteCell(cell)
}

// Newline advances the cursor (scrolling at bottom) and notifies the view
// with the cursor's new position — the actual content edge. We pass the
// cursor globalIdx rather than writeBottom because writeBottom is a derived
// value (writeTop + height - 1) that changes on resize without new content.
func (t *Terminal) Newline() {
	t.write.Newline()
	gi, _ := t.write.Cursor()
	t.view.OnWriteBottomChanged(gi)
}

// CarriageReturn resets cursor column to 0.
func (t *Terminal) CarriageReturn() { t.write.CarriageReturn() }

// SetCursor places the cursor at row, col (viewport-relative to writeTop).
func (t *Terminal) SetCursor(row, col int) { t.write.SetCursor(row, col) }

// Cursor returns the cursor (globalIdx, col) pair.
func (t *Terminal) Cursor() (globalIdx int64, col int) { return t.write.Cursor() }

// CursorRow returns the cursor row relative to writeTop.
func (t *Terminal) CursorRow() int { return t.write.CursorRow() }

// WriteTop returns the top globalIdx of the write window.
func (t *Terminal) WriteTop() int64 { return t.write.WriteTop() }

// WriteBottom returns the bottom globalIdx of the write window.
func (t *Terminal) WriteBottom() int64 { return t.write.WriteBottom() }

// VisibleRange returns the (top, bottom) globalIdx pair of the current view.
func (t *Terminal) VisibleRange() (top, bottom int64) { return t.view.VisibleRange() }

// Resize resizes both the write and view windows. ViewWindow observes the
// (possibly extended) writeBottom to update autoFollow.
func (t *Terminal) Resize(newWidth, newHeight int) {
	t.write.Resize(newWidth, newHeight)
	t.view.Resize(newWidth, newHeight, t.write.WriteBottom())
}

// ScrollUp scrolls the view back by n reflowed rows and disengages autoFollow.
// If the view was auto-following, the live-edge anchor is first computed from
// the cursor's chain so the scroll is relative to what the user was looking
// at — not the stale viewAnchor (which may still be 0 if RenderReflow hasn't
// run yet, e.g. on session restore via SetScrollOffset). Units are reflowed
// rows so scrolling through a wrapped chain advances one sub-row at a time.
func (t *Terminal) ScrollUp(n int) {
	if t.view.IsFollowing() {
		gi, col := t.write.Cursor()
		t.view.RecomputeLiveAnchor(t.store, gi, col, t.write.WriteTop())
	}
	t.view.ScrollUpRows(t.store, n)
}

// ScrollDown scrolls the view forward by n reflowed rows toward the live
// edge. Same RecomputeLiveAnchor pre-step as ScrollUp for identical reasons:
// the "from" anchor must be the live-edge anchor when auto-following.
func (t *Terminal) ScrollDown(n int) {
	if t.view.IsFollowing() {
		gi, col := t.write.Cursor()
		t.view.RecomputeLiveAnchor(t.store, gi, col, t.write.WriteTop())
	}
	t.view.ScrollDownRows(t.store, n, t.write.WriteBottom())
}

// ScrollToBottom snaps the view to the live edge and re-engages autoFollow.
func (t *Terminal) ScrollToBottom() { t.view.ScrollToBottom(t.write.WriteBottom()) }

// OnInput re-engages autoFollow after a user keystroke or click.
func (t *Terminal) OnInput() { t.view.OnInput(t.write.WriteBottom()) }

// EraseDisplay clears every cell in the current write window. This is
// the sparse equivalent of ESC[2J on the main screen.
func (t *Terminal) EraseDisplay() {
	t.write.EraseDisplay()
}

// RewindWriteTop forwards to the WriteWindow and resyncs the ViewWindow so
// viewBottom doesn't drift above the new writeBottom. Without the resync,
// ScrollOffset reports nonzero immediately after a rewind and input-triggered
// auto-scroll-to-bottom misbehaves. See WriteWindow.RewindWriteTop.
func (t *Terminal) RewindWriteTop(to int64) {
	t.write.RewindWriteTop(to)
	t.view.ScrollToBottom(t.write.WriteBottom())
}

// EraseLine clears the cells of the line at the cursor's current globalIdx.
// This is the sparse equivalent of ESC[2K.
func (t *Terminal) EraseLine() {
	t.write.EraseLine()
}

// EraseToEndOfLine clears cells from col to the end of the cursor's line.
func (t *Terminal) EraseToEndOfLine(col int) {
	t.write.EraseToEndOfLine(col)
}

// EraseFromStartOfLine clears cells from column 0 through col (inclusive).
func (t *Terminal) EraseFromStartOfLine(col int) {
	t.write.EraseFromStartOfLine(col)
}

// InsertLines inserts n blank lines at cursorRow within [marginTop, marginBottom]
// (all relative to the current writeTop). Lines shift down; bottom n are cleared.
func (t *Terminal) InsertLines(n, cursorRow, marginTop, marginBottom int) {
	t.write.InsertLines(n, cursorRow, marginTop, marginBottom)
}

// DeleteLines deletes n lines at cursorRow within [marginTop, marginBottom]
// (all relative to the current writeTop). Lines shift up; bottom n are cleared.
func (t *Terminal) DeleteLines(n, cursorRow, marginTop, marginBottom int) {
	t.write.DeleteLines(n, cursorRow, marginTop, marginBottom)
}

// NewlineInRegion handles a LF within a partial DECSTBM scroll region. The
// region [marginTop, marginBottom] (relative to writeTop) scrolls up by 1.
// writeTop does NOT advance — only content within the region shifts.
func (t *Terminal) NewlineInRegion(marginTop, marginBottom int) {
	t.write.NewlineInRegion(marginTop, marginBottom)
	// The writeBottom does not change when only content shifts, so no
	// ViewWindow notification is needed.
}

// SetLine overwrites the cells at globalIdx directly in the store,
// bypassing the WriteWindow's cursor / writeTop invariants. Intended for
// callers that need to mutate specific lines outside the cursor model:
// ED / EL erase operations (fill a range with the current FG/BG), ICH /
// DCH character edits (rewrite a single row in place), and overlay-insert
// paths that sync cells into a globalIdx chosen by the parser. It does
// NOT advance writeTop, touch the cursor, or update HWM; callers are
// responsible for keeping write-window state consistent with whatever
// they write here.
func (t *Terminal) SetLine(globalIdx int64, cells []parser.Cell) {
	t.store.SetLine(globalIdx, cells)
}

// ClearRange removes all lines in [lo, hi] from the store, bypassing the
// WriteWindow. Same contract as SetLine: callers take on responsibility
// for keeping writeTop / cursor / HWM consistent. Typical uses are ED J 3
// (clear scrollback above writeTop), ED-below-cursor (clear tail of the
// write window), and scroll-region rejoin that collapses logical lines.
func (t *Terminal) ClearRange(lo, hi int64) {
	t.store.ClearRange(lo, hi)
}

// SetClearNotifier wires a persistence-layer callback for range clears.
// Passing nil disables notifications. Thread-safety: callers must not race
// with ClearRangePersistent; in practice this is set once during VTerm
// EnableMemoryBuffer.
func (t *Terminal) SetClearNotifier(n parser.ClearNotifier) {
	t.notifier = n
}

// ClearRangePersistent removes lines [lo, hi] from the in-memory store AND
// notifies the persistence layer so the range is tombstoned on disk. Used by
// VTerm ED 0/1/2 and single-line invalidate. WriteWindow scroll / newline /
// scroll-region clears keep calling ClearRange (in-memory only).
func (t *Terminal) ClearRangePersistent(lo, hi int64) {
	t.store.ClearRange(lo, hi)
	if t.notifier != nil {
		t.notifier.NotifyClearRange(lo, hi)
	}
}

// ReadLine returns a copy of the cells at globalIdx. Returns nil for gaps.
func (t *Terminal) ReadLine(globalIdx int64) []parser.Cell {
	return t.store.GetLine(globalIdx)
}

// RowNoWrap reports whether the row at globalIdx is marked NoWrap.
func (t *Terminal) RowNoWrap(globalIdx int64) bool {
	return t.store.RowNoWrap(globalIdx)
}

// SetRowNoWrap marks the row at globalIdx as NoWrap. The flag is sticky
// (passing false is a no-op).
func (t *Terminal) SetRowNoWrap(globalIdx int64, nowrap bool) {
	t.store.SetRowNoWrap(globalIdx, nowrap)
}

// RestoreWriteState forcibly sets the write window's cursor and anchor,
// used during session restore. The ViewWindow is re-snapped to the new
// writeBottom in follow mode. hwm seeds writeBottomHWM only when it
// exceeds writeTop+height-1; smaller values (including zero, as written
// by older WAL entries that predate this field) fall back to that floor.
func (t *Terminal) RestoreWriteState(writeTop, cursorGlobalIdx int64, cursorCol int, hwm int64) {
	t.write.RestoreState(writeTop, cursorGlobalIdx, cursorCol, hwm)
	t.view.ScrollToBottom(t.write.WriteBottom())
}

// RestoreState implements MainScreen.RestoreState by delegating to
// RestoreWriteState.
func (t *Terminal) RestoreState(writeTop, cursorGlobalIdx int64, cursorCol int, hwm int64) {
	t.RestoreWriteState(writeTop, cursorGlobalIdx, cursorCol, hwm)
}

// WriteBottomHWM returns the write window's high-water mark for persistence.
func (t *Terminal) WriteBottomHWM() int64 {
	return t.write.WriteBottomHWM()
}

// LoadFromPageStore loads all lines from the PageStore into the sparse
// store. Called once on session restore to replay persistent scrollback.
func (t *Terminal) LoadFromPageStore(ps *parser.PageStore) error {
	return LoadStore(t.store, ps)
}

// CursorToView maps the current cursor to its viewport-relative (row, col).
// Returns ok=false if the cursor's globalIdx is outside the currently-rendered
// chain walk (e.g. scrolled far back). Callers may fall back to writeTop-
// relative projection in that case.
func (t *Terminal) CursorToView() (viewRow, viewCol int, ok bool) {
	gi, col := t.write.Cursor()
	t.view.RecomputeLiveAnchor(t.store, gi, col, t.write.WriteTop())
	return t.view.CursorToView(t.store, gi, col)
}

// RenderReflow produces the viewport via the reflow-aware ViewWindow.Render
// path. Recomputes the live anchor from the cursor's chain first so that
// autoFollow keeps the cursor on the bottom row of the viewport. This is the
// bridge method; callers switch over from Grid() in a later step.
func (t *Terminal) RenderReflow() [][]parser.Cell {
	cursorGI, cursorCol := t.write.Cursor()
	t.view.RecomputeLiveAnchor(t.store, cursorGI, cursorCol, t.write.WriteTop())
	return t.view.Render(t.store)
}

// Grid builds a dense height x width grid from the current view range by
// reading the Store. Unwritten cells and short lines are blank-padded.
//
// The returned slice is owned by the caller and safe to mutate.
//
// Grid acquires each underlying lock separately and does not hold a single
// consistent snapshot. A concurrent Resize may cause the returned grid to
// reflect a transient mix of old and new dimensions; the next call will be
// consistent.
func (t *Terminal) Grid() [][]parser.Cell {
	width := t.write.Width()
	height := t.write.Height()
	top, _ := t.view.VisibleRange()

	grid := make([][]parser.Cell, height)
	for y := 0; y < height; y++ {
		row := make([]parser.Cell, width)
		gi := top + int64(y)
		if gi >= 0 {
			line := t.store.GetLine(gi)
			for x := 0; x < width && x < len(line); x++ {
				row[x] = line[x]
			}
		}
		grid[y] = row
	}
	return grid
}

// Store returns the underlying sparse store. Intended for read-only
// scrollback fetch paths; callers must not mutate cells through it.
func (t *Terminal) Store() *Store { return t.store }

// ViewWindow returns the underlying ViewWindow. Intended for callers that
// need to query per-row globalIdxs of the last RenderReflow call (see
// ViewWindow.RowGlobalIdx). Callers must not mutate the ViewWindow's view
// state through this handle.
func (t *Terminal) ViewWindow() *ViewWindow { return t.view }

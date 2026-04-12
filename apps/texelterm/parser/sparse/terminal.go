// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import "github.com/framegrace/texelation/apps/texelterm/parser"

// Terminal is a thin composition of Store, WriteWindow, and ViewWindow. It
// exposes the API that VTerm's main-screen path calls into.
//
// Construction is eager: all three underlying types are created up-front so
// that no method has to lazy-init anything. This keeps the locking strategy
// simple — reads never upgrade to writes.
type Terminal struct {
	store *Store
	write *WriteWindow
	view  *ViewWindow
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

// WriteCell writes one cell and notifies the ViewWindow of any writeBottom
// change so auto-follow stays coherent.
func (t *Terminal) WriteCell(cell parser.Cell) {
	t.write.WriteCell(cell)
	t.view.OnWriteBottomChanged(t.write.WriteBottom())
}

// Newline advances the cursor (scrolling at bottom) and notifies the view.
func (t *Terminal) Newline() {
	t.write.Newline()
	t.view.OnWriteBottomChanged(t.write.WriteBottom())
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

// Resize resizes both the write and view windows. WriteWindow applies
// Rule 5 first; ViewWindow then applies Rule 6 observing the (possibly
// moved) writeBottom.
func (t *Terminal) Resize(newWidth, newHeight int) {
	t.write.Resize(newWidth, newHeight)
	t.view.Resize(newWidth, newHeight, t.write.WriteBottom())
}

// ScrollUp scrolls the view back by n lines and disengages autoFollow.
func (t *Terminal) ScrollUp(n int) { t.view.ScrollUp(n) }

// ScrollDown scrolls the view forward by n lines toward the live edge.
func (t *Terminal) ScrollDown(n int) { t.view.ScrollDown(n, t.write.WriteBottom()) }

// ScrollToBottom snaps the view to the live edge and re-engages autoFollow.
func (t *Terminal) ScrollToBottom() { t.view.ScrollToBottom(t.write.WriteBottom()) }

// OnInput re-engages autoFollow after a user keystroke or click.
func (t *Terminal) OnInput() { t.view.OnInput(t.write.WriteBottom()) }

// EraseDisplay clears every cell in the current write window. This is
// the sparse equivalent of ESC[2J on the main screen.
func (t *Terminal) EraseDisplay() {
	t.write.EraseDisplay()
}

// EraseLine clears the cells of the line at the cursor's current globalIdx.
// This is the sparse equivalent of ESC[2K.
func (t *Terminal) EraseLine() {
	t.write.EraseLine()
}

// ReadLine returns a copy of the cells at globalIdx. Returns nil for gaps.
func (t *Terminal) ReadLine(globalIdx int64) []parser.Cell {
	return t.store.GetLine(globalIdx)
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

// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"sync"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// WriteWindow is the TUI-facing portion of a sparse terminal. It owns the
// cursor and the writeTop anchor, and it forwards writes to an underlying
// Store.
//
// Applications issue cursor-relative writes: ESC[row;colH resolves to
// (writeTop + row - 1, col - 1). The addressable area is the closed range
// [writeTop, writeBottom], where writeBottom is derived as writeTop + height - 1.
//
// WriteWindow is safe for concurrent use. Callers that need to observe
// window-move events should consult WriteTop/WriteBottom after each call.
type WriteWindow struct {
	mu     sync.Mutex
	store  *Store
	width  int
	height int

	writeTop        int64
	cursorGlobalIdx int64
	cursorCol       int
}

// NewWriteWindow creates a WriteWindow anchored at globalIdx 0 with the given
// dimensions. The cursor starts at (writeTop, 0).
func NewWriteWindow(store *Store, width, height int) *WriteWindow {
	return &WriteWindow{
		store:  store,
		width:  width,
		height: height,
	}
}

// Width returns the current column width.
func (w *WriteWindow) Width() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.width
}

// Height returns the current row height.
func (w *WriteWindow) Height() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.height
}

// WriteTop returns the globalIdx of the top row of the write window.
func (w *WriteWindow) WriteTop() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writeTop
}

// WriteBottom returns the globalIdx of the bottom row of the write window.
func (w *WriteWindow) WriteBottom() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writeTop + int64(w.height) - 1
}

// Cursor returns the current cursor position as (globalIdx, col).
func (w *WriteWindow) Cursor() (globalIdx int64, col int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cursorGlobalIdx, w.cursorCol
}

// WriteCell writes one cell at the current cursor position and advances the
// cursor column by one. This method does NOT handle line wrap — the caller
// (typically the Parser layer) is responsible for wrap semantics.
func (w *WriteWindow) WriteCell(cell parser.Cell) {
	w.mu.Lock()
	gi := w.cursorGlobalIdx
	col := w.cursorCol
	if cell.Wide {
		w.cursorCol += 2
	} else {
		w.cursorCol++
	}
	w.mu.Unlock()

	w.store.Set(gi, col, cell)
	if cell.Wide {
		// Place a zero-rune placeholder in the adjacent column so the grid
		// reflects the 2-cell wide character correctly.
		placeholder := parser.Cell{Rune: 0, FG: cell.FG, BG: cell.BG, Attr: cell.Attr}
		w.store.Set(gi, col+1, placeholder)
	}
}

// CarriageReturn resets the cursor column to 0. The cursor globalIdx is
// unchanged.
func (w *WriteWindow) CarriageReturn() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cursorCol = 0
}

// SetCursor places the cursor at row, col relative to the write window.
// Rows are clamped to [0, height-1]; cols to [0, width-1].
func (w *WriteWindow) SetCursor(row, col int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if row < 0 {
		row = 0
	}
	if row >= w.height {
		row = w.height - 1
	}
	if col < 0 {
		col = 0
	}
	if col >= w.width {
		col = w.width - 1
	}
	w.cursorGlobalIdx = w.writeTop + int64(row)
	w.cursorCol = col
}

// CursorRow returns the cursor's row relative to the write window top.
func (w *WriteWindow) CursorRow() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return int(w.cursorGlobalIdx - w.writeTop)
}

// Newline advances the cursor to the next row. If the cursor is already at
// the bottom row of the write window, writeTop is advanced by 1 (classical
// scroll-up). Cells at the old writeTop remain in the Store — they are now
// "historical" simply because the window moved, not because they were copied.
// The cursor column is reset to 0 (combined CR+LF semantics of LF in most
// terminal modes; pure LF variants are handled by the parser, not here).
func (w *WriteWindow) Newline() {
	w.mu.Lock()
	defer w.mu.Unlock()
	writeBottom := w.writeTop + int64(w.height) - 1
	if w.cursorGlobalIdx >= writeBottom {
		// At or below bottom — scroll up.
		w.writeTop++
		w.cursorGlobalIdx = w.writeTop + int64(w.height) - 1
	} else {
		w.cursorGlobalIdx++
	}
	w.cursorCol = 0
}

// Resize applies Rule 5 from the design spec.
//
// Grow: writeTop retreats by the grow delta, clamped at 0. No cells are
// cleared. The new top rows of the window expose whatever is already stored
// there (old scrollback, or blank if none).
//
// Shrink: cursor-minimum-advance — writeTop advances by the minimum amount
// needed to keep the cursor inside the new write window. Cells below the
// new writeBottom are cleared. Cells in [oldWriteTop, newWriteTop) stay in
// the Store and become "above the window" (scrollback).
//
// Pure width changes (newHeight == height) apply only to width without
// touching writeTop.
func (w *WriteWindow) Resize(newWidth, newHeight int) {
	if newWidth <= 0 || newHeight <= 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if newHeight > w.height {
		w.resizeGrowLocked(newHeight)
	} else if newHeight < w.height {
		w.resizeShrinkLocked(newHeight)
	}
	w.width = newWidth
	w.height = newHeight

	// Keep cursor in bounds.
	if w.cursorGlobalIdx < w.writeTop {
		w.cursorGlobalIdx = w.writeTop
	}
	if bottom := w.writeTop + int64(w.height) - 1; w.cursorGlobalIdx > bottom {
		w.cursorGlobalIdx = bottom
	}
	if w.cursorCol >= w.width {
		w.cursorCol = w.width - 1
	}
}

// resizeGrowLocked assumes w.mu is held.
func (w *WriteWindow) resizeGrowLocked(newHeight int) {
	delta := int64(newHeight - w.height)
	newTop := w.writeTop - delta
	if newTop < 0 {
		newTop = 0
	}
	w.writeTop = newTop
}

// resizeShrinkLocked implements Rule 5 shrink: cursor-minimum-advance.
// Preconditions: w.mu held, newHeight < w.height.
func (w *WriteWindow) resizeShrinkLocked(newHeight int) {
	oldWriteBottom := w.writeTop + int64(w.height) - 1
	// Tentative newWriteBottom if writeTop didn't move.
	tentativeBottom := w.writeTop + int64(newHeight) - 1

	advance := int64(0)
	if w.cursorGlobalIdx > tentativeBottom {
		advance = w.cursorGlobalIdx - tentativeBottom
	}
	w.writeTop += advance
	newWriteBottom := w.writeTop + int64(newHeight) - 1

	// Cells [newWriteBottom+1, oldWriteBottom] are scratch space below the
	// new window. Clear them.
	if oldWriteBottom > newWriteBottom {
		// ClearRange is called while w.mu is held (unlike WriteCell/EraseDisplay which
		// release first). This is intentional: newWriteBottom is derived from the
		// already-updated w.writeTop, so the mutation and the clear must be atomic
		// with respect to w.mu. The call order (WriteWindow.mu → Store.mu) is safe
		// per the design's acyclic lock acquisition rule.
		w.store.ClearRange(newWriteBottom+1, oldWriteBottom)
	}

	// Cells in [oldWriteTop, writeTop) (only when advance > 0) stay in the
	// store. They are now "above the window" — scrollback. No action needed.
}

// EraseDisplay clears every cell in the current write window [writeTop,
// writeBottom]. Cells outside the window are not touched.
func (w *WriteWindow) EraseDisplay() {
	w.mu.Lock()
	top := w.writeTop
	bottom := w.writeTop + int64(w.height) - 1
	w.mu.Unlock()
	w.store.ClearRange(top, bottom)
}

// EraseLine clears the line at the cursor's current globalIdx.
func (w *WriteWindow) EraseLine() {
	w.mu.Lock()
	gi := w.cursorGlobalIdx
	w.mu.Unlock()
	w.store.ClearRange(gi, gi)
}

// EraseToEndOfLine clears cells from col to the end of the current line.
func (w *WriteWindow) EraseToEndOfLine(col int) {
	w.mu.Lock()
	gi := w.cursorGlobalIdx
	width := w.width
	w.mu.Unlock()
	for x := col; x < width; x++ {
		w.store.Set(gi, x, parser.Cell{})
	}
}

// EraseFromStartOfLine clears cells from column 0 through col (inclusive).
func (w *WriteWindow) EraseFromStartOfLine(col int) {
	w.mu.Lock()
	gi := w.cursorGlobalIdx
	w.mu.Unlock()
	for x := 0; x <= col; x++ {
		w.store.Set(gi, x, parser.Cell{})
	}
}

// RestoreState forcibly sets writeTop and cursor, used during session
// restore. Do not call during normal operation.
func (w *WriteWindow) RestoreState(writeTop, cursorGlobalIdx int64, cursorCol int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeTop = writeTop
	w.cursorGlobalIdx = cursorGlobalIdx
	w.cursorCol = cursorCol
}

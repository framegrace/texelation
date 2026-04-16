// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"log"
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

	// writeBottomHWM is the high-water mark of writeBottom. It tracks the
	// furthest writeBottom has ever reached through normal operation (Newline
	// scrolling) or expansion. Shrinking may temporarily drop writeBottom
	// below HWM (cursor-fits absorbs empty rows), but expansion always
	// anchors against HWM so that writeTop never retreats into scrollback.
	writeBottomHWM int64
}

// NewWriteWindow creates a WriteWindow anchored at globalIdx 0 with the given
// dimensions. The cursor starts at (writeTop, 0).
func NewWriteWindow(store *Store, width, height int) *WriteWindow {
	return &WriteWindow{
		store:          store,
		width:          width,
		height:         height,
		writeBottomHWM: int64(height - 1),
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
	// Update HWM after potential scroll.
	w.extendHWMLocked(w.writeTop + int64(w.height) - 1)
}

// extendHWMLocked advances writeBottomHWM to newBottom if newBottom is
// greater. Caller must hold w.mu. HWM never moves backwards — a persisted
// HWM across reload, a grow-on-resize, or a defensive bump from an op that
// touched a row past the nominal writeBottom all funnel through here.
func (w *WriteWindow) extendHWMLocked(newBottom int64) {
	if newBottom > w.writeBottomHWM {
		w.writeBottomHWM = newBottom
	}
}

// Resize changes the write window dimensions.
//
// Shrink: empty space below the cursor absorbs the shrink first (writeTop
// stays, writeBottom drops). When the cursor doesn't fit, writeTop advances
// just enough to keep it at the window's bottom row — this hides history
// from the top, like moving the top border down.
//
// Expand: anchored against the high-water mark of writeBottom. This ensures
// writeTop never retreats past the point the window originally occupied,
// preventing a TUI's ESC[2J from destroying scrollback on expand. When
// there is no history (writeTop would go negative), it clamps to 0.
//
// The cursor is clamped into the new window; the TUI's SIGWINCH handler
// will reposition it.
func (w *WriteWindow) Resize(newWidth, newHeight int) {
	if newWidth <= 0 || newHeight <= 0 {
		// Silently ignoring zero dimensions used to be a dead end for
		// diagnosing broken SIGWINCH propagation — the window stayed at
		// its previous size with no trail. Log so at least the symptom
		// shows up in the terminal's own log.
		log.Printf("[sparse] WriteWindow.Resize ignored: newWidth=%d newHeight=%d (both must be > 0)",
			newWidth, newHeight)
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	oldHeight := w.height
	w.width = newWidth
	w.height = newHeight

	if newHeight < oldHeight {
		// Shrink: absorb empty space below cursor first.
		if w.cursorGlobalIdx-w.writeTop >= int64(newHeight) {
			// Cursor doesn't fit — advance writeTop to keep cursor visible.
			w.writeTop = w.cursorGlobalIdx - int64(newHeight) + 1
		}
		// Otherwise writeTop stays — shrink eats empty space from bottom.
	} else if newHeight > oldHeight {
		// Expand: anchor against HWM so writeTop doesn't retreat into
		// scrollback that a TUI erase would destroy.
		w.writeTop = w.writeBottomHWM - int64(newHeight) + 1
		if w.writeTop < 0 {
			w.writeTop = 0
		}
	}

	// Update HWM if the new writeBottom exceeds it.
	w.extendHWMLocked(w.writeTop + int64(w.height) - 1)

	// Clamp cursor into the new window.
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

// InsertLines inserts n blank lines at cursorRow within [marginTop, marginBottom].
// Lines from cursorRow..marginBottom-n shift down; bottom n lines are cleared.
// The write window anchor and cursor are not moved — IL does not scroll.
// cursorRow, marginTop, marginBottom are all relative to writeTop.
//
// Invariant: marginBottom < height. HWM is bumped defensively to
// writeTop+marginBottom in case a caller violates that, so the "furthest
// row the window has touched" stays monotonic even across misuse.
func (w *WriteWindow) InsertLines(n, cursorRow, marginTop, marginBottom int) {
	if n <= 0 {
		return
	}
	w.mu.Lock()
	base := w.writeTop
	w.extendHWMLocked(base + int64(marginBottom))
	w.mu.Unlock()

	// Shift lines down within [cursorRow, marginBottom].
	for y := marginBottom; y >= cursorRow+n; y-- {
		w.store.SetLine(base+int64(y), w.store.GetLine(base+int64(y-n)))
	}
	// Clear the inserted rows.
	for y := cursorRow; y < cursorRow+n && y <= marginBottom; y++ {
		w.store.ClearRange(base+int64(y), base+int64(y))
	}
}

// DeleteLines deletes n lines at cursorRow within [marginTop, marginBottom].
// Lines from cursorRow+n..marginBottom shift up; bottom n lines are cleared.
// The write window anchor and cursor are not moved.
// cursorRow, marginTop, marginBottom are all relative to writeTop.
//
// Invariant: marginBottom < height. Defensive HWM bump as in InsertLines.
func (w *WriteWindow) DeleteLines(n, cursorRow, marginTop, marginBottom int) {
	if n <= 0 {
		return
	}
	w.mu.Lock()
	base := w.writeTop
	w.extendHWMLocked(base + int64(marginBottom))
	w.mu.Unlock()

	// Shift lines up within [cursorRow, marginBottom].
	for y := cursorRow; y <= marginBottom-n; y++ {
		w.store.SetLine(base+int64(y), w.store.GetLine(base+int64(y+n)))
	}
	// Clear the vacated bottom rows.
	clearStart := marginBottom - n + 1
	if clearStart < cursorRow {
		clearStart = cursorRow
	}
	for y := clearStart; y <= marginBottom; y++ {
		w.store.ClearRange(base+int64(y), base+int64(y))
	}
}

// NewlineInRegion handles a line-feed within a partial DECSTBM scroll region
// [marginTop, marginBottom] (both relative to writeTop). The content within the
// region scrolls up by 1: line at marginTop is lost, lines shift up, and
// marginBottom is cleared. writeTop does NOT advance — only content within the
// region is affected.
//
// Call Newline() instead for full-screen (marginTop==0 AND marginBottom==height-1).
//
// Invariant: marginBottom < height. Defensive HWM bump as in InsertLines.
func (w *WriteWindow) NewlineInRegion(marginTop, marginBottom int) {
	w.mu.Lock()
	base := w.writeTop
	w.extendHWMLocked(base + int64(marginBottom))
	w.mu.Unlock()

	// Shift lines up within the region.
	for y := marginTop; y < marginBottom; y++ {
		w.store.SetLine(base+int64(y), w.store.GetLine(base+int64(y+1)))
	}
	// Clear the bottom line of the region.
	w.store.ClearRange(base+int64(marginBottom), base+int64(marginBottom))
}

// WriteBottomHWM returns the high-water mark of writeBottom. Exposed so
// that callers (session save) can persist HWM across reload and prevent
// a grown viewport from retreating into scrollback. See Resize() for why
// HWM is load-bearing on expand.
func (w *WriteWindow) WriteBottomHWM() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writeBottomHWM
}

// RestoreState forcibly sets writeTop and cursor, used during session
// restore. Do not call during normal operation. If hwm >= 0 the supplied
// value seeds writeBottomHWM (the high-water mark cannot move backwards,
// so a persisted HWM from a prior session must be honored); otherwise
// HWM is derived from writeTop+height-1 as in fresh construction.
func (w *WriteWindow) RestoreState(writeTop, cursorGlobalIdx int64, cursorCol int, hwm int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeTop = writeTop
	w.cursorGlobalIdx = cursorGlobalIdx
	w.cursorCol = cursorCol
	// Seed HWM: take the max of the restored value and the current
	// writeBottom so the invariant HWM >= writeBottom always holds.
	minHWM := w.writeTop + int64(w.height) - 1
	if hwm > minHWM {
		w.writeBottomHWM = hwm
	} else {
		w.writeBottomHWM = minHWM
	}
}

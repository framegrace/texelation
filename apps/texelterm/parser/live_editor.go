// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/live_editor.go
// Summary: LiveEditor manages editing at the live edge of the terminal.
// It is the single source of truth for the current (uncommitted) line
// and cursor position, with physical position derived on demand.

package parser

// LiveEditor manages editing of the current (uncommitted) logical line.
// It owns the cursor position as a logical offset (character position within
// the line), and derives physical screen coordinates on demand.
//
// This is the single source of truth for:
// - Current line content (not yet committed to history)
// - Cursor position (as character offset)
//
// Physical cursor position (x, y) is computed from the logical offset
// based on the terminal width, handling line wrapping correctly.
type LiveEditor struct {
	// line is the current logical line being edited (uncommitted)
	line *LogicalLine

	// cursorOffset is the cursor position as a character offset within the line.
	// This is the SINGLE SOURCE OF TRUTH for cursor position.
	// Range: [0, line.Len()] - cursor can be at the end (append position)
	cursorOffset int
}

// NewLiveEditor creates a new LiveEditor with an empty line and cursor at position 0.
func NewLiveEditor() *LiveEditor {
	return &LiveEditor{
		line:         NewLogicalLine(),
		cursorOffset: 0,
	}
}

// --- Editing Operations ---

// WriteChar writes a character at the current cursor position and advances the cursor.
// If insertMode is true, existing content is shifted right; otherwise it's overwritten.
func (e *LiveEditor) WriteChar(r rune, fg, bg Color, attr Attribute, insertMode bool) {
	cell := Cell{Rune: r, FG: fg, BG: bg, Attr: attr}

	if insertMode {
		e.line.InsertCell(e.cursorOffset, cell)
	} else {
		e.line.SetCell(e.cursorOffset, cell)
	}

	e.cursorOffset++
}

// DeleteChars deletes n characters at the current cursor position, shifting content left.
// Used for DCH (Delete Character) - CSI P.
func (e *LiveEditor) DeleteChars(n int) {
	if e.cursorOffset >= e.line.Len() {
		return // Nothing to delete at or beyond end
	}

	deleteCount := n
	remaining := e.line.Len() - e.cursorOffset
	if deleteCount > remaining {
		deleteCount = remaining
	}

	if deleteCount <= 0 {
		return
	}

	// Shift content left
	newLen := e.line.Len() - deleteCount
	if e.cursorOffset+deleteCount < e.line.Len() {
		copy(e.line.Cells[e.cursorOffset:], e.line.Cells[e.cursorOffset+deleteCount:])
	}
	e.line.Cells = e.line.Cells[:newLen]
}

// EraseChars replaces n characters at the current position with spaces.
// Used for ECH (Erase Character) - CSI X.
func (e *LiveEditor) EraseChars(n int, fg, bg Color) {
	for i := 0; i < n; i++ {
		pos := e.cursorOffset + i
		if pos < e.line.Len() {
			e.line.Cells[pos] = Cell{Rune: ' ', FG: fg, BG: bg}
		}
	}
}

// EraseToEnd truncates the line from the cursor position to the end.
// Used for EL 0 (Erase to End of Line).
func (e *LiveEditor) EraseToEnd() {
	e.line.Truncate(e.cursorOffset)
}

// EraseFromStart replaces characters from the start of the line to the cursor with spaces.
// Used for EL 1 (Erase from Start of Line to Cursor) - erases positions 0 through cursorOffset inclusive.
func (e *LiveEditor) EraseFromStart(fg, bg Color) {
	// Determine the end position (inclusive, but clamped to actual line content)
	endPos := e.cursorOffset
	if endPos >= e.line.Len() {
		endPos = e.line.Len() - 1
	}
	for i := 0; i <= endPos; i++ {
		e.line.Cells[i] = Cell{Rune: ' ', FG: fg, BG: bg}
	}
}

// EraseLine clears the entire line and resets cursor to position 0.
// Used for EL 2 (Erase Entire Line).
func (e *LiveEditor) EraseLine() {
	e.line.Clear()
	e.cursorOffset = 0
}

// --- Cursor Operations (Logical - Source of Truth) ---

// SetCursorOffset sets the cursor position to the given character offset.
// The offset is clamped to the valid range [0, line.Len()].
// Cursor CAN be at line.Len() (the append position).
func (e *LiveEditor) SetCursorOffset(offset int) {
	if offset < 0 {
		offset = 0
	}
	// Allow cursor to go beyond line length (for void space / future typing)
	// but for most operations we'll clamp to line.Len()
	e.cursorOffset = offset
}

// MoveCursor moves the cursor by a relative offset (can be negative).
// The result is clamped to the valid range [0, line.Len()].
func (e *LiveEditor) MoveCursor(delta int) {
	newOffset := e.cursorOffset + delta
	if newOffset < 0 {
		newOffset = 0
	}
	e.cursorOffset = newOffset
}

// GetCursorOffset returns the current cursor position as a character offset.
func (e *LiveEditor) GetCursorOffset() int {
	return e.cursorOffset
}

// --- Physical Cursor Derivation ---

// GetPhysicalCursor returns the physical screen position (row, col) for the cursor
// given the terminal width. The row is relative to the start of this line
// (row 0 is the first physical line of this logical line).
//
// The column is the x-position within that physical row.
//
// Examples (width=10):
//   - Line "Hello", cursor at 3 → (row=0, col=3)
//   - Line "Hello World!", cursor at 11 → (row=1, col=1)
//   - Empty line, cursor at 0 → (row=0, col=0)
func (e *LiveEditor) GetPhysicalCursor(width int) (row, col int) {
	if width <= 0 {
		width = 80 // Fallback
	}

	// Special case: empty line or cursor at start
	if e.cursorOffset == 0 {
		return 0, 0
	}

	// Calculate which physical row and column the cursor is on
	// Row = offset / width (integer division)
	// Col = offset % width
	row = e.cursorOffset / width
	col = e.cursorOffset % width

	return row, col
}

// SetCursorFromPhysical sets the cursor offset based on a physical position.
// This is used when the user clicks with the mouse or when escape sequences
// move the cursor to a physical position.
//
// Parameters:
//   - physRow: the physical row relative to the start of this line (0-based)
//   - physCol: the column within that row (0-based)
//   - width: the terminal width
//
// The resulting offset may be beyond the current line length (void space).
func (e *LiveEditor) SetCursorFromPhysical(physRow, physCol, width int) {
	if width <= 0 {
		width = 80
	}
	if physRow < 0 {
		physRow = 0
	}
	if physCol < 0 {
		physCol = 0
	}
	if physCol >= width {
		physCol = width - 1
	}

	e.cursorOffset = physRow*width + physCol
}

// --- Content Access ---

// GetPhysicalLines returns the current line wrapped to the given width.
// Each PhysicalLine has LogicalIndex=-1 (indicating uncommitted line).
func (e *LiveEditor) GetPhysicalLines(width int) []PhysicalLine {
	physical := e.line.WrapToWidth(width)
	// Mark all as uncommitted (-1)
	for i := range physical {
		physical[i].LogicalIndex = -1
	}
	return physical
}

// Commit returns the current line for adding to history and starts a fresh line.
// The cursor is reset to position 0.
// The caller is responsible for appending the returned line to history.
func (e *LiveEditor) Commit() *LogicalLine {
	committed := e.line.Clone()
	e.line = NewLogicalLine()
	e.cursorOffset = 0
	return committed
}

// RestoreLine replaces the current (empty) line with a previously committed line.
// This is used for "uncommitting" lines during bash redraw sequences.
// The cursor is placed at the start of the restored line.
func (e *LiveEditor) RestoreLine(line *LogicalLine) {
	if line != nil {
		e.line = line.Clone()
	} else {
		e.line = NewLogicalLine()
	}
	e.cursorOffset = 0
}

// Line returns a read-only reference to the current logical line.
func (e *LiveEditor) Line() *LogicalLine {
	return e.line
}

// Len returns the length of the current line in characters.
func (e *LiveEditor) Len() int {
	return e.line.Len()
}

// Clear resets the line to empty and cursor to position 0.
func (e *LiveEditor) Clear() {
	e.line.Clear()
	e.cursorOffset = 0
}

// --- Advanced Operations ---

// CarriageReturn moves the cursor to the beginning of the line (offset 0).
// This does NOT commit the line - that's LineFeed's job.
func (e *LiveEditor) CarriageReturn() {
	e.cursorOffset = 0
}

// Backspace moves the cursor back one position (if possible).
// It does NOT delete the character - shells typically send BS followed by EL.
// Returns true if the cursor actually moved.
func (e *LiveEditor) Backspace() bool {
	if e.cursorOffset > 0 {
		e.cursorOffset--
		return true
	}
	return false
}

// Tab advances the cursor to the next tab stop.
// tabStops is a map of column positions that are tab stops.
// width is the terminal width.
// Returns the new cursor offset.
// If no tab stop is found on the current row, the cursor wraps to the start of the next row.
func (e *LiveEditor) Tab(tabStops map[int]bool, width int) int {
	if width <= 0 {
		width = 80
	}

	// Get current column within the physical row
	_, col := e.GetPhysicalCursor(width)

	// Find next tab stop in current row
	for x := col + 1; x < width; x++ {
		if tabStops[x] {
			// Move cursor forward by the difference
			e.cursorOffset += (x - col)
			return e.cursorOffset
		}
	}

	// No tab stop found - wrap to start of next row (standard VT100 behavior)
	nextRowOffset := ((e.cursorOffset / width) + 1) * width
	e.cursorOffset = nextRowOffset
	return e.cursorOffset
}

// ExtendToOffset ensures the line has content up to the given offset.
// Fills with spaces if needed. Used when cursor moves beyond line content.
func (e *LiveEditor) ExtendToOffset(offset int, fg, bg Color) {
	for e.line.Len() <= offset {
		e.line.Append(Cell{Rune: ' ', FG: fg, BG: bg})
	}
}

// InsertChars inserts n blank spaces at the current position, shifting content right.
// Used for ICH (Insert Character) - CSI @.
func (e *LiveEditor) InsertChars(n int, fg, bg Color) {
	blankCell := Cell{Rune: ' ', FG: fg, BG: bg}
	for i := 0; i < n; i++ {
		e.line.InsertCell(e.cursorOffset, blankCell)
	}
}

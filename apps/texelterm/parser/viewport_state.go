// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/viewport_state.go
// Summary: ViewportState manages the writable terminal viewport grid.
//
// This implements a "page editor" model where the viewport is always a
// writable grid (like altBuffer). The cursor can move anywhere without
// restrictions. Lines are committed to history when they scroll off
// the top of the viewport.

package parser

// ViewportState manages the physical terminal viewport.
// It provides a writable grid similar to altBuffer, with cursor tracking
// and logical line metadata for proper history commits.
type ViewportState struct {
	// grid is the physical viewport - writable anywhere (like altBuffer)
	// Dimensions: [height][width]
	grid [][]Cell

	// cursorX, cursorY are the cursor position in viewport coordinates
	cursorX, cursorY int

	// width, height are the viewport dimensions
	width, height int

	// rowMeta tracks logical line boundaries for each physical row
	// This enables proper reflow on resize and correct history commits
	rowMeta []RowMetadata

	// history is where committed lines go (for scrollback)
	history *ScrollbackHistory

	// atLiveEdge indicates whether viewport is showing live content
	// When false, user has scrolled back into history
	atLiveEdge bool

	// scrollOffset tracks how far back we've scrolled (0 = live edge)
	scrollOffset int

	// lastPromptLine is the global history line index where the last prompt started.
	// Used for seamless recovery - shell restarts at this position.
	// -1 means no prompt position has been recorded.
	lastPromptLine int64

	// lastPromptHeight is the number of lines in the prompt (for multiline prompts).
	// Calculated from OSC 133;A to OSC 133;B. Default is 1.
	lastPromptHeight int

	// promptStartRow is the viewport row where the current prompt started (OSC 133;A).
	// Used to calculate prompt height when OSC 133;B fires.
	promptStartRow int

	// eraseBG is the background color for erase operations
	eraseBG Color

	// debugLog is an optional logging function for debugging
	debugLog func(format string, args ...interface{})
}

// RowMetadata tracks logical line information for each physical row.
type RowMetadata struct {
	// LogicalLineID identifies which logical line this row belongs to.
	// Rows with the same ID are part of the same wrapped line.
	// -1 means this row hasn't been assigned to a logical line yet.
	LogicalLineID int64

	// IsFirstRow is true if this is the first physical row of a logical line.
	// Used to identify line boundaries during extraction.
	IsFirstRow bool

	// IsContinuation is true if this row continues from the previous row
	// (i.e., the line wrapped). Opposite of IsFirstRow for non-first rows.
	IsContinuation bool

	// State tracks the line lifecycle
	State LineState

	// FromHistory is true if this row was populated from scrollback history.
	// Such rows should never be re-committed by CommitViewportAsFixedWidth()
	// to avoid creating duplicate entries in history.
	FromHistory bool
}

// LineState tracks whether a row's content should be committed to history.
type LineState int

const (
	// LineStateDirty means the row has been modified and not yet committed
	LineStateDirty LineState = iota
	// LineStateClean means the row content matches what's in history
	LineStateClean
	// LineStateCommitted means the row has been committed to history
	LineStateCommitted
)

// NewViewportState creates a new viewport with the given dimensions.
func NewViewportState(width, height int, history *ScrollbackHistory) *ViewportState {
	vs := &ViewportState{
		width:            width,
		height:           height,
		history:          history,
		atLiveEdge:       true,
		lastPromptLine:   -1, // No prompt recorded yet
		lastPromptHeight: 1,  // Default to single-line prompt
		promptStartRow:   -1, // No prompt in progress
		eraseBG:          DefaultBG,
	}
	vs.initGrid()
	return vs
}

// initGrid initializes the grid and row metadata.
func (vs *ViewportState) initGrid() {
	vs.grid = make([][]Cell, vs.height)
	vs.rowMeta = make([]RowMetadata, vs.height)
	for y := 0; y < vs.height; y++ {
		vs.grid[y] = vs.makeEmptyRow()
		vs.rowMeta[y] = RowMetadata{
			LogicalLineID:  -1,
			IsFirstRow:     true,
			IsContinuation: false,
			State:          LineStateClean,
		}
	}
}

// makeEmptyRow creates a row filled with empty cells.
func (vs *ViewportState) makeEmptyRow() []Cell {
	row := make([]Cell, vs.width)
	for x := 0; x < vs.width; x++ {
		// Use eraseBG for proper background color handling during scroll/erase operations
		row[x] = Cell{Rune: ' ', FG: DefaultFG, BG: vs.eraseBG}
	}
	return row
}

// SetDebugLog sets an optional debug logging function.
func (vs *ViewportState) SetDebugLog(fn func(format string, args ...interface{})) {
	vs.debugLog = fn
}

// --- Writing Operations ---

// Write writes a character at the current cursor position.
func (vs *ViewportState) Write(r rune, fg, bg Color, attr Attribute, insertMode bool) {
	vs.WriteWide(r, fg, bg, attr, insertMode, false)
}

// WriteWide writes a character at the current cursor position with wide character support.
func (vs *ViewportState) WriteWide(r rune, fg, bg Color, attr Attribute, insertMode bool, isWide bool) {
	if vs.cursorY < 0 || vs.cursorY >= vs.height {
		return
	}
	if vs.cursorX < 0 || vs.cursorX >= vs.width {
		return
	}

	charWidth := 1
	if isWide {
		charWidth = 2
	}

	if insertMode {
		// Shift content right within the row by charWidth
		row := vs.grid[vs.cursorY]
		if vs.cursorX+charWidth < vs.width {
			copy(row[vs.cursorX+charWidth:], row[vs.cursorX:vs.width-charWidth])
		}
	}

	// Debug: log character writes to help diagnose cursor positioning issues
	if vs.debugLog != nil {
		vs.debugLog("[VS.Write] '%c' (0x%04X) at (%d,%d) wide=%v", r, r, vs.cursorX, vs.cursorY, isWide)
	}

	// Place the main character
	vs.grid[vs.cursorY][vs.cursorX] = Cell{Rune: r, FG: fg, BG: bg, Attr: attr, Wide: isWide}

	// For wide characters, place a placeholder in the next cell
	if isWide && vs.cursorX+1 < vs.width {
		vs.grid[vs.cursorY][vs.cursorX+1] = Cell{Rune: 0, FG: fg, BG: bg, Attr: attr, Wide: true}
	}

	// Mark row as dirty
	vs.rowMeta[vs.cursorY].State = LineStateDirty

	// If writing at column 0 and this row was a continuation, break the chain.
	// This happens when shell starts a new prompt on a row that had history content.
	// The new content should be a fresh logical line, not a continuation.
	if vs.cursorX == 0 && vs.rowMeta[vs.cursorY].IsContinuation {
		vs.rowMeta[vs.cursorY].IsContinuation = false
		vs.rowMeta[vs.cursorY].IsFirstRow = true
	}

	// Advance cursor by character width (like old LiveEditor.WriteChar behavior)
	vs.cursorX += charWidth
	// Note: wrapping is handled by VTerm, not here
}

// SetCursor moves the cursor to the given position.
// No restrictions - cursor can move anywhere within viewport bounds.
func (vs *ViewportState) SetCursor(x, y int) {
	if vs.debugLog != nil {
		vs.debugLog("[VS.SetCursor] (%d, %d) -> (%d, %d)", vs.cursorX, vs.cursorY, x, y)
	}
	if x < 0 {
		x = 0
	}
	if x >= vs.width {
		x = vs.width - 1
	}
	if y < 0 {
		y = 0
	}
	if y >= vs.height {
		y = vs.height - 1
	}
	vs.cursorX = x
	vs.cursorY = y
}

// Cursor returns the current cursor position.
func (vs *ViewportState) Cursor() (x, y int) {
	return vs.cursorX, vs.cursorY
}

// Grid returns the viewport grid for rendering.
func (vs *ViewportState) Grid() [][]Cell {
	return vs.grid
}

// Width returns the viewport width.
func (vs *ViewportState) Width() int {
	return vs.width
}

// Height returns the viewport height.
func (vs *ViewportState) Height() int {
	return vs.height
}

// AtLiveEdge returns true if viewport is showing live content.
func (vs *ViewportState) AtLiveEdge() bool {
	return vs.atLiveEdge
}

// ScrollToLiveEdge returns to showing live content.
func (vs *ViewportState) ScrollToLiveEdge() {
	vs.atLiveEdge = true
	vs.scrollOffset = 0
}

// --- Cell Operations ---

// SetCell sets a specific cell in the viewport.
func (vs *ViewportState) SetCell(x, y int, cell Cell) {
	if y < 0 || y >= vs.height || x < 0 || x >= vs.width {
		return
	}
	vs.grid[y][x] = cell
	vs.rowMeta[y].State = LineStateDirty
}

// GetCell returns the cell at the given position.
func (vs *ViewportState) GetCell(x, y int) Cell {
	if y < 0 || y >= vs.height || x < 0 || x >= vs.width {
		return Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
	}
	return vs.grid[y][x]
}

// ClearRow clears a specific row with spaces.
func (vs *ViewportState) ClearRow(y int) {
	if y < 0 || y >= vs.height {
		return
	}
	for x := 0; x < vs.width; x++ {
		vs.grid[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: vs.eraseBG}
	}
	vs.rowMeta[y].State = LineStateDirty
}

// SetEraseColor sets the background color for erase operations.
func (vs *ViewportState) SetEraseColor(bg Color) {
	vs.eraseBG = bg
}

// --- Erase Operations ---

// EraseToEndOfLine clears from cursor to end of row.
func (vs *ViewportState) EraseToEndOfLine() {
	if vs.cursorY < 0 || vs.cursorY >= vs.height {
		return
	}
	for x := vs.cursorX; x < vs.width; x++ {
		vs.grid[vs.cursorY][x] = Cell{Rune: ' ', FG: DefaultFG, BG: vs.eraseBG}
	}
	vs.rowMeta[vs.cursorY].State = LineStateDirty
}

// EraseFromStartOfLine clears from start of row to cursor (inclusive).
func (vs *ViewportState) EraseFromStartOfLine() {
	if vs.cursorY < 0 || vs.cursorY >= vs.height {
		return
	}
	for x := 0; x <= vs.cursorX && x < vs.width; x++ {
		vs.grid[vs.cursorY][x] = Cell{Rune: ' ', FG: DefaultFG, BG: vs.eraseBG}
	}
	vs.rowMeta[vs.cursorY].State = LineStateDirty
}

// EraseLine clears the entire current row.
func (vs *ViewportState) EraseLine() {
	if vs.cursorY < 0 || vs.cursorY >= vs.height {
		return
	}
	for x := 0; x < vs.width; x++ {
		vs.grid[vs.cursorY][x] = Cell{Rune: ' ', FG: DefaultFG, BG: vs.eraseBG}
	}
	vs.rowMeta[vs.cursorY].State = LineStateDirty
}

// EraseScreen clears the entire viewport.
func (vs *ViewportState) EraseScreen() {
	for y := 0; y < vs.height; y++ {
		for x := 0; x < vs.width; x++ {
			vs.grid[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: vs.eraseBG}
		}
		// Reset row metadata - each row starts as a fresh line
		vs.rowMeta[y] = RowMetadata{
			LogicalLineID:  -1,
			IsFirstRow:     true,
			IsContinuation: false,
			State:          LineStateDirty,
		}
	}
}

// EraseToEndOfScreen clears from cursor to end of current row, plus all rows below.
// This implements ED 0 (Erase from cursor to end of screen).
func (vs *ViewportState) EraseToEndOfScreen() {
	// Erase from cursor to end of current row
	if vs.cursorY >= 0 && vs.cursorY < vs.height {
		for x := vs.cursorX; x < vs.width; x++ {
			vs.grid[vs.cursorY][x] = Cell{Rune: ' ', FG: DefaultFG, BG: vs.eraseBG}
		}
		vs.rowMeta[vs.cursorY].State = LineStateDirty
	}
	// Erase all rows below cursor
	for y := vs.cursorY + 1; y < vs.height; y++ {
		for x := 0; x < vs.width; x++ {
			vs.grid[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: vs.eraseBG}
		}
		vs.rowMeta[y].State = LineStateDirty
	}
}

// EraseFromStartOfScreen clears from start of screen to cursor (inclusive).
// This implements ED 1 (Erase from start of screen to cursor).
func (vs *ViewportState) EraseFromStartOfScreen() {
	// Erase all rows above cursor
	for y := 0; y < vs.cursorY; y++ {
		for x := 0; x < vs.width; x++ {
			vs.grid[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: vs.eraseBG}
		}
		vs.rowMeta[y].State = LineStateDirty
	}
	// Erase from start of current row to cursor (inclusive)
	if vs.cursorY >= 0 && vs.cursorY < vs.height {
		for x := 0; x <= vs.cursorX && x < vs.width; x++ {
			vs.grid[vs.cursorY][x] = Cell{Rune: ' ', FG: DefaultFG, BG: vs.eraseBG}
		}
		vs.rowMeta[vs.cursorY].State = LineStateDirty
	}
}

// EraseCharacters replaces n characters at cursor with spaces (ECH).
// Uses the current eraseBG for background (terminal standard behavior).
func (vs *ViewportState) EraseCharacters(n int) {
	if vs.cursorY < 0 || vs.cursorY >= vs.height {
		return
	}
	for i := 0; i < n && vs.cursorX+i < vs.width; i++ {
		vs.grid[vs.cursorY][vs.cursorX+i] = Cell{Rune: ' ', FG: DefaultFG, BG: vs.eraseBG}
	}
	vs.rowMeta[vs.cursorY].State = LineStateDirty
}

// DeleteCharacters removes n characters at cursor, shifting content left (DCH).
func (vs *ViewportState) DeleteCharacters(n int) {
	if vs.cursorY < 0 || vs.cursorY >= vs.height {
		return
	}
	row := vs.grid[vs.cursorY]
	// Shift characters left
	copy(row[vs.cursorX:], row[vs.cursorX+n:])
	// Fill end with spaces
	for x := vs.width - n; x < vs.width; x++ {
		if x >= 0 {
			row[x] = Cell{Rune: ' ', FG: DefaultFG, BG: vs.eraseBG}
		}
	}
	vs.rowMeta[vs.cursorY].State = LineStateDirty
}

// InsertCharacters inserts n blank characters at cursor (ICH).
func (vs *ViewportState) InsertCharacters(n int, fg, bg Color) {
	if vs.cursorY < 0 || vs.cursorY >= vs.height {
		return
	}
	row := vs.grid[vs.cursorY]
	// Shift characters right
	copy(row[vs.cursorX+n:], row[vs.cursorX:vs.width-n])
	// Fill gap with blanks
	for i := 0; i < n && vs.cursorX+i < vs.width; i++ {
		row[vs.cursorX+i] = Cell{Rune: ' ', FG: fg, BG: bg}
	}
	vs.rowMeta[vs.cursorY].State = LineStateDirty
}

// --- Scroll Operations ---

// ScrollContentUp scrolls content up (content moves up, new blank line at bottom).
// This is the terminal escape sequence behavior (LF at bottom of screen).
func (vs *ViewportState) ScrollContentUp(n int) {
	if n <= 0 {
		return
	}

	// Commit lines that scroll off the top
	for y := 0; y < n && y < vs.height; y++ {
		vs.commitRow(y)
	}

	// Shift rows up
	for y := 0; y < vs.height-n; y++ {
		vs.grid[y] = vs.grid[y+n]
		vs.rowMeta[y] = vs.rowMeta[y+n]
		// Preserve LineStateCommitted to prevent re-committing history rows.
		// Committed rows came from history and shouldn't be re-committed when they scroll off.
		// Other rows get marked dirty since content moved position.
		if vs.rowMeta[y].State != LineStateCommitted {
			vs.rowMeta[y].State = LineStateDirty
		}
	}

	// Create new blank rows at bottom
	for y := vs.height - n; y < vs.height; y++ {
		if y >= 0 {
			vs.grid[y] = vs.makeEmptyRow()
			vs.rowMeta[y] = RowMetadata{
				LogicalLineID:  -1,
				IsFirstRow:     true,
				IsContinuation: false,
				State:          LineStateDirty, // Mark as dirty - new content
			}
		}
	}
}

// ScrollContentDown scrolls content down (content moves down, new blank line at top).
// This is the terminal escape sequence behavior (reverse index).
func (vs *ViewportState) ScrollContentDown(n int) {
	if n <= 0 {
		return
	}

	// Shift rows down
	for y := vs.height - 1; y >= n; y-- {
		vs.grid[y] = vs.grid[y-n]
		vs.rowMeta[y] = vs.rowMeta[y-n]
		// Preserve LineStateCommitted to prevent re-committing history rows
		if vs.rowMeta[y].State != LineStateCommitted {
			vs.rowMeta[y].State = LineStateDirty
		}
	}

	// Create new blank rows at top
	for y := 0; y < n && y < vs.height; y++ {
		vs.grid[y] = vs.makeEmptyRow()
		vs.rowMeta[y] = RowMetadata{
			LogicalLineID:  -1,
			IsFirstRow:     true,
			IsContinuation: false,
			State:          LineStateDirty, // Mark as dirty - new content
		}
	}
}

// ScrollRegionUp scrolls rows within [top, bottom] up by n rows.
// Lines scrolling off the top of the region are lost (not committed to history).
func (vs *ViewportState) ScrollRegionUp(top, bottom, n int) {
	if n <= 0 || top < 0 || bottom >= vs.height || top > bottom {
		return
	}

	regionHeight := bottom - top + 1
	if n > regionHeight {
		n = regionHeight
	}

	// If this is the full screen (top=0), commit lines scrolling off
	if top == 0 {
		for y := 0; y < n; y++ {
			vs.commitRow(y)
		}
	}

	// Shift rows up within the region
	for y := top; y <= bottom-n; y++ {
		vs.grid[y] = vs.grid[y+n]
		vs.rowMeta[y] = vs.rowMeta[y+n]
		// Preserve LineStateCommitted to prevent re-committing history rows
		if vs.rowMeta[y].State != LineStateCommitted {
			vs.rowMeta[y].State = LineStateDirty
		}
	}

	// Create new blank rows at the bottom of the region
	for y := bottom - n + 1; y <= bottom; y++ {
		vs.grid[y] = vs.makeEmptyRow()
		vs.rowMeta[y] = RowMetadata{
			LogicalLineID:  -1,
			IsFirstRow:     true,
			IsContinuation: false,
			State:          LineStateDirty, // Mark as dirty - new content
		}
	}
}

// ScrollRegionDown scrolls rows within [top, bottom] down by n rows.
// Lines scrolling off the bottom of the region are lost.
func (vs *ViewportState) ScrollRegionDown(top, bottom, n int) {
	if n <= 0 || top < 0 || bottom >= vs.height || top > bottom {
		return
	}

	regionHeight := bottom - top + 1
	if n > regionHeight {
		n = regionHeight
	}

	if vs.debugLog != nil {
		vs.debugLog("[VS] ScrollRegionDown: top=%d, bottom=%d, n=%d", top, bottom, n)
		// Log before state
		for y := top; y <= bottom; y++ {
			if len(vs.grid[y]) > 0 {
				vs.debugLog("[VS]   BEFORE row[%d][0] = '%c' (0x%04X)", y, vs.grid[y][0].Rune, vs.grid[y][0].Rune)
			}
		}
	}

	// Shift rows down within the region
	for y := bottom; y >= top+n; y-- {
		vs.grid[y] = vs.grid[y-n]
		vs.rowMeta[y] = vs.rowMeta[y-n]
		// Preserve LineStateCommitted to prevent re-committing history rows
		if vs.rowMeta[y].State != LineStateCommitted {
			vs.rowMeta[y].State = LineStateDirty
		}
	}

	// Create new blank rows at the top of the region
	for y := top; y < top+n && y <= bottom; y++ {
		vs.grid[y] = vs.makeEmptyRow()
		vs.rowMeta[y] = RowMetadata{
			LogicalLineID:  -1,
			IsFirstRow:     true,
			IsContinuation: false,
			State:          LineStateDirty, // Mark as dirty - new content
		}
	}

	if vs.debugLog != nil {
		// Log after state
		for y := top; y <= bottom; y++ {
			if len(vs.grid[y]) > 0 {
				vs.debugLog("[VS]   AFTER row[%d][0] = '%c' (0x%04X)", y, vs.grid[y][0].Rune, vs.grid[y][0].Rune)
			}
		}
	}
}

// ScrollColumnsUp scrolls content up within a column range [leftCol, rightCol]
// across row range [top, bottom]. This is for left/right margin scrolling.
// Content in the column range shifts up by n rows, bottom rows are cleared.
func (vs *ViewportState) ScrollColumnsUp(top, bottom, leftCol, rightCol, n int, clearFG, clearBG Color) {
	if n <= 0 || top < 0 || bottom >= vs.height || top > bottom {
		return
	}
	if leftCol < 0 {
		leftCol = 0
	}
	if rightCol >= vs.width {
		rightCol = vs.width - 1
	}
	if leftCol > rightCol {
		return
	}

	// Shift content within columns upward
	for y := top; y <= bottom-n; y++ {
		srcY := y + n
		if srcY <= bottom {
			for x := leftCol; x <= rightCol; x++ {
				vs.grid[y][x] = vs.grid[srcY][x]
			}
			vs.rowMeta[y].State = LineStateDirty
		}
	}

	// Clear the bottom n rows' column range
	clearStart := bottom - n + 1
	if clearStart < top {
		clearStart = top
	}
	for y := clearStart; y <= bottom; y++ {
		for x := leftCol; x <= rightCol; x++ {
			vs.grid[y][x] = Cell{Rune: ' ', FG: clearFG, BG: clearBG}
		}
		vs.rowMeta[y].State = LineStateDirty
	}
}

// ScrollColumnsDown scrolls content down within a column range [leftCol, rightCol]
// across row range [top, bottom]. This is for left/right margin scrolling.
// Content in the column range shifts down by n rows, top rows are cleared.
func (vs *ViewportState) ScrollColumnsDown(top, bottom, leftCol, rightCol, n int, clearFG, clearBG Color) {
	if n <= 0 || top < 0 || bottom >= vs.height || top > bottom {
		return
	}
	if leftCol < 0 {
		leftCol = 0
	}
	if rightCol >= vs.width {
		rightCol = vs.width - 1
	}
	if leftCol > rightCol {
		return
	}

	// Shift content within columns downward
	for y := bottom; y >= top+n; y-- {
		srcY := y - n
		if srcY >= top {
			for x := leftCol; x <= rightCol; x++ {
				vs.grid[y][x] = vs.grid[srcY][x]
			}
			vs.rowMeta[y].State = LineStateDirty
		}
	}

	// Clear the top n rows' column range
	clearEnd := top + n - 1
	if clearEnd > bottom {
		clearEnd = bottom
	}
	for y := top; y <= clearEnd; y++ {
		for x := leftCol; x <= rightCol; x++ {
			vs.grid[y][x] = Cell{Rune: ' ', FG: clearFG, BG: clearBG}
		}
		vs.rowMeta[y].State = LineStateDirty
	}
}

// ScrollColumnsHorizontal scrolls content horizontally within a column range.
// n > 0: shift right (blank inserted at left), n < 0: shift left (blank at right).
func (vs *ViewportState) ScrollColumnsHorizontal(top, bottom, leftCol, rightCol, n int, clearFG, clearBG Color) {
	if n == 0 || top < 0 || bottom >= vs.height || top > bottom {
		return
	}
	if leftCol < 0 {
		leftCol = 0
	}
	if rightCol >= vs.width {
		rightCol = vs.width - 1
	}
	if leftCol > rightCol {
		return
	}

	if n > 0 {
		// Scroll right: shift content right, insert blank at left
		for i := 0; i < n; i++ {
			for y := top; y <= bottom; y++ {
				for x := rightCol; x > leftCol; x-- {
					vs.grid[y][x] = vs.grid[y][x-1]
				}
				vs.grid[y][leftCol] = Cell{Rune: ' ', FG: clearFG, BG: clearBG}
				vs.rowMeta[y].State = LineStateDirty
			}
		}
	} else {
		// Scroll left: shift content left, insert blank at right
		for i := 0; i < -n; i++ {
			for y := top; y <= bottom; y++ {
				for x := leftCol; x < rightCol; x++ {
					vs.grid[y][x] = vs.grid[y][x+1]
				}
				vs.grid[y][rightCol] = Cell{Rune: ' ', FG: clearFG, BG: clearBG}
				vs.rowMeta[y].State = LineStateDirty
			}
		}
	}
}

// InsertLines inserts n blank lines at cursor row, within scroll region.
func (vs *ViewportState) InsertLines(n int, scrollTop, scrollBottom int) {
	if vs.cursorY < scrollTop || vs.cursorY > scrollBottom {
		return
	}

	// Scroll down from cursor to bottom of region
	for y := scrollBottom; y >= vs.cursorY+n; y-- {
		vs.grid[y] = vs.grid[y-n]
		vs.rowMeta[y] = vs.rowMeta[y-n]
		vs.rowMeta[y].State = LineStateDirty // Mark as dirty since content moved
	}

	// Insert blank lines at cursor position
	for y := vs.cursorY; y < vs.cursorY+n && y <= scrollBottom; y++ {
		vs.grid[y] = vs.makeEmptyRow()
		vs.rowMeta[y] = RowMetadata{
			LogicalLineID:  -1,
			IsFirstRow:     true,
			IsContinuation: false,
			State:          LineStateDirty, // Mark as dirty - new content
		}
	}
}

// DeleteLines deletes n lines at cursor row, within scroll region.
func (vs *ViewportState) DeleteLines(n int, scrollTop, scrollBottom int) {
	if vs.cursorY < scrollTop || vs.cursorY > scrollBottom {
		return
	}

	// Scroll up from cursor to bottom of region
	for y := vs.cursorY; y <= scrollBottom-n; y++ {
		vs.grid[y] = vs.grid[y+n]
		vs.rowMeta[y] = vs.rowMeta[y+n]
		vs.rowMeta[y].State = LineStateDirty // Mark as dirty since content moved
	}

	// Insert blank lines at bottom of region
	for y := scrollBottom - n + 1; y <= scrollBottom; y++ {
		if y >= vs.cursorY {
			vs.grid[y] = vs.makeEmptyRow()
			vs.rowMeta[y] = RowMetadata{
				LogicalLineID:  -1,
				IsFirstRow:     true,
				IsContinuation: false,
				State:          LineStateDirty, // Mark as dirty - new content
			}
		}
	}
}

// --- Resize ---

// Resize changes the viewport dimensions.
func (vs *ViewportState) Resize(newWidth, newHeight int) {
	if newWidth == vs.width && newHeight == vs.height {
		return
	}

	// Create new grid
	newGrid := make([][]Cell, newHeight)
	newMeta := make([]RowMetadata, newHeight)

	for y := 0; y < newHeight; y++ {
		newGrid[y] = make([]Cell, newWidth)
		for x := 0; x < newWidth; x++ {
			if y < vs.height && x < vs.width {
				newGrid[y][x] = vs.grid[y][x]
			} else {
				newGrid[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
			}
		}
		if y < vs.height {
			newMeta[y] = vs.rowMeta[y]
		} else {
			newMeta[y] = RowMetadata{
				LogicalLineID:  -1,
				IsFirstRow:     true,
				IsContinuation: false,
				State:          LineStateClean,
			}
		}
	}

	vs.grid = newGrid
	vs.rowMeta = newMeta
	vs.width = newWidth
	vs.height = newHeight

	// Clamp cursor
	if vs.cursorX >= vs.width {
		vs.cursorX = vs.width - 1
	}
	if vs.cursorY >= vs.height {
		vs.cursorY = vs.height - 1
	}
}

// --- History Commits ---

// CommitCurrentLine commits the row at cursor to history.
func (vs *ViewportState) CommitCurrentLine() {
	vs.commitRow(vs.cursorY)
}

// commitRow commits a single row to history.
func (vs *ViewportState) commitRow(y int) {
	if vs.history == nil || y < 0 || y >= vs.height {
		return
	}

	// Skip rows that are already committed (e.g., rows populated from history)
	if vs.rowMeta[y].State == LineStateCommitted {
		return
	}

	// Extract logical line starting from this row
	lines := vs.ExtractLogicalLines(y, y)
	for _, line := range lines {
		vs.history.Append(line)
	}

	vs.rowMeta[y].State = LineStateCommitted
}

// ExtractLogicalLines extracts logical lines from the given row range.
func (vs *ViewportState) ExtractLogicalLines(startRow, endRow int) []*LogicalLine {
	if startRow < 0 {
		startRow = 0
	}
	if endRow >= vs.height {
		endRow = vs.height - 1
	}
	if startRow > endRow {
		return nil
	}

	var result []*LogicalLine
	var currentLine *LogicalLine

	for y := startRow; y <= endRow; y++ {
		if !vs.rowMeta[y].IsContinuation || currentLine == nil {
			// Start a new logical line
			if currentLine != nil {
				result = append(result, currentLine)
			}
			currentLine = NewLogicalLine()
		}

		// Append this row's content to current logical line
		row := vs.grid[y]
		// Find the last non-space character
		lastNonSpace := -1
		for x := len(row) - 1; x >= 0; x-- {
			if row[x].Rune != ' ' || row[x].BG != DefaultBG {
				lastNonSpace = x
				break
			}
		}

		// Append cells up to last non-space (or all if continuation)
		end := lastNonSpace + 1
		if vs.rowMeta[y].IsContinuation && y < endRow {
			end = vs.width // Include trailing spaces for wrapped lines
		}
		for x := 0; x < end; x++ {
			currentLine.Append(row[x])
		}
	}

	if currentLine != nil {
		result = append(result, currentLine)
	}

	return result
}

// --- Row Metadata ---

// MarkRowAsLineStart marks a row as the start of a new logical line.
func (vs *ViewportState) MarkRowAsLineStart(y int) {
	if y < 0 || y >= vs.height {
		return
	}
	vs.rowMeta[y].IsFirstRow = true
	vs.rowMeta[y].IsContinuation = false
}

// MarkRowAsContinuation marks a row as continuation of the previous line.
func (vs *ViewportState) MarkRowAsContinuation(y int) {
	if y < 0 || y >= vs.height {
		return
	}
	vs.rowMeta[y].IsFirstRow = false
	vs.rowMeta[y].IsContinuation = true
}

// MarkRowAsCommitted marks a row as already committed to history.
// Used when populating viewport from history to prevent re-committing
// the same content when it scrolls off the top.
func (vs *ViewportState) MarkRowAsCommitted(y int) {
	if y < 0 || y >= vs.height {
		return
	}
	vs.rowMeta[y].State = LineStateCommitted
	// Mark as from history to prevent re-committing by CommitViewportAsFixedWidth
	vs.rowMeta[y].FromHistory = true
}

// --- Shell Integration ---

// MarkPromptStart records the start of a shell prompt (OSC 133;A).
// Records the global line position for seamless recovery.
// This fires at the START of the prompt, so for multiline prompts,
// we exclude the entire prompt from history on recovery.
func (vs *ViewportState) MarkPromptStart() {
	// Calculate global line index: history lines + current viewport row
	historyLines := int64(0)
	if vs.history != nil {
		historyLines = vs.history.TotalLen()
	}
	vs.lastPromptLine = historyLines + int64(vs.cursorY)

	// Record the viewport row for prompt height calculation
	vs.promptStartRow = vs.cursorY
}

// LastPromptLine returns the global line index where the prompt starts.
// Returns -1 if no position has been recorded.
func (vs *ViewportState) LastPromptLine() int64 {
	return vs.lastPromptLine
}

// LastPromptHeight returns the number of lines in the prompt.
// Calculated when OSC 133;B fires. Defaults to 1.
func (vs *ViewportState) LastPromptHeight() int {
	return vs.lastPromptHeight
}

// MarkInputStart marks the start of user input (OSC 133;B).
// This fires after the prompt, so we can calculate prompt height.
func (vs *ViewportState) MarkInputStart() {
	// Calculate prompt height from prompt start to current position
	if vs.promptStartRow >= 0 {
		height := vs.cursorY - vs.promptStartRow + 1
		if height > 0 {
			vs.lastPromptHeight = height
		}
	}
	vs.promptStartRow = -1 // Reset for next prompt
}

// MarkOutputStart marks the current line as output (OSC 133;C).
func (vs *ViewportState) MarkOutputStart() {
	// Currently a no-op - could be used for semantic line tracking
}

// --- TUI Content Preservation ---

// CommitViewportAsFixedWidth commits all viewport rows to history as fixed-width lines.
// Fixed-width lines don't reflow on resize - they are clipped or padded instead.
// This is used for TUI app content (scroll regions, cursor-addressed output) that
// should be preserved as-is in scrollback.
//
// Returns the number of lines committed. Skips rows already marked as committed.
func (vs *ViewportState) CommitViewportAsFixedWidth() int {
	if vs.history == nil {
		return 0
	}

	committed := 0
	for y := 0; y < vs.height; y++ {
		// Skip rows that are already committed
		if vs.rowMeta[y].State == LineStateCommitted {
			continue
		}

		// Skip rows that came from history to prevent creating duplicate entries.
		// This handles the case where history content is displayed in viewport
		// and TUI mode tries to commit it again.
		if vs.rowMeta[y].FromHistory {
			continue
		}

		// Create cells copy for the logical line
		cells := make([]Cell, vs.width)
		copy(cells, vs.grid[y])

		// Create logical line with FixedWidth set
		line := &LogicalLine{
			Cells:      cells,
			FixedWidth: vs.width,
		}

		// Trim trailing spaces for storage efficiency
		line.TrimTrailingSpaces()

		// Append to history
		vs.history.Append(line)

		// Mark row as committed
		vs.rowMeta[y].State = LineStateCommitted
		committed++
	}

	return committed
}

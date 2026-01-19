// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/display_buffer.go
// Summary: DisplayBuffer manages the terminal viewport with scrollback support.
//
// Architecture:
//
//	┌─────────────────────────────────────────┐
//	│           SCROLLBACK HISTORY            │
//	│   (Logical lines - width independent)   │
//	│   (Disk-backed, supports global index)  │
//	└─────────────────────────────────────────┘
//	                    ▲
//	                    │ Commit on: scroll-off, LF at bottom, OSC 133;D
//	                    │
//	┌─────────────────────────────────────────┐
//	│         VIEWPORT (ViewportState)        │
//	│   ┌─────────────────────────────────┐   │
//	│   │  Writable [][]Cell grid         │   │
//	│   │  Any position can be written    │   │
//	│   │  Tracks logical line metadata   │   │
//	│   └─────────────────────────────────┘   │
//	└─────────────────────────────────────────┘
//
// Key design: The viewport is always a writable grid (like altBuffer).
// Cursor can move anywhere without restrictions. Lines are committed
// to history when they scroll off the top of the viewport.

package parser

import "log"

// Default values for display buffer configuration.
const (
	// DefaultMarginAbove is how many off-screen lines to keep above viewport.
	DefaultMarginAbove = 200

	// DefaultMarginBelow is how many off-screen lines to keep below viewport.
	DefaultMarginBelow = 50

	// DefaultMaxMemoryLines is the default number of lines to keep in memory.
	DefaultMaxMemoryLines = 5000

	// DefaultWidth is the fallback terminal width when none is specified.
	DefaultWidth = 80

	// DefaultHeight is the fallback terminal height when none is specified.
	DefaultHeight = 24
)

// DisplayBuffer manages the terminal viewport and scrollback history.
type DisplayBuffer struct {
	// viewport is the writable screen grid
	viewport *ViewportState

	// history stores lines that have scrolled off the top
	history *ScrollbackHistory

	// Configuration
	marginAbove int
	marginBelow int

	// For scrollback viewing (when user scrolls up into history)
	historyViewOffset int64 // How many lines scrolled back into history
	viewingHistory    bool  // True when viewing history (not live edge)

	// restoredView is set when restoring a session to suppress auto-scroll
	// until the user interacts (types or explicitly scrolls to live edge).
	// This prevents shell startup (bash clearing screen) from disrupting the view.
	restoredView bool

	// hasLiveContent is set when the shell writes new content after history
	// population. When true, Resize skips reflow to preserve live content.
	hasLiveContent bool

	// cachedHistoryView caches the history view when scrolled back
	cachedHistoryView [][]Cell

	// debugLog is an optional logging function
	debugLog func(format string, args ...interface{})
}

// DisplayBufferConfig holds configuration for creating a DisplayBuffer.
type DisplayBufferConfig struct {
	Width       int
	Height      int
	MarginAbove int
	MarginBelow int
}

// NewDisplayBuffer creates a new display buffer with the given history and config.
func NewDisplayBuffer(history *ScrollbackHistory, config DisplayBufferConfig) *DisplayBuffer {
	if config.MarginAbove <= 0 {
		config.MarginAbove = DefaultMarginAbove
	}
	if config.MarginBelow <= 0 {
		config.MarginBelow = DefaultMarginBelow
	}
	if config.Width <= 0 {
		config.Width = DefaultWidth
	}
	if config.Height <= 0 {
		config.Height = DefaultHeight
	}

	db := &DisplayBuffer{
		viewport:    NewViewportState(config.Width, config.Height, history),
		history:     history,
		marginAbove: config.MarginAbove,
		marginBelow: config.MarginBelow,
	}

	// Link viewport to history
	db.viewport.history = history

	return db
}

// --- Core Operations ---

// SetCursor moves the cursor to the given position.
// No restrictions - cursor can move anywhere within viewport.
func (db *DisplayBuffer) SetCursor(physX, physY int) {
	db.viewport.SetCursor(physX, physY)

	// If viewing history and cursor moves, return to live edge
	// unless we're in restored view mode (suppresses auto-scroll until user interacts)
	if db.viewingHistory && !db.restoredView {
		db.ScrollToBottom()
	}

	if db.debugLog != nil {
		db.debugLog("DisplayBuffer.SetCursor: (%d, %d)", physX, physY)
	}
}

// Write writes a character at the current cursor position.
func (db *DisplayBuffer) Write(r rune, fg, bg Color, attr Attribute, insertMode bool) {
	db.viewport.Write(r, fg, bg, attr, insertMode)

	// Return to live edge on write, unless we're in restored view mode
	// (which suppresses auto-scroll until user interacts)
	if db.viewingHistory && !db.restoredView {
		db.ScrollToBottom()
	}
}

// WriteWide writes a character at the current cursor position with wide character support.
func (db *DisplayBuffer) WriteWide(r rune, fg, bg Color, attr Attribute, insertMode bool, isWide bool) {
	db.viewport.WriteWide(r, fg, bg, attr, insertMode, isWide)

	// Mark that we have live content (shell has written since history population)
	db.hasLiveContent = true

	// Return to live edge on write, unless we're in restored view mode
	// (which suppresses auto-scroll until user interacts)
	if db.viewingHistory && !db.restoredView {
		db.ScrollToBottom()
	}
}

// SetEraseColor sets the background color for erase operations.
// Terminal erase ops (EL, ECH, ED) fill with current BG, not default.
func (db *DisplayBuffer) SetEraseColor(bg Color) {
	db.viewport.SetEraseColor(bg)
}

// Erase performs erase operations on the current line.
// mode 0: Erase from cursor to end (EL 0)
// mode 1: Erase from start to cursor (EL 1)
// mode 2: Erase entire line (EL 2)
func (db *DisplayBuffer) Erase(mode int) {
	switch mode {
	case 0:
		db.viewport.EraseToEndOfLine()
	case 1:
		db.viewport.EraseFromStartOfLine()
	case 2:
		db.viewport.EraseLine()
	}
}

// EraseScreenMode handles ED (Erase in Display) with different modes.
// mode 0: Erase from cursor to end of screen
// mode 1: Erase from start of screen to cursor
// mode 2: Erase entire screen
func (db *DisplayBuffer) EraseScreenMode(mode int) {
	db.viewport.SetEraseColor(db.viewport.eraseBG)
	switch mode {
	case 0:
		db.viewport.EraseToEndOfScreen()
	case 1:
		db.viewport.EraseFromStartOfScreen()
	case 2:
		db.viewport.EraseScreen()
	}
}

// EraseCharacters replaces n characters at cursor with spaces (ECH).
func (db *DisplayBuffer) EraseCharacters(n int) {
	db.viewport.EraseCharacters(n)
}

// DeleteCharacters removes n characters at cursor, shifting content left (DCH).
func (db *DisplayBuffer) DeleteCharacters(n int) {
	db.viewport.DeleteCharacters(n)
}

// InsertCharacters inserts n blank characters at cursor (ICH).
func (db *DisplayBuffer) InsertCharacters(n int, fg, bg Color) {
	db.viewport.InsertCharacters(n, fg, bg)
}

// --- Cursor Information ---

// GetCursorOffset returns the logical cursor offset.
// For backward compatibility - calculates from physical position.
func (db *DisplayBuffer) GetCursorOffset() int {
	x, y := db.viewport.Cursor()
	return y*db.viewport.Width() + x
}

// GetPhysicalCursorPos returns the viewport coordinates of the cursor.
func (db *DisplayBuffer) GetPhysicalCursorPos() (x, y int, found bool) {
	if db.viewingHistory {
		return 0, 0, false // Cursor not visible when viewing history
	}
	x, y = db.viewport.Cursor()
	return x, y, true
}

// --- Line Operations ---

// CommitCurrentLine commits the current line to history.
// Called on: LF at bottom row, OSC 133;D.
func (db *DisplayBuffer) CommitCurrentLine() {
	db.viewport.CommitCurrentLine()

	if db.debugLog != nil {
		db.debugLog("DisplayBuffer.CommitCurrentLine")
	}
}

// CurrentLine returns the current logical line (for backward compatibility).
// Extracts from viewport at cursor position.
func (db *DisplayBuffer) CurrentLine() *LogicalLine {
	x, y := db.viewport.Cursor()

	// Find start of logical line
	startRow := y
	for startRow > 0 && db.viewport.rowMeta[startRow].IsContinuation {
		startRow--
	}

	// Find end of logical line
	endRow := y
	for endRow < db.viewport.Height()-1 && db.viewport.rowMeta[endRow+1].IsContinuation {
		endRow++
	}

	// Extract logical line
	lines := db.viewport.ExtractLogicalLines(startRow, endRow)
	if len(lines) > 0 {
		return lines[0]
	}

	// Find logical line index for cursor position
	_ = x // cursor x position available if needed

	return NewLogicalLine()
}

// RebuildCurrentLine is a no-op in the new architecture.
// Kept for backward compatibility.
func (db *DisplayBuffer) RebuildCurrentLine() {
	// No-op - viewport is always up to date
}

// currentLinePhysical returns the physical rows of the current logical line.
// For backward compatibility with tests.
func (db *DisplayBuffer) currentLinePhysical() [][]Cell {
	_, y := db.viewport.Cursor()

	// Find start of logical line
	startRow := y
	for startRow > 0 && db.viewport.rowMeta[startRow].IsContinuation {
		startRow--
	}

	// Find end of logical line
	endRow := y
	for endRow < db.viewport.Height()-1 && db.viewport.rowMeta[endRow+1].IsContinuation {
		endRow++
	}

	// Extract physical rows
	result := make([][]Cell, endRow-startRow+1)
	grid := db.viewport.Grid()
	for i := 0; i <= endRow-startRow; i++ {
		result[i] = make([]Cell, len(grid[startRow+i]))
		copy(result[i], grid[startRow+i])
	}
	return result
}

// lines is a backward-compatibility accessor for tests.
// Returns number of committed history lines.
func (db *DisplayBuffer) lines() int {
	if db.history == nil {
		return 0
	}
	return int(db.history.TotalLen())
}

// --- Viewport Access ---

// GetViewportAsCells returns the viewport as a 2D Cell grid.
func (db *DisplayBuffer) GetViewportAsCells() [][]Cell {
	if db.viewingHistory {
		if db.debugLog != nil {
			db.debugLog("[GetViewportAsCells] viewingHistory=true, calling getHistoryView")
		}
		return db.getHistoryView()
	}
	if db.debugLog != nil {
		db.debugLog("[GetViewportAsCells] viewingHistory=false, returning live viewport")
	}
	return db.viewport.Grid()
}

// getHistoryView builds the view when scrolled back into history.
func (db *DisplayBuffer) getHistoryView() [][]Cell {
	if db.cachedHistoryView != nil {
		if db.debugLog != nil {
			db.debugLog("[getHistoryView] returning cached view")
		}
		return db.cachedHistoryView
	}

	height := db.viewport.Height()
	width := db.viewport.Width()
	result := make([][]Cell, height)

	// Fill with empty rows first
	for y := 0; y < height; y++ {
		result[y] = make([]Cell, width)
		for x := 0; x < width; x++ {
			result[y][x] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
		}
	}

	if db.history == nil {
		if db.debugLog != nil {
			db.debugLog("[getHistoryView] history is nil, returning empty")
		}
		return result
	}

	// Load history lines at the scroll offset
	// historyViewOffset is how many physical rows we've scrolled back
	totalHistoryLines := db.history.TotalLen()
	if totalHistoryLines == 0 {
		if db.debugLog != nil {
			db.debugLog("[getHistoryView] totalHistoryLines=0, returning empty")
		}
		return result
	}

	if db.debugLog != nil {
		db.debugLog("[getHistoryView] totalHistoryLines=%d, historyViewOffset=%d", totalHistoryLines, db.historyViewOffset)
	}

	// Calculate which logical lines to load
	// We need to work backwards from the end of history
	physicalRowsBack := db.historyViewOffset

	// Get wrapped lines from history
	// Start from end and work back
	var physicalLines []PhysicalLine
	logicalIdx := totalHistoryLines - 1

	for logicalIdx >= 0 && int64(len(physicalLines)) < physicalRowsBack+int64(height) {
		line := db.history.GetGlobal(logicalIdx)
		if line != nil {
			wrapped := line.WrapToWidth(width)
			// Prepend to physicalLines (we're going backwards)
			newLines := make([]PhysicalLine, len(wrapped)+len(physicalLines))
			copy(newLines, wrapped)
			copy(newLines[len(wrapped):], physicalLines)
			physicalLines = newLines
		}
		logicalIdx--
	}

	// Now physicalLines contains history from some point to the end
	// We want to show starting at (len - historyViewOffset - height)
	startIdx := len(physicalLines) - int(physicalRowsBack) - height
	if startIdx < 0 {
		startIdx = 0
	}

	for y := 0; y < height && startIdx+y < len(physicalLines); y++ {
		pl := physicalLines[startIdx+y]
		for x, cell := range pl.Cells {
			if x < width {
				result[y][x] = cell
			}
		}
	}

	db.cachedHistoryView = result
	return result
}

// GetViewport returns physical lines (for backward compatibility).
func (db *DisplayBuffer) GetViewport() []PhysicalLine {
	height := db.viewport.Height()
	result := make([]PhysicalLine, height)

	grid := db.viewport.Grid()
	for y := 0; y < height; y++ {
		cells := make([]Cell, len(grid[y]))
		copy(cells, grid[y])
		result[y] = PhysicalLine{
			Cells:        cells,
			LogicalIndex: -1,
			Offset:       0,
		}
	}

	return result
}

// --- Scroll Operations ---

// ScrollUp scrolls the viewport up (content moves down).
// Called when LF at bottom of scroll region.
func (db *DisplayBuffer) ScrollUp(n int) int {
	db.viewport.ScrollUp(n)
	return n
}

// ScrollDown scrolls the viewport down (content moves up).
func (db *DisplayBuffer) ScrollDown(n int) int {
	db.viewport.ScrollDown(n)
	return n
}

// ScrollToBottom returns to the live edge.
// Also clears the restoredView flag since we're now at live edge.
func (db *DisplayBuffer) ScrollToBottom() {
	db.viewingHistory = false
	db.historyViewOffset = 0
	db.restoredView = false // User explicitly scrolled to bottom, exit restored mode
	db.cachedHistoryView = nil
	db.viewport.ScrollToLiveEdge()
}

// ScrollViewportUp scrolls the view up into history (for user scrollback).
func (db *DisplayBuffer) ScrollViewportUp(lines int) int {
	if db.history == nil {
		return 0
	}

	// Calculate max scroll
	maxScroll := db.calculateMaxHistoryScroll()
	newOffset := db.historyViewOffset + int64(lines)
	if newOffset > maxScroll {
		newOffset = maxScroll
	}

	scrolled := int(newOffset - db.historyViewOffset)
	db.historyViewOffset = newOffset
	db.viewingHistory = newOffset > 0
	db.cachedHistoryView = nil // Invalidate cache

	return scrolled
}

// ScrollViewportDown scrolls the view down toward live edge.
func (db *DisplayBuffer) ScrollViewportDown(lines int) int {
	if !db.viewingHistory {
		return 0
	}

	newOffset := db.historyViewOffset - int64(lines)
	if newOffset < 0 {
		newOffset = 0
	}

	scrolled := int(db.historyViewOffset - newOffset)
	db.historyViewOffset = newOffset
	db.viewingHistory = newOffset > 0
	db.cachedHistoryView = nil // Invalidate cache

	if !db.viewingHistory {
		db.viewport.ScrollToLiveEdge()
	}

	return scrolled
}

// calculateMaxHistoryScroll returns the maximum scroll offset.
func (db *DisplayBuffer) calculateMaxHistoryScroll() int64 {
	if db.history == nil {
		return 0
	}

	// Count total physical lines in history
	var totalPhysical int64
	for i := int64(0); i < db.history.TotalLen(); i++ {
		line := db.history.GetGlobal(i)
		if line != nil {
			wrapped := line.WrapToWidth(db.viewport.Width())
			totalPhysical += int64(len(wrapped))
		}
	}

	// Can scroll back by total physical lines minus viewport height
	max := totalPhysical - int64(db.viewport.Height())
	if max < 0 {
		max = 0
	}
	return max
}

// --- Resize ---

// Resize changes the viewport dimensions and reflows content from history.
func (db *DisplayBuffer) Resize(newWidth, newHeight int) {
	if newWidth == db.viewport.Width() && newHeight == db.viewport.Height() {
		return
	}

	// First resize the viewport structure (simple copy, may truncate)
	db.viewport.Resize(newWidth, newHeight)
	db.cachedHistoryView = nil // Invalidate cache on resize

	// Repopulate viewport from history to reflow content at new width.
	// This re-wraps logical lines from history at the new width.
	// The shell will redraw its current prompt line via SIGWINCH handling.
	if db.history != nil && db.history.TotalLen() > 0 {
		db.PopulateViewportFromHistory()
	}
}

// PopulateViewportFromHistory fills the viewport with the last lines from history.
// This should be called when restoring a session to ensure the viewport matches
// the end of history, so the shell continues writing where it left off.
// Returns the cursor position (x, y) that should be at the start of the next line.
func (db *DisplayBuffer) PopulateViewportFromHistory() (cursorX, cursorY int) {
	if db.history == nil || db.history.TotalLen() == 0 {
		return 0, 0
	}

	height := db.viewport.Height()
	width := db.viewport.Width()

	// Get the last lines from history that would fill the viewport
	totalHistoryLines := db.history.TotalLen()

	log.Printf("[PopulateViewportFromHistory] Starting: totalHistoryLines=%d, height=%d, width=%d", totalHistoryLines, height, width)

	// We need to get logical lines and wrap them to physical lines
	// Work backwards from the end of history to fill the viewport
	var physicalLines []PhysicalLine
	logicalIdx := totalHistoryLines - 1

	for len(physicalLines) < height && logicalIdx >= 0 {
		line := db.history.GetGlobal(logicalIdx)
		if line == nil {
			log.Printf("[PopulateViewportFromHistory] GetGlobal(%d) returned nil", logicalIdx)
			logicalIdx--
			continue
		}

		wrapped := line.WrapToWidth(width)
		log.Printf("[PopulateViewportFromHistory] GetGlobal(%d): len=%d, wrapped to %d physical lines",
			logicalIdx, line.Len(), len(wrapped))
		// Prepend wrapped lines (since we're going backwards)
		physicalLines = append(wrapped, physicalLines...)
		logicalIdx--
	}

	log.Printf("[PopulateViewportFromHistory] Collected %d physical lines, will trim to %d", len(physicalLines), height-1)

	// Trim to viewport height - 1, leaving room for shell prompt on last row.
	// This ensures the cursor is on a fresh row, not overwriting history content.
	maxLines := height - 1
	if maxLines < 1 {
		maxLines = 1
	}
	if len(physicalLines) > maxLines {
		physicalLines = physicalLines[len(physicalLines)-maxLines:]
	}

	// Clear the viewport first (this also resets row metadata)
	db.viewport.EraseScreen()

	// Write the physical lines to the viewport with proper row metadata
	for y, pline := range physicalLines {
		for x, cell := range pline.Cells {
			if x < width {
				db.viewport.SetCell(x, y, cell)
			}
		}
		// Set row metadata based on whether this is start or continuation of logical line
		// Offset == 0 means first physical row of the logical line
		if pline.Offset == 0 {
			db.viewport.MarkRowAsLineStart(y)
		} else {
			db.viewport.MarkRowAsContinuation(y)
		}
		// Mark as committed to prevent re-committing when these rows scroll off.
		// This content already exists in the scrollback history.
		db.viewport.MarkRowAsCommitted(y)
	}

	// Position cursor at the START of the line AFTER history content.
	// History lines are complete (they were committed with newlines),
	// so the shell prompt should start on a fresh line.
	cursorX = 0
	cursorY = len(physicalLines) // This is now guaranteed to be < height

	// Actually set the cursor on the viewport
	db.viewport.SetCursor(cursorX, cursorY)

	// Reset scroll state - we're now at the live edge
	db.viewingHistory = false
	db.historyViewOffset = 0
	db.cachedHistoryView = nil

	// Clear the live content flag - viewport now matches history
	// Shell will set hasLiveContent=true when it writes
	db.hasLiveContent = false

	if db.debugLog != nil {
		db.debugLog("[PopulateViewportFromHistory] Populated %d lines (height=%d), cursor set to (%d,%d)", len(physicalLines), height, cursorX, cursorY)
		// Log the last few physical lines to debug phantom line issue
		for i := len(physicalLines) - 3; i < len(physicalLines); i++ {
			if i >= 0 && i < len(physicalLines) {
				pline := physicalLines[i]
				first20 := ""
				for j := 0; j < 20 && j < len(pline.Cells); j++ {
					if pline.Cells[j].Rune != 0 {
						first20 += string(pline.Cells[j].Rune)
					}
				}
				db.debugLog("[PopulateViewportFromHistory] physLine[%d]: offset=%d, len=%d, content=%q", i, pline.Offset, len(pline.Cells), first20)
			}
		}
	}

	return cursorX, cursorY
}

// --- Accessors ---

// Width returns the viewport width.
func (db *DisplayBuffer) Width() int {
	return db.viewport.Width()
}

// Height returns the viewport height.
func (db *DisplayBuffer) Height() int {
	return db.viewport.Height()
}

// AtLiveEdge returns whether viewport is at the live edge.
func (db *DisplayBuffer) AtLiveEdge() bool {
	return !db.viewingHistory && db.viewport.AtLiveEdge()
}

// CanScrollUp returns whether there's content above to scroll to.
func (db *DisplayBuffer) CanScrollUp() bool {
	return db.history != nil && db.history.TotalLen() > 0
}

// CanScrollDown returns whether we can scroll down (toward live edge).
func (db *DisplayBuffer) CanScrollDown() bool {
	return db.viewingHistory
}

// SetDebugLog sets the debug logging function.
func (db *DisplayBuffer) SetDebugLog(fn func(format string, args ...interface{})) {
	db.debugLog = fn
	if db.viewport != nil {
		db.viewport.SetDebugLog(fn)
	}
}

// SetRestoredView sets the restored view mode flag.
// When true, auto-scroll to live edge is suppressed on writes and cursor moves,
// allowing the user to view history even while shell startup runs in the background.
// This is cleared when the user explicitly scrolls to bottom or types.
func (db *DisplayBuffer) SetRestoredView(restored bool) {
	db.restoredView = restored
}

// InRestoredView returns whether we're in restored view mode.
func (db *DisplayBuffer) InRestoredView() bool {
	return db.restoredView
}

// --- Shell Integration ---

// MarkPromptStart marks the current line as a prompt (OSC 133;A).
func (db *DisplayBuffer) MarkPromptStart() {
	db.viewport.MarkPromptStart()
}

// MarkInputStart marks the current line as input (OSC 133;B).
func (db *DisplayBuffer) MarkInputStart() {
	db.viewport.MarkInputStart()
}

// MarkOutputStart marks the current line as output (OSC 133;C).
func (db *DisplayBuffer) MarkOutputStart() {
	db.viewport.MarkOutputStart()
}

// --- Line/Row Operations (for VTerm integration) ---

// InsertLines inserts n blank lines at cursor row.
func (db *DisplayBuffer) InsertLines(n int, scrollTop, scrollBottom int) {
	db.viewport.InsertLines(n, scrollTop, scrollBottom)
}

// DeleteLines deletes n lines at cursor row.
func (db *DisplayBuffer) DeleteLines(n int, scrollTop, scrollBottom int) {
	db.viewport.DeleteLines(n, scrollTop, scrollBottom)
}

// ScrollRegionUp scrolls within a region.
func (db *DisplayBuffer) ScrollRegionUp(top, bottom, n int) {
	db.viewport.ScrollRegionUp(top, bottom, n)
}

// ScrollRegionDown scrolls within a region.
func (db *DisplayBuffer) ScrollRegionDown(top, bottom, n int) {
	db.viewport.ScrollRegionDown(top, bottom, n)
}

// ClearRow clears a specific row.
func (db *DisplayBuffer) ClearRow(y int) {
	db.viewport.ClearRow(y)
}

// ClearViewport clears the entire viewport.
func (db *DisplayBuffer) ClearViewport() {
	db.viewport.EraseScreen()
}

// SetCellXY sets a specific cell in the viewport at (x, y).
func (db *DisplayBuffer) SetCellXY(x, y int, cell Cell) {
	db.viewport.SetCell(x, y, cell)
}

// SetCell sets a cell at a linear offset from (0,0) for backward compatibility.
// offset is converted to (x, y) based on current width.
func (db *DisplayBuffer) SetCell(offset int, cell Cell) {
	width := db.viewport.Width()
	x := offset % width
	y := offset / width
	db.viewport.SetCell(x, y, cell)
}

// InsertCell inserts a cell at offset, shifting content right.
// For backward compatibility with tests.
func (db *DisplayBuffer) InsertCell(offset int, cell Cell) {
	width := db.viewport.Width()
	x := offset % width
	y := offset / width

	// Move cursor to position and use InsertCharacters to shift
	db.SetCursor(x, y)
	db.InsertCharacters(1, DefaultFG, DefaultBG)
	db.viewport.SetCell(x, y, cell)
}

// --- Backward Compatibility Methods ---

// GlobalViewportStart returns the history offset when viewing history.
func (db *DisplayBuffer) GlobalViewportStart() int64 {
	return db.historyViewOffset
}

// MarkRowAsLineStart marks a row as the start of a new logical line.
func (db *DisplayBuffer) MarkRowAsLineStart(y int) {
	db.viewport.MarkRowAsLineStart(y)
}

// MarkRowAsContinuation marks a row as continuation of the previous line.
func (db *DisplayBuffer) MarkRowAsContinuation(y int) {
	db.viewport.MarkRowAsContinuation(y)
}

// ReplaceCurrentLine replaces content at the cursor row with the given cells.
// For backward compatibility with code that modifies lines in place.
func (db *DisplayBuffer) ReplaceCurrentLine(cells []Cell) {
	_, y := db.viewport.Cursor()
	width := db.viewport.Width()

	// Clear the current row and write cells
	db.viewport.ClearRow(y)
	for x, cell := range cells {
		if x < width {
			db.viewport.SetCell(x, y, cell)
		}
	}
}

// SetCursorOffset sets cursor position from a logical offset.
// For backward compatibility - converts offset to (x, y).
func (db *DisplayBuffer) SetCursorOffset(offset int) {
	width := db.viewport.Width()
	if width <= 0 {
		return
	}
	y := offset / width
	x := offset % width
	db.viewport.SetCursor(x, y)
}

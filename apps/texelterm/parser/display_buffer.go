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

	// TUI Content Preservation - frozen lines model.
	// The TUIViewportManager coordinates frozen line commits for TUI applications.
	// Content freezes (commits to history) as it scrolls off the top of the viewport.
	tuiViewportMgr *TUIViewportManager
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
			db.debugLog("[GetViewportAsCells] viewingHistory=true, calling buildHistoryViewGrid")
		}
		return db.buildHistoryViewGrid()
	}
	if db.debugLog != nil {
		db.debugLog("[GetViewportAsCells] viewingHistory=false, returning live viewport")
	}
	return db.viewport.Grid()
}

// buildHistoryViewGrid builds the view when scrolled back into history.
// This includes both regular history lines and the TUI snapshot (if any).
func (db *DisplayBuffer) buildHistoryViewGrid() [][]Cell {
	if db.cachedHistoryView != nil {
		if db.debugLog != nil {
			db.debugLog("[buildHistoryViewGrid] returning cached view")
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

	// Collect all physical lines from history
	// Frozen TUI content is now stored directly in history as fixed-width lines
	var allPhysicalLines []PhysicalLine

	if db.history != nil {
		totalHistoryLines := db.history.TotalLen()
		for i := int64(0); i < totalHistoryLines; i++ {
			line := db.history.GetGlobal(i)
			if line != nil {
				wrapped := line.WrapToWidth(width)
				allPhysicalLines = append(allPhysicalLines, wrapped...)
			}
		}
	}

	totalPhysical := len(allPhysicalLines)
	if totalPhysical == 0 {
		if db.debugLog != nil {
			db.debugLog("[buildHistoryViewGrid] no physical lines, returning empty")
		}
		// Reset scroll state if history is empty
		db.historyViewOffset = 0
		db.viewingHistory = false
		return result
	}

	// Clamp historyViewOffset to valid range.
	// This handles cases where history was truncated externally (e.g., by TUI mode exit).
	maxScroll := int64(totalPhysical - height)
	if maxScroll < 0 {
		maxScroll = 0
	}
	if db.historyViewOffset > maxScroll {
		if db.debugLog != nil {
			db.debugLog("[buildHistoryViewGrid] clamping historyViewOffset from %d to %d (history truncated)",
				db.historyViewOffset, maxScroll)
		}
		db.historyViewOffset = maxScroll
		db.cachedHistoryView = nil // Invalidate any stale cache
		if db.historyViewOffset == 0 {
			db.viewingHistory = false
		}
	}

	if db.debugLog != nil {
		db.debugLog("[buildHistoryViewGrid] totalPhysical=%d, historyViewOffset=%d",
			totalPhysical, db.historyViewOffset)
	}

	// Calculate which physical lines to show
	// historyViewOffset is how many physical rows we've scrolled back from the end
	physicalRowsBack := int(db.historyViewOffset)

	// We want to show starting at (total - offset - height)
	startIdx := totalPhysical - physicalRowsBack - height
	if startIdx < 0 {
		startIdx = 0
	}

	for y := 0; y < height && startIdx+y < totalPhysical; y++ {
		pl := allPhysicalLines[startIdx+y]
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

// ScrollContentUp scrolls content up (content moves up, new blank line at bottom).
// Called when LF at bottom of scroll region (terminal escape sequence behavior).
// In TUI mode, the line scrolling off the top is frozen to history.
func (db *DisplayBuffer) ScrollContentUp(n int) int {
	// In TUI mode, freeze the line at row 0 before it scrolls off
	if db.tuiViewportMgr != nil && db.tuiViewportMgr.IsActive() && n > 0 {
		db.freezeRowToHistory(0)
	}
	db.viewport.ScrollContentUp(n)
	return n
}

// ScrollContentDown scrolls content down (content moves down, new blank line at top).
// Called for reverse index (terminal escape sequence behavior).
func (db *DisplayBuffer) ScrollContentDown(n int) int {
	db.viewport.ScrollContentDown(n)
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

// ScrollViewUp scrolls the view up into history (for user scrollback).
// This is user navigation, not terminal escape sequences.
func (db *DisplayBuffer) ScrollViewUp(lines int) int {
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

// ScrollViewDown scrolls the view down toward live edge.
// This is user navigation, not terminal escape sequences.
func (db *DisplayBuffer) ScrollViewDown(lines int) int {
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
	width := db.viewport.Width()

	// Count total physical lines in history
	// Frozen TUI content is now stored directly in history, not separately
	var totalPhysical int64
	if db.history != nil {
		for i := int64(0); i < db.history.TotalLen(); i++ {
			line := db.history.GetGlobal(i)
			if line != nil {
				wrapped := line.WrapToWidth(width)
				totalPhysical += int64(len(wrapped))
			}
		}
	}

	// Can scroll back by total physical lines minus viewport height
	max := totalPhysical - int64(db.viewport.Height())
	if max < 0 {
		max = 0
	}
	return max
}

// --- TUI Viewport Manager Integration ---

// SetTUIViewportManager sets the TUI viewport manager for frozen line coordination.
func (db *DisplayBuffer) SetTUIViewportManager(mgr *TUIViewportManager) {
	db.tuiViewportMgr = mgr
}

// GetTUIViewportManager returns the TUI viewport manager.
func (db *DisplayBuffer) GetTUIViewportManager() *TUIViewportManager {
	return db.tuiViewportMgr
}

// CommitFrozenLines commits viewport rows as frozen (fixed-width) lines.
// Uses the truncate+append pattern via the TUIViewportManager.
// Returns the number of lines committed.
func (db *DisplayBuffer) CommitFrozenLines() int {
	if db.tuiViewportMgr == nil {
		return 0
	}
	return db.tuiViewportMgr.CommitLiveViewport()
}

// CommitBeforeScreenClear captures the current viewport state before screen erase.
// This should be called BEFORE displayBufferEraseScreen() to capture content
// (like token usage) before it's erased, replacing transient content (like autocomplete menus).
func (db *DisplayBuffer) CommitBeforeScreenClear() {
	if db.tuiViewportMgr != nil {
		db.tuiViewportMgr.CommitBeforeScreenClear()
	}
}

// freezeRowToHistory freezes a single row to history in TUI mode.
// This is called when a line scrolls off the top of the screen/region.
func (db *DisplayBuffer) freezeRowToHistory(row int) {
	if db.tuiViewportMgr == nil || db.viewport == nil {
		return
	}
	if row < 0 || row >= db.viewport.Height() {
		return
	}
	// Skip rows that are already committed (came from history)
	if db.viewport.IsRowCommitted(row) {
		return
	}
	// Create a LogicalLine from the row
	width := db.viewport.Width()
	cells := make([]Cell, width)
	copy(cells, db.viewport.Grid()[row])
	line := &LogicalLine{
		Cells:      cells,
		FixedWidth: width,
	}
	line.TrimTrailingSpaces()
	db.tuiViewportMgr.FreezeScrolledLines([]*LogicalLine{line})
}

// FinalizeOnTUIExit commits any remaining live viewport content when TUI mode ends.
// Called automatically by TUIMode.Reset() via TUIViewportManager.ExitTUIMode().
func (db *DisplayBuffer) FinalizeOnTUIExit() {
	if db.tuiViewportMgr != nil {
		db.tuiViewportMgr.ExitTUIMode()
	}
}

// --- Resize ---

// physicalOffsetToLogical converts a physical row offset (from the end of history)
// to a width-independent (logicalIndex, charOffset) coordinate.
// This is used to preserve scroll position across width changes.
func (db *DisplayBuffer) physicalOffsetToLogical(physicalOffset int64, width int) (logicalIdx int64, charOffset int) {
	if db.history == nil || db.history.TotalLen() == 0 {
		return 0, 0
	}

	// Count physical rows from the end of history until we reach physicalOffset
	var physicalCount int64
	totalLines := db.history.TotalLen()

	for i := totalLines - 1; i >= 0; i-- {
		line := db.history.GetGlobal(i)
		if line == nil {
			continue
		}
		wrapped := line.WrapToWidth(width)
		wrappedLen := int64(len(wrapped))

		// Check if our target offset falls within this logical line
		if physicalCount+wrappedLen > physicalOffset {
			// The offset is within this logical line
			// Calculate which wrapped row within this line
			rowWithinLine := int(physicalOffset - physicalCount)
			// Convert to character offset (approximate: row * width)
			charOffset = rowWithinLine * width
			return i, charOffset
		}
		physicalCount += wrappedLen
	}

	// If we get here, the offset is beyond history - return start
	return 0, 0
}

// logicalToPhysicalOffset converts a (logicalIndex, charOffset) coordinate
// to a physical row offset at the given width.
func (db *DisplayBuffer) logicalToPhysicalOffset(logicalIdx int64, charOffset int, width int) int64 {
	if db.history == nil || db.history.TotalLen() == 0 {
		return 0
	}

	var physicalOffset int64
	totalLines := db.history.TotalLen()

	// Count physical rows from logicalIdx+1 to the end (lines after our anchor)
	for i := logicalIdx + 1; i < totalLines; i++ {
		line := db.history.GetGlobal(i)
		if line != nil {
			wrapped := line.WrapToWidth(width)
			physicalOffset += int64(len(wrapped))
		}
	}

	// Add the rows within our anchor line (from charOffset to end of line)
	anchorLine := db.history.GetGlobal(logicalIdx)
	if anchorLine != nil {
		wrapped := anchorLine.WrapToWidth(width)
		// Find which wrapped row corresponds to charOffset
		rowWithinLine := charOffset / width
		if rowWithinLine >= len(wrapped) {
			rowWithinLine = len(wrapped) - 1
		}
		if rowWithinLine < 0 {
			rowWithinLine = 0
		}
		// Add remaining rows from this line (from rowWithinLine to end)
		physicalOffset += int64(len(wrapped) - rowWithinLine - 1)
	}

	return physicalOffset
}

// Resize changes the viewport dimensions and reflows content from history.
// If currently viewing history, it preserves the scroll position across the resize.
func (db *DisplayBuffer) Resize(newWidth, newHeight int) {
	if newWidth == db.viewport.Width() && newHeight == db.viewport.Height() {
		return
	}

	oldWidth := db.viewport.Width()
	wasViewingHistory := db.viewingHistory
	var anchorLogicalIdx int64
	var anchorCharOffset int

	// If viewing history, save position as (logicalLine, charOffset) before resize
	if wasViewingHistory && db.history != nil && db.historyViewOffset > 0 {
		anchorLogicalIdx, anchorCharOffset = db.physicalOffsetToLogical(db.historyViewOffset, oldWidth)
	}

	// Resize the viewport structure
	db.viewport.Resize(newWidth, newHeight)
	db.cachedHistoryView = nil // Invalidate cache on resize

	if db.history != nil && db.history.TotalLen() > 0 {
		// Always repopulate viewport from history to reflow content at new width.
		// This ensures the live viewport has correctly wrapped content.
		// The shell will redraw its current prompt line via SIGWINCH handling.
		db.PopulateViewportFromHistory()

		// If we were viewing history, restore that state after repopulating
		if wasViewingHistory {
			// Recalculate physical offset at new width
			db.historyViewOffset = db.logicalToPhysicalOffset(anchorLogicalIdx, anchorCharOffset, newWidth)

			// Validate offset doesn't exceed max
			maxScroll := db.calculateMaxHistoryScroll()
			if db.historyViewOffset > maxScroll {
				db.historyViewOffset = maxScroll
			}

			// Restore viewing history mode and suppress auto-scroll
			// (shell will redraw via SIGWINCH which triggers SetCursor - we don't want to jump)
			db.viewingHistory = true
			db.restoredView = true
		}
	}
}

// loadHistoryWithCursorAt fills the viewport with history lines, placing the cursor
// at the specified row. History fills rows 0 to targetCursorY-1, leaving row targetCursorY
// and below empty for the shell to draw the prompt.
// skipFromEnd specifies how many logical lines to skip from the end of history (to hide old prompts).
func (db *DisplayBuffer) loadHistoryWithCursorAt(targetCursorY int, skipFromEnd int) (cursorX, cursorY int) {
	if db.history == nil || db.history.TotalLen() == 0 {
		return 0, targetCursorY
	}

	width := db.viewport.Width()
	totalHistoryLines := db.history.TotalLen()

	// We want to fill rows 0 to targetCursorY-1 with history
	maxLinesToShow := targetCursorY
	if maxLinesToShow <= 0 {
		// No history to show, just position cursor
		db.viewport.EraseScreen()
		db.viewport.SetCursor(0, 0)
		db.viewingHistory = false
		db.historyViewOffset = 0
		db.cachedHistoryView = nil
		db.hasLiveContent = false
		return 0, 0
	}

	// Calculate where to start in history (skip lines from end to hide old prompts)
	startIdx := totalHistoryLines - 1 - int64(skipFromEnd)
	if startIdx < 0 {
		startIdx = 0
	}

	log.Printf("[loadHistoryWithCursorAt] targetCursorY=%d, maxLinesToShow=%d, historyLen=%d, skipFromEnd=%d, startIdx=%d",
		targetCursorY, maxLinesToShow, totalHistoryLines, skipFromEnd, startIdx)

	// Work backwards from startIdx to collect physical lines
	var physicalLines []PhysicalLine
	logicalIdx := startIdx

	for len(physicalLines) < maxLinesToShow && logicalIdx >= 0 {
		line := db.history.GetGlobal(logicalIdx)
		if line == nil {
			logicalIdx--
			continue
		}

		wrapped := line.WrapToWidth(width)
		// Prepend wrapped lines (since we're going backwards)
		physicalLines = append(wrapped, physicalLines...)
		logicalIdx--
	}

	// Trim to maxLinesToShow if we collected more
	if len(physicalLines) > maxLinesToShow {
		physicalLines = physicalLines[len(physicalLines)-maxLinesToShow:]
	}

	// Clear the viewport first
	db.viewport.EraseScreen()

	// Write the physical lines to the viewport
	for y, pline := range physicalLines {
		for x, cell := range pline.Cells {
			if x < width {
				db.viewport.SetCell(x, y, cell)
			}
		}
		if pline.Offset == 0 {
			db.viewport.MarkRowAsLineStart(y)
		} else {
			db.viewport.MarkRowAsContinuation(y)
		}
		db.viewport.MarkRowAsCommitted(y)
	}

	// Position cursor at the target row (where shell will draw prompt)
	cursorX = 0
	cursorY = len(physicalLines)
	if cursorY > targetCursorY {
		cursorY = targetCursorY
	}

	db.viewport.SetCursor(cursorX, cursorY)

	// Reset scroll state
	db.viewingHistory = false
	db.historyViewOffset = 0
	db.cachedHistoryView = nil
	db.hasLiveContent = false

	log.Printf("[loadHistoryWithCursorAt] Populated %d lines, cursor at (%d,%d)", len(physicalLines), cursorX, cursorY)

	return cursorX, cursorY
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

// PopulateViewportFromHistoryToPrompt fills the viewport from history up to (but not including)
// the prompt line. This enables seamless shell recovery - when the shell restarts, it draws
// its prompt and the user doesn't see a duplicate.
//
// promptLine: Global line index where the prompt starts (-1 to fall back to regular populate)
// promptHeight: Number of lines in the prompt (used to hide the previous prompt from history)
//
// If promptLine is -1, falls back to PopulateViewportFromHistory.
// If promptLine > historyLen (prompt was on viewport, not in history), we calculate the
// implied viewport row and position the cursor there.
func (db *DisplayBuffer) PopulateViewportFromHistoryToPrompt(promptLine int64, promptHeight int) (cursorX, cursorY int) {
	if db.history == nil || db.history.TotalLen() == 0 {
		return 0, 0
	}

	totalHistoryLines := db.history.TotalLen()
	height := db.viewport.Height()

	// If promptLine is negative, fall back to regular behavior
	if promptLine < 0 {
		log.Printf("[PopulateViewportFromHistoryToPrompt] Invalid promptLine=%d, falling back", promptLine)
		return db.PopulateViewportFromHistory()
	}

	// If promptLine is beyond history, the prompt was on the viewport (not committed yet).
	// Calculate the implied viewport row where the prompt was.
	if promptLine >= totalHistoryLines {
		// promptLine = historyLen_at_save + viewportRow
		// So: viewportRow = promptLine - historyLen_at_save
		// But we don't have historyLen_at_save. However, if nothing was committed between
		// save and restore, historyLen is the same, so:
		impliedViewportRow := int(promptLine - totalHistoryLines)
		log.Printf("[PopulateViewportFromHistoryToPrompt] promptLine=%d > historyLen=%d, impliedViewportRow=%d",
			promptLine, totalHistoryLines, impliedViewportRow)

		// The history contains the PREVIOUS prompt (from the last command cycle).
		// Skip enough lines to hide it: promptHeight * 2 (current + previous prompt).
		// Use minimum of 4 to handle common multiline prompts even if detection fails.
		skipFromEnd := promptHeight * 2
		if skipFromEnd < 4 {
			skipFromEnd = 4
		}

		// Clamp viewport row to valid range
		if impliedViewportRow < 0 {
			impliedViewportRow = 0
		}
		if impliedViewportRow >= height {
			impliedViewportRow = height - 1
		}

		log.Printf("[PopulateViewportFromHistoryToPrompt] impliedViewportRow=%d, skipping %d lines from end of history",
			impliedViewportRow, skipFromEnd)

		// Fill viewport with history (skipping old prompt), cursor at implied row
		return db.loadHistoryWithCursorAt(impliedViewportRow, skipFromEnd)
	}

	width := db.viewport.Width()

	log.Printf("[PopulateViewportFromHistoryToPrompt] Starting (within history): promptLine=%d, totalHistoryLines=%d, height=%d, width=%d", promptLine, totalHistoryLines, height, width)

	// We need to show history UP TO the prompt line (exclusive - the prompt line itself is where
	// the shell will redraw its prompt). So we include lines 0 to promptLine-1.
	// Work backwards from promptLine-1 to fill the viewport.
	var physicalLines []PhysicalLine
	logicalIdx := promptLine - 1

	for len(physicalLines) < height && logicalIdx >= 0 {
		line := db.history.GetGlobal(logicalIdx)
		if line == nil {
			log.Printf("[PopulateViewportFromHistoryToPrompt] GetGlobal(%d) returned nil", logicalIdx)
			logicalIdx--
			continue
		}

		wrapped := line.WrapToWidth(width)
		log.Printf("[PopulateViewportFromHistoryToPrompt] GetGlobal(%d): len=%d, wrapped to %d physical lines",
			logicalIdx, line.Len(), len(wrapped))
		// Prepend wrapped lines (since we're going backwards)
		physicalLines = append(wrapped, physicalLines...)
		logicalIdx--
	}

	log.Printf("[PopulateViewportFromHistoryToPrompt] Collected %d physical lines", len(physicalLines))

	// Trim to viewport height - 1, leaving room for shell prompt on last row.
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
		if pline.Offset == 0 {
			db.viewport.MarkRowAsLineStart(y)
		} else {
			db.viewport.MarkRowAsContinuation(y)
		}
		// Mark as committed to prevent re-committing when these rows scroll off.
		db.viewport.MarkRowAsCommitted(y)
	}

	// Position cursor at the START of the line AFTER history content.
	// The shell will draw its prompt here.
	cursorX = 0
	cursorY = len(physicalLines)

	// Actually set the cursor on the viewport
	db.viewport.SetCursor(cursorX, cursorY)

	// Reset scroll state - we're now at the live edge
	db.viewingHistory = false
	db.historyViewOffset = 0
	db.cachedHistoryView = nil

	// Clear the live content flag
	db.hasLiveContent = false

	if db.debugLog != nil {
		db.debugLog("[PopulateViewportFromHistoryToPrompt] Populated %d lines (height=%d), cursor set to (%d,%d)", len(physicalLines), height, cursorX, cursorY)
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

// LastPromptLine returns the global line index of the last prompt.
// Returns -1 if no prompt position has been recorded.
func (db *DisplayBuffer) LastPromptLine() int64 {
	return db.viewport.LastPromptLine()
}

// LastPromptHeight returns the number of lines in the prompt.
// Defaults to 1 for single-line prompts.
func (db *DisplayBuffer) LastPromptHeight() int {
	return db.viewport.LastPromptHeight()
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
// In TUI mode, the line at the top of the region is frozen to history before scrolling off.
func (db *DisplayBuffer) ScrollRegionUp(top, bottom, n int) {
	// In TUI mode, freeze the line at the top of the scroll region before it scrolls off
	if db.tuiViewportMgr != nil && db.tuiViewportMgr.IsActive() && n > 0 {
		db.freezeRowToHistory(top)
	}
	db.viewport.ScrollRegionUp(top, bottom, n)
}

// ScrollRegionDown scrolls within a region.
func (db *DisplayBuffer) ScrollRegionDown(top, bottom, n int) {
	db.viewport.ScrollRegionDown(top, bottom, n)
}

// ScrollColumnsUp scrolls content up within a column range (for left/right margin scrolling).
// This is used when left/right margins are set and content needs to scroll within them.
func (db *DisplayBuffer) ScrollColumnsUp(top, bottom, leftCol, rightCol, n int, clearFG, clearBG Color) {
	db.viewport.ScrollColumnsUp(top, bottom, leftCol, rightCol, n, clearFG, clearBG)
}

// ScrollColumnsDown scrolls content down within a column range (for left/right margin scrolling).
func (db *DisplayBuffer) ScrollColumnsDown(top, bottom, leftCol, rightCol, n int, clearFG, clearBG Color) {
	db.viewport.ScrollColumnsDown(top, bottom, leftCol, rightCol, n, clearFG, clearBG)
}

// ScrollColumnsHorizontal scrolls content horizontally within specified bounds.
// n > 0: shift right (blank at left), n < 0: shift left (blank at right).
func (db *DisplayBuffer) ScrollColumnsHorizontal(top, bottom, leftCol, rightCol, n int, clearFG, clearBG Color) {
	db.viewport.ScrollColumnsHorizontal(top, bottom, leftCol, rightCol, n, clearFG, clearBG)
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

// --- Selection Coordinate Conversion ---

// ViewportToContent converts viewport coordinates (y, x) to content coordinates.
// Returns (logicalLine, charOffset, isCurrentLine, ok).
// - logicalLine: the global logical line index in history (-1 if current line)
// - charOffset: character offset within that logical line
// - isCurrentLine: true if this is the uncommitted current line
// - ok: false if coordinates are out of range
func (db *DisplayBuffer) ViewportToContent(y, x int) (logicalLine int64, charOffset int, isCurrentLine bool, ok bool) {
	height := db.viewport.Height()
	width := db.viewport.Width()

	if y < 0 || y >= height || x < 0 {
		return 0, 0, false, false
	}

	if db.viewingHistory {
		// When scrolled back, compute which logical line is at viewport row y
		return db.viewportToContentHistory(y, x, width, height)
	}

	// At live edge - viewport shows recent history + current line
	return db.viewportToContentLive(y, x, width, height)
}

// viewportToContentHistory handles coordinate conversion when viewing history.
func (db *DisplayBuffer) viewportToContentHistory(y, x, width, height int) (int64, int, bool, bool) {
	if db.history == nil {
		return 0, 0, false, false
	}

	totalHistoryLines := db.history.TotalLen()
	if totalHistoryLines == 0 {
		return 0, 0, false, false
	}

	// Build physical lines from history (same logic as getHistoryView)
	physicalRowsBack := db.historyViewOffset
	var physicalLines []PhysicalLine
	logicalIdx := totalHistoryLines - 1

	for logicalIdx >= 0 && int64(len(physicalLines)) < physicalRowsBack+int64(height) {
		line := db.history.GetGlobal(logicalIdx)
		if line != nil {
			wrapped := line.WrapToWidth(width)
			// Set LogicalIndex for each physical line
			for i := range wrapped {
				wrapped[i].LogicalIndex = int(logicalIdx)
			}
			// Prepend
			newLines := make([]PhysicalLine, len(wrapped)+len(physicalLines))
			copy(newLines, wrapped)
			copy(newLines[len(wrapped):], physicalLines)
			physicalLines = newLines
		}
		logicalIdx--
	}

	// Calculate which physical line corresponds to viewport row y
	startIdx := len(physicalLines) - int(physicalRowsBack) - height
	if startIdx < 0 {
		startIdx = 0
	}

	physIdx := startIdx + y
	if physIdx < 0 || physIdx >= len(physicalLines) {
		return 0, 0, false, false
	}

	pl := physicalLines[physIdx]
	charOffset := pl.Offset + x
	if x >= len(pl.Cells) {
		charOffset = pl.Offset + len(pl.Cells)
	}

	return int64(pl.LogicalIndex), charOffset, false, true
}

// viewportToContentLive handles coordinate conversion at live edge.
func (db *DisplayBuffer) viewportToContentLive(y, x, width, height int) (int64, int, bool, bool) {
	// At live edge, the viewport shows:
	// - Recent history lines at top (if any)
	// - Current uncommitted line at bottom (possibly spanning multiple rows)

	if db.history == nil {
		// No history - everything is current line
		charOffset := y*width + x
		return -1, charOffset, true, true
	}

	// Get current line info
	currentLine := db.CurrentLine()
	currentLineRows := 1
	if currentLine != nil && len(currentLine.Cells) > 0 {
		currentLineRows = (len(currentLine.Cells) + width - 1) / width
	}

	// The current line occupies the bottom currentLineRows of the viewport
	// History fills the top (height - currentLineRows) rows
	historyRowsVisible := height - currentLineRows
	if historyRowsVisible < 0 {
		historyRowsVisible = 0
	}

	if y >= historyRowsVisible {
		// Clicking on current line
		rowInCurrentLine := y - historyRowsVisible
		charOffset := rowInCurrentLine*width + x
		if currentLine != nil && charOffset > len(currentLine.Cells) {
			charOffset = len(currentLine.Cells)
		}
		return -1, charOffset, true, true
	}

	// Clicking on history portion
	// Need to figure out which logical line is at viewport row y
	totalHistoryLines := db.history.TotalLen()
	if totalHistoryLines == 0 {
		return -1, y*width + x, true, true
	}

	// Build physical lines for the visible history portion
	var physicalLines []PhysicalLine
	logicalIdx := totalHistoryLines - 1

	for logicalIdx >= 0 && len(physicalLines) < historyRowsVisible {
		line := db.history.GetGlobal(logicalIdx)
		if line != nil {
			wrapped := line.WrapToWidth(width)
			for i := range wrapped {
				wrapped[i].LogicalIndex = int(logicalIdx)
			}
			// Prepend
			newLines := make([]PhysicalLine, len(wrapped)+len(physicalLines))
			copy(newLines, wrapped)
			copy(newLines[len(wrapped):], physicalLines)
			physicalLines = newLines
		}
		logicalIdx--
	}

	// Trim to exactly historyRowsVisible from the end
	if len(physicalLines) > historyRowsVisible {
		physicalLines = physicalLines[len(physicalLines)-historyRowsVisible:]
	}

	if y < 0 || y >= len(physicalLines) {
		return 0, 0, false, false
	}

	pl := physicalLines[y]
	charOffset := pl.Offset + x
	if x >= len(pl.Cells) {
		charOffset = pl.Offset + len(pl.Cells)
	}

	return int64(pl.LogicalIndex), charOffset, false, true
}

// ContentToViewport converts content coordinates back to viewport coordinates.
// Returns (y, x, visible) where visible is true if the content is currently on screen.
// For logicalLine == -1, this refers to the current uncommitted line.
func (db *DisplayBuffer) ContentToViewport(logicalLine int64, charOffset int) (y, x int, visible bool) {
	height := db.viewport.Height()
	width := db.viewport.Width()

	if width <= 0 || height <= 0 {
		return 0, 0, false
	}

	if db.viewingHistory {
		return db.contentToViewportHistory(logicalLine, charOffset, width, height)
	}

	return db.contentToViewportLive(logicalLine, charOffset, width, height)
}

// contentToViewportHistory handles reverse conversion when viewing history.
func (db *DisplayBuffer) contentToViewportHistory(logicalLine int64, charOffset, width, height int) (int, int, bool) {
	if logicalLine < 0 {
		// Current line is not visible when viewing history
		return 0, 0, false
	}

	if db.history == nil {
		return 0, 0, false
	}

	totalHistoryLines := db.history.TotalLen()
	if logicalLine >= totalHistoryLines {
		return 0, 0, false
	}

	// Build physical lines from history
	physicalRowsBack := db.historyViewOffset
	var physicalLines []PhysicalLine
	logicalIdx := totalHistoryLines - 1

	for logicalIdx >= 0 && int64(len(physicalLines)) < physicalRowsBack+int64(height) {
		line := db.history.GetGlobal(logicalIdx)
		if line != nil {
			wrapped := line.WrapToWidth(width)
			for i := range wrapped {
				wrapped[i].LogicalIndex = int(logicalIdx)
			}
			newLines := make([]PhysicalLine, len(wrapped)+len(physicalLines))
			copy(newLines, wrapped)
			copy(newLines[len(wrapped):], physicalLines)
			physicalLines = newLines
		}
		logicalIdx--
	}

	startIdx := len(physicalLines) - int(physicalRowsBack) - height
	if startIdx < 0 {
		startIdx = 0
	}

	// Find the physical line that matches our logical line and offset
	for viewY := 0; viewY < height && startIdx+viewY < len(physicalLines); viewY++ {
		pl := physicalLines[startIdx+viewY]
		if int64(pl.LogicalIndex) == logicalLine {
			// Check if charOffset falls within this physical line
			if charOffset >= pl.Offset && charOffset < pl.Offset+width {
				return viewY, charOffset - pl.Offset, true
			}
			// charOffset might be at end of line
			if charOffset == pl.Offset+len(pl.Cells) && len(pl.Cells) < width {
				return viewY, len(pl.Cells), true
			}
		}
	}

	return 0, 0, false
}

// contentToViewportLive handles reverse conversion at live edge.
func (db *DisplayBuffer) contentToViewportLive(logicalLine int64, charOffset, width, height int) (int, int, bool) {
	// Get current line info
	currentLine := db.CurrentLine()
	currentLineRows := 1
	if currentLine != nil && len(currentLine.Cells) > 0 {
		currentLineRows = (len(currentLine.Cells) + width - 1) / width
	}

	historyRowsVisible := height - currentLineRows
	if historyRowsVisible < 0 {
		historyRowsVisible = 0
	}

	if logicalLine < 0 {
		// Current line
		rowInCurrent := charOffset / width
		col := charOffset % width
		viewY := historyRowsVisible + rowInCurrent
		if viewY >= height {
			return 0, 0, false
		}
		return viewY, col, true
	}

	// History line
	if db.history == nil {
		return 0, 0, false
	}

	totalHistoryLines := db.history.TotalLen()
	if logicalLine >= totalHistoryLines {
		return 0, 0, false
	}

	// Build physical lines for visible history
	var physicalLines []PhysicalLine
	logicalIdx := totalHistoryLines - 1

	for logicalIdx >= 0 && len(physicalLines) < historyRowsVisible {
		line := db.history.GetGlobal(logicalIdx)
		if line != nil {
			wrapped := line.WrapToWidth(width)
			for i := range wrapped {
				wrapped[i].LogicalIndex = int(logicalIdx)
			}
			newLines := make([]PhysicalLine, len(wrapped)+len(physicalLines))
			copy(newLines, wrapped)
			copy(newLines[len(wrapped):], physicalLines)
			physicalLines = newLines
		}
		logicalIdx--
	}

	if len(physicalLines) > historyRowsVisible {
		physicalLines = physicalLines[len(physicalLines)-historyRowsVisible:]
	}

	// Find matching physical line
	for viewY, pl := range physicalLines {
		if int64(pl.LogicalIndex) == logicalLine {
			if charOffset >= pl.Offset && charOffset < pl.Offset+width {
				return viewY, charOffset - pl.Offset, true
			}
			if charOffset == pl.Offset+len(pl.Cells) && len(pl.Cells) < width {
				return viewY, len(pl.Cells), true
			}
		}
	}

	return 0, 0, false
}

// GetContentText extracts text from a range of content coordinates.
// For logicalLine == -1, uses the current uncommitted line.
func (db *DisplayBuffer) GetContentText(startLine int64, startOffset int, endLine int64, endOffset int) string {
	if startLine > endLine || (startLine == endLine && startOffset > endOffset) {
		// Swap to normalize
		startLine, endLine = endLine, startLine
		startOffset, endOffset = endOffset, startOffset
	}

	var result []rune

	for line := startLine; line <= endLine; line++ {
		var cells []Cell

		if line < 0 {
			// Current line
			currentLine := db.CurrentLine()
			if currentLine != nil {
				cells = currentLine.Cells
			}
		} else if db.history != nil {
			logicalLine := db.history.GetGlobal(line)
			if logicalLine != nil {
				cells = logicalLine.Cells
			}
		}

		lineStart := 0
		lineEnd := len(cells)

		if line == startLine {
			lineStart = startOffset
			if lineStart > lineEnd {
				lineStart = lineEnd
			}
		}
		if line == endLine {
			lineEnd = endOffset
			if lineEnd > len(cells) {
				lineEnd = len(cells)
			}
		}

		for i := lineStart; i < lineEnd; i++ {
			r := cells[i].Rune
			if r == 0 {
				r = ' '
			}
			result = append(result, r)
		}

		// Add newline between logical lines (but not after last)
		if line < endLine {
			result = append(result, '\n')
		}
	}

	return string(result)
}

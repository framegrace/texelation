// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_display_buffer.go
// Summary: Display buffer integration for VTerm - enables proper scrollback reflow.
// Usage: Provides alternate Grid/scroll/resize paths using the display buffer architecture.

package parser

import (
	"fmt"
	"log"
	"os"
)

// displayBufferState holds the new reflow-capable scrollback system.
// This runs alongside the existing historyManager during migration.
type displayBufferState struct {
	// history stores logical (unwrapped) lines - the source of truth
	history *ScrollbackHistory

	// display manages the physical viewport with margins
	display *DisplayBuffer

	// enabled toggles between old and new rendering paths
	enabled bool
}

// DisplayBufferOptions configures the display buffer system.
type DisplayBufferOptions struct {
	// MaxMemoryLines is the max logical lines to keep in memory (default 5000).
	MaxMemoryLines int
	// MarginAbove is display buffer margin above viewport (default 200).
	MarginAbove int
	// MarginBelow is display buffer margin below viewport (default 50).
	MarginBelow int
	// DiskPath enables disk persistence if non-empty.
	DiskPath string
}

// DefaultDisplayBufferOptions returns sensible defaults.
func DefaultDisplayBufferOptions() DisplayBufferOptions {
	return DisplayBufferOptions{
		MaxMemoryLines: DefaultMaxMemoryLines,
		MarginAbove:    DefaultMarginAbove,
		MarginBelow:    DefaultMarginBelow,
		DiskPath:       "",
	}
}

// initDisplayBuffer initializes the display buffer system for VTerm.
// Called from NewVTerm when the feature is enabled.
func (v *VTerm) initDisplayBuffer() {
	v.initDisplayBufferWithOptions(DefaultDisplayBufferOptions())
}

// initDisplayBufferWithOptions initializes the display buffer with custom options.
func (v *VTerm) initDisplayBufferWithOptions(opts DisplayBufferOptions) {
	if opts.MaxMemoryLines <= 0 {
		opts.MaxMemoryLines = DefaultMaxMemoryLines
	}
	if opts.MarginAbove <= 0 {
		opts.MarginAbove = DefaultMarginAbove
	}
	if opts.MarginBelow <= 0 {
		opts.MarginBelow = DefaultMarginBelow
	}

	var history *ScrollbackHistory
	var err error

	if opts.DiskPath != "" {
		// Create disk-backed history
		history, err = NewScrollbackHistoryWithDisk(ScrollbackHistoryConfig{
			MaxMemoryLines: opts.MaxMemoryLines,
			MarginAbove:    opts.MarginAbove,
			MarginBelow:    opts.MarginBelow,
			DiskPath:       opts.DiskPath,
		})
		if err != nil {
			log.Printf("[DISPLAY_BUFFER] Failed to create disk-backed history: %v, falling back to memory-only", err)
			history = nil
		}
	}

	if history == nil {
		// Memory-only history
		history = NewScrollbackHistory(ScrollbackHistoryConfig{
			MaxMemoryLines: opts.MaxMemoryLines,
		})
	}

	v.displayBuf = &displayBufferState{
		history: history,
		enabled: false, // Start disabled, enable explicitly
	}
	v.displayBuf.display = NewDisplayBuffer(v.displayBuf.history, DisplayBufferConfig{
		Width:       v.width,
		Height:      v.height,
		MarginAbove: opts.MarginAbove,
		MarginBelow: opts.MarginBelow,
	})
}

// EnableDisplayBuffer switches to the new display buffer rendering path.
func (v *VTerm) EnableDisplayBuffer() {
	if v.displayBuf == nil {
		v.initDisplayBuffer()
	}
	v.displayBuf.enabled = true

	// Sync cursor position with display buffer's live edge
	v.syncCursorWithDisplayBuffer()
}

// EnableDisplayBufferWithDisk enables the display buffer with disk-backed persistence.
// This bypasses the legacy HistoryManager and uses the new three-level architecture:
// Disk -> Memory (ScrollbackHistory) -> Display (DisplayBuffer)
//
// The diskPath should be the full path to the history file (e.g., ~/.texelation/scrollback/pane-id.hist2).
// If the file exists with valid TXHIST02 format, history is loaded from it.
// If the file doesn't exist or has an old format, starts fresh.
func (v *VTerm) EnableDisplayBufferWithDisk(diskPath string, opts DisplayBufferOptions) error {
	opts.DiskPath = diskPath
	v.initDisplayBufferWithOptions(opts)
	v.displayBuf.enabled = true

	// Sync cursor position with display buffer's live edge
	v.syncCursorWithDisplayBuffer()

	return nil
}

// syncCursorWithDisplayBuffer positions the cursor to match where new content
// will appear in the display buffer. Called after loading history.
func (v *VTerm) syncCursorWithDisplayBuffer() {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}

	// Get the row where new content will appear
	liveEdgeRow := v.displayBuf.display.LiveEdgeRow()

	// Position cursor at the live edge
	v.cursorY = liveEdgeRow
	v.cursorX = 0
}

// CloseDisplayBuffer closes the display buffer and its disk backing (if any).
// Should be called when the terminal is shutting down.
func (v *VTerm) CloseDisplayBuffer() error {
	if v.displayBuf == nil || v.displayBuf.history == nil {
		return nil
	}
	return v.displayBuf.history.Close()
}

// DisableDisplayBuffer switches back to the legacy rendering path.
func (v *VTerm) DisableDisplayBuffer() {
	if v.displayBuf != nil {
		v.displayBuf.enabled = false
	}
}

// IsDisplayBufferEnabled returns whether the display buffer path is active.
func (v *VTerm) IsDisplayBufferEnabled() bool {
	return v.displayBuf != nil && v.displayBuf.enabled
}

// SetDisplayBufferDebugLog sets a debug logging function on the display buffer.
func (v *VTerm) SetDisplayBufferDebugLog(fn func(format string, args ...interface{})) {
	if v.displayBuf != nil && v.displayBuf.display != nil {
		v.displayBuf.display.SetDebugLog(fn)
	}
}

// displayBufferGrid returns the viewport using the display buffer system.
func (v *VTerm) displayBufferGrid() [][]Cell {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return nil
	}
	return v.displayBuf.display.GetViewportAsCells()
}

// displayBufferPlaceChar writes a character using the display buffer system.
// This performs a dual-write: to the current logical line AND the display buffer.
// Respects insert mode (IRM) - in insert mode, shifts existing content right.
func (v *VTerm) displayBufferPlaceChar(r rune) {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}

	// Debug: log every character written
	if v.displayBuf.display.debugLog != nil {
		v.displayBuf.display.debugLog("displayBufferPlaceChar: '%c' at offset %d", r, v.displayBuf.display.GetCursorOffset())
	}

	// Use the new logical editor
	v.displayBuf.display.Write(r, v.currentFG, v.currentBG, v.currentAttr, v.insertMode)

	// Mark all dirty?
	// Ideally we'd get a dirty range. For now, mark all.
	v.MarkAllDirty()
}

// displayBufferLineFeed commits the current line and starts a new one.
// However, it only commits if:
// - cursor is at position 0 (typical CR+LF sequence), OR
// - cursor is at or past the end of line content, OR
// - cursor is on the last physical row of the line (LF would move beyond line)
// If cursor is in the MIDDLE of the line AND not on the last row,
// this is cursor movement (e.g., bash redraw via CR+LF on wrapped lines) - don't commit.
func (v *VTerm) displayBufferLineFeed() {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}

	cursorOffset := v.displayBuf.display.GetCursorOffset()
	lineLen := v.displayBuf.display.CurrentLine().Len()
	width := v.width

	// Calculate which physical row the cursor is on (within the line)
	cursorPhysRow := 0
	if width > 0 {
		cursorPhysRow = cursorOffset / width
	}

	// Calculate how many physical rows the line occupies
	numPhysRows := 1
	if lineLen > 0 && width > 0 {
		numPhysRows = (lineLen + width - 1) / width
	}

	// Skip commit if cursor is NOT at/past the end of the line AND not on the last physical row.
	// This prevents premature commits when bash repositions cursor during editing on wrapped lines.
	// - offset < lineLen means we're before the end of the line content
	// - !onLastPhysRow means LF won't move us past the line
	// If on the last row, LF would move cursor beyond the line, so we should commit.
	onLastPhysRow := cursorPhysRow >= numPhysRows-1
	if cursorOffset < lineLen && !onLastPhysRow {
		if v.displayBuf.display.debugLog != nil {
			v.displayBuf.display.debugLog("displayBufferLineFeed: SKIPPING commit (cursor in middle: offset=%d, lineLen=%d, physRow=%d/%d)",
				cursorOffset, lineLen, cursorPhysRow, numPhysRows)
		}
		return
	}

	// Debug: log before commit
	if v.displayBuf.display.debugLog != nil {
		v.displayBuf.display.debugLog("displayBufferLineFeed: COMMITTING line with len=%d (offset=%d, physRow=%d/%d)",
			lineLen, cursorOffset, cursorPhysRow, numPhysRows)
	}

	// Commit current logical line to history
	v.displayBuf.display.CommitCurrentLine()
}

// displayBufferCarriageReturn handles CR - syncs logical position with physical position (start of line).
func (v *VTerm) displayBufferCarriageReturn() {
	// vterm.go has already set v.cursorX = 0 before calling this.
	// We just need to sync the logical cursor.
	// CR is absolute positioning (column 0), not a relative move.
	v.displayBufferSetCursorFromPhysical(false)
}

// displayBufferScroll handles viewport scrolling.
// Positive delta = scroll down (view newer content, like pressing Page Down)
// Negative delta = scroll up (view older content, like pressing Page Up)
func (v *VTerm) displayBufferScroll(delta int) {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}

	if delta > 0 {
		// Positive delta: scroll down (view newer content)
		v.displayBuf.display.ScrollDown(delta)
	} else if delta < 0 {
		// Negative delta: scroll up (view older content)
		v.displayBuf.display.ScrollUp(-delta)
	}
}

// logDebug writes to the debug log if enabled
func (v *VTerm) logDebug(format string, args ...interface{}) {
	if os.Getenv("TEXELTERM_DEBUG") == "" {
		return
	}
	debugFile, err := os.OpenFile("/tmp/texelterm-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer debugFile.Close()
	fmt.Fprintf(debugFile, "[VTERM] "+format+"\n", args...)
}

// displayBufferResize handles terminal resize with proper reflow.
func (v *VTerm) displayBufferResize(width, height int) {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}

	oldX, oldY := v.cursorX, v.cursorY
	v.logDebug("displayBufferResize START width=%d height=%d cursor=%d,%d", width, height, oldX, oldY)

	v.displayBuf.display.Resize(width, height)

	// Update physical cursor to match logical cursor position in the new layout
	if physX, physY, found := v.displayBuf.display.GetPhysicalCursorPos(); found {
		v.cursorX = physX
		v.cursorY = physY
		v.logDebug("displayBufferResize FOUND cursor offset=%d -> set physical to %d,%d", v.displayBuf.display.GetCursorOffset(), physX, physY)
	} else if v.displayBuf.display.AtLiveEdge() {
		// Fallback: if at live edge but cursor not found (e.g. validly scrolled off?),
		// snap to live edge row.
		v.cursorY = v.displayBuf.display.LiveEdgeRow()
		v.logDebug("displayBufferResize NOT FOUND, snapping to LiveEdgeRow %d", v.cursorY)
		// Keep cursorX clamped later by SetCursorPos
	} else {
		v.logDebug("displayBufferResize NOT FOUND and NOT at live edge")
	}
}

// displayBufferScrollToBottom scrolls to live edge.
func (v *VTerm) displayBufferScrollToBottom() {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}

	v.displayBuf.display.ScrollToBottom()
}

// displayBufferAtLiveEdge returns whether viewport is at the live edge.
func (v *VTerm) displayBufferAtLiveEdge() bool {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return true
	}
	return v.displayBuf.display.AtLiveEdge()
}

// AtLiveEdge returns whether the viewport is at the live edge (bottom of output).
func (v *VTerm) AtLiveEdge() bool {
	return v.displayBufferAtLiveEdge()
}

// ScrollToLiveEdge scrolls the viewport to the live edge (bottom of output).
func (v *VTerm) ScrollToLiveEdge() {
	v.displayBufferScrollToBottom()
	v.MarkAllDirty()
}

// displayBufferSetCursorFromPhysical syncs the display buffer cursor with VTerm's cursor.
// In the new viewport-based architecture, this is simple: just set the cursor position.
// The isRelativeMove parameter is kept for API compatibility but no longer affects behavior.
func (v *VTerm) displayBufferSetCursorFromPhysical(isRelativeMove bool) {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}

	// In the new architecture, cursor can move anywhere freely.
	// Just sync the display buffer cursor to VTerm's cursor position.
	v.displayBuf.display.SetCursor(v.cursorX, v.cursorY)

	// Update prevCursor tracking
	v.prevCursorX = v.cursorX
	v.prevCursorY = v.cursorY
}

// displayBufferClear clears the display buffer and history.
func (v *VTerm) displayBufferClear() {
	if v.displayBuf == nil {
		return
	}

	v.displayBuf.history.Clear()
	v.displayBuf.display = NewDisplayBuffer(v.displayBuf.history, DisplayBufferConfig{
		Width:       v.width,
		Height:      v.height,
		MarginAbove: DefaultMarginAbove,
		MarginBelow: DefaultMarginBelow,
	})
}

// displayBufferGetCurrentLine returns the current (uncommitted) logical line.
func (v *VTerm) displayBufferGetCurrentLine() *LogicalLine {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return nil
	}
	return v.displayBuf.display.CurrentLine()
}

// displayBufferHistoryLen returns the number of committed logical lines in memory.
func (v *VTerm) displayBufferHistoryLen() int {
	if v.displayBuf == nil || v.displayBuf.history == nil {
		return 0
	}
	return v.displayBuf.history.Len()
}

// displayBufferHistoryTotalLen returns the total number of lines including disk.
func (v *VTerm) displayBufferHistoryTotalLen() int64 {
	if v.displayBuf == nil || v.displayBuf.history == nil {
		return 0
	}
	return v.displayBuf.history.TotalLen()
}

// displayBufferLoadHistory loads logical lines into the display buffer history.
// This is used when restoring from persisted history.
func (v *VTerm) displayBufferLoadHistory(lines []*LogicalLine) {
	if v.displayBuf == nil {
		v.initDisplayBuffer()
	}

	for _, line := range lines {
		v.displayBuf.history.Append(line)
	}

	// Rebuild the display buffer with loaded history
	v.displayBuf.display = NewDisplayBuffer(v.displayBuf.history, DisplayBufferConfig{
		Width:       v.width,
		Height:      v.height,
		MarginAbove: DefaultMarginAbove,
		MarginBelow: DefaultMarginBelow,
	})

	// Scroll to live edge
	v.displayBuf.display.ScrollToBottom()
}

// displayBufferLoadFromPhysical loads physical lines (old format) into the display buffer.
// Converts them to logical lines using the Wrapped flag.
func (v *VTerm) displayBufferLoadFromPhysical(physical [][]Cell) {
	logical := ConvertPhysicalToLogical(physical)
	v.displayBufferLoadHistory(logical)
}

// DisplayBufferGetHistory returns the ScrollbackHistory for persistence.
// Returns nil if display buffer is not enabled.
func (v *VTerm) DisplayBufferGetHistory() *ScrollbackHistory {
	if v.displayBuf == nil {
		return nil
	}
	return v.displayBuf.history
}

// displayBufferEraseToEndOfLine truncates the current logical line at the current position.
// Used for EL 0 (Erase from cursor to end of line).
func (v *VTerm) displayBufferEraseToEndOfLine() {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}

	// IMPORTANT: Sync cursor position before erasing.
	// The VTerm cursor may have moved (e.g., via backspace) without updating
	// the display buffer. We must sync to ensure erase happens at the right position.
	// Use absolute mapping (false) since we don't know how cursor got here.
	v.displayBufferSetCursorFromPhysical(false)

	// Set erase color to current background (terminal standard behavior)
	v.displayBuf.display.SetEraseColor(v.currentBG)

	if v.displayBuf.display.debugLog != nil {
		lineContent := ""
		line := v.displayBuf.display.CurrentLine()
		for i := 0; i < line.Len() && i < 20; i++ {
			if line.Cells[i].Rune != 0 {
				lineContent += string(line.Cells[i].Rune)
			} else {
				lineContent += "."
			}
		}
		if line.Len() > 20 {
			lineContent += "..."
		}
		v.displayBuf.display.debugLog("displayBufferEraseToEndOfLine: vtermCursor=(%d,%d), offset=%d, line len=%d, content=%q",
			v.cursorX, v.cursorY, v.displayBuf.display.GetCursorOffset(), line.Len(), lineContent)
	}
	v.displayBuf.display.Erase(0)
	if v.displayBuf.display.debugLog != nil {
		v.displayBuf.display.debugLog("displayBufferEraseToEndOfLine: after erase, line len=%d",
			v.displayBuf.display.CurrentLine().Len())
	}
	v.MarkAllDirty()
}

// withDisplayBufferOp runs an operation on the display buffer with standard nil checks
// and dirty marking. If syncCursor is true, syncs cursor position first.
// This eliminates repetition across display buffer edit operations.
func (v *VTerm) withDisplayBufferOp(syncCursor bool, op func(*DisplayBuffer)) {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}
	if syncCursor {
		v.displayBufferSetCursorFromPhysical(false)
	}
	op(v.displayBuf.display)
	v.MarkAllDirty()
}

// displayBufferEraseFromStartOfLine clears the current logical line from start to cursor.
// Used for EL 1 (Erase from start of line to cursor).
func (v *VTerm) displayBufferEraseFromStartOfLine() {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}
	v.displayBuf.display.SetEraseColor(v.currentBG)
	v.displayBuf.display.Erase(1)
	v.MarkAllDirty()
}

// displayBufferEraseLine clears the entire current logical line.
// Used for EL 2 (Erase entire line).
func (v *VTerm) displayBufferEraseLine() {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}
	v.displayBuf.display.SetEraseColor(v.currentBG)
	if v.displayBuf.display.debugLog != nil {
		v.displayBuf.display.debugLog("displayBufferEraseLine (EL 2): CLEARING entire line, was len=%d",
			v.displayBuf.display.CurrentLine().Len())
	}
	v.displayBuf.display.Erase(2)
	v.MarkAllDirty()
}

// displayBufferEraseCharacters replaces n characters at current position with spaces.
// Used for ECH (Erase Character).
func (v *VTerm) displayBufferEraseCharacters(n int) {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}
	v.displayBufferSetCursorFromPhysical(false)
	v.displayBuf.display.SetEraseColor(v.currentBG)
	v.displayBuf.display.EraseCharacters(n)
	v.MarkAllDirty()
}

// displayBufferDeleteCharacters removes n characters at current position, shifting content left.
// Used for DCH (Delete Character) - CSI P.
func (v *VTerm) displayBufferDeleteCharacters(n int) {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}
	v.displayBufferSetCursorFromPhysical(false)
	v.displayBuf.display.SetEraseColor(v.currentBG)
	v.displayBuf.display.DeleteCharacters(n)
	v.MarkAllDirty()
}

// displayBufferInsertCharacters inserts n blank characters at current position, shifting content right.
// Used for ICH (Insert Character) - CSI @.
// TODO: Currently works for non-wrapped lines only. Wrapped lines need special handling
// to reflow content across physical row boundaries after insertion.
func (v *VTerm) displayBufferInsertCharacters(n int) {
	fg, bg := v.currentFG, v.currentBG // Capture for closure
	v.withDisplayBufferOp(true, func(db *DisplayBuffer) {
		db.InsertCharacters(n, fg, bg)
	})
}

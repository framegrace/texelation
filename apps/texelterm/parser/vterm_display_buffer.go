// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_display_buffer.go
// Summary: Display buffer integration for VTerm - enables proper scrollback reflow.
// Usage: Provides alternate Grid/scroll/resize paths using the display buffer architecture.

package parser

import (
	"fmt"
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

	// currentLogicalX tracks cursor position within the current logical line
	// This is separate from cursorX which is the physical display position
	currentLogicalX int
}

// DisplayBufferMemoryLines is the number of logical lines to keep in the
// DisplayBuffer's in-memory history. More lines can be loaded on-demand
// from the HistoryManager when scrolling.
const DisplayBufferMemoryLines = 5000

// initDisplayBuffer initializes the display buffer system for VTerm.
// Called from NewVTerm when the feature is enabled.
func (v *VTerm) initDisplayBuffer() {
	v.displayBuf = &displayBufferState{
		history: NewScrollbackHistory(DisplayBufferMemoryLines),
		enabled: false, // Start disabled, enable explicitly
	}
	v.displayBuf.display = NewDisplayBuffer(v.displayBuf.history, DisplayBufferConfig{
		Width:       v.width,
		Height:      v.height,
		MarginAbove: 200,
		MarginBelow: 50,
	})
}

// EnableDisplayBuffer switches to the new display buffer rendering path.
func (v *VTerm) EnableDisplayBuffer() {
	if v.displayBuf == nil {
		v.initDisplayBuffer()
	}
	v.displayBuf.enabled = true

	// If historyManager already has content (loaded from disk), import it
	if v.historyManager != nil {
		hmLen := v.historyManager.Length()
		fmt.Fprintf(os.Stderr, "[DISPLAY_BUFFER] EnableDisplayBuffer: historyManager.Length()=%d\n", hmLen)
		if hmLen > 0 {
			v.loadHistoryManagerIntoDisplayBuffer()
			fmt.Fprintf(os.Stderr, "[DISPLAY_BUFFER] After load: displayBuf.history.Len()=%d\n", v.displayBuf.history.Len())
		}
	} else {
		fmt.Fprintf(os.Stderr, "[DISPLAY_BUFFER] EnableDisplayBuffer: historyManager is nil\n")
	}
}

// loadHistoryManagerIntoDisplayBuffer converts physical lines from the legacy
// historyManager and loads them into the display buffer's logical line storage.
// Only loads the most recent lines to fit in DisplayBufferMemoryLines; older
// lines are loaded on-demand when scrolling via the HistoryLoader.
func (v *VTerm) loadHistoryManagerIntoDisplayBuffer() {
	if v.historyManager == nil || v.displayBuf == nil {
		return
	}

	totalLines := v.historyManager.Length()
	if totalLines == 0 {
		return
	}

	// Only load the most recent lines that fit in our memory window
	// The rest will be loaded on-demand via the loader
	linesToLoad := min(totalLines, DisplayBufferMemoryLines)
	startIdx := totalLines - linesToLoad

	// Extract physical lines from history manager (most recent portion)
	physical := make([][]Cell, linesToLoad)
	for i := 0; i < linesToLoad; i++ {
		physical[i] = v.historyManager.GetLine(startIdx + i)
	}

	// Convert physical lines to logical lines and load into display buffer
	logical := ConvertPhysicalToLogical(physical)
	for _, line := range logical {
		v.displayBuf.history.Append(line)
	}

	// Rebuild the display buffer with loaded history
	v.displayBuf.display = NewDisplayBuffer(v.displayBuf.history, DisplayBufferConfig{
		Width:       v.width,
		Height:      v.height,
		MarginAbove: 200,
		MarginBelow: 50,
	})

	// Set up the loader for on-demand loading of older history
	// The loader knows that 'linesToLoad' physical lines have been loaded
	loader := NewHistoryManagerLoader(v.historyManager, linesToLoad)
	v.displayBuf.display.SetLoader(loader)

	// Scroll to live edge
	v.displayBuf.display.ScrollToBottom()

	// Position cursor at bottom of viewport where new shell output will appear
	v.cursorY = v.height - 1
	v.cursorX = 0
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

// displayBufferGrid returns the viewport using the display buffer system.
func (v *VTerm) displayBufferGrid() [][]Cell {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return nil
	}
	return v.displayBuf.display.GetViewportAsCells()
}

// displayBufferPlaceChar writes a character using the display buffer system.
// This performs a dual-write: to the current logical line AND the display buffer.
func (v *VTerm) displayBufferPlaceChar(r rune) {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}

	db := v.displayBuf.display
	cell := Cell{Rune: r, FG: v.currentFG, BG: v.currentBG, Attr: v.currentAttr}

	// Write to logical line at currentLogicalX
	db.SetCell(v.displayBuf.currentLogicalX, cell)

	// Advance logical position
	v.displayBuf.currentLogicalX++
}

// displayBufferLineFeed commits the current line and starts a new one.
func (v *VTerm) displayBufferLineFeed() {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}

	// Commit current logical line to history
	v.displayBuf.display.CommitCurrentLine()

	// Reset logical X position for new line
	v.displayBuf.currentLogicalX = 0
}

// displayBufferCarriageReturn handles CR - resets logical X without committing.
func (v *VTerm) displayBufferCarriageReturn() {
	if v.displayBuf == nil {
		return
	}
	v.displayBuf.currentLogicalX = 0
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

// displayBufferResize handles terminal resize with proper reflow.
func (v *VTerm) displayBufferResize(width, height int) {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}

	wasAtLiveEdge := v.displayBuf.display.AtLiveEdge()

	v.displayBuf.display.Resize(width, height)

	// When at live edge, cursor should be at the bottom of the viewport
	if wasAtLiveEdge && v.displayBuf.display.AtLiveEdge() {
		v.cursorY = height - 1
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
// When display buffer is disabled, checks the legacy viewOffset.
func (v *VTerm) AtLiveEdge() bool {
	if v.IsDisplayBufferEnabled() {
		return v.displayBufferAtLiveEdge()
	}
	return v.viewOffset == 0
}

// ScrollToLiveEdge scrolls the viewport to the live edge (bottom of output).
func (v *VTerm) ScrollToLiveEdge() {
	if v.IsDisplayBufferEnabled() {
		v.displayBufferScrollToBottom()
	} else {
		v.viewOffset = 0
	}
	v.MarkAllDirty()
}

// displayBufferSetCursorFromPhysical syncs the logical cursor position
// based on the physical cursor position. Used when cursor moves via escape sequences.
func (v *VTerm) displayBufferSetCursorFromPhysical() {
	if v.displayBuf == nil {
		return
	}
	// For now, assume physical X maps directly to logical X
	// This works for simple cases; cursor movement within wrapped lines is more complex
	v.displayBuf.currentLogicalX = v.cursorX
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
		MarginAbove: 200,
		MarginBelow: 50,
	})
	v.displayBuf.currentLogicalX = 0
}

// displayBufferBackspace handles backspace - moves logical X back.
func (v *VTerm) displayBufferBackspace() {
	if v.displayBuf == nil {
		return
	}
	if v.displayBuf.currentLogicalX > 0 {
		v.displayBuf.currentLogicalX--
	}
}

// displayBufferGetCurrentLine returns the current (uncommitted) logical line.
func (v *VTerm) displayBufferGetCurrentLine() *LogicalLine {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return nil
	}
	return v.displayBuf.display.CurrentLine()
}

// displayBufferHistoryLen returns the number of committed logical lines.
func (v *VTerm) displayBufferHistoryLen() int {
	if v.displayBuf == nil || v.displayBuf.history == nil {
		return 0
	}
	return v.displayBuf.history.Len()
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
		MarginAbove: 200,
		MarginBelow: 50,
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

	currentLine := v.displayBuf.display.CurrentLine()
	if currentLine != nil {
		currentLine.Truncate(v.displayBuf.currentLogicalX)
	}
}

// displayBufferEraseFromStartOfLine clears the current logical line from start to cursor.
// Used for EL 1 (Erase from start of line to cursor).
func (v *VTerm) displayBufferEraseFromStartOfLine() {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}

	currentLine := v.displayBuf.display.CurrentLine()
	if currentLine != nil {
		// Fill from 0 to currentLogicalX with spaces
		for i := 0; i <= v.displayBuf.currentLogicalX && i < currentLine.Len(); i++ {
			currentLine.Cells[i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}
	}
}

// displayBufferEraseLine clears the entire current logical line.
// Used for EL 2 (Erase entire line).
func (v *VTerm) displayBufferEraseLine() {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}

	currentLine := v.displayBuf.display.CurrentLine()
	if currentLine != nil {
		currentLine.Clear()
	}
	v.displayBuf.currentLogicalX = 0
}

// displayBufferEraseCharacters replaces n characters at current position with spaces.
// Used for ECH (Erase Character).
func (v *VTerm) displayBufferEraseCharacters(n int) {
	if v.displayBuf == nil || v.displayBuf.display == nil {
		return
	}

	currentLine := v.displayBuf.display.CurrentLine()
	if currentLine != nil {
		for i := 0; i < n; i++ {
			pos := v.displayBuf.currentLogicalX + i
			if pos < currentLine.Len() {
				currentLine.Cells[pos] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}
		}
	}
}

// SyncDisplayBufferToHistoryManager converts the display buffer's logical lines
// back to physical lines and updates the history manager's buffer.
// This should be called before closing the history manager to persist changes.
func (v *VTerm) SyncDisplayBufferToHistoryManager() {
	if !v.IsDisplayBufferEnabled() || v.historyManager == nil || v.displayBuf == nil {
		return
	}

	history := v.displayBuf.history
	if history == nil || history.Len() == 0 {
		return
	}

	// Convert logical lines to physical lines at current width
	// Include the current (uncommitted) line if it has content
	var physical [][]Cell

	for i := 0; i < history.Len(); i++ {
		line := history.Get(i)
		if line == nil {
			continue
		}
		wrapped := line.WrapToWidth(v.width)
		for j, pl := range wrapped {
			// Set Wrapped flag on all but the last physical line of each logical line
			row := make([]Cell, len(pl.Cells))
			copy(row, pl.Cells)
			if j < len(wrapped)-1 && len(row) > 0 {
				// Mark as wrapped (continuation line)
				row[len(row)-1].Wrapped = true
			}
			physical = append(physical, row)
		}
	}

	// Also include the current line if it has content
	currentLine := v.displayBuf.display.CurrentLine()
	if currentLine != nil && currentLine.Len() > 0 {
		wrapped := currentLine.WrapToWidth(v.width)
		for j, pl := range wrapped {
			row := make([]Cell, len(pl.Cells))
			copy(row, pl.Cells)
			if j < len(wrapped)-1 && len(row) > 0 {
				row[len(row)-1].Wrapped = true
			}
			physical = append(physical, row)
		}
	}

	// Replace the history manager's buffer with these physical lines
	if len(physical) > 0 {
		v.historyManager.ReplaceBuffer(physical)
	}
}

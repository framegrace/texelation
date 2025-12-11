// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_display_buffer.go
// Summary: Display buffer integration for VTerm - enables proper scrollback reflow.
// Usage: Provides alternate Grid/scroll/resize paths using the display buffer architecture.

package parser

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

// initDisplayBuffer initializes the display buffer system for VTerm.
// Called from NewVTerm when the feature is enabled.
func (v *VTerm) initDisplayBuffer() {
	v.displayBuf = &displayBufferState{
		history: NewScrollbackHistory(10000), // 10k logical lines
		enabled: false,                       // Start disabled, enable explicitly
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

	v.displayBuf.display.Resize(width, height)
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

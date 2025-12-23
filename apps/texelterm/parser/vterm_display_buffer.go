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
		MaxMemoryLines: 5000,
		MarginAbove:    200,
		MarginBelow:    50,
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
		opts.MaxMemoryLines = 5000
	}
	if opts.MarginAbove <= 0 {
		opts.MarginAbove = 200
	}
	if opts.MarginBelow <= 0 {
		opts.MarginBelow = 50
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
			fmt.Fprintf(os.Stderr, "[DISPLAY_BUFFER] Failed to create disk-backed history: %v, falling back to memory-only\n", err)
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

	// If historyManager already has content (loaded from disk), import it
	if v.historyManager != nil && v.historyManager.Length() > 0 {
		v.loadHistoryManagerIntoDisplayBuffer()
	}

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

// loadHistoryManagerIntoDisplayBuffer converts physical lines from the legacy
// historyManager and loads them into the display buffer's logical line storage.
func (v *VTerm) loadHistoryManagerIntoDisplayBuffer() {
	if v.historyManager == nil || v.displayBuf == nil {
		return
	}

	// Extract all physical lines from history manager
	length := v.historyManager.Length()
	physical := make([][]Cell, length)
	for i := 0; i < length; i++ {
		physical[i] = v.historyManager.GetLine(i)
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

        // Use the new logical editor
        v.displayBuf.display.Write(r, v.currentFG, v.currentBG, v.currentAttr, v.insertMode)
        
        // Mark all dirty?
        // Ideally we'd get a dirty range. For now, mark all.
        v.MarkAllDirty()
}
// displayBufferLineFeed commits the current line and starts a new one.
func (v *VTerm) displayBufferLineFeed() {
        if v.displayBuf == nil || v.displayBuf.display == nil {
                return
        }

        // Commit current logical line to history
        v.displayBuf.display.CommitCurrentLine()
}
// displayBufferCarriageReturn handles CR - syncs logical position with physical position (start of line).
func (v *VTerm) displayBufferCarriageReturn() {
        // vterm.go has already set v.cursorX = 0 before calling this.
        // We just need to sync the logical cursor.
        v.displayBufferSetCursorFromPhysical()
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
        if v.displayBuf == nil || v.displayBuf.display == nil {
                return
        }
        
        // Use the new logical mapping
        v.displayBuf.display.SetCursor(v.cursorX, v.cursorY)
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
		}
// displayBufferBackspace is deprecated. Cursor synchronization is handled by SetCursorPos.
func (v *VTerm) displayBufferBackspace() {
        // No-op
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
        v.displayBuf.display.Erase(0)
        v.MarkAllDirty()
}

// displayBufferEraseFromStartOfLine clears the current logical line from start to cursor.
// Used for EL 1 (Erase from start of line to cursor).
func (v *VTerm) displayBufferEraseFromStartOfLine() {
        if v.displayBuf == nil || v.displayBuf.display == nil {
                return
        }
        v.displayBuf.display.Erase(1)
        v.MarkAllDirty()
}

// displayBufferEraseLine clears the entire current logical line.
// Used for EL 2 (Erase entire line).
func (v *VTerm) displayBufferEraseLine() {
        if v.displayBuf == nil || v.displayBuf.display == nil {
                return
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
        v.displayBuf.display.EraseCharacters(n)
        v.MarkAllDirty()
}

// displayBufferDeleteCharacters removes n characters at current position, shifting content left.
// Used for DCH (Delete Character) - CSI P.
func (v *VTerm) displayBufferDeleteCharacters(n int) {
        if v.displayBuf == nil || v.displayBuf.display == nil {
                return
        }
        v.displayBuf.display.DeleteCharacters(n)
        v.MarkAllDirty()
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

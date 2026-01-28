// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_memory_buffer.go
// Summary: VTerm integration with the new MemoryBuffer/ViewportWindow/AdaptivePersistence system.
//
// Architecture:
//
//	This file integrates VTerm with the Phase 1-3 components:
//	  - MemoryBuffer: Central storage for terminal content (Phase 1)
//	  - ViewportWindow: Pure view layer for rendering (Phase 3)
//	  - AdaptivePersistence: Dynamic disk persistence (Phase 2)
//
//	The integration follows these principles:
//	  1. MemoryBuffer is the single source of truth for content
//	  2. VTerm writes to MemoryBuffer via this bridge
//	  3. ViewportWindow reads from MemoryBuffer for Grid()
//	  4. AdaptivePersistence is notified of writes for disk persistence
//
//	Key differences from the old DisplayBuffer system:
//	  - ViewportWindow is pure read-only (no cursor, no writes)
//	  - Cursor is tracked in MemoryBuffer, not in the view layer
//	  - Persistence is separate from storage

package parser

import (
	"fmt"
	"log"
	"os"
)

// memoryBufferState holds the new memory buffer system state.
// This replaces displayBufferState when the new system is enabled.
type memoryBufferState struct {
	// memBuf is the central content storage
	memBuf *MemoryBuffer

	// viewport is the read-only view layer (handles scrolling for viewing)
	viewport *ViewportWindow

	// persistence handles disk writes (optional)
	persistence *AdaptivePersistence

	// diskHistory is needed for persistence (optional)
	diskHistory *DiskHistory

	// fixedDetector detects TUI patterns and flags lines as fixed-width (Phase 5)
	fixedDetector *FixedWidthDetector

	// enabled tracks if the new system is active
	enabled bool

	// liveEdgeBase is the global line index at the top of the "live" viewport.
	// This is where cursor Y=0 maps to during writes.
	// It advances as content scrolls (line feeds at screen bottom).
	// NOTE: This is separate from viewport scroll position (for viewing).
	// When user scrolls back to view history, liveEdgeBase doesn't change -
	// cursor writes still go to the live edge.
	liveEdgeBase int64
}

// MemoryBufferOptions configures the new memory buffer system.
type MemoryBufferOptions struct {
	// MaxLines is the maximum logical lines to keep in memory (default 50000)
	MaxLines int

	// EvictionBatch is how many lines to evict at once (default 1000)
	EvictionBatch int

	// DiskPath enables disk persistence if non-empty
	DiskPath string
}

// DefaultMemoryBufferOptions returns sensible defaults.
func DefaultMemoryBufferOptions() MemoryBufferOptions {
	return MemoryBufferOptions{
		MaxLines:      50000,
		EvictionBatch: 1000,
		DiskPath:      "",
	}
}

// --- Initialization ---

// initMemoryBuffer initializes the new memory buffer system for VTerm.
// This replaces initDisplayBuffer when the new system is used.
func (v *VTerm) initMemoryBuffer() {
	v.initMemoryBufferWithOptions(DefaultMemoryBufferOptions())
}

// initMemoryBufferWithOptions initializes with custom options.
func (v *VTerm) initMemoryBufferWithOptions(opts MemoryBufferOptions) {
	if opts.MaxLines <= 0 {
		opts.MaxLines = 50000
	}
	if opts.EvictionBatch <= 0 {
		opts.EvictionBatch = 1000
	}

	// Create memory buffer
	mbConfig := MemoryBufferConfig{
		MaxLines:      opts.MaxLines,
		EvictionBatch: opts.EvictionBatch,
	}
	memBuf := NewMemoryBuffer(mbConfig)
	memBuf.SetTermWidth(v.width)

	// Create viewport window
	viewport := NewViewportWindow(memBuf, v.width, v.height)

	// Create fixed-width detector (Phase 5)
	fixedDetector := NewFixedWidthDetector(memBuf)
	fixedDetector.OnResize(v.width, v.height)

	// Create memory buffer state
	v.memBufState = &memoryBufferState{
		memBuf:        memBuf,
		viewport:      viewport,
		fixedDetector: fixedDetector,
		enabled:       false,
		liveEdgeBase:  0,
	}

	// Set up disk persistence if path provided
	if opts.DiskPath != "" {
		diskConfig := DefaultDiskHistoryConfig(opts.DiskPath)
		disk, err := CreateDiskHistory(diskConfig)
		if err != nil {
			log.Printf("[MEMORY_BUFFER] Failed to create disk history: %v, running without persistence", err)
		} else {
			v.memBufState.diskHistory = disk

			// Create adaptive persistence
			apConfig := DefaultAdaptivePersistenceConfig()
			persistence, err := NewAdaptivePersistence(apConfig, memBuf, disk)
			if err != nil {
				log.Printf("[MEMORY_BUFFER] Failed to create adaptive persistence: %v", err)
				disk.Close()
			} else {
				v.memBufState.persistence = persistence
			}
		}
	}
}

// EnableMemoryBuffer switches to the new memory buffer system.
func (v *VTerm) EnableMemoryBuffer() {
	if v.memBufState == nil {
		v.initMemoryBuffer()
	}
	v.memBufState.enabled = true

	// Initialize the first line
	v.memBufState.memBuf.EnsureLine(0)
	v.memBufState.liveEdgeBase = 0
}

// EnableMemoryBufferWithDisk enables with disk-backed persistence.
func (v *VTerm) EnableMemoryBufferWithDisk(diskPath string, opts MemoryBufferOptions) error {
	opts.DiskPath = diskPath
	v.initMemoryBufferWithOptions(opts)
	v.memBufState.enabled = true

	// Initialize the first line
	v.memBufState.memBuf.EnsureLine(0)
	v.memBufState.liveEdgeBase = 0

	return nil
}

// CloseMemoryBuffer closes the memory buffer system and flushes to disk.
func (v *VTerm) CloseMemoryBuffer() error {
	if v.memBufState == nil {
		return nil
	}

	var firstErr error

	// Close persistence first (flushes pending writes)
	if v.memBufState.persistence != nil {
		if err := v.memBufState.persistence.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Close disk history
	if v.memBufState.diskHistory != nil {
		if err := v.memBufState.diskHistory.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// IsMemoryBufferEnabled returns whether the new system is active.
func (v *VTerm) IsMemoryBufferEnabled() bool {
	return v.memBufState != nil && v.memBufState.enabled
}

// --- Writing Operations ---

// ensureLiveEdgeBaseConsistency ensures liveEdgeBase >= GlobalOffset.
// This prevents issues if lines were evicted and liveEdgeBase points to non-existent lines.
func (v *VTerm) ensureLiveEdgeBaseConsistency() {
	if v.memBufState == nil || v.memBufState.memBuf == nil {
		return
	}
	mb := v.memBufState.memBuf
	globalOffset := mb.GlobalOffset()
	if v.memBufState.liveEdgeBase < globalOffset {
		v.logMemBufDebug("[CONSISTENCY] liveEdgeBase %d < GlobalOffset %d, adjusting to %d",
			v.memBufState.liveEdgeBase, globalOffset, globalOffset)
		v.memBufState.liveEdgeBase = globalOffset
	}
}

// memoryBufferPlaceChar writes a character at the current cursor position.
func (v *VTerm) memoryBufferPlaceChar(r rune) {
	v.memoryBufferPlaceCharWide(r, false)
}

// memoryBufferPlaceCharWide writes a character with wide character support.
func (v *VTerm) memoryBufferPlaceCharWide(r rune, isWide bool) {
	if !v.IsMemoryBufferEnabled() {
		return
	}

	// Ensure liveEdgeBase is consistent with GlobalOffset
	v.ensureLiveEdgeBaseConsistency()

	mb := v.memBufState.memBuf

	// Calculate the global line index from viewport Y
	globalLine := v.memBufState.liveEdgeBase + int64(v.cursorY)

	// Ensure the line exists
	mb.EnsureLine(globalLine)

	// Set cursor to correct position
	mb.SetCursor(globalLine, v.cursorX)

	// Write the character
	if isWide {
		mb.WriteWide(r, v.currentFG, v.currentBG, v.currentAttr, true)
	} else {
		mb.Write(r, v.currentFG, v.currentBG, v.currentAttr)
	}

	// Notify persistence layer
	if v.memBufState.persistence != nil {
		v.memBufState.persistence.NotifyWrite(globalLine)
	}

	// Notify fixed-width detector (Phase 5)
	if v.memBufState.fixedDetector != nil {
		v.memBufState.fixedDetector.OnWrite(globalLine, mb.TermWidth())
	}

	// Debug logging
	if os.Getenv("TEXELTERM_DEBUG") != "" {
		v.logMemBufDebug("placeCharWide: '%c' at global line %d, col %d, wide=%v",
			r, globalLine, v.cursorX, isWide)
	}
}

// memoryBufferLineFeed handles line feed - commits current line and starts new one.
func (v *VTerm) memoryBufferLineFeed() {
	if !v.IsMemoryBufferEnabled() {
		return
	}

	// Only commit when at full-screen margins (normal shell operation)
	// TUI apps with custom scroll regions don't commit lines
	isFullScreenMargins := v.marginTop == 0 && v.marginBottom == v.height-1
	if !isFullScreenMargins {
		return
	}

	// Calculate current global line
	currentGlobal := v.memBufState.liveEdgeBase + int64(v.cursorY)

	// When cursor moves to next row, we may need to advance liveEdgeBase
	// if we're at the bottom of the viewport
	if v.cursorY >= v.marginBottom {
		v.memBufState.liveEdgeBase++
	}

	// Ensure the next line exists
	nextGlobal := v.memBufState.liveEdgeBase + int64(v.cursorY)
	if v.cursorY < v.marginBottom {
		nextGlobal = currentGlobal + 1
	}
	v.memBufState.memBuf.EnsureLine(nextGlobal)

	// Invalidate viewport cache since content shifted
	v.memBufState.viewport.InvalidateCache()

	// Mark as dirty for persistence
	if v.memBufState.persistence != nil {
		v.memBufState.persistence.NotifyWrite(currentGlobal)
	}
}

// memoryBufferCarriageReturn handles carriage return.
func (v *VTerm) memoryBufferCarriageReturn() {
	if !v.IsMemoryBufferEnabled() {
		return
	}

	// Calculate global line and set cursor
	globalLine := v.memBufState.liveEdgeBase + int64(v.cursorY)
	v.memBufState.memBuf.SetCursor(globalLine, 0)
}

// --- Grid Access ---

// memoryBufferGrid returns the viewport grid from ViewportWindow.
func (v *VTerm) memoryBufferGrid() [][]Cell {
	if !v.IsMemoryBufferEnabled() {
		return nil
	}

	return v.memBufState.viewport.GetVisibleGrid()
}

// --- Scrolling ---

// memoryBufferScroll handles user scrollback navigation.
// Positive delta = scroll down (toward live edge)
// Negative delta = scroll up (into history)
func (v *VTerm) memoryBufferScroll(delta int) {
	if !v.IsMemoryBufferEnabled() {
		return
	}

	mb := v.memBufState.memBuf
	vw := v.memBufState.viewport
	beforeOffset := vw.ScrollOffset()
	totalPhysical := vw.TotalPhysicalLines()
	viewportHeight := vw.Height()
	maxScrollOffset := totalPhysical - int64(viewportHeight)
	if maxScrollOffset < 0 {
		maxScrollOffset = 0
	}

	if delta > 0 {
		v.memBufState.viewport.ScrollDown(delta)
	} else if delta < 0 {
		v.memBufState.viewport.ScrollUp(-delta)
	}

	afterOffset := vw.ScrollOffset()
	v.logMemBufDebug("[SCROLL] delta=%d: offset %d -> %d (maxScroll=%d, totalPhysical=%d, viewportHeight=%d, globalOffset=%d, globalEnd=%d, liveEdgeBase=%d)",
		delta, beforeOffset, afterOffset, maxScrollOffset, totalPhysical, viewportHeight, mb.GlobalOffset(), mb.GlobalEnd(), v.memBufState.liveEdgeBase)
}

// memoryBufferScrollToBottom scrolls to the live edge.
func (v *VTerm) memoryBufferScrollToBottom() {
	if !v.IsMemoryBufferEnabled() {
		return
	}

	v.memBufState.viewport.ScrollToBottom()
}

// ScrollToLiveEdge scrolls the viewport to show the most recent content.
func (v *VTerm) ScrollToLiveEdge() {
	v.memoryBufferScrollToBottom()
	v.MarkAllDirty()
}

// EnsureLiveEdge scrolls to live edge if not already there.
// Used when user performs an action (typing, pasting) to ensure they see the result.
func (v *VTerm) EnsureLiveEdge() {
	if !v.AtLiveEdge() {
		v.ScrollToLiveEdge()
	}
}

// AtLiveEdge returns whether the viewport is at the live edge (showing most recent content).
func (v *VTerm) AtLiveEdge() bool {
	return v.memoryBufferAtLiveEdge()
}

// ScrollOffset returns the current scroll offset from the live edge.
// 0 means at live edge, positive means scrolled back into history.
// This delegates to ViewportWindow - scroll position is for VIEWING only,
// not for cursor/write operations.
func (v *VTerm) ScrollOffset() int64 {
	if !v.IsMemoryBufferEnabled() {
		return 0
	}
	return v.memBufState.viewport.ScrollOffset()
}

// SetScrollOffset sets the scroll offset from the live edge.
// 0 means at live edge, positive means scrolled back into history.
// This only affects VIEWING (what Grid() returns), not cursor writes.
// Cursor always writes at the live edge regardless of scroll position.
func (v *VTerm) SetScrollOffset(offset int64) {
	if !v.IsMemoryBufferEnabled() {
		return
	}
	if offset <= 0 {
		v.memBufState.viewport.ScrollToBottom()
	} else {
		v.memBufState.viewport.ScrollToOffset(offset)
	}
	v.MarkAllDirty()
}

// LastPromptLine returns the line index of the last shell prompt.
// This is a stub - full implementation would track OSC 133 markers.
func (v *VTerm) LastPromptLine() int64 {
	// TODO: Track actual prompt lines via OSC 133 markers
	return -1
}

// LastPromptHeight returns the height of the last prompt in lines.
// This is a stub - full implementation would track OSC 133 markers.
func (v *VTerm) LastPromptHeight() int {
	// TODO: Track actual prompt height via OSC 133 markers
	return 1
}

// CurrentLineCells returns the cells of the current cursor line.
func (v *VTerm) CurrentLineCells() []Cell {
	if v.inAltScreen {
		if v.cursorY >= 0 && v.cursorY < len(v.altBuffer) {
			return v.altBuffer[v.cursorY]
		}
		return nil
	}
	if !v.IsMemoryBufferEnabled() {
		return nil
	}
	globalLine := v.memBufState.liveEdgeBase + int64(v.cursorY)
	line := v.memBufState.memBuf.GetLine(globalLine)
	if line == nil {
		return nil
	}
	return line.Cells
}

// memoryBufferAtLiveEdge returns whether the viewport is at the live edge.
func (v *VTerm) memoryBufferAtLiveEdge() bool {
	if !v.IsMemoryBufferEnabled() {
		return true
	}

	return v.memBufState.viewport.IsAtLiveEdge()
}

// --- Resize ---

// memoryBufferResize handles terminal resize.
func (v *VTerm) memoryBufferResize(width, height int) {
	if !v.IsMemoryBufferEnabled() {
		return
	}

	oldHeight := v.memBufState.viewport.Height()
	mb := v.memBufState.memBuf

	// Calculate cursor's global line before resize
	cursorGlobalLine := v.memBufState.liveEdgeBase + int64(v.cursorY)

	v.logMemBufDebug("[RESIZE] Before: width=%d->%d, height=%d->%d, liveEdgeBase=%d, cursorY=%d, cursorGlobal=%d, GlobalEnd=%d",
		v.memBufState.viewport.Width(), width, oldHeight, height,
		v.memBufState.liveEdgeBase, v.cursorY, cursorGlobalLine, mb.GlobalEnd())

	// Update viewport dimensions
	mb.SetTermWidth(width)
	v.memBufState.viewport.Resize(width, height)

	// Adjust liveEdgeBase for vertical resize
	if height != oldHeight {
		globalEnd := mb.GlobalEnd()
		globalOffset := mb.GlobalOffset()

		if height < oldHeight {
			// Shrinking: Adjust liveEdgeBase so cursor stays visible
			// The cursor row must be < height
			if v.cursorY >= height {
				// Cursor would be off screen - adjust liveEdgeBase to keep cursor visible
				// New cursor row will be height-1 (bottom of screen)
				// liveEdgeBase = cursorGlobalLine - (height - 1)
				newLiveEdgeBase := cursorGlobalLine - int64(height-1)
				if newLiveEdgeBase < globalOffset {
					newLiveEdgeBase = globalOffset
				}
				v.memBufState.liveEdgeBase = newLiveEdgeBase
				v.cursorY = int(cursorGlobalLine - newLiveEdgeBase)

				v.logMemBufDebug("[RESIZE] Shrink: cursor off-screen, adjusted liveEdgeBase=%d, cursorY=%d",
					v.memBufState.liveEdgeBase, v.cursorY)
			}
			// If cursor is still on screen after shrink (cursorY < height), no adjustment needed
		} else {
			// Growing: Show more scrollback above if available
			// We want to show more history while keeping the cursor at the same relative position
			// from the bottom of the content.

			// How many more rows do we have?
			heightDelta := height - oldHeight

			// Try to move liveEdgeBase back to show more history
			newLiveEdgeBase := v.memBufState.liveEdgeBase - int64(heightDelta)
			if newLiveEdgeBase < globalOffset {
				newLiveEdgeBase = globalOffset
			}

			// Calculate new cursor Y to point to the same global line
			newCursorY := int(cursorGlobalLine - newLiveEdgeBase)

			// Make sure cursor is within bounds
			if newCursorY >= height {
				newCursorY = height - 1
			}
			if newCursorY < 0 {
				newCursorY = 0
			}

			// But also make sure we're not showing beyond GlobalEnd
			// The viewport should show lines from liveEdgeBase to liveEdgeBase + height - 1
			// This should not exceed GlobalEnd - 1
			maxLiveEdgeBase := globalEnd - int64(height)
			if maxLiveEdgeBase < globalOffset {
				maxLiveEdgeBase = globalOffset
			}
			if newLiveEdgeBase > maxLiveEdgeBase {
				newLiveEdgeBase = maxLiveEdgeBase
			}

			v.memBufState.liveEdgeBase = newLiveEdgeBase
			v.cursorY = int(cursorGlobalLine - newLiveEdgeBase)

			// Re-clamp cursor
			if v.cursorY >= height {
				v.cursorY = height - 1
			}
			if v.cursorY < 0 {
				v.cursorY = 0
			}

			v.logMemBufDebug("[RESIZE] Grow: adjusted liveEdgeBase=%d, cursorY=%d",
				v.memBufState.liveEdgeBase, v.cursorY)
		}
	}

	// Ensure consistency
	v.ensureLiveEdgeBaseConsistency()

	v.logMemBufDebug("[RESIZE] After: liveEdgeBase=%d, cursorY=%d, height=%d",
		v.memBufState.liveEdgeBase, v.cursorY, height)

	// Notify fixed-width detector (Phase 5)
	if v.memBufState.fixedDetector != nil {
		v.memBufState.fixedDetector.OnResize(width, height)
	}
}

// --- Erase Operations ---

// memoryBufferEraseToEndOfLine erases from cursor to end of line.
func (v *VTerm) memoryBufferEraseToEndOfLine() {
	if !v.IsMemoryBufferEnabled() {
		return
	}

	globalLine := v.memBufState.liveEdgeBase + int64(v.cursorY)
	v.memBufState.memBuf.EraseToEndOfLine(globalLine, v.cursorX)

	// Mark dirty for persistence
	if v.memBufState.persistence != nil {
		v.memBufState.persistence.NotifyWrite(globalLine)
	}
}

// memoryBufferEraseFromStartOfLine erases from start of line to cursor.
func (v *VTerm) memoryBufferEraseFromStartOfLine() {
	if !v.IsMemoryBufferEnabled() {
		return
	}

	globalLine := v.memBufState.liveEdgeBase + int64(v.cursorY)
	v.memBufState.memBuf.EraseFromStartOfLine(globalLine, v.cursorX)

	// Mark dirty for persistence
	if v.memBufState.persistence != nil {
		v.memBufState.persistence.NotifyWrite(globalLine)
	}
}

// memoryBufferEraseLine erases the entire current line.
func (v *VTerm) memoryBufferEraseLine() {
	if !v.IsMemoryBufferEnabled() {
		return
	}

	globalLine := v.memBufState.liveEdgeBase + int64(v.cursorY)
	v.memBufState.memBuf.EraseLine(globalLine)

	// Mark dirty for persistence
	if v.memBufState.persistence != nil {
		v.memBufState.persistence.NotifyWrite(globalLine)
	}
}

// memoryBufferEraseCharacters erases n characters at current position.
func (v *VTerm) memoryBufferEraseCharacters(n int) {
	if !v.IsMemoryBufferEnabled() {
		return
	}

	globalLine := v.memBufState.liveEdgeBase + int64(v.cursorY)
	line := v.memBufState.memBuf.GetLine(globalLine)
	if line == nil {
		return
	}

	// Replace n characters at cursor position with spaces
	for i := 0; i < n; i++ {
		col := v.cursorX + i
		if col < len(line.Cells) {
			line.Cells[col] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}
	}

	v.memBufState.memBuf.MarkDirty(globalLine)

	// Mark dirty for persistence
	if v.memBufState.persistence != nil {
		v.memBufState.persistence.NotifyWrite(globalLine)
	}
}

// memoryBufferEraseScreen erases parts of the screen.
func (v *VTerm) memoryBufferEraseScreen(mode int) {
	if !v.IsMemoryBufferEnabled() {
		return
	}

	mb := v.memBufState.memBuf
	v.logMemBufDebug("[ERASE] EraseScreen mode=%d: globalOffset=%d, globalEnd=%d, liveEdgeBase=%d, cursorY=%d",
		mode, mb.GlobalOffset(), mb.GlobalEnd(), v.memBufState.liveEdgeBase, v.cursorY)

	switch mode {
	case 0: // Erase from cursor to end of screen
		// Erase current line from cursor
		v.memoryBufferEraseToEndOfLine()
		// Erase all lines below
		for y := v.cursorY + 1; y < v.height; y++ {
			globalLine := v.memBufState.liveEdgeBase + int64(y)
			v.memBufState.memBuf.EraseLine(globalLine)
			if v.memBufState.persistence != nil {
				v.memBufState.persistence.NotifyWrite(globalLine)
			}
		}

	case 1: // Erase from start of screen to cursor
		// Erase all lines above
		for y := 0; y < v.cursorY; y++ {
			globalLine := v.memBufState.liveEdgeBase + int64(y)
			v.memBufState.memBuf.EraseLine(globalLine)
			if v.memBufState.persistence != nil {
				v.memBufState.persistence.NotifyWrite(globalLine)
			}
		}
		// Erase current line from start to cursor
		v.memoryBufferEraseFromStartOfLine()

	case 2: // Erase entire visible screen
		for y := 0; y < v.height; y++ {
			globalLine := v.memBufState.liveEdgeBase + int64(y)
			v.memBufState.memBuf.EraseLine(globalLine)
			if v.memBufState.persistence != nil {
				v.memBufState.persistence.NotifyWrite(globalLine)
			}
		}
	}
}

// --- Scroll Region Operations ---

// memoryBufferScrollRegion scrolls content within the current scroll region.
// n > 0: scroll up (content moves up, blank lines at bottom)
// n < 0: scroll down (content moves down, blank lines at top)
func (v *VTerm) memoryBufferScrollRegion(n int, top int, bottom int) {
	if !v.IsMemoryBufferEnabled() {
		return
	}

	mb := v.memBufState.memBuf

	if n > 0 {
		// Scroll up: delete top lines, insert blank at bottom
		for i := 0; i < n; i++ {
			// Shift lines up
			for y := top; y < bottom; y++ {
				srcGlobal := v.memBufState.liveEdgeBase + int64(y+1)
				dstGlobal := v.memBufState.liveEdgeBase + int64(y)

				srcLine := mb.GetLine(srcGlobal)
				dstLine := mb.EnsureLine(dstGlobal)

				if srcLine != nil && dstLine != nil {
					// Copy content
					dstLine.Cells = make([]Cell, len(srcLine.Cells))
					copy(dstLine.Cells, srcLine.Cells)
					dstLine.FixedWidth = srcLine.FixedWidth
				}
				mb.MarkDirty(dstGlobal)
			}
			// Clear bottom line
			bottomGlobal := v.memBufState.liveEdgeBase + int64(bottom)
			mb.EraseLine(bottomGlobal)
			mb.MarkDirty(bottomGlobal)
		}
	} else if n < 0 {
		// Scroll down: insert blank at top, delete bottom
		n = -n
		for i := 0; i < n; i++ {
			// Shift lines down
			for y := bottom; y > top; y-- {
				srcGlobal := v.memBufState.liveEdgeBase + int64(y-1)
				dstGlobal := v.memBufState.liveEdgeBase + int64(y)

				srcLine := mb.GetLine(srcGlobal)
				dstLine := mb.EnsureLine(dstGlobal)

				if srcLine != nil && dstLine != nil {
					// Copy content
					dstLine.Cells = make([]Cell, len(srcLine.Cells))
					copy(dstLine.Cells, srcLine.Cells)
					dstLine.FixedWidth = srcLine.FixedWidth
				}
				mb.MarkDirty(dstGlobal)
			}
			// Clear top line
			topGlobal := v.memBufState.liveEdgeBase + int64(top)
			mb.EraseLine(topGlobal)
			mb.MarkDirty(topGlobal)
		}
	}

	// Notify persistence for all affected lines
	if v.memBufState.persistence != nil {
		for y := top; y <= bottom; y++ {
			globalLine := v.memBufState.liveEdgeBase + int64(y)
			v.memBufState.persistence.NotifyWrite(globalLine)
		}
	}
}

// --- Cursor Sync ---

// memoryBufferSetCursorFromPhysical syncs the memory buffer cursor with VTerm cursor.
func (v *VTerm) memoryBufferSetCursorFromPhysical() {
	if !v.IsMemoryBufferEnabled() {
		return
	}

	globalLine := v.memBufState.liveEdgeBase + int64(v.cursorY)
	v.memBufState.memBuf.SetCursor(globalLine, v.cursorX)
}

// --- Coordinate Conversion ---

// memoryBufferViewportToContent converts viewport coordinates to content coordinates.
func (v *VTerm) memoryBufferViewportToContent(y, x int) (globalLine int64, charOffset int, ok bool) {
	if !v.IsMemoryBufferEnabled() {
		return 0, 0, false
	}

	globalLine, charOffset, ok = v.memBufState.viewport.ViewportToContent(y, x)
	return
}

// memoryBufferContentToViewport converts content coordinates to viewport coordinates.
func (v *VTerm) memoryBufferContentToViewport(globalLine int64, charOffset int) (y, x int, visible bool) {
	if !v.IsMemoryBufferEnabled() {
		return 0, 0, false
	}

	y, x, visible = v.memBufState.viewport.ContentToViewport(globalLine, charOffset)
	return
}

// --- Status and Debugging ---

// memoryBufferScrollOffset returns the current scroll offset.
func (v *VTerm) memoryBufferScrollOffset() int64 {
	if !v.IsMemoryBufferEnabled() {
		return 0
	}

	return v.memBufState.viewport.ScrollOffset()
}

// memoryBufferTotalLines returns the total number of lines.
func (v *VTerm) memoryBufferTotalLines() int64 {
	if !v.IsMemoryBufferEnabled() {
		return 0
	}

	return v.memBufState.memBuf.TotalLines()
}

// logMemBufDebug writes to the debug log if TEXELTERM_DEBUG is set.
func (v *VTerm) logMemBufDebug(format string, args ...interface{}) {
	if os.Getenv("TEXELTERM_DEBUG") == "" {
		return
	}
	debugFile, err := os.OpenFile("/tmp/texelterm-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer debugFile.Close()
	fmt.Fprintf(debugFile, "[MEMBUF] "+format+"\n", args...)
}

// --- FixedWidthDetector Access (Phase 5) ---

// fixedWidthDetector returns the FixedWidthDetector, or nil if not available.
// Safe to call even when memory buffer is not enabled.
func (v *VTerm) fixedWidthDetector() *FixedWidthDetector {
	if v.memBufState == nil {
		return nil
	}
	return v.memBufState.fixedDetector
}

// notifyDetectorCursorMove notifies the detector of cursor movement.
// Called from SetCursorPos when cursor moves to a new row.
func (v *VTerm) notifyDetectorCursorMove(newY int) {
	if d := v.fixedWidthDetector(); d != nil {
		prevJumps := d.ConsecutiveJumps()
		d.OnCursorMove(newY)
		if d.ConsecutiveJumps() > prevJumps {
			v.logMemBufDebug("[TUI-DETECT] Cursor jump to Y=%d, consecutive=%d, inTUI=%v",
				newY, d.ConsecutiveJumps(), d.IsInTUIMode())
		}
	}
}

// notifyDetectorScrollRegion notifies the detector of scroll region changes.
// Called from SetMargins.
func (v *VTerm) notifyDetectorScrollRegion(top, bottom, height int) {
	if d := v.fixedWidthDetector(); d != nil {
		d.OnScrollRegionSet(top, bottom, height)
		v.logMemBufDebug("[TUI-DETECT] Scroll region set: top=%d, bottom=%d, height=%d, inTUI=%v",
			top, bottom, height, d.IsInTUIMode())
	}
}

// notifyDetectorScrollRegionClear notifies the detector of scroll region reset.
// Called from SetMargins when resetting to full screen.
func (v *VTerm) notifyDetectorScrollRegionClear() {
	if d := v.fixedWidthDetector(); d != nil {
		wasInTUI := d.IsInTUIMode()
		d.OnScrollRegionClear()
		if wasInTUI {
			v.logMemBufDebug("[TUI-DETECT] Scroll region cleared, exited TUI mode")
		}
	}
}

// notifyDetectorCursorVisibility notifies the detector of cursor visibility changes.
// Called from SetCursorVisible.
func (v *VTerm) notifyDetectorCursorVisibility(hidden bool) {
	if d := v.fixedWidthDetector(); d != nil {
		d.OnCursorVisibilityChange(hidden)
		if hidden {
			v.logMemBufDebug("[TUI-DETECT] Cursor hidden, signals=%d", d.SignalCount())
		}
	}
}

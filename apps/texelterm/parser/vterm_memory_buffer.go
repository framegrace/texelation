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
	"strings"
	"time"
	"unicode"
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

	// pageStore is the page-based disk storage (optional)
	pageStore *PageStore

	// fixedDetector detects TUI patterns and flags lines as fixed-width (Phase 5)
	fixedDetector *FixedWidthDetector

	// enabled tracks if the new system is active
	enabled bool

	// historyLoaded tracks whether startup history has been loaded from disk.
	// Set to true after first Resize() loads history from PageStore.
	// This prevents loading history multiple times.
	historyLoaded bool

	// pendingHistoryLines is the number of historical lines available in PageStore.
	// Set during initialization, used by first Resize() to load history.
	pendingHistoryLines int64

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

	// TerminalID is the unique identifier for this terminal session.
	// If empty, a random ID is generated (history won't persist across sessions).
	// For standalone texelterm, use a fixed ID like "standalone-texelterm".
	TerminalID string
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
		terminalID := opts.TerminalID
		if terminalID == "" {
			terminalID = fmt.Sprintf("term-%d", time.Now().UnixNano())
		}
		log.Printf("[MEMORY_BUFFER] Setting up persistence: dir=%s, terminalID=%s", opts.DiskPath, terminalID)

		apConfig := DefaultAdaptivePersistenceConfig()
		walConfig := DefaultWALConfig(opts.DiskPath, terminalID)
		persistence, err := NewAdaptivePersistenceWithWAL(apConfig, memBuf, walConfig)
		if err != nil {
			log.Printf("[MEMORY_BUFFER] Failed to create persistence: %v, running without history", err)
		} else {
			v.memBufState.persistence = persistence
			v.memBufState.pageStore = persistence.PageStore()
			v.memBufState.viewport.SetPageStore(v.memBufState.pageStore)
			log.Printf("[MEMORY_BUFFER] Persistence enabled, lineCount=%d", v.memBufState.pageStore.LineCount())
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

	// Check PageStore for historical content info (for search index continuity)
	log.Printf("[MEMORY_BUFFER] Checking PageStore: pageStore=%v", v.memBufState.pageStore != nil)
	if v.memBufState.pageStore != nil {
		lineCount := v.memBufState.pageStore.LineCount()
		log.Printf("[MEMORY_BUFFER] PageStore has %d historical lines (searchable)", lineCount)

		// Start new lines AFTER historical content to maintain global index continuity
		// This ensures search results have valid global indices
		if lineCount > 0 {
			v.memBufState.memBuf.SetGlobalOffset(lineCount)
			v.memBufState.liveEdgeBase = lineCount
			v.memBufState.memBuf.EnsureLine(lineCount)

			// Load history now since viewport dimensions are already known (v.height)
			// We can't defer to Resize() because Resize() returns early if dimensions match
			v.memBufState.pendingHistoryLines = lineCount
			v.memBufState.historyLoaded = false
			log.Printf("[MEMORY_BUFFER] %d historical lines available, loading now (height=%d)", lineCount, v.height)
			v.loadHistoryFromDisk(v.height)
			v.memBufState.historyLoaded = true
			return nil
		}
	}

	// No disk content - initialize the first line
	v.memBufState.memBuf.EnsureLine(0)
	v.memBufState.liveEdgeBase = 0
	v.memBufState.historyLoaded = true // Nothing to load

	return nil
}

// CloseMemoryBuffer closes the memory buffer system and flushes to disk.
func (v *VTerm) CloseMemoryBuffer() error {
	if v.memBufState == nil {
		return nil
	}

	var firstErr error

	// Save viewport state through WAL before closing (crash-safe)
	if v.memBufState.persistence != nil && v.memBufState.viewport != nil {
		v.notifyMetadataChange()
	}

	// Close persistence first (flushes pending writes, checkpoints WAL, and closes PageStore)
	if v.memBufState.persistence != nil {
		if err := v.memBufState.persistence.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	} else if v.memBufState.pageStore != nil {
		// Only close PageStore directly if no persistence layer
		if err := v.memBufState.pageStore.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// IsMemoryBufferEnabled returns whether the new system is active.
func (v *VTerm) IsMemoryBufferEnabled() bool {
	return v.memBufState != nil && v.memBufState.enabled
}

// SetOnLineIndexed sets a callback that's invoked AFTER a line is persisted to disk.
// This is used for search indexing - the callback is only called for lines that
// have actually been written to WAL, ensuring the search index never has entries
// for content that doesn't exist on disk.
//
// The callback receives:
//   - lineIdx: global line index
//   - line: the LogicalLine with cell content
//   - timestamp: when the line was written
//   - isCommand: true if this was a shell command (OSC 133)
func (v *VTerm) SetOnLineIndexed(callback func(lineIdx int64, line *LogicalLine, timestamp time.Time, isCommand bool)) {
	if v.memBufState != nil && v.memBufState.persistence != nil {
		v.memBufState.persistence.OnLineIndexed = callback
	}
}

// notifyMetadataChange notifies AdaptivePersistence of metadata changes.
// Metadata is batched with content and written together, ensuring consistency.
// Called on scroll changes and before close.
func (v *VTerm) notifyMetadataChange() {
	if v.memBufState == nil || v.memBufState.persistence == nil || v.memBufState.viewport == nil {
		return
	}

	// Build viewport state with cursor position
	state := &ViewportState{
		ScrollOffset: v.memBufState.viewport.ScrollOffset(),
		LiveEdgeBase: v.memBufState.liveEdgeBase,
		CursorX:      v.cursorX,
		CursorY:      v.cursorY,
		SavedAt:      time.Now(),
	}

	// Notify persistence layer - metadata will be written with content on flush
	v.memBufState.persistence.NotifyMetadataChange(state)
}

// --- History Loading ---

// loadHistoryFromDisk loads a window of historical lines from PageStore into MemoryBuffer.
// Called on first resize when viewport dimensions are known.
// viewportHeight is used to determine how many lines to load.
func (v *VTerm) loadHistoryFromDisk(viewportHeight int) {
	if v.memBufState == nil || v.memBufState.pageStore == nil {
		return
	}

	if viewportHeight <= 0 {
		log.Printf("[MEMORY_BUFFER] Invalid viewportHeight: %d, skipping history load", viewportHeight)
		return
	}

	lineCount := v.memBufState.pendingHistoryLines
	if lineCount == 0 {
		log.Printf("[MEMORY_BUFFER] No history lines to load")
		return
	}

	// Monitor: Log warning for very large histories that might impact performance
	if lineCount > 100000 {
		log.Printf("[MEMORY_BUFFER] WARNING: Large history detected (%d lines). "+
			"Consider increasing memory buffer size or archiving old history.", lineCount)
	}

	// Load a window of history: viewport + margin for smoother scrolling
	// Older content is accessible via PageStore fallback when scrolling
	margin := 500
	windowSize := viewportHeight + margin
	if int64(windowSize) > lineCount {
		windowSize = int(lineCount)
	}

	// Calculate the range to load (last windowSize lines from history)
	startIdx := lineCount - int64(windowSize)
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := lineCount

	log.Printf("[MEMORY_BUFFER] Loading history: range [%d, %d) (%d lines) from %d total",
		startIdx, endIdx, endIdx-startIdx, lineCount)

	// Monitor: Time the history loading for performance tracking
	loadStart := time.Now()

	// Read lines from PageStore
	lines, err := v.memBufState.pageStore.ReadLineRange(startIdx, endIdx)
	if err != nil {
		log.Printf("[MEMORY_BUFFER] Failed to read history from PageStore: %v", err)
		return
	}

	readDuration := time.Since(loadStart)

	if len(lines) == 0 {
		log.Printf("[MEMORY_BUFFER] No lines returned from PageStore")
		return
	}

	// Now we need to restore these lines into the MemoryBuffer
	// The MemoryBuffer's globalOffset is already set to lineCount (end of history)
	// We need to temporarily adjust it to load historical lines
	mb := v.memBufState.memBuf

	// Get current state
	currentGlobalOffset := mb.GlobalOffset()
	log.Printf("[MEMORY_BUFFER] Current globalOffset=%d, loading lines starting at %d", currentGlobalOffset, startIdx)

	// Temporarily set globalOffset to startIdx to allow RestoreLines to work
	// This is safe because no one is reading from the buffer yet (first resize)
	mb.SetGlobalOffset(startIdx)

	restoreStart := time.Now()

	// Restore the lines
	mb.RestoreLines(startIdx, lines)

	// The live edge should still be at the original lineCount (after all history)
	// The new shell output will start there
	// Make sure liveEdgeBase is correct
	v.memBufState.liveEdgeBase = lineCount

	// Ensure the live edge line exists for new shell output
	mb.EnsureLine(lineCount)

	restoreDuration := time.Since(restoreStart)
	totalDuration := time.Since(loadStart)

	log.Printf("[MEMORY_BUFFER] History loaded: %d lines, new globalOffset=%d, globalEnd=%d, liveEdgeBase=%d",
		len(lines), mb.GlobalOffset(), mb.GlobalEnd(), v.memBufState.liveEdgeBase)

	// Monitor: Log performance metrics
	log.Printf("[MEMORY_BUFFER] Load timing: read=%v, restore=%v, total=%v",
		readDuration, restoreDuration, totalDuration)

	// Monitor: Warn if loading is slow (> 100ms)
	if totalDuration > 100*time.Millisecond {
		log.Printf("[MEMORY_BUFFER] WARNING: History loading took %v (> 100ms). "+
			"Consider reducing history window size.", totalDuration)
	}

	// Restore saved viewport state from WAL if available
	var savedState *ViewportState
	if v.memBufState.persistence != nil && v.memBufState.persistence.wal != nil {
		savedState = v.memBufState.persistence.wal.GetRecoveredMetadata()
		if savedState != nil {
			log.Printf("[MEMORY_BUFFER] Restoring WAL-recovered metadata: scrollOffset=%d, liveEdgeBase=%d, cursor=(%d,%d) (saved at %v)",
				savedState.ScrollOffset, savedState.LiveEdgeBase, savedState.CursorX, savedState.CursorY, savedState.SavedAt)
		}
	}

	if savedState != nil {
		// Restore liveEdgeBase if it's valid (within loaded history)
		if savedState.LiveEdgeBase >= mb.GlobalOffset() && savedState.LiveEdgeBase <= mb.GlobalEnd() {
			v.memBufState.liveEdgeBase = savedState.LiveEdgeBase
		}

		// Restore scroll offset through the viewport
		if v.memBufState.viewport != nil && savedState.ScrollOffset > 0 {
			v.memBufState.viewport.ScrollToOffset(savedState.ScrollOffset)
			log.Printf("[MEMORY_BUFFER] Viewport scroll restored to offset %d", savedState.ScrollOffset)
		}

		// Restore cursor position
		if savedState.CursorX >= 0 && savedState.CursorY >= 0 {
			v.cursorX = savedState.CursorX
			v.cursorY = savedState.CursorY
			log.Printf("[MEMORY_BUFFER] Cursor position restored to (%d, %d)", savedState.CursorX, savedState.CursorY)
		}
	}
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

	// Mark as dirty for persistence with metadata for search indexing
	// The search index callback (OnLineIndexed) is called by AdaptivePersistence
	// AFTER the line is successfully persisted to disk, ensuring consistency.
	if v.memBufState.persistence != nil {
		v.memBufState.persistence.NotifyWriteWithMeta(currentGlobal, time.Now(), v.CommandActive)
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
// If a search term is set, matching text is highlighted with reversed colors.
func (v *VTerm) memoryBufferGrid() [][]Cell {
	if !v.IsMemoryBufferEnabled() {
		return nil
	}

	grid := v.memBufState.viewport.GetVisibleGrid()

	// Apply search highlighting if a term is set
	if v.searchHighlight != "" && len(grid) > 0 {
		v.applySearchHighlight(grid)
	}

	return grid
}

// applySearchHighlight finds all occurrences of the search term in the grid
// and highlights them with styled colors. It searches across the entire
// grid as continuous text to handle matches that span wrapped lines.
//
// Highlighting style:
//   - Other matches: FG changed to accent color (subtle)
//   - Selected match: selection color + Reverse attribute (stands out)
//
// If no styled colors are set (Mode == 0), falls back to simple reverse attribute.
func (v *VTerm) applySearchHighlight(grid [][]Cell) {
	termRunes := []rune(strings.ToLower(v.searchHighlight))
	termLen := len(termRunes)
	if termLen == 0 {
		return
	}

	// Check if we have styled highlighting configured
	hasStyledHighlight := v.searchSelectionColor.Mode != 0 || v.searchAccentColor.Mode != 0

	// Build a flat array of all runes and their grid coordinates
	type cellPos struct {
		y, x int
	}
	var allRunes []rune
	var positions []cellPos

	for y, row := range grid {
		for x, cell := range row {
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			allRunes = append(allRunes, unicode.ToLower(r))
			positions = append(positions, cellPos{y, x})
		}
	}

	// Find all matches first
	type match struct {
		start      int
		isSelected bool
	}
	var matches []match

	for i := 0; i <= len(allRunes)-termLen; i++ {
		found := true
		for j := 0; j < termLen; j++ {
			if allRunes[i+j] != termRunes[j] {
				found = false
				break
			}
		}
		if found {
			// Determine if this match is on the selected line
			isSelected := false
			if hasStyledHighlight && v.searchHighlightLine >= 0 && v.IsMemoryBufferEnabled() {
				// Check if the first character of the match is on the selected line
				pos := positions[i]
				globalLine, _, ok := v.memoryBufferViewportToContent(pos.y, pos.x)
				if ok && globalLine == v.searchHighlightLine {
					isSelected = true
				}
			}
			matches = append(matches, match{start: i, isSelected: isSelected})
		}
	}

	// Apply highlighting to all matches
	for _, m := range matches {
		for j := 0; j < termLen; j++ {
			pos := positions[m.start+j]
			cell := &grid[pos.y][pos.x]

			if hasStyledHighlight {
				if m.isSelected {
					// Selected match: selection color + Reverse (stands out)
					cell.FG = v.searchSelectionColor
				} else {
					// Other matches: muted/accent color + Reverse (subtle)
					cell.FG = v.searchAccentColor
				}
				cell.Attr |= AttrReverse
			} else {
				// Fallback: simple reverse attribute for all matches
				cell.Attr ^= AttrReverse
			}
		}
	}
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

	// Write metadata to WAL for crash recovery (if scroll actually changed)
	if beforeOffset != afterOffset {
		v.notifyMetadataChange()
	}
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

	// Write metadata to WAL for crash recovery
	v.notifyMetadataChange()
}

// ScrollToGlobalLine scrolls the viewport to show the specified global line index
// at approximately the center of the viewport. This is used for search result navigation.
// Returns false if the line is out of range.
func (v *VTerm) ScrollToGlobalLine(globalLineIdx int64) bool {
	if !v.IsMemoryBufferEnabled() {
		log.Printf("[SCROLL] ScrollToGlobalLine: memory buffer not enabled")
		return false
	}

	vw := v.memBufState.viewport
	reader := vw.Reader()

	// Check bounds using reader (which supports PageStore fallback)
	globalStart := reader.GlobalOffset()
	globalEnd := reader.GlobalEnd()
	log.Printf("[SCROLL] ScrollToGlobalLine(%d): globalStart=%d, globalEnd=%d", globalLineIdx, globalStart, globalEnd)
	if globalLineIdx < globalStart || globalLineIdx >= globalEnd {
		log.Printf("[SCROLL] ScrollToGlobalLine: line %d out of range [%d, %d)", globalLineIdx, globalStart, globalEnd)
		return false
	}

	// Count physical lines from the target line to the end.
	// This gives us the scroll offset (physical lines from bottom).
	// Use reader.GetLine() for PageStore fallback support.
	var physicalLinesFromTarget int64
	builder := vw.Builder()

	// For efficiency, estimate disk content (before memory) as 1:1
	memOffset := reader.MemoryBufferOffset()
	if globalLineIdx < memOffset {
		// Target is in disk content - estimate lines from target to memOffset
		physicalLinesFromTarget += memOffset - globalLineIdx
		// Then calculate exactly from memOffset to end
		for idx := memOffset; idx < globalEnd; idx++ {
			line := reader.GetLine(idx)
			if line != nil {
				wrapped := builder.BuildLine(line, idx)
				physicalLinesFromTarget += int64(len(wrapped))
			}
		}
	} else {
		// Target is in memory - calculate exactly
		for idx := globalLineIdx; idx < globalEnd; idx++ {
			line := reader.GetLine(idx)
			if line != nil {
				wrapped := builder.BuildLine(line, idx)
				physicalLinesFromTarget += int64(len(wrapped))
			}
		}
	}

	// We want the target line to be roughly centered in the viewport.
	// Subtract half the viewport height to center.
	viewportHeight := int64(vw.Height())
	targetOffset := physicalLinesFromTarget - viewportHeight/2
	if targetOffset < 0 {
		targetOffset = 0
	}

	vw.ScrollToOffset(targetOffset)
	v.MarkAllDirty()
	return true
}

// SetSearchHighlight sets the search term to highlight with reversed colors.
// The term will be highlighted wherever it appears in the visible grid.
// Pass empty string to clear highlighting.
// This is a simple version - use SetSearchHighlightStyled for styled highlighting.
func (v *VTerm) SetSearchHighlight(term string) {
	v.searchHighlight = term
	v.searchHighlightLine = -1 // No specific line selected
	v.MarkAllDirty()
}

// SetSearchHighlightStyled sets up styled search highlighting.
//
// Other matches: FG changed to accentColor (subtle highlight)
// Selected match: selectionColor + Reverse attribute (stands out)
//
// Parameters:
//   - term: the search term to highlight
//   - currentLine: the line index of the current/selected result (-1 for none)
//   - selectionColor: color for selected match (used with Reverse)
//   - accentColor: color for other matches (just FG change)
func (v *VTerm) SetSearchHighlightStyled(term string, currentLine int64, selectionColor, accentColor Color) {
	v.searchHighlight = term
	v.searchHighlightLine = currentLine
	v.searchSelectionColor = selectionColor
	v.searchAccentColor = accentColor
	v.MarkAllDirty()
}

// UpdateSearchHighlightLine updates just the current line for styled highlighting.
// Use this when navigating between results to avoid re-setting all colors.
func (v *VTerm) UpdateSearchHighlightLine(currentLine int64) {
	v.searchHighlightLine = currentLine
	v.MarkAllDirty()
}

// ClearSearchHighlight removes search term highlighting.
func (v *VTerm) ClearSearchHighlight() {
	v.searchHighlight = ""
	v.searchHighlightLine = -1
	v.MarkAllDirty()
}

// GlobalOffset returns the global index of the oldest line in the memory buffer.
// Used by the history navigator to determine valid search ranges.
func (v *VTerm) GlobalOffset() int64 {
	if !v.IsMemoryBufferEnabled() {
		return 0
	}
	return v.memBufState.memBuf.GlobalOffset()
}

// GlobalEnd returns the global index just past the last line (the live edge).
// Used by the history navigator to determine valid search ranges.
func (v *VTerm) GlobalEnd() int64 {
	if !v.IsMemoryBufferEnabled() {
		return 0
	}
	return v.memBufState.memBuf.GlobalEnd()
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

	// On first resize after initialization with history, load history from disk.
	// This is deferred until resize because we need to know the viewport height.
	if !v.memBufState.historyLoaded && v.memBufState.pageStore != nil {
		v.loadHistoryFromDisk(height)
		v.memBufState.historyLoaded = true
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

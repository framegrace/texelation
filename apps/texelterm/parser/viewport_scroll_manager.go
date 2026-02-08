// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/viewport_scroll_manager.go
// Summary: ScrollManager manages viewport scroll position and calculations.
//
// Architecture:
//
//	ScrollManager encapsulates all scroll-related state and logic:
//
//	  - Scroll offset tracking (physical lines from bottom)
//	  - Live edge detection (scrollOffset == 0)
//	  - Valid scroll range calculations
//	  - Visible line range calculations
//
//	The scroll model uses "physical lines from bottom":
//	  - scrollOffset = 0: live edge (most recent content visible)
//	  - scrollOffset = 10: scrolled back 10 physical lines
//
//	When at live edge (scrollOffset == 0), new content automatically
//	appears as it's written (auto-tracking).

package parser

// ScrollManager manages viewport scroll position and range calculations.
// Thread-safety must be managed by the caller (ViewportWindow).
type ScrollManager struct {
	// Scroll position: physical lines from bottom (0 = live edge)
	scrollOffset int64

	// Viewport height for max scroll calculations
	viewportHeight int

	// Dependencies
	reader  ContentReader
	builder *PhysicalLineBuilder

	// index caches physical line counts for O(1) total and O(log N) lookups.
	// Lazily initialized on first use.
	index *PhysicalLineIndex
}

// NewScrollManager creates a new scroll manager.
// Starts at live edge (scrollOffset = 0).
func NewScrollManager(reader ContentReader, builder *PhysicalLineBuilder) *ScrollManager {
	return &ScrollManager{
		scrollOffset:   0,
		viewportHeight: DefaultHeight,
		reader:         reader,
		builder:        builder,
	}
}

// SetViewportHeight updates the viewport height for scroll calculations.
func (sm *ScrollManager) SetViewportHeight(height int) {
	if height > 0 {
		sm.viewportHeight = height
	}
}

// ScrollUp scrolls backward (toward older content) by n physical lines.
// Returns the actual number of lines scrolled (may be less if hit boundary).
func (sm *ScrollManager) ScrollUp(n int) int {
	if n <= 0 {
		return 0
	}

	maxScroll := sm.MaxScrollOffset()
	oldOffset := sm.scrollOffset
	newOffset := min(sm.scrollOffset+int64(n), maxScroll)

	sm.scrollOffset = newOffset
	return int(newOffset - oldOffset)
}

// ScrollDown scrolls forward (toward newer content) by n physical lines.
// Returns the actual number of lines scrolled.
func (sm *ScrollManager) ScrollDown(n int) int {
	if n <= 0 {
		return 0
	}

	oldOffset := sm.scrollOffset
	newOffset := max(sm.scrollOffset-int64(n), 0)

	sm.scrollOffset = newOffset
	return int(oldOffset - newOffset)
}

// ScrollToBottom jumps to the live edge (most recent content).
func (sm *ScrollManager) ScrollToBottom() {
	sm.scrollOffset = 0
}

// ScrollToTop jumps to the oldest content.
func (sm *ScrollManager) ScrollToTop() {
	sm.scrollOffset = sm.MaxScrollOffset()
}

// ScrollToOffset sets an absolute scroll position.
// Clamps to valid range [0, MaxScrollOffset()].
func (sm *ScrollManager) ScrollToOffset(offset int64) {
	if offset < 0 {
		offset = 0
	}

	maxScroll := sm.MaxScrollOffset()
	if offset > maxScroll {
		offset = maxScroll
	}

	sm.scrollOffset = offset
}

// Offset returns the current scroll offset (physical lines from bottom).
func (sm *ScrollManager) Offset() int64 {
	return sm.scrollOffset
}

// IsAtLiveEdge returns true if showing the most recent content.
func (sm *ScrollManager) IsAtLiveEdge() bool {
	return sm.scrollOffset == 0
}

// CanScrollUp returns true if there's older content to scroll to.
func (sm *ScrollManager) CanScrollUp() bool {
	return sm.scrollOffset < sm.MaxScrollOffset()
}

// CanScrollDown returns true if scrolled back (not at live edge).
func (sm *ScrollManager) CanScrollDown() bool {
	return sm.scrollOffset > 0
}

// MaxScrollOffset returns the maximum valid scroll offset.
// This is (total physical lines - viewport height), so that we stop
// when the viewport is filled with the oldest content.
func (sm *ScrollManager) MaxScrollOffset() int64 {
	totalPhysical := sm.TotalPhysicalLines()
	// scrollOffset is "lines from bottom", so max scroll is when the oldest
	// content is at the top of the viewport
	return max(totalPhysical-int64(sm.viewportHeight), 0)
}

// TotalPhysicalLines returns the total number of physical lines at current width.
// Uses cached PhysicalLineIndex for O(1) performance.
// Disk content (before MemoryBufferOffset) is estimated at 1 physical line per logical line.
func (sm *ScrollManager) TotalPhysicalLines() int64 {
	globalOffset := sm.reader.GlobalOffset()
	memOffset := sm.reader.MemoryBufferOffset()

	// Estimate disk content (before memory): 1 physical line per logical line
	diskLines := memOffset - globalOffset

	sm.ensureIndexValid()
	return diskLines + sm.index.TotalPhysicalLines()
}

// ensureIndexValid creates or updates the physical line index as needed.
func (sm *ScrollManager) ensureIndexValid() {
	if sm.index == nil {
		sm.index = NewPhysicalLineIndex(sm.reader, sm.builder.Width())
		sm.index.Build()
		return
	}

	// Width change → full rebuild
	if sm.builder.Width() != sm.index.Width() {
		sm.index.width = sm.builder.Width()
		sm.index.Build()
		return
	}

	memOffset := sm.reader.MemoryBufferOffset()
	globalEnd := sm.reader.GlobalEnd()

	// Eviction: memory base moved forward
	if memOffset > sm.index.BaseOffset() {
		evicted := int(memOffset - sm.index.BaseOffset())
		sm.index.HandleEviction(memOffset, evicted)
	}

	// Append: new lines added
	indexEnd := sm.index.BaseOffset() + int64(sm.index.Count())
	if globalEnd > indexEnd {
		sm.index.HandleAppend(globalEnd)
	}

	// Content version changed (e.g., line edited in-place) → full rebuild
	if sm.reader.ContentVersion() != sm.index.ContentVersion() {
		sm.index.Build()
	}
}

// InvalidateIndex marks the physical line index for rebuild.
// Call this when the terminal width changes to ensure the index
// is rebuilt before the next render.
func (sm *ScrollManager) InvalidateIndex() {
	if sm.index != nil {
		sm.index.Invalidate()
	}
}

// VisibleRange returns the global line indices that should be visible.
// viewportHeight is the number of physical lines the viewport can display.
//
// Returns (startGlobalIdx, endGlobalIdx) where endGlobalIdx is exclusive.
// Note: This returns logical line indices, not physical line indices.
// The caller must wrap these lines to get the actual physical lines to display.
func (sm *ScrollManager) VisibleRange(viewportHeight int) (startGlobalIdx, endGlobalIdx int64) {
	globalOffset := sm.reader.GlobalOffset()
	memOffset := sm.reader.MemoryBufferOffset()
	globalEnd := sm.reader.GlobalEnd()

	if globalEnd <= globalOffset {
		// No content
		return globalOffset, globalOffset
	}

	// Calculate total physical lines and target range
	totalPhysical := sm.TotalPhysicalLines()
	physicalEnd := min(totalPhysical-sm.scrollOffset, totalPhysical)
	physicalStart := max(physicalEnd-int64(viewportHeight), 0)

	// Disk content uses 1:1 logical:physical mapping (estimation)
	diskLines := memOffset - globalOffset

	// If entire visible range is in disk content, use direct mapping
	if physicalEnd <= diskLines {
		startGlobalIdx = globalOffset + physicalStart
		endGlobalIdx = globalOffset + physicalEnd
		return startGlobalIdx, min(endGlobalIdx, memOffset)
	}

	// If entire visible range is in memory, use exact calculation
	if physicalStart >= diskLines {
		memPhysicalStart := physicalStart - diskLines
		memPhysicalEnd := physicalEnd - diskLines
		return sm.findLogicalRangeInMemory(memOffset, globalEnd, memPhysicalStart, memPhysicalEnd)
	}

	// Range spans disk and memory - handle both parts
	// Disk part: lines from globalOffset to memOffset
	startGlobalIdx = globalOffset + physicalStart

	// Memory part: find where physical line (physicalEnd - diskLines) falls
	memPhysicalEnd := physicalEnd - diskLines
	_, endGlobalIdx = sm.findLogicalRangeInMemory(memOffset, globalEnd, 0, memPhysicalEnd)

	return startGlobalIdx, endGlobalIdx
}

// findLogicalRangeInMemory finds which logical lines contain the given physical line range.
// Uses binary search on the PhysicalLineIndex prefix sum for O(log N) performance.
func (sm *ScrollManager) findLogicalRangeInMemory(memStart, memEnd, physicalStart, physicalEnd int64) (startGlobalIdx, endGlobalIdx int64) {
	sm.ensureIndexValid()

	if sm.index.Count() == 0 {
		return memStart, memEnd
	}

	// Find the logical line containing physicalStart
	startGlobalIdx, _ = sm.index.PhysicalToLogical(physicalStart)

	// Find the exclusive end boundary.
	// physicalEnd is exclusive, so we need the first logical line AFTER the
	// visible range. PhysicalToLogical returns the line containing physicalEnd.
	// If physicalEnd falls partway through a logical line (offset > 0),
	// that line is still needed, so bump the end past it.
	if physicalEnd >= sm.index.TotalPhysicalLines() {
		endGlobalIdx = memEnd
	} else {
		endLogical, endOffset := sm.index.PhysicalToLogical(physicalEnd)
		if endOffset > 0 {
			endGlobalIdx = endLogical + 1
		} else {
			endGlobalIdx = endLogical
		}
	}

	// Clamp to valid range
	if startGlobalIdx < memStart {
		startGlobalIdx = memStart
	}
	if endGlobalIdx > memEnd {
		endGlobalIdx = memEnd
	}

	return startGlobalIdx, endGlobalIdx
}

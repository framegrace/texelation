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
func (sm *ScrollManager) TotalPhysicalLines() int64 {
	globalStart := sm.reader.GlobalOffset()
	globalEnd := sm.reader.GlobalEnd()

	var total int64
	for idx := globalStart; idx < globalEnd; idx++ {
		line := sm.reader.GetLine(idx)
		if line != nil {
			wrapped := sm.builder.BuildLine(line, idx)
			total += int64(len(wrapped))
		}
	}
	return total
}

// VisibleRange returns the global line indices that should be visible.
// viewportHeight is the number of physical lines the viewport can display.
//
// Returns (startGlobalIdx, endGlobalIdx) where endGlobalIdx is exclusive.
// Note: This returns logical line indices, not physical line indices.
// The caller must wrap these lines to get the actual physical lines to display.
func (sm *ScrollManager) VisibleRange(viewportHeight int) (startGlobalIdx, endGlobalIdx int64) {
	globalOffset := sm.reader.GlobalOffset()
	globalEnd := sm.reader.GlobalEnd()

	if globalEnd <= globalOffset {
		// No content
		return globalOffset, globalOffset
	}

	// We need to work backwards from the end to find which logical lines
	// produce the physical lines we want to display
	totalPhysical := sm.TotalPhysicalLines()

	// The last physical line we want to show is at (totalPhysical - scrollOffset - 1)
	// The first physical line we want to show is that minus viewportHeight
	physicalEnd := min(totalPhysical-sm.scrollOffset, totalPhysical)
	physicalStart := max(physicalEnd-int64(viewportHeight), 0)

	// Now find which logical lines contain these physical lines
	// We need to find the logical line that contains physicalStart
	// and the logical line that contains physicalEnd-1

	var runningPhysical int64
	startFound := false
	startGlobalIdx = globalOffset
	endGlobalIdx = globalEnd

	for idx := globalOffset; idx < globalEnd; idx++ {
		line := sm.reader.GetLine(idx)
		physicalCount := int64(1)
		if line != nil {
			wrapped := sm.builder.BuildLine(line, idx)
			physicalCount = int64(len(wrapped))
		}

		// Check if this logical line contains our start physical line
		if !startFound && runningPhysical+physicalCount > physicalStart {
			startGlobalIdx = idx
			startFound = true
		}

		// Check if this logical line contains our end physical line
		if runningPhysical >= physicalEnd {
			endGlobalIdx = idx
			break
		}

		runningPhysical += physicalCount
	}

	return startGlobalIdx, endGlobalIdx
}

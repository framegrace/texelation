// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/scroll/state.go
// Summary: Immutable scroll state calculator for viewport scrolling.
// Provides stateless scroll math that can be reused by any scrollable widget.

package scroll

// State represents the scroll state of a viewport.
// All methods return new State values (immutable pattern).
type State struct {
	ContentHeight  int // Total rows in scrollable content
	ViewportHeight int // Visible rows in viewport
	Offset         int // Current scroll offset (first visible row)
}

// NewState creates a scroll state with the given dimensions.
func NewState(contentHeight, viewportHeight int) State {
	s := State{
		ContentHeight:  contentHeight,
		ViewportHeight: viewportHeight,
		Offset:         0,
	}
	return s.Clamp()
}

// Clamp returns a new state with the offset clamped to valid bounds.
func (s State) Clamp() State {
	result := s

	// Ensure non-negative values
	if result.ContentHeight < 0 {
		result.ContentHeight = 0
	}
	if result.ViewportHeight < 0 {
		result.ViewportHeight = 0
	}

	// Clamp offset to valid range [0, maxOffset]
	maxOffset := result.MaxOffset()
	if result.Offset < 0 {
		result.Offset = 0
	}
	if result.Offset > maxOffset {
		result.Offset = maxOffset
	}

	return result
}

// MaxOffset returns the maximum valid scroll offset.
// Returns 0 if content fits within viewport.
func (s State) MaxOffset() int {
	max := s.ContentHeight - s.ViewportHeight
	if max < 0 {
		return 0
	}
	return max
}

// CanScrollUp returns true if there is content above the viewport.
func (s State) CanScrollUp() bool {
	return s.Offset > 0
}

// CanScrollDown returns true if there is content below the viewport.
func (s State) CanScrollDown() bool {
	return s.Offset < s.MaxOffset()
}

// CanScroll returns true if the content exceeds the viewport height.
func (s State) CanScroll() bool {
	return s.ContentHeight > s.ViewportHeight
}

// VisibleRange returns the range of visible rows [start, end).
// The end is exclusive: rows from start to end-1 are visible.
func (s State) VisibleRange() (start, end int) {
	start = s.Offset
	end = s.Offset + s.ViewportHeight
	if end > s.ContentHeight {
		end = s.ContentHeight
	}
	return start, end
}

// IsRowVisible returns true if the given row is within the visible range.
func (s State) IsRowVisible(row int) bool {
	start, end := s.VisibleRange()
	return row >= start && row < end
}

// ScrollBy returns a new state scrolled by the given delta.
// Positive delta scrolls down, negative scrolls up.
func (s State) ScrollBy(delta int) State {
	result := s
	result.Offset += delta
	return result.Clamp()
}

// ScrollTo returns a new state scrolled to make the given row visible.
// Uses minimal movement: only scrolls if the row is outside the viewport.
func (s State) ScrollTo(row int) State {
	if s.ViewportHeight <= 0 {
		return s
	}

	result := s

	// If row is above viewport, scroll up to show it at top
	if row < result.Offset {
		result.Offset = row
	}

	// If row is below viewport, scroll down to show it at bottom
	if row >= result.Offset+result.ViewportHeight {
		result.Offset = row - result.ViewportHeight + 1
	}

	return result.Clamp()
}

// ScrollToCentered returns a new state with the given row centered in the viewport.
func (s State) ScrollToCentered(row int) State {
	if s.ViewportHeight <= 0 {
		return s
	}

	result := s
	result.Offset = row - result.ViewportHeight/2
	return result.Clamp()
}

// ScrollToTop returns a new state scrolled to the top.
func (s State) ScrollToTop() State {
	result := s
	result.Offset = 0
	return result
}

// ScrollToBottom returns a new state scrolled to the bottom.
func (s State) ScrollToBottom() State {
	result := s
	result.Offset = result.MaxOffset()
	return result
}

// WithContentHeight returns a new state with updated content height.
func (s State) WithContentHeight(height int) State {
	result := s
	result.ContentHeight = height
	return result.Clamp()
}

// WithViewportHeight returns a new state with updated viewport height.
func (s State) WithViewportHeight(height int) State {
	result := s
	result.ViewportHeight = height
	return result.Clamp()
}

// WithOffset returns a new state with updated offset.
func (s State) WithOffset(offset int) State {
	result := s
	result.Offset = offset
	return result.Clamp()
}

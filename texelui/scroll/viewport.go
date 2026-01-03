// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/scroll/viewport.go
// Summary: Viewport coordinate translation for scrollable content.
// Translates between content coordinates and screen coordinates.

package scroll

import "texelation/texelui/core"

// Viewport represents the visible region of scrolled content.
// It handles coordinate translation between content space and screen space.
type Viewport struct {
	// ScreenRect is the viewport bounds in screen coordinates
	ScreenRect core.Rect

	// ScrollOffset is the content row offset (how many rows scrolled)
	ScrollOffset int
}

// NewViewport creates a viewport with the given screen rectangle and scroll offset.
func NewViewport(screenRect core.Rect, scrollOffset int) Viewport {
	return Viewport{
		ScreenRect:   screenRect,
		ScrollOffset: scrollOffset,
	}
}

// ContentToScreen translates a content Y coordinate to a screen Y coordinate.
// Returns the screen Y and whether the coordinate is within the visible viewport.
func (v Viewport) ContentToScreen(contentY int) (screenY int, visible bool) {
	screenY = contentY - v.ScrollOffset + v.ScreenRect.Y
	visible = screenY >= v.ScreenRect.Y && screenY < v.ScreenRect.Y+v.ScreenRect.H
	return screenY, visible
}

// ScreenToContent translates a screen Y coordinate to a content Y coordinate.
func (v Viewport) ScreenToContent(screenY int) int {
	return screenY - v.ScreenRect.Y + v.ScrollOffset
}

// ContentRectToScreen translates a content rectangle to screen coordinates.
// Returns the screen rectangle and whether any part is visible.
func (v Viewport) ContentRectToScreen(contentRect core.Rect) (screenRect core.Rect, visible bool) {
	screenY, _ := v.ContentToScreen(contentRect.Y)
	screenRect = core.Rect{
		X: contentRect.X, // X is not translated (no horizontal scroll)
		Y: screenY,
		W: contentRect.W,
		H: contentRect.H,
	}

	// Check if any part of the rectangle is visible
	rectTop := screenRect.Y
	rectBottom := screenRect.Y + screenRect.H
	viewportTop := v.ScreenRect.Y
	viewportBottom := v.ScreenRect.Y + v.ScreenRect.H

	visible = rectBottom > viewportTop && rectTop < viewportBottom
	return screenRect, visible
}

// ClipToViewport returns the intersection of a screen rectangle with the viewport.
// Returns the clipped rectangle and whether any part is visible.
func (v Viewport) ClipToViewport(screenRect core.Rect) (clipped core.Rect, visible bool) {
	// Calculate intersection
	left := max(screenRect.X, v.ScreenRect.X)
	top := max(screenRect.Y, v.ScreenRect.Y)
	right := min(screenRect.X+screenRect.W, v.ScreenRect.X+v.ScreenRect.W)
	bottom := min(screenRect.Y+screenRect.H, v.ScreenRect.Y+v.ScreenRect.H)

	// Check if there's a valid intersection
	if left >= right || top >= bottom {
		return core.Rect{}, false
	}

	clipped = core.Rect{
		X: left,
		Y: top,
		W: right - left,
		H: bottom - top,
	}
	return clipped, true
}

// IsContentRectVisible returns true if any part of the content rectangle is visible.
func (v Viewport) IsContentRectVisible(contentRect core.Rect) bool {
	_, visible := v.ContentRectToScreen(contentRect)
	return visible
}

// IsContentRowVisible returns true if the given content row is visible.
func (v Viewport) IsContentRowVisible(contentY int) bool {
	_, visible := v.ContentToScreen(contentY)
	return visible
}

// VisibleContentRange returns the range of content rows that are visible.
// Returns [startY, endY) where endY is exclusive.
func (v Viewport) VisibleContentRange() (startY, endY int) {
	startY = v.ScrollOffset
	endY = v.ScrollOffset + v.ScreenRect.H
	return startY, endY
}

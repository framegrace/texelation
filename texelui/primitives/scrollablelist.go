// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/primitives/scrollablelist.go
// Summary: Vertical scrollable list widget with item selection.

package primitives

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
	"texelation/texelui/scroll"
)

// ListItem represents a single item in a ScrollableList.
type ListItem struct {
	Text  string
	Value interface{} // Optional data payload
}

// ListItemRenderer is a custom rendering function for list items.
// If nil, the default rendering is used (text with selection highlight).
type ListItemRenderer func(p *core.Painter, rect core.Rect, item ListItem, selected bool)

// ScrollableList is a vertical scrollable list widget with item selection.
// It supports keyboard navigation (Up/Down/Home/End/PgUp/PgDn), mouse clicks,
// and scroll wheel. Features a draggable scrollbar when content overflows.
type ScrollableList struct {
	core.BaseWidget
	Items       []ListItem
	SelectedIdx int
	OnChange    func(int) // Called when selection changes

	// Custom rendering (optional)
	RenderItem ListItemRenderer

	// Show scroll indicators when content overflows
	ShowScrollIndicators bool

	// Internal state
	scrollOffset    int
	scrollState     scroll.State
	indicatorConfig scroll.IndicatorConfig
	inv             func(core.Rect)

	// Scrollbar mouse interaction state
	draggingThumb   bool // True when thumb is being dragged
	dragStartY      int  // Y position where drag started
	dragStartOffset int  // Scroll offset when drag started
}

// NewScrollableList creates a new scrollable list at the specified position and size.
func NewScrollableList(x, y, w, h int) *ScrollableList {
	sl := &ScrollableList{
		Items:                []ListItem{},
		SelectedIdx:          0,
		ShowScrollIndicators: true,
		scrollOffset:         0,
	}

	sl.SetPosition(x, y)
	sl.Resize(w, h)
	sl.SetFocusable(true)

	// Configure focus style from theme
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	sl.SetFocusedStyle(tcell.StyleDefault.Foreground(fg).Background(bg), true)

	// Set up scrollbar with text.primary color for thumb
	thumbStyle := tcell.StyleDefault.Foreground(fg).Background(bg)
	trackFg := tm.GetSemanticColor("text.muted")
	if trackFg == tcell.ColorDefault {
		trackFg = fg
	}
	trackStyle := tcell.StyleDefault.Foreground(trackFg).Background(bg)
	sl.indicatorConfig = scroll.DefaultIndicatorConfigWithScrollbar(thumbStyle, trackStyle)

	return sl
}

// SetInvalidator allows the UI manager to inject a dirty-region invalidator.
func (sl *ScrollableList) SetInvalidator(fn func(core.Rect)) {
	sl.inv = fn
}

// SetItems replaces the list items.
func (sl *ScrollableList) SetItems(items []ListItem) {
	sl.Items = items
	// Clamp selection to valid range
	if sl.SelectedIdx >= len(items) {
		sl.SelectedIdx = len(items) - 1
	}
	if sl.SelectedIdx < 0 {
		sl.SelectedIdx = 0
	}
	sl.ensureSelectedVisible()
	sl.invalidate()
}

// SetSelected changes the selected item by index.
func (sl *ScrollableList) SetSelected(idx int) {
	if idx < 0 || idx >= len(sl.Items) {
		return
	}
	if idx == sl.SelectedIdx {
		return
	}
	sl.SelectedIdx = idx
	sl.ensureSelectedVisible()
	sl.invalidate()
	if sl.OnChange != nil {
		sl.OnChange(idx)
	}
}

// SelectedItem returns the currently selected item, or nil if none.
func (sl *ScrollableList) SelectedItem() *ListItem {
	if sl.SelectedIdx >= 0 && sl.SelectedIdx < len(sl.Items) {
		return &sl.Items[sl.SelectedIdx]
	}
	return nil
}

// Clear removes all items from the list.
func (sl *ScrollableList) Clear() {
	sl.Items = []ListItem{}
	sl.SelectedIdx = 0
	sl.scrollOffset = 0
	sl.invalidate()
}

// ensureSelectedVisible adjusts scroll offset to keep selected item visible.
// Centers the selected item when possible.
func (sl *ScrollableList) ensureSelectedVisible() {
	if len(sl.Items) == 0 || sl.Rect.H <= 0 {
		sl.scrollOffset = 0
		sl.scrollState = scroll.NewState(0, sl.Rect.H)
		return
	}

	viewportH := sl.Rect.H

	// Center selected item in viewport
	targetOffset := sl.SelectedIdx - viewportH/2
	if targetOffset < 0 {
		targetOffset = 0
	}

	maxOffset := len(sl.Items) - viewportH
	if maxOffset < 0 {
		maxOffset = 0
	}
	if targetOffset > maxOffset {
		targetOffset = maxOffset
	}

	sl.scrollOffset = targetOffset
	sl.scrollState = scroll.State{
		ContentHeight:  len(sl.Items),
		ViewportHeight: viewportH,
		Offset:         targetOffset,
	}
}

// Draw renders the scrollable list.
func (sl *ScrollableList) Draw(painter *core.Painter) {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	baseStyle := tcell.StyleDefault.Foreground(fg).Background(bg)

	// Fill background
	painter.Fill(sl.Rect, ' ', baseStyle)

	if len(sl.Items) == 0 {
		return
	}

	viewportH := sl.Rect.H

	// Ensure scroll state is up to date
	sl.scrollState = scroll.State{
		ContentHeight:  len(sl.Items),
		ViewportHeight: viewportH,
		Offset:         sl.scrollOffset,
	}

	// Calculate content width (leave room for scrollbar if needed)
	contentW := sl.Rect.W
	if sl.ShowScrollIndicators && sl.scrollState.CanScroll() {
		contentW-- // Reserve 1 column for scrollbar
	}

	// Draw visible items
	y := sl.Rect.Y
	for i := sl.scrollOffset; i < len(sl.Items) && y < sl.Rect.Y+viewportH; i++ {
		item := sl.Items[i]
		selected := i == sl.SelectedIdx

		itemRect := core.Rect{
			X: sl.Rect.X,
			Y: y,
			W: contentW,
			H: 1,
		}

		if sl.RenderItem != nil {
			// Custom rendering
			sl.RenderItem(painter, itemRect, item, selected)
		} else {
			// Default rendering
			sl.drawDefaultItem(painter, itemRect, item, selected, baseStyle)
		}

		y++
	}

	// Draw scroll indicators if enabled
	if sl.ShowScrollIndicators {
		scroll.DrawIndicators(painter, sl.Rect, sl.scrollState, sl.indicatorConfig)
	}
}

// drawDefaultItem renders a list item with default styling.
func (sl *ScrollableList) drawDefaultItem(painter *core.Painter, rect core.Rect, item ListItem, selected bool, baseStyle tcell.Style) {
	style := baseStyle
	if selected {
		style = style.Reverse(true)
	}

	// Fill item background
	painter.Fill(rect, ' ', style)

	// Draw text (truncate if needed)
	text := item.Text
	maxLen := rect.W
	if len(text) > maxLen && maxLen > 0 {
		text = text[:maxLen]
	}

	painter.DrawText(rect.X, rect.Y, text, style)
}


// HandleKey processes keyboard input for list navigation.
func (sl *ScrollableList) HandleKey(ev *tcell.EventKey) bool {
	if len(sl.Items) == 0 {
		return false
	}

	switch ev.Key() {
	case tcell.KeyUp:
		if sl.SelectedIdx > 0 {
			sl.SetSelected(sl.SelectedIdx - 1)
			return true
		}
		return false

	case tcell.KeyDown:
		if sl.SelectedIdx < len(sl.Items)-1 {
			sl.SetSelected(sl.SelectedIdx + 1)
			return true
		}
		return false

	case tcell.KeyHome:
		if sl.SelectedIdx != 0 {
			sl.SetSelected(0)
			return true
		}
		return false

	case tcell.KeyEnd:
		lastIdx := len(sl.Items) - 1
		if sl.SelectedIdx != lastIdx {
			sl.SetSelected(lastIdx)
			return true
		}
		return false

	case tcell.KeyPgUp:
		pageSize := sl.Rect.H
		if pageSize < 1 {
			pageSize = 1
		}
		newIdx := sl.SelectedIdx - pageSize
		if newIdx < 0 {
			newIdx = 0
		}
		if newIdx != sl.SelectedIdx {
			sl.SetSelected(newIdx)
			return true
		}
		return false

	case tcell.KeyPgDn:
		pageSize := sl.Rect.H
		if pageSize < 1 {
			pageSize = 1
		}
		newIdx := sl.SelectedIdx + pageSize
		if newIdx >= len(sl.Items) {
			newIdx = len(sl.Items) - 1
		}
		if newIdx != sl.SelectedIdx {
			sl.SetSelected(newIdx)
			return true
		}
		return false
	}

	return false
}

// HandleMouse processes mouse input for item selection and scrolling.
func (sl *ScrollableList) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	buttons := ev.Buttons()

	// Handle drag release
	if sl.draggingThumb && buttons&tcell.Button1 == 0 {
		sl.draggingThumb = false
		return true
	}

	// Handle ongoing thumb drag
	if sl.draggingThumb && buttons&tcell.Button1 != 0 {
		sl.handleThumbDrag(y)
		return true
	}

	if len(sl.Items) == 0 {
		return false
	}

	if !sl.HitTest(x, y) {
		return false
	}

	// Handle scroll wheel
	if buttons&tcell.WheelUp != 0 {
		if sl.SelectedIdx > 0 {
			sl.SetSelected(sl.SelectedIdx - 1)
		}
		return true
	}
	if buttons&tcell.WheelDown != 0 {
		if sl.SelectedIdx < len(sl.Items)-1 {
			sl.SetSelected(sl.SelectedIdx + 1)
		}
		return true
	}

	// Check if click is on scrollbar
	if sl.ShowScrollIndicators && sl.scrollState.CanScroll() && buttons&tcell.Button1 != 0 {
		scrollbarX, thumbStart, thumbEnd, trackHeight := sl.scrollbarGeometry()
		if scrollbarX >= 0 && x == scrollbarX {
			relY := y - sl.Rect.Y

			// Up arrow (row 0)
			if relY == 0 {
				sl.scrollBy(-1)
				return true
			}

			// Down arrow (last row)
			if relY == sl.Rect.H-1 {
				sl.scrollBy(1)
				return true
			}

			// Track area (between arrows)
			trackY := relY - 1
			if trackY >= 0 && trackY < trackHeight {
				if trackY < thumbStart {
					// Click above thumb - page up
					sl.scrollBy(-sl.Rect.H)
					return true
				} else if trackY >= thumbEnd {
					// Click below thumb - page down
					sl.scrollBy(sl.Rect.H)
					return true
				} else {
					// Click on thumb - start drag
					sl.draggingThumb = true
					sl.dragStartY = y
					sl.dragStartOffset = sl.scrollOffset
					return true
				}
			}
		}
	}

	// Handle click on list item
	if buttons == tcell.Button1 {
		relY := y - sl.Rect.Y
		clickedIdx := sl.scrollOffset + relY

		if clickedIdx >= 0 && clickedIdx < len(sl.Items) {
			if clickedIdx != sl.SelectedIdx {
				sl.SetSelected(clickedIdx)
			}
			return true
		}
	}

	return false
}

// scrollbarGeometry returns the scrollbar's X position and thumb start/end rows.
// Returns scrollbarX, thumbStart, thumbEnd, trackHeight.
func (sl *ScrollableList) scrollbarGeometry() (scrollbarX, thumbStart, thumbEnd, trackHeight int) {
	rect := sl.Rect
	if rect.H < 3 || !sl.scrollState.CanScroll() {
		return -1, 0, 0, 0
	}

	// Scrollbar X position (right edge)
	scrollbarX = rect.X + rect.W - 1

	// Track is between arrows (row 1 to H-2)
	trackHeight = rect.H - 2
	if trackHeight <= 0 {
		return scrollbarX, 0, 0, 0
	}

	// Calculate thumb size
	thumbSize := (sl.scrollState.ViewportHeight * trackHeight) / sl.scrollState.ContentHeight
	minThumb := sl.indicatorConfig.Scrollbar.MinThumbSize
	if minThumb <= 0 {
		minThumb = 1
	}
	if thumbSize < minThumb {
		thumbSize = minThumb
	}
	if thumbSize > trackHeight {
		thumbSize = trackHeight
	}

	// Calculate thumb position
	scrollableContent := sl.scrollState.ContentHeight - sl.scrollState.ViewportHeight
	scrollableTrack := trackHeight - thumbSize

	thumbStart = 0
	if scrollableContent > 0 && scrollableTrack > 0 {
		thumbStart = (sl.scrollState.Offset * scrollableTrack) / scrollableContent
	}
	if thumbStart < 0 {
		thumbStart = 0
	}
	if thumbStart > scrollableTrack {
		thumbStart = scrollableTrack
	}
	thumbEnd = thumbStart + thumbSize

	return scrollbarX, thumbStart, thumbEnd, trackHeight
}

// handleThumbDrag updates scroll position based on thumb drag.
func (sl *ScrollableList) handleThumbDrag(currentY int) {
	if !sl.scrollState.CanScroll() {
		return
	}

	deltaY := currentY - sl.dragStartY
	trackHeight := sl.Rect.H - 2
	if trackHeight <= 0 {
		return
	}

	// Calculate thumb size
	thumbSize := (sl.scrollState.ViewportHeight * trackHeight) / sl.scrollState.ContentHeight
	minThumb := sl.indicatorConfig.Scrollbar.MinThumbSize
	if minThumb <= 0 {
		minThumb = 1
	}
	if thumbSize < minThumb {
		thumbSize = minThumb
	}
	if thumbSize > trackHeight {
		thumbSize = trackHeight
	}

	scrollableTrack := trackHeight - thumbSize
	scrollableContent := sl.scrollState.ContentHeight - sl.scrollState.ViewportHeight

	if scrollableTrack <= 0 || scrollableContent <= 0 {
		return
	}

	// Convert mouse delta to content offset delta
	deltaOffset := (deltaY * scrollableContent) / scrollableTrack
	newOffset := sl.dragStartOffset + deltaOffset

	// Clamp to valid range
	if newOffset < 0 {
		newOffset = 0
	}
	if newOffset > scrollableContent {
		newOffset = scrollableContent
	}

	if newOffset != sl.scrollOffset {
		sl.scrollOffset = newOffset
		sl.scrollState = sl.scrollState.WithOffset(newOffset)
		sl.invalidate()
	}
}

// scrollBy scrolls by delta items (positive = down, negative = up).
func (sl *ScrollableList) scrollBy(delta int) {
	maxOffset := len(sl.Items) - sl.Rect.H
	if maxOffset < 0 {
		maxOffset = 0
	}

	newOffset := sl.scrollOffset + delta
	if newOffset < 0 {
		newOffset = 0
	}
	if newOffset > maxOffset {
		newOffset = maxOffset
	}

	if newOffset != sl.scrollOffset {
		sl.scrollOffset = newOffset
		sl.scrollState = sl.scrollState.WithOffset(newOffset)
		sl.invalidate()
	}
}

// invalidate marks the widget as needing redraw.
func (sl *ScrollableList) invalidate() {
	if sl.inv != nil {
		sl.inv(sl.Rect)
	}
}

// GetKeyHints implements KeyHintsProvider from core package.
func (sl *ScrollableList) GetKeyHints() []core.KeyHint {
	return []core.KeyHint{
		{Key: "↑↓", Label: "Navigate"},
		{Key: "PgUp/Dn", Label: "Page"},
		{Key: "Home/End", Label: "Jump"},
	}
}

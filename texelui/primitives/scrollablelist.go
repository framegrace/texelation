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
	scrollPane *scroll.ScrollPane
	content    *listContent
	inv        func(core.Rect)
}

// listContent is the internal widget that renders list items.
// It's wrapped by ScrollPane for scrolling support.
type listContent struct {
	core.BaseWidget
	parent *ScrollableList
}

// NewScrollableList creates a new scrollable list at the specified position and size.
func NewScrollableList(x, y, w, h int) *ScrollableList {
	sl := &ScrollableList{
		Items:                []ListItem{},
		SelectedIdx:          0,
		ShowScrollIndicators: true,
	}

	// Create internal content widget
	sl.content = &listContent{parent: sl}

	// Create scroll pane wrapping the content
	sl.scrollPane = scroll.NewScrollPane()
	sl.scrollPane.SetChild(sl.content)

	sl.SetPosition(x, y)
	sl.Resize(w, h)
	sl.SetFocusable(true)

	// Configure focus style from theme
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	sl.SetFocusedStyle(tcell.StyleDefault.Foreground(fg).Background(bg), true)

	return sl
}

// SetInvalidator allows the UI manager to inject a dirty-region invalidator.
func (sl *ScrollableList) SetInvalidator(fn func(core.Rect)) {
	sl.inv = fn
	sl.scrollPane.SetInvalidator(fn)
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
	// Update scroll pane content height
	sl.updateScrollPaneContentHeight()
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
	sl.updateScrollPaneContentHeight()
	sl.invalidate()
}

// Resize updates the list size and recalculates layout.
func (sl *ScrollableList) Resize(w, h int) {
	sl.BaseWidget.Resize(w, h)
	sl.scrollPane.SetPosition(sl.Rect.X, sl.Rect.Y)
	sl.scrollPane.Resize(w, h)
	// Update content size
	sl.content.Resize(w, len(sl.Items))
	sl.updateScrollPaneContentHeight()
}

// SetPosition updates the list position.
func (sl *ScrollableList) SetPosition(x, y int) {
	sl.BaseWidget.SetPosition(x, y)
	sl.scrollPane.SetPosition(x, y)
}

// updateScrollPaneContentHeight updates the scroll pane's content height.
func (sl *ScrollableList) updateScrollPaneContentHeight() {
	sl.scrollPane.SetContentHeight(len(sl.Items))
}

// ensureSelectedVisible scrolls to make the selected item visible.
func (sl *ScrollableList) ensureSelectedVisible() {
	if len(sl.Items) == 0 {
		return
	}
	// Center the selected item in the viewport
	sl.scrollPane.ScrollToCentered(sl.SelectedIdx)
}

// Draw renders the scrollable list via the scroll pane.
func (sl *ScrollableList) Draw(painter *core.Painter) {
	// Ensure content size matches items count
	sl.content.Resize(sl.Rect.W, len(sl.Items))
	sl.scrollPane.ShowIndicators(sl.ShowScrollIndicators)
	sl.scrollPane.Draw(painter)
}

// ContentHeight implements scroll.ContentHeightProvider for listContent.
func (lc *listContent) ContentHeight() int {
	return len(lc.parent.Items)
}

// HandlePageNavigation implements scroll.PageNavigator for selection-based page navigation.
// This moves the selected item by a page-worth of items rather than just scrolling the viewport.
func (lc *listContent) HandlePageNavigation(direction int, pageSize int) bool {
	sl := lc.parent
	if len(sl.Items) == 0 {
		return false
	}

	if pageSize < 1 {
		pageSize = 1
	}

	// Calculate target index
	targetIdx := sl.SelectedIdx + (direction * pageSize)

	// Clamp to valid range
	if targetIdx < 0 {
		targetIdx = 0
	}
	if targetIdx >= len(sl.Items) {
		targetIdx = len(sl.Items) - 1
	}

	// Only act if we're actually moving
	if targetIdx == sl.SelectedIdx {
		return false
	}

	sl.SetSelected(targetIdx)
	return true
}

// Draw renders the list items.
func (lc *listContent) Draw(painter *core.Painter) {
	sl := lc.parent
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	baseStyle := tcell.StyleDefault.Foreground(fg).Background(bg)

	if len(sl.Items) == 0 {
		return
	}

	// Get scroll offset from parent's scroll pane
	scrollOffset := sl.scrollPane.ScrollOffset()

	// Calculate content width (leave room for scrollbar if needed)
	contentW := sl.Rect.W
	if sl.ShowScrollIndicators && sl.scrollPane.CanScroll() {
		contentW-- // Reserve 1 column for scrollbar
	}

	// Draw items, accounting for scroll offset
	// Note: Use sl.Rect (parent's rect) for screen positions since ScrollPane
	// manages clipping. lc.Rect is adjusted by ScrollPane during Draw which
	// we don't want to use here.
	for i, item := range sl.Items {
		// Skip items above viewport
		if i < scrollOffset {
			continue
		}
		// Stop if below viewport
		if i >= scrollOffset+sl.Rect.H {
			break
		}

		// Calculate screen position relative to parent's viewport
		y := sl.Rect.Y + (i - scrollOffset)
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
			lc.drawDefaultItem(painter, itemRect, item, selected, baseStyle)
		}
	}
}

// drawDefaultItem renders a list item with default styling.
func (lc *listContent) drawDefaultItem(painter *core.Painter, rect core.Rect, item ListItem, selected bool, baseStyle tcell.Style) {
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

	case tcell.KeyPgUp, tcell.KeyPgDn:
		// Let scroll pane handle page up/down - it will delegate to our
		// listContent.HandlePageNavigation for selection-based navigation
		return sl.scrollPane.HandleKey(ev)
	}

	return false
}

// HandleMouse processes mouse input for item selection and scrolling.
func (sl *ScrollableList) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	buttons := ev.Buttons()

	if len(sl.Items) == 0 && buttons&(tcell.WheelUp|tcell.WheelDown) == 0 {
		return false
	}

	if !sl.HitTest(x, y) {
		return false
	}

	// Handle scroll wheel - moves selection, not viewport
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

	// Let scroll pane handle scrollbar interactions
	if sl.scrollPane.HandleMouse(ev) {
		return true
	}

	// Handle click on list item
	if buttons == tcell.Button1 {
		scrollOffset := sl.scrollPane.ScrollOffset()
		relY := y - sl.Rect.Y
		clickedIdx := scrollOffset + relY

		if clickedIdx >= 0 && clickedIdx < len(sl.Items) {
			if clickedIdx != sl.SelectedIdx {
				sl.SetSelected(clickedIdx)
			}
			return true
		}
	}

	return false
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

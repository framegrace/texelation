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
// and scroll wheel.
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
	scrollOffset int
	inv          func(core.Rect)
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

	// Draw visible items
	y := sl.Rect.Y
	for i := sl.scrollOffset; i < len(sl.Items) && y < sl.Rect.Y+viewportH; i++ {
		item := sl.Items[i]
		selected := i == sl.SelectedIdx

		itemRect := core.Rect{
			X: sl.Rect.X,
			Y: y,
			W: sl.Rect.W,
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
		sl.drawScrollIndicators(painter, baseStyle)
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

// drawScrollIndicators draws ▲ and ▼ when content is scrollable.
func (sl *ScrollableList) drawScrollIndicators(painter *core.Painter, baseStyle tcell.Style) {
	viewportH := sl.Rect.H
	indicatorX := sl.Rect.X + sl.Rect.W - 1

	// Show ▲ if there's content above
	if sl.scrollOffset > 0 {
		painter.SetCell(indicatorX, sl.Rect.Y, '▲', baseStyle)
	}

	// Show ▼ if there's content below
	if sl.scrollOffset+viewportH < len(sl.Items) {
		painter.SetCell(indicatorX, sl.Rect.Y+viewportH-1, '▼', baseStyle)
	}
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
	if len(sl.Items) == 0 {
		return false
	}

	x, y := ev.Position()
	if !sl.HitTest(x, y) {
		return false
	}

	buttons := ev.Buttons()

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

	// Handle click
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

// invalidate marks the widget as needing redraw.
func (sl *ScrollableList) invalidate() {
	if sl.inv != nil {
		sl.inv(sl.Rect)
	}
}

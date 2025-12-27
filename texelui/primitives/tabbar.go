// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/primitives/tabbar.go
// Summary: Horizontal tab bar widget with keyboard and mouse navigation.

package primitives

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
)

// TabItem represents a single tab in a TabBar.
type TabItem struct {
	Label string
	ID    string // Optional identifier for the tab
}

// TabBar is a horizontal tab navigation widget.
// It displays tabs and handles keyboard (Left/Right, number keys) and mouse navigation.
type TabBar struct {
	core.BaseWidget
	Tabs      []TabItem
	ActiveIdx int
	OnChange  func(int) // Called when active tab changes

	// Styling
	ShowFocusMarker bool // Show '►' marker when focused (default true)

	inv func(core.Rect)
}

// NewTabBar creates a new tab bar at the specified position.
// Width determines the available space for tabs. Height is always 1.
func NewTabBar(x, y, w int, tabs []TabItem) *TabBar {
	tb := &TabBar{
		Tabs:            tabs,
		ActiveIdx:       0,
		ShowFocusMarker: true,
	}

	tb.SetPosition(x, y)
	tb.Resize(w, 1)
	tb.SetFocusable(true)

	// Configure focus style from theme
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	tb.SetFocusedStyle(tcell.StyleDefault.Foreground(fg).Background(bg), true)

	return tb
}

// SetInvalidator allows the UI manager to inject a dirty-region invalidator.
func (tb *TabBar) SetInvalidator(fn func(core.Rect)) {
	tb.inv = fn
}

// SetActive changes the active tab by index.
func (tb *TabBar) SetActive(idx int) {
	if idx < 0 || idx >= len(tb.Tabs) {
		return
	}
	if idx == tb.ActiveIdx {
		return
	}
	tb.ActiveIdx = idx
	tb.invalidate()
	if tb.OnChange != nil {
		tb.OnChange(idx)
	}
}

// ActiveTab returns the currently active tab item.
func (tb *TabBar) ActiveTab() TabItem {
	if tb.ActiveIdx >= 0 && tb.ActiveIdx < len(tb.Tabs) {
		return tb.Tabs[tb.ActiveIdx]
	}
	return TabItem{}
}

// Draw renders the tab bar.
func (tb *TabBar) Draw(painter *core.Painter) {
	if len(tb.Tabs) == 0 {
		return
	}

	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	baseStyle := tcell.StyleDefault.Foreground(fg).Background(bg)

	focused := tb.IsFocused()
	x := tb.Rect.X
	y := tb.Rect.Y

	// Draw focus marker if focused and enabled
	if focused && tb.ShowFocusMarker {
		painter.SetCell(x, y, '►', baseStyle.Bold(true))
		x++
	}

	// Draw each tab
	for i, tab := range tb.Tabs {
		tabLabel := " " + tab.Label + " "
		isActive := i == tb.ActiveIdx

		tabStyle := baseStyle
		if isActive {
			if focused {
				// Active tab with focus: bold + reverse
				tabStyle = tabStyle.Reverse(true).Bold(true)
			} else {
				// Active tab without focus: just reverse
				tabStyle = tabStyle.Reverse(true)
			}
		} else if focused {
			// Inactive tabs when focused: dim
			tabStyle = tabStyle.Dim(true)
		}

		// Draw tab label
		for _, ch := range tabLabel {
			if x >= tb.Rect.X+tb.Rect.W {
				break
			}
			painter.SetCell(x, y, ch, tabStyle)
			x++
		}

		// Add spacing between tabs (unless at end)
		if i < len(tb.Tabs)-1 && x < tb.Rect.X+tb.Rect.W {
			painter.SetCell(x, y, ' ', baseStyle)
			x++
		}
	}
}

// HandleKey processes keyboard input for tab navigation.
// Returns true if the key was handled.
func (tb *TabBar) HandleKey(ev *tcell.EventKey) bool {
	if len(tb.Tabs) == 0 {
		return false
	}

	switch ev.Key() {
	case tcell.KeyLeft:
		if tb.ActiveIdx > 0 {
			tb.SetActive(tb.ActiveIdx - 1)
			return true
		}
		return false

	case tcell.KeyRight:
		if tb.ActiveIdx < len(tb.Tabs)-1 {
			tb.SetActive(tb.ActiveIdx + 1)
			return true
		}
		return false

	case tcell.KeyHome:
		if tb.ActiveIdx != 0 {
			tb.SetActive(0)
			return true
		}
		return false

	case tcell.KeyEnd:
		lastIdx := len(tb.Tabs) - 1
		if tb.ActiveIdx != lastIdx {
			tb.SetActive(lastIdx)
			return true
		}
		return false

	case tcell.KeyRune:
		// Number keys 1-9 for direct tab selection
		r := ev.Rune()
		if r >= '1' && r <= '9' {
			idx := int(r - '1')
			if idx < len(tb.Tabs) && idx != tb.ActiveIdx {
				tb.SetActive(idx)
				return true
			}
		}
		return false
	}

	return false
}

// HandleMouse processes mouse input for tab selection.
func (tb *TabBar) HandleMouse(ev *tcell.EventMouse) bool {
	if len(tb.Tabs) == 0 {
		return false
	}

	x, y := ev.Position()
	if !tb.HitTest(x, y) {
		return false
	}

	if ev.Buttons() != tcell.Button1 {
		return false
	}

	// Calculate which tab was clicked
	tabX := tb.Rect.X
	focused := tb.IsFocused()

	// Account for focus marker
	if focused && tb.ShowFocusMarker {
		tabX++
	}

	for i, tab := range tb.Tabs {
		tabLabel := " " + tab.Label + " "
		tabWidth := len(tabLabel)

		if x >= tabX && x < tabX+tabWidth {
			if i != tb.ActiveIdx {
				tb.SetActive(i)
			}
			return true
		}

		tabX += tabWidth + 1 // +1 for spacing
	}

	return true
}

// invalidate marks the widget as needing redraw.
func (tb *TabBar) invalidate() {
	if tb.inv != nil {
		tb.inv(tb.Rect)
	}
}

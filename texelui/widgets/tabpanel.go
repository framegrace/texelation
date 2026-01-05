// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/widgets/tabpanel.go
// Summary: High-level tab container with simple AddTab API.

package widgets

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texelui/core"
	"texelation/texelui/primitives"
)

// TabPanel is a high-level tab container that combines a tab bar with switchable
// content panels. Unlike TabLayout which requires pre-defining tabs, TabPanel
// lets you add tabs dynamically with a simple API:
//
//	panel := widgets.NewTabPanel(0, 0, 80, 24)
//	panel.AddTab("General", generalPane)
//	panel.AddTab("Advanced", advancedPane)
type TabPanel struct {
	*TabLayout
	tabs []primitives.TabItem
}

// NewTabPanel creates a new empty tab panel.
// Position and size default to 0,0,1,1 and should be set via SetPosition/Resize
// when the panel is added to a layout.
func NewTabPanel() *TabPanel {
	tp := &TabPanel{
		TabLayout: NewTabLayout(0, 0, 1, 1, nil),
		tabs:      make([]primitives.TabItem, 0),
	}
	return tp
}

// AddTab adds a new tab with the given name and content widget.
// Returns the index of the added tab.
func (tp *TabPanel) AddTab(name string, content core.Widget) int {
	idx := len(tp.tabs)
	tp.tabs = append(tp.tabs, primitives.TabItem{Label: name, ID: name})

	// Rebuild the tab bar with new tabs
	tp.rebuildTabBar()

	// Set the content for the new tab
	tp.TabLayout.SetTabContent(idx, content)

	return idx
}

// AddTabWithID adds a new tab with the given name, ID and content widget.
// Returns the index of the added tab.
func (tp *TabPanel) AddTabWithID(name, id string, content core.Widget) int {
	idx := len(tp.tabs)
	tp.tabs = append(tp.tabs, primitives.TabItem{Label: name, ID: id})

	// Rebuild the tab bar with new tabs
	tp.rebuildTabBar()

	// Set the content for the new tab
	tp.TabLayout.SetTabContent(idx, content)

	return idx
}

// TabCount returns the number of tabs.
func (tp *TabPanel) TabCount() int {
	return len(tp.tabs)
}

// RemoveTab removes the tab at the given index.
func (tp *TabPanel) RemoveTab(idx int) {
	if idx < 0 || idx >= len(tp.tabs) {
		return
	}

	// Remove from tabs slice
	tp.tabs = append(tp.tabs[:idx], tp.tabs[idx+1:]...)

	// Rebuild the tab bar
	tp.rebuildTabBar()
}

// ClearTabs removes all tabs.
func (tp *TabPanel) ClearTabs() {
	tp.tabs = tp.tabs[:0]
	tp.rebuildTabBar()
}

// rebuildTabBar recreates the internal TabLayout with current tabs.
func (tp *TabPanel) rebuildTabBar() {
	// Save current state
	activeIdx := tp.TabLayout.ActiveIndex()
	inv := tp.TabLayout.inv
	trapsFocus := tp.TabLayout.TrapsFocus()
	rect := tp.TabLayout.Rect

	// Collect existing content widgets
	oldChildren := tp.TabLayout.children

	// Create new TabLayout with updated tabs
	tp.TabLayout = NewTabLayout(rect.X, rect.Y, rect.W, rect.H, tp.tabs)
	tp.TabLayout.SetTrapsFocus(trapsFocus)
	if inv != nil {
		tp.TabLayout.SetInvalidator(inv)
	}

	// Re-assign content widgets that still exist
	for i := 0; i < len(tp.tabs) && i < len(oldChildren); i++ {
		if oldChildren[i] != nil {
			tp.TabLayout.SetTabContent(i, oldChildren[i])
		}
	}

	// Restore active index if still valid
	if activeIdx >= 0 && activeIdx < len(tp.tabs) {
		tp.TabLayout.SetActive(activeIdx)
	} else if len(tp.tabs) > 0 {
		tp.TabLayout.SetActive(0)
	}
}

// SetTabContent updates the content widget for a specific tab.
// This is a convenience method that delegates to the underlying TabLayout.
func (tp *TabPanel) SetTabContent(idx int, content core.Widget) {
	tp.TabLayout.SetTabContent(idx, content)
}

// OnTabChange sets a callback that's called when the active tab changes.
func (tp *TabPanel) OnTabChange(fn func(idx int)) {
	tp.TabLayout.tabBar.OnChange = func(idx int) {
		tp.TabLayout.invalidate()
		if fn != nil {
			fn(idx)
		}
	}
}

// HandleKey processes keyboard input.
func (tp *TabPanel) HandleKey(ev *tcell.EventKey) bool {
	return tp.TabLayout.HandleKey(ev)
}

// HandleMouse processes mouse input.
func (tp *TabPanel) HandleMouse(ev *tcell.EventMouse) bool {
	return tp.TabLayout.HandleMouse(ev)
}

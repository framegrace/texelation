// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/widgets/tablayout.go
// Summary: Tab layout container combining TabBar with switchable content panels.

package widgets

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
	"texelation/texelui/primitives"
)

// TabLayout is a container that combines a TabBar with switchable content panels.
// Each tab has an associated content widget; only the active tab's content is displayed.
type TabLayout struct {
	core.BaseWidget
	tabBar   *primitives.TabBar
	children []core.Widget // One widget per tab
	inv      func(core.Rect)

	// focusArea tracks which part has focus: 0 = tabBar, 1 = content
	focusArea int
}

// NewTabLayout creates a new tab layout at the specified position.
func NewTabLayout(x, y, w, h int, tabs []primitives.TabItem) *TabLayout {
	tl := &TabLayout{
		tabBar:   primitives.NewTabBar(x, y, w, tabs),
		children: make([]core.Widget, len(tabs)),
	}

	tl.SetPosition(x, y)
	tl.Resize(w, h)
	tl.SetFocusable(true)

	// Wire up tab change to trigger redraw
	tl.tabBar.OnChange = func(idx int) {
		tl.invalidate()
	}

	return tl
}

// SetTabContent sets the content widget for a specific tab index.
func (tl *TabLayout) SetTabContent(idx int, w core.Widget) {
	if idx < 0 || idx >= len(tl.children) {
		return
	}
	tl.children[idx] = w
	if w != nil {
		// Position in content area
		cr := tl.contentRect()
		w.SetPosition(cr.X, cr.Y)
		w.Resize(cr.W, cr.H)
		// Propagate invalidator
		if ia, ok := w.(core.InvalidationAware); ok && tl.inv != nil {
			ia.SetInvalidator(tl.inv)
		}
	}
}

// ActiveIndex returns the currently active tab index.
func (tl *TabLayout) ActiveIndex() int {
	return tl.tabBar.ActiveIdx
}

// SetActive changes the active tab.
func (tl *TabLayout) SetActive(idx int) {
	tl.tabBar.SetActive(idx)
}

// contentRect returns the rectangle for the content area (below the tab bar).
func (tl *TabLayout) contentRect() core.Rect {
	return core.Rect{
		X: tl.Rect.X,
		Y: tl.Rect.Y + 1, // Tab bar is 1 row
		W: tl.Rect.W,
		H: tl.Rect.H - 1,
	}
}

// Resize updates the layout dimensions and repositions children.
func (tl *TabLayout) Resize(w, h int) {
	tl.BaseWidget.Resize(w, h)

	// Resize tab bar (always 1 row at top)
	tl.tabBar.SetPosition(tl.Rect.X, tl.Rect.Y)
	tl.tabBar.Resize(w, 1)

	// Resize all children to content area
	cr := tl.contentRect()
	for _, child := range tl.children {
		if child != nil {
			child.SetPosition(cr.X, cr.Y)
			child.Resize(cr.W, cr.H)
		}
	}
}

// SetPosition updates the layout position.
func (tl *TabLayout) SetPosition(x, y int) {
	tl.BaseWidget.SetPosition(x, y)
	tl.tabBar.SetPosition(x, y)

	cr := tl.contentRect()
	for _, child := range tl.children {
		if child != nil {
			child.SetPosition(cr.X, cr.Y)
		}
	}
}

// Draw renders the tab bar and active content.
func (tl *TabLayout) Draw(p *core.Painter) {
	tm := theme.Get()
	bg := tm.GetSemanticColor("bg.surface")
	baseStyle := tcell.StyleDefault.Background(bg)

	// Fill background
	p.Fill(tl.Rect, ' ', baseStyle)

	// Draw tab bar
	tl.tabBar.Draw(p)

	// Draw active content
	activeIdx := tl.tabBar.ActiveIdx
	if activeIdx >= 0 && activeIdx < len(tl.children) && tl.children[activeIdx] != nil {
		tl.children[activeIdx].Draw(p)
	}
}

// HandleKey processes keyboard input.
func (tl *TabLayout) HandleKey(ev *tcell.EventKey) bool {
	// Handle Tab/Shift-Tab for focus switching between tabBar and content
	if ev.Key() == tcell.KeyTab {
		if ev.Modifiers()&tcell.ModShift != 0 {
			// Shift-Tab: content -> tabBar -> parent
			if tl.focusArea == 1 {
				// Try to move focus backward within content first
				activeChild := tl.activeChild()
				if activeChild != nil {
					if activeChild.HandleKey(ev) {
						return true
					}
				}
				// Content didn't handle it, move to tab bar
				tl.focusArea = 0
				tl.tabBar.Focus()
				if activeChild != nil {
					activeChild.Blur()
				}
				tl.invalidate()
				return true
			}
			// Focus is on tab bar, let parent handle
			return false
		} else {
			// Tab: tabBar -> content -> parent
			if tl.focusArea == 0 {
				// Move from tab bar to content
				activeChild := tl.activeChild()
				if activeChild != nil && activeChild.Focusable() {
					tl.focusArea = 1
					tl.tabBar.Blur()
					activeChild.Focus()
					tl.invalidate()
					return true
				}
				// No focusable content, let parent handle
				return false
			}
			// Focus is on content, try to move within content
			activeChild := tl.activeChild()
			if activeChild != nil {
				if activeChild.HandleKey(ev) {
					return true
				}
			}
			// Content exhausted, let parent handle
			return false
		}
	}

	// Route other keys based on focus area
	if tl.focusArea == 0 {
		return tl.tabBar.HandleKey(ev)
	}

	activeChild := tl.activeChild()
	if activeChild != nil {
		return activeChild.HandleKey(ev)
	}
	return false
}

// HandleMouse processes mouse input.
func (tl *TabLayout) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !tl.HitTest(x, y) {
		return false
	}

	// Check if click is on tab bar
	if tl.tabBar.HitTest(x, y) {
		// Focus tab bar
		if tl.focusArea != 0 {
			if activeChild := tl.activeChild(); activeChild != nil {
				activeChild.Blur()
			}
			tl.focusArea = 0
			tl.tabBar.Focus()
			tl.invalidate()
		}
		return tl.tabBar.HandleMouse(ev)
	}

	// Click is on content area
	activeChild := tl.activeChild()
	if activeChild != nil {
		// Focus content area
		if tl.focusArea != 1 {
			tl.tabBar.Blur()
			tl.focusArea = 1
			activeChild.Focus()
			tl.invalidate()
		}

		// Check if child handles mouse
		if ma, ok := activeChild.(core.MouseAware); ok {
			return ma.HandleMouse(ev)
		}
	}

	return true
}

// Focus sets focus on this layout.
func (tl *TabLayout) Focus() {
	tl.BaseWidget.Focus()
	// Start with tab bar focused
	tl.focusArea = 0
	tl.tabBar.Focus()
}

// Blur removes focus from this layout.
func (tl *TabLayout) Blur() {
	tl.BaseWidget.Blur()
	tl.tabBar.Blur()
	if activeChild := tl.activeChild(); activeChild != nil {
		activeChild.Blur()
	}
}

// SetInvalidator sets the invalidation callback.
func (tl *TabLayout) SetInvalidator(fn func(core.Rect)) {
	tl.inv = fn
	tl.tabBar.SetInvalidator(fn)
	// Propagate to all children
	for _, child := range tl.children {
		if child != nil {
			if ia, ok := child.(core.InvalidationAware); ok {
				ia.SetInvalidator(fn)
			}
		}
	}
}

// VisitChildren implements core.ChildContainer.
func (tl *TabLayout) VisitChildren(f func(core.Widget)) {
	// Visit tab bar (it's focusable)
	f(tl.tabBar)
	// Visit active child only
	if activeChild := tl.activeChild(); activeChild != nil {
		f(activeChild)
	}
}

// WidgetAt implements core.HitTester for deep focus traversal.
func (tl *TabLayout) WidgetAt(x, y int) core.Widget {
	if !tl.HitTest(x, y) {
		return nil
	}

	// Check tab bar
	if tl.tabBar.HitTest(x, y) {
		return tl.tabBar
	}

	// Check active content
	activeChild := tl.activeChild()
	if activeChild != nil && activeChild.HitTest(x, y) {
		if ht, ok := activeChild.(core.HitTester); ok {
			if dw := ht.WidgetAt(x, y); dw != nil {
				return dw
			}
		}
		return activeChild
	}

	return tl
}

// activeChild returns the currently active tab's content widget.
func (tl *TabLayout) activeChild() core.Widget {
	idx := tl.tabBar.ActiveIdx
	if idx >= 0 && idx < len(tl.children) {
		return tl.children[idx]
	}
	return nil
}

// invalidate marks the widget as needing redraw.
func (tl *TabLayout) invalidate() {
	if tl.inv != nil {
		tl.inv(tl.Rect)
	}
}

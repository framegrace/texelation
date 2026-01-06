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
	// trapsFocus: if true, wraps focus at boundaries instead of returning false
	trapsFocus bool
}

// NewTabLayout creates a new tab layout with the specified tabs.
// Position defaults to 0,0 and size to 1x1.
// Use SetPosition and Resize to adjust after adding to a layout.
func NewTabLayout(tabs []primitives.TabItem) *TabLayout {
	tl := &TabLayout{
		tabBar:   primitives.NewTabBar(0, 0, 1, tabs),
		children: make([]core.Widget, len(tabs)),
	}

	tl.SetPosition(0, 0)
	tl.Resize(1, 1)
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

// SetTrapsFocus sets whether this TabLayout wraps focus at boundaries.
// Set to true for root containers that should cycle focus internally.
func (tl *TabLayout) SetTrapsFocus(trap bool) {
	tl.trapsFocus = trap
}

// TrapsFocus returns whether this TabLayout wraps focus at boundaries.
func (tl *TabLayout) TrapsFocus() bool {
	return tl.trapsFocus
}

// CycleFocus moves focus to next (forward=true) or previous (forward=false) element.
// The internal order is: tabBar -> content widgets -> tabBar (internal cycling).
// trapsFocus controls whether we exit at the edges (Shift-Tab from tab bar exits if false).
// Returns true if focus was successfully cycled, false if at boundary and should exit.
func (tl *TabLayout) CycleFocus(forward bool) bool {
	activeChild := tl.activeChild()

	if forward {
		if tl.focusArea == 0 {
			// From tab bar -> first focusable in content
			if activeChild != nil {
				first := tl.findFirstFocusable(activeChild)
				if first != nil {
					tl.tabBar.Blur()
					tl.focusArea = 1
					first.Focus()
					tl.invalidate()
					return true
				}
			}
			// No focusable content - exit or stay
			if tl.trapsFocus {
				return true // Stay on tab bar
			}
			return false // Exit TabLayout
		} else {
			// In content area - try cycling within content first
			if activeChild != nil {
				if fc, ok := activeChild.(core.FocusCycler); ok {
					if fc.CycleFocus(true) {
						return true
					}
				}
			}
			// Content exhausted - ALWAYS wrap to tab bar (internal cycling)
			tl.blurContentFocus()
			tl.focusArea = 0
			tl.tabBar.Focus()
			tl.invalidate()
			return true
		}
	} else {
		// Backward (Shift-Tab)
		if tl.focusArea == 1 {
			// In content area - try cycling backward within content first
			if activeChild != nil {
				if fc, ok := activeChild.(core.FocusCycler); ok {
					if fc.CycleFocus(false) {
						return true
					}
				}
			}
			// Content exhausted at beginning - go to tab bar
			tl.blurContentFocus()
			tl.focusArea = 0
			tl.tabBar.Focus()
			tl.invalidate()
			return true
		} else {
			// On tab bar - exit or wrap to last content
			if tl.trapsFocus {
				if activeChild != nil {
					last := tl.findLastFocusable(activeChild)
					if last != nil {
						tl.tabBar.Blur()
						tl.focusArea = 1
						last.Focus()
						tl.invalidate()
						return true
					}
				}
				return true // No content - stay on tab bar
			}
			return false // Exit TabLayout
		}
	}
}

// findLastFocusable recursively finds the last focusable widget.
func (tl *TabLayout) findLastFocusable(w core.Widget) core.Widget {
	// Check children first (in reverse order)
	if cc, ok := w.(core.ChildContainer); ok {
		var children []core.Widget
		cc.VisitChildren(func(child core.Widget) {
			children = append(children, child)
		})
		// Iterate in reverse
		for i := len(children) - 1; i >= 0; i-- {
			last := tl.findLastFocusable(children[i])
			if last != nil {
				return last
			}
		}
	}
	// No focusable children, check this widget
	if w.Focusable() {
		return w
	}
	return nil
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
	// Handle Tab/Shift-Tab for focus cycling
	if ev.Key() == tcell.KeyTab || ev.Key() == tcell.KeyBacktab {
		forward := ev.Key() == tcell.KeyTab && ev.Modifiers()&tcell.ModShift == 0

		// If in content area, let content try to handle Tab first
		if tl.focusArea == 1 {
			activeChild := tl.activeChild()
			if activeChild != nil {
				if activeChild.HandleKey(ev) {
					return true
				}
			}
		}

		// Content didn't handle it (or we're on tab bar) - cycle focus
		return tl.CycleFocus(forward)
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
		// Focus tab bar (always ensure it's focused - may have been blurred by parent)
		if tl.focusArea != 0 {
			tl.blurContentFocus()
			tl.focusArea = 0
		}
		if !tl.tabBar.IsFocused() {
			tl.tabBar.Focus()
			tl.invalidate()
		}
		return tl.tabBar.HandleMouse(ev)
	}

	// Mouse not on tab bar - clear hover
	tl.tabBar.ClearHover()

	// Click is on content area
	activeChild := tl.activeChild()
	if activeChild != nil {
		// Focus content area
		if tl.focusArea != 1 {
			tl.tabBar.Blur()
			tl.focusArea = 1
			// Focus first widget in content
			first := tl.findFirstFocusable(activeChild)
			if first != nil {
				first.Focus()
			}
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
// Focus starts at the tab bar, allowing users to Tab into content.
func (tl *TabLayout) Focus() {
	tl.BaseWidget.Focus()

	// Restore focus to previous area, or default to tab bar
	if tl.focusArea == 1 {
		// Was in content area - try to restore focus there
		activeChild := tl.activeChild()
		if activeChild != nil {
			first := tl.findFirstFocusable(activeChild)
			if first != nil {
				tl.tabBar.Blur()
				first.Focus()
				tl.invalidate()
				return
			}
		}
	}

	// Default: focus tab bar
	tl.focusArea = 0
	tl.tabBar.Focus()
	tl.invalidate()
}

// findFirstFocusable recursively finds the first focusable widget.
func (tl *TabLayout) findFirstFocusable(w core.Widget) core.Widget {
	if w.Focusable() {
		// Check if it's a container - if so, look inside first
		if cc, ok := w.(core.ChildContainer); ok {
			var first core.Widget
			cc.VisitChildren(func(child core.Widget) {
				if first != nil {
					return
				}
				first = tl.findFirstFocusable(child)
			})
			if first != nil {
				return first
			}
		}
		// No focusable children, return this widget
		return w
	}
	// Not focusable, check children
	if cc, ok := w.(core.ChildContainer); ok {
		var first core.Widget
		cc.VisitChildren(func(child core.Widget) {
			if first != nil {
				return
			}
			first = tl.findFirstFocusable(child)
		})
		return first
	}
	return nil
}

// findFocusedWidget recursively finds the currently focused widget.
func (tl *TabLayout) findFocusedWidget(w core.Widget) core.Widget {
	if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
		// Check if it has focused children
		if cc, ok := w.(core.ChildContainer); ok {
			var focused core.Widget
			cc.VisitChildren(func(child core.Widget) {
				if focused != nil {
					return
				}
				focused = tl.findFocusedWidget(child)
			})
			if focused != nil {
				return focused
			}
		}
		return w
	}
	// Check children even if parent not focused
	if cc, ok := w.(core.ChildContainer); ok {
		var focused core.Widget
		cc.VisitChildren(func(child core.Widget) {
			if focused != nil {
				return
			}
			focused = tl.findFocusedWidget(child)
		})
		return focused
	}
	return nil
}

// blurContentFocus blurs the currently focused widget in content.
func (tl *TabLayout) blurContentFocus() {
	activeChild := tl.activeChild()
	if activeChild == nil {
		return
	}
	focused := tl.findFocusedWidget(activeChild)
	if focused != nil {
		focused.Blur()
	}
}

// Blur removes focus from this layout.
// Preserves focusArea for restoration when Focus() is called again.
func (tl *TabLayout) Blur() {
	tl.BaseWidget.Blur()
	tl.tabBar.Blur()
	tl.blurContentFocus()
	// Keep focusArea as-is for restoration on next Focus()
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
	// Visit tab bar
	f(tl.tabBar)
	// Visit active child
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

// GetKeyHints implements core.KeyHintsProvider.
// Returns hints based on whether tab bar or content has focus.
func (tl *TabLayout) GetKeyHints() []core.KeyHint {
	if tl.focusArea == 0 {
		// Tab bar focused - includes Tab/S-Tab to suppress focus cycler hints
		// (TabLayout handles these internally for tab bar <-> content navigation)
		return []core.KeyHint{
			{Key: "←→", Label: "Switch"},
			{Key: "1-9", Label: "Jump"},
			{Key: "Tab", Label: "Content"},
			{Key: "S-Tab", Label: "Exit"},
		}
	}
	// Content focused - delegate to content widget if it provides hints
	activeChild := tl.activeChild()
	if activeChild != nil {
		if khp, ok := activeChild.(core.KeyHintsProvider); ok {
			// Get content hints and add S-Tab for returning to tab bar
			hints := khp.GetKeyHints()
			hints = append(hints, core.KeyHint{Key: "S-Tab", Label: "Tab Bar"})
			return hints
		}
	}
	return []core.KeyHint{
		{Key: "S-Tab", Label: "Tab Bar"},
	}
}

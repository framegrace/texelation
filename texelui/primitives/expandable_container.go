// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/primitives/expandable_container.go
// Summary: A container widget that toggles between collapsed and expanded views.
// When collapsed, shows a single-row preview widget. When expanded (via Space),
// becomes modal and overlays other widgets with the expanded content.

package primitives

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texelui/core"
)

// ExpandableContainer is a widget that switches between collapsed and expanded states.
// In collapsed state, it displays a single-row preview widget.
// In expanded state (triggered by Space), it becomes modal and displays the
// expanded content at a configurable size, overlaid on other widgets.
type ExpandableContainer struct {
	core.BaseWidget

	// Child widgets
	collapsedChild core.Widget // Shown when collapsed (typically 1 row)
	expandedChild  core.Widget // Shown when expanded

	// State
	expanded bool

	// Expanded size configuration
	expandedWidth  int
	expandedHeight int

	// Callbacks
	OnExpand   func() // Called when expanding
	OnCollapse func() // Called when collapsing

	inv func(core.Rect)
}

// NewExpandableContainer creates an expandable container at the given position.
// The collapsed size determines the widget bounds when collapsed.
// Expanded size is set separately via SetExpandedSize.
func NewExpandableContainer(x, y, collapsedWidth, collapsedHeight int) *ExpandableContainer {
	ec := &ExpandableContainer{
		expandedWidth:  collapsedWidth,
		expandedHeight: 10, // Default expanded height
	}
	ec.SetPosition(x, y)
	ec.Resize(collapsedWidth, collapsedHeight)
	ec.SetFocusable(true)
	return ec
}

// SetCollapsedChild sets the widget shown in collapsed state.
func (ec *ExpandableContainer) SetCollapsedChild(w core.Widget) {
	ec.collapsedChild = w
	if w != nil {
		w.SetPosition(ec.Rect.X, ec.Rect.Y)
		w.Resize(ec.Rect.W, ec.Rect.H)
		if ia, ok := w.(core.InvalidationAware); ok {
			ia.SetInvalidator(ec.invalidate)
		}
	}
}

// SetExpandedChild sets the widget shown in expanded state.
func (ec *ExpandableContainer) SetExpandedChild(w core.Widget) {
	ec.expandedChild = w
	if w != nil {
		w.SetPosition(ec.Rect.X, ec.Rect.Y)
		w.Resize(ec.expandedWidth, ec.expandedHeight)
		if ia, ok := w.(core.InvalidationAware); ok {
			ia.SetInvalidator(ec.invalidate)
		}
	}
}

// SetExpandedSize sets the dimensions used when expanded.
func (ec *ExpandableContainer) SetExpandedSize(w, h int) {
	ec.expandedWidth = w
	ec.expandedHeight = h
	if ec.expandedChild != nil && ec.expanded {
		ec.expandedChild.Resize(w, h)
	}
}

// SetPosition updates the position and propagates to children.
func (ec *ExpandableContainer) SetPosition(x, y int) {
	ec.BaseWidget.SetPosition(x, y)
	if ec.collapsedChild != nil {
		ec.collapsedChild.SetPosition(x, y)
	}
	if ec.expandedChild != nil {
		ec.expandedChild.SetPosition(x, y)
	}
}

// Resize updates the collapsed size and propagates to children.
func (ec *ExpandableContainer) Resize(w, h int) {
	ec.BaseWidget.Resize(w, h)
	if ec.collapsedChild != nil {
		ec.collapsedChild.Resize(w, h)
	}
	// Expanded child uses its own size, set via SetExpandedSize
}

// IsExpanded returns true if the container is currently expanded.
func (ec *ExpandableContainer) IsExpanded() bool {
	return ec.expanded
}

// Expand transitions to expanded state.
func (ec *ExpandableContainer) Expand() {
	if ec.expanded {
		return
	}
	ec.expanded = true
	ec.SetZIndex(100) // Overlay other widgets

	// Position and size the expanded child
	if ec.expandedChild != nil {
		ec.expandedChild.SetPosition(ec.Rect.X, ec.Rect.Y)
		ec.expandedChild.Resize(ec.expandedWidth, ec.expandedHeight)
		if ec.expandedChild.Focusable() {
			ec.expandedChild.Focus()
		}
	}

	if ec.OnExpand != nil {
		ec.OnExpand()
	}
	ec.invalidate(ec.expandedRect())
}

// Collapse transitions to collapsed state.
func (ec *ExpandableContainer) Collapse() {
	if !ec.expanded {
		return
	}

	// Invalidate old expanded area before collapsing
	oldRect := ec.expandedRect()

	ec.expanded = false
	ec.SetZIndex(0)

	// Blur expanded child, focus collapsed child
	if ec.expandedChild != nil {
		ec.expandedChild.Blur()
	}
	if ec.collapsedChild != nil && ec.collapsedChild.Focusable() {
		ec.collapsedChild.Focus()
	}

	if ec.OnCollapse != nil {
		ec.OnCollapse()
	}
	ec.invalidate(oldRect)
}

// Toggle switches between expanded and collapsed states.
func (ec *ExpandableContainer) Toggle() {
	if ec.expanded {
		ec.Collapse()
	} else {
		ec.Expand()
	}
}

// expandedRect returns the rect covering the expanded area.
func (ec *ExpandableContainer) expandedRect() core.Rect {
	return core.Rect{
		X: ec.Rect.X,
		Y: ec.Rect.Y,
		W: ec.expandedWidth,
		H: ec.expandedHeight,
	}
}

// ZIndex implements core.ZIndexer.
// Returns 100 when expanded (overlay), 0 when collapsed.
func (ec *ExpandableContainer) ZIndex() int {
	if ec.expanded {
		return 100
	}
	return ec.BaseWidget.ZIndex()
}

// IsModal implements core.Modal.
// Returns true when expanded to capture all input.
func (ec *ExpandableContainer) IsModal() bool {
	return ec.expanded
}

// DismissModal implements core.Modal.
// Collapses the container when clicking outside.
func (ec *ExpandableContainer) DismissModal() {
	ec.Collapse()
}

// Draw renders either the collapsed or expanded child.
func (ec *ExpandableContainer) Draw(p *core.Painter) {
	if ec.expanded {
		if ec.expandedChild != nil {
			ec.expandedChild.Draw(p)
		}
	} else {
		if ec.collapsedChild != nil {
			ec.collapsedChild.Draw(p)
		}
	}
}

// Focus focuses the container and the active child.
func (ec *ExpandableContainer) Focus() {
	ec.BaseWidget.Focus()
	activeChild := ec.activeChild()
	if activeChild != nil && activeChild.Focusable() {
		activeChild.Focus()
	}
}

// Blur blurs the container and the active child.
func (ec *ExpandableContainer) Blur() {
	ec.BaseWidget.Blur()
	if ec.collapsedChild != nil {
		ec.collapsedChild.Blur()
	}
	if ec.expandedChild != nil {
		ec.expandedChild.Blur()
	}
}

// HandleKey processes keyboard input.
// Space toggles expanded state. Other keys go to the active child.
func (ec *ExpandableContainer) HandleKey(ev *tcell.EventKey) bool {
	// Space toggles the expanded state (but only when collapsed, or to collapse)
	if ev.Key() == tcell.KeyRune && ev.Rune() == ' ' {
		// If collapsed, expand
		if !ec.expanded {
			ec.Expand()
			return true
		}
		// If expanded, let child handle space (e.g., for selection)
		// Child can call Collapse() if it wants to close
	}

	// Escape always collapses
	if ev.Key() == tcell.KeyEscape && ec.expanded {
		ec.Collapse()
		return true
	}

	// Route to active child
	activeChild := ec.activeChild()
	if activeChild != nil {
		return activeChild.HandleKey(ev)
	}
	return false
}

// HandleMouse processes mouse input.
func (ec *ExpandableContainer) HandleMouse(ev *tcell.EventMouse) bool {
	activeChild := ec.activeChild()
	if activeChild == nil {
		return false
	}

	// Check if click is within the active child's bounds
	x, y := ev.Position()
	if ec.expanded {
		rect := ec.expandedRect()
		if !rect.Contains(x, y) {
			return false
		}
	} else {
		if !ec.HitTest(x, y) {
			return false
		}
	}

	// Route to active child if it handles mouse
	if ma, ok := activeChild.(core.MouseAware); ok {
		return ma.HandleMouse(ev)
	}
	return true
}

// HitTest returns true if the point is within the widget's current bounds.
func (ec *ExpandableContainer) HitTest(x, y int) bool {
	if ec.expanded {
		return ec.expandedRect().Contains(x, y)
	}
	return ec.Rect.Contains(x, y)
}

// Size returns the current size (collapsed or expanded).
func (ec *ExpandableContainer) Size() (int, int) {
	if ec.expanded {
		return ec.expandedWidth, ec.expandedHeight
	}
	return ec.Rect.W, ec.Rect.H
}

// VisitChildren implements core.ChildContainer.
func (ec *ExpandableContainer) VisitChildren(f func(core.Widget)) {
	if ec.collapsedChild != nil {
		f(ec.collapsedChild)
	}
	if ec.expandedChild != nil {
		f(ec.expandedChild)
	}
}

// WidgetAt implements core.HitTester.
func (ec *ExpandableContainer) WidgetAt(x, y int) core.Widget {
	activeChild := ec.activeChild()
	if activeChild == nil {
		if ec.HitTest(x, y) {
			return ec
		}
		return nil
	}

	// Check if point is within active child
	if ht, ok := activeChild.(core.HitTester); ok {
		if w := ht.WidgetAt(x, y); w != nil {
			return w
		}
	}
	if activeChild.HitTest(x, y) {
		return activeChild
	}
	if ec.HitTest(x, y) {
		return ec
	}
	return nil
}

// CycleFocus implements core.FocusCycler.
// When expanded, delegates to expanded child if it's a FocusCycler.
// When collapsed, returns false (no internal cycling).
func (ec *ExpandableContainer) CycleFocus(forward bool) bool {
	if !ec.expanded {
		return false
	}
	if fc, ok := ec.expandedChild.(core.FocusCycler); ok {
		return fc.CycleFocus(forward)
	}
	return false
}

// TrapsFocus implements core.FocusCycler.
// Returns true when expanded (modal traps focus).
func (ec *ExpandableContainer) TrapsFocus() bool {
	return ec.expanded
}

// SetInvalidator sets the invalidation callback.
func (ec *ExpandableContainer) SetInvalidator(fn func(core.Rect)) {
	ec.inv = fn
	if ec.collapsedChild != nil {
		if ia, ok := ec.collapsedChild.(core.InvalidationAware); ok {
			ia.SetInvalidator(fn)
		}
	}
	if ec.expandedChild != nil {
		if ia, ok := ec.expandedChild.(core.InvalidationAware); ok {
			ia.SetInvalidator(fn)
		}
	}
}

// invalidate marks a region as needing redraw.
func (ec *ExpandableContainer) invalidate(r core.Rect) {
	if ec.inv != nil {
		ec.inv(r)
	}
}

// activeChild returns the currently active child widget.
func (ec *ExpandableContainer) activeChild() core.Widget {
	if ec.expanded {
		return ec.expandedChild
	}
	return ec.collapsedChild
}

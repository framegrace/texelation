// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package primitives

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"texelation/texelui/core"
)

// mockWidget is a simple widget for testing.
type mockWidget struct {
	core.BaseWidget
	drawCalled    bool
	keyHandled    bool
	lastKey       tcell.Key
	lastRune      rune
	inv           func(core.Rect)
}

func newMockWidget(x, y, w, h int, focusable bool) *mockWidget {
	m := &mockWidget{}
	m.SetPosition(x, y)
	m.Resize(w, h)
	m.SetFocusable(focusable)
	return m
}

func (m *mockWidget) Draw(p *core.Painter) {
	m.drawCalled = true
}

func (m *mockWidget) HandleKey(ev *tcell.EventKey) bool {
	m.lastKey = ev.Key()
	m.lastRune = ev.Rune()
	m.keyHandled = true
	return true
}

func (m *mockWidget) SetInvalidator(fn func(core.Rect)) {
	m.inv = fn
}

func TestExpandableContainer_NewCreatesCollapsedState(t *testing.T) {
	ec := NewExpandableContainer(10, 20, 30, 1)

	if ec.IsExpanded() {
		t.Error("new container should be collapsed")
	}

	x, y := ec.Position()
	if x != 10 || y != 20 {
		t.Errorf("position = (%d, %d), want (10, 20)", x, y)
	}

	w, h := ec.Size()
	if w != 30 || h != 1 {
		t.Errorf("size = (%d, %d), want (30, 1)", w, h)
	}

	if ec.ZIndex() != 0 {
		t.Errorf("collapsed ZIndex = %d, want 0", ec.ZIndex())
	}

	if ec.IsModal() {
		t.Error("collapsed container should not be modal")
	}
}

func TestExpandableContainer_ExpandAndCollapse(t *testing.T) {
	ec := NewExpandableContainer(0, 0, 20, 1)
	ec.SetExpandedSize(20, 10)

	expandCalled := false
	collapseCalled := false
	ec.OnExpand = func() { expandCalled = true }
	ec.OnCollapse = func() { collapseCalled = true }

	// Expand
	ec.Expand()
	if !ec.IsExpanded() {
		t.Error("should be expanded after Expand()")
	}
	if !ec.IsModal() {
		t.Error("expanded container should be modal")
	}
	if ec.ZIndex() != 100 {
		t.Errorf("expanded ZIndex = %d, want 100", ec.ZIndex())
	}
	if !expandCalled {
		t.Error("OnExpand callback not called")
	}

	// Size should be expanded size
	w, h := ec.Size()
	if w != 20 || h != 10 {
		t.Errorf("expanded size = (%d, %d), want (20, 10)", w, h)
	}

	// Collapse
	ec.Collapse()
	if ec.IsExpanded() {
		t.Error("should be collapsed after Collapse()")
	}
	if ec.IsModal() {
		t.Error("collapsed container should not be modal")
	}
	if ec.ZIndex() != 0 {
		t.Errorf("collapsed ZIndex = %d, want 0", ec.ZIndex())
	}
	if !collapseCalled {
		t.Error("OnCollapse callback not called")
	}

	// Size should be collapsed size
	w, h = ec.Size()
	if w != 20 || h != 1 {
		t.Errorf("collapsed size = (%d, %d), want (20, 1)", w, h)
	}
}

func TestExpandableContainer_Toggle(t *testing.T) {
	ec := NewExpandableContainer(0, 0, 20, 1)

	ec.Toggle()
	if !ec.IsExpanded() {
		t.Error("should be expanded after first toggle")
	}

	ec.Toggle()
	if ec.IsExpanded() {
		t.Error("should be collapsed after second toggle")
	}
}

func TestExpandableContainer_DrawsCorrectChild(t *testing.T) {
	ec := NewExpandableContainer(0, 0, 20, 1)
	ec.SetExpandedSize(20, 10)

	collapsed := newMockWidget(0, 0, 20, 1, true)
	expanded := newMockWidget(0, 0, 20, 10, true)
	ec.SetCollapsedChild(collapsed)
	ec.SetExpandedChild(expanded)

	// Draw when collapsed - should draw collapsed child
	collapsed.drawCalled = false
	expanded.drawCalled = false
	ec.Draw(nil)
	if !collapsed.drawCalled {
		t.Error("collapsed child should be drawn when collapsed")
	}
	if expanded.drawCalled {
		t.Error("expanded child should not be drawn when collapsed")
	}

	// Expand and draw - should draw expanded child
	ec.Expand()
	collapsed.drawCalled = false
	expanded.drawCalled = false
	ec.Draw(nil)
	if collapsed.drawCalled {
		t.Error("collapsed child should not be drawn when expanded")
	}
	if !expanded.drawCalled {
		t.Error("expanded child should be drawn when expanded")
	}
}

func TestExpandableContainer_SpaceExpandsWhenCollapsed(t *testing.T) {
	ec := NewExpandableContainer(0, 0, 20, 1)
	ec.SetExpandedSize(20, 10)

	collapsed := newMockWidget(0, 0, 20, 1, true)
	expanded := newMockWidget(0, 0, 20, 10, true)
	ec.SetCollapsedChild(collapsed)
	ec.SetExpandedChild(expanded)

	// Space when collapsed should expand
	ev := tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)
	handled := ec.HandleKey(ev)

	if !handled {
		t.Error("space key should be handled when collapsed")
	}
	if !ec.IsExpanded() {
		t.Error("container should be expanded after space")
	}
}

func TestExpandableContainer_EscapeCollapsesWhenExpanded(t *testing.T) {
	ec := NewExpandableContainer(0, 0, 20, 1)
	ec.SetExpandedSize(20, 10)

	expanded := newMockWidget(0, 0, 20, 10, true)
	ec.SetExpandedChild(expanded)
	ec.Expand()

	// Escape when expanded should collapse
	ev := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	handled := ec.HandleKey(ev)

	if !handled {
		t.Error("escape key should be handled when expanded")
	}
	if ec.IsExpanded() {
		t.Error("container should be collapsed after escape")
	}
}

func TestExpandableContainer_KeysRouteToActiveChild(t *testing.T) {
	ec := NewExpandableContainer(0, 0, 20, 1)
	ec.SetExpandedSize(20, 10)

	collapsed := newMockWidget(0, 0, 20, 1, true)
	expanded := newMockWidget(0, 0, 20, 10, true)
	ec.SetCollapsedChild(collapsed)
	ec.SetExpandedChild(expanded)

	// Keys to collapsed child
	ev := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	ec.HandleKey(ev)
	if !collapsed.keyHandled {
		t.Error("key should route to collapsed child")
	}

	// Expand and send keys
	ec.Expand()
	ev = tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)
	ec.HandleKey(ev)
	if !expanded.keyHandled || expanded.lastKey != tcell.KeyLeft {
		t.Error("key should route to expanded child")
	}
}

func TestExpandableContainer_HitTestUsesCorrectBounds(t *testing.T) {
	ec := NewExpandableContainer(10, 10, 20, 1)
	ec.SetExpandedSize(20, 10)

	// When collapsed, only 1 row is hit testable
	if !ec.HitTest(10, 10) {
		t.Error("should hit test true at (10,10) when collapsed")
	}
	if ec.HitTest(10, 15) {
		t.Error("should hit test false at (10,15) when collapsed")
	}

	// When expanded, full area is hit testable
	ec.Expand()
	if !ec.HitTest(10, 10) {
		t.Error("should hit test true at (10,10) when expanded")
	}
	if !ec.HitTest(10, 15) {
		t.Error("should hit test true at (10,15) when expanded")
	}
	if ec.HitTest(10, 25) {
		t.Error("should hit test false at (10,25) when expanded")
	}
}

func TestExpandableContainer_DismissModalCollapses(t *testing.T) {
	ec := NewExpandableContainer(0, 0, 20, 1)
	ec.SetExpandedSize(20, 10)
	ec.Expand()

	ec.DismissModal()

	if ec.IsExpanded() {
		t.Error("DismissModal should collapse the container")
	}
}

func TestExpandableContainer_SetChildrenPositionsCorrectly(t *testing.T) {
	ec := NewExpandableContainer(10, 20, 30, 1)
	ec.SetExpandedSize(30, 15)

	collapsed := newMockWidget(0, 0, 10, 10, true)
	expanded := newMockWidget(0, 0, 10, 10, true)
	ec.SetCollapsedChild(collapsed)
	ec.SetExpandedChild(expanded)

	// Collapsed child should be positioned at container position with container size
	x, y := collapsed.Position()
	w, h := collapsed.Size()
	if x != 10 || y != 20 || w != 30 || h != 1 {
		t.Errorf("collapsed child rect = (%d,%d,%d,%d), want (10,20,30,1)", x, y, w, h)
	}

	// Expanded child should be positioned at container position with expanded size
	x, y = expanded.Position()
	w, h = expanded.Size()
	if x != 10 || y != 20 || w != 30 || h != 15 {
		t.Errorf("expanded child rect = (%d,%d,%d,%d), want (10,20,30,15)", x, y, w, h)
	}
}

func TestExpandableContainer_VisitChildren(t *testing.T) {
	ec := NewExpandableContainer(0, 0, 20, 1)

	collapsed := newMockWidget(0, 0, 20, 1, true)
	expanded := newMockWidget(0, 0, 20, 10, true)
	ec.SetCollapsedChild(collapsed)
	ec.SetExpandedChild(expanded)

	var visited []core.Widget
	ec.VisitChildren(func(w core.Widget) {
		visited = append(visited, w)
	})

	if len(visited) != 2 {
		t.Errorf("visited %d children, want 2", len(visited))
	}
}

func TestExpandableContainer_FocusCycler(t *testing.T) {
	ec := NewExpandableContainer(0, 0, 20, 1)
	ec.SetExpandedSize(20, 10)

	// When collapsed, CycleFocus returns false
	if ec.CycleFocus(true) {
		t.Error("CycleFocus should return false when collapsed")
	}

	// TrapsFocus returns false when collapsed
	if ec.TrapsFocus() {
		t.Error("TrapsFocus should return false when collapsed")
	}

	// When expanded, TrapsFocus returns true
	ec.Expand()
	if !ec.TrapsFocus() {
		t.Error("TrapsFocus should return true when expanded")
	}
}

func TestExpandableContainer_InvalidatorPropagation(t *testing.T) {
	ec := NewExpandableContainer(0, 0, 20, 1)

	invalidated := false
	ec.SetInvalidator(func(r core.Rect) {
		invalidated = true
	})

	collapsed := newMockWidget(0, 0, 20, 1, true)
	ec.SetCollapsedChild(collapsed)

	// Collapsed child should have received the invalidator
	if collapsed.inv == nil {
		t.Error("collapsed child should have received invalidator")
	}

	// Trigger invalidation through child
	collapsed.inv(collapsed.Rect)
	if !invalidated {
		t.Error("invalidation should propagate from child")
	}
}

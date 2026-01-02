// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/scroll/scrollpane.go
// Summary: ScrollPane widget for scrollable content that exceeds viewport size.
// Composes State, Viewport, and Indicators primitives.

package scroll

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
)

// ScrollPane is a container widget that scrolls its child when content exceeds the viewport.
// It handles vertical scrolling with keyboard and mouse wheel input.
type ScrollPane struct {
	core.BaseWidget
	Style           tcell.Style
	IndicatorStyle  tcell.Style
	child           core.Widget
	contentHeight   int // Total height of the child content
	state           State
	inv             func(core.Rect)
	showIndicators  bool
	indicatorConfig IndicatorConfig
	lastFocused     core.Widget // Track focused widget for auto-scroll on focus change
	trapsFocus      bool        // If true, wraps focus at boundaries instead of returning false
}

// NewScrollPane creates a new scroll pane with the given dimensions and style.
func NewScrollPane(x, y, w, h int, style tcell.Style) *ScrollPane {
	sp := &ScrollPane{
		showIndicators: true,
	}
	sp.SetPosition(x, y)
	sp.Resize(w, h)
	sp.SetFocusable(true) // ScrollPane must be focusable to receive key events

	// Resolve default colors from theme
	tm := theme.Get()
	fg, bg, attr := style.Decompose()
	if fg == tcell.ColorDefault {
		fg = tm.GetSemanticColor("text.primary")
	}
	if bg == tcell.ColorDefault {
		bg = tm.GetSemanticColor("bg.surface")
	}
	sp.Style = tcell.StyleDefault.Foreground(fg).Background(bg).Attributes(attr)

	// Set indicator style from theme
	indicatorFg := tm.GetSemanticColor("text.muted")
	if indicatorFg == tcell.ColorDefault {
		indicatorFg = fg
	}
	sp.IndicatorStyle = tcell.StyleDefault.Foreground(indicatorFg).Background(bg)
	sp.indicatorConfig = DefaultIndicatorConfig(sp.IndicatorStyle)

	return sp
}

// SetChild sets the child widget to be scrolled.
// The child's position will be managed by the scroll pane.
func (sp *ScrollPane) SetChild(child core.Widget) {
	sp.child = child
	if child != nil {
		// Propagate invalidator if set
		if sp.inv != nil {
			if ia, ok := child.(core.InvalidationAware); ok {
				ia.SetInvalidator(sp.inv)
			}
		}
		sp.updateContentHeight()
	} else {
		sp.contentHeight = 0
		sp.state = NewState(0, sp.Rect.H)
	}
}

// GetChild returns the child widget.
func (sp *ScrollPane) GetChild() core.Widget {
	return sp.child
}

// SetContentHeight explicitly sets the content height.
// Use this when the child widget doesn't report its full height.
func (sp *ScrollPane) SetContentHeight(h int) {
	sp.contentHeight = h
	sp.state = NewState(h, sp.Rect.H)
}

// ContentHeight returns the current content height.
func (sp *ScrollPane) ContentHeight() int {
	return sp.contentHeight
}

// ScrollOffset returns the current scroll offset.
func (sp *ScrollPane) ScrollOffset() int {
	return sp.state.Offset
}

// State returns the current scroll state.
func (sp *ScrollPane) State() State {
	return sp.state
}

// updateContentHeight calculates content height from child.
func (sp *ScrollPane) updateContentHeight() {
	if sp.child == nil {
		sp.contentHeight = 0
		sp.state = NewState(0, sp.Rect.H)
		return
	}
	_, h := sp.child.Size()
	sp.contentHeight = h
	sp.state = sp.state.WithContentHeight(h).WithViewportHeight(sp.Rect.H)
}

// SetInvalidator sets the invalidation callback.
func (sp *ScrollPane) SetInvalidator(fn func(core.Rect)) {
	sp.inv = fn
	if sp.child != nil {
		if ia, ok := sp.child.(core.InvalidationAware); ok {
			ia.SetInvalidator(fn)
		}
	}
}

// invalidate marks the entire scroll pane region as dirty.
func (sp *ScrollPane) invalidate() {
	if sp.inv != nil {
		sp.inv(sp.Rect)
	}
}

// ShowIndicators enables or disables scroll indicators.
func (sp *ScrollPane) ShowIndicators(show bool) {
	sp.showIndicators = show
}

// SetIndicatorConfig sets the indicator configuration.
func (sp *ScrollPane) SetIndicatorConfig(config IndicatorConfig) {
	sp.indicatorConfig = config
}

// Draw renders the scroll pane with its scrolled child content.
// Note: The child widget's position is managed by ScrollPane and is updated
// during Draw to reflect the current scroll offset. This is similar to how
// layout managers work - the child's position should not be relied upon by
// external code. The UI framework is single-threaded, so this is safe.
func (sp *ScrollPane) Draw(painter *core.Painter) {
	style := sp.EffectiveStyle(sp.Style)
	rect := sp.Rect

	// Fill background
	painter.Fill(rect, ' ', style)

	if sp.child == nil {
		return
	}

	// Only auto-scroll when focus changes (e.g., Tab navigation).
	// This allows manual scrolling with wheel/PgUp/PgDn without fighting back.
	currentFocused := sp.findFocusedWidget(sp.child)
	if currentFocused != sp.lastFocused {
		sp.lastFocused = currentFocused
		if currentFocused != nil {
			sp.EnsureFocusedVisible()
		}
	}

	// Position child relative to scroll offset.
	// Child's Y position is adjusted by scroll offset to simulate scrolling.
	// When offset > 0, child Y becomes negative, moving content "up" out of view.
	childX := rect.X
	childY := rect.Y - sp.state.Offset
	sp.child.SetPosition(childX, childY)

	// Create a clipped painter for the child so it doesn't draw outside bounds
	clipped := painter.WithClip(rect)
	sp.child.Draw(clipped)

	// Draw scroll indicators
	if sp.showIndicators {
		DrawIndicators(painter, rect, sp.state, sp.indicatorConfig)
	}
}

// Resize updates the viewport dimensions and recalculates scroll state.
func (sp *ScrollPane) Resize(w, h int) {
	sp.BaseWidget.Resize(w, h)
	sp.state = sp.state.WithViewportHeight(h)
	// Note: Child widget size is not changed here.
	// The child determines its own content height.
}

// ScrollBy scrolls by the given delta (positive = down, negative = up).
func (sp *ScrollPane) ScrollBy(delta int) {
	oldOffset := sp.state.Offset
	sp.state = sp.state.ScrollBy(delta)
	if sp.state.Offset != oldOffset {
		sp.invalidate()
	}
}

// ScrollTo scrolls to make the given row visible with minimal movement.
func (sp *ScrollPane) ScrollTo(row int) {
	oldOffset := sp.state.Offset
	sp.state = sp.state.ScrollTo(row)
	if sp.state.Offset != oldOffset {
		sp.invalidate()
	}
}

// ScrollToCentered scrolls to center the given row in the viewport.
func (sp *ScrollPane) ScrollToCentered(row int) {
	oldOffset := sp.state.Offset
	sp.state = sp.state.ScrollToCentered(row)
	if sp.state.Offset != oldOffset {
		sp.invalidate()
	}
}

// ScrollToTop scrolls to the top of the content.
func (sp *ScrollPane) ScrollToTop() {
	oldOffset := sp.state.Offset
	sp.state = sp.state.ScrollToTop()
	if sp.state.Offset != oldOffset {
		sp.invalidate()
	}
}

// ScrollToBottom scrolls to the bottom of the content.
func (sp *ScrollPane) ScrollToBottom() {
	oldOffset := sp.state.Offset
	sp.state = sp.state.ScrollToBottom()
	if sp.state.Offset != oldOffset {
		sp.invalidate()
	}
}

// EnsureFocusedVisible scrolls to make the currently focused widget visible.
func (sp *ScrollPane) EnsureFocusedVisible() {
	if sp.child == nil {
		return
	}

	focused := sp.findFocusedWidget(sp.child)
	if focused == nil {
		return
	}

	// Get focused widget's bounds in content coordinates
	_, widgetY := focused.Position()
	_, widgetH := focused.Size()

	// Calculate widget position relative to scroll pane content
	// widgetY is screen position, we need content position
	contentY := widgetY - sp.Rect.Y + sp.state.Offset

	// Check if widget is already fully visible
	if sp.state.IsRowVisible(contentY) && sp.state.IsRowVisible(contentY+widgetH-1) {
		return
	}

	// Scroll to make the widget visible
	// Prefer showing the top of the widget
	sp.ScrollTo(contentY)
}

// findFocusedWidget recursively finds the focused widget in the tree.
func (sp *ScrollPane) findFocusedWidget(w core.Widget) core.Widget {
	if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
		return w
	}
	if cc, ok := w.(core.ChildContainer); ok {
		var found core.Widget
		cc.VisitChildren(func(child core.Widget) {
			if found != nil {
				return
			}
			found = sp.findFocusedWidget(child)
		})
		return found
	}
	return nil
}

// SetTrapsFocus sets whether this ScrollPane wraps focus at boundaries.
// Set to true for root containers that should cycle focus internally.
func (sp *ScrollPane) SetTrapsFocus(trap bool) {
	sp.trapsFocus = trap
}

// TrapsFocus returns whether this ScrollPane wraps focus at boundaries.
func (sp *ScrollPane) TrapsFocus() bool {
	return sp.trapsFocus
}

// CycleFocus moves focus to next (forward=true) or previous (forward=false) child.
// Delegates to child if it implements FocusCycler.
// Returns true if focus was successfully cycled, false if at boundary.
func (sp *ScrollPane) CycleFocus(forward bool) bool {
	if sp.child == nil {
		return false
	}

	// Delegate to child if it's a FocusCycler
	if fc, ok := sp.child.(core.FocusCycler); ok {
		if fc.CycleFocus(forward) {
			sp.EnsureFocusedVisible()
			return true
		}
	}

	// Child exhausted or not a FocusCycler
	if sp.trapsFocus {
		// Wrap around - focus first/last widget in child
		if forward {
			sp.focusFirstInChild()
		} else {
			sp.focusLastInChild()
		}
		sp.EnsureFocusedVisible()
		return true
	}
	return false
}

// focusFirstInChild focuses the first focusable widget in the child.
func (sp *ScrollPane) focusFirstInChild() {
	if sp.child == nil {
		return
	}
	first := sp.findFirstFocusable(sp.child)
	if first != nil {
		first.Focus()
	}
}

// focusLastInChild focuses the last focusable widget in the child.
func (sp *ScrollPane) focusLastInChild() {
	if sp.child == nil {
		return
	}
	last := sp.findLastFocusable(sp.child)
	if last != nil {
		last.Focus()
	}
}

// findFirstFocusable recursively finds the first focusable widget.
func (sp *ScrollPane) findFirstFocusable(w core.Widget) core.Widget {
	if w.Focusable() {
		if cc, ok := w.(core.ChildContainer); ok {
			var first core.Widget
			cc.VisitChildren(func(child core.Widget) {
				if first != nil {
					return
				}
				first = sp.findFirstFocusable(child)
			})
			if first != nil {
				return first
			}
		}
		return w
	}
	if cc, ok := w.(core.ChildContainer); ok {
		var first core.Widget
		cc.VisitChildren(func(child core.Widget) {
			if first != nil {
				return
			}
			first = sp.findFirstFocusable(child)
		})
		return first
	}
	return nil
}

// findLastFocusable recursively finds the last focusable widget.
func (sp *ScrollPane) findLastFocusable(w core.Widget) core.Widget {
	if cc, ok := w.(core.ChildContainer); ok {
		var children []core.Widget
		cc.VisitChildren(func(child core.Widget) {
			children = append(children, child)
		})
		for i := len(children) - 1; i >= 0; i-- {
			last := sp.findLastFocusable(children[i])
			if last != nil {
				return last
			}
		}
	}
	if w.Focusable() {
		return w
	}
	return nil
}

// HandleKey handles keyboard input for scrolling.
func (sp *ScrollPane) HandleKey(ev *tcell.EventKey) bool {
	// Handle scroll-specific keys first (these don't trigger auto-scroll back)
	switch ev.Key() {
	case tcell.KeyPgUp:
		sp.ScrollBy(-sp.Rect.H)
		return true
	case tcell.KeyPgDn:
		sp.ScrollBy(sp.Rect.H)
		return true
	case tcell.KeyHome:
		if ev.Modifiers()&tcell.ModCtrl != 0 {
			sp.ScrollToTop()
			return true
		}
	case tcell.KeyEnd:
		if ev.Modifiers()&tcell.ModCtrl != 0 {
			sp.ScrollToBottom()
			return true
		}
	}

	// Route other keys to child
	if sp.child != nil {
		if sp.child.HandleKey(ev) {
			// After child handles a non-scroll key, ensure focused widget is visible.
			// This brings the view back to the focused widget when user types.
			sp.EnsureFocusedVisible()
			return true
		}
	}

	return false
}

// HandleMouse handles mouse input for scrolling.
func (sp *ScrollPane) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !sp.HitTest(x, y) {
		return false
	}

	// Handle scroll wheel
	switch ev.Buttons() {
	case tcell.WheelUp:
		sp.ScrollBy(-3) // Scroll 3 rows up
		return true
	case tcell.WheelDown:
		sp.ScrollBy(3) // Scroll 3 rows down
		return true
	}

	// Route other mouse events to child
	if sp.child != nil {
		if ma, ok := sp.child.(core.MouseAware); ok {
			return ma.HandleMouse(ev)
		}
	}

	return true
}

// VisitChildren implements core.ChildContainer for focus traversal.
func (sp *ScrollPane) VisitChildren(f func(core.Widget)) {
	if sp.child != nil {
		f(sp.child)
	}
}

// WidgetAt implements core.HitTester for mouse event routing.
// ScrollPane returns itself to ensure it receives mouse events (especially wheel).
// Child widget delegation is handled in HandleMouse.
func (sp *ScrollPane) WidgetAt(x, y int) core.Widget {
	if !sp.HitTest(x, y) {
		return nil
	}
	// Always return self - we handle child routing in HandleMouse
	return sp
}

// CanScroll returns true if the content can be scrolled.
func (sp *ScrollPane) CanScroll() bool {
	return sp.state.CanScroll()
}

// CanScrollUp returns true if there is content above the viewport.
func (sp *ScrollPane) CanScrollUp() bool {
	return sp.state.CanScrollUp()
}

// CanScrollDown returns true if there is content below the viewport.
func (sp *ScrollPane) CanScrollDown() bool {
	return sp.state.CanScrollDown()
}

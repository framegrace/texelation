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

	// Scrollbar mouse interaction state
	draggingThumb   bool // True when thumb is being dragged
	dragStartY      int  // Y position where drag started
	dragStartOffset int  // Scroll offset when drag started
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

	// Set up scrollbar with text.primary color for thumb
	thumbStyle := tcell.StyleDefault.Foreground(fg).Background(bg)
	trackFg := tm.GetSemanticColor("text.muted")
	if trackFg == tcell.ColorDefault {
		trackFg = fg
	}
	trackStyle := tcell.StyleDefault.Foreground(trackFg).Background(bg)

	// Enable scrollbar by default
	sp.indicatorConfig = DefaultIndicatorConfigWithScrollbar(thumbStyle, trackStyle)

	// Also set IndicatorStyle for backwards compatibility
	sp.IndicatorStyle = thumbStyle

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
	// Preserve existing offset when updating content height
	sp.state = sp.state.WithContentHeight(h).WithViewportHeight(sp.Rect.H)
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
// Returns true if the scroll position changed (useful for event bubbling).
func (sp *ScrollPane) ScrollBy(delta int) bool {
	oldOffset := sp.state.Offset
	sp.state = sp.state.ScrollBy(delta)
	changed := sp.state.Offset != oldOffset
	if changed {
		sp.invalidate()
	}
	return changed
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

// scrollbarGeometry returns the scrollbar's X position and thumb start/end rows (relative to rect).
// Returns scrollbarX, thumbStart, thumbEnd, trackHeight.
// thumbStart and thumbEnd are relative to the track area (excluding arrows).
func (sp *ScrollPane) scrollbarGeometry() (scrollbarX, thumbStart, thumbEnd, trackHeight int) {
	rect := sp.Rect
	if rect.H < 1 || !sp.state.CanScroll() {
		return -1, 0, 0, 0 // No scrollbar
	}

	// Scrollbar X position
	switch sp.indicatorConfig.Scrollbar.Position {
	case IndicatorLeft:
		scrollbarX = rect.X
	default:
		scrollbarX = rect.X + rect.W - 1
	}

	// Track area is between arrows (rows 1 to H-2)
	trackHeight = rect.H - 2
	if trackHeight <= 0 {
		return scrollbarX, 0, 0, 0
	}

	// Calculate thumb size (same logic as DrawScrollbar)
	thumbSize := (sp.state.ViewportHeight * trackHeight) / sp.state.ContentHeight
	minThumb := sp.indicatorConfig.Scrollbar.MinThumbSize
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
	scrollableContent := sp.state.ContentHeight - sp.state.ViewportHeight
	scrollableTrack := trackHeight - thumbSize

	thumbStart = 0
	if scrollableContent > 0 && scrollableTrack > 0 {
		thumbStart = (sp.state.Offset * scrollableTrack) / scrollableContent
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

// HandleMouse handles mouse input for scrolling.
func (sp *ScrollPane) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	buttons := ev.Buttons()

	// Handle drag release
	if sp.draggingThumb && buttons&tcell.Button1 == 0 {
		sp.draggingThumb = false
		// Restore focus to content after drag
		if sp.lastFocused != nil {
			sp.lastFocused.Focus()
		}
		return true
	}

	// Handle ongoing thumb drag
	if sp.draggingThumb && buttons&tcell.Button1 != 0 {
		sp.handleThumbDrag(y)
		// Maintain focus on content during drag
		if sp.lastFocused != nil {
			sp.lastFocused.Focus()
		}
		return true
	}

	if !sp.HitTest(x, y) {
		return false
	}

	// Handle scroll wheel - forward to child first, then handle ourselves
	if buttons&(tcell.WheelUp|tcell.WheelDown) != 0 {
		// Forward to child first - nested scrollable content gets priority
		if sp.child != nil && sp.child.HitTest(x, y) {
			if ma, ok := sp.child.(core.MouseAware); ok {
				if ma.HandleMouse(ev) {
					return true
				}
			}
		}
		// Child didn't handle it (or no child), ScrollPane handles it
		if buttons&tcell.WheelUp != 0 {
			return sp.ScrollBy(-3)
		} else if buttons&tcell.WheelDown != 0 {
			return sp.ScrollBy(3)
		}
	}

	// Check if click is on scrollbar
	if sp.showIndicators && sp.indicatorConfig.ShowScrollbar && buttons&tcell.Button1 != 0 {
		scrollbarX, thumbStart, thumbEnd, trackHeight := sp.scrollbarGeometry()
		if scrollbarX >= 0 && x == scrollbarX {
			// Restore focus after scrollbar interaction.
			// UIManager blurs before routing mouse, so use lastFocused from Draw.
			restoreFocus := func() {
				if sp.lastFocused != nil {
					sp.lastFocused.Focus()
				}
			}

			// Convert y to relative position in scrollbar
			relY := y - sp.Rect.Y

			// Up arrow at row 0
			if relY == 0 {
				sp.ScrollBy(-1)
				restoreFocus()
				return true
			}

			// Down arrow at last row
			if relY == sp.Rect.H-1 {
				sp.ScrollBy(1)
				restoreFocus()
				return true
			}

			// Track area (between arrows, rows 1 to H-2)
			trackY := relY - 1 // Convert to track-relative position
			if trackY >= 0 && trackY < trackHeight {
				if trackY >= thumbStart && trackY < thumbEnd {
					// Click on thumb - start drag
					sp.draggingThumb = true
					sp.dragStartY = y
					sp.dragStartOffset = sp.state.Offset
					restoreFocus()
					return true
				} else if trackY < thumbStart {
					// Click above thumb - page up
					sp.ScrollBy(-sp.Rect.H)
					restoreFocus()
					return true
				} else {
					// Click below thumb - page down
					sp.ScrollBy(sp.Rect.H)
					restoreFocus()
					return true
				}
			}
		}
	}

	// Route other mouse events to child
	if sp.child != nil {
		if ma, ok := sp.child.(core.MouseAware); ok {
			return ma.HandleMouse(ev)
		}
	}

	// Return false if we didn't actually handle the event
	// This allows parent widgets to handle clicks that weren't on the scrollbar
	return false
}

// handleThumbDrag updates scroll position based on thumb drag.
func (sp *ScrollPane) handleThumbDrag(currentY int) {
	if !sp.state.CanScroll() {
		return
	}

	// Calculate how far the mouse has moved in screen pixels
	deltaY := currentY - sp.dragStartY

	// Track area is between arrows (rows 1 to H-2)
	trackHeight := sp.Rect.H - 2
	if trackHeight <= 0 {
		return
	}

	// Calculate thumb size
	thumbSize := (sp.state.ViewportHeight * trackHeight) / sp.state.ContentHeight
	minThumb := sp.indicatorConfig.Scrollbar.MinThumbSize
	if minThumb <= 0 {
		minThumb = 1
	}
	if thumbSize < minThumb {
		thumbSize = minThumb
	}
	if thumbSize > trackHeight {
		thumbSize = trackHeight
	}

	// Scrollable ranges
	scrollableTrack := trackHeight - thumbSize
	scrollableContent := sp.state.ContentHeight - sp.state.ViewportHeight

	if scrollableTrack <= 0 || scrollableContent <= 0 {
		return
	}

	// Convert mouse delta to content offset delta
	// deltaOffset / scrollableContent = deltaY / scrollableTrack
	deltaOffset := (deltaY * scrollableContent) / scrollableTrack
	newOffset := sp.dragStartOffset + deltaOffset

	// Clamp to valid range
	if newOffset < 0 {
		newOffset = 0
	}
	if newOffset > scrollableContent {
		newOffset = scrollableContent
	}

	if newOffset != sp.state.Offset {
		sp.state = sp.state.WithOffset(newOffset)
		sp.invalidate()
	}
}

// VisitChildren implements core.ChildContainer for focus traversal.
func (sp *ScrollPane) VisitChildren(f func(core.Widget)) {
	if sp.child != nil {
		f(sp.child)
	}
}

// WidgetAt implements core.HitTester for deep focus traversal.
// Returns the deepest focusable widget under the point.
func (sp *ScrollPane) WidgetAt(x, y int) core.Widget {
	if !sp.HitTest(x, y) {
		return nil
	}

	// Check if click is on scrollbar (rightmost column when scrollbar is shown)
	if sp.showIndicators && sp.indicatorConfig.ShowScrollbar && sp.state.CanScroll() {
		scrollbarX := sp.Rect.X + sp.Rect.W - 1
		if sp.indicatorConfig.Scrollbar.Position == IndicatorLeft {
			scrollbarX = sp.Rect.X
		}
		if x == scrollbarX {
			// Click on scrollbar - don't change focus, return currently focused widget
			// This prevents scrollbar interactions from stealing focus from content
			if focused := sp.findFocusedWidget(sp.child); focused != nil {
				return focused
			}
			// No focused child, return self
			return sp
		}
	}

	// Delegate to child for deep hit testing
	if sp.child != nil && sp.child.HitTest(x, y) {
		if ht, ok := sp.child.(core.HitTester); ok {
			if dw := ht.WidgetAt(x, y); dw != nil {
				return dw
			}
		}
		// Child hit but no deeper widget - return child if focusable
		if sp.child.Focusable() {
			return sp.child
		}
	}

	// Return self as fallback
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

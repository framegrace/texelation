package widgets

import (
	"sort"

	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
)

// Pane is a container widget that fills its area with a background style
// and can contain child widgets.
type Pane struct {
	core.BaseWidget
	Style    tcell.Style
	children []core.Widget
	inv      func(core.Rect)

	// Focus cycling support
	trapsFocus     bool // If true, wraps focus at boundaries instead of returning false
	lastFocusedIdx int  // Index of last focused child for focus restoration
}

func NewPane(x, y, w, h int, style tcell.Style) *Pane {
	p := &Pane{
		lastFocusedIdx: -1, // No child focused yet
	}
	p.SetPosition(x, y)
	p.Resize(w, h)

	// Resolve default colors from theme
	tm := theme.Get()
	fg, bg, attr := style.Decompose()
	if fg == tcell.ColorDefault {
		fg = tm.GetSemanticColor("text.primary")
	}
	if bg == tcell.ColorDefault {
		bg = tm.GetSemanticColor("bg.surface")
	}
	// Update style with resolved colors
	p.Style = tcell.StyleDefault.Foreground(fg).Background(bg).Attributes(attr)

	// Configure focus style
	fbg := tm.GetSemanticColor("bg.surface")
	ffg := tm.GetSemanticColor("text.primary")
	p.SetFocusedStyle(tcell.StyleDefault.Background(fbg).Foreground(ffg), true)
	return p
}

// SetTrapsFocus sets whether this pane wraps focus at boundaries.
// Set to true for root containers that should cycle focus internally.
func (p *Pane) SetTrapsFocus(trap bool) {
	p.trapsFocus = trap
}

// TrapsFocus returns whether this pane wraps focus at boundaries.
func (p *Pane) TrapsFocus() bool {
	return p.trapsFocus
}

// AddChild adds a child widget to this pane.
func (p *Pane) AddChild(w core.Widget) {
	p.children = append(p.children, w)
	// Propagate invalidator if set
	if p.inv != nil {
		if ia, ok := w.(core.InvalidationAware); ok {
			ia.SetInvalidator(p.inv)
		}
	}
}

// RemoveChild removes a child widget from this pane.
func (p *Pane) RemoveChild(w core.Widget) {
	for i, child := range p.children {
		if child == w {
			p.children = append(p.children[:i], p.children[i+1:]...)
			return
		}
	}
}

// SetInvalidator sets the invalidation callback for this pane and its children.
func (p *Pane) SetInvalidator(fn func(core.Rect)) {
	p.inv = fn
	for _, child := range p.children {
		if ia, ok := child.(core.InvalidationAware); ok {
			ia.SetInvalidator(fn)
		}
	}
}

// Draw fills the pane background and draws all children sorted by z-index.
func (p *Pane) Draw(painter *core.Painter) {
	style := p.EffectiveStyle(p.Style)
	painter.Fill(core.Rect{X: p.Rect.X, Y: p.Rect.Y, W: p.Rect.W, H: p.Rect.H}, ' ', style)

	// Sort children by z-index (lower z-index drawn first, higher on top)
	sorted := make([]core.Widget, len(p.children))
	copy(sorted, p.children)
	sort.Slice(sorted, func(i, j int) bool {
		zi, zj := 0, 0
		if z, ok := sorted[i].(core.ZIndexer); ok {
			zi = z.ZIndex()
		}
		if z, ok := sorted[j].(core.ZIndexer); ok {
			zj = z.ZIndex()
		}
		return zi < zj
	})

	// Draw children
	for _, child := range sorted {
		child.Draw(painter)
	}
}

// VisitChildren implements core.ChildContainer for focus traversal.
func (p *Pane) VisitChildren(f func(core.Widget)) {
	for _, child := range p.children {
		f(child)
	}
}

// WidgetAt implements core.HitTester for mouse event routing, respecting Z-index.
func (p *Pane) WidgetAt(x, y int) core.Widget {
	if !p.HitTest(x, y) {
		return nil
	}

	// Sort children by Z-index descending (highest first)
	// This ensures expanded dropdowns and modals are found first
	sorted := make([]core.Widget, len(p.children))
	copy(sorted, p.children)
	sort.Slice(sorted, func(i, j int) bool {
		zi, zj := 0, 0
		if z, ok := sorted[i].(core.ZIndexer); ok {
			zi = z.ZIndex()
		}
		if z, ok := sorted[j].(core.ZIndexer); ok {
			zj = z.ZIndex()
		}
		return zi > zj // Descending order (highest Z first)
	})

	// Check children in Z-order (highest first)
	for _, child := range sorted {
		if child.HitTest(x, y) {
			if ht, ok := child.(core.HitTester); ok {
				if dw := ht.WidgetAt(x, y); dw != nil {
					return dw
				}
			}
			return child
		}
	}
	return p
}

// Focus focuses the first focusable child, or restores last focused child.
func (p *Pane) Focus() {
	p.BaseWidget.Focus()

	focusables := p.getFocusableChildren()
	if len(focusables) == 0 {
		return
	}

	// Try to restore last focused child
	if p.lastFocusedIdx >= 0 && p.lastFocusedIdx < len(focusables) {
		focusables[p.lastFocusedIdx].Focus()
		return
	}

	// Focus first child
	focusables[0].Focus()
	p.lastFocusedIdx = 0
}

// Blur blurs all children and tracks which one was focused.
func (p *Pane) Blur() {
	focusables := p.getFocusableChildren()
	for i, w := range focusables {
		if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
			p.lastFocusedIdx = i
			w.Blur()
			break
		}
	}
	p.BaseWidget.Blur()
}

// CycleFocus moves focus to next (forward=true) or previous (forward=false) child.
// Returns true if focus was successfully cycled, false if at boundary.
func (p *Pane) CycleFocus(forward bool) bool {
	focusables := p.getFocusableChildren()
	if len(focusables) == 0 {
		return false
	}

	// Find currently focused widget
	currentIdx := -1
	var focusedWidget core.Widget
	for i, w := range focusables {
		if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
			currentIdx = i
			focusedWidget = w
			break
		}
	}

	// If nothing focused, focus first/last based on direction
	if currentIdx < 0 {
		if forward {
			focusables[0].Focus()
			p.lastFocusedIdx = 0
		} else {
			focusables[len(focusables)-1].Focus()
			p.lastFocusedIdx = len(focusables) - 1
		}
		if p.inv != nil {
			p.inv(p.Rect)
		}
		return true
	}

	var nextIdx int
	if forward {
		nextIdx = currentIdx + 1
		if nextIdx >= len(focusables) {
			if p.trapsFocus {
				nextIdx = 0 // Wrap around
			} else {
				return false // At boundary, let parent handle
			}
		}
	} else {
		nextIdx = currentIdx - 1
		if nextIdx < 0 {
			if p.trapsFocus {
				nextIdx = len(focusables) - 1 // Wrap around
			} else {
				return false // At boundary, let parent handle
			}
		}
	}

	focusedWidget.Blur()
	focusables[nextIdx].Focus()
	p.lastFocusedIdx = nextIdx
	if p.inv != nil {
		p.inv(p.Rect)
	}
	return true
}

// HandleKey routes key events to the focused child.
// Tab/Shift-Tab is NOT handled here - parent containers should call CycleFocus.
func (p *Pane) HandleKey(ev *tcell.EventKey) bool {
	// Find the focused widget
	focusables := p.getFocusableChildren()
	var focusedWidget core.Widget
	var focusedIdx int = -1
	for i, w := range focusables {
		if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
			focusedWidget = w
			focusedIdx = i
			break
		}
	}

	if focusedWidget == nil {
		return false
	}

	// For Tab/Shift-Tab, only forward to nested containers (not leaf widgets)
	isTab := ev.Key() == tcell.KeyTab || ev.Key() == tcell.KeyBacktab
	if isTab {
		// Check if focused widget is a container that can handle Tab internally
		if _, isContainer := focusedWidget.(core.FocusCycler); isContainer {
			if focusedWidget.HandleKey(ev) {
				return true
			}
		}
		// Leaf widget or container exhausted - return false to let parent handle
		return false
	}

	// Route non-Tab keys to focused widget
	handled := focusedWidget.HandleKey(ev)
	if handled {
		p.lastFocusedIdx = focusedIdx
	}
	return handled
}

// getFocusableChildren returns all focusable widgets in this pane (flattened).
func (p *Pane) getFocusableChildren() []core.Widget {
	var result []core.Widget
	var visit func(w core.Widget)
	visit = func(w core.Widget) {
		if w.Focusable() {
			result = append(result, w)
		}
		if cc, ok := w.(core.ChildContainer); ok {
			cc.VisitChildren(func(child core.Widget) {
				visit(child)
			})
		}
	}
	for _, child := range p.children {
		visit(child)
	}
	return result
}

// HandleMouse routes mouse events to children, respecting Z-index.
func (p *Pane) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !p.HitTest(x, y) {
		return false
	}

	buttons := ev.Buttons()
	isPress := buttons&tcell.Button1 != 0
	isWheel := buttons&(tcell.WheelUp|tcell.WheelDown|tcell.WheelLeft|tcell.WheelRight) != 0

	// Sort children by Z-index descending (highest first) for mouse routing
	// This ensures expanded dropdowns and modals receive events first
	sorted := make([]core.Widget, len(p.children))
	copy(sorted, p.children)
	sort.Slice(sorted, func(i, j int) bool {
		zi, zj := 0, 0
		if z, ok := sorted[i].(core.ZIndexer); ok {
			zi = z.ZIndex()
		}
		if z, ok := sorted[j].(core.ZIndexer); ok {
			zj = z.ZIndex()
		}
		return zi > zj // Descending order for mouse (highest Z first)
	})

	// Check children in Z-order (highest first)
	for _, child := range sorted {
		if child.HitTest(x, y) {
			// Focus the clicked widget on button press
			if isPress && child.Focusable() {
				// Blur currently focused widget
				focusables := p.getFocusableChildren()
				for i, w := range focusables {
					if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
						if w != child {
							w.Blur()
						}
					}
					if w == child {
						p.lastFocusedIdx = i
					}
				}
				// Focus the clicked widget
				child.Focus()
				if p.inv != nil {
					p.inv(p.Rect)
				}
			}
			if ma, ok := child.(core.MouseAware); ok {
				return ma.HandleMouse(ev)
			}
			// Child hit but not MouseAware - only claim handled for non-wheel events
			// Wheel events should bubble up to parent scrollable containers
			return !isWheel
		}
	}
	// No child hit - only claim handled for non-wheel events
	return !isWheel
}

// Resize updates all children positions relative to pane position.
func (p *Pane) Resize(w, h int) {
	p.BaseWidget.Resize(w, h)
	// Children keep their relative positions
}

// SetPosition updates the pane position and adjusts child positions.
func (p *Pane) SetPosition(x, y int) {
	// Calculate offset from old position
	dx := x - p.Rect.X
	dy := y - p.Rect.Y

	p.BaseWidget.SetPosition(x, y)

	// Move children by the same offset
	for _, child := range p.children {
		oldX, oldY := child.Position()
		child.SetPosition(oldX+dx, oldY+dy)
	}
}

// Position returns the current position.
func (p *Pane) Position() (int, int) {
	return p.Rect.X, p.Rect.Y
}

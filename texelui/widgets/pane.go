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
}

func NewPane(x, y, w, h int, style tcell.Style) *Pane {
	p := &Pane{}
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

// HandleKey routes key events to the focused child.
func (p *Pane) HandleKey(ev *tcell.EventKey) bool {
	// Find focused child and route to it
	for _, child := range p.children {
		if fs, ok := child.(core.FocusState); ok && fs.IsFocused() {
			return child.HandleKey(ev)
		}
		// Check nested containers
		if cc, ok := child.(core.ChildContainer); ok {
			handled := false
			cc.VisitChildren(func(w core.Widget) {
				if handled {
					return
				}
				if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
					handled = child.HandleKey(ev)
				}
			})
			if handled {
				return true
			}
		}
	}
	return false
}

// HandleMouse routes mouse events to children, respecting Z-index.
func (p *Pane) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !p.HitTest(x, y) {
		return false
	}

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
			if ma, ok := child.(core.MouseAware); ok {
				return ma.HandleMouse(ev)
			}
			return true
		}
	}
	return true
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

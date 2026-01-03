package widgets

import (
    "github.com/gdamore/tcell/v2"
    "texelation/texel/theme"
    "texelation/texelui/core"
)

// Border draws a border around its Rect and can optionally have a child rendered inside.
type Border struct {
    core.BaseWidget
    Style   tcell.Style
    Charset [6]rune // h, v, tl, tr, bl, br
    Child   core.Widget
    inv     func(core.Rect)
    // FocusedStyle optionally overrides Style when this border (or a focused descendant) is focused.
    FocusedStyle tcell.Style

    // SeparatorMode when true draws only edges where neighbors exist.
    // This is useful for status bars and section dividers.
    SeparatorMode bool
    // Neighbors specifies which edges have neighboring widgets.
    // Only used when SeparatorMode is true.
    Neighbors core.NeighborInfo
}

func NewBorder(x, y, w, h int, style tcell.Style) *Border {
	b := &Border{}

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
	b.Style = tcell.StyleDefault.Foreground(fg).Background(bg).Attributes(attr)

	// Focused style uses border.active foreground
	ffg := tm.GetSemanticColor("border.active")
	b.FocusedStyle = tcell.StyleDefault.Foreground(ffg).Background(bg)
	b.SetFocusedStyle(tcell.StyleDefault.Foreground(ffg).Background(bg), true)

	// Default rounded corner charset
	b.Charset = [6]rune{'─', '│', '╭', '╮', '╰', '╯'}
	b.SetPosition(x, y)
	b.Resize(w, h)
	return b
}

func (b *Border) ClientRect() core.Rect {
	r := b.Rect
	if r.W < 2 || r.H < 2 {
		return core.Rect{X: r.X, Y: r.Y, W: 0, H: 0}
	}
	return core.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}
}

func (b *Border) SetChild(w core.Widget) {
	b.Child = w
	cr := b.ClientRect()
	if b.Child != nil {
		b.Child.SetPosition(cr.X, cr.Y)
		b.Child.Resize(cr.W, cr.H)
		if ia, ok := b.Child.(core.InvalidationAware); ok {
			ia.SetInvalidator(b.invalidate)
		}
	}
}

func (b *Border) SetPosition(x, y int) {
	b.BaseWidget.SetPosition(x, y)
	if b.Child != nil {
		cr := b.ClientRect()
		b.Child.SetPosition(cr.X, cr.Y)
	}
}

func (b *Border) Resize(w, h int) {
	b.BaseWidget.Resize(w, h)
	if b.Child != nil {
		cr := b.ClientRect()
		b.Child.SetPosition(cr.X, cr.Y)
		b.Child.Resize(cr.W, cr.H)
	}
}

func (b *Border) Draw(p *core.Painter) {
	style := b.Style
	// If this border or any descendant contains focus, use FocusedStyle
	if b.IsFocused() || core.IsDescendantFocused(b.Child) {
		style = b.FocusedStyle
	} else {
		// Otherwise apply own focus style if enabled
		style = b.EffectiveStyle(style)
	}

	if b.SeparatorMode {
		b.drawSeparator(p, style)
	} else {
		p.DrawBorder(b.Rect, style, b.Charset)
	}

	if b.Child != nil {
		b.Child.Draw(p)
	}
}

// drawSeparator draws only the edges where neighbors exist.
func (b *Border) drawSeparator(p *core.Painter, style tcell.Style) {
	r := b.Rect
	hChar := b.Charset[0] // horizontal line character

	// Draw top edge if there's a neighbor above
	if b.Neighbors.Top {
		for x := r.X; x < r.X+r.W; x++ {
			p.SetCell(x, r.Y, hChar, style)
		}
	}

	// Draw bottom edge if there's a neighbor below
	if b.Neighbors.Bottom {
		bottomY := r.Y + r.H - 1
		for x := r.X; x < r.X+r.W; x++ {
			p.SetCell(x, bottomY, hChar, style)
		}
	}

	// Draw left edge if there's a neighbor to the left
	if b.Neighbors.Left {
		vChar := b.Charset[1] // vertical line character
		for y := r.Y; y < r.Y+r.H; y++ {
			p.SetCell(r.X, y, vChar, style)
		}
	}

	// Draw right edge if there's a neighbor to the right
	if b.Neighbors.Right {
		vChar := b.Charset[1] // vertical line character
		rightX := r.X + r.W - 1
		for y := r.Y; y < r.Y+r.H; y++ {
			p.SetCell(rightX, y, vChar, style)
		}
	}
}

// VisitChildren implements core.ChildContainer for recursive operations.
func (b *Border) VisitChildren(f func(core.Widget)) {
	if b.Child != nil {
		f(b.Child)
	}
}

// invalidate adapts the injected invalidator to child-local use.
func (b *Border) invalidate(r core.Rect) {
	if b.inv != nil {
		b.inv(r)
	}
}

// SetInvalidator lets UIManager inject invalidation into the child tree.
func (b *Border) SetInvalidator(fn func(core.Rect)) {
	b.inv = fn
	if b.Child != nil {
		if ia, ok := b.Child.(core.InvalidationAware); ok {
			ia.SetInvalidator(fn)
		}
	}
}

// WidgetAt returns the deepest child under the point or the border itself.
func (b *Border) WidgetAt(x, y int) core.Widget {
	if b.Child != nil && b.Child.HitTest(x, y) {
		if ht, ok := b.Child.(core.HitTester); ok {
			if dw := ht.WidgetAt(x, y); dw != nil {
				return dw
			}
		}
		return b.Child
	}
	if b.HitTest(x, y) {
		return b
	}
	return nil
}

// HandleMouse routes mouse events to child and handles focus on click.
func (b *Border) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !b.HitTest(x, y) {
		return false
	}

	// On button press, focus the child if clicked
	isPress := ev.Buttons()&tcell.Button1 != 0
	if isPress && b.Child != nil && b.Child.HitTest(x, y) && b.Child.Focusable() {
		b.Child.Focus()
		if b.inv != nil {
			b.inv(b.Rect)
		}
	}

	// Forward to child if it handles mouse
	if b.Child != nil && b.Child.HitTest(x, y) {
		if ma, ok := b.Child.(core.MouseAware); ok {
			return ma.HandleMouse(ev)
		}
		return true
	}

	return true
}


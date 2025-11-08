package widgets

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texelui/core"
)

// Border draws a border around its Rect and can optionally have a child rendered inside.
type Border struct {
	core.BaseWidget
	Style   tcell.Style
	Charset [6]rune // h, v, tl, tr, bl, br
	Child   core.Widget
}

func NewBorder(x, y, w, h int, style tcell.Style) *Border {
	b := &Border{Style: style}
	// default single-line charset
	b.Charset = [6]rune{'─', '│', '┌', '┐', '└', '┘'}
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
	p.DrawBorder(b.Rect, b.Style, b.Charset)
	if b.Child != nil {
		b.Child.Draw(p)
	}
}

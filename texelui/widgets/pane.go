package widgets

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texelui/core"
)

type Pane struct {
	core.BaseWidget
	Style tcell.Style
}

func NewPane(x, y, w, h int, style tcell.Style) *Pane {
	p := &Pane{Style: style}
	p.SetPosition(x, y)
	p.Resize(w, h)
	return p
}

func (p *Pane) Draw(painter *core.Painter) {
    style := p.EffectiveStyle(p.Style)
    painter.Fill(core.Rect{X: p.Rect.X, Y: p.Rect.Y, W: p.Rect.W, H: p.Rect.H}, ' ', style)
}

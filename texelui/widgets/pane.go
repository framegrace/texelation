package widgets

import (
    "github.com/gdamore/tcell/v2"
    "texelation/texel/theme"
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
    // Configure default focus style from theme; fall back to base style colors
    tm := theme.Get()
    fg, bg, _ := style.Decompose()
    if fg == tcell.ColorDefault {
        fg = tm.GetSemanticColor("text.primary")
    }
    if bg == tcell.ColorDefault {
        bg = tm.GetSemanticColor("bg.surface")
    }
    fbg := tm.GetSemanticColor("bg.surface") // Default to surface, can be overridden if needed
    ffg := tm.GetSemanticColor("text.primary")
    p.SetFocusedStyle(tcell.StyleDefault.Background(fbg).Foreground(ffg), true)
    return p
}

func (p *Pane) Draw(painter *core.Painter) {
    style := p.EffectiveStyle(p.Style)
    painter.Fill(core.Rect{X: p.Rect.X, Y: p.Rect.Y, W: p.Rect.W, H: p.Rect.H}, ' ', style)
}

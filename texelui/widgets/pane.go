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
        fg = tm.GetColor("ui", "surface_fg", tcell.ColorWhite)
    }
    if bg == tcell.ColorDefault {
        bg = tm.GetColor("ui", "surface_bg", tcell.ColorBlack)
    }
    fbg := tm.GetColor("ui", "focus_surface_bg", bg)
    ffg := tm.GetColor("ui", "focus_surface_fg", fg)
    p.SetFocusedStyle(tcell.StyleDefault.Background(fbg).Foreground(ffg), true)
    return p
}

func (p *Pane) Draw(painter *core.Painter) {
    style := p.EffectiveStyle(p.Style)
    painter.Fill(core.Rect{X: p.Rect.X, Y: p.Rect.Y, W: p.Rect.W, H: p.Rect.H}, ' ', style)
}

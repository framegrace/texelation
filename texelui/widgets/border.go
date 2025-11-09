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
}

func NewBorder(x, y, w, h int, style tcell.Style) *Border {
    b := &Border{Style: style}
    // Default focused style from theme (fg override), bg same as base style
    tm := theme.Get()
    ffg := tm.GetColor("ui", "focus_border_fg", tcell.ColorYellow)
    fg, bg, _ := style.Decompose()
    if bg == tcell.ColorDefault {
        bg = tm.GetColor("ui", "surface_bg", tcell.ColorBlack)
    }
    b.FocusedStyle = tcell.StyleDefault.Foreground(ffg).Background(bg)
    // Also set BaseWidget focused style for self-focus (theme defaults)
    fbg := tm.GetColor("ui", "focus_border_bg", bg)
    if fg == tcell.ColorDefault {
        fg = tm.GetColor("ui", "surface_fg", tcell.ColorWhite)
    }
    b.SetFocusedStyle(tcell.StyleDefault.Foreground(ffg).Background(fbg), true)
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
		if ia, ok := b.Child.(core.InvalidationAware); ok {
			ia.SetInvalidator(b.invalidate)
		}
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
    if b.isDescendantFocused() {
        style = b.FocusedStyle
    } else {
        // Otherwise apply own focus style if enabled
        style = b.EffectiveStyle(style)
    }
    p.DrawBorder(b.Rect, style, b.Charset)
    if b.Child != nil {
        b.Child.Draw(p)
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

func (b *Border) isDescendantFocused() bool {
    // Check self
    if fs, ok := interface{}(b).(core.FocusState); ok {
        if fs.IsFocused() { return true }
    }
    // Check child recursively
    if b.Child == nil { return false }
    if fs, ok := b.Child.(core.FocusState); ok {
        if fs.IsFocused() { return true }
    }
    if cc, ok := b.Child.(core.ChildContainer); ok {
        focused := false
        cc.VisitChildren(func(w core.Widget) {
            if focused { return }
            if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
                focused = true
                return
            }
            if ccc, ok := w.(core.ChildContainer); ok {
                // nested containers
                ccc.VisitChildren(func(ww core.Widget) {
                    if focused { return }
                    if fs2, ok := ww.(core.FocusState); ok && fs2.IsFocused() {
                        focused = true
                    }
                })
            }
        })
        if focused { return true }
    }
    return false
}

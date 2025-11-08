package core

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel"
	"texelation/texel/theme"
)

// UIManager owns a small widget tree (floating for MVP) and composes to a buffer.
type UIManager struct {
	W, H     int
	widgets  []Widget // z-ordered: later entries draw on top
	bgStyle  tcell.Style
	notifier chan<- bool
	focused  Widget
	buf      [][]texel.Cell
	dirty    []Rect
	lay      Layout
	capture  Widget
}

func NewUIManager() *UIManager {
	tm := theme.Get()
	bg := tm.GetColor("ui", "surface_bg", tcell.ColorBlack)
	fg := tm.GetColor("ui", "surface_fg", tcell.ColorWhite)
	return &UIManager{bgStyle: tcell.StyleDefault.Background(bg).Foreground(fg)}
}

func (u *UIManager) SetRefreshNotifier(ch chan<- bool) { u.notifier = ch }

func (u *UIManager) RequestRefresh() {
	if u.notifier == nil {
		return
	}
	select {
	case u.notifier <- true:
	default:
	}
}

func (u *UIManager) Resize(w, h int) {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	u.W, u.H = w, h
	// Resize framebuffer and invalidate all
	u.buf = nil
	u.InvalidateAll()
}

func (u *UIManager) AddWidget(w Widget) {
	u.widgets = append(u.widgets, w)
}

// SetLayout sets the layout manager (defaults to Absolute).
func (u *UIManager) SetLayout(l Layout) { u.lay = l }

func (u *UIManager) Focus(w Widget) {
	if u.focused == w {
		return
	}
	if u.focused != nil {
		u.focused.Blur()
	}
	u.focused = w
	if u.focused != nil {
		u.focused.Focus()
	}
}

func (u *UIManager) HandleKey(ev *tcell.EventKey) bool {
	// Focus traversal on Tab/Shift-Tab
	if ev.Key() == tcell.KeyTab {
		if ev.Modifiers()&tcell.ModShift != 0 {
			u.focusPrev()
		} else {
			u.focusNext()
		}
		u.InvalidateAll()
		u.RequestRefresh()
		return true
	}

	if u.focused != nil && u.focused.HandleKey(ev) {
		u.InvalidateAll()
		u.RequestRefresh()
		return true
	}
	return false
}

// HandleMouse routes mouse events for click-to-focus and optional capture drags.
func (u *UIManager) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	buttons := ev.Buttons()
	prevIsDown := u.capture != nil
	nowDown := buttons&tcell.Button1 != 0

	// Start capture on press over a widget
	if !prevIsDown && nowDown {
		if w := u.topmostAt(x, y); w != nil {
			u.Focus(w)
			u.capture = w
			if mw, ok := w.(MouseAware); ok {
				_ = mw.HandleMouse(ev)
			}
			u.InvalidateAll()
			u.RequestRefresh()
			return true
		}
		return false
	}

	// While captured, forward all mouse events
	if u.capture != nil {
		if mw, ok := u.capture.(MouseAware); ok {
			_ = mw.HandleMouse(ev)
		}
		// Release on button up
		if prevIsDown && !nowDown {
			u.capture = nil
		}
		u.InvalidateAll()
		u.RequestRefresh()
		return true
	}
	// Wheel-only events over topmost
	if buttons&(tcell.WheelUp|tcell.WheelDown|tcell.WheelLeft|tcell.WheelRight) != 0 {
		if w := u.topmostAt(x, y); w != nil {
			if mw, ok := w.(MouseAware); ok {
				_ = mw.HandleMouse(ev)
				u.InvalidateAll()
				u.RequestRefresh()
				return true
			}
		}
	}
	return false
}

func (u *UIManager) topmostAt(x, y int) Widget {
	for i := len(u.widgets) - 1; i >= 0; i-- {
		w := u.widgets[i]
		if w.HitTest(x, y) {
			return w
		}
	}
	return nil
}

func (u *UIManager) focusNext() {
	if len(u.widgets) == 0 {
		return
	}
	start := -1
	for i, w := range u.widgets {
		if w == u.focused {
			start = i
			break
		}
	}
	n := len(u.widgets)
	for i := 1; i <= n; i++ {
		idx := (start + i) % n
		if u.widgets[idx].Focusable() {
			u.Focus(u.widgets[idx])
			return
		}
	}
}

func (u *UIManager) focusPrev() {
	if len(u.widgets) == 0 {
		return
	}
	start := -1
	for i, w := range u.widgets {
		if w == u.focused {
			start = i
			break
		}
	}
	n := len(u.widgets)
	for i := 1; i <= n; i++ {
		idx := (start - i + n) % n
		if u.widgets[idx].Focusable() {
			u.Focus(u.widgets[idx])
			return
		}
	}
}

// Invalidate marks a region for redraw.
func (u *UIManager) Invalidate(r Rect) {
	// Clip to surface
	if r.X < 0 {
		r.X = 0
	}
	if r.Y < 0 {
		r.Y = 0
	}
	if r.X+r.W > u.W {
		r.W = u.W - r.X
	}
	if r.Y+r.H > u.H {
		r.H = u.H - r.Y
	}
	if r.W <= 0 || r.H <= 0 {
		return
	}
	u.dirty = append(u.dirty, r)
}

// InvalidateAll marks the whole surface for redraw.
func (u *UIManager) InvalidateAll() {
	u.dirty = append(u.dirty, Rect{X: 0, Y: 0, W: u.W, H: u.H})
}

func (u *UIManager) ensureBuffer() {
	if u.buf != nil && len(u.buf) == u.H && (u.H == 0 || len(u.buf[0]) == u.W) {
		return
	}
	u.buf = make([][]texel.Cell, u.H)
	for y := 0; y < u.H; y++ {
		row := make([]texel.Cell, u.W)
		for x := 0; x < u.W; x++ {
			row[x] = texel.Cell{Ch: ' ', Style: u.bgStyle}
		}
		u.buf[y] = row
	}
}

// Render updates dirty regions and returns the framebuffer.
func (u *UIManager) Render() [][]texel.Cell {
	u.ensureBuffer()
	if len(u.dirty) == 0 {
		return u.buf
	}
	// Merge dirties into a single clip for MVP
	x0, y0, x1, y1 := u.W, u.H, 0, 0
	for _, r := range u.dirty {
		if r.X < x0 {
			x0 = r.X
		}
		if r.Y < y0 {
			y0 = r.Y
		}
		if r.X+r.W > x1 {
			x1 = r.X + r.W
		}
		if r.Y+r.H > y1 {
			y1 = r.Y + r.H
		}
	}
	if x0 > x1 || y0 > y1 {
		x0, y0, x1, y1 = 0, 0, 0, 0
	}
	clip := Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
	p := NewPainter(u.buf, clip)
	// Clear dirty region
	p.Fill(clip, ' ', u.bgStyle)
	// Draw widgets intersecting clip
	for _, w := range u.widgets {
		wx, wy := w.Position()
		ww, wh := w.Size()
		wr := Rect{X: wx, Y: wy, W: ww, H: wh}
		if rectsOverlap(wr, clip) {
			w.Draw(p)
		}
	}
	u.dirty = u.dirty[:0]
	return u.buf
}

func rectsOverlap(a, b Rect) bool {
	if a.W <= 0 || a.H <= 0 || b.W <= 0 || b.H <= 0 {
		return false
	}
	ax1 := a.X + a.W
	ay1 := a.Y + a.H
	bx1 := b.X + b.W
	by1 := b.Y + b.H
	return a.X < bx1 && ax1 > b.X && a.Y < by1 && ay1 > b.Y
}

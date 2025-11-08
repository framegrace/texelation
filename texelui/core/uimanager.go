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
	for _, w := range u.widgets {
		// Widgets manage their own geometry; no-op here for absolute layout
		_ = w
	}
}

func (u *UIManager) AddWidget(w Widget) {
	u.widgets = append(u.widgets, w)
}

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
	if u.focused != nil && u.focused.HandleKey(ev) {
		u.RequestRefresh()
		return true
	}
	return false
}

// Render draws into a fresh buffer and returns it.
func (u *UIManager) Render() [][]texel.Cell {
	buf := make([][]texel.Cell, u.H)
	for y := 0; y < u.H; y++ {
		row := make([]texel.Cell, u.W)
		for x := 0; x < u.W; x++ {
			row[x] = texel.Cell{Ch: ' ', Style: u.bgStyle}
		}
		buf[y] = row
	}
	p := NewPainter(buf, Rect{X: 0, Y: 0, W: u.W, H: u.H})
	for _, w := range u.widgets {
		w.Draw(p)
	}
	return buf
}

package core

import "github.com/gdamore/tcell/v2"

// Widget is the minimal contract for drawable UI elements.
type Widget interface {
	SetPosition(x, y int)
	Position() (int, int)
	Resize(w, h int)
	Size() (int, int)
	Draw(p *Painter)
	Focusable() bool
	Focus()
	Blur()
	HandleKey(ev *tcell.EventKey) bool
	HitTest(x, y int) bool
}

// BaseWidget provides common fields/behaviour for widgets.
type BaseWidget struct {
	Rect      Rect
	focused   bool
	enabled   bool
	visible   bool
	focusable bool
}

func (b *BaseWidget) SetPosition(x, y int) { b.Rect.X, b.Rect.Y = x, y }
func (b *BaseWidget) Position() (int, int) { return b.Rect.X, b.Rect.Y }
func (b *BaseWidget) Resize(w, h int) {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	b.Rect.W, b.Rect.H = w, h
}
func (b *BaseWidget) Size() (int, int)    { return b.Rect.W, b.Rect.H }
func (b *BaseWidget) Focusable() bool     { return b.focusable }
func (b *BaseWidget) SetFocusable(f bool) { b.focusable = f }
func (b *BaseWidget) Focus() {
	if b.focusable {
		b.focused = true
	}
}
func (b *BaseWidget) Blur()                             { b.focused = false }
func (b *BaseWidget) IsFocused() bool                   { return b.focused }
func (b *BaseWidget) HitTest(x, y int) bool             { return b.Rect.Contains(x, y) }
func (b *BaseWidget) HandleKey(ev *tcell.EventKey) bool { return false }

// MouseAware widgets can consume mouse events directly.
type MouseAware interface {
	HandleMouse(ev *tcell.EventMouse) bool
}

// InvalidationAware widgets accept an invalidation callback to mark dirty regions.
type InvalidationAware interface {
	SetInvalidator(func(Rect))
}

// ChildContainer allows recursive operations over widget trees without
// depending on concrete widget packages.
type ChildContainer interface {
	VisitChildren(func(Widget))
}

// HitTester allows a container to return the deepest widget under a point.
type HitTester interface {
    WidgetAt(x, y int) Widget
}

// BlinkAware widgets support periodic blink updates (e.g., caret blink).
// UI frameworks can call BlinkTick at a fixed interval; the widget should
// invalidate any regions that need redraw and return immediately.
// (Deprecated) BlinkAware was used for caret blinking and is no longer needed.

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

// ZIndexer is an optional interface for widgets that need z-ordering control.
// Widgets with higher ZIndex are drawn on top of widgets with lower ZIndex.
// Widgets that don't implement this interface default to ZIndex 0.
type ZIndexer interface {
	ZIndex() int
}

// BaseWidget provides common fields/behaviour for widgets.
type BaseWidget struct {
	Rect      Rect
	focused   bool
	focusable bool
	zIndex    int // z-ordering: higher values draw on top
	// Optional focus styling: if enabled, widgets may use FocusedStyle when focused.
	focusStyleEnabled bool
	focusedStyle      tcell.Style
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
func (b *BaseWidget) ZIndex() int                       { return b.zIndex }
func (b *BaseWidget) SetZIndex(z int)                   { b.zIndex = z }

// SetFocusedStyle enables or disables focused styling and sets the focused style value.
func (b *BaseWidget) SetFocusedStyle(style tcell.Style, enabled bool) {
    b.focusedStyle = style
    b.focusStyleEnabled = enabled
}

// EffectiveStyle returns the style to use given a base style, applying focused style
// if the widget is focused and focus styling is enabled.
func (b *BaseWidget) EffectiveStyle(base tcell.Style) tcell.Style {
    if b.focused && b.focusStyleEnabled {
        // Merge: use focused style's FG/BG but preserve other attributes from base
        fFG, fBG, fAttr := b.focusedStyle.Decompose()
        _, _, bAttr := base.Decompose()
        // Combine attributes: keep base attrs and add focused attrs
        return tcell.StyleDefault.Foreground(fFG).Background(fBG).Attributes(bAttr | fAttr)
    }
    return base
}

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

// FocusState is implemented by widgets embedding BaseWidget and allows
// containers to query whether a widget is focused.
type FocusState interface {
    IsFocused() bool
}

// Modal is an optional interface for widgets that can enter a modal state.
// When a modal widget is focused and IsModal() returns true:
// - It receives ALL key events (including Tab, which normally does focus traversal)
// - Clicks outside the modal widget will call DismissModal()
type Modal interface {
	IsModal() bool
	DismissModal()
}

// FocusCycler is implemented by containers that manage focus cycling internally.
// When Tab/Shift-Tab is pressed, the container cycles focus among its children.
// Returns true if focus was cycled, false if exhausted (at boundary).
// Root containers should wrap around; nested containers should return false at boundary.
type FocusCycler interface {
	// CycleFocus moves focus to next (forward=true) or previous (forward=false) child.
	// Returns true if focus was successfully cycled, false if at boundary.
	CycleFocus(forward bool) bool

	// TrapsFocus returns true if this container wraps focus at boundaries
	// (i.e., is a root container). False means it returns false at boundaries
	// to let parent handle focus cycling.
	TrapsFocus() bool
}

// MultilineWidget is implemented by widgets that use Enter internally for newlines.
// UIManager's AdvanceFocusOnEnter will skip focus advancement for these widgets.
type MultilineWidget interface {
	IsMultiline() bool
}

// BlinkAware widgets support periodic blink updates (e.g., caret blink).
// UI frameworks can call BlinkTick at a fixed interval; the widget should
// invalidate any regions that need redraw and return immediately.
// (Deprecated) BlinkAware was used for caret blinking and is no longer needed.

// FocusObserver receives notifications when focus changes in the UIManager.
// This allows widgets like StatusBar to react to focus changes without polling.
type FocusObserver interface {
	// OnFocusChanged is called when the focused widget changes.
	// The focused parameter may be nil if no widget has focus.
	OnFocusChanged(focused Widget)
}

package cards

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel"
)

// WrapApp converts a legacy texel.App into a Card. The legacy app ignores
// the incoming buffer and produces a fresh frame during Render.
func WrapApp(app texel.App) Card {
	return &appAdapter{app: app}
}

// DefaultPipeline constructs the conventional TexelApp pipeline:
// the base texel.App wrapped as a card plus any extra cards, returned
// as a *Pipeline (which implements texel.App).
func DefaultPipeline(app texel.App, extra ...Card) *Pipeline {
	base := WrapApp(app)
	cards := append([]Card{base}, extra...)
	return NewPipeline(nil, cards...)
}

// AppAccessor is implemented by cards that wrap a texel.App directly.
type AppAccessor interface {
	UnderlyingApp() texel.App
}

type appAdapter struct {
	app texel.App
}

var _ Card = (*appAdapter)(nil)
var _ texel.SelectionDeclarer = (*appAdapter)(nil)
var _ texel.MouseWheelHandler = (*appAdapter)(nil)
var _ texel.MouseWheelDeclarer = (*appAdapter)(nil)

func (a *appAdapter) Run() error                             { return a.app.Run() }
func (a *appAdapter) Stop()                                  { a.app.Stop() }
func (a *appAdapter) Resize(cols, rows int)                  { a.app.Resize(cols, rows) }
func (a *appAdapter) Render(_ [][]texel.Cell) [][]texel.Cell { return a.app.Render() }
func (a *appAdapter) HandleKey(ev *tcell.EventKey)           { a.app.HandleKey(ev) }
func (a *appAdapter) SetRefreshNotifier(ch chan<- bool)      { a.app.SetRefreshNotifier(ch) }

// Selection handling delegates to the underlying app when available.
func (a *appAdapter) SelectionStart(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) bool {
	if handler, ok := a.app.(texel.SelectionHandler); ok {
		return handler.SelectionStart(x, y, buttons, modifiers)
	}
	return false
}

func (a *appAdapter) SelectionUpdate(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) {
	if handler, ok := a.app.(texel.SelectionHandler); ok {
		handler.SelectionUpdate(x, y, buttons, modifiers)
	}
}

func (a *appAdapter) SelectionFinish(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) (string, []byte, bool) {
	if handler, ok := a.app.(texel.SelectionHandler); ok {
		return handler.SelectionFinish(x, y, buttons, modifiers)
	}
	return "", nil, false
}

func (a *appAdapter) SelectionCancel() {
	if handler, ok := a.app.(texel.SelectionHandler); ok {
		handler.SelectionCancel()
	}
}

func (a *appAdapter) SelectionEnabled() bool {
	_, ok := a.app.(texel.SelectionHandler)
	return ok
}

// Mouse wheel handling delegates to the underlying app when available.
func (a *appAdapter) HandleMouseWheel(x, y, deltaX, deltaY int, modifiers tcell.ModMask) {
	if handler, ok := a.app.(texel.MouseWheelHandler); ok {
		handler.HandleMouseWheel(x, y, deltaX, deltaY, modifiers)
	}
}

func (a *appAdapter) MouseWheelEnabled() bool {
	if declarer, ok := a.app.(texel.MouseWheelDeclarer); ok {
		return declarer.MouseWheelEnabled()
	}
	_, ok := a.app.(texel.MouseWheelHandler)
	return ok
}

func (a *appAdapter) UnderlyingApp() texel.App {
	return a.app
}

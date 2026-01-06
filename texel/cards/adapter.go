package cards

import (
	texelcore "github.com/framegrace/texelui/core"
	"github.com/gdamore/tcell/v2"
)

// WrapApp converts a legacy texelcore.App into a Card. The legacy app ignores
// the incoming buffer and produces a fresh frame during Render.
func WrapApp(app texelcore.App) Card {
	return &appAdapter{app: app}
}

// AppAccessor is implemented by cards that wrap a texelcore.App directly.
type AppAccessor interface {
	UnderlyingApp() texelcore.App
}

type appAdapter struct {
	app texelcore.App
}

var _ Card = (*appAdapter)(nil)
var _ texelcore.SelectionDeclarer = (*appAdapter)(nil)
var _ texelcore.MouseWheelHandler = (*appAdapter)(nil)
var _ texelcore.MouseWheelDeclarer = (*appAdapter)(nil)

func (a *appAdapter) Run() error                                     { return a.app.Run() }
func (a *appAdapter) Stop()                                          { a.app.Stop() }
func (a *appAdapter) Resize(cols, rows int)                          { a.app.Resize(cols, rows) }
func (a *appAdapter) Render(_ [][]texelcore.Cell) [][]texelcore.Cell { return a.app.Render() }
func (a *appAdapter) HandleKey(ev *tcell.EventKey)                   { a.app.HandleKey(ev) }
func (a *appAdapter) SetRefreshNotifier(ch chan<- bool)              { a.app.SetRefreshNotifier(ch) }

// Selection handling delegates to the underlying app when available.
func (a *appAdapter) SelectionStart(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) bool {
	if handler, ok := a.app.(texelcore.SelectionHandler); ok {
		return handler.SelectionStart(x, y, buttons, modifiers)
	}
	return false
}

func (a *appAdapter) SelectionUpdate(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) {
	if handler, ok := a.app.(texelcore.SelectionHandler); ok {
		handler.SelectionUpdate(x, y, buttons, modifiers)
	}
}

func (a *appAdapter) SelectionFinish(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) (string, []byte, bool) {
	if handler, ok := a.app.(texelcore.SelectionHandler); ok {
		return handler.SelectionFinish(x, y, buttons, modifiers)
	}
	return "", nil, false
}

func (a *appAdapter) SelectionCancel() {
	if handler, ok := a.app.(texelcore.SelectionHandler); ok {
		handler.SelectionCancel()
	}
}

func (a *appAdapter) SelectionEnabled() bool {
	_, ok := a.app.(texelcore.SelectionHandler)
	return ok
}

// Mouse wheel handling delegates to the underlying app when available.
func (a *appAdapter) HandleMouseWheel(x, y, deltaX, deltaY int, modifiers tcell.ModMask) {
	if handler, ok := a.app.(texelcore.MouseWheelHandler); ok {
		handler.HandleMouseWheel(x, y, deltaX, deltaY, modifiers)
	}
}

func (a *appAdapter) MouseWheelEnabled() bool {
	if declarer, ok := a.app.(texelcore.MouseWheelDeclarer); ok {
		return declarer.MouseWheelEnabled()
	}
	_, ok := a.app.(texelcore.MouseWheelHandler)
	return ok
}

func (a *appAdapter) UnderlyingApp() texelcore.App {
	return a.app
}

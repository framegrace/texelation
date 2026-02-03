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
var _ texelcore.MouseHandler = (*appAdapter)(nil)

func (a *appAdapter) Run() error                                     { return a.app.Run() }
func (a *appAdapter) Stop()                                          { a.app.Stop() }
func (a *appAdapter) Resize(cols, rows int)                          { a.app.Resize(cols, rows) }
func (a *appAdapter) Render(_ [][]texelcore.Cell) [][]texelcore.Cell { return a.app.Render() }
func (a *appAdapter) HandleKey(ev *tcell.EventKey)                   { a.app.HandleKey(ev) }
func (a *appAdapter) SetRefreshNotifier(ch chan<- bool)              { a.app.SetRefreshNotifier(ch) }

// HandleMouse delegates to the underlying app when available.
func (a *appAdapter) HandleMouse(ev *tcell.EventMouse) {
	if handler, ok := a.app.(texelcore.MouseHandler); ok {
		handler.HandleMouse(ev)
	}
}

func (a *appAdapter) UnderlyingApp() texelcore.App {
	return a.app
}

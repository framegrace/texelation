package adapter

import (
    "github.com/gdamore/tcell/v2"
    "texelation/texel"
    "texelation/texelui/core"
    "texelation/texelui/widgets"
)

// UIApp adapts a TexelUI UIManager to the texel.App interface.
type UIApp struct {
	title    string
	ui       *core.UIManager
	stopCh   chan struct{}
	refresh  chan<- bool
	onResize func(w, h int)
}

func NewUIApp(title string, ui *core.UIManager) *UIApp {
	if ui == nil {
		ui = core.NewUIManager()
	}
	return &UIApp{title: title, ui: ui, stopCh: make(chan struct{})}
}

func (a *UIApp) Run() error { <-a.stopCh; return nil }

func (a *UIApp) Stop() {
	select {
	case <-a.stopCh:
	default:
		close(a.stopCh)
	}
}

func (a *UIApp) Resize(cols, rows int) {
	a.ui.Resize(cols, rows)
	if a.onResize != nil {
		a.onResize(cols, rows)
	}
}

func (a *UIApp) Render() [][]texel.Cell { return a.ui.Render() }

func (a *UIApp) GetTitle() string {
	if a.title == "" {
		return "TexelUI"
	}
	return a.title
}

func (a *UIApp) HandleKey(ev *tcell.EventKey) { a.ui.HandleKey(ev) }

func (a *UIApp) HandleMouse(ev *tcell.EventMouse) { a.ui.HandleMouse(ev) }

func (a *UIApp) SetRefreshNotifier(ch chan<- bool) { a.refresh = ch; a.ui.SetRefreshNotifier(ch) }

// Expose UI for composition
func (a *UIApp) UI() *core.UIManager { return a.ui }

// NewTextEditorApp constructs a minimal floating TextArea inside a bordered pane.
func NewTextEditorApp(title string) *UIApp {
	ui := core.NewUIManager()
	// base pane
	pane := widgets.NewPane(0, 0, 0, 0, tcell.StyleDefault)
	ui.AddWidget(pane)
	// border + text area
	border := widgets.NewBorder(0, 0, 0, 0, tcell.StyleDefault)
	ta := widgets.NewTextArea(0, 0, 0, 0)
	border.SetChild(ta)
	ui.AddWidget(border)
	ui.Focus(ta)
	app := NewUIApp(title, ui)
	app.onResize = func(w, h int) {
		pane.SetPosition(0, 0)
		pane.Resize(w, h)
		border.SetPosition(0, 0)
		border.Resize(w, h)
	}
	return app
}

// NewDualTextEditorApp constructs a UI with two bordered TextAreas side-by-side to test focus.
func NewDualTextEditorApp(title string) *UIApp {
    ui := core.NewUIManager()

    // Base pane background
    pane := widgets.NewPane(0, 0, 0, 0, tcell.StyleDefault)
    ui.AddWidget(pane)

    // Left editor
    leftBorder := widgets.NewBorder(0, 0, 0, 0, tcell.StyleDefault)
    leftTA := widgets.NewTextArea(0, 0, 0, 0)
    leftBorder.SetChild(leftTA)
    ui.AddWidget(leftBorder)

    // Right editor
    rightBorder := widgets.NewBorder(0, 0, 0, 0, tcell.StyleDefault)
    rightTA := widgets.NewTextArea(0, 0, 0, 0)
    rightBorder.SetChild(rightTA)
    ui.AddWidget(rightBorder)

    // Start focused on left
    ui.Focus(leftTA)

    app := NewUIApp(title, ui)
    app.onResize = func(w, h int) {
        pane.SetPosition(0, 0)
        pane.Resize(w, h)

        // Split vertical into two columns
        lw := w / 2
        rw := w - lw
        leftBorder.SetPosition(0, 0)
        leftBorder.Resize(lw, h)
        rightBorder.SetPosition(lw, 0)
        rightBorder.Resize(rw, h)
    }
    return app
}

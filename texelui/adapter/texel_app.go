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

// NewColorPickerDemoApp constructs a demo for the ColorPicker widget.
func NewColorPickerDemoApp(title string) *UIApp {
    ui := core.NewUIManager()

    // Base pane background
    pane := widgets.NewPane(0, 0, 0, 0, tcell.StyleDefault)
    ui.AddWidget(pane)

    // Title label
    titleLabel := widgets.NewLabel(2, 1, 0, 1, "Color Picker Demo - Press Tab to navigate, Enter to select")
    ui.AddWidget(titleLabel)

    // Color picker 1: All modes enabled
    picker1 := widgets.NewColorPicker(2, 3, widgets.ColorPickerConfig{
        EnableSemantic: true,
        EnablePalette:  true,
        EnableOKLCH:    true,
        Label:          "Accent",
    })
    picker1.SetValue("accent")
    ui.AddWidget(picker1)

    // Color picker 2: Semantic only
    picker2 := widgets.NewColorPicker(2, 5, widgets.ColorPickerConfig{
        EnableSemantic: true,
        EnablePalette:  false,
        EnableOKLCH:    false,
        Label:          "Text Color",
    })
    picker2.SetValue("text.primary")
    ui.AddWidget(picker2)

    // Color picker 3: Palette only
    picker3 := widgets.NewColorPicker(2, 7, widgets.ColorPickerConfig{
        EnableSemantic: false,
        EnablePalette:  true,
        EnableOKLCH:    false,
        Label:          "Palette",
    })
    picker3.SetValue("@mauve")
    ui.AddWidget(picker3)

    // Color picker 4: OKLCH only (custom color)
    picker4 := widgets.NewColorPicker(2, 9, widgets.ColorPickerConfig{
        EnableSemantic: false,
        EnablePalette:  false,
        EnableOKLCH:    true,
        Label:          "Custom",
    })
    picker4.SetValue("#ff6b6b")
    ui.AddWidget(picker4)

    // Status label for showing selection results
    statusLabel := widgets.NewLabel(2, 11, 0, 1, "Click a color picker to expand, then select a color")
    ui.AddWidget(statusLabel)

    // Set up callbacks to update status
    updateStatus := func(result widgets.ColorPickerResult) {
        statusLabel.Text = "Selected: " + result.Source + " (" + result.Mode.String() + ")"
    }
    picker1.OnChange = updateStatus
    picker2.OnChange = updateStatus
    picker3.OnChange = updateStatus
    picker4.OnChange = updateStatus

    // Focus first picker
    ui.Focus(picker1)

    app := NewUIApp(title, ui)
    app.onResize = func(w, h int) {
        pane.SetPosition(0, 0)
        pane.Resize(w, h)

        // Update title label width
        titleLabel.Resize(w-4, 1)
        statusLabel.Resize(w-4, 1)
    }
    return app
}

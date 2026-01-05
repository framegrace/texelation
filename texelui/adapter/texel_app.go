package adapter

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"texelation/texel"
	"texelation/texelui/core"
	"texelation/texelui/primitives"
	"texelation/texelui/scroll"
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
	app := &UIApp{title: title, ui: ui, stopCh: make(chan struct{})}
	app.EnableStatusBar() // Enable status bar by default
	return app
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

// EnableStatusBar creates and enables a status bar.
// Returns the status bar widget for message display.
// The status bar displays key hints (left) and timed messages (right).
func (a *UIApp) EnableStatusBar() *widgets.StatusBar {
	sb := widgets.NewStatusBar(0, 0, 80)
	a.ui.SetStatusBar(sb)
	return sb
}

// StatusBar returns the current status bar widget, or nil if none.
func (a *UIApp) StatusBar() *widgets.StatusBar {
	if sbw := a.ui.StatusBar(); sbw != nil {
		if sb, ok := sbw.(*widgets.StatusBar); ok {
			return sb
		}
	}
	return nil
}

// DisableStatusBar removes and disables the status bar.
func (a *UIApp) DisableStatusBar() {
	a.ui.SetStatusBar(nil)
}

// NewWidgetShowcaseApp creates a tabbed demo showcasing all TexelUI widgets.
// This is the unified demo that replaces individual demos.
func NewWidgetShowcaseApp(title string) *UIApp {
	ui := core.NewUIManager()

	// Create tab layout
	tabs := []primitives.TabItem{
		{Label: "Inputs", ID: "inputs"},
		{Label: "Layouts", ID: "layouts"},
		{Label: "Widgets", ID: "widgets"},
		{Label: "Scrolling", ID: "scrolling"},
	}
	tabLayout := widgets.NewTabLayout(0, 0, 80, 24, tabs)

	app := NewUIApp(title, ui)

	// Get status bar (enabled by default in NewUIApp)
	statusBar := app.StatusBar()

	// === Inputs Tab (wrapped in ScrollPane for tall content) ===
	inputsPane := createInputsTab()
	inputsScroll := scroll.NewScrollPane()
	inputsScroll.Resize(80, 20)
	inputsScroll.SetChild(inputsPane)
	inputsScroll.SetContentHeight(30) // Form is taller than viewport
	tabLayout.SetTabContent(0, inputsScroll)

	// === Layouts Tab ===
	layoutsPane := createLayoutsTab()
	tabLayout.SetTabContent(1, layoutsPane)

	// === Widgets Tab ===
	widgetsPane := createWidgetsTabWithStatusBar(ui, statusBar)
	tabLayout.SetTabContent(2, widgetsPane)

	// === Scrolling Tab (dedicated scroll demo) ===
	scrollingPane := createScrollingTab()
	tabLayout.SetTabContent(3, scrollingPane)

	ui.AddWidget(tabLayout)
	ui.Focus(tabLayout)

	app.onResize = func(w, h int) {
		contentH := ui.ContentHeight()
		tabLayout.SetPosition(0, 0)
		tabLayout.Resize(w, contentH)
	}
	return app
}

// createInputsTab creates the Inputs tab content with Input, TextArea, ComboBox, ColorPicker.
// This form is intentionally tall to demonstrate scrolling in the Inputs tab.
func createInputsTab() *widgets.Pane {
	pane := widgets.NewPane()
	pane.Resize(80, 30) // Tall pane for scrolling demo

	// Helper to position a label
	posLabel := func(x, y int, text string) *widgets.Label {
		l := widgets.NewLabel(text)
		l.SetPosition(x, y)
		return l
	}
	// Helper to position an input
	posInput := func(x, y, w int) *widgets.Input {
		i := widgets.NewInput()
		i.SetPosition(x, y)
		i.Resize(w, 1)
		return i
	}

	// Input field
	nameLabel := posLabel(2, 1, "Name:")
	nameInput := posInput(14, 1, 30)
	nameInput.Placeholder = "Enter your name"
	pane.AddChild(nameLabel)
	pane.AddChild(nameInput)

	// Email field
	emailLabel := posLabel(2, 3, "Email:")
	emailInput := posInput(14, 3, 30)
	emailInput.Placeholder = "user@example.com"
	pane.AddChild(emailLabel)
	pane.AddChild(emailInput)

	// Phone field (new)
	phoneLabel := posLabel(2, 5, "Phone:")
	phoneInput := posInput(14, 5, 30)
	phoneInput.Placeholder = "+1 (555) 000-0000"
	pane.AddChild(phoneLabel)
	pane.AddChild(phoneInput)

	// ComboBox (editable) - for country selection with autocomplete
	countryLabel := posLabel(2, 7, "Country:")
	countries := []string{
		"Argentina", "Australia", "Austria", "Belgium", "Brazil",
		"Canada", "Chile", "China", "Denmark", "Egypt",
		"Finland", "France", "Germany", "Greece", "India",
		"Ireland", "Italy", "Japan", "Mexico", "Netherlands",
		"New Zealand", "Norway", "Poland", "Portugal", "Russia",
		"South Africa", "Spain", "Sweden", "Switzerland",
		"United Kingdom", "United States",
	}
	countryCombo := widgets.NewComboBox(14, 7, 30, countries, true)
	countryCombo.Placeholder = "Type to search..."
	pane.AddChild(countryLabel)
	pane.AddChild(countryCombo)

	// ComboBox (non-editable) - for priority selection
	priorityLabel := posLabel(2, 9, "Priority:")
	priorities := []string{"Low", "Medium", "High", "Critical"}
	priorityCombo := widgets.NewComboBox(14, 9, 20, priorities, false)
	priorityCombo.SetValue("Medium")
	pane.AddChild(priorityLabel)
	pane.AddChild(priorityCombo)

	// TextArea with internal ScrollPane - just set size, scrolling works automatically
	notesLabel := posLabel(2, 11, "Notes:")
	notesBorder := widgets.NewBorder(14, 11, 40, 5, tcell.StyleDefault)
	notesArea := widgets.NewTextArea()
	notesArea.Resize(38, 3) // Size matches border interior
	notesBorder.SetChild(notesArea)
	pane.AddChild(notesLabel)
	pane.AddChild(notesBorder)

	// ColorPicker
	colorLabel := posLabel(2, 17, "Color:")
	colorPicker := widgets.NewColorPicker(14, 17, widgets.ColorPickerConfig{
		EnableSemantic: true,
		EnablePalette:  true,
		EnableOKLCH:    true,
		Label:          "Theme",
	})
	colorPicker.SetValue("accent")
	pane.AddChild(colorLabel)
	pane.AddChild(colorPicker)

	// Additional fields to make form taller (for scrolling demo)
	website := posLabel(2, 19, "Website:")
	websiteInput := posInput(14, 19, 30)
	websiteInput.Placeholder = "https://example.com"
	pane.AddChild(website)
	pane.AddChild(websiteInput)

	company := posLabel(2, 21, "Company:")
	companyInput := posInput(14, 21, 30)
	companyInput.Placeholder = "Company name"
	pane.AddChild(company)
	pane.AddChild(companyInput)

	department := posLabel(2, 23, "Department:")
	depts := []string{"Engineering", "Design", "Marketing", "Sales", "Support", "HR"}
	deptCombo := widgets.NewComboBox(14, 23, 25, depts, false)
	pane.AddChild(department)
	pane.AddChild(deptCombo)

	// Checkboxes for preferences
	prefsLabel := posLabel(2, 25, "Preferences:")
	check1 := widgets.NewCheckbox(2, 26, "Email notifications")
	check2 := widgets.NewCheckbox(2, 27, "SMS notifications")
	check3 := widgets.NewCheckbox(2, 28, "Newsletter subscription")
	pane.AddChild(prefsLabel)
	pane.AddChild(check1)
	pane.AddChild(check2)
	pane.AddChild(check3)

	return pane
}

// createLayoutsTab creates the Layouts tab content demonstrating VBox and HBox.
func createLayoutsTab() *widgets.Pane {
	pane := widgets.NewPane()
	pane.Resize(80, 20)

	// Helper functions
	posLabel := func(x, y int, text string) *widgets.Label {
		l := widgets.NewLabel(text)
		l.SetPosition(x, y)
		return l
	}
	posButton := func(x, y int, text string) *widgets.Button {
		b := widgets.NewButton(text)
		b.SetPosition(x, y)
		return b
	}

	// Title
	title := posLabel(2, 1, "Layout Managers Demo")

	// VBox demonstration
	vboxLabel := posLabel(2, 3, "VBox (vertical):")
	vboxBorder := widgets.NewBorder(2, 4, 25, 8, tcell.StyleDefault)
	vboxPane := widgets.NewPane()
	vboxPane.Resize(23, 6)
	vboxBtn1 := posButton(1, 1, "Button 1")
	vboxBtn2 := posButton(1, 2, "Button 2")
	vboxBtn3 := posButton(1, 3, "Button 3")
	vboxPane.AddChild(vboxBtn1)
	vboxPane.AddChild(vboxBtn2)
	vboxPane.AddChild(vboxBtn3)
	vboxBorder.SetChild(vboxPane)

	// HBox demonstration
	hboxLabel := posLabel(30, 3, "HBox (horizontal):")
	hboxBorder := widgets.NewBorder(30, 4, 40, 4, tcell.StyleDefault)
	hboxPane := widgets.NewPane()
	hboxPane.Resize(38, 2)
	hboxBtn1 := posButton(1, 0, "Left")
	hboxBtn2 := posButton(13, 0, "Center")
	hboxBtn3 := posButton(25, 0, "Right")
	hboxPane.AddChild(hboxBtn1)
	hboxPane.AddChild(hboxBtn2)
	hboxPane.AddChild(hboxBtn3)
	hboxBorder.SetChild(hboxPane)

	// Help text
	helpLabel := posLabel(2, 13, "Tab: navigate between buttons")

	pane.AddChild(title)
	pane.AddChild(vboxLabel)
	pane.AddChild(vboxBorder)
	pane.AddChild(hboxLabel)
	pane.AddChild(hboxBorder)
	pane.AddChild(helpLabel)

	return pane
}

// createWidgetsTab creates the Widgets tab content with Label, Button, Checkbox.
func createWidgetsTab(ui *core.UIManager) *widgets.Pane {
	return createWidgetsTabWithStatusBar(ui, nil)
}

// createWidgetsTabWithStatusBar creates the Widgets tab content with optional status bar integration.
func createWidgetsTabWithStatusBar(ui *core.UIManager, statusBar *widgets.StatusBar) *widgets.Pane {
	pane := widgets.NewPane()
	pane.Resize(80, 20)

	// Helper functions
	posLabel := func(x, y int, text string) *widgets.Label {
		l := widgets.NewLabel(text)
		l.SetPosition(x, y)
		return l
	}
	posButton := func(x, y int, text string) *widgets.Button {
		b := widgets.NewButton(text)
		b.SetPosition(x, y)
		return b
	}

	// Title
	title := posLabel(2, 1, "Basic Widgets Demo")

	// Labels with different alignments
	labelTitle := posLabel(2, 3, "Labels:")
	leftLabel := posLabel(2, 4, "Left aligned")
	leftLabel.Align = widgets.AlignLeft
	centerLabel := posLabel(2, 5, "Center aligned")
	centerLabel.Align = widgets.AlignCenter
	rightLabel := posLabel(2, 6, "Right aligned")
	rightLabel.Align = widgets.AlignRight

	// Buttons
	buttonTitle := posLabel(30, 3, "Buttons:")
	statusLabel := posLabel(30, 8, "Click a button...")

	actionBtn := posButton(30, 4, "Action")
	actionBtn.OnClick = func() {
		statusLabel.Text = "Action button clicked!"
		if statusBar != nil {
			statusBar.ShowSuccess("Action performed successfully!")
		}
	}

	toggleBtn := posButton(30, 5, "Toggle")
	toggleBtn.OnClick = func() {
		statusLabel.Text = "Toggle button clicked!"
		if statusBar != nil {
			statusBar.ShowMessage("Toggle state changed")
		}
	}

	errorBtn := posButton(30, 6, "Error Demo")
	errorBtn.OnClick = func() {
		statusLabel.Text = "Error demo clicked!"
		if statusBar != nil {
			statusBar.ShowError("Something went wrong!")
		}
	}

	// Checkboxes
	checkTitle := posLabel(2, 8, "Checkboxes:")
	check1 := widgets.NewCheckbox(2, 9, "Option A")
	check2 := widgets.NewCheckbox(2, 10, "Option B")
	check3 := widgets.NewCheckbox(2, 11, "Option C (checked)")
	check3.Checked = true

	// Update status on checkbox change
	check1.OnChange = func(checked bool) {
		statusLabel.Text = fmt.Sprintf("Option A: %v", checked)
		if statusBar != nil {
			statusBar.ShowMessage(fmt.Sprintf("Option A: %v", checked))
		}
	}
	check2.OnChange = func(checked bool) {
		statusLabel.Text = fmt.Sprintf("Option B: %v", checked)
		if statusBar != nil {
			statusBar.ShowMessage(fmt.Sprintf("Option B: %v", checked))
		}
	}
	check3.OnChange = func(checked bool) {
		statusLabel.Text = fmt.Sprintf("Option C: %v", checked)
		if statusBar != nil {
			statusBar.ShowWarning(fmt.Sprintf("Option C changed: %v", checked))
		}
	}

	// Help text - note that status bar shows key hints automatically
	helpLabel := posLabel(2, 14, "Key hints shown in status bar below")

	pane.AddChild(title)
	pane.AddChild(labelTitle)
	pane.AddChild(leftLabel)
	pane.AddChild(centerLabel)
	pane.AddChild(rightLabel)
	pane.AddChild(buttonTitle)
	pane.AddChild(actionBtn)
	pane.AddChild(toggleBtn)
	pane.AddChild(errorBtn)
	pane.AddChild(statusLabel)
	pane.AddChild(checkTitle)
	pane.AddChild(check1)
	pane.AddChild(check2)
	pane.AddChild(check3)
	pane.AddChild(helpLabel)

	return pane
}

// createScrollingTab creates the Scrolling tab demonstrating the ScrollPane widget.
// Returns the ScrollPane directly so it receives key events properly.
func createScrollingTab() core.Widget {
	// Helper to position a label
	posLabel := func(x, y int, text string) *widgets.Label {
		l := widgets.NewLabel(text)
		l.SetPosition(x, y)
		return l
	}

	// Create content with title, scrollable area, and instructions
	// Total height: 1 (title) + 1 (desc) + 1 (gap) + 50 (content) + 1 (gap) + 5 (instructions) = 59 rows
	contentPane := widgets.NewPane()
	contentPane.Resize(80, 59)

	// Title and description at top
	title := posLabel(2, 0, "ScrollPane Widget Demo")
	desc := posLabel(2, 1, "Scroll to see 50 rows of content. Use mouse wheel or PgUp/PgDn.")
	contentPane.AddChild(title)
	contentPane.AddChild(desc)

	// 50 rows of scrollable content
	for i := 0; i < 50; i++ {
		label := posLabel(4, 3+i, fmt.Sprintf("Row %02d - This is scrollable content that demonstrates the ScrollPane widget", i+1))
		contentPane.AddChild(label)
	}

	// Instructions at bottom
	instructions := []string{
		"Scroll Controls:",
		"  Mouse wheel: Scroll up/down (3 rows)",
		"  PgUp/PgDn: Scroll one page",
		"  Ctrl+Home: Go to top",
		"  Ctrl+End: Go to bottom",
	}
	for i, text := range instructions {
		help := posLabel(2, 54+i, text)
		contentPane.AddChild(help)
	}

	// Wrap everything in a ScrollPane
	scrollPane := scroll.NewScrollPane()
	scrollPane.Resize(80, 20)
	scrollPane.SetChild(contentPane)
	scrollPane.SetContentHeight(59)

	return scrollPane
}

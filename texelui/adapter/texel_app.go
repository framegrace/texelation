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

	// Enable status bar with key hints and messages
	statusBar := app.EnableStatusBar()

	// === Inputs Tab (wrapped in ScrollPane for tall content) ===
	inputsPane := createInputsTab()
	inputsScroll := scroll.NewScrollPane(0, 0, 80, 20, tcell.StyleDefault)
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
	pane := widgets.NewPane(0, 0, 80, 30, tcell.StyleDefault) // Tall pane for scrolling demo

	// Input field
	nameLabel := widgets.NewLabel(2, 1, 12, 1, "Name:")
	nameInput := widgets.NewInput(14, 1, 30)
	nameInput.Placeholder = "Enter your name"
	pane.AddChild(nameLabel)
	pane.AddChild(nameInput)

	// Email field
	emailLabel := widgets.NewLabel(2, 3, 12, 1, "Email:")
	emailInput := widgets.NewInput(14, 3, 30)
	emailInput.Placeholder = "user@example.com"
	pane.AddChild(emailLabel)
	pane.AddChild(emailInput)

	// Phone field (new)
	phoneLabel := widgets.NewLabel(2, 5, 12, 1, "Phone:")
	phoneInput := widgets.NewInput(14, 5, 30)
	phoneInput.Placeholder = "+1 (555) 000-0000"
	pane.AddChild(phoneLabel)
	pane.AddChild(phoneInput)

	// ComboBox (editable) - for country selection with autocomplete
	countryLabel := widgets.NewLabel(2, 7, 12, 1, "Country:")
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
	priorityLabel := widgets.NewLabel(2, 9, 12, 1, "Priority:")
	priorities := []string{"Low", "Medium", "High", "Critical"}
	priorityCombo := widgets.NewComboBox(14, 9, 20, priorities, false)
	priorityCombo.SetValue("Medium")
	pane.AddChild(priorityLabel)
	pane.AddChild(priorityCombo)

	// TextArea with internal ScrollPane - just set size, scrolling works automatically
	notesLabel := widgets.NewLabel(2, 11, 12, 1, "Notes:")
	notesBorder := widgets.NewBorder(14, 11, 40, 5, tcell.StyleDefault)
	notesArea := widgets.NewTextArea(0, 0, 38, 3) // Size matches border interior
	notesBorder.SetChild(notesArea)
	pane.AddChild(notesLabel)
	pane.AddChild(notesBorder)

	// ColorPicker
	colorLabel := widgets.NewLabel(2, 17, 12, 1, "Color:")
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
	website := widgets.NewLabel(2, 19, 12, 1, "Website:")
	websiteInput := widgets.NewInput(14, 19, 30)
	websiteInput.Placeholder = "https://example.com"
	pane.AddChild(website)
	pane.AddChild(websiteInput)

	company := widgets.NewLabel(2, 21, 12, 1, "Company:")
	companyInput := widgets.NewInput(14, 21, 30)
	companyInput.Placeholder = "Company name"
	pane.AddChild(company)
	pane.AddChild(companyInput)

	department := widgets.NewLabel(2, 23, 12, 1, "Department:")
	depts := []string{"Engineering", "Design", "Marketing", "Sales", "Support", "HR"}
	deptCombo := widgets.NewComboBox(14, 23, 25, depts, false)
	pane.AddChild(department)
	pane.AddChild(deptCombo)

	// Checkboxes for preferences
	prefsLabel := widgets.NewLabel(2, 25, 20, 1, "Preferences:")
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
	pane := widgets.NewPane(0, 0, 80, 20, tcell.StyleDefault)

	// Title
	title := widgets.NewLabel(2, 1, 40, 1, "Layout Managers Demo")

	// VBox demonstration
	vboxLabel := widgets.NewLabel(2, 3, 20, 1, "VBox (vertical):")
	vboxBorder := widgets.NewBorder(2, 4, 25, 8, tcell.StyleDefault)
	vboxPane := widgets.NewPane(0, 0, 23, 6, tcell.StyleDefault)
	vboxBtn1 := widgets.NewButton(1, 1, 20, 1, "Button 1")
	vboxBtn2 := widgets.NewButton(1, 2, 20, 1, "Button 2")
	vboxBtn3 := widgets.NewButton(1, 3, 20, 1, "Button 3")
	vboxPane.AddChild(vboxBtn1)
	vboxPane.AddChild(vboxBtn2)
	vboxPane.AddChild(vboxBtn3)
	vboxBorder.SetChild(vboxPane)

	// HBox demonstration
	hboxLabel := widgets.NewLabel(30, 3, 20, 1, "HBox (horizontal):")
	hboxBorder := widgets.NewBorder(30, 4, 40, 4, tcell.StyleDefault)
	hboxPane := widgets.NewPane(0, 0, 38, 2, tcell.StyleDefault)
	hboxBtn1 := widgets.NewButton(1, 0, 10, 1, "Left")
	hboxBtn2 := widgets.NewButton(13, 0, 10, 1, "Center")
	hboxBtn3 := widgets.NewButton(25, 0, 10, 1, "Right")
	hboxPane.AddChild(hboxBtn1)
	hboxPane.AddChild(hboxBtn2)
	hboxPane.AddChild(hboxBtn3)
	hboxBorder.SetChild(hboxPane)

	// Help text
	helpLabel := widgets.NewLabel(2, 13, 60, 1, "Tab: navigate between buttons")

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
	pane := widgets.NewPane(0, 0, 80, 20, tcell.StyleDefault)

	// Title
	title := widgets.NewLabel(2, 1, 40, 1, "Basic Widgets Demo")

	// Labels with different alignments
	labelTitle := widgets.NewLabel(2, 3, 20, 1, "Labels:")
	leftLabel := widgets.NewLabel(2, 4, 20, 1, "Left aligned")
	leftLabel.Align = widgets.AlignLeft
	centerLabel := widgets.NewLabel(2, 5, 20, 1, "Center aligned")
	centerLabel.Align = widgets.AlignCenter
	rightLabel := widgets.NewLabel(2, 6, 20, 1, "Right aligned")
	rightLabel.Align = widgets.AlignRight

	// Buttons
	buttonTitle := widgets.NewLabel(30, 3, 20, 1, "Buttons:")
	statusLabel := widgets.NewLabel(30, 8, 40, 1, "Click a button...")

	actionBtn := widgets.NewButton(30, 4, 15, 1, "Action")
	actionBtn.OnClick = func() {
		statusLabel.Text = "Action button clicked!"
		if statusBar != nil {
			statusBar.ShowSuccess("Action performed successfully!")
		}
	}

	toggleBtn := widgets.NewButton(30, 5, 15, 1, "Toggle")
	toggleBtn.OnClick = func() {
		statusLabel.Text = "Toggle button clicked!"
		if statusBar != nil {
			statusBar.ShowMessage("Toggle state changed")
		}
	}

	errorBtn := widgets.NewButton(30, 6, 15, 1, "Error Demo")
	errorBtn.OnClick = func() {
		statusLabel.Text = "Error demo clicked!"
		if statusBar != nil {
			statusBar.ShowError("Something went wrong!")
		}
	}

	// Checkboxes
	checkTitle := widgets.NewLabel(2, 8, 20, 1, "Checkboxes:")
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
	helpLabel := widgets.NewLabel(2, 14, 60, 1, "Key hints shown in status bar below")

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
	// Create content with title, scrollable area, and instructions
	// Total height: 1 (title) + 1 (desc) + 1 (gap) + 50 (content) + 1 (gap) + 5 (instructions) = 59 rows
	contentPane := widgets.NewPane(0, 0, 80, 59, tcell.StyleDefault)

	// Title and description at top
	title := widgets.NewLabel(2, 0, 40, 1, "ScrollPane Widget Demo")
	desc := widgets.NewLabel(2, 1, 70, 1, "Scroll to see 50 rows of content. Use mouse wheel or PgUp/PgDn.")
	contentPane.AddChild(title)
	contentPane.AddChild(desc)

	// 50 rows of scrollable content
	for i := 0; i < 50; i++ {
		label := widgets.NewLabel(4, 3+i, 70, 1, fmt.Sprintf("Row %02d - This is scrollable content that demonstrates the ScrollPane widget", i+1))
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
		help := widgets.NewLabel(2, 54+i, 70, 1, text)
		contentPane.AddChild(help)
	}

	// Wrap everything in a ScrollPane
	scrollPane := scroll.NewScrollPane(0, 0, 80, 20, tcell.StyleDefault)
	scrollPane.SetChild(contentPane)
	scrollPane.SetContentHeight(59)

	return scrollPane
}

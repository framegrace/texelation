// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelui-demo/demo.go
// Summary: Widget showcase demo app for TexelUI.
// Usage: Demonstrates all TexelUI widgets in a tabbed interface.

package texeluidemo

import (
	"fmt"

	"texelation/texel"
	"texelation/texelui/adapter"
	"texelation/texelui/core"
	"texelation/texelui/scroll"
	"texelation/texelui/widgets"
)

// New creates a new TexelUI widget showcase demo app.
func New() texel.App {
	ui := core.NewUIManager()
	app := adapter.NewUIApp("TexelUI Widget Showcase", ui)

	// Get status bar (enabled by default in NewUIApp)
	statusBar := app.StatusBar()

	// Create tab panel with simple AddTab API
	tabPanel := widgets.NewTabPanel()

	// === Inputs Tab (wrapped in ScrollPane for tall content) ===
	inputsPane := createInputsTab()
	inputsScroll := scroll.NewScrollPane()
	inputsScroll.Resize(80, 20)
	inputsScroll.SetChild(inputsPane)
	inputsScroll.SetContentHeight(30) // Form is taller than viewport
	tabPanel.AddTab("Inputs", inputsScroll)

	// === Layouts Tab ===
	tabPanel.AddTab("Layouts", createLayoutsTab())

	// === Widgets Tab ===
	tabPanel.AddTab("Widgets", createWidgetsTab(statusBar))

	// === Scrolling Tab (dedicated scroll demo) ===
	tabPanel.AddTab("Scrolling", createScrollingTab())

	ui.AddWidget(tabPanel)
	ui.Focus(tabPanel)

	app.SetOnResize(func(w, h int) {
		contentH := ui.ContentHeight()
		tabPanel.SetPosition(0, 0)
		tabPanel.Resize(w, contentH)
	})
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

	// Phone field
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
	countryCombo := widgets.NewComboBox(countries, true)
	countryCombo.SetPosition(14, 7)
	countryCombo.Resize(30, 1)
	countryCombo.Placeholder = "Type to search..."
	pane.AddChild(countryLabel)
	pane.AddChild(countryCombo)

	// ComboBox (non-editable) - for priority selection
	priorityLabel := posLabel(2, 9, "Priority:")
	priorities := []string{"Low", "Medium", "High", "Critical"}
	priorityCombo := widgets.NewComboBox(priorities, false)
	priorityCombo.SetPosition(14, 9)
	priorityCombo.Resize(20, 1)
	priorityCombo.SetValue("Medium")
	pane.AddChild(priorityLabel)
	pane.AddChild(priorityCombo)

	// TextArea with internal ScrollPane - just set size, scrolling works automatically
	notesLabel := posLabel(2, 11, "Notes:")
	notesBorder := widgets.NewBorder()
	notesBorder.SetPosition(14, 11)
	notesBorder.Resize(40, 5)
	notesArea := widgets.NewTextArea()
	notesArea.Resize(38, 3) // Size matches border interior
	notesBorder.SetChild(notesArea)
	pane.AddChild(notesLabel)
	pane.AddChild(notesBorder)

	// ColorPicker
	colorLabel := posLabel(2, 17, "Color:")
	colorPicker := widgets.NewColorPicker(widgets.ColorPickerConfig{
		EnableSemantic: true,
		EnablePalette:  true,
		EnableOKLCH:    true,
		Label:          "Theme",
	})
	colorPicker.SetPosition(14, 17)
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
	deptCombo := widgets.NewComboBox(depts, false)
	deptCombo.SetPosition(14, 23)
	deptCombo.Resize(25, 1)
	pane.AddChild(department)
	pane.AddChild(deptCombo)

	// Checkboxes for preferences
	prefsLabel := posLabel(2, 25, "Preferences:")
	check1 := widgets.NewCheckbox("Email notifications")
	check1.SetPosition(2, 26)
	check2 := widgets.NewCheckbox("SMS notifications")
	check2.SetPosition(2, 27)
	check3 := widgets.NewCheckbox("Newsletter subscription")
	check3.SetPosition(2, 28)
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
	vboxBorder := widgets.NewBorder()
	vboxBorder.SetPosition(2, 4)
	vboxBorder.Resize(25, 8)
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
	hboxBorder := widgets.NewBorder()
	hboxBorder.SetPosition(30, 4)
	hboxBorder.Resize(40, 4)
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
func createWidgetsTab(statusBar *widgets.StatusBar) *widgets.Pane {
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
	check1 := widgets.NewCheckbox("Option A")
	check1.SetPosition(2, 9)
	check2 := widgets.NewCheckbox("Option B")
	check2.SetPosition(2, 10)
	check3 := widgets.NewCheckbox("Option C (checked)")
	check3.SetPosition(2, 11)
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

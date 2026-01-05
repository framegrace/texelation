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

	// === Inputs Tab (uses Form widget) ===
	inputsForm := createInputsTab()
	inputsScroll := scroll.NewScrollPane()
	inputsScroll.SetChild(inputsForm)
	inputsScroll.SetContentHeight(inputsForm.ContentHeight())
	tabPanel.AddTab("Inputs", inputsScroll)

	// === Layouts Tab (uses VBox/HBox widgets) ===
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

// createInputsTab creates the Inputs tab using the Form widget.
// Demonstrates: Form, Input, ComboBox, TextArea, ColorPicker, Checkbox
func createInputsTab() *widgets.Form {
	form := widgets.NewForm()

	// Basic text inputs
	nameInput := widgets.NewInput()
	nameInput.Placeholder = "Enter your name"
	form.AddField("Name:", nameInput)

	emailInput := widgets.NewInput()
	emailInput.Placeholder = "user@example.com"
	form.AddField("Email:", emailInput)

	phoneInput := widgets.NewInput()
	phoneInput.Placeholder = "+1 (555) 000-0000"
	form.AddField("Phone:", phoneInput)

	// ComboBox (editable) - for country selection with autocomplete
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
	countryCombo.Placeholder = "Type to search..."
	form.AddField("Country:", countryCombo)

	// ComboBox (non-editable) - for priority selection
	priorities := []string{"Low", "Medium", "High", "Critical"}
	priorityCombo := widgets.NewComboBox(priorities, false)
	priorityCombo.SetValue("Medium")
	form.AddField("Priority:", priorityCombo)

	// TextArea (with border for visual containment)
	notesArea := widgets.NewTextArea()
	notesArea.SetText("Line 1: This is a test\nLine 2: More content here\nLine 3: Even more text\nLine 4: Keep scrolling\nLine 5: Almost there\nLine 6: One more\nLine 7: And another\nLine 8: Last line")
	notesBorder := widgets.NewBorder()
	notesBorder.SetChild(notesArea)
	form.AddRow(widgets.FormRow{
		Label:  widgets.NewLabel("Notes:"),
		Field:  notesBorder,
		Height: 5,
	})

	// ColorPicker
	colorPicker := widgets.NewColorPicker(widgets.ColorPickerConfig{
		EnableSemantic: true,
		EnablePalette:  true,
		EnableOKLCH:    true,
		Label:          "Theme",
	})
	colorPicker.SetValue("accent")
	form.AddField("Color:", colorPicker)

	// Additional fields to make form taller (for scrolling demo)
	websiteInput := widgets.NewInput()
	websiteInput.Placeholder = "https://example.com"
	form.AddField("Website:", websiteInput)

	companyInput := widgets.NewInput()
	companyInput.Placeholder = "Company name"
	form.AddField("Company:", companyInput)

	depts := []string{"Engineering", "Design", "Marketing", "Sales", "Support", "HR"}
	deptCombo := widgets.NewComboBox(depts, false)
	form.AddField("Department:", deptCombo)

	// Spacer before checkboxes
	form.AddSpacer(1)

	// Checkboxes as full-width fields
	check1 := widgets.NewCheckbox("Email notifications")
	form.AddFullWidthField(check1, 1)

	check2 := widgets.NewCheckbox("SMS notifications")
	form.AddFullWidthField(check2, 1)

	check3 := widgets.NewCheckbox("Newsletter subscription")
	form.AddFullWidthField(check3, 1)

	return form
}

// createLayoutsTab creates the Layouts tab demonstrating VBox and HBox widgets.
func createLayoutsTab() *widgets.Pane {
	pane := widgets.NewPane()

	// Title
	title := widgets.NewLabel("Layout Widgets Demo - VBox and HBox")
	title.SetPosition(2, 1)
	pane.AddChild(title)

	// === VBox demonstration ===
	vboxLabel := widgets.NewLabel("VBox (vertical stacking):")
	vboxLabel.SetPosition(2, 3)
	pane.AddChild(vboxLabel)

	// Create VBox with buttons
	vbox := widgets.NewVBox()
	vbox.Spacing = 1
	vbox.AddChild(widgets.NewButton("First"))
	vbox.AddChild(widgets.NewButton("Second"))
	vbox.AddChild(widgets.NewButton("Third"))

	// Wrap in border for visibility
	vboxBorder := widgets.NewBorder()
	vboxBorder.SetPosition(2, 4)
	vboxBorder.Resize(20, 8)
	vboxBorder.SetChild(vbox)
	pane.AddChild(vboxBorder)

	// === HBox demonstration ===
	hboxLabel := widgets.NewLabel("HBox (horizontal stacking):")
	hboxLabel.SetPosition(30, 3)
	pane.AddChild(hboxLabel)

	// Create HBox with buttons
	hbox := widgets.NewHBox()
	hbox.Spacing = 2
	hbox.AddChild(widgets.NewButton("Left"))
	hbox.AddChild(widgets.NewButton("Center"))
	hbox.AddChild(widgets.NewButton("Right"))

	// Wrap in border for visibility
	hboxBorder := widgets.NewBorder()
	hboxBorder.SetPosition(30, 4)
	hboxBorder.Resize(35, 3)
	hboxBorder.SetChild(hbox)
	pane.AddChild(hboxBorder)

	// === Nested layout demonstration ===
	nestedLabel := widgets.NewLabel("Nested VBox in HBox:")
	nestedLabel.SetPosition(2, 13)
	pane.AddChild(nestedLabel)

	// Create nested layout: HBox containing two VBoxes
	outerHBox := widgets.NewHBox()
	outerHBox.Spacing = 2

	leftVBox := widgets.NewVBox()
	leftVBox.Spacing = 0
	leftVBox.AddChild(widgets.NewButton("L1"))
	leftVBox.AddChild(widgets.NewButton("L2"))

	rightVBox := widgets.NewVBox()
	rightVBox.Spacing = 0
	rightVBox.AddChild(widgets.NewButton("R1"))
	rightVBox.AddChild(widgets.NewButton("R2"))

	outerHBox.AddChild(leftVBox)
	outerHBox.AddChild(rightVBox)

	nestedBorder := widgets.NewBorder()
	nestedBorder.SetPosition(2, 14)
	nestedBorder.Resize(25, 5)
	nestedBorder.SetChild(outerHBox)
	pane.AddChild(nestedBorder)

	// Help text
	helpLabel := widgets.NewLabel("Tab: navigate between buttons")
	helpLabel.SetPosition(2, 20)
	pane.AddChild(helpLabel)

	return pane
}

// createWidgetsTab creates the Widgets tab content with Label, Button, Checkbox.
func createWidgetsTab(statusBar *widgets.StatusBar) *widgets.Pane {
	pane := widgets.NewPane()

	// Title
	title := widgets.NewLabel("Basic Widgets Demo")
	title.SetPosition(2, 1)
	pane.AddChild(title)

	// Labels section
	labelTitle := widgets.NewLabel("Labels:")
	labelTitle.SetPosition(2, 3)
	pane.AddChild(labelTitle)

	leftLabel := widgets.NewLabel("Left aligned")
	leftLabel.SetPosition(2, 4)
	leftLabel.Align = widgets.AlignLeft
	pane.AddChild(leftLabel)

	centerLabel := widgets.NewLabel("Center aligned")
	centerLabel.SetPosition(2, 5)
	centerLabel.Align = widgets.AlignCenter
	pane.AddChild(centerLabel)

	rightLabel := widgets.NewLabel("Right aligned")
	rightLabel.SetPosition(2, 6)
	rightLabel.Align = widgets.AlignRight
	pane.AddChild(rightLabel)

	// Buttons section
	buttonTitle := widgets.NewLabel("Buttons:")
	buttonTitle.SetPosition(30, 3)
	pane.AddChild(buttonTitle)

	statusLabel := widgets.NewLabel("Click a button...")
	statusLabel.SetPosition(30, 8)
	pane.AddChild(statusLabel)

	actionBtn := widgets.NewButton("Action")
	actionBtn.SetPosition(30, 4)
	actionBtn.OnClick = func() {
		statusLabel.Text = "Action button clicked!"
		if statusBar != nil {
			statusBar.ShowSuccess("Action performed successfully!")
		}
	}
	pane.AddChild(actionBtn)

	toggleBtn := widgets.NewButton("Toggle")
	toggleBtn.SetPosition(30, 5)
	toggleBtn.OnClick = func() {
		statusLabel.Text = "Toggle button clicked!"
		if statusBar != nil {
			statusBar.ShowMessage("Toggle state changed")
		}
	}
	pane.AddChild(toggleBtn)

	errorBtn := widgets.NewButton("Error Demo")
	errorBtn.SetPosition(30, 6)
	errorBtn.OnClick = func() {
		statusLabel.Text = "Error demo clicked!"
		if statusBar != nil {
			statusBar.ShowError("Something went wrong!")
		}
	}
	pane.AddChild(errorBtn)

	// Checkboxes section
	checkTitle := widgets.NewLabel("Checkboxes:")
	checkTitle.SetPosition(2, 8)
	pane.AddChild(checkTitle)

	check1 := widgets.NewCheckbox("Option A")
	check1.SetPosition(2, 9)
	check1.OnChange = func(checked bool) {
		statusLabel.Text = fmt.Sprintf("Option A: %v", checked)
		if statusBar != nil {
			statusBar.ShowMessage(fmt.Sprintf("Option A: %v", checked))
		}
	}
	pane.AddChild(check1)

	check2 := widgets.NewCheckbox("Option B")
	check2.SetPosition(2, 10)
	check2.OnChange = func(checked bool) {
		statusLabel.Text = fmt.Sprintf("Option B: %v", checked)
		if statusBar != nil {
			statusBar.ShowMessage(fmt.Sprintf("Option B: %v", checked))
		}
	}
	pane.AddChild(check2)

	check3 := widgets.NewCheckbox("Option C (checked)")
	check3.SetPosition(2, 11)
	check3.Checked = true
	check3.OnChange = func(checked bool) {
		statusLabel.Text = fmt.Sprintf("Option C: %v", checked)
		if statusBar != nil {
			statusBar.ShowWarning(fmt.Sprintf("Option C changed: %v", checked))
		}
	}
	pane.AddChild(check3)

	// Help text
	helpLabel := widgets.NewLabel("Key hints shown in status bar below")
	helpLabel.SetPosition(2, 14)
	pane.AddChild(helpLabel)

	return pane
}

// createScrollingTab creates the Scrolling tab demonstrating the ScrollPane widget.
func createScrollingTab() core.Widget {
	contentPane := widgets.NewPane()
	contentPane.Resize(80, 59)

	// Title and description
	title := widgets.NewLabel("ScrollPane Widget Demo")
	title.SetPosition(2, 0)
	contentPane.AddChild(title)

	desc := widgets.NewLabel("Scroll to see 50 rows of content. Use mouse wheel or PgUp/PgDn.")
	desc.SetPosition(2, 1)
	contentPane.AddChild(desc)

	// 50 rows of scrollable content
	for i := 0; i < 50; i++ {
		label := widgets.NewLabel(fmt.Sprintf("Row %02d - This is scrollable content that demonstrates the ScrollPane widget", i+1))
		label.SetPosition(4, 3+i)
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
		help := widgets.NewLabel(text)
		help.SetPosition(2, 54+i)
		contentPane.AddChild(help)
	}

	// Wrap in ScrollPane
	scrollPane := scroll.NewScrollPane()
	scrollPane.SetChild(contentPane)
	scrollPane.SetContentHeight(59)

	return scrollPane
}

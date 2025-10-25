// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/welcome/interactive_demo.go
// Summary: Interactive tview demo with dynamic widgets (lists, forms, tables).
// Usage: Demonstrates all interactive tview features.
// Notes: Fully interactive - use arrow keys, Tab, Enter, etc.

package welcome

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"texelation/internal/tviewapps"
	"texelation/texel"
)

// NewInteractiveDemo creates a fully interactive tview demo.
// This demonstrates dynamic features: lists, forms, buttons, tables, input fields.
func NewInteractiveDemo() texel.App {
	return tviewapps.New("Interactive Demo", createInteractiveDemoWidgets)
}

func createInteractiveDemoWidgets() tview.Primitive {
	app := &interactiveDemoApp{}
	return app.createLayout()
}

type interactiveDemoApp struct {
	selectedItem string
	counter      int
	logView      *tview.TextView
	inputField   *tview.InputField
	list         *tview.List
	table        *tview.Table
}

func (a *interactiveDemoApp) createLayout() tview.Primitive {
	// Create title
	title := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)
	title.SetBackgroundColor(tcell.ColorDarkMagenta)
	title.SetText("[yellow::b]Interactive TView Demo[white::-]\n[gray]Use Tab to switch focus, Arrow keys to navigate")

	// Create interactive list
	a.list = a.createList()

	// Create form
	form := a.createForm()

	// Create table
	a.table = a.createTable()

	// Create log view
	a.logView = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetChangedFunc(func() {
			// Auto-scroll to bottom
		})
	a.logView.SetBorder(true).
		SetTitle(" Event Log ").
		SetBorderColor(tcell.ColorYellow)
	a.logView.SetBackgroundColor(tcell.ColorBlack)
	a.log("[green]Interactive demo started")
	a.log("[cyan]Use Tab to switch between widgets")

	// Left panel: list + form
	leftPanel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.list, 0, 1, true).
		AddItem(form, 0, 1, false)

	// Right panel: table + log
	rightPanel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.table, 0, 1, false).
		AddItem(a.logView, 0, 1, false)

	// Main content: left + right
	content := tview.NewFlex().
		AddItem(leftPanel, 0, 1, true).
		AddItem(rightPanel, 0, 1, false)

	// Full layout: title + content
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(title, 3, 0, false).
		AddItem(content, 0, 1, true)

	return layout
}

func (a *interactiveDemoApp) createList() *tview.List {
	list := tview.NewList().
		ShowSecondaryText(true)

	list.SetBackgroundColor(tcell.ColorDarkBlue)
	list.SetBorder(true).
		SetTitle(" Options (use arrows/numbers) ").
		SetBorderColor(tcell.ColorGreen)

	// Add dynamic items
	items := []struct {
		main      string
		secondary string
		shortcut  rune
	}{
		{"Start Process", "Begin background task", '1'},
		{"View Status", "Check current state", '2'},
		{"Configure Settings", "Modify preferences", '3'},
		{"Run Tests", "Execute test suite", '4'},
		{"Clear Log", "Remove all log entries", '5'},
		{"Toggle Mode", "Switch display mode", '6'},
		{"Refresh Data", "Reload information", '7'},
		{"Export Results", "Save to file", '8'},
	}

	for _, item := range items {
		main := item.main
		list.AddItem(main, item.secondary, item.shortcut, func() {
			a.log(fmt.Sprintf("[yellow]Selected: %s", main))
			a.selectedItem = main
			a.updateTable()
		})
	}

	return list
}

func (a *interactiveDemoApp) createForm() *tview.Form {
	form := tview.NewForm()

	form.SetBackgroundColor(tcell.ColorDarkRed)
	form.SetBorder(true).
		SetTitle(" Interactive Form (Tab to navigate) ").
		SetBorderColor(tcell.ColorAqua)

	// Input field
	a.inputField = tview.NewInputField().
		SetLabel("Name: ").
		SetFieldWidth(20).
		SetAcceptanceFunc(tview.InputFieldMaxLength(20))

	form.AddFormItem(a.inputField)

	// Password field
	form.AddPasswordField("Password: ", "", 20, '*', nil)

	// Dropdown
	form.AddDropDown("Priority: ", []string{"Low", "Medium", "High", "Critical"}, 1, func(option string, index int) {
		a.log(fmt.Sprintf("[cyan]Priority changed to: %s", option))
	})

	// Checkbox
	form.AddCheckbox("Enable notifications", true, func(checked bool) {
		a.log(fmt.Sprintf("[cyan]Notifications: %v", checked))
	})

	// Buttons
	form.AddButton("Submit", func() {
		name := a.inputField.GetText()
		a.log(fmt.Sprintf("[green]Form submitted! Name: %s", name))
		a.counter++
		a.updateTable()
	})

	form.AddButton("Reset", func() {
		a.inputField.SetText("")
		a.log("[yellow]Form reset")
	})

	return form
}

func (a *interactiveDemoApp) createTable() *tview.Table {
	table := tview.NewTable().
		SetBorders(true).
		SetFixed(1, 0)

	table.SetBackgroundColor(tcell.ColorDarkGreen)
	table.SetBorder(true).
		SetTitle(" Dynamic Table ").
		SetBorderColor(tcell.ColorPurple)

	// Headers
	headers := []string{"Property", "Value", "Status"}
	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorYellow).
			SetAlign(tview.AlignCenter).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold)
		table.SetCell(0, col, cell)
	}

	a.updateTable()
	return table
}

func (a *interactiveDemoApp) updateTable() {
	// Clear existing rows (keep header)
	for row := a.table.GetRowCount() - 1; row > 0; row-- {
		a.table.RemoveRow(row)
	}

	// Add dynamic data
	data := [][]string{
		{"Counter", fmt.Sprintf("%d", a.counter), "Active"},
		{"Selected", a.selectedItem, "OK"},
		{"Timestamp", time.Now().Format("15:04:05"), "Current"},
		{"Mode", "Interactive", "Enabled"},
	}

	for rowIdx, row := range data {
		for colIdx, cellText := range row {
			color := tcell.ColorWhite
			if colIdx == 2 {
				if cellText == "Active" || cellText == "OK" || cellText == "Enabled" {
					color = tcell.ColorGreen
				} else {
					color = tcell.ColorRed
				}
			}

			cell := tview.NewTableCell(cellText).
				SetTextColor(color).
				SetAlign(tview.AlignLeft)
			a.table.SetCell(rowIdx+1, colIdx, cell)
		}
	}
}

func (a *interactiveDemoApp) log(message string) {
	timestamp := time.Now().Format("15:04:05")
	fmt.Fprintf(a.logView, "[gray]%s[white] %s\n", timestamp, message)
}

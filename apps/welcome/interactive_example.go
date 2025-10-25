// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/welcome/interactive_example.go
// Summary: Example of an interactive tview app (list with key handling).
// Usage: Shows how interactive widgets work automatically with SimpleTViewApp.
// Notes: HandleKey is automatically forwarded to tview - no extra code needed.

package welcome

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"texelation/internal/tviewapps"
	"texelation/texel"
)

// NewInteractiveExample creates an example app with an interactive list.
// Demonstrates that key handling is automatic - no custom code required!
func NewInteractiveExample() texel.App {
	return tviewapps.New("Interactive Example", createInteractiveWidgets).
		WithBaseLayer(tviewapps.DotPattern)
}

// createInteractiveWidgets creates a list widget that responds to keys automatically.
func createInteractiveWidgets() tview.Primitive {
	// Create an interactive list
	list := tview.NewList().
		AddItem("Option 1", "First choice", '1', nil).
		AddItem("Option 2", "Second choice", '2', nil).
		AddItem("Option 3", "Third choice", '3', nil).
		AddItem("Option 4", "Fourth choice", '4', nil).
		AddItem("Option 5", "Fifth choice", '5', nil).
		ShowSecondaryText(true)

	list.SetBackgroundColor(tcell.ColorDarkGreen)
	list.SetBorder(true).
		SetTitle(" Interactive List (use arrows/numbers) ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.ColorYellow)

	// Create a form example
	form := tview.NewForm().
		AddInputField("Name", "", 20, nil, nil).
		AddPasswordField("Password", "", 20, '*', nil).
		AddCheckbox("Remember me", false, nil).
		AddButton("Login", func() {}).
		AddButton("Cancel", func() {})

	form.SetBackgroundColor(tcell.ColorDarkRed)
	form.SetBorder(true).
		SetTitle(" Form Example ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.ColorAqua)

	// Layout: list on left, form on right
	// Both are interactive - tviewapps.SimpleTViewApp handles all key forwarding!
	flex := tview.NewFlex().
		AddItem(list, 0, 1, true).
		AddItem(form, 0, 1, false)

	return flex
}

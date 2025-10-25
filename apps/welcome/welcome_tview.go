// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/welcome/welcome_tview.go
// Summary: TView-based welcome screen implementation.
// Usage: Alternative welcome app using tview for richer UI.
// Notes: Demonstrates tview integration with texelation.

package welcome

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"texelation/internal/tviewbridge"
	"texelation/texel"
)

// NewTViewWelcomeApp creates a welcome screen using tview components.
func NewTViewWelcomeApp() texel.App {
	// Create a TextView with welcome message
	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetWordWrap(true)

	// Set the welcome content with colors and formatting
	textView.SetText(`[yellow::b]Welcome to Texelation![white::-]

[green]A modular text-based desktop environment[white]

Press [yellow]Ctrl-A[white] to enter Control Mode, then:
  [cyan]|[white] or [cyan]-[white]  - Split vertically or horizontally
  [cyan]x[white]       - Close active pane
  [cyan]z[white]       - Zoom/unzoom active pane
  [cyan]w[white]       - Enter swap mode (then use arrows)
  [cyan]1-9[white]     - Switch to workspace N

Press [yellow]Shift-Arrow[white] to navigate panes anytime.
Press [yellow]Ctrl-Arrow[white] to resize panes.
Press [yellow]Ctrl-Q[white] to quit.

[gray]This welcome screen is powered by TView![white]`)

	// Add a border with title
	textView.SetBorder(true).
		SetTitle(" Welcome ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.ColorPurple)

	return tviewbridge.NewTViewApp("Welcome (TView)", textView)
}

// NewTViewDemoForm creates a demo form to showcase tview capabilities.
func NewTViewDemoForm() texel.App {
	form := tview.NewForm().
		AddInputField("Name", "", 20, nil, nil).
		AddDropDown("Theme", []string{"Dark", "Light", "Solarized", "Monokai"}, 0, nil).
		AddCheckbox("Enable animations", true, nil).
		AddCheckbox("Show notifications", false, nil).
		AddButton("Save", func() {
			// Would save settings here
		}).
		AddButton("Cancel", func() {
			// Would cancel here
		})

	form.SetBorder(true).
		SetTitle(" Configuration Demo ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderPadding(1, 1, 2, 2)

	return tviewbridge.NewTViewApp("Config Demo", form)
}

// NewTViewTableDemo creates a demo table to showcase tview table widget.
func NewTViewTableDemo() texel.App {
	table := tview.NewTable().
		SetBorders(true).
		SetFixed(1, 1).
		SetSelectable(true, false)

	// Add headers
	headers := []string{"Feature", "Status", "Priority"}
	for i, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorYellow).
			SetAlign(tview.AlignCenter).
			SetSelectable(false).
			SetExpansion(1)
		table.SetCell(0, i, cell)
	}

	// Add data rows
	data := [][]string{
		{"Client/Server", "✓ Done", "High"},
		{"TView Integration", "⚙ In Progress", "High"},
		{"Configuration UI", "◯ Planned", "Medium"},
		{"Themes System", "✓ Done", "High"},
		{"Workspace Management", "✓ Done", "High"},
		{"Multi-client Support", "◯ Future", "Low"},
	}

	for r, row := range data {
		for c, cell := range row {
			color := tcell.ColorWhite
			if cell == "✓ Done" {
				color = tcell.ColorGreen
			} else if cell == "⚙ In Progress" {
				color = tcell.ColorYellow
			} else if cell == "◯ Planned" || cell == "◯ Future" {
				color = tcell.ColorGray
			}

			tableCell := tview.NewTableCell(cell).
				SetTextColor(color).
				SetAlign(tview.AlignLeft).
				SetExpansion(1)
			table.SetCell(r+1, c, tableCell)
		}
	}

	table.SetBorder(true).
		SetTitle(" Development Status ").
		SetTitleAlign(tview.AlignCenter)

	return tviewbridge.NewTViewApp("Status Table", table)
}

// NewTViewMenuDemo creates a demo menu/list to showcase tview list widget.
func NewTViewMenuDemo() texel.App {
	list := tview.NewList().
		AddItem("Configuration", "Edit application settings", 'c', nil).
		AddItem("Workspaces", "Manage workspace layout", 'w', nil).
		AddItem("Themes", "Select color theme", 't', nil).
		AddItem("Keybindings", "Customize keyboard shortcuts", 'k', nil).
		AddItem("Help", "Show help documentation", 'h', nil).
		AddItem("About", "About Texelation", 'a', nil)

	list.SetBorder(true).
		SetTitle(" Main Menu ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderPadding(1, 1, 2, 2)

	list.SetMainTextColor(tcell.ColorWhite).
		SetSecondaryTextColor(tcell.ColorGray).
		SetSelectedTextColor(tcell.ColorBlack).
		SetSelectedBackgroundColor(tcell.ColorPurple)

	return tviewbridge.NewTViewApp("Menu", list)
}

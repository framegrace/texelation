// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/menu_bar.go
// Summary: Creates a tview menu bar overlay for texelterm.
// Usage: Wrap texelterm with tviewapps.WithOverlay(baseApp, CreateMenuBar).
// Notes: Menu bar is optional overlay on top of terminal output.

package texelterm

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// CreateMenuBar creates a tview menu bar widget.
// This is designed to be overlaid on top of terminal output.
// The menu bar is opaque (blocks terminal), rest is transparent.
func CreateMenuBar() tview.Primitive {
	// Create menu bar text
	menuBar := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)

	// Opaque background so it blocks terminal output underneath
	menuBar.SetBackgroundColor(tcell.ColorDarkBlue)

	menuBar.SetText(" [yellow::b]File[white::-]  [yellow::b]Edit[white::-]  [yellow::b]View[white::-]  [yellow::b]Help[white::-]")

	// Create flex that positions menu at top
	// Top: menu bar (1 row, opaque)
	// Bottom: transparent space (terminal shows through)
	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(menuBar, 1, 0, false).        // Menu bar at top (1 row fixed)
		AddItem(nil, 0, 1, false)             // Transparent rest (terminal shows)

	return flex
}

// CreateBottomStatusBar creates a status bar at the bottom.
// Example of another type of overlay.
func CreateBottomStatusBar() tview.Primitive {
	statusBar := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)

	statusBar.SetBackgroundColor(tcell.ColorDarkGreen)
	statusBar.SetText(" [white::b]Ready[white::-]  |  Line 1, Col 1")

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).            // Transparent (terminal shows)
		AddItem(statusBar, 1, 0, false)       // Status bar at bottom (1 row)

	return flex
}

// CreateMenuAndStatus creates both menu bar and status bar.
// Example of multiple overlays.
func CreateMenuAndStatus() tview.Primitive {
	// Menu at top
	menuBar := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	menuBar.SetBackgroundColor(tcell.ColorDarkBlue)
	menuBar.SetText(" [yellow::b]File[white::-]  [yellow::b]Edit[white::-]  [yellow::b]View[white::-]  [yellow::b]Help[white::-]")

	// Status at bottom
	statusBar := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	statusBar.SetBackgroundColor(tcell.ColorDarkGreen)
	statusBar.SetText(" [white::b]Ready[white::-]  |  Line 1, Col 1")

	// Layout: menu (top 1 row) + transparent middle + status (bottom 1 row)
	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(menuBar, 1, 0, false).        // Top
		AddItem(nil, 0, 1, false).            // Middle (transparent)
		AddItem(statusBar, 1, 0, false)       // Bottom

	return flex
}

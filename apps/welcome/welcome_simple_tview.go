// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/welcome/welcome_simple_tview.go
// Summary: Welcome app using simplified tviewapps wrapper (minimal boilerplate).
// Usage: Shows how to create tview apps with just a widget factory function.
// Notes: All tview integration handled automatically by SimpleTViewApp.

package welcome

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"texelation/internal/tviewapps"
	"texelation/texel"
)

// NewSimpleTView creates a welcome app using the simplified tviewapps wrapper.
// This requires minimal code: just define the widgets and optionally a base layer.
func NewSimpleTView() texel.App {
	return tviewapps.New("Welcome", createWelcomeWidgets).
		WithBaseLayer(tviewapps.DotPattern)
}

// createWelcomeWidgets defines the tview widget hierarchy.
// This is the only function you need to implement - all else is automatic!
func createWelcomeWidgets() tview.Primitive {
	// Create centered text box with solid background
	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetWordWrap(true)

	textView.SetBackgroundColor(tcell.ColorDarkCyan) // Solid/opaque background

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

[gray]Simplified tview integration - zero boilerplate![white]`)

	textView.SetBorder(true).
		SetTitle(" Welcome ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.ColorPurple)

	// Center the text box using Flex layout
	// Flex containers are transparent by default
	flex := tview.NewFlex().
		AddItem(nil, 0, 1, false). // Left spacer
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).        // Top spacer
			AddItem(textView, 18, 0, false).  // Text box (18 rows)
			AddItem(nil, 0, 1, false),        // Bottom spacer
			0, 1, false).
		AddItem(nil, 0, 1, false) // Right spacer

	return flex
}

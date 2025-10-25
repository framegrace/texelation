// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/welcome/welcome_with_tview.go
// Summary: Welcome app with tview overlay (all compositing internal).
// Usage: Self-contained app that manages its own tview overlay.
// Notes: From the server's perspective, this is just a regular App.

package welcome

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"texelation/internal/tviewbridge"
	"texelation/texel"
)

// WelcomeWithTView is a welcome app that composites a tview overlay internally.
type WelcomeWithTView struct {
	width   int
	height  int
	tview   *tviewbridge.TViewApp
	enabled bool
}

// NewWithTView creates a welcome app with tview overlay.
func NewWithTView() texel.App {
	// Create tview welcome screen
	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetWordWrap(true)

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

[gray]This welcome screen is powered by TView (composited by the app)![white]`)

	textView.SetBorder(true).
		SetTitle(" Welcome ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.ColorPurple)

	tviewApp := tviewbridge.NewTViewApp("Welcome", textView)

	return &WelcomeWithTView{
		width:   80,
		height:  24,
		tview:   tviewApp,
		enabled: true,
	}
}

func (w *WelcomeWithTView) Run() error {
	// Initialize tview (does one draw, no continuous loop)
	return w.tview.Run()
}

func (w *WelcomeWithTView) Stop() {
	w.tview.Stop()
}

func (w *WelcomeWithTView) Resize(cols, rows int) {
	w.width = cols
	w.height = rows
	w.tview.Resize(cols, rows)
}

func (w *WelcomeWithTView) Render() [][]texel.Cell {
	// Create base buffer (blank)
	base := make([][]texel.Cell, w.height)
	for y := 0; y < w.height; y++ {
		base[y] = make([]texel.Cell, w.width)
		for x := 0; x < w.width; x++ {
			base[y][x] = texel.Cell{
				Ch:    ' ',
				Style: tcell.StyleDefault,
			}
		}
	}

	// If overlay is disabled, return base only
	if !w.enabled {
		return base
	}

	// Get tview's buffer
	overlayBuffer := w.tview.Render()

	// Composite tview onto base
	return texel.CompositeBuffers(base, overlayBuffer)
}

func (w *WelcomeWithTView) GetTitle() string {
	return "Welcome"
}

func (w *WelcomeWithTView) HandleKey(ev *tcell.EventKey) {
	// For now, tview handles all keys
	w.tview.HandleKey(ev)
}

func (w *WelcomeWithTView) SetRefreshNotifier(ch chan<- bool) {
	w.tview.SetRefreshNotifier(ch)
}

// EnableOverlay enables the tview overlay.
func (w *WelcomeWithTView) EnableOverlay() {
	w.enabled = true
}

// DisableOverlay disables the tview overlay (shows base buffer only).
func (w *WelcomeWithTView) DisableOverlay() {
	w.enabled = false
}

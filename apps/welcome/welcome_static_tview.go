// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/welcome/welcome_static_tview.go
// Summary: Static welcome screen with tview (render once, no events).
// Usage: Self-contained app that renders tview once and caches the buffer.
// Notes: No event processing, no continuous drawing - completely static.

package welcome

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"texelation/internal/tviewbridge"
	"texelation/texel"
)

// StaticTViewWelcome is a welcome app that runs tview in a background goroutine.
// The tview event loop updates a VirtualScreen buffer continuously, and
// Render() reads from that buffer (thread-safe).
type StaticTViewWelcome struct {
	width    int
	height   int
	tviewApp *tviewbridge.TViewApp
}

// NewStaticTView creates a static welcome screen using tview (render once only).
func NewStaticTView() texel.App {
	return &StaticTViewWelcome{
		width:  80,
		height: 24,
	}
}

func (w *StaticTViewWelcome) Run() error {
	// Use current dimensions (may have been set by Resize() before Run())
	if w.width == 0 || w.height == 0 {
		w.width = 80
		w.height = 24
	}

	// Create centered text box with solid background inside, transparent outside
	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetWordWrap(true)

	textView.SetBackgroundColor(tcell.ColorDarkCyan) // Solid background - opaque

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

[gray]Solid box on transparent background![white]`)

	textView.SetBorder(true).
		SetTitle(" Welcome ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.ColorPurple)

	// Use Flex to center the text box (smaller, not full screen)
	flex := tview.NewFlex().
		AddItem(nil, 0, 1, false). // Left spacer
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false). // Top spacer
			AddItem(textView, 18, 0, false). // Text box (18 rows)
			AddItem(nil, 0, 1, false), // Bottom spacer
			0, 1, false).
		AddItem(nil, 0, 1, false) // Right spacer

	// Flex has transparent background by default, so blue dots show through
	// Create tview app that will run in background
	w.tviewApp = tviewbridge.NewTViewApp("Welcome", flex)
	w.tviewApp.Resize(w.width, w.height)

	// Start tview's event loop in background goroutine
	// It will continuously update the VirtualScreen buffer
	return w.tviewApp.Run()
}

func (w *StaticTViewWelcome) Stop() {
	if w.tviewApp != nil {
		w.tviewApp.Stop()
	}
}

func (w *StaticTViewWelcome) Resize(cols, rows int) {
	w.width = cols
	w.height = rows

	// Forward resize to tview app if it exists
	if w.tviewApp != nil {
		w.tviewApp.Resize(cols, rows)
	}
}

func (w *StaticTViewWelcome) Render() [][]texel.Cell {
	// Create base buffer with visible pattern (same as SimpleColoredWelcome)
	// This ensures we never have empty/garbage cells showing through
	if w.width == 0 || w.height == 0 {
		w.width = 80
		w.height = 24
	}

	// Create base layer with dark blue background and dot pattern
	baseBuffer := make([][]texel.Cell, w.height)
	bgStyle := tcell.StyleDefault.Background(tcell.ColorDarkBlue)
	for y := 0; y < w.height; y++ {
		baseBuffer[y] = make([]texel.Cell, w.width)
		for x := 0; x < w.width; x++ {
			var ch rune
			if (x+y)%2 == 0 {
				ch = '·' // Middle dot - visible pattern for debugging
			} else {
				ch = ' '
			}
			baseBuffer[y][x] = texel.Cell{Ch: ch, Style: bgStyle}
		}
	}

	// If tview isn't ready yet, return base buffer
	if w.tviewApp == nil {
		return baseBuffer
	}

	// Get tview buffer and composite it on top of base buffer
	tviewBuffer := w.tviewApp.Render()

	// Composite: overlay tview buffer onto base buffer
	// Treat default background as "transparent" - base layer shows through
	// This allows rich overlay interfaces where tview only draws what it needs
	defaultBg, _, _ := tcell.StyleDefault.Decompose()

	for y := 0; y < len(tviewBuffer) && y < len(baseBuffer); y++ {
		for x := 0; x < len(tviewBuffer[y]) && x < len(baseBuffer[y]); x++ {
			tviewCell := tviewBuffer[y][x]

			// Check if tview cell has default background (treat as transparent)
			_, bg, _ := tviewCell.Style.Decompose()

			// If background is default (transparent), keep base layer
			// Otherwise, overlay tview content
			if bg == defaultBg && tviewCell.Ch == ' ' {
				// Transparent: keep base buffer cell
				// (default bg + space = nothing to draw)
				continue
			} else {
				// Opaque: use tview cell
				baseBuffer[y][x] = tviewCell
			}
		}
	}

	return baseBuffer
}

func (w *StaticTViewWelcome) GetTitle() string {
	return "Welcome"
}

func (w *StaticTViewWelcome) HandleKey(ev *tcell.EventKey) {
	// Static screen - ignore all keys
}

func (w *StaticTViewWelcome) SetRefreshNotifier(ch chan<- bool) {
	// Forward to tview app so it can notify on updates
	if w.tviewApp != nil {
		w.tviewApp.SetRefreshNotifier(ch)
	}
}

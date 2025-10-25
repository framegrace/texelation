// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/tviewapps/overlay.go
// Summary: Adds optional tview overlay on top of existing apps (e.g., menu bars).
// Usage: Wrap any existing texel.App with WithOverlay() to add tview widgets on top.
// Notes: Minimal changes to existing apps - just wrap and define widgets.

package tviewapps

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"texelation/internal/tviewbridge"
	"texelation/texel"
)

// AppWithOverlay wraps an existing texel.App and adds a tview overlay on top.
// The base app renders normally, then tview widgets are composited on top.
type AppWithOverlay struct {
	baseApp       texel.App
	tviewApp      *tviewbridge.TViewApp
	widgetFactory func() tview.Primitive
	width, height int
}

// WithOverlay wraps an existing app with a tview overlay.
// The base app continues to work normally - tview is just added on top.
//
// Example - Add menu bar to texelterm:
//
//	func NewTexelTermWithMenu(title, shell string) texel.App {
//	    baseApp := texelterm.New(title, shell)
//	    return tviewapps.WithOverlay(baseApp, func() tview.Primitive {
//	        return createMenuBar()
//	    })
//	}
func WithOverlay(baseApp texel.App, widgetFactory func() tview.Primitive) texel.App {
	return &AppWithOverlay{
		baseApp:       baseApp,
		widgetFactory: widgetFactory,
		width:         80,
		height:        24,
	}
}

// Run starts both the base app and the tview overlay.
func (a *AppWithOverlay) Run() error {
	// Start base app first
	if err := a.baseApp.Run(); err != nil {
		return err
	}

	// Initialize tview overlay
	if a.width == 0 || a.height == 0 {
		a.width = 80
		a.height = 24
	}

	root := a.widgetFactory()
	a.tviewApp = tviewbridge.NewTViewApp(a.baseApp.GetTitle(), root)
	a.tviewApp.Resize(a.width, a.height)

	// Start tview in background
	return a.tviewApp.Run()
}

// Stop terminates both base app and overlay.
func (a *AppWithOverlay) Stop() {
	if a.tviewApp != nil {
		a.tviewApp.Stop()
	}
	a.baseApp.Stop()
}

// Resize forwards to both base app and overlay.
func (a *AppWithOverlay) Resize(cols, rows int) {
	a.width = cols
	a.height = rows

	a.baseApp.Resize(cols, rows)

	if a.tviewApp != nil {
		a.tviewApp.Resize(cols, rows)
	}
}

// Render returns the composite: base app buffer + tview overlay on top.
// This is the key method - base app renders first, then tview is overlaid.
func (a *AppWithOverlay) Render() [][]texel.Cell {
	// Get base app's buffer (terminal output, game graphics, etc.)
	baseBuffer := a.baseApp.Render()

	// If tview isn't ready, just return base buffer
	if a.tviewApp == nil {
		return baseBuffer
	}

	// Get tview overlay buffer (menu bar, dialogs, etc.)
	overlayBuffer := a.tviewApp.Render()

	// Composite: tview on top of base app
	return compositeOverlay(baseBuffer, overlayBuffer)
}

// GetTitle returns the base app's title.
func (a *AppWithOverlay) GetTitle() string {
	return a.baseApp.GetTitle()
}

// HandleKey forwards keys exclusively to tview overlay.
// Design: When tview overlay is active, ALL keys go to tview only.
// The base app (e.g., texelterm) should NOT receive keys - tview handles everything.
// This prevents double rendering from both apps processing the same keystroke.
func (a *AppWithOverlay) HandleKey(ev *tcell.EventKey) {
	if a.tviewApp != nil {
		a.tviewApp.HandleKey(ev)
	}
	// Do NOT forward to base app - tview handles all input when active
}

// SetRefreshNotifier sets refresh for both base app and overlay.
func (a *AppWithOverlay) SetRefreshNotifier(ch chan<- bool) {
	a.baseApp.SetRefreshNotifier(ch)
	if a.tviewApp != nil {
		a.tviewApp.SetRefreshNotifier(ch)
	}
}

// compositeOverlay overlays tview widgets on top of the base buffer.
// Transparent cells (default bg + space) let the base layer show through.
func compositeOverlay(baseBuffer, overlayBuffer [][]texel.Cell) [][]texel.Cell {
	// Ensure baseBuffer exists
	if len(baseBuffer) == 0 {
		return baseBuffer
	}

	defaultBg, _, _ := tcell.StyleDefault.Decompose()

	for y := 0; y < len(overlayBuffer) && y < len(baseBuffer); y++ {
		for x := 0; x < len(overlayBuffer[y]) && x < len(baseBuffer[y]); x++ {
			overlayCell := overlayBuffer[y][x]
			_, bg, _ := overlayCell.Style.Decompose()

			// If overlay is transparent, keep base layer
			if bg == defaultBg && overlayCell.Ch == ' ' {
				continue
			} else {
				// Overlay is opaque, replace base
				baseBuffer[y][x] = overlayCell
			}
		}
	}

	return baseBuffer
}

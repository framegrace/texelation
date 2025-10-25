// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/tviewapps/simple_app.go
// Summary: Simplified wrapper for creating tview-based apps with minimal boilerplate.
// Usage: Apps use New() to create a complete texel.App from just a widget factory function.
// Notes: Handles all tview integration (Stop, Resize, Render, HandleKey) automatically.

package tviewapps

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"texelation/internal/tviewbridge"
	"texelation/texel"
)

// SimpleTViewApp wraps tview integration with automatic handling of all texel.App methods.
// Users only need to provide a widget factory function and optionally a base layer renderer.
type SimpleTViewApp struct {
	title         string
	width, height int
	tviewApp      *tviewbridge.TViewApp
	widgetFactory func() tview.Primitive
	baseFactory   func(int, int) [][]texel.Cell
	onAppCreated  func(*tview.Application) // Callback after tview app is created
}

// New creates a new SimpleTViewApp with the given title and widget factory.
// The widget factory is called during Run() to create the tview widget tree.
//
// Example:
//
//	app := tviewapps.New("My App", func() tview.Primitive {
//	    textView := tview.NewTextView()
//	    textView.SetText("Hello World!")
//	    return textView
//	})
func New(title string, widgetFactory func() tview.Primitive) *SimpleTViewApp {
	return &SimpleTViewApp{
		title:         title,
		widgetFactory: widgetFactory,
		baseFactory:   defaultBaseLayer,
		width:         80,
		height:        24,
	}
}

// WithBaseLayer sets a custom base layer renderer.
// If not set, a default empty base layer is used.
//
// Example:
//
//	app := tviewapps.New("My App", createWidgets).
//	    WithBaseLayer(func(w, h int) [][]texel.Cell {
//	        return createDotPattern(w, h)
//	    })
func (a *SimpleTViewApp) WithBaseLayer(factory func(int, int) [][]texel.Cell) *SimpleTViewApp {
	a.baseFactory = factory
	return a
}

// WithAppCallback sets a callback that is called after the tview.Application is created.
// This allows direct access to the tview app for advanced features like focus management.
//
// Example:
//
//	app := tviewapps.New("My App", createWidgets).
//	    WithAppCallback(func(app *tview.Application) {
//	        app.SetFocus(myWidget)
//	    })
func (a *SimpleTViewApp) WithAppCallback(callback func(*tview.Application)) *SimpleTViewApp {
	a.onAppCreated = callback
	return a
}

// Run initializes the tview app and starts its event loop.
// This is called automatically by the desktop when the app is launched.
func (a *SimpleTViewApp) Run() error {
	if a.width == 0 || a.height == 0 {
		a.width = 80
		a.height = 24
	}

	// Create tview widgets using the factory
	root := a.widgetFactory()

	// Create tview app wrapper
	a.tviewApp = tviewbridge.NewTViewApp(a.title, root)
	a.tviewApp.Resize(a.width, a.height)

	// Start tview's event loop in background
	if err := a.tviewApp.Run(); err != nil {
		return err
	}

	// Call callback if provided (now that tview app is created)
	if a.onAppCreated != nil {
		app := a.tviewApp.GetApplication()
		if app != nil {
			a.onAppCreated(app)
		}
	}

	return nil
}

// Stop terminates the tview app.
func (a *SimpleTViewApp) Stop() {
	if a.tviewApp != nil {
		a.tviewApp.Stop()
	}
}

// Resize updates the app dimensions and forwards to tview.
func (a *SimpleTViewApp) Resize(cols, rows int) {
	a.width = cols
	a.height = rows

	if a.tviewApp != nil {
		a.tviewApp.Resize(cols, rows)
	}
}

// Render returns the composite buffer: base layer + tview overlay.
// Transparency: tview cells with default background + space are treated as transparent.
func (a *SimpleTViewApp) Render() [][]texel.Cell {
	// Create base layer (always initialized)
	baseBuffer := a.baseFactory(a.width, a.height)

	// If tview isn't ready, return base layer
	if a.tviewApp == nil {
		return baseBuffer
	}

	// Get tview buffer
	tviewBuffer := a.tviewApp.Render()

	// Composite: overlay tview on base buffer
	return compositeBuffers(baseBuffer, tviewBuffer)
}

// GetTitle returns the app title.
func (a *SimpleTViewApp) GetTitle() string {
	return a.title
}

// HandleKey forwards key events to tview for interactive widgets.
// This enables forms, lists, text inputs, etc. to work automatically.
func (a *SimpleTViewApp) HandleKey(ev *tcell.EventKey) {
	if a.tviewApp != nil {
		a.tviewApp.HandleKey(ev)
	}
}

// SetRefreshNotifier forwards the refresh channel to tview.
func (a *SimpleTViewApp) SetRefreshNotifier(ch chan<- bool) {
	if a.tviewApp != nil {
		a.tviewApp.SetRefreshNotifier(ch)
	}
}

// GetTViewApp returns the underlying TViewApp wrapper for advanced use cases.
// This allows direct access to tview.Application for focus management, etc.
func (a *SimpleTViewApp) GetTViewApp() *tviewbridge.TViewApp {
	return a.tviewApp
}

// defaultBaseLayer creates an empty base layer with transparent cells.
func defaultBaseLayer(width, height int) [][]texel.Cell {
	buffer := make([][]texel.Cell, height)
	style := tcell.StyleDefault

	for y := 0; y < height; y++ {
		buffer[y] = make([]texel.Cell, width)
		for x := 0; x < width; x++ {
			buffer[y][x] = texel.Cell{Ch: ' ', Style: style}
		}
	}

	return buffer
}

// compositeBuffers overlays tviewBuffer on top of baseBuffer.
// Cells with default background + space character are treated as transparent.
func compositeBuffers(baseBuffer, tviewBuffer [][]texel.Cell) [][]texel.Cell {
	defaultBg, _, _ := tcell.StyleDefault.Decompose()

	for y := 0; y < len(tviewBuffer) && y < len(baseBuffer); y++ {
		for x := 0; x < len(tviewBuffer[y]) && x < len(baseBuffer[y]); x++ {
			tviewCell := tviewBuffer[y][x]
			_, bg, _ := tviewCell.Style.Decompose()

			// If default bg + space = transparent, keep base layer
			if bg == defaultBg && tviewCell.Ch == ' ' {
				continue
			} else {
				baseBuffer[y][x] = tviewCell
			}
		}
	}

	return baseBuffer
}

// DotPattern creates a base layer with a visible dot pattern.
// Useful for debugging transparency.
func DotPattern(width, height int) [][]texel.Cell {
	buffer := make([][]texel.Cell, height)
	bgStyle := tcell.StyleDefault.Background(tcell.ColorDarkBlue)

	for y := 0; y < height; y++ {
		buffer[y] = make([]texel.Cell, width)
		for x := 0; x < width; x++ {
			var ch rune
			if (x+y)%2 == 0 {
				ch = '·'
			} else {
				ch = ' '
			}
			buffer[y][x] = texel.Cell{Ch: ch, Style: bgStyle}
		}
	}

	return buffer
}

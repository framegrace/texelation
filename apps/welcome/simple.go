// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/welcome/simple.go
// Summary: Simple base welcome app (used under overlay).
// Usage: Provides a simple background for the tview overlay.
// Notes: Minimal app that just shows a blank screen.

package welcome

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel"
)

// SimpleWelcome is a minimal welcome app that serves as a base for overlays.
type SimpleWelcome struct {
	title  string
	width  int
	height int
}

// NewSimple creates a new simple welcome app.
func NewSimple() texel.App {
	return &SimpleWelcome{
		title:  "Welcome",
		width:  80,
		height: 24,
	}
}

func (w *SimpleWelcome) Run() error {
	return nil
}

func (w *SimpleWelcome) Stop() {
}

func (w *SimpleWelcome) Resize(cols, rows int) {
	w.width = cols
	w.height = rows
}

func (w *SimpleWelcome) Render() [][]texel.Cell {
	// Return empty buffer (will be overlaid by tview)
	buffer := make([][]texel.Cell, w.height)
	for y := 0; y < w.height; y++ {
		buffer[y] = make([]texel.Cell, w.width)
		for x := 0; x < w.width; x++ {
			buffer[y][x] = texel.Cell{
				Ch:    ' ',
				Style: tcell.StyleDefault,
			}
		}
	}
	return buffer
}

func (w *SimpleWelcome) GetTitle() string {
	return w.title
}

func (w *SimpleWelcome) HandleKey(ev *tcell.EventKey) {
	// No-op
}

func (w *SimpleWelcome) SetRefreshNotifier(ch chan<- bool) {
	// No-op
}

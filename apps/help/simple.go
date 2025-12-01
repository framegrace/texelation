// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/help/simple.go
// Summary: Simple base help app (used under overlay).
// Usage: Provides a simple background for the tview overlay.
// Notes: Minimal app that just shows a blank screen.

package help

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel"
)

// SimpleHelp is a minimal help app that serves as a base for overlays.
type SimpleHelp struct {
	title  string
	width  int
	height int
}

// NewSimpleHelp creates a new simple help app.
func NewSimpleHelp() texel.App {
	return &SimpleHelp{
		title:  "Help",
		width:  80,
		height: 24,
	}
}

func (w *SimpleHelp) Run() error {
	return nil
}

func (w *SimpleHelp) Stop() {
}

func (w *SimpleHelp) Resize(cols, rows int) {
	w.width = cols
	w.height = rows
}

func (w *SimpleHelp) Render() [][]texel.Cell {
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

func (w *SimpleHelp) GetTitle() string {
	return w.title
}

func (w *SimpleHelp) HandleKey(ev *tcell.EventKey) {
	// No-op
}

func (w *SimpleHelp) SetRefreshNotifier(ch chan<- bool) {
	// No-op
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/welcome/welcome_simple_colored.go
// Summary: Simple colored welcome screen (no tview needed).
// Usage: Creates a static welcome buffer with colors.
// Notes: No dependencies, no event loop, just a static buffer.

package welcome

import (
	"sync"

	"github.com/gdamore/tcell/v2"
	"texelation/texel"
)

// SimpleColoredWelcome is a static welcome screen with colors.
type SimpleColoredWelcome struct {
	width  int
	height int
	buffer [][]texel.Cell
	mu     sync.RWMutex
}

// NewSimpleColored creates a simple colored welcome screen.
func NewSimpleColored() texel.App {
	return &SimpleColoredWelcome{
		width:  80,
		height: 24,
	}
}

func (w *SimpleColoredWelcome) Run() error {
	// Create the buffer immediately
	w.mu.Lock()
	w.renderBuffer()
	w.mu.Unlock()
	return nil
}

func (w *SimpleColoredWelcome) Stop() {
}

func (w *SimpleColoredWelcome) Resize(cols, rows int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.width = cols
	w.height = rows
	if w.buffer != nil {
		w.renderBuffer()
	}
}

func (w *SimpleColoredWelcome) renderBuffer() {
	// Don't render if dimensions are invalid
	if w.width <= 0 || w.height <= 0 {
		w.buffer = make([][]texel.Cell, 0)
		return
	}

	// Create buffer
	w.buffer = make([][]texel.Cell, w.height)
	for y := 0; y < w.height; y++ {
		w.buffer[y] = make([]texel.Cell, w.width)
	}

	// Fill entire buffer with visible pattern to debug rendering issues
	// This helps distinguish between empty/uninitialized cells and properly rendered content
	bgStyle := tcell.StyleDefault.Background(tcell.ColorDarkBlue)
	for y := 0; y < w.height && y < len(w.buffer); y++ {
		for x := 0; x < w.width && x < len(w.buffer[y]); x++ {
			// Create a checkerboard pattern with dots and dashes
			var ch rune
			if (x+y)%2 == 0 {
				ch = '·' // Middle dot
			} else {
				ch = ' '
			}
			w.buffer[y][x] = texel.Cell{Ch: ch, Style: bgStyle}
		}
	}

	// Draw a simple colored welcome message
	yellowBold := tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	green := tcell.StyleDefault.Foreground(tcell.ColorGreen)
	cyan := tcell.StyleDefault.Foreground(tcell.ColorAqua)
	white := tcell.StyleDefault.Foreground(tcell.ColorWhite)
	gray := tcell.StyleDefault.Foreground(tcell.ColorGray)

	lines := []struct {
		y     int
		text  string
		style tcell.Style
	}{
		{2, "Welcome to Texelation!", yellowBold},
		{4, "A modular text-based desktop environment", green},
		{6, "Press Ctrl-A to enter Control Mode, then:", white},
		{7, "  | or -  - Split vertically or horizontally", cyan},
		{8, "  x       - Close active pane", cyan},
		{9, "  z       - Zoom/unzoom active pane", cyan},
		{10, "  w       - Enter swap mode (then use arrows)", cyan},
		{11, "  1-9     - Switch to workspace N", cyan},
		{13, "Press Shift-Arrow to navigate panes anytime.", white},
		{14, "Press Ctrl-Arrow to resize panes.", white},
		{15, "Press Ctrl-Q to quit.", white},
		{17, "This is a simple colored welcome (no tview!).", gray},
	}

	for _, line := range lines {
		// Bounds check: ensure row exists and is the right size
		if line.y < w.height && line.y < len(w.buffer) && len(w.buffer) > 0 && len(w.buffer[line.y]) == w.width {
			// Center the text
			x := (w.width - len(line.text)) / 2
			if x < 0 {
				x = 0
			}
			for i, ch := range line.text {
				if x+i < w.width && x+i < len(w.buffer[line.y]) {
					w.buffer[line.y][x+i] = texel.Cell{Ch: ch, Style: line.style}
				}
			}
		}
	}
}

func (w *SimpleColoredWelcome) Render() [][]texel.Cell {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.buffer == nil {
		// Can't call renderBuffer here as it needs write lock
		// Return empty buffer
		return make([][]texel.Cell, 0)
	}
	return w.buffer
}

func (w *SimpleColoredWelcome) GetTitle() string {
	return "Welcome"
}

func (w *SimpleColoredWelcome) HandleKey(ev *tcell.EventKey) {
}

func (w *SimpleColoredWelcome) SetRefreshNotifier(ch chan<- bool) {
}

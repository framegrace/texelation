// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/help/help.go
// Summary: Implements help capabilities for the help application.
// Usage: Presented on new sessions to guide users through the interface.
// Notes: Displays static content; simple example app.

package help

import (
	"sync"
	"texelation/texel"
	"texelation/texel/cards"
	"texelation/texel/theme"

	"github.com/gdamore/tcell/v2"
)

// helpApp is a simple internal widget that displays a static help message.
type helpApp struct {
	width, height int
	mu            sync.RWMutex
	stop          chan struct{}
	stopOnce      sync.Once
}

// NewHelpApp now returns the App interface for consistency.
func NewHelpApp() texel.App {
	base := &helpApp{stop: make(chan struct{})}
	return cards.NewPipeline(nil, cards.WrapApp(base))
}

func (a *helpApp) Run() error {
	<-a.stop
	return nil
}

func (a *helpApp) Stop() {
	a.stopOnce.Do(func() {
		close(a.stop)
	})
}

func (a *helpApp) Resize(cols, rows int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.width, a.height = cols, rows
}

func (a *helpApp) Render() [][]texel.Cell {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.width <= 0 || a.height <= 0 {
		return [][]texel.Cell{}
	}

	tm := theme.Get()
	textColor := tm.GetColor("welcome", "text_fg", tcell.ColorPurple)
	style := tcell.StyleDefault.Background(tm.GetColor("desktop", "default_bg", tcell.ColorReset).TrueColor()).Foreground(textColor)

	buffer := make([][]texel.Cell, a.height)
	for i := range buffer {
		buffer[i] = make([]texel.Cell, a.width)
		for j := range buffer[i] {
			buffer[i][j] = texel.Cell{Ch: ' ', Style: style}
		}
	}

	messages := []string{
		"Texelation Help",
		"",
		"Global Shortcuts:",
		"  F1           Show this Help",
		"  Ctrl+A L     Open Launcher",
		"  Ctrl+A H     Show this Help",
		"",
		"Control Mode (Ctrl-A):",
		"  |            Split vertically",
		"  -            Split horizontally",
		"  x            Close active pane",
		"  w            Swap panes (then Arrow keys)",
		"  z            Toggle zoom",
		"  1-9          Switch workspaces",
		"  Ctrl+Arrow   Resize panes",
		"  Esc          Exit control mode",
		"",
		"Anytime:",
		"  Shift+Arrow  Move focus",
		"  Ctrl+Q       Quit Texelation",
		"",
		"TexelTerm Tips:",
		"  Mouse wheel            Scroll history",
		"  Shift + wheel          Page through history",
		"  Alt + wheel            Scroll history (line)",
		"  Alt + PgUp/PgDn        Scroll history (pane)",
		"  Drag with mouse        Select & copy text",
		"  Click to focus panes   Activate target pane",
	}

	for i, msg := range messages {
		y := (a.height / 2) - len(messages)/2 + i
		x := (a.width - len(msg)) / 2
		if y >= 0 && y < a.height && x >= 0 {
			for j, ch := range msg {
				if x+j < a.width {
					buffer[y][x+j] = texel.Cell{Ch: ch, Style: style}
				}
			}
		}
	}
	return buffer
}

func (a *helpApp) GetTitle() string {
	return "Help"
}

func (a *helpApp) HandleKey(ev *tcell.EventKey) {
	// This app doesn't handle key presses.
}

func (a *helpApp) SetRefreshNotifier(refreshChan chan<- bool) {
	// No-op
}

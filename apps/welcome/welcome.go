// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/welcome/welcome.go
// Summary: Implements welcome capabilities for the welcome application.
// Usage: Presented on new sessions to guide users through the interface.
// Notes: Displays static content; simple example app.

package welcome

import (
	"sync"
	"texelation/texel"
	"texelation/texel/cards"
	"texelation/texel/theme"

	"github.com/gdamore/tcell/v2"
)

// welcomeApp is a simple internal widget that displays a static welcome message.
type welcomeApp struct {
	width, height int
	mu            sync.RWMutex
}

// NewWelcomeApp now returns the App interface for consistency.
func NewWelcomeApp() texel.App {
	base := &welcomeApp{}
	return cards.NewPipeline(nil, cards.WrapApp(base))
}

func (a *welcomeApp) Run() error {
	// No background process needed for this static app.
	return nil
}

func (a *welcomeApp) Stop() {}

func (a *welcomeApp) Resize(cols, rows int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.width, a.height = cols, rows
}

func (a *welcomeApp) Render() [][]texel.Cell {
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
		"Welcome to Texelation!",
		"",
		"Press 'Ctrl-A' to enter Control Mode, then:",
		"  | or -  - Split vertically or horizontally",
		"  x       - Close active pane",
		"  w       - Enter swap mode (then use arrows)",
		"",
		"Press 'Shift-Arrow' to navigate panes anytime.",
		"Press 'Ctrl-Arrow' to resize panes.",
		"Press 'Ctrl-Q' to quit.",
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

func (a *welcomeApp) GetTitle() string {
	return "Welcome"
}

func (a *welcomeApp) HandleKey(ev *tcell.EventKey) {
	// This app doesn't handle key presses.
}

func (a *welcomeApp) SetRefreshNotifier(refreshChan chan<- bool) {
	// No-op
}

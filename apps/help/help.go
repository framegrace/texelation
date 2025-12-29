// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/help/help.go
// Summary: Implements help capabilities for the help application.
// Usage: Presented on new sessions to guide users through the interface.
// Notes: Displays static content with structured grid layout.

package help

import (
	"sync"
	"texelation/texel"
	"texelation/texel/theme"

	"github.com/gdamore/tcell/v2"
)

// helpEntry represents a key-description pair.
type helpEntry struct {
	key  string
	desc string
}

// helpSection represents a titled section with entries.
type helpSection struct {
	title   string
	entries []helpEntry
}

// helpApp is a simple internal widget that displays a static help message.
type helpApp struct {
	width, height int
	mu            sync.RWMutex
	stop          chan struct{}
	stopOnce      sync.Once
	sections      []helpSection
}

// NewHelpApp returns a simple help display app.
func NewHelpApp() texel.App {
	return &helpApp{
		stop: make(chan struct{}),
		sections: []helpSection{
			{
				title: "Global Shortcuts",
				entries: []helpEntry{
					{"F1", "Show this Help"},
					{"Ctrl+A L", "Open Launcher"},
					{"Ctrl+A H", "Show this Help"},
					{"Ctrl+A F", "Open Config Editor"},
				},
			},
			{
				title: "Control Mode (Ctrl-A)",
				entries: []helpEntry{
					{"|", "Split vertically"},
					{"-", "Split horizontally"},
					{"x", "Close active pane"},
					{"w", "Swap panes (then Arrow keys)"},
					{"z", "Toggle zoom"},
					{"f", "Open Config Editor"},
					{"1-9", "Switch workspaces"},
					{"Ctrl+Arrow", "Resize panes"},
					{"Esc", "Exit control mode"},
				},
			},
			{
				title: "Anytime",
				entries: []helpEntry{
					{"Shift+Arrow", "Move focus"},
					{"Ctrl+F", "Edit config for active app"},
					{"Ctrl+Q", "Quit Texelation"},
				},
			},
			{
				title: "TexelTerm Tips",
				entries: []helpEntry{
					{"Mouse wheel", "Scroll history"},
					{"Shift+wheel", "Page through history"},
					{"Alt+wheel", "Scroll history (line)"},
					{"Alt+PgUp/PgDn", "Scroll history (pane)"},
					{"Drag mouse", "Select & copy text"},
					{"Click", "Focus target pane"},
				},
			},
		},
	}
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

	tm := theme.ForApp("help")
	bgColor := tm.GetColor("desktop", "default_bg", tcell.ColorReset).TrueColor()
	textColor := tm.GetSemanticColor("text.primary")
	dimColor := tm.GetSemanticColor("text.secondary")
	activeColor := tm.GetSemanticColor("text.active")

	baseStyle := tcell.StyleDefault.Background(bgColor).Foreground(textColor)
	titleStyle := tcell.StyleDefault.Background(bgColor).Foreground(activeColor).Bold(true)
	keyStyle := tcell.StyleDefault.Background(bgColor).Foreground(activeColor)
	descStyle := tcell.StyleDefault.Background(bgColor).Foreground(dimColor)

	// Initialize buffer
	buffer := make([][]texel.Cell, a.height)
	for i := range buffer {
		buffer[i] = make([]texel.Cell, a.width)
		for j := range buffer[i] {
			buffer[i][j] = texel.Cell{Ch: ' ', Style: baseStyle}
		}
	}

	// Calculate total height needed
	totalLines := 1 // Main title
	for _, section := range a.sections {
		totalLines += 2 // Empty line + section title
		totalLines += len(section.entries)
	}

	// Calculate key column width (find longest key)
	keyWidth := 0
	for _, section := range a.sections {
		for _, entry := range section.entries {
			if len(entry.key) > keyWidth {
				keyWidth = len(entry.key)
			}
		}
	}
	keyWidth += 2 // Padding

	// Calculate content width and starting position
	contentWidth := keyWidth + 30 // key column + description space
	if contentWidth > a.width-4 {
		contentWidth = a.width - 4
	}
	startX := (a.width - contentWidth) / 2
	if startX < 2 {
		startX = 2
	}

	// Start rendering from vertical center
	startY := (a.height - totalLines) / 2
	if startY < 1 {
		startY = 1
	}
	y := startY

	// Draw main title
	mainTitle := "Texelation Help"
	a.drawCenteredText(buffer, y, mainTitle, titleStyle)
	y += 2

	// Draw each section
	for _, section := range a.sections {
		if y >= a.height {
			break
		}

		// Section title - centered and bold
		a.drawCenteredText(buffer, y, section.title, titleStyle)
		y++

		// Draw entries as two-column grid
		for _, entry := range section.entries {
			if y >= a.height {
				break
			}

			// Key column (right-aligned within its width)
			keyX := startX + keyWidth - len(entry.key) - 1
			if keyX < startX {
				keyX = startX
			}
			a.drawText(buffer, keyX, y, entry.key, keyStyle)

			// Description column
			descX := startX + keyWidth + 1
			a.drawText(buffer, descX, y, entry.desc, descStyle)

			y++
		}

		y++ // Extra space between sections
	}

	return buffer
}

// drawCenteredText draws text centered horizontally on the given row.
func (a *helpApp) drawCenteredText(buffer [][]texel.Cell, y int, text string, style tcell.Style) {
	if y < 0 || y >= a.height {
		return
	}
	x := (a.width - len(text)) / 2
	if x < 0 {
		x = 0
	}
	a.drawText(buffer, x, y, text, style)
}

// drawText draws text at the given position.
func (a *helpApp) drawText(buffer [][]texel.Cell, x, y int, text string, style tcell.Style) {
	if y < 0 || y >= a.height {
		return
	}
	for i, ch := range text {
		if x+i >= 0 && x+i < a.width {
			buffer[y][x+i] = texel.Cell{Ch: ch, Style: style}
		}
	}
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

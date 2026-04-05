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

	"github.com/framegrace/texelation/internal/keybind"
	"github.com/framegrace/texelation/internal/theming"
	texelcore "github.com/framegrace/texelui/core"
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
	title         string
}

// NewHelpApp returns a simple help display app.
// If a keybinding registry is provided, sections are built dynamically
// from the registry so they reflect the user's actual bindings.
func NewHelpApp() texelcore.App {
	return newHelpApp("Help", allSections(nil))
}

// NewHelpAppWithBindings returns a help display that reflects actual keybindings.
func NewHelpAppWithBindings(r *keybind.Registry) texelcore.App {
	return newHelpApp("Help", allSections(r))
}

// NewControlHelpApp returns a compact help overlay showing only control mode commands.
func NewControlHelpApp() texelcore.App {
	return newHelpApp("Control Mode", controlSections(nil))
}

// NewControlHelpAppWithBindings returns control help with actual keybindings.
func NewControlHelpAppWithBindings(r *keybind.Registry) texelcore.App {
	return newHelpApp("Control Mode", controlSections(r))
}

func newHelpApp(title string, sections []helpSection) texelcore.App {
	return &helpApp{
		stop:     make(chan struct{}),
		sections: sections,
		title:    title,
	}
}

// formatKeys returns the key string for an action from the registry,
// or the provided fallback if no registry is available.
func formatKeys(r *keybind.Registry, action keybind.Action, fallback string) string {
	if r == nil {
		return fallback
	}
	keys := r.KeysForAction(action)
	if len(keys) == 0 {
		return fallback
	}
	s := keybind.FormatKeyCombo(keys[0])
	if len(keys) > 1 {
		s += " / " + keybind.FormatKeyCombo(keys[1])
	}
	return s
}

func allSections(r *keybind.Registry) []helpSection {
	return []helpSection{
		{
			title: "Global Shortcuts",
			entries: []helpEntry{
				{formatKeys(r, keybind.Help, "F1"), "Show this Help"},
				{formatKeys(r, keybind.WorkspaceSwitchPrev, "Alt+Left") + ", " + formatKeys(r, keybind.WorkspaceSwitchNext, "Alt+Right"), "Switch workspace"},
				{formatKeys(r, keybind.PaneNavUp, "Shift+Up") + "/Down/Left/Right", "Move pane focus"},
				{formatKeys(r, keybind.PaneResizeUp, "Ctrl+Up") + "/Down/Left/Right", "Resize panes"},
				{formatKeys(r, keybind.ConfigEditor, "F4"), "Edit config for active app"},
				{formatKeys(r, keybind.Screenshot, "F5"), "Save screenshot as PNG"},
				{formatKeys(r, keybind.Screensaver, "Ctrl+S"), "Activate screensaver"},
			},
		},
		controlSection(r),
		{
			title: "Navigation",
			entries: []helpEntry{
				{formatKeys(r, keybind.PaneNavUp, "Shift+Up"), "Focus up (→ tab mode at top)"},
				{formatKeys(r, keybind.PaneNavDown, "Shift+Down"), "Focus down (exits tab mode)"},
				{formatKeys(r, keybind.PaneNavLeft, "Shift+Left") + "/" + formatKeys(r, keybind.PaneNavRight, "Shift+Right"), "Focus left/right (workspace in tab mode)"},
				{"Click tab", "Switch workspace"},
			},
		},
		{
			title: "Terminal",
			entries: []helpEntry{
				{formatKeys(r, keybind.TermSearch, "F3"), "Toggle history search"},
				{formatKeys(r, keybind.TermScrollbar, "F7"), "Toggle scrollbar"},
				{formatKeys(r, keybind.TermTransformer, "F8"), "Toggle transformers"},
				{formatKeys(r, keybind.TermScreenshot, "Ctrl+P"), "Save pane screenshot"},
				{formatKeys(r, keybind.TermScrollPgUp, "Alt+PgUp") + "/" + formatKeys(r, keybind.TermScrollPgDn, "Alt+PgDn"), "Scroll history (page)"},
				{"Mouse wheel", "Scroll history"},
				{"Drag mouse", "Select & copy text"},
			},
		},
	}
}

func controlSection(r *keybind.Registry) helpSection {
	prefix := formatKeys(r, keybind.ControlToggle, "Ctrl+A")
	return helpSection{
		title: "Control Mode (" + prefix + ")",
		entries: []helpEntry{
			{formatKeys(r, keybind.ControlVSplit, "|"), "Split vertically"},
			{formatKeys(r, keybind.ControlHSplit, "-"), "Split horizontally"},
			{formatKeys(r, keybind.ControlClose, "x"), "Close active pane"},
			{formatKeys(r, keybind.ControlSwap, "w"), "Swap panes (then Arrow)"},
			{formatKeys(r, keybind.ControlZoom, "z"), "Toggle zoom"},
			{formatKeys(r, keybind.ControlNewTab, "t"), "New workspace (type name, Enter)"},
			{formatKeys(r, keybind.ControlCloseTab, "X"), "Close workspace (y/n confirm)"},
			{formatKeys(r, keybind.ControlLauncher, "l"), "Open Launcher"},
			{formatKeys(r, keybind.ControlHelp, "h"), "Show Help"},
			{formatKeys(r, keybind.ControlConfig, "f"), "Open Config Editor"},
			{"Esc", "Exit control mode"},
		},
	}
}

func controlSections(r *keybind.Registry) []helpSection {
	return []helpSection{controlSection(r)}
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

func (a *helpApp) Render() [][]texelcore.Cell {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.width <= 0 || a.height <= 0 {
		return [][]texelcore.Cell{}
	}

	tm := theming.ForApp("help")
	bgColor := tm.GetColor("desktop", "default_bg", tcell.ColorReset).TrueColor()
	textColor := tm.GetSemanticColor("text.primary")
	dimColor := tm.GetSemanticColor("text.secondary")
	activeColor := tm.GetSemanticColor("text.active")

	baseStyle := tcell.StyleDefault.Background(bgColor).Foreground(textColor)
	titleStyle := tcell.StyleDefault.Background(bgColor).Foreground(activeColor).Bold(true)
	keyStyle := tcell.StyleDefault.Background(bgColor).Foreground(activeColor)
	descStyle := tcell.StyleDefault.Background(bgColor).Foreground(dimColor)

	// Initialize buffer
	buffer := make([][]texelcore.Cell, a.height)
	for i := range buffer {
		buffer[i] = make([]texelcore.Cell, a.width)
		for j := range buffer[i] {
			buffer[i][j] = texelcore.Cell{Ch: ' ', Style: baseStyle}
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
func (a *helpApp) drawCenteredText(buffer [][]texelcore.Cell, y int, text string, style tcell.Style) {
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
func (a *helpApp) drawText(buffer [][]texelcore.Cell, x, y int, text string, style tcell.Style) {
	if y < 0 || y >= a.height {
		return
	}
	for i, ch := range text {
		if x+i >= 0 && x+i < a.width {
			buffer[y][x+i] = texelcore.Cell{Ch: ch, Style: style}
		}
	}
}

func (a *helpApp) GetTitle() string {
	if a.title != "" {
		return a.title
	}
	return "Help"
}

func (a *helpApp) HandleKey(ev *tcell.EventKey) {
	// This app doesn't handle key presses.
}

func (a *helpApp) SetRefreshNotifier(refreshChan chan<- bool) {
	// No-op
}

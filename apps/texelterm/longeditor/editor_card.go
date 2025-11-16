// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/longeditor/editor_card.go
// Summary: Overlay editor card for long command line editing in texelterm
// Usage: Added to texelterm pipeline to provide better editing for long lines
// Notes: Uses TexelUI TextArea widget for multi-line editing

package longeditor

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"texelation/texel"
	"texelation/texel/cards"
	"texelation/texel/theme"
	"texelation/texelui/core"
	"texelation/texelui/widgets"
)

// EditorCard provides an overlay editor for long command lines
type EditorCard struct {
	active      bool
	ui          *core.UIManager
	textarea    *widgets.TextArea
	border      *widgets.Border
	pane        *widgets.Pane
	width       int
	height      int
	onCommit    func(text string)
	onCancel    func()
	refreshChan chan<- bool
	stopChan    chan struct{}
}

// NewEditorCard creates a new long line editor card
func NewEditorCard(onCommit func(string), onCancel func()) *EditorCard {
	ec := &EditorCard{
		active:   false,
		ui:       core.NewUIManager(),
		onCommit: onCommit,
		onCancel: onCancel,
		stopChan: make(chan struct{}),
	}

	tm := theme.Get()
	bg := tm.GetColor("ui", "overlay_bg", tcell.ColorBlack)
	fg := tm.GetColor("ui", "overlay_fg", tcell.ColorWhite)
	borderColor := tm.GetColor("ui", "overlay_border", tcell.ColorSilver)

	// Create pane as background
	ec.pane = widgets.NewPane(0, 0, 0, 0, tcell.StyleDefault.Background(bg).Foreground(fg))
	ec.ui.AddWidget(ec.pane)

	// Create bordered textarea
	ec.border = widgets.NewBorder(0, 0, 0, 0, tcell.StyleDefault.Foreground(borderColor))
	ec.textarea = widgets.NewTextArea(0, 0, 0, 0)
	ec.border.SetChild(ec.textarea)
	ec.ui.AddWidget(ec.border)
	ec.ui.Focus(ec.textarea)

	return ec
}

// Run implements cards.Card
func (e *EditorCard) Run() error {
	<-e.stopChan
	return nil
}

// Stop implements cards.Card
func (e *EditorCard) Stop() {
	select {
	case <-e.stopChan:
	default:
		close(e.stopChan)
	}
}

// Resize implements cards.Card
func (e *EditorCard) Resize(cols, rows int) {
	e.width = cols
	e.height = rows
	e.ui.Resize(cols, rows)

	// Calculate overlay dimensions (80% width, 60% height, centered)
	overlayWidth := (cols * 8) / 10
	overlayHeight := (rows * 6) / 10
	if overlayWidth < 20 {
		overlayWidth = 20
	}
	if overlayHeight < 5 {
		overlayHeight = 5
	}
	if overlayWidth > cols {
		overlayWidth = cols
	}
	if overlayHeight > rows {
		overlayHeight = rows
	}

	offsetX := (cols - overlayWidth) / 2
	offsetY := (rows - overlayHeight) / 2

	// Position widgets
	e.pane.SetPosition(offsetX, offsetY)
	e.pane.Resize(overlayWidth, overlayHeight)
	e.border.SetPosition(offsetX, offsetY)
	e.border.Resize(overlayWidth, overlayHeight)
}

// Render implements cards.Card
func (e *EditorCard) Render(input [][]texel.Cell) [][]texel.Cell {
	if !e.active {
		// Pass through when inactive
		return input
	}

	// Render input buffer as base, then overlay UI on top
	output := make([][]texel.Cell, len(input))
	for i := range input {
		output[i] = make([]texel.Cell, len(input[i]))
		copy(output[i], input[i])
	}

	// Render UI overlay
	uiBuf := e.ui.Render()
	for y := 0; y < len(uiBuf) && y < len(output); y++ {
		for x := 0; x < len(uiBuf[y]) && x < len(output[y]); x++ {
			// Only overlay non-background cells (simple transparency)
			cell := uiBuf[y][x]
			// For now, always overlay since we have a pane background
			output[y][x] = cell
		}
	}

	return output
}

// HandleKey implements cards.Card
func (e *EditorCard) HandleKey(ev *tcell.EventKey) {
	key := ev.Key()

	// Ctrl+o: Toggle (always check, even when inactive)
	// Ctrl+o produces ASCII control character 0x0F (SI - Shift In)
	if key == tcell.KeyRune && ev.Rune() == '\x0f' {
		e.Toggle()
		return
	}

	if !e.active {
		// Pass through when inactive
		return
	}

	// Handle other special keys when active

	// Enter: Commit
	if key == tcell.KeyEnter {
		text := e.GetText()
		e.Close()
		if e.onCommit != nil {
			e.onCommit(text)
		}
		return
	}

	// Escape: Cancel
	if key == tcell.KeyEsc {
		e.Close()
		if e.onCancel != nil {
			e.onCancel()
		}
		return
	}

	// Passthrough keys: Ctrl+C, Ctrl+D, Ctrl+Z, Ctrl+\
	// These are control characters: Ctrl+C=0x03, Ctrl+D=0x04, Ctrl+Z=0x1A, Ctrl+\=0x1C
	r := ev.Rune()
	if r == '\x03' || r == '\x04' || r == '\x1a' || r == '\x1c' {
		text := e.GetText()
		e.Close()
		if e.onCommit != nil {
			// Commit text first, then the control character will be sent separately
			e.onCommit(text)
		}
		return
	}

	// Otherwise, route to textarea
	e.ui.HandleKey(ev)
}

// SetRefreshNotifier implements cards.Card
func (e *EditorCard) SetRefreshNotifier(ch chan<- bool) {
	e.refreshChan = ch
	e.ui.SetRefreshNotifier(ch)
}

// HandleMessage implements cards.Card
func (e *EditorCard) HandleMessage(msg texel.Message) {
	// No messages to handle for now
}

// Toggle opens or closes the overlay
func (e *EditorCard) Toggle() {
	if e.active {
		e.Close()
	} else {
		e.Open("")
	}
}

// Open activates the overlay with optional initial text
func (e *EditorCard) Open(initialText string) {
	e.active = true
	e.SetText(initialText)
	e.ui.Focus(e.textarea)
	e.requestRefresh()
}

// Close deactivates the overlay
func (e *EditorCard) Close() {
	e.active = false
	e.SetText("") // Clear for next time
	e.requestRefresh()
}

// IsActive returns whether the overlay is currently visible
func (e *EditorCard) IsActive() bool {
	return e.active
}

// SetText sets the textarea content
func (e *EditorCard) SetText(text string) {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	e.textarea.Lines = lines
	e.textarea.CaretX = 0
	e.textarea.CaretY = 0
}

// GetText returns the current textarea content
func (e *EditorCard) GetText() string {
	return strings.Join(e.textarea.Lines, "\n")
}

func (e *EditorCard) requestRefresh() {
	if e.refreshChan == nil {
		return
	}
	select {
	case e.refreshChan <- true:
	default:
	}
}

// RegisterControls implements cards.ControllableCard
func (e *EditorCard) RegisterControls(reg cards.ControlRegistry) error {
	return reg.Register("longeditor.toggle", "Toggle long line editor overlay", func(payload interface{}) error {
		e.Toggle()
		return nil
	})
}

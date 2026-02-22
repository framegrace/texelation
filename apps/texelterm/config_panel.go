// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/config_panel.go
// Summary: Floating config panel overlay for texelterm.

package texelterm

import (
	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/theme"

	"github.com/framegrace/texelation/apps/configeditor"
	"github.com/gdamore/tcell/v2"
)

// ConfigPanel is a floating overlay that hosts an app config editor panel.
type ConfigPanel struct {
	ui      *texelcore.UIManager
	widget  texelcore.Widget
	visible bool
	rect    texelcore.Rect // Panel bounds within terminal buffer
	termW   int            // Full terminal width
	termH   int            // Full terminal height

	onStatus func(msg string, isErr bool)
}

// NewConfigPanel creates a config panel for the given app name.
// onStatus is called with save status messages.
// refreshChan is used to request screen redraws.
func NewConfigPanel(appName string, onStatus func(msg string, isErr bool), refreshChan chan<- bool) *ConfigPanel {
	cp := &ConfigPanel{
		onStatus: onStatus,
	}

	widget, err := configeditor.NewAppConfigPanel(appName, onStatus)
	if err != nil {
		return cp
	}
	cp.widget = widget

	ui := texelcore.NewUIManager()
	ui.SetRootWidget(widget)
	ui.Focus(widget)
	if refreshChan != nil {
		ui.SetRefreshNotifier(refreshChan)
	}
	cp.ui = ui

	return cp
}

// IsVisible returns whether the config panel is currently shown.
func (cp *ConfigPanel) IsVisible() bool {
	return cp != nil && cp.visible && cp.widget != nil
}

// Show makes the config panel visible.
func (cp *ConfigPanel) Show() {
	if cp == nil || cp.widget == nil {
		return
	}
	cp.visible = true
	cp.updateLayout()
}

// Hide hides the config panel.
func (cp *ConfigPanel) Hide() {
	if cp == nil {
		return
	}
	cp.visible = false
}

// Toggle flips visibility.
func (cp *ConfigPanel) Toggle() {
	if cp.IsVisible() {
		cp.Hide()
	} else {
		cp.Show()
	}
}

// Resize updates the panel bounds for the given terminal dimensions.
func (cp *ConfigPanel) Resize(w, h int) {
	if cp == nil {
		return
	}
	cp.termW = w
	cp.termH = h
	if cp.visible {
		cp.updateLayout()
	}
}

// updateLayout computes the panel rect and resizes the UI manager.
func (cp *ConfigPanel) updateLayout() {
	if cp.ui == nil {
		return
	}

	// Panel size: ~70% width, ~80% height, centered
	panelW := cp.termW * 70 / 100
	panelH := cp.termH * 80 / 100
	if panelW < 40 {
		panelW = min(40, cp.termW)
	}
	if panelH < 10 {
		panelH = min(10, cp.termH)
	}

	x := (cp.termW - panelW) / 2
	y := (cp.termH - panelH) / 2

	cp.rect = texelcore.Rect{X: x, Y: y, W: panelW, H: panelH}

	// UIManager content area is inside the border (1 cell inset on each side)
	contentW := panelW - 2
	contentH := panelH - 2
	if contentW < 1 {
		contentW = 1
	}
	if contentH < 1 {
		contentH = 1
	}
	cp.ui.Resize(contentW, contentH)
}

// Render composites the config panel overlay onto the terminal buffer.
func (cp *ConfigPanel) Render(buf [][]texelcore.Cell) {
	if !cp.IsVisible() || cp.ui == nil {
		return
	}

	bufH := len(buf)
	if bufH == 0 {
		return
	}
	bufW := len(buf[0])

	r := cp.rect

	// Get theme colors for the panel
	tm := theme.Get()
	bgColor := tm.GetSemanticColor("bg.surface")
	fgColor := tm.GetSemanticColor("text.primary")
	borderColor := tm.GetSemanticColor("text.muted")
	if bgColor == tcell.ColorDefault {
		bgColor = tcell.NewRGBColor(30, 30, 46)
	}
	if fgColor == tcell.ColorDefault {
		fgColor = tcell.NewRGBColor(205, 214, 244)
	}
	if borderColor == tcell.ColorDefault {
		borderColor = tcell.NewRGBColor(108, 112, 134)
	}

	bgStyle := tcell.StyleDefault.Background(bgColor).Foreground(fgColor)
	borderStyle := tcell.StyleDefault.Background(bgColor).Foreground(borderColor)

	// Fill panel background
	for row := 0; row < r.H; row++ {
		by := r.Y + row
		if by < 0 || by >= bufH {
			continue
		}
		for col := 0; col < r.W; col++ {
			bx := r.X + col
			if bx < 0 || bx >= bufW {
				continue
			}
			buf[by][bx] = texelcore.Cell{Ch: ' ', Style: bgStyle}
		}
	}

	// Draw border
	for col := 1; col < r.W-1; col++ {
		setCell(buf, r.X+col, r.Y, tcell.RuneHLine, borderStyle, bufW, bufH)
		setCell(buf, r.X+col, r.Y+r.H-1, tcell.RuneHLine, borderStyle, bufW, bufH)
	}
	for row := 1; row < r.H-1; row++ {
		setCell(buf, r.X, r.Y+row, tcell.RuneVLine, borderStyle, bufW, bufH)
		setCell(buf, r.X+r.W-1, r.Y+row, tcell.RuneVLine, borderStyle, bufW, bufH)
	}
	setCell(buf, r.X, r.Y, tcell.RuneULCorner, borderStyle, bufW, bufH)
	setCell(buf, r.X+r.W-1, r.Y, tcell.RuneURCorner, borderStyle, bufW, bufH)
	setCell(buf, r.X, r.Y+r.H-1, tcell.RuneLLCorner, borderStyle, bufW, bufH)
	setCell(buf, r.X+r.W-1, r.Y+r.H-1, tcell.RuneLRCorner, borderStyle, bufW, bufH)

	// Draw title bar
	title := " Settings "
	titleX := r.X + (r.W-len(title))/2
	titleStyle := borderStyle.Bold(true).Foreground(fgColor)
	for i, ch := range title {
		setCell(buf, titleX+i, r.Y, ch, titleStyle, bufW, bufH)
	}

	// Draw close hint on right side of title bar
	closeHint := " Esc:Close "
	hintX := r.X + r.W - len(closeHint) - 1
	hintStyle := borderStyle.Foreground(tm.GetSemanticColor("text.muted"))
	for i, ch := range closeHint {
		setCell(buf, hintX+i, r.Y, ch, hintStyle, bufW, bufH)
	}

	// Render UIManager content inside the border
	uiBuf := cp.ui.Render()
	if uiBuf != nil {
		contentX := r.X + 1
		contentY := r.Y + 1
		for row := 0; row < len(uiBuf); row++ {
			by := contentY + row
			if by < 0 || by >= bufH {
				continue
			}
			for col := 0; col < len(uiBuf[row]); col++ {
				bx := contentX + col
				if bx < 0 || bx >= bufW {
					continue
				}
				buf[by][bx] = uiBuf[row][col]
			}
		}
	}
}

// HandleKey processes key events when the panel is visible.
// Returns true if the event was consumed.
func (cp *ConfigPanel) HandleKey(ev *tcell.EventKey) bool {
	if !cp.IsVisible() {
		return false
	}

	// Delegate to UIManager first — widgets like ColorPicker and ComboBox
	// use Escape to collapse their expanded state.
	if cp.ui != nil && cp.ui.HandleKey(ev) {
		return true
	}

	// Escape closes the panel (only if no widget consumed it)
	if ev.Key() == tcell.KeyEscape {
		cp.Hide()
		return true
	}

	return true
}

// HandleMouse processes mouse events when the panel is visible.
// Clicks inside the panel are delegated to UIManager.
// Clicks outside close the panel.
// Returns true if the event was consumed.
func (cp *ConfigPanel) HandleMouse(ev *tcell.EventMouse) bool {
	if !cp.IsVisible() {
		return false
	}

	x, y := ev.Position()

	// Click outside the panel is absorbed (modal behavior)
	if !cp.rect.Contains(x, y) {
		return true
	}

	// Translate coordinates to UIManager space (content area inside border)
	localX := x - cp.rect.X - 1
	localY := y - cp.rect.Y - 1
	if localX < 0 || localY < 0 {
		return true // Click on border
	}

	localEv := tcell.NewEventMouse(localX, localY, ev.Buttons(), ev.Modifiers())
	if cp.ui != nil {
		cp.ui.HandleMouse(localEv)
	}
	return true
}

// setCell writes a cell to the buffer with bounds checking.
func setCell(buf [][]texelcore.Cell, x, y int, ch rune, style tcell.Style, w, h int) {
	if x >= 0 && x < w && y >= 0 && y < h {
		buf[y][x] = texelcore.Cell{Ch: ch, Style: style}
	}
}

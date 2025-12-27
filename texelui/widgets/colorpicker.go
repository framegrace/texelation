// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/widgets/colorpicker.go
// Summary: Color picker widget with semantic, palette, and OKLCH modes.

package widgets

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
	"texelation/texelui/widgets/colorpicker"
)

// ColorPickerMode identifies the selection mode.
type ColorPickerMode int

const (
	ColorModeNone     ColorPickerMode = iota
	ColorModeSemantic                 // Semantic color names (text.primary, etc.)
	ColorModePalette                  // Palette colors (@mauve, etc.)
	ColorModeOKLCH                    // Custom OKLCH picker
)

func (m ColorPickerMode) String() string {
	switch m {
	case ColorModeSemantic:
		return "Semantic"
	case ColorModePalette:
		return "Palette"
	case ColorModeOKLCH:
		return "Custom"
	default:
		return ""
	}
}

// ColorPickerConfig defines which modes are enabled.
type ColorPickerConfig struct {
	EnableSemantic bool
	EnablePalette  bool
	EnableOKLCH    bool
	Label          string // Label shown in collapsed state
}

// ColorPickerResult is returned when a color is selected.
type ColorPickerResult struct {
	Color   tcell.Color
	Mode    ColorPickerMode
	Source  string // e.g., "text.primary", "@lavender", "oklch(0.7,0.15,300)"
	R, G, B int32
}

// focusArea identifies which part of the expanded picker has focus.
type focusArea int

const (
	focusTabBar  focusArea = iota // Focus is on the mode tab bar
	focusContent                  // Focus is on the mode content
)

// ColorPicker is a comprehensive color selection widget.
// Collapsed: shows a 2-char color sample + label
// Expanded: shows tabs for each enabled mode
type ColorPicker struct {
	core.BaseWidget
	config ColorPickerConfig

	// State
	expanded    bool
	currentMode ColorPickerMode
	result      ColorPickerResult
	focus       focusArea // Which area has focus when expanded

	// Mode pickers
	modes      map[ColorPickerMode]colorpicker.ModePicker
	modeOrder  []ColorPickerMode // Order for tab display
	activeMode colorpicker.ModePicker

	// Callbacks
	OnChange func(ColorPickerResult)

	// Invalidation
	inv func(core.Rect)
}

// NewColorPicker creates a color picker at (x, y) with the given configuration.
func NewColorPicker(x, y int, config ColorPickerConfig) *ColorPicker {
	cp := &ColorPicker{
		config:    config,
		expanded:  false,
		modes:     make(map[ColorPickerMode]colorpicker.ModePicker),
		modeOrder: []ColorPickerMode{},
	}

	cp.SetPosition(x, y)
	cp.SetFocusable(true)

	// Initialize enabled modes in order
	if config.EnableSemantic {
		cp.modes[ColorModeSemantic] = colorpicker.NewSemanticPicker()
		cp.modeOrder = append(cp.modeOrder, ColorModeSemantic)
	}
	if config.EnablePalette {
		cp.modes[ColorModePalette] = colorpicker.NewPalettePicker()
		cp.modeOrder = append(cp.modeOrder, ColorModePalette)
	}
	if config.EnableOKLCH {
		cp.modes[ColorModeOKLCH] = colorpicker.NewOKLCHPicker()
		cp.modeOrder = append(cp.modeOrder, ColorModeOKLCH)
	}

	// Ensure at least one mode is enabled - default to OKLCH if none specified
	if len(cp.modeOrder) == 0 {
		cp.modes[ColorModeOKLCH] = colorpicker.NewOKLCHPicker()
		cp.modeOrder = append(cp.modeOrder, ColorModeOKLCH)
	}

	// Set initial mode to first available
	cp.currentMode = cp.modeOrder[0]
	cp.activeMode = cp.modes[cp.currentMode]

	// Get initial color from first mode
	r := cp.activeMode.GetResult()
	cp.result = ColorPickerResult{
		Color:  r.Color,
		Mode:   cp.currentMode,
		Source: r.Source,
		R:      r.R,
		G:      r.G,
		B:      r.B,
	}

	// Set initial focused style from theme
	// Use border.focus for the border line color (foreground), keep bg.surface as background
	tm := theme.Get()
	focusFg := tm.GetSemanticColor("border.focus")
	focusBg := tm.GetSemanticColor("bg.surface")
	cp.SetFocusedStyle(tcell.StyleDefault.Foreground(focusFg).Background(focusBg), true)

	cp.calculateSize()

	return cp
}

// SetInvalidator allows the UI manager to inject a dirty-region invalidator.
func (cp *ColorPicker) SetInvalidator(fn func(core.Rect)) {
	cp.inv = fn
}

// SetValue sets the current color by parsing a color string.
// Supported formats: "text.primary" (semantic), "@mauve" (palette), "#ff00ff" (hex)
func (cp *ColorPicker) SetValue(colorStr string) {
	tm := theme.Get()

	var resolvedColor tcell.Color
	var mode ColorPickerMode
	var source string

	if len(colorStr) > 0 && colorStr[0] == '@' {
		// Palette color
		name := colorStr[1:]
		resolvedColor = theme.ResolveColorName(name)
		mode = ColorModePalette
		source = colorStr
	} else if len(colorStr) > 0 && colorStr[0] == '#' {
		// Hex color -> use OKLCH mode
		resolvedColor = theme.HexColor(colorStr).ToTcell()
		mode = ColorModeOKLCH
		source = colorStr
	} else {
		// Try as semantic color
		resolvedColor = tm.GetSemanticColor(colorStr)
		if resolvedColor != tcell.ColorDefault {
			mode = ColorModeSemantic
			source = colorStr
		} else {
			// Fallback to hex
			resolvedColor = theme.HexColor(colorStr).ToTcell()
			mode = ColorModeOKLCH
			source = colorStr
		}
	}

	r, g, b := resolvedColor.RGB()
	cp.result = ColorPickerResult{
		Color:  resolvedColor,
		Mode:   mode,
		Source: source,
		R:      r,
		G:      g,
		B:      b,
	}

	// Switch to appropriate mode if available
	if _, ok := cp.modes[mode]; ok {
		cp.currentMode = mode
		cp.activeMode = cp.modes[mode]
		cp.activeMode.SetColor(resolvedColor)
	}

	cp.invalidate()
}

// GetResult returns the current color selection.
func (cp *ColorPicker) GetResult() ColorPickerResult {
	return cp.result
}

// Toggle expands or collapses the picker.
func (cp *ColorPicker) Toggle() {
	cp.expanded = !cp.expanded
	// When expanded, raise z-index so picker draws on top of other widgets
	if cp.expanded {
		cp.SetZIndex(100) // High z-index for overlay
	} else {
		cp.SetZIndex(0) // Normal z-index when collapsed
	}
	cp.calculateSize()
	cp.invalidate()
}

// Expand shows the color picker modes.
func (cp *ColorPicker) Expand() {
	if !cp.expanded {
		cp.Toggle()
	}
}

// Collapse hides the color picker modes.
func (cp *ColorPicker) Collapse() {
	if cp.expanded {
		cp.Toggle()
	}
}

// IsModal returns true when the picker is expanded (modal state).
// Implements core.Modal interface.
func (cp *ColorPicker) IsModal() bool {
	return cp.expanded
}

// DismissModal collapses the picker when clicking outside.
// Implements core.Modal interface.
func (cp *ColorPicker) DismissModal() {
	cp.Collapse()
}

// Draw renders the color picker.
func (cp *ColorPicker) Draw(painter *core.Painter) {
	if cp.expanded {
		cp.drawExpanded(painter)
	} else {
		cp.drawCollapsed(painter)
	}
}

// drawCollapsed renders: [█A] source
func (cp *ColorPicker) drawCollapsed(painter *core.Painter) {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	globalBg := tm.GetSemanticColor("bg.base")
	style := cp.EffectiveStyle(tcell.StyleDefault.Foreground(fg).Background(bg))

	// Fill background
	painter.Fill(cp.Rect, ' ', style)

	x := cp.Rect.X
	y := cp.Rect.Y

	// Draw color sample: [█A]
	// █ = solid block with selected color as background
	// A = letter with selected color as foreground on global background (contrast check)
	painter.SetCell(x, y, '[', style)
	x++

	// Block with color as background
	painter.SetCell(x, y, ' ', tcell.StyleDefault.Background(cp.result.Color))
	x++

	// Letter on global background (to show contrast)
	sampleLetter := 'A'
	if len(cp.config.Label) > 0 {
		sampleLetter = []rune(cp.config.Label)[0]
	}
	painter.SetCell(x, y, sampleLetter, tcell.StyleDefault.Foreground(cp.result.Color).Background(globalBg))
	x++

	painter.SetCell(x, y, ']', style)
	x += 2

	// Draw source (truncated if needed)
	source := cp.result.Source
	maxLen := cp.Rect.W - (x - cp.Rect.X)
	if len(source) > maxLen && maxLen > 0 {
		source = source[:maxLen]
	}
	painter.DrawText(x, y, source, style.Dim(true))
}

// drawExpanded renders tabs and active mode content.
func (cp *ColorPicker) drawExpanded(painter *core.Painter) {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	baseStyle := tcell.StyleDefault.Foreground(fg).Background(bg)

	// Fill background
	painter.Fill(cp.Rect, ' ', baseStyle)

	// Draw border
	borderStyle := cp.EffectiveStyle(baseStyle)
	painter.DrawBorder(cp.Rect, borderStyle, [6]rune{'─', '│', '┌', '┐', '└', '┘'})

	// Draw tabs on top border
	x := cp.Rect.X + 2
	y := cp.Rect.Y

	// When tab bar has focus, show a focus indicator
	tabBarFocused := cp.focus == focusTabBar
	if tabBarFocused {
		painter.SetCell(cp.Rect.X+1, y, '►', baseStyle.Bold(true))
	}

	for _, mode := range cp.modeOrder {
		tabName := " " + mode.String() + " "
		isActive := mode == cp.currentMode

		tabStyle := baseStyle
		if isActive {
			if tabBarFocused {
				// Active tab with tab bar focus: bold + reverse
				tabStyle = tabStyle.Reverse(true).Bold(true)
			} else {
				// Active tab without tab bar focus: just reverse
				tabStyle = tabStyle.Reverse(true)
			}
		} else if tabBarFocused {
			// Inactive tabs when tab bar focused: dim
			tabStyle = tabStyle.Dim(true)
		}

		painter.DrawText(x, y, tabName, tabStyle)
		x += len(tabName) + 1
	}

	// Draw mode content inside border
	contentRect := core.Rect{
		X: cp.Rect.X + 1,
		Y: cp.Rect.Y + 1,
		W: cp.Rect.W - 2,
		H: cp.Rect.H - 2,
	}

	if cp.activeMode != nil {
		cp.activeMode.Draw(painter, contentRect)
	}

	// Draw live preview in bottom-left corner
	previewX := cp.Rect.X + 2
	previewY := cp.Rect.Y + cp.Rect.H - 1
	r := cp.activeMode.GetResult()
	globalBg := tm.GetSemanticColor("bg.base")

	// Preview: [█A]
	painter.SetCell(previewX, previewY, '[', baseStyle)
	painter.SetCell(previewX+1, previewY, ' ', tcell.StyleDefault.Background(r.Color))
	painter.SetCell(previewX+2, previewY, 'T', tcell.StyleDefault.Foreground(r.Color).Background(globalBg))
	painter.SetCell(previewX+3, previewY, ']', baseStyle)
}

// HandleKey processes keyboard input.
func (cp *ColorPicker) HandleKey(ev *tcell.EventKey) bool {
	if !cp.expanded {
		// Collapsed: Enter/Space to expand
		if ev.Key() == tcell.KeyEnter || ev.Rune() == ' ' {
			cp.Expand()
			cp.focus = focusTabBar // Start with focus on tab bar
			return true
		}
		return false
	}

	// Esc always closes
	if ev.Key() == tcell.KeyEsc {
		cp.Collapse()
		return true
	}

	// Number keys always switch modes (regardless of focus area)
	if ev.Key() == tcell.KeyRune {
		switch ev.Rune() {
		case '1':
			if len(cp.modeOrder) >= 1 {
				cp.selectMode(cp.modeOrder[0])
				return true
			}
		case '2':
			if len(cp.modeOrder) >= 2 {
				cp.selectMode(cp.modeOrder[1])
				return true
			}
		case '3':
			if len(cp.modeOrder) >= 3 {
				cp.selectMode(cp.modeOrder[2])
				return true
			}
		}
	}

	// Handle based on focus area
	if cp.focus == focusTabBar {
		return cp.handleTabBarKey(ev)
	}

	// Content focus: let mode handle first, then check for commit
	return cp.handleContentKey(ev)
}

// handleTabBarKey handles keys when focus is on the tab bar.
func (cp *ColorPicker) handleTabBarKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyTab:
		// Tab moves to content, reset mode focus to first element
		cp.focus = focusContent
		if cp.activeMode != nil {
			cp.activeMode.ResetFocus()
		}
		cp.invalidate()
		return true

	case tcell.KeyLeft:
		// Previous mode
		cp.cycleModeBackward()
		return true

	case tcell.KeyRight:
		// Next mode
		cp.cycleModeForward()
		return true

	case tcell.KeyDown:
		// Enter content, reset mode focus to first element
		cp.focus = focusContent
		if cp.activeMode != nil {
			cp.activeMode.ResetFocus()
		}
		cp.invalidate()
		return true
	}

	return false
}

// handleContentKey handles keys when focus is on the mode content.
func (cp *ColorPicker) handleContentKey(ev *tcell.EventKey) bool {
	// Let mode try to handle the key first (including Tab for internal navigation)
	if cp.activeMode != nil {
		if cp.activeMode.HandleKey(ev) {
			cp.invalidate()
			return true
		}
	}

	// Mode didn't handle it - check what to do
	switch ev.Key() {
	case tcell.KeyTab:
		// Mode didn't handle Tab, return to tab bar
		cp.focus = focusTabBar
		cp.invalidate()
		return true

	case tcell.KeyUp:
		// Mode didn't handle Up, go to tab bar
		cp.focus = focusTabBar
		cp.invalidate()
		return true

	case tcell.KeyEnter:
		// Mode didn't handle Enter, commit current selection and close
		if cp.activeMode != nil {
			r := cp.activeMode.GetResult()
			cp.result = ColorPickerResult{
				Color:  r.Color,
				Mode:   cp.currentMode,
				Source: r.Source,
				R:      r.R,
				G:      r.G,
				B:      r.B,
			}
			if cp.OnChange != nil {
				cp.OnChange(cp.result)
			}
			cp.Collapse()
		}
		return true
	}

	return false
}

// cycleModeForward moves to the next mode.
func (cp *ColorPicker) cycleModeForward() {
	if len(cp.modeOrder) <= 1 {
		return
	}
	currentIdx := 0
	for i, m := range cp.modeOrder {
		if m == cp.currentMode {
			currentIdx = i
			break
		}
	}
	nextIdx := (currentIdx + 1) % len(cp.modeOrder)
	cp.selectMode(cp.modeOrder[nextIdx])
}

// cycleModeBackward moves to the previous mode.
func (cp *ColorPicker) cycleModeBackward() {
	if len(cp.modeOrder) <= 1 {
		return
	}
	currentIdx := 0
	for i, m := range cp.modeOrder {
		if m == cp.currentMode {
			currentIdx = i
			break
		}
	}
	prevIdx := (currentIdx - 1 + len(cp.modeOrder)) % len(cp.modeOrder)
	cp.selectMode(cp.modeOrder[prevIdx])
}

// HandleMouse processes mouse input.
func (cp *ColorPicker) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !cp.HitTest(x, y) {
		return false
	}

	if !cp.expanded {
		// Click to expand
		if ev.Buttons() == tcell.Button1 {
			cp.Expand()
			return true
		}
		return false
	}

	// Check if clicking on tabs (top border row)
	if y == cp.Rect.Y {
		if ev.Buttons() == tcell.Button1 {
			// Find which tab was clicked
			tabX := cp.Rect.X + 2
			for _, mode := range cp.modeOrder {
				tabName := " " + mode.String() + " "
				tabWidth := len(tabName) + 1

				if x >= tabX && x < tabX+tabWidth-1 {
					cp.selectMode(mode)
					return true
				}
				tabX += tabWidth
			}
		}
		return true
	}

	// Delegate to active mode (content area)
	if cp.activeMode != nil {
		contentRect := core.Rect{
			X: cp.Rect.X + 1,
			Y: cp.Rect.Y + 1,
			W: cp.Rect.W - 2,
			H: cp.Rect.H - 2,
		}
		if cp.activeMode.HandleMouse(ev, contentRect) {
			cp.invalidate()
			return true
		}
	}

	return true
}

// selectMode switches to a different mode.
func (cp *ColorPicker) selectMode(mode ColorPickerMode) {
	if picker, ok := cp.modes[mode]; ok {
		cp.currentMode = mode
		cp.activeMode = picker
		cp.calculateSize()
		cp.invalidate()
	}
}

// calculateSize updates widget size based on state.
func (cp *ColorPicker) calculateSize() {
	if !cp.expanded {
		// Collapsed: [█A] source
		w := 5                          // [█A]
		w += len(cp.result.Source) + 1  // " source"
		if w < 20 {
			w = 20
		}
		cp.Resize(w, 1)
	} else {
		// Calculate minimum width needed for tabs
		// Format: ┌► Semantic  Palette  Custom ─┐
		// = 2 (left border + focus indicator) + tabs + 1 (right border)
		tabsWidth := 2 // Left border + space for focus indicator
		for _, mode := range cp.modeOrder {
			tabName := " " + mode.String() + " "
			tabsWidth += len(tabName) + 1 // tab + spacing
		}
		tabsWidth += 1 // Right border

		// Expanded: get preferred size from active mode
		w, h := 30, 12 // Default minimum
		if cp.activeMode != nil {
			mw, mh := cp.activeMode.PreferredSize()
			if mw+2 > w {
				w = mw + 2 // +2 for border
			}
			if mh+2 > h {
				h = mh + 2 // +2 for border
			}
		}

		// Ensure width is at least enough for all tabs
		if tabsWidth > w {
			w = tabsWidth
		}

		cp.Resize(w, h)
	}
}

// invalidate marks the widget as needing redraw.
func (cp *ColorPicker) invalidate() {
	if cp.inv != nil {
		cp.inv(cp.Rect)
	}
}

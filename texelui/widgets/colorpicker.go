// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/widgets/colorpicker.go
// Summary: Color picker widget with semantic, palette, and OKLCH modes.
// Uses OKLCHEditor widget for custom color selection.

package widgets

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
	"texelation/texelui/primitives"
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

	// Tab bar for mode switching
	tabBar    *primitives.TabBar
	modeOrder []ColorPickerMode // Order for tab display (maps to tab indices)

	// Mode pickers (legacy interface for Semantic/Palette)
	modes      map[ColorPickerMode]colorpicker.ModePicker
	activeMode colorpicker.ModePicker

	// New widget-based OKLCH editor
	oklchEditor *OKLCHEditor

	// Callbacks
	OnChange func(ColorPickerResult)

	// Invalidation
	inv func(core.Rect)
}

// NewColorPicker creates a color picker with the given configuration.
// Position defaults to 0,0. Use SetPosition to adjust after adding to a layout.
func NewColorPicker(config ColorPickerConfig) *ColorPicker {
	cp := &ColorPicker{
		config:    config,
		expanded:  false,
		modes:     make(map[ColorPickerMode]colorpicker.ModePicker),
		modeOrder: []ColorPickerMode{},
	}

	cp.SetPosition(0, 0)
	cp.SetFocusable(true)

	// Build tab items and initialize enabled modes in order
	var tabItems []primitives.TabItem
	if config.EnableSemantic {
		cp.modes[ColorModeSemantic] = colorpicker.NewSemanticPicker()
		cp.modeOrder = append(cp.modeOrder, ColorModeSemantic)
		tabItems = append(tabItems, primitives.TabItem{Label: ColorModeSemantic.String(), ID: "semantic"})
	}
	if config.EnablePalette {
		cp.modes[ColorModePalette] = colorpicker.NewPalettePicker()
		cp.modeOrder = append(cp.modeOrder, ColorModePalette)
		tabItems = append(tabItems, primitives.TabItem{Label: ColorModePalette.String(), ID: "palette"})
	}
	if config.EnableOKLCH {
		// Use new widget-based OKLCHEditor instead of legacy ModePicker
		cp.oklchEditor = NewOKLCHEditor()
		cp.modeOrder = append(cp.modeOrder, ColorModeOKLCH)
		tabItems = append(tabItems, primitives.TabItem{Label: ColorModeOKLCH.String(), ID: "oklch"})
	}

	// Ensure at least one mode is enabled - default to OKLCH if none specified
	if len(cp.modeOrder) == 0 {
		cp.oklchEditor = NewOKLCHEditor()
		cp.modeOrder = append(cp.modeOrder, ColorModeOKLCH)
		tabItems = append(tabItems, primitives.TabItem{Label: ColorModeOKLCH.String(), ID: "oklch"})
	}

	// Create tab bar
	cp.tabBar = primitives.NewTabBar(0, 0, 40, tabItems)
	cp.tabBar.OnChange = func(idx int) {
		if idx >= 0 && idx < len(cp.modeOrder) {
			cp.selectModeByIndex(idx)
		}
	}

	// Set initial mode to first available
	cp.currentMode = cp.modeOrder[0]
	if cp.currentMode != ColorModeOKLCH {
		cp.activeMode = cp.modes[cp.currentMode]
	}

	// Get initial color from first mode
	cp.result = cp.getResultFromCurrentMode()

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
	if cp.tabBar != nil {
		cp.tabBar.SetInvalidator(fn)
	}
	if cp.oklchEditor != nil {
		cp.oklchEditor.SetInvalidator(fn)
	}
}

// getResultFromCurrentMode returns a ColorPickerResult from the active mode.
func (cp *ColorPicker) getResultFromCurrentMode() ColorPickerResult {
	if cp.currentMode == ColorModeOKLCH && cp.oklchEditor != nil {
		color := cp.oklchEditor.GetColor()
		r, g, b := color.RGB()
		return ColorPickerResult{
			Color:  color,
			Mode:   ColorModeOKLCH,
			Source: cp.oklchEditor.GetSource(),
			R:      r,
			G:      g,
			B:      b,
		}
	}
	if cp.activeMode != nil {
		mr := cp.activeMode.GetResult()
		return ColorPickerResult{
			Color:  mr.Color,
			Mode:   cp.currentMode,
			Source: mr.Source,
			R:      mr.R,
			G:      mr.G,
			B:      mr.B,
		}
	}
	return ColorPickerResult{}
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
	if mode == ColorModeOKLCH && cp.oklchEditor != nil {
		cp.currentMode = mode
		cp.activeMode = nil
		cp.oklchEditor.SetColor(resolvedColor)
	} else if _, ok := cp.modes[mode]; ok {
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

// IsExpanded returns true when the picker is expanded.
// Implements core.Expandable interface.
func (cp *ColorPicker) IsExpanded() bool {
	return cp.expanded
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

	// Position and draw tab bar on top border (after the corner)
	if cp.tabBar != nil {
		tabBarFocused := cp.focus == focusTabBar
		cp.tabBar.SetPosition(cp.Rect.X+1, cp.Rect.Y)
		cp.tabBar.Resize(cp.Rect.W-2, 1)
		if tabBarFocused {
			cp.tabBar.Focus()
		} else {
			cp.tabBar.Blur()
		}
		cp.tabBar.Draw(painter)
	}

	// Draw mode content inside border (below tab bar)
	contentRect := core.Rect{
		X: cp.Rect.X + 1,
		Y: cp.Rect.Y + 1,
		W: cp.Rect.W - 2,
		H: cp.Rect.H - 2,
	}

	if cp.currentMode == ColorModeOKLCH && cp.oklchEditor != nil {
		// Use new widget-based OKLCHEditor
		cp.oklchEditor.SetPosition(contentRect.X, contentRect.Y)
		cp.oklchEditor.Resize(contentRect.W, contentRect.H)
		cp.oklchEditor.Draw(painter)
	} else if cp.activeMode != nil {
		cp.activeMode.Draw(painter, contentRect)
	}

	// Draw live preview in bottom-left corner
	previewX := cp.Rect.X + 2
	previewY := cp.Rect.Y + cp.Rect.H - 1
	r := cp.getResultFromCurrentMode()
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
		// Collapsed: Space only to expand (Enter validates/cycles, like other widgets)
		if ev.Rune() == ' ' {
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
		// Tab cycles focus - use CycleFocus for consistent behavior
		if ev.Modifiers()&tcell.ModShift != 0 {
			cp.CycleFocus(false) // Shift+Tab goes backward
		} else {
			cp.CycleFocus(true) // Tab goes forward (to content)
		}
		return true

	case tcell.KeyDown:
		// Enter content, reset mode focus to first element
		cp.focus = focusContent
		cp.resetContentFocus()
		cp.invalidate()
		return true
	}

	// Delegate to TabBar for Left/Right/Home/End/number keys
	if cp.tabBar != nil && cp.tabBar.HandleKey(ev) {
		cp.invalidate()
		return true
	}

	return false
}

// resetContentFocus resets focus to the first element in the current mode.
func (cp *ColorPicker) resetContentFocus() {
	if cp.currentMode == ColorModeOKLCH && cp.oklchEditor != nil {
		cp.oklchEditor.ResetFocus()
		cp.oklchEditor.Focus()
	} else if cp.activeMode != nil {
		cp.activeMode.ResetFocus()
	}
}

// handleContentKey handles keys when focus is on the mode content.
func (cp *ColorPicker) handleContentKey(ev *tcell.EventKey) bool {
	// Handle Tab specially - cycle focus within picker
	if ev.Key() == tcell.KeyTab {
		if ev.Modifiers()&tcell.ModShift != 0 {
			cp.CycleFocus(false) // Shift+Tab goes backward
		} else {
			cp.CycleFocus(true) // Tab goes forward
		}
		return true
	}

	// Let mode try to handle the key first
	var handled bool
	if cp.currentMode == ColorModeOKLCH && cp.oklchEditor != nil {
		handled = cp.oklchEditor.HandleKey(ev)
	} else if cp.activeMode != nil {
		handled = cp.activeMode.HandleKey(ev)
	}

	if handled {
		cp.invalidate()
		return true
	}

	// Mode didn't handle it - check what to do
	switch ev.Key() {
	case tcell.KeyUp:
		// Mode didn't handle Up, go to tab bar
		cp.focus = focusTabBar
		if cp.oklchEditor != nil {
			cp.oklchEditor.Blur()
		}
		cp.invalidate()
		return true

	case tcell.KeyEnter:
		// Mode didn't handle Enter, commit current selection and close
		cp.result = cp.getResultFromCurrentMode()
		if cp.OnChange != nil {
			cp.OnChange(cp.result)
		}
		cp.Collapse()
		return true
	}

	return false
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
	if y == cp.Rect.Y && cp.tabBar != nil {
		// Delegate to TabBar
		if cp.tabBar.HandleMouse(ev) {
			cp.focus = focusTabBar
			cp.invalidate()
			return true
		}
		return true
	}

	// Delegate to active mode (content area)
	contentRect := core.Rect{
		X: cp.Rect.X + 1,
		Y: cp.Rect.Y + 1,
		W: cp.Rect.W - 2,
		H: cp.Rect.H - 2,
	}

	if cp.currentMode == ColorModeOKLCH && cp.oklchEditor != nil {
		// Ensure OKLCHEditor is positioned correctly for hit testing
		cp.oklchEditor.SetPosition(contentRect.X, contentRect.Y)
		cp.oklchEditor.Resize(contentRect.W, contentRect.H)
		if cp.oklchEditor.HandleMouse(ev) {
			cp.focus = focusContent
			cp.invalidate()
			return true
		}
	} else if cp.activeMode != nil {
		if cp.activeMode.HandleMouse(ev, contentRect) {
			cp.invalidate()
			return true
		}
	}

	return true
}

// selectModeByIndex switches to a mode by its index in modeOrder.
// Called by TabBar.OnChange callback.
func (cp *ColorPicker) selectModeByIndex(idx int) {
	if idx < 0 || idx >= len(cp.modeOrder) {
		return
	}
	mode := cp.modeOrder[idx]
	if mode == ColorModeOKLCH && cp.oklchEditor != nil {
		cp.currentMode = mode
		cp.activeMode = nil
	} else if picker, ok := cp.modes[mode]; ok {
		cp.currentMode = mode
		cp.activeMode = picker
	}
	cp.calculateSize()
	cp.invalidate()
}

// selectMode switches to a different mode.
func (cp *ColorPicker) selectMode(mode ColorPickerMode) {
	// Find index of mode and update TabBar
	for i, m := range cp.modeOrder {
		if m == mode {
			if cp.tabBar != nil {
				cp.tabBar.SetActive(i)
			}
			cp.selectModeByIndex(i)
			return
		}
	}
}

// calculateSize updates widget size based on state.
func (cp *ColorPicker) calculateSize() {
	if !cp.expanded {
		// Collapsed: [█A] source
		w := 5                         // [█A]
		w += len(cp.result.Source) + 1 // " source"
		if w < 20 {
			w = 20
		}
		cp.Resize(w, 1)
	} else {
		// Calculate minimum width needed for tabs
		// TabBar format: ► Semantic  Palette  Custom
		// Plus 2 for left/right borders
		tabsWidth := 2 // Left and right border
		if cp.tabBar != nil {
			// Estimate TabBar width: focus marker + tabs + spacing
			tabsWidth += 1 // Focus marker
			for _, mode := range cp.modeOrder {
				tabName := " " + mode.String() + " "
				tabsWidth += len(tabName) + 1 // tab + spacing
			}
		}

		// Expanded: get preferred size from active mode
		w, h := 30, 15 // Default minimum (increased for OKLCHEditor)
		if cp.currentMode == ColorModeOKLCH && cp.oklchEditor != nil {
			// OKLCHEditor needs more space for HCPlane + slider + preview
			mw, mh := 28, 13
			if mw+2 > w {
				w = mw + 2 // +2 for border
			}
			if mh+2 > h {
				h = mh + 2 // +2 for border
			}
		} else if cp.activeMode != nil {
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

// CycleFocus implements core.FocusCycler.
// When expanded, cycles focus between tab bar and content.
func (cp *ColorPicker) CycleFocus(forward bool) bool {
	if !cp.expanded {
		return false
	}

	if forward {
		switch cp.focus {
		case focusTabBar:
			// Move to content
			cp.focus = focusContent
			cp.resetContentFocus()
			cp.invalidate()
			return true
		case focusContent:
			// Try to cycle within content first
			if cp.currentMode == ColorModeOKLCH && cp.oklchEditor != nil {
				if cp.oklchEditor.CycleFocus(true) {
					cp.invalidate()
					return true
				}
			}
			// Content exhausted, wrap to tab bar
			cp.focus = focusTabBar
			if cp.oklchEditor != nil {
				cp.oklchEditor.Blur()
			}
			cp.invalidate()
			return true
		}
	} else {
		switch cp.focus {
		case focusContent:
			// Try to cycle backward within content first
			if cp.currentMode == ColorModeOKLCH && cp.oklchEditor != nil {
				if cp.oklchEditor.CycleFocus(false) {
					cp.invalidate()
					return true
				}
			}
			// Content exhausted, go to tab bar
			cp.focus = focusTabBar
			if cp.oklchEditor != nil {
				cp.oklchEditor.Blur()
			}
			cp.invalidate()
			return true
		case focusTabBar:
			// Wrap to content (last element)
			cp.focus = focusContent
			cp.resetContentFocus()
			// Move to last element in content
			if cp.currentMode == ColorModeOKLCH && cp.oklchEditor != nil {
				// Move to slider (last element)
				cp.oklchEditor.CycleFocus(true) // plane -> slider
			}
			cp.invalidate()
			return true
		}
	}
	return true
}

// TrapsFocus implements core.FocusCycler.
// Returns true when expanded to trap focus within the picker.
func (cp *ColorPicker) TrapsFocus() bool {
	return cp.expanded
}

// GetKeyHints implements core.KeyHintsProvider.
func (cp *ColorPicker) GetKeyHints() []core.KeyHint {
	if !cp.expanded {
		return []core.KeyHint{
			{Key: "Space", Label: "Open"},
		}
	}
	if cp.focus == focusTabBar && cp.tabBar != nil {
		// Get hints from TabBar and add navigation hint
		hints := cp.tabBar.GetKeyHints()
		hints = append(hints, core.KeyHint{Key: "↓/Tab", Label: "Edit"})
		return hints
	}
	// Content focus
	return []core.KeyHint{
		{Key: "Enter", Label: "Apply"},
		{Key: "Esc", Label: "Close"},
	}
}

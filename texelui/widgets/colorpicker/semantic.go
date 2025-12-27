// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/widgets/colorpicker/semantic.go
// Summary: Semantic color selection mode using ScrollableList widget.

package colorpicker

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
	"texelation/texelui/primitives"
)

// SemanticPicker allows selection from semantic color names.
// It uses a ScrollableList internally for navigation and rendering.
type SemanticPicker struct {
	semanticNames []string
	list          *primitives.ScrollableList
}

// NewSemanticPicker creates a semantic color picker.
func NewSemanticPicker() *SemanticPicker {
	sp := &SemanticPicker{
		semanticNames: []string{
			"accent",
			"accent_secondary",
			"bg.base",
			"bg.mantle",
			"bg.crust",
			"bg.surface",
			"text.primary",
			"text.secondary",
			"text.muted",
			"text.inverse",
			"text.accent",
			"action.primary",
			"action.success",
			"action.warning",
			"action.danger",
			"selection",
			"border.active",
			"border.inactive",
			"border.resizing",
			"border.focus",
			"caret",
		},
	}

	// Create list items
	items := make([]primitives.ListItem, len(sp.semanticNames))
	for i, name := range sp.semanticNames {
		items[i] = primitives.ListItem{Text: name, Value: name}
	}

	// Create scrollable list
	sp.list = primitives.NewScrollableList(0, 0, 28, 10)
	sp.list.SetItems(items)
	sp.list.RenderItem = sp.renderColorItem

	return sp
}

// renderColorItem renders a semantic color item with swatch and name.
func (sp *SemanticPicker) renderColorItem(p *core.Painter, rect core.Rect, item primitives.ListItem, selected bool) {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	baseStyle := tcell.StyleDefault.Foreground(fg).Background(bg)

	style := baseStyle
	if selected {
		style = style.Reverse(true)
	}

	// Fill background
	p.Fill(rect, ' ', style)

	name := item.Text
	color := tm.GetSemanticColor(name)

	// Draw: [██] semantic.name
	x := rect.X
	DrawColorSwatch(p, x, rect.Y, color, style)
	x += 5 // swatch width (4) + space (1)

	// Truncate name if needed
	displayName := name
	maxLen := rect.W - 6
	if len(displayName) > maxLen && maxLen > 0 {
		displayName = displayName[:maxLen]
	}
	p.DrawText(x, rect.Y, displayName, style)
}

func (sp *SemanticPicker) Draw(painter *core.Painter, rect core.Rect) {
	// Update list position and size to match provided rect
	sp.list.SetPosition(rect.X, rect.Y)
	sp.list.Resize(rect.W, rect.H)

	// Delegate to scrollable list
	sp.list.Draw(painter)
}

func (sp *SemanticPicker) HandleKey(ev *tcell.EventKey) bool {
	return sp.list.HandleKey(ev)
}

func (sp *SemanticPicker) HandleMouse(ev *tcell.EventMouse, rect core.Rect) bool {
	// Update list position and size to match provided rect
	sp.list.SetPosition(rect.X, rect.Y)
	sp.list.Resize(rect.W, rect.H)

	return sp.list.HandleMouse(ev)
}

func (sp *SemanticPicker) GetResult() PickerResult {
	tm := theme.Get()
	item := sp.list.SelectedItem()
	if item == nil {
		// Fallback to first item
		name := sp.semanticNames[0]
		return MakeResult(tm.GetSemanticColor(name), name)
	}
	name := item.Text
	color := tm.GetSemanticColor(name)
	return MakeResult(color, name)
}

func (sp *SemanticPicker) PreferredSize() (int, int) {
	// Width: "[██] border.inactive" = ~25 chars
	// Height: show ~10 items
	return 28, 10
}

func (sp *SemanticPicker) SetColor(color tcell.Color) {
	// Try to find matching semantic color
	tm := theme.Get()
	for i, name := range sp.semanticNames {
		if tm.GetSemanticColor(name) == color {
			sp.list.SetSelected(i)
			return
		}
	}
}

// ResetFocus is a no-op for semantic picker (single focus area).
func (sp *SemanticPicker) ResetFocus() {
	// No internal tab stops to reset
}

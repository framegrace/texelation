// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/widgets/colorpicker/palette.go
// Summary: Palette color selection mode using Grid widget.

package colorpicker

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
	"texelation/texelui/primitives"
)

// PalettePicker allows selection from the current color palette.
// It uses a Grid internally for 2D navigation and rendering.
type PalettePicker struct {
	paletteNames []string
	grid         *primitives.Grid
}

// NewPalettePicker creates a palette color picker.
func NewPalettePicker() *PalettePicker {
	pp := &PalettePicker{
		paletteNames: []string{
			// Standard Catppuccin palette colors
			"rosewater",
			"flamingo",
			"pink",
			"mauve",
			"red",
			"maroon",
			"peach",
			"yellow",
			"green",
			"teal",
			"sky",
			"sapphire",
			"blue",
			"lavender",
			"text",
			"subtext1",
			"subtext0",
			"overlay2",
			"overlay1",
			"overlay0",
			"surface2",
			"surface1",
			"surface0",
			"base",
			"mantle",
			"crust",
		},
	}

	// Find longest name to calculate proper cell width
	maxNameLen := 0
	for _, name := range pp.paletteNames {
		if len(name) > maxNameLen {
			maxNameLen = len(name)
		}
	}

	// Create grid items
	items := make([]primitives.GridItem, len(pp.paletteNames))
	for i, name := range pp.paletteNames {
		items[i] = primitives.GridItem{Text: name, Value: name}
	}

	// Create grid with appropriate sizing
	// Cell width: [██] + space + name + padding = 4 + 1 + maxNameLen + 1 = 6 + maxNameLen
	pp.grid = primitives.NewGrid(0, 0, 40, 10)
	pp.grid.MinCellWidth = 6 + maxNameLen
	pp.grid.MaxCols = 3
	pp.grid.SetItems(items)
	pp.grid.RenderCell = pp.renderColorCell

	return pp
}

// renderColorCell renders a palette color cell with swatch and name.
func (pp *PalettePicker) renderColorCell(p *core.Painter, rect core.Rect, item primitives.GridItem, selected bool) {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	baseStyle := tcell.StyleDefault.Foreground(fg).Background(bg)

	style := baseStyle
	if selected {
		style = style.Reverse(true)
	}

	// Fill cell background
	p.Fill(rect, ' ', style)

	name := item.Text
	color := theme.ResolveColorName(name)

	// Draw: [██] name
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

func (pp *PalettePicker) Draw(painter *core.Painter, rect core.Rect) {
	// Update grid position and size to match provided rect
	pp.grid.SetPosition(rect.X, rect.Y)
	pp.grid.Resize(rect.W, rect.H)

	// Delegate to grid
	pp.grid.Draw(painter)
}

func (pp *PalettePicker) HandleKey(ev *tcell.EventKey) bool {
	return pp.grid.HandleKey(ev)
}

func (pp *PalettePicker) HandleMouse(ev *tcell.EventMouse, rect core.Rect) bool {
	// Update grid position and size to match provided rect
	pp.grid.SetPosition(rect.X, rect.Y)
	pp.grid.Resize(rect.W, rect.H)

	return pp.grid.HandleMouse(ev)
}

func (pp *PalettePicker) GetResult() PickerResult {
	item := pp.grid.SelectedItem()
	if item == nil {
		// Fallback to first item
		name := pp.paletteNames[0]
		return MakeResult(theme.ResolveColorName(name), "@"+name)
	}
	name := item.Text
	color := theme.ResolveColorName(name)
	// Prefix with @ to indicate palette color
	return MakeResult(color, "@"+name)
}

func (pp *PalettePicker) PreferredSize() (int, int) {
	// Calculate width needed for full names
	maxNameLen := 0
	for _, name := range pp.paletteNames {
		if len(name) > maxNameLen {
			maxNameLen = len(name)
		}
	}
	// Cell width: [██] + space + name + padding = 6 + maxNameLen
	cellWidth := 6 + maxNameLen
	// Request width for 2 columns (will expand to 3 if space available)
	width := cellWidth * 2

	// Calculate rows based on 2 columns
	cols := 2
	rows := (len(pp.paletteNames) + cols - 1) / cols
	return width, rows
}

func (pp *PalettePicker) SetColor(color tcell.Color) {
	// Try to find matching palette color
	for i, name := range pp.paletteNames {
		if theme.ResolveColorName(name) == color {
			pp.grid.SetSelected(i)
			return
		}
	}
}

// ResetFocus is a no-op for palette picker (single focus area).
func (pp *PalettePicker) ResetFocus() {
	// No internal tab stops to reset
}

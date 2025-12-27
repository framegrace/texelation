// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/widgets/colorpicker/palette.go
// Summary: Palette color selection mode.

package colorpicker

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
)

// PalettePicker allows selection from the current color palette.
type PalettePicker struct {
	paletteNames []string
	selectedIdx  int
	cols         int // Number of columns in grid (updated by Draw based on available width)
}

// NewPalettePicker creates a palette color picker.
func NewPalettePicker() *PalettePicker {
	return &PalettePicker{
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
		selectedIdx: 0,
		cols:        3,
	}
}

func (pp *PalettePicker) Draw(painter *core.Painter, rect core.Rect) {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	baseStyle := tcell.StyleDefault.Foreground(fg).Background(bg)

	// Fill background
	painter.Fill(rect, ' ', baseStyle)

	// Find longest name to calculate cell width
	maxNameLen := 0
	for _, name := range pp.paletteNames {
		if len(name) > maxNameLen {
			maxNameLen = len(name)
		}
	}

	// Cell width: [██] + space + name + padding
	// = 4 (brackets+blocks) + 1 (space) + maxNameLen + 1 (padding between columns)
	minCellWidth := 6 + maxNameLen

	// Calculate how many columns fit in available width
	cols := rect.W / minCellWidth
	if cols < 1 {
		cols = 1
	}
	if cols > 3 {
		cols = 3 // Max 3 columns
	}

	// Store for navigation
	pp.cols = cols

	// Recalculate cell width to use available space evenly
	cellWidth := rect.W / cols

	x := rect.X
	y := rect.Y
	col := 0

	for i, name := range pp.paletteNames {
		if y >= rect.Y+rect.H {
			break
		}

		color := theme.ResolveColorName(name)

		style := baseStyle
		if i == pp.selectedIdx {
			style = style.Reverse(true)
		}

		// Draw: [██] name
		cx := x
		painter.SetCell(cx, y, '[', style)
		cx++
		painter.SetCell(cx, y, '█', tcell.StyleDefault.Foreground(color).Background(bg))
		cx++
		painter.SetCell(cx, y, '█', tcell.StyleDefault.Foreground(color).Background(bg))
		cx++
		painter.SetCell(cx, y, ']', style)
		cx += 2

		// Draw full name (no truncation needed with proper sizing)
		painter.DrawText(cx, y, name, style)

		col++
		if col >= cols {
			col = 0
			x = rect.X
			y++
		} else {
			x += cellWidth
		}
	}
}

func (pp *PalettePicker) HandleKey(ev *tcell.EventKey) bool {
	cols := pp.cols
	if cols < 1 {
		cols = 1
	}
	rows := (len(pp.paletteNames) + cols - 1) / cols
	currentRow := pp.selectedIdx / cols
	currentCol := pp.selectedIdx % cols

	switch ev.Key() {
	case tcell.KeyUp:
		if currentRow > 0 {
			pp.selectedIdx -= cols
		}
		return true
	case tcell.KeyDown:
		if currentRow < rows-1 {
			newIdx := pp.selectedIdx + cols
			if newIdx < len(pp.paletteNames) {
				pp.selectedIdx = newIdx
			} else {
				// Move to last item if going down from partial last row
				pp.selectedIdx = len(pp.paletteNames) - 1
			}
		}
		return true
	case tcell.KeyLeft:
		if currentCol > 0 {
			pp.selectedIdx--
		}
		return true
	case tcell.KeyRight:
		if currentCol < cols-1 && pp.selectedIdx < len(pp.paletteNames)-1 {
			pp.selectedIdx++
		}
		return true
	case tcell.KeyHome:
		pp.selectedIdx = 0
		return true
	case tcell.KeyEnd:
		pp.selectedIdx = len(pp.paletteNames) - 1
		return true
	case tcell.KeyTab:
		// Tab: left-to-right, then top-to-bottom
		if ev.Modifiers()&tcell.ModShift != 0 {
			// Shift+Tab: go backwards
			if pp.selectedIdx > 0 {
				pp.selectedIdx--
				return true
			}
		} else {
			// Tab: go forwards
			if pp.selectedIdx < len(pp.paletteNames)-1 {
				pp.selectedIdx++
				return true
			}
		}
		// At boundary, let parent handle
		return false
	}
	return false
}

func (pp *PalettePicker) HandleMouse(ev *tcell.EventMouse, rect core.Rect) bool {
	x, y := ev.Position()
	if x < rect.X || y < rect.Y || x >= rect.X+rect.W || y >= rect.Y+rect.H {
		return false
	}

	if ev.Buttons() == tcell.Button1 {
		cols := pp.cols
		if cols < 1 {
			cols = 1
		}
		cellWidth := rect.W / cols

		relX := x - rect.X
		relY := y - rect.Y

		clickedCol := relX / cellWidth
		if clickedCol >= cols {
			clickedCol = cols - 1
		}
		clickedRow := relY
		clickedIdx := clickedRow*cols + clickedCol

		if clickedIdx >= 0 && clickedIdx < len(pp.paletteNames) {
			pp.selectedIdx = clickedIdx
			return true
		}
	}

	return false
}

func (pp *PalettePicker) GetResult() PickerResult {
	name := pp.paletteNames[pp.selectedIdx]
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
			pp.selectedIdx = i
			return
		}
	}
}

// ResetFocus is a no-op for palette picker (single focus area).
func (pp *PalettePicker) ResetFocus() {
	// No internal tab stops to reset
}

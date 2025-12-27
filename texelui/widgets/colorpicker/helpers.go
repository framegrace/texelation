// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/widgets/colorpicker/helpers.go
// Summary: Helper functions for color rendering.

package colorpicker

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texelui/core"
)

// DrawColorSwatch renders a color swatch in the format [██]
// where the two blocks show the color.
// Returns the width of the rendered swatch (4 characters).
func DrawColorSwatch(p *core.Painter, x, y int, color tcell.Color, bracketStyle tcell.Style) int {
	_, bg, _ := bracketStyle.Decompose()
	p.SetCell(x, y, '[', bracketStyle)
	p.SetCell(x+1, y, '█', tcell.StyleDefault.Foreground(color).Background(bg))
	p.SetCell(x+2, y, '█', tcell.StyleDefault.Foreground(color).Background(bg))
	p.SetCell(x+3, y, ']', bracketStyle)
	return 4
}

// DrawColorSwatchWithLabel renders a color swatch in the format [█L]
// where the first block shows the color as foreground on background,
// and L is a label character shown with the color as foreground on bgColor.
// This is useful for showing contrast/readability of the color.
// Returns the width of the rendered swatch (4 characters).
func DrawColorSwatchWithLabel(p *core.Painter, x, y int, color, bgColor tcell.Color, label rune, bracketStyle tcell.Style) int {
	_, bg, _ := bracketStyle.Decompose()
	p.SetCell(x, y, '[', bracketStyle)
	p.SetCell(x+1, y, ' ', tcell.StyleDefault.Background(color))
	p.SetCell(x+2, y, label, tcell.StyleDefault.Foreground(color).Background(bgColor))
	p.SetCell(x+3, y, ']', bracketStyle)
	_ = bg // bg is used by bracket style
	return 4
}

// DrawColorSwatchSingle renders a compact 1-block color swatch [█]
// Returns the width of the rendered swatch (3 characters).
func DrawColorSwatchSingle(p *core.Painter, x, y int, color tcell.Color, bracketStyle tcell.Style) int {
	_, bg, _ := bracketStyle.Decompose()
	p.SetCell(x, y, '[', bracketStyle)
	p.SetCell(x+1, y, '█', tcell.StyleDefault.Foreground(color).Background(bg))
	p.SetCell(x+2, y, ']', bracketStyle)
	return 3
}

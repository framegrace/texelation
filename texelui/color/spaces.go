// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/color/spaces.go
// Summary: Color space conversion helpers for tcell integration.

package color

import "github.com/gdamore/tcell/v2"

// TcellToOKLCH converts a tcell.Color to OKLCH components.
func TcellToOKLCH(c tcell.Color) (L, C, H float64) {
	r, g, b := c.RGB()
	return RGBToOKLCH(r, g, b)
}

// OKLCHToTcell converts OKLCH components to a tcell.Color.
func OKLCHToTcell(L, C, H float64) tcell.Color {
	rgb := OKLCHToRGB(L, C, H)
	return tcell.NewRGBColor(rgb.R, rgb.G, rgb.B)
}

// RGBToTcell creates a tcell.Color from RGB components.
func RGBToTcell(rgb RGB) tcell.Color {
	return tcell.NewRGBColor(rgb.R, rgb.G, rgb.B)
}

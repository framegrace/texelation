// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/colors.go
// Summary: Color parsing and conversion utilities for the client runtime.
// Usage: Shared helpers for parsing hex colors and converting RGB values to tcell colors.

package clientruntime

import (
	"strconv"

	"github.com/gdamore/tcell/v2"
)

// parseHexColor parses a hex color string (e.g., "#ff5733") into a tcell color.
// Returns the parsed color and true if successful, or ColorDefault and false if parsing fails.
func parseHexColor(value string) (tcell.Color, bool) {
	if len(value) == 0 {
		return tcell.ColorDefault, false
	}
	if len(value) == 7 && value[0] == '#' {
		if fg, err := strconv.ParseInt(value[1:], 16, 32); err == nil {
			r := int32((fg >> 16) & 0xFF)
			g := int32((fg >> 8) & 0xFF)
			b := int32(fg & 0xFF)
			return tcell.NewRGBColor(r, g, b), true
		}
	}
	return tcell.ColorDefault, false
}

// colorFromRGB converts a packed 24-bit RGB value to a tcell color.
// Format: 0xRRGGBB where RR=red, GG=green, BB=blue.
func colorFromRGB(rgb uint32) tcell.Color {
	r := int32((rgb >> 16) & 0xFF)
	g := int32((rgb >> 8) & 0xFF)
	b := int32(rgb & 0xFF)
	return tcell.NewRGBColor(r, g, b)
}

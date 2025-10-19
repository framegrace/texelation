// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/cell.go
// Summary: Implements cell capabilities for the terminal parser module.
// Usage: Consumed by the terminal app when decoding VT sequences.
// Notes: Keeps parsing concerns isolated from rendering.

package parser

type Attribute uint16

const (
	AttrBold Attribute = 1 << iota
	AttrUnderline
	AttrReverse
)

// ColorMode defines the type of color stored.
type ColorMode int

const (
	ColorModeDefault  ColorMode = iota // Default terminal color
	ColorModeStandard                  // The basic 8 ANSI colors
	ColorMode256                       // 256-color palette
	ColorModeRGB                       // 24-bit "true" color
)

// Color now represents a color in potentially different modes.
type Color struct {
	Mode    ColorMode
	Value   uint8 // Holds the color code for Standard (0-7) and 256-mode (0-255)
	R, G, B uint8 // Holds the values for RGB mode
}

// Cell represents a single character cell on the screen.
type Cell struct {
	Rune rune
	FG   Color
	BG   Color
	Attr Attribute
}

// --- Predefined default colors for convenience ---
var (
	DefaultFG = Color{Mode: ColorModeDefault}
	DefaultBG = Color{Mode: ColorModeDefault}
)

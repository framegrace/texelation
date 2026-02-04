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

// String returns a human-readable representation of the attribute flags.
func (a Attribute) String() string {
	if a == 0 {
		return "none"
	}
	var parts []string
	if a&AttrBold != 0 {
		parts = append(parts, "bold")
	}
	if a&AttrUnderline != 0 {
		parts = append(parts, "underline")
	}
	if a&AttrReverse != 0 {
		parts = append(parts, "reverse")
	}
	if len(parts) == 0 {
		return "unknown"
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += "|" + parts[i]
	}
	return result
}

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
	Rune    rune
	FG      Color
	BG      Color
	Attr    Attribute
	Wrapped bool // True if this cell is at the end of a line that wraps to the next line
	Wide    bool // True if this cell contains a wide (2-column) character
}

// --- Predefined default colors for convenience ---
var (
	DefaultFG = Color{Mode: ColorModeDefault}
	DefaultBG = Color{Mode: ColorModeDefault}
)

// ToRGB converts a Color to RGB values.
// For non-RGB colors, returns reasonable defaults.
// For default colors, returns black (suitable for background blending on dark terminals).
func (c Color) ToRGB() (r, g, b uint8) {
	switch c.Mode {
	case ColorModeRGB:
		return c.R, c.G, c.B
	case ColorMode256:
		return color256ToRGB(c.Value)
	case ColorModeStandard:
		return standardColorToRGB(c.Value)
	default:
		// Default colors - assume black (works well for dark terminal backgrounds)
		return 0, 0, 0
	}
}

// standardColorToRGB converts standard ANSI color (0-7) to RGB.
func standardColorToRGB(value uint8) (r, g, b uint8) {
	// Standard ANSI colors
	colors := [][3]uint8{
		{0, 0, 0},       // 0: black
		{205, 49, 49},   // 1: red
		{13, 188, 121},  // 2: green
		{229, 229, 16},  // 3: yellow
		{36, 114, 200},  // 4: blue
		{188, 63, 188},  // 5: magenta
		{17, 168, 205},  // 6: cyan
		{229, 229, 229}, // 7: white
	}
	if value < 8 {
		return colors[value][0], colors[value][1], colors[value][2]
	}
	return 192, 192, 192
}

// color256ToRGB converts 256-color palette index to RGB.
func color256ToRGB(value uint8) (r, g, b uint8) {
	if value < 16 {
		// Standard + bright colors
		if value < 8 {
			return standardColorToRGB(value)
		}
		// Bright variants (add intensity)
		br, bg, bb := standardColorToRGB(value - 8)
		return min(255, br+50), min(255, bg+50), min(255, bb+50)
	}
	if value < 232 {
		// 6x6x6 color cube (indices 16-231)
		idx := value - 16
		r = uint8((idx / 36) * 51)
		g = uint8(((idx / 6) % 6) * 51)
		b = uint8((idx % 6) * 51)
		return r, g, b
	}
	// Grayscale (indices 232-255)
	gray := uint8((int(value)-232)*10 + 8)
	return gray, gray, gray
}

// BlendColor blends two colors together with the given intensity.
// intensity 0.0 = base color, 1.0 = overlay color
// defaultBG is used when base is ColorModeDefault (pass the terminal's actual background)
func BlendColor(base, overlay Color, intensity float32, defaultBG Color) Color {
	if overlay.Mode == ColorModeDefault || intensity <= 0 {
		return base
	}
	if intensity >= 1 {
		return overlay
	}

	// For default colors, use the provided defaultBG
	baseForBlend := base
	if base.Mode == ColorModeDefault {
		baseForBlend = defaultBG
	}

	// Convert both to RGB
	br, bg, bb := baseForBlend.ToRGB()
	or, og, ob := overlay.ToRGB()

	// Blend
	blend := func(bc, oc uint8) uint8 {
		return uint8(float32(bc)*(1-intensity) + float32(oc)*intensity)
	}

	return Color{
		Mode: ColorModeRGB,
		R:    blend(br, or),
		G:    blend(bg, og),
		B:    blend(bb, ob),
	}
}

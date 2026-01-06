// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file tests OSC (Operating System Command) color sequences.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/change_dynamic_color.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// Note: The original tests focus on color querying (OSC Ps ; ? BEL), which
// requires PTY response handling. These tests focus on setting colors and
// verifying they affect subsequently written text.
package esctest

import (
	"testing"
	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// Test_OSC_SetDefaultForeground tests OSC 10 (set default foreground color).
// OSC 10/11 change what "default" means, but don't immediately affect text.
// They affect what SGR 39/49 reset to.
func Test_OSC_SetDefaultForeground(t *testing.T) {
	d := NewDriver(80, 24)

	// Write text with original default
	d.WriteRaw("a")

	// Change default foreground to red using OSC 10
	// Format: OSC 10 ; rgb:rrrr/gggg/bbbb BEL
	// OSC colors are 16-bit, scaled down to 8-bit: value / 257 (integer division)
	// 0xfe00 / 257 = 253
	d.WriteRaw("\x1b]10;rgb:fe00/0000/0000\x07")

	// Now use SGR 39 to explicitly set foreground to "default"
	// This should use the new OSC-set default (red)
	SGR(d, SGR_FG_DEFAULT)
	d.WriteRaw("R")

	// First character should have original default
	AssertCellForegroundColor(t, d, NewPoint(1, 1), parser.DefaultFG, "First char should have original default")

	// Second character should have red foreground from OSC-set default
	expectedRed := parser.Color{Mode: parser.ColorModeRGB, R: 253, G: 0, B: 0}
	AssertCellForegroundColor(t, d, NewPoint(2, 1), expectedRed, "SGR 39 should use OSC-set default")
}

// Test_OSC_SetDefaultBackground tests OSC 11 (set default background color).
func Test_OSC_SetDefaultBackground(t *testing.T) {
	d := NewDriver(80, 24)

	// Write text with default background
	d.WriteRaw("a")

	// Change default background to blue using OSC 11
	// Format: OSC 11 ; rgb:rrrr/gggg/bbbb BEL
	// 0xff00 / 257 = 254
	d.WriteRaw("\x1b]11;rgb:0000/0000/ff00\x07")

	// Use SGR 49 to explicitly set background to "default"
	// This should use the new OSC-set default (blue)
	SGR(d, SGR_BG_DEFAULT)
	d.WriteRaw("b")

	// First character should have old default
	AssertCellBackgroundColor(t, d, NewPoint(1, 1), parser.DefaultBG, "First char should have original default BG")

	// Second character should have blue background from OSC-set default
	expectedBlue := parser.Color{Mode: parser.ColorModeRGB, R: 0, G: 0, B: 254}
	AssertCellBackgroundColor(t, d, NewPoint(2, 1), expectedBlue, "SGR 49 should use OSC-set default")
}

// Test_OSC_SetBothColors tests setting both foreground and background via OSC.
func Test_OSC_SetBothColors(t *testing.T) {
	d := NewDriver(80, 24)

	// Set both default colors
	// 0xff00/257=254, 0x8000/257=127, 0x4000/257=63, 0xe000/257=223, 0xd000/257=207
	d.WriteRaw("\x1b]10;rgb:ff00/8000/0000\x07") // Orange foreground
	d.WriteRaw("\x1b]11;rgb:4000/e000/d000\x07") // Turquoise background

	// Use SGR 39 and 49 to explicitly use the new defaults
	SGR(d, SGR_FG_DEFAULT, SGR_BG_DEFAULT)
	d.WriteRaw("X")

	// Check colors
	expectedOrange := parser.Color{Mode: parser.ColorModeRGB, R: 254, G: 127, B: 0}
	expectedTurquoise := parser.Color{Mode: parser.ColorModeRGB, R: 63, G: 223, B: 207}

	AssertCellForegroundColor(t, d, NewPoint(1, 1), expectedOrange, "Should have orange FG from OSC-set default")
	AssertCellBackgroundColor(t, d, NewPoint(1, 1), expectedTurquoise, "Should have turquoise BG from OSC-set default")
}

// Test_OSC_SGROverridesDefault tests that SGR colors override OSC defaults.
func Test_OSC_SGROverridesDefault(t *testing.T) {
	d := NewDriver(80, 24)

	// Set default foreground to red via OSC
	// 0xff00 / 257 = 254
	d.WriteRaw("\x1b]10;rgb:ff00/0000/0000\x07")

	// Use SGR to set green foreground
	SGR(d, SGR_FG_GREEN)
	d.WriteRaw("G")

	// Use SGR to reset to default (should go back to red from OSC)
	SGR(d, SGR_FG_DEFAULT)
	d.WriteRaw("R")

	// First char should be green (SGR overrides)
	expectedGreen := parser.Color{Mode: parser.ColorModeStandard, Value: 2}
	AssertCellForegroundColor(t, d, NewPoint(1, 1), expectedGreen, "SGR green should override OSC default")

	// Second char should be red (SGR 39 resets to current default, which is from OSC)
	expectedRed := parser.Color{Mode: parser.ColorModeRGB, R: 254, G: 0, B: 0}
	AssertCellForegroundColor(t, d, NewPoint(2, 1), expectedRed, "SGR 39 should reset to OSC-set default")
}

// Test_OSC_ResetRestoresOriginalDefaults tests that RIS resets OSC colors.
func Test_OSC_ResetRestoresOriginalDefaults(t *testing.T) {
	d := NewDriver(80, 24)

	// Set custom default via OSC
	// 0xff00 / 257 = 254
	d.WriteRaw("\x1b]10;rgb:ff00/0000/0000\x07")

	// Use SGR 39 to write with the OSC-set default
	SGR(d, SGR_FG_DEFAULT)
	d.WriteRaw("a")

	// Verify 'a' has red color from OSC-set default
	expectedRed := parser.Color{Mode: parser.ColorModeRGB, R: 254, G: 0, B: 0}
	AssertCellForegroundColor(t, d, NewPoint(1, 1), expectedRed, "Char before reset should be red")

	// Reset terminal (clears screen and resets OSC colors)
	RIS(d)

	// After RIS, write text - should use original default
	d.WriteRaw("b")

	// After RIS, 'a' is cleared and 'b' is written at (1,1)
	// 'b' should have original default color (RIS resets OSC colors)
	AssertCellForegroundColor(t, d, NewPoint(1, 1), parser.DefaultFG, "Char after RIS should have original default")
}

// Test_OSC_ColorFormat_RGB tests different RGB color format variations.
func Test_OSC_ColorFormat_RGB(t *testing.T) {
	d := NewDriver(80, 24)

	// Test 16-bit RGB format (most common): rgb:rrrr/gggg/bbbb
	// Each component is 16-bit hex, scaled to 8-bit (divide by 257)
	// 0x8000 = 32768, 32768/257 = 127
	d.WriteRaw("\x1b]10;rgb:8000/8000/8000\x07") // Mid-gray

	// Use SGR 39 to explicitly use the new default
	SGR(d, SGR_FG_DEFAULT)
	d.WriteRaw("A")

	expectedGray := parser.Color{Mode: parser.ColorModeRGB, R: 127, G: 127, B: 127}
	AssertCellForegroundColor(t, d, NewPoint(1, 1), expectedGray, "Should parse 16-bit RGB format")
}

// Test_OSC_MultipleChanges tests changing default colors multiple times.
func Test_OSC_MultipleChanges(t *testing.T) {
	d := NewDriver(80, 24)

	// Set to red (0xff00 / 257 = 254)
	d.WriteRaw("\x1b]10;rgb:ff00/0000/0000\x07")
	SGR(d, SGR_FG_DEFAULT)
	d.WriteRaw("R")

	// Change to green (0xff00 / 257 = 254)
	d.WriteRaw("\x1b]10;rgb:0000/ff00/0000\x07")
	SGR(d, SGR_FG_DEFAULT)
	d.WriteRaw("G")

	// Change to blue (0xff00 / 257 = 254)
	d.WriteRaw("\x1b]10;rgb:0000/0000/ff00\x07")
	SGR(d, SGR_FG_DEFAULT)
	d.WriteRaw("B")

	// Check all three
	expectedRed := parser.Color{Mode: parser.ColorModeRGB, R: 254, G: 0, B: 0}
	expectedGreen := parser.Color{Mode: parser.ColorModeRGB, R: 0, G: 254, B: 0}
	expectedBlue := parser.Color{Mode: parser.ColorModeRGB, R: 0, G: 0, B: 254}

	AssertCellForegroundColor(t, d, NewPoint(1, 1), expectedRed, "First char should be red")
	AssertCellForegroundColor(t, d, NewPoint(2, 1), expectedGreen, "Second char should be green")
	AssertCellForegroundColor(t, d, NewPoint(3, 1), expectedBlue, "Third char should be blue")
}

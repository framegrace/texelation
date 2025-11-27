// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This file tests SGR (Select Graphic Rendition) - text attributes and colors.
//
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/sgr.py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
//
// The tests have been converted from Python to Go to enable offline, deterministic
// testing of the texelterm terminal emulator.
package esctest

import (
	"testing"
	"texelation/apps/texelterm/parser"
)

// Test_SGR_Bold tests that bold attribute works.
func Test_SGR_Bold(t *testing.T) {
	d := NewDriver(80, 24)

	// Write 'x' without bold
	d.WriteRaw("x")

	// Enable bold and write 'y'
	SGR(d, SGR_BOLD)
	d.WriteRaw("y")

	// Check that 'x' at (1,1) does NOT have bold
	AssertCellDoesNotHaveAttribute(t, d, NewPoint(1, 1), parser.AttrBold, "First char should not be bold")

	// Check that 'x' has default foreground and background
	AssertCellForegroundColor(t, d, NewPoint(1, 1), parser.DefaultFG, "First char should have default FG")
	AssertCellBackgroundColor(t, d, NewPoint(1, 1), parser.DefaultBG, "First char should have default BG")

	// Check that 'y' at (2,1) DOES have bold
	AssertCellHasAttribute(t, d, NewPoint(2, 1), parser.AttrBold, "Second char should be bold")

	// Check that 'y' still has default colors
	AssertCellForegroundColor(t, d, NewPoint(2, 1), parser.DefaultFG, "Bold char should have default FG")
	AssertCellBackgroundColor(t, d, NewPoint(2, 1), parser.DefaultBG, "Bold char should have default BG")
}

// Test_SGR_Underline tests that underline attribute works.
func Test_SGR_Underline(t *testing.T) {
	d := NewDriver(80, 24)

	d.WriteRaw("a")
	SGR(d, SGR_UNDERLINE)
	d.WriteRaw("b")

	AssertCellDoesNotHaveAttribute(t, d, NewPoint(1, 1), parser.AttrUnderline, "")
	AssertCellHasAttribute(t, d, NewPoint(2, 1), parser.AttrUnderline, "")
}

// Test_SGR_Reverse tests that reverse video attribute works.
func Test_SGR_Reverse(t *testing.T) {
	d := NewDriver(80, 24)

	d.WriteRaw("a")
	SGR(d, SGR_REVERSE)
	d.WriteRaw("b")

	AssertCellDoesNotHaveAttribute(t, d, NewPoint(1, 1), parser.AttrReverse, "")
	AssertCellHasAttribute(t, d, NewPoint(2, 1), parser.AttrReverse, "")
}

// Test_SGR_Reset tests that SGR 0 resets all attributes.
func Test_SGR_Reset(t *testing.T) {
	d := NewDriver(80, 24)

	// Set bold, underline, and reverse
	SGR(d, SGR_BOLD, SGR_UNDERLINE, SGR_REVERSE)
	d.WriteRaw("a")

	// Reset all
	SGR(d, SGR_RESET)
	d.WriteRaw("b")

	// Check 'a' has all attributes
	AssertCellHasAttribute(t, d, NewPoint(1, 1), parser.AttrBold, "")
	AssertCellHasAttribute(t, d, NewPoint(1, 1), parser.AttrUnderline, "")
	AssertCellHasAttribute(t, d, NewPoint(1, 1), parser.AttrReverse, "")

	// Check 'b' has none
	AssertCellDoesNotHaveAttribute(t, d, NewPoint(2, 1), parser.AttrBold, "")
	AssertCellDoesNotHaveAttribute(t, d, NewPoint(2, 1), parser.AttrUnderline, "")
	AssertCellDoesNotHaveAttribute(t, d, NewPoint(2, 1), parser.AttrReverse, "")
}

// Test_SGR_DisableBold tests that SGR 22 disables bold.
func Test_SGR_DisableBold(t *testing.T) {
	d := NewDriver(80, 24)

	SGR(d, SGR_BOLD)
	d.WriteRaw("a")

	SGR(d, SGR_NORMAL)
	d.WriteRaw("b")

	AssertCellHasAttribute(t, d, NewPoint(1, 1), parser.AttrBold, "")
	AssertCellDoesNotHaveAttribute(t, d, NewPoint(2, 1), parser.AttrBold, "")
}

// Test_SGR_DisableUnderline tests that SGR 24 disables underline.
func Test_SGR_DisableUnderline(t *testing.T) {
	d := NewDriver(80, 24)

	SGR(d, SGR_UNDERLINE)
	d.WriteRaw("a")

	SGR(d, SGR_NOT_UNDERLINE)
	d.WriteRaw("b")

	AssertCellHasAttribute(t, d, NewPoint(1, 1), parser.AttrUnderline, "")
	AssertCellDoesNotHaveAttribute(t, d, NewPoint(2, 1), parser.AttrUnderline, "")
}

// Test_SGR_ForegroundColor tests basic foreground colors (30-37).
func Test_SGR_ForegroundColor(t *testing.T) {
	d := NewDriver(80, 24)

	// Test red foreground
	SGR(d, SGR_FG_RED)
	d.WriteRaw("R")

	expectedRed := parser.Color{Mode: parser.ColorModeStandard, Value: 1} // Red is index 1
	AssertCellForegroundColor(t, d, NewPoint(1, 1), expectedRed, "Should have red foreground")
	AssertCellBackgroundColor(t, d, NewPoint(1, 1), parser.DefaultBG, "Should have default background")
}

// Test_SGR_BackgroundColor tests basic background colors (40-47).
func Test_SGR_BackgroundColor(t *testing.T) {
	d := NewDriver(80, 24)

	// Test blue background
	SGR(d, SGR_BG_BLUE)
	d.WriteRaw("B")

	expectedBlue := parser.Color{Mode: parser.ColorModeStandard, Value: 4} // Blue is index 4
	AssertCellForegroundColor(t, d, NewPoint(1, 1), parser.DefaultFG, "Should have default foreground")
	AssertCellBackgroundColor(t, d, NewPoint(1, 1), expectedBlue, "Should have blue background")
}

// Test_SGR_ResetForeground tests that SGR 39 resets foreground to default.
func Test_SGR_ResetForeground(t *testing.T) {
	d := NewDriver(80, 24)

	SGR(d, SGR_FG_RED)
	d.WriteRaw("a")

	SGR(d, SGR_FG_DEFAULT)
	d.WriteRaw("b")

	expectedRed := parser.Color{Mode: parser.ColorModeStandard, Value: 1}
	AssertCellForegroundColor(t, d, NewPoint(1, 1), expectedRed, "")
	AssertCellForegroundColor(t, d, NewPoint(2, 1), parser.DefaultFG, "")
}

// Test_SGR_ResetBackground tests that SGR 49 resets background to default.
func Test_SGR_ResetBackground(t *testing.T) {
	d := NewDriver(80, 24)

	SGR(d, SGR_BG_GREEN)
	d.WriteRaw("a")

	SGR(d, SGR_BG_DEFAULT)
	d.WriteRaw("b")

	expectedGreen := parser.Color{Mode: parser.ColorModeStandard, Value: 2} // Green is index 2
	AssertCellBackgroundColor(t, d, NewPoint(1, 1), expectedGreen, "")
	AssertCellBackgroundColor(t, d, NewPoint(2, 1), parser.DefaultBG, "")
}

// Test_SGR_BrightForegroundColors tests bright foreground colors (90-97).
func Test_SGR_BrightForegroundColors(t *testing.T) {
	d := NewDriver(80, 24)

	// SGR 90 = bright black (gray), which is color index 8
	SGR(d, 90)
	d.WriteRaw("A")

	// SGR 97 = bright white, which is color index 15
	SGR(d, 97)
	d.WriteRaw("B")

	expectedBrightBlack := parser.Color{Mode: parser.ColorModeStandard, Value: 8}
	expectedBrightWhite := parser.Color{Mode: parser.ColorModeStandard, Value: 15}

	AssertCellForegroundColor(t, d, NewPoint(1, 1), expectedBrightBlack, "Should have bright black (gray) foreground")
	AssertCellForegroundColor(t, d, NewPoint(2, 1), expectedBrightWhite, "Should have bright white foreground")
}

// Test_SGR_BrightBackgroundColors tests bright background colors (100-107).
func Test_SGR_BrightBackgroundColors(t *testing.T) {
	d := NewDriver(80, 24)

	// SGR 100 = bright black background (gray), which is color index 8
	SGR(d, 100)
	d.WriteRaw("A")

	// SGR 107 = bright white background, which is color index 15
	SGR(d, 107)
	d.WriteRaw("B")

	expectedBrightBlack := parser.Color{Mode: parser.ColorModeStandard, Value: 8}
	expectedBrightWhite := parser.Color{Mode: parser.ColorModeStandard, Value: 15}

	AssertCellBackgroundColor(t, d, NewPoint(1, 1), expectedBrightBlack, "Should have bright black background")
	AssertCellBackgroundColor(t, d, NewPoint(2, 1), expectedBrightWhite, "Should have bright white background")
}

// Test_SGR_256ColorForeground tests 256-color mode for foreground (SGR 38;5;n).
func Test_SGR_256ColorForeground(t *testing.T) {
	d := NewDriver(80, 24)

	// SGR 38;5;196 = 256-color mode, color 196 (bright red)
	SGR(d, 38, 5, 196)
	d.WriteRaw("R")

	// SGR 38;5;21 = 256-color mode, color 21 (blue)
	SGR(d, 38, 5, 21)
	d.WriteRaw("B")

	expectedRed := parser.Color{Mode: parser.ColorMode256, Value: 196}
	expectedBlue := parser.Color{Mode: parser.ColorMode256, Value: 21}

	AssertCellForegroundColor(t, d, NewPoint(1, 1), expectedRed, "Should have 256-color red foreground")
	AssertCellForegroundColor(t, d, NewPoint(2, 1), expectedBlue, "Should have 256-color blue foreground")
}

// Test_SGR_256ColorBackground tests 256-color mode for background (SGR 48;5;n).
func Test_SGR_256ColorBackground(t *testing.T) {
	d := NewDriver(80, 24)

	// SGR 48;5;226 = 256-color mode, color 226 (yellow)
	SGR(d, 48, 5, 226)
	d.WriteRaw("Y")

	expectedYellow := parser.Color{Mode: parser.ColorMode256, Value: 226}
	AssertCellBackgroundColor(t, d, NewPoint(1, 1), expectedYellow, "Should have 256-color yellow background")
}

// Test_SGR_RGBForeground tests RGB true-color for foreground (SGR 38;2;r;g;b).
func Test_SGR_RGBForeground(t *testing.T) {
	d := NewDriver(80, 24)

	// SGR 38;2;255;128;0 = RGB orange
	SGR(d, 38, 2, 255, 128, 0)
	d.WriteRaw("O")

	expectedOrange := parser.Color{Mode: parser.ColorModeRGB, R: 255, G: 128, B: 0}
	AssertCellForegroundColor(t, d, NewPoint(1, 1), expectedOrange, "Should have RGB orange foreground")
}

// Test_SGR_RGBBackground tests RGB true-color for background (SGR 48;2;r;g;b).
func Test_SGR_RGBBackground(t *testing.T) {
	d := NewDriver(80, 24)

	// SGR 48;2;64;224;208 = RGB turquoise
	SGR(d, 48, 2, 64, 224, 208)
	d.WriteRaw("T")

	expectedTurquoise := parser.Color{Mode: parser.ColorModeRGB, R: 64, G: 224, B: 208}
	AssertCellBackgroundColor(t, d, NewPoint(1, 1), expectedTurquoise, "Should have RGB turquoise background")
}

// Test_SGR_CombinedAttributesAndColors tests combining attributes with colors.
func Test_SGR_CombinedAttributesAndColors(t *testing.T) {
	d := NewDriver(80, 24)

	// Set bold, underline, red foreground, blue background all at once
	SGR(d, SGR_BOLD, SGR_UNDERLINE, SGR_FG_RED, SGR_BG_BLUE)
	d.WriteRaw("X")

	// Check attributes
	AssertCellHasAttribute(t, d, NewPoint(1, 1), parser.AttrBold, "Should be bold")
	AssertCellHasAttribute(t, d, NewPoint(1, 1), parser.AttrUnderline, "Should be underlined")

	// Check colors
	expectedRed := parser.Color{Mode: parser.ColorModeStandard, Value: 1}
	expectedBlue := parser.Color{Mode: parser.ColorModeStandard, Value: 4}
	AssertCellForegroundColor(t, d, NewPoint(1, 1), expectedRed, "Should have red foreground")
	AssertCellBackgroundColor(t, d, NewPoint(1, 1), expectedBlue, "Should have blue background")
}

// Test_SGR_ResetDoesNotAffectColors tests that color resets are independent.
func Test_SGR_ResetDoesNotAffectColors(t *testing.T) {
	d := NewDriver(80, 24)

	// Set colors
	SGR(d, SGR_FG_RED, SGR_BG_BLUE)
	// Reset attributes (but not colors)
	SGR(d, SGR_RESET)
	d.WriteRaw("X")

	// Colors should be reset to default by SGR 0
	AssertCellForegroundColor(t, d, NewPoint(1, 1), parser.DefaultFG, "SGR 0 should reset foreground")
	AssertCellBackgroundColor(t, d, NewPoint(1, 1), parser.DefaultBG, "SGR 0 should reset background")
}

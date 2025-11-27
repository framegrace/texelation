// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This package is derived from esctest2 by George Nachman and Thomas E. Dickey.
// Original project: https://github.com/ThomasDickey/esctest2
// License: GPL v2
//
// The tests have been converted from Python to Go to enable offline, deterministic
// testing of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

import (
	"texelation/apps/texelterm/parser"
)

// Driver provides a headless interface to a texelterm instance for testing.
// It allows sending escape sequences and text, and querying terminal state.
type Driver struct {
	vterm  *parser.VTerm
	parser *parser.Parser
	width  int
	height int
}

// NewDriver creates a new headless terminal driver with the given dimensions.
func NewDriver(width, height int) *Driver {
	vterm := parser.NewVTerm(width, height)
	p := parser.NewParser(vterm)

	return &Driver{
		vterm:  vterm,
		parser: p,
		width:  width,
		height: height,
	}
}

// Write sends text to the terminal (without parsing escape sequences in it).
// This is equivalent to escio.Write() in the original Python tests.
func (d *Driver) Write(text string) {
	for _, r := range text {
		d.parser.Parse(r)
	}
}

// WriteRaw sends raw bytes to the terminal parser, including escape sequences.
// Use this when you need to send control characters or escape sequences.
func (d *Driver) WriteRaw(data string) {
	for _, r := range data {
		d.parser.Parse(r)
	}
}

// GetCursorPosition returns the current cursor position (1-indexed).
// In origin mode, returns position relative to scroll region margins.
func (d *Driver) GetCursorPosition() Point {
	// VTerm uses 0-indexed cursor, but VT standards use 1-indexed
	x, y := d.vterm.Cursor()

	// In origin mode, report relative to scroll region
	if d.vterm.OriginMode() {
		marginTop, marginLeft := d.vterm.ScrollMargins()
		x -= marginLeft
		y -= marginTop
	}

	return NewPoint(x+1, y+1)
}

// GetScreenSize returns the terminal dimensions in cells.
func (d *Driver) GetScreenSize() Size {
	return NewSize(d.width, d.height)
}

// GetScreenCharsInRect returns the characters in the specified rectangle.
// The rectangle is 1-indexed to match VT conventions.
func (d *Driver) GetScreenCharsInRect(rect Rect) []string {
	grid := d.vterm.Grid()
	lines := make([]string, 0, rect.Height())

	for y := rect.Top; y <= rect.Bottom; y++ {
		if y < 1 || y > d.height {
			lines = append(lines, "")
			continue
		}

		line := ""
		for x := rect.Left; x <= rect.Right; x++ {
			if x < 1 || x > d.width {
				line += " "
				continue
			}

			cell := grid[y-1][x-1] // Convert to 0-indexed
			if cell.Rune == 0 {
				line += " "
			} else {
				line += string(cell.Rune)
			}
		}
		lines = append(lines, line)
	}

	return lines
}

// GetScreenChar returns the character at the specified position (1-indexed).
func (d *Driver) GetScreenChar(p Point) rune {
	if p.X < 1 || p.X > d.width || p.Y < 1 || p.Y > d.height {
		return ' '
	}

	grid := d.vterm.Grid()
	cell := grid[p.Y-1][p.X-1]
	if cell.Rune == 0 {
		return ' '
	}
	return cell.Rune
}

// Reset resets the terminal to its initial state.
func (d *Driver) Reset() {
	d.vterm = parser.NewVTerm(d.width, d.height)
	d.parser = parser.NewParser(d.vterm)
}

// GetCellAt returns the cell at the specified position (1-indexed).
// Returns nil if the position is out of bounds.
func (d *Driver) GetCellAt(p Point) *parser.Cell {
	if p.X < 1 || p.X > d.width || p.Y < 1 || p.Y > d.height {
		return nil
	}

	grid := d.vterm.Grid()
	cell := grid[p.Y-1][p.X-1]
	return &cell
}

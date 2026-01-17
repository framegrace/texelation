// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/testutil/ansi_parser.go
// Summary: Parses ANSI escape sequences from tmux output to extract colors and attributes.
//
// This parser reconstructs a Cell grid from tmux's capture-pane -e output,
// enabling full color/attribute comparison against texelterm's output.
//
// Usage:
//   parser := NewANSIParser(80, 24)
//   grid := parser.ParseTmuxOutput(tmuxOutput)
//   // grid now contains Cell structs with Rune, FG, BG, Attr

package testutil

import (
	"strconv"
	"strings"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// ANSIParser parses ANSI escape sequences and builds a Cell grid.
type ANSIParser struct {
	width, height    int
	grid             [][]parser.Cell
	cursorX, cursorY int
	state            SGRState
}

// SGRState tracks the current text styling state during ANSI parsing.
type SGRState struct {
	FG   parser.Color
	BG   parser.Color
	Attr parser.Attribute
}

// NewANSIParser creates a parser for tmux output with specified dimensions.
func NewANSIParser(width, height int) *ANSIParser {
	grid := make([][]parser.Cell, height)
	for y := range grid {
		grid[y] = make([]parser.Cell, width)
		// Initialize with default colors (space with default FG/BG)
		for x := range grid[y] {
			grid[y][x] = parser.Cell{
				Rune: ' ',
				FG:   parser.Color{Mode: parser.ColorModeDefault},
				BG:   parser.Color{Mode: parser.ColorModeDefault},
			}
		}
	}
	return &ANSIParser{
		width:  width,
		height: height,
		grid:   grid,
		state: SGRState{
			FG: parser.Color{Mode: parser.ColorModeDefault},
			BG: parser.Color{Mode: parser.ColorModeDefault},
		},
	}
}

// ParseTmuxOutput parses tmux capture-pane output with ANSI sequences.
// Returns a fully-populated Cell grid with colors and attributes.
func (p *ANSIParser) ParseTmuxOutput(data []byte) [][]parser.Cell {
	i := 0
	for i < len(data) {
		if data[i] == 0x1b && i+1 < len(data) && data[i+1] == '[' {
			// ESC [ - CSI sequence
			i += 2
			i = p.parseCSI(data, i)
		} else if data[i] == '\n' {
			// Newline - move to next row
			p.cursorY++
			p.cursorX = 0
			i++
		} else if data[i] == '\r' {
			// Carriage return - move to start of line
			p.cursorX = 0
			i++
		} else if data[i] >= 0x20 && data[i] < 0x7f {
			// Printable ASCII
			p.placeChar(rune(data[i]))
			i++
		} else if data[i] >= 0x80 {
			// UTF-8 sequence
			r, size := decodeUTF8(data[i:])
			if size > 0 {
				p.placeChar(r)
				i += size
			} else {
				i++
			}
		} else {
			// Other control character - skip
			i++
		}
	}
	return p.grid
}

// parseCSI parses a CSI sequence starting after ESC [
// Returns the index after the sequence.
func (p *ANSIParser) parseCSI(data []byte, start int) int {
	i := start
	params := []int{}
	currentParam := 0
	hasParam := false
	private := false

	// Check for private marker (?)
	if i < len(data) && data[i] == '?' {
		private = true
		i++
	}

	// Parse parameters (digits separated by semicolons)
	for i < len(data) {
		b := data[i]
		if b >= '0' && b <= '9' {
			currentParam = currentParam*10 + int(b-'0')
			hasParam = true
			i++
		} else if b == ';' {
			if hasParam {
				params = append(params, currentParam)
			} else {
				params = append(params, 0)
			}
			currentParam = 0
			hasParam = false
			i++
		} else if b >= 0x40 && b <= 0x7e {
			// Final byte
			if hasParam {
				params = append(params, currentParam)
			}
			i++
			p.executeCSI(rune(b), params, private)
			return i
		} else {
			// Intermediate byte or other - skip
			i++
		}
	}
	return i
}

// executeCSI executes a CSI command.
func (p *ANSIParser) executeCSI(command rune, params []int, private bool) {
	switch command {
	case 'm':
		// SGR - Select Graphic Rendition
		p.parseSGR(params)
	case 'H', 'f':
		// CUP - Cursor Position
		row := 1
		col := 1
		if len(params) >= 1 {
			row = params[0]
		}
		if len(params) >= 2 {
			col = params[1]
		}
		if row < 1 {
			row = 1
		}
		if col < 1 {
			col = 1
		}
		p.cursorY = row - 1
		p.cursorX = col - 1
	case 'K':
		// EL - Erase in Line
		mode := 0
		if len(params) >= 1 {
			mode = params[0]
		}
		p.eraseLine(mode)
	case 'J':
		// ED - Erase in Display
		mode := 0
		if len(params) >= 1 {
			mode = params[0]
		}
		p.eraseDisplay(mode)
	case 'A':
		// CUU - Cursor Up
		n := 1
		if len(params) >= 1 && params[0] > 0 {
			n = params[0]
		}
		p.cursorY -= n
		if p.cursorY < 0 {
			p.cursorY = 0
		}
	case 'B':
		// CUD - Cursor Down
		n := 1
		if len(params) >= 1 && params[0] > 0 {
			n = params[0]
		}
		p.cursorY += n
		if p.cursorY >= p.height {
			p.cursorY = p.height - 1
		}
	case 'C':
		// CUF - Cursor Forward
		n := 1
		if len(params) >= 1 && params[0] > 0 {
			n = params[0]
		}
		p.cursorX += n
		if p.cursorX >= p.width {
			p.cursorX = p.width - 1
		}
	case 'D':
		// CUB - Cursor Back
		n := 1
		if len(params) >= 1 && params[0] > 0 {
			n = params[0]
		}
		p.cursorX -= n
		if p.cursorX < 0 {
			p.cursorX = 0
		}
	case 'G':
		// CHA - Cursor Horizontal Absolute
		col := 1
		if len(params) >= 1 {
			col = params[0]
		}
		if col < 1 {
			col = 1
		}
		p.cursorX = col - 1
	case 'd':
		// VPA - Vertical Position Absolute
		row := 1
		if len(params) >= 1 {
			row = params[0]
		}
		if row < 1 {
			row = 1
		}
		p.cursorY = row - 1
	case 'X':
		// ECH - Erase Character
		n := 1
		if len(params) >= 1 && params[0] > 0 {
			n = params[0]
		}
		p.eraseCharacters(n)
	// Ignore other sequences for now
	}
}

// parseSGR interprets SGR (Select Graphic Rendition) sequences.
func (p *ANSIParser) parseSGR(params []int) {
	if len(params) == 0 {
		// SGR 0 - reset
		p.state = SGRState{
			FG: parser.Color{Mode: parser.ColorModeDefault},
			BG: parser.Color{Mode: parser.ColorModeDefault},
		}
		return
	}

	i := 0
	for i < len(params) {
		code := params[i]
		switch {
		case code == 0:
			// Reset
			p.state = SGRState{
				FG: parser.Color{Mode: parser.ColorModeDefault},
				BG: parser.Color{Mode: parser.ColorModeDefault},
			}
		case code == 1:
			// Bold
			p.state.Attr |= parser.AttrBold
		case code == 4:
			// Underline
			p.state.Attr |= parser.AttrUnderline
		case code == 7:
			// Reverse
			p.state.Attr |= parser.AttrReverse
		case code == 22:
			// Not bold
			p.state.Attr &= ^parser.AttrBold
		case code == 24:
			// Not underline
			p.state.Attr &= ^parser.AttrUnderline
		case code == 27:
			// Not reverse
			p.state.Attr &= ^parser.AttrReverse
		case code >= 30 && code <= 37:
			// Standard foreground (30-37)
			p.state.FG = parser.Color{Mode: parser.ColorModeStandard, Value: uint8(code - 30)}
		case code == 38:
			// Extended foreground
			if i+2 < len(params) && params[i+1] == 5 {
				// 256-color: 38;5;n
				p.state.FG = parser.Color{Mode: parser.ColorMode256, Value: uint8(params[i+2])}
				i += 2
			} else if i+4 < len(params) && params[i+1] == 2 {
				// RGB: 38;2;r;g;b
				p.state.FG = parser.Color{
					Mode: parser.ColorModeRGB,
					R:    uint8(params[i+2]),
					G:    uint8(params[i+3]),
					B:    uint8(params[i+4]),
				}
				i += 4
			}
		case code == 39:
			// Default foreground
			p.state.FG = parser.Color{Mode: parser.ColorModeDefault}
		case code >= 40 && code <= 47:
			// Standard background (40-47)
			p.state.BG = parser.Color{Mode: parser.ColorModeStandard, Value: uint8(code - 40)}
		case code == 48:
			// Extended background
			if i+2 < len(params) && params[i+1] == 5 {
				// 256-color: 48;5;n
				if params[i+2] >= 0 && params[i+2] <= 255 {
					p.state.BG = parser.Color{Mode: parser.ColorMode256, Value: uint8(params[i+2])}
				}
				i += 2
			} else if i+4 < len(params) && params[i+1] == 2 {
				// RGB: 48;2;r;g;b
				r, g, b := params[i+2], params[i+3], params[i+4]
				if r >= 0 && r <= 255 && g >= 0 && g <= 255 && b >= 0 && b <= 255 {
					p.state.BG = parser.Color{
						Mode: parser.ColorModeRGB,
						R:    uint8(r),
						G:    uint8(g),
						B:    uint8(b),
					}
				}
				i += 4
			}
		case code == 49:
			// Default background
			p.state.BG = parser.Color{Mode: parser.ColorModeDefault}
		case code >= 90 && code <= 97:
			// Bright foreground (90-97)
			p.state.FG = parser.Color{Mode: parser.ColorModeStandard, Value: uint8(code - 90 + 8)}
		case code >= 100 && code <= 107:
			// Bright background (100-107)
			p.state.BG = parser.Color{Mode: parser.ColorModeStandard, Value: uint8(code - 100 + 8)}
		}
		i++
	}
}

// placeChar writes a character at the current cursor position.
// When cursor reaches the right edge, it stays there (allowing overwrite on next char).
// This matches tmux behavior for text at the right margin.
func (p *ANSIParser) placeChar(r rune) {
	if p.cursorY >= 0 && p.cursorY < p.height && p.cursorX >= 0 && p.cursorX < p.width {
		p.grid[p.cursorY][p.cursorX] = parser.Cell{
			Rune: r,
			FG:   p.state.FG,
			BG:   p.state.BG,
			Attr: p.state.Attr,
		}
		// Only advance if not at the right edge
		if p.cursorX < p.width-1 {
			p.cursorX++
		}
	}
}

// eraseLine erases part of the current line.
func (p *ANSIParser) eraseLine(mode int) {
	if p.cursorY < 0 || p.cursorY >= p.height {
		return
	}
	blankCell := parser.Cell{
		Rune: ' ',
		FG:   parser.Color{Mode: parser.ColorModeDefault},
		BG:   p.state.BG, // Erase uses current BG
	}
	switch mode {
	case 0: // From cursor to end
		for x := p.cursorX; x < p.width; x++ {
			p.grid[p.cursorY][x] = blankCell
		}
	case 1: // From start to cursor
		for x := 0; x <= p.cursorX && x < p.width; x++ {
			p.grid[p.cursorY][x] = blankCell
		}
	case 2: // Entire line
		for x := 0; x < p.width; x++ {
			p.grid[p.cursorY][x] = blankCell
		}
	}
}

// eraseDisplay erases part of the display.
func (p *ANSIParser) eraseDisplay(mode int) {
	blankCell := parser.Cell{
		Rune: ' ',
		FG:   parser.Color{Mode: parser.ColorModeDefault},
		BG:   p.state.BG, // Erase uses current BG
	}
	switch mode {
	case 0: // From cursor to end
		// Current line from cursor
		for x := p.cursorX; x < p.width && p.cursorY < p.height; x++ {
			p.grid[p.cursorY][x] = blankCell
		}
		// Remaining lines
		for y := p.cursorY + 1; y < p.height; y++ {
			for x := 0; x < p.width; x++ {
				p.grid[y][x] = blankCell
			}
		}
	case 1: // From start to cursor
		// Lines before cursor
		for y := 0; y < p.cursorY; y++ {
			for x := 0; x < p.width; x++ {
				p.grid[y][x] = blankCell
			}
		}
		// Current line up to cursor
		for x := 0; x <= p.cursorX && x < p.width && p.cursorY < p.height; x++ {
			p.grid[p.cursorY][x] = blankCell
		}
	case 2, 3: // Entire screen
		for y := 0; y < p.height; y++ {
			for x := 0; x < p.width; x++ {
				p.grid[y][x] = blankCell
			}
		}
	}
}

// eraseCharacters erases n characters starting at cursor.
func (p *ANSIParser) eraseCharacters(n int) {
	if p.cursorY < 0 || p.cursorY >= p.height {
		return
	}
	blankCell := parser.Cell{
		Rune: ' ',
		FG:   parser.Color{Mode: parser.ColorModeDefault},
		BG:   p.state.BG, // ECH uses current BG
	}
	for i := 0; i < n && p.cursorX+i < p.width; i++ {
		p.grid[p.cursorY][p.cursorX+i] = blankCell
	}
}

// decodeUTF8 decodes a UTF-8 sequence and returns the rune and byte count.
func decodeUTF8(data []byte) (rune, int) {
	if len(data) == 0 {
		return 0, 0
	}
	b := data[0]
	if b < 0x80 {
		return rune(b), 1
	}
	if b < 0xC0 {
		return 0, 0 // Invalid
	}
	if b < 0xE0 {
		if len(data) < 2 {
			return 0, 0
		}
		return rune(b&0x1F)<<6 | rune(data[1]&0x3F), 2
	}
	if b < 0xF0 {
		if len(data) < 3 {
			return 0, 0
		}
		return rune(b&0x0F)<<12 | rune(data[1]&0x3F)<<6 | rune(data[2]&0x3F), 3
	}
	if b < 0xF8 {
		if len(data) < 4 {
			return 0, 0
		}
		return rune(b&0x07)<<18 | rune(data[1]&0x3F)<<12 | rune(data[2]&0x3F)<<6 | rune(data[3]&0x3F), 4
	}
	return 0, 0 // Invalid
}

// GetGrid returns the current grid state.
func (p *ANSIParser) GetGrid() [][]parser.Cell {
	return p.grid
}

// GetCursor returns the current cursor position.
func (p *ANSIParser) GetCursor() (x, y int) {
	return p.cursorX, p.cursorY
}

// ParseSGRString parses an SGR parameter string (e.g., "48;5;240" -> color info).
// Useful for debugging and testing.
func ParseSGRString(s string) (fg, bg parser.Color, attr parser.Attribute) {
	parts := strings.Split(s, ";")
	params := make([]int, 0, len(parts))
	for _, p := range parts {
		if n, err := strconv.Atoi(p); err == nil {
			params = append(params, n)
		}
	}
	parser := NewANSIParser(1, 1)
	parser.parseSGR(params)
	return parser.state.FG, parser.state.BG, parser.state.Attr
}

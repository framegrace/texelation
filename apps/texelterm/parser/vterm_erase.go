// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_erase.go
// Summary: Erase operations - screen, line, and character erasing.
// Usage: Part of VTerm terminal emulator.

package parser

// handleErase processes erase-related CSI commands (J, K, X, P, b).
func (v *VTerm) handleErase(command rune, params []int) {
	param := func(i int, defaultVal int) int {
		if i < len(params) && params[i] != 0 {
			return params[i]
		}
		return defaultVal
	}
	v.wrapNext = false
	switch command {
	case 'J': // Erase in Display
		v.ClearScreenMode(param(0, 0))
	case 'K': // Erase in Line
		v.ClearLine(param(0, 0))
	case 'P': // Delete Character
		v.DeleteCharacters(param(0, 1))
	case 'X': // Erase Character
		v.EraseCharacters(param(0, 1))
	case 'b': // REP - Repeat previous graphic character
		v.RepeatCharacter(param(0, 1))
	}
}

// ClearScreenMode handles ED (Erase in Display) with different modes.
func (v *VTerm) ClearScreenMode(mode int) {
	v.MarkAllDirty()
	switch mode {
	case 0: // Erase from cursor to end of screen
		if v.inAltScreen {
			// Clear from cursor to end of current line
			for x := v.cursorX; x < v.width; x++ {
				v.altBuffer[v.cursorY][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}
			// Clear all lines below cursor
			for y := v.cursorY + 1; y < v.height; y++ {
				for x := 0; x < v.width; x++ {
					v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
		} else if v.IsDisplayBufferEnabled() {
			// Use the display buffer's screen erase function
			v.displayBufferEraseScreen(0)
		}
	case 1: // Erase from beginning of screen to cursor
		if v.inAltScreen {
			// Clear all lines above cursor
			for y := 0; y < v.cursorY; y++ {
				for x := 0; x < v.width; x++ {
					v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
			// Clear from start to cursor on current line
			for x := 0; x <= v.cursorX && x < v.width; x++ {
				v.altBuffer[v.cursorY][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}
		} else if v.IsDisplayBufferEnabled() {
			// Use the display buffer's screen erase function
			v.displayBufferEraseScreen(1)
		}
	case 2: // Erase entire visible screen (ED 2)
		if v.inAltScreen {
			for y := range v.altBuffer {
				for x := range v.altBuffer[y] {
					v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
		} else if v.IsDisplayBufferEnabled() {
			// Use the display buffer's screen erase function
			v.displayBufferEraseScreen(2)
		}
	case 3: // Erase scrollback only, leave visible screen intact (ED 3)
		if !v.inAltScreen && v.displayBuf != nil && v.displayBuf.history != nil {
			// Clear scrollback history while preserving the current (uncommitted) line
			v.displayBuf.history.ClearScrollback()
			v.MarkAllDirty()
		}
		// On alt screen, ED 3 does nothing (no scrollback to clear)
	}
}

// ClearLine handles EL (Erase in Line) with different modes.
func (v *VTerm) ClearLine(mode int) {
	v.MarkDirty(v.cursorY)

	// For main screen with display buffer, use display buffer operations only.
	// The display buffer handles logical lines which may span multiple physical rows.
	if !v.inAltScreen && v.IsDisplayBufferEnabled() {
		switch mode {
		case 0:
			v.displayBufferEraseToEndOfLine()
		case 1:
			v.displayBufferEraseFromStartOfLine()
		case 2:
			v.displayBufferEraseLine()
		}
		return // Display buffer handles everything, don't fall through to legacy code
	}

	// Alt screen: manipulate altBuffer directly
	var line []Cell
	if v.inAltScreen {
		line = v.altBuffer[v.cursorY]
	} else {
		// This path should only be reached if display buffer is not enabled
		return
	}

	start, end := 0, v.width
	switch mode {
	case 0: // Erase from cursor to end
		start = v.cursorX
	case 1: // Erase from beginning to cursor
		end = v.cursorX + 1
	case 2: // Erase entire line
	}

	for len(line) < v.width { // Ensure line is full width before clearing
		line = append(line, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
	}

	for x := start; x < end; x++ {
		if x < len(line) {
			line[x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}
	}

	// If we're erasing to the end of line, truncate any content beyond 'end'
	if mode == 0 || mode == 2 {
		if len(line) > end {
			line = line[:end]
		}
	}

	v.altBuffer[v.cursorY] = line
}

// EraseCharacters handles ECH (Erase Character) - replaces n characters with blanks.
func (v *VTerm) EraseCharacters(n int) {
	v.MarkDirty(v.cursorY)

	// Update display buffer if enabled
	if !v.inAltScreen && v.IsDisplayBufferEnabled() {
		v.displayBufferEraseCharacters(n)
	}

	var line []Cell
	logicalY := v.cursorY + v.getTopHistoryLine()
	if v.inAltScreen {
		line = v.altBuffer[v.cursorY]
	} else {
		// Ensure line exists
		for v.getHistoryLen() <= logicalY {
			v.appendHistoryLine(make([]Cell, 0, v.width))
		}
		line = v.getHistoryLine(logicalY)
		if line == nil {
			line = make([]Cell, 0, v.width)
		}
	}

	for i := 0; i < n; i++ {
		if v.cursorX+i < len(line) {
			line[v.cursorX+i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}
	}

	// Write the modified line back to the appropriate buffer
	if v.inAltScreen {
		v.altBuffer[v.cursorY] = line
	} else {
		v.setHistoryLine(logicalY, line)
	}
}

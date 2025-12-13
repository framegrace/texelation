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
		v.ClearLine(0) // Clear from cursor to end of current line
		if v.inAltScreen {
			// Clear all lines below cursor
			for y := v.cursorY + 1; y < v.height; y++ {
				for x := 0; x < v.width; x++ {
					v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
		} else {
			// For main screen, clear all lines below cursor by clearing them individually
			logicalY := v.cursorY + v.getTopHistoryLine()
			endY := v.getTopHistoryLine() + v.height
			blankLine := make([]Cell, v.width)
			for x := 0; x < v.width; x++ {
				blankLine[x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}
			// Ensure all lines exist in history up to end of viewport
			for v.getHistoryLen() < endY {
				v.appendHistoryLine(make([]Cell, 0, v.width))
			}
			// Now clear lines below cursor
			for y := logicalY + 1; y < endY; y++ {
				v.setHistoryLine(y, append([]Cell(nil), blankLine...))
			}
		}
	case 1: // Erase from beginning of screen to cursor
		v.ClearLine(1)
		if v.inAltScreen {
			for y := 0; y < v.cursorY; y++ {
				for x := 0; x < v.width; x++ {
					v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
		} else {
			logicalY := v.cursorY + v.getTopHistoryLine()
			blankLine := make([]Cell, v.width)
			for x := 0; x < v.width; x++ {
				blankLine[x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}
			for i := v.getTopHistoryLine(); i < logicalY; i++ {
				v.setHistoryLine(i, append([]Cell(nil), blankLine...))
			}
		}
	case 2: // Erase entire visible screen (ED 2)
		v.ClearVisibleScreen()
	case 3: // Erase scrollback only, leave visible screen intact (ED 3)
		if !v.inAltScreen {
			// Clear scrollback by resetting history to only contain visible screen
			topHistory := v.getTopHistoryLine()
			newHistory := make([][]Cell, v.maxHistorySize)

			// Copy visible screen lines to new history buffer starting at position 0
			for i := 0; i < v.height; i++ {
				oldLine := v.getHistoryLine(topHistory + i)
				if oldLine != nil {
					newHistory[i] = append([]Cell(nil), oldLine...)
				} else {
					newHistory[i] = make([]Cell, 0, v.width)
				}
			}

			// Replace history buffer and reset pointers
			v.historyBuffer = newHistory
			v.historyHead = 0
			v.historyLen = v.height
			v.viewOffset = 0
		}
		// On alt screen, ED 3 does nothing (no scrollback to clear)
	}
}

// ClearLine handles EL (Erase in Line) with different modes.
func (v *VTerm) ClearLine(mode int) {
	v.MarkDirty(v.cursorY)

	// Update display buffer if enabled (only for main screen, current line)
	if !v.inAltScreen && v.IsDisplayBufferEnabled() {
		switch mode {
		case 0:
			v.displayBufferEraseToEndOfLine()
		case 1:
			v.displayBufferEraseFromStartOfLine()
		case 2:
			v.displayBufferEraseLine()
		}
	}

	var line []Cell
	var logicalY int
	if v.inAltScreen {
		line = v.altBuffer[v.cursorY]
	} else {
		logicalY = v.cursorY + v.getTopHistoryLine()
		// Ensure line exists
		for v.getHistoryLen() <= logicalY {
			v.appendHistoryLine(make([]Cell, 0, v.width))
		}
		line = v.getHistoryLine(logicalY)
		if line == nil {
			line = make([]Cell, 0, v.width)
		}
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
	// This handles cases where the history line is longer than the terminal width
	if mode == 0 || mode == 2 { // EL 0 (cursor to end) or EL 2 (entire line)
		if len(line) > end {
			line = line[:end]
		}
	}

	// Write the modified line back to the appropriate buffer
	if v.inAltScreen {
		v.altBuffer[v.cursorY] = line
	} else {
		v.setHistoryLine(logicalY, line)
	}
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

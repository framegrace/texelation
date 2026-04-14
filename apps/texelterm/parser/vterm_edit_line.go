// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_edit_line.go
// Summary: Line-level editing operations - insert and delete lines.
// Usage: Part of VTerm terminal emulator.

package parser

// InsertLines handles IL (Insert Line) - inserts n blank lines at cursor.
func (v *VTerm) InsertLines(n int) {
	v.wrapNext = false

	// Check if cursor is within top/bottom margins
	if v.cursorY < v.marginTop || v.cursorY > v.marginBottom {
		return
	}

	// Check if cursor is within left/right margins when DECLRMM is active
	if v.leftRightMarginMode && (v.cursorX < v.marginLeft || v.cursorX > v.marginRight) {
		return
	}

	// When DECLRMM is active, only insert content within left/right margins
	if v.leftRightMarginMode {
		v.insertLinesWithinMargins(n)
	} else {
		v.insertFullLines(n)
	}

	v.MarkAllDirty()
}

// insertFullLines inserts entire blank lines (traditional IL behavior)
func (v *VTerm) insertFullLines(n int) {
	// IL works within the scroll region for both alt and main screens
	// Shift lines down, starting from bottom to avoid overwriting source data

	if v.inAltScreen {
		for i := 0; i < n; i++ {
			// Alt screen: shift rows down and clear at cursor
			for y := v.marginBottom - 1; y >= v.cursorY; y-- {
				if y+1 <= v.marginBottom {
					v.altBuffer[y+1] = v.altBuffer[y]
				}
			}
			// Create blank line at cursor position
			v.altBuffer[v.cursorY] = make([]Cell, v.width)
			v.altBufferClearRow(v.cursorY, v.defaultFG, v.defaultBG)
		}
	} else {
		// Main screen: delegate to sparse InsertLines.
		if v.mainScreen != nil {
			v.mainScreen.InsertLines(n, v.cursorY, v.marginTop, v.marginBottom)
		}
	}
}

// insertLinesWithinMargins inserts blank content within left/right margins only
func (v *VTerm) insertLinesWithinMargins(n int) {
	leftCol := v.marginLeft
	rightCol := v.marginRight

	if v.inAltScreen {
		// Shift content within margins downward
		for y := v.marginBottom; y >= v.cursorY+n; y-- {
			srcY := y - n
			if srcY >= v.cursorY {
				v.altBufferCopyRow(srcY, y, leftCol, rightCol)
			}
		}
		// Clear the top n lines' margin regions (starting at cursor)
		endY := v.cursorY + n - 1
		if endY > v.marginBottom {
			endY = v.marginBottom
		}
		v.altBufferClearRegion(leftCol, v.cursorY, rightCol, endY, v.defaultFG, v.defaultBG)
	} else {
		// Main screen with history buffer
		topHistory := v.getTopHistoryLine()

		// Shift content within margins downward
		for y := v.marginBottom; y >= v.cursorY+n; y-- {
			srcY := y - n
			if srcY >= v.cursorY {
				// Copy slices to avoid mutating MemoryBuffer backing array
				// directly, which races with the WAL persistence goroutine.
				dstLine := append([]Cell{}, v.getHistoryLine(topHistory+y)...)
				srcLine := append([]Cell{}, v.getHistoryLine(topHistory+srcY)...)

				// Ensure lines are wide enough
				for len(dstLine) <= rightCol {
					dstLine = append(dstLine, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}
				for len(srcLine) <= rightCol {
					srcLine = append(srcLine, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}

				// Copy margin region
				copy(dstLine[leftCol:rightCol+1], srcLine[leftCol:rightCol+1])
				v.setHistoryLine(topHistory+y, dstLine)
			}
		}

		// Clear the top n lines' margin regions
		for y := v.cursorY; y < v.cursorY+n && y <= v.marginBottom; y++ {
			if y >= 0 {
				line := append([]Cell{}, v.getHistoryLine(topHistory+y)...)
				for len(line) <= rightCol {
					line = append(line, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}
				for x := leftCol; x <= rightCol; x++ {
					line[x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
				}
				v.setHistoryLine(topHistory+y, line)
			}
		}
	}
}

// DeleteLines handles DL (Delete Line) - deletes n lines at cursor.
func (v *VTerm) DeleteLines(n int) {
	v.wrapNext = false

	// Check if cursor is within top/bottom margins
	if v.cursorY < v.marginTop || v.cursorY > v.marginBottom {
		return
	}

	// Check if cursor is within left/right margins when DECLRMM is active
	if v.leftRightMarginMode && (v.cursorX < v.marginLeft || v.cursorX > v.marginRight) {
		return
	}

	// When DECLRMM is active, only delete content within left/right margins
	if v.leftRightMarginMode {
		v.deleteLinesWithinMargins(n)
	} else {
		v.deleteFullLines(n)
	}

	v.MarkAllDirty()
}

// deleteFullLines deletes entire lines (traditional DL behavior)
func (v *VTerm) deleteFullLines(n int) {
	// DL works within the scroll region for both alt and main screens

	if v.inAltScreen {
		for i := 0; i < n; i++ {
			// Alt screen: shift lines up
			for y := v.cursorY; y < v.marginBottom; y++ {
				v.altBuffer[y] = v.altBuffer[y+1]
			}
			// Create blank line at bottom of region
			v.altBuffer[v.marginBottom] = make([]Cell, v.width)
			v.altBufferClearRow(v.marginBottom, v.defaultFG, v.defaultBG)
		}
	} else {
		// Main screen: delegate to sparse DeleteLines.
		if v.mainScreen != nil {
			v.mainScreen.DeleteLines(n, v.cursorY, v.marginTop, v.marginBottom)
		}
	}
}

// deleteLinesWithinMargins deletes content within left/right margins only
func (v *VTerm) deleteLinesWithinMargins(n int) {
	leftCol := v.marginLeft
	rightCol := v.marginRight

	if v.inAltScreen {
		// Shift content within margins upward
		for y := v.cursorY; y <= v.marginBottom-n; y++ {
			srcY := y + n
			if srcY <= v.marginBottom {
				v.altBufferCopyRow(srcY, y, leftCol, rightCol)
			}
		}
		// Clear the bottom n lines' margin regions (clamped to cursor position)
		clearStart := v.marginBottom - n + 1
		if clearStart < v.cursorY {
			clearStart = v.cursorY
		}
		v.altBufferClearRegion(leftCol, clearStart, rightCol, v.marginBottom, v.defaultFG, v.defaultBG)
	} else {
		// Main screen with history buffer
		topHistory := v.getTopHistoryLine()

		// Shift content within margins upward
		for y := v.cursorY; y <= v.marginBottom-n; y++ {
			srcY := y + n
			if srcY <= v.marginBottom {
				// Copy slices to avoid mutating MemoryBuffer backing array
				// directly, which races with the WAL persistence goroutine.
				dstLine := append([]Cell{}, v.getHistoryLine(topHistory+y)...)
				srcLine := append([]Cell{}, v.getHistoryLine(topHistory+srcY)...)

				// Ensure lines are wide enough
				for len(dstLine) <= rightCol {
					dstLine = append(dstLine, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}
				for len(srcLine) <= rightCol {
					srcLine = append(srcLine, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}

				// Copy margin region
				copy(dstLine[leftCol:rightCol+1], srcLine[leftCol:rightCol+1])
				v.setHistoryLine(topHistory+y, dstLine)
			}
		}

		// Clear the bottom n lines' margin regions (clamped to cursor position)
		clearStart := v.marginBottom - n + 1
		if clearStart < v.cursorY {
			clearStart = v.cursorY
		}
		for y := clearStart; y <= v.marginBottom; y++ {
			if y >= 0 {
				line := append([]Cell{}, v.getHistoryLine(topHistory+y)...)
				for len(line) <= rightCol {
					line = append(line, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}
				for x := leftCol; x <= rightCol; x++ {
					line[x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
				}
				v.setHistoryLine(topHistory+y, line)
			}
		}
	}
}


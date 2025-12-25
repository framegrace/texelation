// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_edit_char.go
// Summary: Character-level editing operations - insert, delete, repeat.
// Usage: Part of VTerm terminal emulator.

package parser

// InsertCharacters handles ICH (Insert Character) - inserts n blank characters at cursor.
func (v *VTerm) InsertCharacters(n int) {
	v.MarkDirty(v.cursorY)
	if v.cursorX >= v.width {
		return
	}

	// Determine the effective right boundary
	rightBoundary := v.width
	if v.leftRightMarginMode {
		// If DECLRMM is enabled and cursor is outside margins, do nothing
		if v.cursorX < v.marginLeft || v.cursorX > v.marginRight {
			return
		}
		rightBoundary = v.marginRight + 1
	}

	// Update display buffer if enabled (main screen only)
	// TODO: ICH with display buffer currently works for non-wrapped lines only.
	// For wrapped lines, the insertion needs to account for physical row boundaries
	// and potentially reflow content across multiple physical rows.
	if !v.inAltScreen && v.IsDisplayBufferEnabled() {
		v.displayBufferInsertCharacters(n)
	}

	if v.inAltScreen {
		line := v.altBuffer[v.cursorY]
		// Calculate how many chars to copy and where they should end
		segmentStart := v.cursorX
		segmentEnd := rightBoundary
		if segmentEnd > len(line) {
			segmentEnd = len(line)
		}

		// Create a copy of the segment that will be shifted
		segmentLen := segmentEnd - segmentStart
		if segmentLen > 0 {
			segment := make([]Cell, segmentLen)
			copy(segment, line[segmentStart:segmentEnd])

			// Insert blanks at cursor position
			blanksToInsert := n
			if v.cursorX+blanksToInsert > rightBoundary {
				blanksToInsert = rightBoundary - v.cursorX
			}
			for i := 0; i < blanksToInsert; i++ {
				line[v.cursorX+i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}

			// Copy the original segment back, shifted right
			destStart := v.cursorX + n
			if destStart < rightBoundary {
				toCopy := rightBoundary - destStart
				if toCopy > len(segment) {
					toCopy = len(segment)
				}
				copy(line[destStart:rightBoundary], segment[:toCopy])
			}
		}
	} else {
		// Main screen with history buffer
		logicalY := v.cursorY + v.getTopHistoryLine()
		line := v.getHistoryLine(logicalY)

		// Insert blanks at cursor position
		blanks := make([]Cell, n)
		for i := range blanks {
			blanks[i] = Cell{Rune: ' '}
		}

		if v.leftRightMarginMode {
			// With left/right margins: preserve everything outside margins
			// Build: [before cursor] + [blanks] + [cursor to right margin - n chars] + [after right margin]
			newLine := append([]Cell{}, line[:v.cursorX]...)
			newLine = append(newLine, blanks...)

			// Add shifted content within margins (up to right boundary - n)
			copyEnd := rightBoundary - n
			if copyEnd > len(line) {
				copyEnd = len(line)
			}
			if copyEnd > v.cursorX {
				newLine = append(newLine, line[v.cursorX:copyEnd]...)
			}

			// Preserve everything after the right margin
			if rightBoundary < len(line) {
				newLine = append(newLine, line[rightBoundary:]...)
			}

			v.setHistoryLine(logicalY, newLine)
		} else {
			// No margins: insert and shift entire line
			newLine := append(line[:v.cursorX], append(blanks, line[v.cursorX:]...)...)
			v.setHistoryLine(logicalY, newLine)
		}
	}
}

// DeleteCharacters handles DCH (Delete Character) - deletes n characters at cursor.
func (v *VTerm) DeleteCharacters(n int) {
	v.MarkDirty(v.cursorY)
	if v.cursorX >= v.width {
		return
	}

	// Determine the effective right boundary
	rightBoundary := v.width
	if v.leftRightMarginMode {
		// If DECLRMM is enabled and cursor is outside margins, do nothing
		if v.cursorX < v.marginLeft || v.cursorX > v.marginRight {
			return
		}
		rightBoundary = v.marginRight + 1
	}

	// Update display buffer if enabled (main screen only)
	if !v.inAltScreen && v.IsDisplayBufferEnabled() {
		v.displayBufferDeleteCharacters(n)
	}

	if v.inAltScreen {
		line := v.altBuffer[v.cursorY]
		if v.cursorX < len(line) {
			// Determine how many characters to delete
			deleteCount := n
			if v.cursorX+deleteCount > rightBoundary {
				deleteCount = rightBoundary - v.cursorX
			}

			// Shift characters from the right to the left within the boundary
			copySrcStart := v.cursorX + deleteCount
			if copySrcStart < rightBoundary {
				// Shift characters from the right to the left
				copyLen := rightBoundary - copySrcStart
				copy(line[v.cursorX:v.cursorX+copyLen], line[copySrcStart:rightBoundary])
			}

			// Clear the now-empty cells at the end of the region
			clearStart := rightBoundary - deleteCount
			if clearStart < v.cursorX {
				clearStart = v.cursorX
			}
			for i := clearStart; i < rightBoundary; i++ {
				line[i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}
		}
	} else {
		// Main screen with history buffer
		logicalY := v.cursorY + v.getTopHistoryLine()
		line := v.getHistoryLine(logicalY)
		if v.cursorX >= len(line) {
			return
		}

		// For main screen, expand line if needed to rightBoundary
		if len(line) < rightBoundary {
			expanded := make([]Cell, rightBoundary)
			copy(expanded, line)
			for i := len(line); i < rightBoundary; i++ {
				expanded[i] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
			}
			line = expanded
		}

		// Delete characters within the boundary
		deleteCount := n
		if v.cursorX+deleteCount > rightBoundary {
			deleteCount = rightBoundary - v.cursorX
		}

		// Shift characters left
		copy(line[v.cursorX:rightBoundary-deleteCount], line[v.cursorX+deleteCount:rightBoundary])

		// Clear the now-empty cells at the end
		for i := rightBoundary - deleteCount; i < rightBoundary; i++ {
			line[i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}

		v.setHistoryLine(logicalY, line)
	}
}

// RepeatCharacter (REP) repeats the last graphic character n times.
// REP respects both left/right and top/bottom margins.
func (v *VTerm) RepeatCharacter(n int) {
	if v.lastGraphicChar == 0 {
		return // No character to repeat
	}

	// Repeat the character n times
	for i := 0; i < n; i++ {
		v.placeChar(v.lastGraphicChar)
	}
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_edit_col.go
// Summary: Column-level editing operations - insert and delete columns.
// Usage: Part of VTerm terminal emulator.

package parser

// InsertColumns (DECIC) inserts blank columns at cursor position.
// CSI Pn ' } - VT420 feature for horizontal scrolling.
func (v *VTerm) InsertColumns(n int) {
	// Check if cursor is outside top/bottom margins
	if v.cursorY < v.marginTop || v.cursorY > v.marginBottom {
		return
	}

	// Determine effective left/right margins
	leftMargin := 0
	rightMargin := v.width - 1
	if v.leftRightMarginMode {
		leftMargin = v.marginLeft
		rightMargin = v.marginRight
		// If cursor is outside left/right margins, do nothing
		if v.cursorX < leftMargin || v.cursorX > rightMargin {
			return
		}
	}

	// Insert n columns at cursor position, shifting content right
	// Content beyond right margin is truncated
	if v.inAltScreen {
		buffer := v.altBuffer
		for y := v.marginTop; y <= v.marginBottom; y++ {
			if y >= len(buffer) {
				continue
			}
			line := buffer[y]
			// Ensure line is wide enough
			for len(line) <= rightMargin {
				line = append(line, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
			}
			// Shift content right from cursor to right margin
			for i := 0; i < n; i++ {
				for x := rightMargin; x > v.cursorX; x-- {
					if x > 0 && x-1 < len(line) {
						line[x] = line[x-1]
					}
				}
				// Insert blank at cursor position
				if v.cursorX < len(line) {
					line[v.cursorX] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
			buffer[y] = line
		}
	} else {
		// Main screen
		topHistory := v.getTopHistoryLine()
		for y := v.marginTop; y <= v.marginBottom; y++ {
			line := v.getHistoryLine(topHistory + y)
			// Ensure line is wide enough
			for len(line) <= rightMargin {
				line = append(line, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
			}
			// Shift content right from cursor to right margin
			for i := 0; i < n; i++ {
				for x := rightMargin; x > v.cursorX; x-- {
					if x > 0 && x-1 < len(line) {
						line[x] = line[x-1]
					}
				}
				// Insert blank at cursor position
				if v.cursorX < len(line) {
					line[v.cursorX] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
			v.setHistoryLine(topHistory+y, line)
		}
	}
	v.MarkAllDirty()
}

// DeleteColumns (DECDC) deletes columns at cursor position.
// CSI Pn ' ~ - VT420 feature for horizontal scrolling.
func (v *VTerm) DeleteColumns(n int) {
	// Check if cursor is outside top/bottom margins
	if v.cursorY < v.marginTop || v.cursorY > v.marginBottom {
		return
	}

	// Determine effective left/right margins
	leftMargin := 0
	rightMargin := v.width - 1
	if v.leftRightMarginMode {
		leftMargin = v.marginLeft
		rightMargin = v.marginRight
		// If cursor is outside left/right margins, do nothing
		if v.cursorX < leftMargin || v.cursorX > rightMargin {
			return
		}
	}

	// Delete n columns at cursor position, shifting content left
	// Blank columns inserted at right margin
	if v.inAltScreen {
		buffer := v.altBuffer
		for y := v.marginTop; y <= v.marginBottom; y++ {
			if y >= len(buffer) {
				continue
			}
			line := buffer[y]
			// Ensure line is wide enough
			for len(line) <= rightMargin {
				line = append(line, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
			}
			// Shift content left from cursor to right margin
			for i := 0; i < n; i++ {
				for x := v.cursorX; x < rightMargin; x++ {
					if x+1 < len(line) {
						line[x] = line[x+1]
					}
				}
				// Insert blank at right margin
				if rightMargin < len(line) {
					line[rightMargin] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
			buffer[y] = line
		}
	} else {
		// Main screen
		topHistory := v.getTopHistoryLine()
		for y := v.marginTop; y <= v.marginBottom; y++ {
			line := v.getHistoryLine(topHistory + y)
			// Ensure line is wide enough
			for len(line) <= rightMargin {
				line = append(line, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
			}
			// Shift content left from cursor to right margin
			for i := 0; i < n; i++ {
				for x := v.cursorX; x < rightMargin; x++ {
					if x+1 < len(line) {
						line[x] = line[x+1]
					}
				}
				// Insert blank at right margin
				if rightMargin < len(line) {
					line[rightMargin] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
			v.setHistoryLine(topHistory+y, line)
		}
	}
	v.MarkAllDirty()
}

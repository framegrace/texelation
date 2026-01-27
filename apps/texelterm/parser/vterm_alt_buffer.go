// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_alt_buffer.go
// Summary: Alt buffer operations for VTerm - consolidated cell writing and manipulation.
// Usage: Provides symmetric API to display buffer operations for alt screen handling.

package parser

// altBufferWriteCell writes a cell to the alt buffer at the current cursor position.
// Handles insert mode and wide character support.
// This parallels memoryBufferPlaceCharWide for main screen operations.
func (v *VTerm) altBufferWriteCell(r rune, isWide bool) {
	if !v.inAltScreen {
		return
	}
	if v.cursorY < 0 || v.cursorY >= v.height || v.cursorX < 0 || v.cursorX >= v.width {
		return
	}

	charWidth := 1
	if isWide {
		charWidth = 2
	}

	// Determine the effective right edge for insert mode shifting
	rightEdge := v.width - 1
	if v.leftRightMarginMode && v.cursorX >= v.marginLeft && v.cursorX <= v.marginRight {
		rightEdge = v.marginRight
	}

	v.logDebug("[ALT.Write] '%c' (0x%04X) at (%d,%d) width=%d", r, r, v.cursorX, v.cursorY, charWidth)

	if v.insertMode {
		// Shift content right from cursor to right edge
		for x := rightEdge; x >= v.cursorX+charWidth; x-- {
			v.altBuffer[v.cursorY][x] = v.altBuffer[v.cursorY][x-charWidth]
		}
	}

	// Place the main character
	v.altBuffer[v.cursorY][v.cursorX] = Cell{
		Rune: r,
		FG:   v.currentFG,
		BG:   v.currentBG,
		Attr: v.currentAttr,
		Wide: isWide,
	}

	// For wide characters, place a placeholder in the next cell
	if isWide && v.cursorX+1 < v.width {
		v.altBuffer[v.cursorY][v.cursorX+1] = Cell{
			Rune: 0,
			FG:   v.currentFG,
			BG:   v.currentBG,
			Attr: v.currentAttr,
			Wide: true,
		}
	}

	v.MarkDirty(v.cursorY)
}

// altBufferSetCell sets a cell at a specific position in the alt buffer.
// Does not handle insert mode or wide characters - for direct cell manipulation.
func (v *VTerm) altBufferSetCell(x, y int, cell Cell) {
	if !v.inAltScreen {
		return
	}
	if y < 0 || y >= v.height || x < 0 || x >= v.width {
		return
	}
	v.altBuffer[y][x] = cell
	v.MarkDirty(y)
}

// altBufferGetCell gets a cell at a specific position in the alt buffer.
func (v *VTerm) altBufferGetCell(x, y int) Cell {
	if !v.inAltScreen {
		return Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
	}
	if y < 0 || y >= v.height || x < 0 || x >= v.width {
		return Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
	}
	return v.altBuffer[y][x]
}

// altBufferClearCell clears a cell at the cursor position with current colors.
func (v *VTerm) altBufferClearCell(x, y int) {
	v.altBufferSetCell(x, y, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
}

// altBufferClearCellDefault clears a cell with default colors.
func (v *VTerm) altBufferClearCellDefault(x, y int) {
	v.altBufferSetCell(x, y, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
}

// altBufferClearRow clears an entire row in the alt buffer.
func (v *VTerm) altBufferClearRow(y int, fg, bg Color) {
	if !v.inAltScreen {
		return
	}
	if y < 0 || y >= v.height {
		return
	}
	for x := 0; x < v.width; x++ {
		v.altBuffer[y][x] = Cell{Rune: ' ', FG: fg, BG: bg}
	}
	v.MarkDirty(y)
}

// altBufferClearRegion clears a rectangular region in the alt buffer.
func (v *VTerm) altBufferClearRegion(x1, y1, x2, y2 int, fg, bg Color) {
	if !v.inAltScreen {
		return
	}
	// Clamp bounds
	if x1 < 0 {
		x1 = 0
	}
	if y1 < 0 {
		y1 = 0
	}
	if x2 >= v.width {
		x2 = v.width - 1
	}
	if y2 >= v.height {
		y2 = v.height - 1
	}

	for y := y1; y <= y2; y++ {
		for x := x1; x <= x2; x++ {
			v.altBuffer[y][x] = Cell{Rune: ' ', FG: fg, BG: bg}
		}
		v.MarkDirty(y)
	}
}

// altBufferCopyRow copies content from one row to another within a column range.
func (v *VTerm) altBufferCopyRow(srcY, dstY, startCol, endCol int) {
	if !v.inAltScreen {
		return
	}
	if srcY < 0 || srcY >= v.height || dstY < 0 || dstY >= v.height {
		return
	}
	if startCol < 0 {
		startCol = 0
	}
	if endCol >= v.width {
		endCol = v.width - 1
	}
	for x := startCol; x <= endCol; x++ {
		v.altBuffer[dstY][x] = v.altBuffer[srcY][x]
	}
	v.MarkDirty(dstY)
}

// altBufferScrollRegionUp scrolls content up within a row range (full rows).
// Content moves up, new blank row appears at bottom.
func (v *VTerm) altBufferScrollRegionUp(top, bottom, n int, clearFG, clearBG Color) {
	if !v.inAltScreen || n <= 0 {
		return
	}
	if top < 0 {
		top = 0
	}
	if bottom >= v.height {
		bottom = v.height - 1
	}
	if top > bottom {
		return
	}

	for i := 0; i < n; i++ {
		// Use efficient slice copy for row movement
		copy(v.altBuffer[top:bottom], v.altBuffer[top+1:bottom+1])
		// Clear the new bottom row
		v.altBuffer[bottom] = make([]Cell, v.width)
		for x := 0; x < v.width; x++ {
			v.altBuffer[bottom][x] = Cell{Rune: ' ', FG: clearFG, BG: clearBG}
		}
	}
	for y := top; y <= bottom; y++ {
		v.MarkDirty(y)
	}
}

// altBufferScrollRegionDown scrolls content down within a row range (full rows).
// Content moves down, new blank row appears at top.
func (v *VTerm) altBufferScrollRegionDown(top, bottom, n int, clearFG, clearBG Color) {
	if !v.inAltScreen || n <= 0 {
		return
	}
	if top < 0 {
		top = 0
	}
	if bottom >= v.height {
		bottom = v.height - 1
	}
	if top > bottom {
		return
	}

	for i := 0; i < n; i++ {
		// Use efficient slice copy for row movement
		copy(v.altBuffer[top+1:bottom+1], v.altBuffer[top:bottom])
		// Clear the new top row
		v.altBuffer[top] = make([]Cell, v.width)
		for x := 0; x < v.width; x++ {
			v.altBuffer[top][x] = Cell{Rune: ' ', FG: clearFG, BG: clearBG}
		}
	}
	for y := top; y <= bottom; y++ {
		v.MarkDirty(y)
	}
}

// altBufferScrollColumnsHorizontal scrolls content horizontally within specified bounds.
// n > 0: shift right (blank inserted at left), n < 0: shift left (blank at right).
func (v *VTerm) altBufferScrollColumnsHorizontal(top, bottom, left, right, n int, clearFG, clearBG Color) {
	if !v.inAltScreen || n == 0 {
		return
	}
	if top < 0 {
		top = 0
	}
	if bottom >= v.height {
		bottom = v.height - 1
	}
	if left < 0 {
		left = 0
	}
	if right >= v.width {
		right = v.width - 1
	}
	if top > bottom || left > right {
		return
	}

	if n > 0 {
		// Scroll right: shift content right, insert blank at left
		for i := 0; i < n; i++ {
			for y := top; y <= bottom; y++ {
				for x := right; x > left; x-- {
					v.altBuffer[y][x] = v.altBuffer[y][x-1]
				}
				v.altBuffer[y][left] = Cell{Rune: ' ', FG: clearFG, BG: clearBG}
				v.MarkDirty(y)
			}
		}
	} else {
		// Scroll left: shift content left, insert blank at right
		for i := 0; i < -n; i++ {
			for y := top; y <= bottom; y++ {
				for x := left; x < right; x++ {
					v.altBuffer[y][x] = v.altBuffer[y][x+1]
				}
				v.altBuffer[y][right] = Cell{Rune: ' ', FG: clearFG, BG: clearBG}
				v.MarkDirty(y)
			}
		}
	}
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_scroll.go
// Summary: Scrolling operations - vertical and horizontal scrolling within margins.
// Usage: Part of VTerm terminal emulator.

package parser

// LineFeed moves the cursor down one line, scrolling if necessary.
// This is called for explicit LF characters - it commits the logical line.
func (v *VTerm) LineFeed() {
	v.lineFeedInternal(true) // true = commit logical line
}

// lineFeedForWrap is called when auto-wrapping - doesn't commit the logical line.
func (v *VTerm) lineFeedForWrap() {
	v.lineFeedInternal(false) // false = don't commit, just wrap
}

// lineFeedInternal handles the actual line feed logic.
// commitLogical: true if this is an explicit LF (commit line), false if auto-wrap (continue line)
func (v *VTerm) lineFeedInternal(commitLogical bool) {
	        v.wrapNext = false // Clear wrapNext flag when moving to new line
	                v.MarkDirty(v.cursorY)
	        
	                // Check if cursor is outside left/right margins - if so, don't scroll
	                outsideMargins := v.leftRightMarginMode && (v.cursorX < v.marginLeft || v.cursorX > v.marginRight)
	        
	                if v.inAltScreen {
		if v.cursorY == v.marginBottom {
			if !outsideMargins {
				v.scrollRegion(1, v.marginTop, v.marginBottom)
			}
		} else if v.cursorY < v.height-1 {
			v.SetCursorPos(v.cursorY+1, v.cursorX)
		}
	} else {
		// Commit current logical line to display buffer if enabled
		// Only commit on explicit LF, not on auto-wrap
		if commitLogical && v.IsDisplayBufferEnabled() {
			v.displayBufferLineFeed()
		}

		// Main screen: check if we're at bottom margin
		if v.cursorY == v.marginBottom {
			if !outsideMargins {
				v.scrollRegion(1, v.marginTop, v.marginBottom)
			}
		} else if v.cursorY < v.height-1 {
			// Only append history lines when cursor will actually move down
			logicalY := v.cursorY + v.getTopHistoryLine()
			if logicalY+1 >= v.getHistoryLen() {
				v.appendHistoryLine(make([]Cell, 0, v.width))
			}
			v.SetCursorPos(v.cursorY+1, v.cursorX)
		} else {
			// At bottom of screen but not at scroll region bottom: stay put
			                        v.viewOffset = 0 // Jump to the bottom
			                        v.MarkAllDirty()
			                }
			        }
			}
// scrollRegion scrolls a portion of the screen buffer up or down.
func (v *VTerm) scrollRegion(n int, top int, bottom int) {
	v.wrapNext = false

	if v.inAltScreen {
		buffer := v.altBuffer
		if n > 0 { // Scroll Up
			for i := 0; i < n; i++ {
				copy(buffer[top:bottom], buffer[top+1:bottom+1])
				buffer[bottom] = make([]Cell, v.width)
				for x := range buffer[bottom] {
					buffer[bottom][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
		} else { // Scroll Down
			for i := 0; i < -n; i++ {
				copy(buffer[top+1:bottom+1], buffer[top:bottom])
				buffer[top] = make([]Cell, v.width)
				for x := range buffer[top] {
					buffer[top][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
		}
	} else {
		// Main screen scrolling within margins
		topHistory := v.getTopHistoryLine()
		if n > 0 { // Scroll Up
			for i := 0; i < n; i++ {
				if top == 0 && v.getHistoryLen() >= v.height {
					// Scrolling at top with scrollback already present
					// Appending will shift topHistory, moving visible window up
					v.appendHistoryLine(make([]Cell, 0, v.width))
					topHistory = v.getTopHistoryLine()
					v.viewOffset = 0
				} else {
					// Either scrolling within a region (top > 0) or no scrollback yet
					// Manually shift lines
					for y := top; y < bottom; y++ {
						srcLine := v.getHistoryLine(topHistory + y + 1)
						v.setHistoryLine(topHistory+y, srcLine)
					}
					// Clear the bottom line of the region
					blankLine := make([]Cell, 0, v.width)
					v.setHistoryLine(topHistory+bottom, blankLine)

					// If scrolling at top, grow history to record the scroll
					if top == 0 {
						v.appendHistoryLine(make([]Cell, 0, v.width))
						// topHistory stays at 0 since histLen < height
					}
				}
			}
		} else { // Scroll Down
			// Ensure history buffer has all lines we'll be writing to
			endLogicalY := topHistory + bottom
			for v.getHistoryLen() <= endLogicalY {
				v.appendHistoryLine(make([]Cell, 0, v.width))
			}
			for i := 0; i < -n; i++ {
				// Move all lines in region down by one
				for y := bottom; y > top; y-- {
					srcLine := v.getHistoryLine(topHistory + y - 1)
					v.setHistoryLine(topHistory+y, srcLine)
				}
				// Clear the top line
				blankLine := make([]Cell, 0, v.width)
				v.setHistoryLine(topHistory+top, blankLine)
			}
		}
	}
	v.MarkAllDirty()
}

// scrollUpWithinMargins scrolls content up within the left/right margins.
// Similar to deleteLinesWithinMargins but operates on the entire top/bottom region.
func (v *VTerm) scrollUpWithinMargins(n int) {
	v.wrapNext = false
	leftCol := v.marginLeft
	rightCol := v.marginRight

	if v.inAltScreen {
		// Shift content within margins upward
		for y := v.marginTop; y <= v.marginBottom-n; y++ {
			srcY := y + n
			if srcY <= v.marginBottom {
				// Copy the margin region from source line to current line
				copy(v.altBuffer[y][leftCol:rightCol+1], v.altBuffer[srcY][leftCol:rightCol+1])
			}
		}
		// Clear the bottom n lines' margin regions
		clearStart := v.marginBottom - n + 1
		if clearStart < v.marginTop {
			clearStart = v.marginTop
		}
		for y := clearStart; y <= v.marginBottom; y++ {
			if y >= 0 && y < v.height {
				for x := leftCol; x <= rightCol; x++ {
					v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
				}
			}
		}
	} else {
		// Main screen with history buffer
		topHistory := v.getTopHistoryLine()

		// Ensure history has all required lines
		endLogicalY := topHistory + v.marginBottom
		for v.getHistoryLen() <= endLogicalY {
			v.appendHistoryLine(make([]Cell, 0, v.width))
		}

		// Shift content within margins upward
		for y := v.marginTop; y <= v.marginBottom-n; y++ {
			srcY := y + n
			if srcY <= v.marginBottom {
				dstLine := v.getHistoryLine(topHistory + y)
				srcLine := v.getHistoryLine(topHistory + srcY)

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

		// Clear the bottom n lines' margin regions
		clearStart := v.marginBottom - n + 1
		if clearStart < v.marginTop {
			clearStart = v.marginTop
		}
		for y := clearStart; y <= v.marginBottom; y++ {
			if y >= 0 {
				line := v.getHistoryLine(topHistory + y)
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
	v.MarkAllDirty()
}

// scrollDownWithinMargins scrolls content down within the left/right margins.
// Similar to insertLinesWithinMargins but operates on the entire top/bottom region.
func (v *VTerm) scrollDownWithinMargins(n int) {
	v.wrapNext = false
	leftCol := v.marginLeft
	rightCol := v.marginRight

	if v.inAltScreen {
		// Shift content within margins downward
		for y := v.marginBottom; y >= v.marginTop+n; y-- {
			srcY := y - n
			if srcY >= v.marginTop {
				// Copy the margin region from source line to current line
				copy(v.altBuffer[y][leftCol:rightCol+1], v.altBuffer[srcY][leftCol:rightCol+1])
			}
		}
		// Clear the top n lines' margin regions
		clearEnd := v.marginTop + n - 1
		if clearEnd > v.marginBottom {
			clearEnd = v.marginBottom
		}
		for y := v.marginTop; y <= clearEnd; y++ {
			if y >= 0 && y < v.height {
				for x := leftCol; x <= rightCol; x++ {
					v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
				}
			}
		}
	} else {
		// Main screen with history buffer
		topHistory := v.getTopHistoryLine()

		// Ensure history has all required lines
		endLogicalY := topHistory + v.marginBottom
		for v.getHistoryLen() <= endLogicalY {
			v.appendHistoryLine(make([]Cell, 0, v.width))
		}

		// Shift content within margins downward
		for y := v.marginBottom; y >= v.marginTop+n; y-- {
			srcY := y - n
			if srcY >= v.marginTop {
				dstLine := v.getHistoryLine(topHistory + y)
				srcLine := v.getHistoryLine(topHistory + srcY)

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
		clearEnd := v.marginTop + n - 1
		if clearEnd > v.marginBottom {
			clearEnd = v.marginBottom
		}
		for y := v.marginTop; y <= clearEnd; y++ {
			if y >= 0 {
				line := v.getHistoryLine(topHistory + y)
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
	v.MarkAllDirty()
}

// Scroll adjusts the viewport offset for the history buffer.
func (v *VTerm) Scroll(delta int) {
	if v.inAltScreen {
		return
	}

	// Use display buffer scroll if enabled
	if v.IsDisplayBufferEnabled() {
		v.displayBufferScroll(delta)
		v.MarkAllDirty()
		return
	}

	v.viewOffset -= delta
	if v.viewOffset < 0 {
		v.viewOffset = 0
	}
	histLen := v.getHistoryLen()
	maxOffset := histLen - v.height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if v.viewOffset > maxOffset {
		v.viewOffset = maxOffset
	}

	v.MarkAllDirty()
}

// scrollHorizontal scrolls content horizontally within specified margins.
// n > 0: scroll right (content shifts right, blank column inserted at left)
// n < 0: scroll left (content shifts left, blank column inserted at right)
func (v *VTerm) scrollHorizontal(n int, left int, right int, top int, bottom int) {
	if v.inAltScreen {
		buffer := v.altBuffer
		if n > 0 {
			// Scroll right: shift content right, insert blank at left margin
			for i := 0; i < n; i++ {
				for y := top; y <= bottom; y++ {
					if y >= len(buffer) {
						continue
					}
					line := buffer[y]
					// Ensure line is wide enough
					for len(line) <= right {
						line = append(line, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
					}
					// Shift columns right within margin region
					for x := right; x > left; x-- {
						line[x] = line[x-1]
					}
					// Insert blank at left margin
					line[left] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
					buffer[y] = line
				}
			}
		} else if n < 0 {
			// Scroll left: shift content left, insert blank at right margin
			for i := 0; i < -n; i++ {
				for y := top; y <= bottom; y++ {
					if y >= len(buffer) {
						continue
					}
					line := buffer[y]
					// Ensure line is wide enough
					for len(line) <= right {
						line = append(line, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
					}
					// Shift columns left within margin region
					for x := left; x < right; x++ {
						line[x] = line[x+1]
					}
					// Insert blank at right margin
					line[right] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
					buffer[y] = line
				}
			}
		}
	} else {
		// Main screen scrolling
		topHistory := v.getTopHistoryLine()
		if n > 0 {
			// Scroll right: shift content right, insert blank at left margin
			for i := 0; i < n; i++ {
				for y := top; y <= bottom; y++ {
					line := v.getHistoryLine(topHistory + y)
					// Ensure line is wide enough
					for len(line) <= right {
						line = append(line, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
					}
					// Shift columns right within margin region
					for x := right; x > left; x-- {
						line[x] = line[x-1]
					}
					// Insert blank at left margin
					line[left] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
					v.setHistoryLine(topHistory+y, line)
				}
			}
		} else if n < 0 {
			// Scroll left: shift content left, insert blank at right margin
			for i := 0; i < -n; i++ {
				for y := top; y <= bottom; y++ {
					line := v.getHistoryLine(topHistory + y)
					// Ensure line is wide enough
					for len(line) <= right {
						line = append(line, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
					}
					// Shift columns left within margin region
					for x := left; x < right; x++ {
						line[x] = line[x+1]
					}
					// Insert blank at right margin
					line[right] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
					v.setHistoryLine(topHistory+y, line)
				}
			}
		}
	}
	v.MarkAllDirty()
}

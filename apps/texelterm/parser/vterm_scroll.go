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
	v.logDebug("[LF] LineFeed called: cursorY=%d, cursorX=%d, commit=%v, marginBottom=%d, inAltScreen=%v",
		v.cursorY, v.cursorX, commitLogical, v.marginBottom, v.inAltScreen)
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
		// Main screen with display buffer: use display buffer for line management.
		// Only commit on explicit LF, not on auto-wrap.
		// IMPORTANT: Don't commit when using a custom scroll region (TUI apps).
		// Only commit when margins are at full screen (normal shell operation).
		isFullScreenMargins := v.marginTop == 0 && v.marginBottom == v.height-1
		if commitLogical && v.IsDisplayBufferEnabled() && isFullScreenMargins {
			v.displayBufferLineFeed()
		}

		// Main screen: check if we're at bottom margin
		if v.cursorY == v.marginBottom {
			if !outsideMargins {
				v.scrollRegion(1, v.marginTop, v.marginBottom)
			}
		} else if v.cursorY < v.height-1 {
			// Move cursor down - display buffer handles line management
			v.SetCursorPos(v.cursorY+1, v.cursorX)
		} else {
			// At bottom of screen but not at scroll region bottom: stay put
			v.ScrollToLiveEdge()
		}

		// Sync display buffer cursor after cursor movement
		if v.IsDisplayBufferEnabled() {
			v.displayBufferSetCursorFromPhysical(false)
		}
	}
}
// scrollRegion scrolls a portion of the screen buffer up or down.
func (v *VTerm) scrollRegion(n int, top int, bottom int) {
	v.logDebug("[SCROLL] scrollRegion: n=%d, top=%d, bottom=%d, inAltScreen=%v", n, top, bottom, v.inAltScreen)
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
		// Main screen scrolling with display buffer.
		// We need to actually scroll the viewport content within the scroll region.
		if v.IsDisplayBufferEnabled() && v.displayBuf != nil && v.displayBuf.display != nil {
			// Set erase color for blank lines created during scroll
			v.displayBuf.display.SetEraseColor(v.currentBG)
			if n > 0 { // Scroll Up - content moves up, new line at bottom
				v.displayBuf.display.ScrollRegionUp(top, bottom, n)
			} else { // Scroll Down - content moves down, new line at top
				v.displayBuf.display.ScrollRegionDown(top, bottom, -n)
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
	v.displayBufferScroll(delta)
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

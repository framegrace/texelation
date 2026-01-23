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
		if n > 0 {
			v.altBufferScrollRegionUp(top, bottom, n, v.currentFG, v.currentBG)
		} else if n < 0 {
			v.altBufferScrollRegionDown(top, bottom, -n, v.currentFG, v.currentBG)
		}
	} else {
		// Main screen scrolling with display buffer
		if v.IsDisplayBufferEnabled() && v.displayBuf != nil && v.displayBuf.display != nil {
			v.displayBuf.display.SetEraseColor(v.currentBG)
			if n > 0 {
				v.displayBuf.display.ScrollRegionUp(top, bottom, n)
			} else if n < 0 {
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
				v.altBufferCopyRow(srcY, y, leftCol, rightCol)
			}
		}
		// Clear the bottom n lines' margin regions
		clearStart := v.marginBottom - n + 1
		if clearStart < v.marginTop {
			clearStart = v.marginTop
		}
		v.altBufferClearRegion(leftCol, clearStart, rightCol, v.marginBottom, v.defaultFG, v.defaultBG)
	} else {
		// Main screen: use DisplayBuffer for proper viewport manipulation
		if v.IsDisplayBufferEnabled() && v.displayBuf != nil && v.displayBuf.display != nil {
			v.displayBuf.display.ScrollColumnsUp(v.marginTop, v.marginBottom, leftCol, rightCol, n, v.defaultFG, v.defaultBG)
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
				v.altBufferCopyRow(srcY, y, leftCol, rightCol)
			}
		}
		// Clear the top n lines' margin regions
		clearEnd := v.marginTop + n - 1
		if clearEnd > v.marginBottom {
			clearEnd = v.marginBottom
		}
		v.altBufferClearRegion(leftCol, v.marginTop, rightCol, clearEnd, v.defaultFG, v.defaultBG)
	} else {
		// Main screen: use DisplayBuffer for proper viewport manipulation
		if v.IsDisplayBufferEnabled() && v.displayBuf != nil && v.displayBuf.display != nil {
			v.displayBuf.display.ScrollColumnsDown(v.marginTop, v.marginBottom, leftCol, rightCol, n, v.defaultFG, v.defaultBG)
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
		v.altBufferScrollColumnsHorizontal(top, bottom, left, right, n, v.currentFG, v.currentBG)
	} else {
		// Main screen: use DisplayBuffer for proper viewport manipulation
		if v.IsDisplayBufferEnabled() && v.displayBuf != nil && v.displayBuf.display != nil {
			v.displayBuf.display.ScrollColumnsHorizontal(top, bottom, left, right, n, v.currentFG, v.currentBG)
		}
	}
	v.MarkAllDirty()
}

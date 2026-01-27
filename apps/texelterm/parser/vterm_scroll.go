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
	} else if v.IsMemoryBufferEnabled() {
		// New Phase 1-3 architecture: use MemoryBuffer for line management.
		isFullScreenMargins := v.marginTop == 0 && v.marginBottom == v.height-1

		// Auto-jump to live edge when NEW content is being created (explicit LF at full-screen margins).
		// This allows staying scrolled back during resize/redraw (which only redraws existing content),
		// while still jumping to live edge when new output appears (shell commands, background jobs, etc.)
		if commitLogical && isFullScreenMargins && !v.memoryBufferAtLiveEdge() {
			v.memoryBufferScrollToBottom()
			v.MarkAllDirty()
		}

		if v.cursorY == v.marginBottom {
			if isFullScreenMargins {
				// Full-screen scroll: just advance the viewport, don't copy content.
				// MemoryBuffer grows with new content; we just show a different window.
				if commitLogical {
					v.memoryBufferLineFeed()
				}
				// Cursor stays at bottom row - no SetCursorPos needed
				// Mark all dirty since viewport content shifted
				v.MarkAllDirty()
			} else {
				// Custom scroll region (TUI): need to actually shift content within region
				if !outsideMargins {
					v.scrollRegion(1, v.marginTop, v.marginBottom)
				}
			}
		} else if v.cursorY < v.height-1 {
			// Not at bottom: ensure next line exists and move cursor down
			if commitLogical && isFullScreenMargins {
				v.memoryBufferLineFeed()
			}
			v.SetCursorPos(v.cursorY+1, v.cursorX)
		} else {
			v.ScrollToLiveEdge()
		}

		// Sync memory buffer cursor after cursor movement
		v.memoryBufferSetCursorFromPhysical()
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
		// Use MemoryBuffer scroll region
		v.memoryBufferScrollRegion(n, top, bottom)
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
		// Main screen: scroll within margins using MemoryBuffer
		v.memBufScrollColumnsUp(v.marginTop, v.marginBottom, leftCol, rightCol, n, v.defaultFG, v.defaultBG)
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
		// Main screen: scroll within margins using MemoryBuffer
		v.memBufScrollColumnsDown(v.marginTop, v.marginBottom, leftCol, rightCol, n, v.defaultFG, v.defaultBG)
	}
	v.MarkAllDirty()
}

// Scroll adjusts the viewport offset for the history buffer.
func (v *VTerm) Scroll(delta int) {
	if v.inAltScreen {
		return
	}
	v.memoryBufferScroll(delta)
	v.MarkAllDirty()
}

// scrollHorizontal scrolls content horizontally within specified margins.
// n > 0: scroll right (content shifts right, blank column inserted at left)
// n < 0: scroll left (content shifts left, blank column inserted at right)
func (v *VTerm) scrollHorizontal(n int, left int, right int, top int, bottom int) {
	if v.inAltScreen {
		v.altBufferScrollColumnsHorizontal(top, bottom, left, right, n, v.currentFG, v.currentBG)
	} else {
		// Main screen: scroll horizontally using MemoryBuffer
		v.memBufScrollColumnsHorizontal(top, bottom, left, right, n, v.currentFG, v.currentBG)
	}
	v.MarkAllDirty()
}

// memBufScrollColumnsUp scrolls content up within column margins using MemoryBuffer.
func (v *VTerm) memBufScrollColumnsUp(top, bottom, left, right, n int, fg, bg Color) {
	if v.memBufState == nil || v.memBufState.memBuf == nil {
		return
	}
	baseGlobal := v.memBufState.liveEdgeBase

	// Shift content up within the specified column range
	for y := top; y <= bottom-n; y++ {
		srcY := y + n
		if srcY <= bottom {
			srcLine := v.memBufState.memBuf.GetLine(baseGlobal + int64(srcY))
			dstLine := v.memBufState.memBuf.EnsureLine(baseGlobal + int64(y))
			if srcLine != nil && dstLine != nil {
				for x := left; x <= right && x < len(srcLine.Cells); x++ {
					if x < len(dstLine.Cells) {
						v.memBufState.memBuf.SetCell(baseGlobal+int64(y), x, srcLine.Cells[x])
					}
				}
			}
		}
	}

	// Clear the bottom n lines' margin regions
	clearStart := bottom - n + 1
	if clearStart < top {
		clearStart = top
	}
	blankCell := Cell{Rune: ' ', FG: fg, BG: bg}
	for y := clearStart; y <= bottom; y++ {
		for x := left; x <= right; x++ {
			v.memBufState.memBuf.SetCell(baseGlobal+int64(y), x, blankCell)
		}
	}
}

// memBufScrollColumnsDown scrolls content down within column margins using MemoryBuffer.
func (v *VTerm) memBufScrollColumnsDown(top, bottom, left, right, n int, fg, bg Color) {
	if v.memBufState == nil || v.memBufState.memBuf == nil {
		return
	}
	baseGlobal := v.memBufState.liveEdgeBase

	// Shift content down within the specified column range
	for y := bottom; y >= top+n; y-- {
		srcY := y - n
		if srcY >= top {
			srcLine := v.memBufState.memBuf.GetLine(baseGlobal + int64(srcY))
			dstLine := v.memBufState.memBuf.EnsureLine(baseGlobal + int64(y))
			if srcLine != nil && dstLine != nil {
				for x := left; x <= right && x < len(srcLine.Cells); x++ {
					if x < len(dstLine.Cells) {
						v.memBufState.memBuf.SetCell(baseGlobal+int64(y), x, srcLine.Cells[x])
					}
				}
			}
		}
	}

	// Clear the top n lines' margin regions
	clearEnd := top + n - 1
	if clearEnd > bottom {
		clearEnd = bottom
	}
	blankCell := Cell{Rune: ' ', FG: fg, BG: bg}
	for y := top; y <= clearEnd; y++ {
		for x := left; x <= right; x++ {
			v.memBufState.memBuf.SetCell(baseGlobal+int64(y), x, blankCell)
		}
	}
}

// memBufScrollColumnsHorizontal scrolls content horizontally within margins using MemoryBuffer.
func (v *VTerm) memBufScrollColumnsHorizontal(top, bottom, left, right, n int, fg, bg Color) {
	if v.memBufState == nil || v.memBufState.memBuf == nil {
		return
	}
	baseGlobal := v.memBufState.liveEdgeBase
	blankCell := Cell{Rune: ' ', FG: fg, BG: bg}

	for y := top; y <= bottom; y++ {
		line := v.memBufState.memBuf.EnsureLine(baseGlobal + int64(y))
		if line == nil {
			continue
		}

		if n > 0 {
			// Scroll right: shift content right, insert blanks at left
			for x := right; x >= left+n; x-- {
				srcX := x - n
				if srcX >= left && srcX < len(line.Cells) && x < len(line.Cells) {
					v.memBufState.memBuf.SetCell(baseGlobal+int64(y), x, line.Cells[srcX])
				}
			}
			// Clear left side
			for x := left; x < left+n && x <= right; x++ {
				v.memBufState.memBuf.SetCell(baseGlobal+int64(y), x, blankCell)
			}
		} else if n < 0 {
			// Scroll left: shift content left, insert blanks at right
			absN := -n
			for x := left; x <= right-absN; x++ {
				srcX := x + absN
				if srcX <= right && srcX < len(line.Cells) {
					v.memBufState.memBuf.SetCell(baseGlobal+int64(y), x, line.Cells[srcX])
				}
			}
			// Clear right side
			for x := right - absN + 1; x <= right; x++ {
				if x >= left {
					v.memBufState.memBuf.SetCell(baseGlobal+int64(y), x, blankCell)
				}
			}
		}
	}
}

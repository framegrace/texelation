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
			// Clear from cursor to end of current line, then all lines below
			v.altBufferClearRegion(v.cursorX, v.cursorY, v.width-1, v.cursorY, v.currentFG, v.currentBG)
			if v.cursorY+1 < v.height {
				v.altBufferClearRegion(0, v.cursorY+1, v.width-1, v.height-1, v.currentFG, v.currentBG)
			}
		} else {
			v.memoryBufferEraseScreen(0)
		}
	case 1: // Erase from beginning of screen to cursor
		if v.inAltScreen {
			// Clear all lines above cursor, then from start to cursor on current line
			if v.cursorY > 0 {
				v.altBufferClearRegion(0, 0, v.width-1, v.cursorY-1, v.currentFG, v.currentBG)
			}
			v.altBufferClearRegion(0, v.cursorY, v.cursorX, v.cursorY, v.currentFG, v.currentBG)
		} else {
			v.memoryBufferEraseScreen(1)
		}
	case 2: // Erase entire visible screen (ED 2)
		if v.inAltScreen {
			v.altBufferClearRegion(0, 0, v.width-1, v.height-1, v.currentFG, v.currentBG)
		} else {
			v.memoryBufferEraseScreen(2)
		}
	case 3: // Erase scrollback only, leave visible screen intact (ED 3)
		if !v.inAltScreen && v.memBufState != nil && v.memBufState.memBuf != nil {
			mb := v.memBufState.memBuf
			// Log before clearing scrollback
			v.logMemBufDebug("[ED3] ClearScreen mode=3 CLEARING SCROLLBACK: before: GlobalOffset=%d GlobalEnd=%d liveEdgeBase=%d TotalLines=%d",
				mb.GlobalOffset(), mb.GlobalEnd(), v.memBufState.liveEdgeBase, mb.TotalLines())
			// Clear scrollback by evicting all lines except the visible viewport
			mb.Evict(int(mb.TotalLines()) - v.height)
			// Ensure liveEdgeBase is consistent after eviction
			v.ensureLiveEdgeBaseConsistency()
			v.logMemBufDebug("[ED3] ClearScreen mode=3 CLEARING SCROLLBACK: after: GlobalOffset=%d GlobalEnd=%d liveEdgeBase=%d TotalLines=%d",
				mb.GlobalOffset(), mb.GlobalEnd(), v.memBufState.liveEdgeBase, mb.TotalLines())
			v.MarkAllDirty()
		}
		// On alt screen, ED 3 does nothing (no scrollback to clear)
	}
}

// ClearLine handles EL (Erase in Line) with different modes.
func (v *VTerm) ClearLine(mode int) {
	v.MarkDirty(v.cursorY)

	// Alt screen: use consolidated alt buffer operations
	if v.inAltScreen {
		switch mode {
		case 0: // Erase from cursor to end
			v.altBufferClearRegion(v.cursorX, v.cursorY, v.width-1, v.cursorY, v.currentFG, v.currentBG)
		case 1: // Erase from beginning to cursor
			v.altBufferClearRegion(0, v.cursorY, v.cursorX, v.cursorY, v.currentFG, v.currentBG)
		case 2: // Erase entire line
			v.altBufferClearRow(v.cursorY, v.currentFG, v.currentBG)
		}
		return
	}

	// Use MemoryBuffer
	switch mode {
	case 0:
		v.memoryBufferEraseToEndOfLine()
	case 1:
		v.memoryBufferEraseFromStartOfLine()
	case 2:
		v.memoryBufferEraseLine()
	}
}

// EraseCharacters handles ECH (Erase Character) - replaces n characters with blanks.
func (v *VTerm) EraseCharacters(n int) {
	v.MarkDirty(v.cursorY)

	if v.inAltScreen {
		// Alt screen: use consolidated alt buffer operation
		endX := v.cursorX + n - 1
		if endX >= v.width {
			endX = v.width - 1
		}
		v.altBufferClearRegion(v.cursorX, v.cursorY, endX, v.cursorY, v.currentFG, v.currentBG)
	} else {
		// Use MemoryBuffer
		v.memoryBufferEraseCharacters(n)
	}
}

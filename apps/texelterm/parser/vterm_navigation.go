// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_navigation.go
// Summary: Cursor movement and navigation - tabs, carriage return, index operations.
// Usage: Part of VTerm terminal emulator.

package parser

// CarriageReturn moves cursor to the start of the current line (column 0 or left margin).
func (v *VTerm) CarriageReturn() {
	v.logDebug("[CR] CarriageReturn called: cursorY=%d, cursorX=%d, inAltScreen=%v",
		v.cursorY, v.cursorX, v.inAltScreen)
	// If wrapNext is set, clear the Wrapped flag on the current line
	// because an explicit carriage return means this is a hard line break, not a wrap
	if v.wrapNext && !v.inAltScreen {
		logicalY := v.cursorY + v.getTopHistoryLine()
		line := v.getHistoryLine(logicalY)
		if line != nil && len(line) > v.cursorX && line[v.cursorX].Wrapped {
			line[v.cursorX].Wrapped = false
			v.setHistoryLine(logicalY, line)
		}
	}

	v.wrapNext = false // Clear wrapNext when returning to start of line

	// CR behavior with left/right margins:
	// - If inside margins: go to left margin
	// - If left of left margin: go to column 0 (unless in origin mode, then go to left margin)
	// - If right of right margin: no margins, so go to column 0
	// - If at left margin: stay there
	if v.leftRightMarginMode {
		if v.originMode {
			// In origin mode: always go to left margin
			v.SetCursorPos(v.cursorY, v.marginLeft)
		} else if v.cursorX >= v.marginLeft && v.cursorX <= v.marginRight {
			// Inside margins: go to left margin
			v.SetCursorPos(v.cursorY, v.marginLeft)
		} else {
			// Outside margins (left or right): go to column 0
			v.SetCursorPos(v.cursorY, 0)
		}
	} else {
		v.SetCursorPos(v.cursorY, 0)
	}

	// Update display buffer logical cursor AFTER SetCursorPos so delta calculation works
	if !v.inAltScreen && v.IsDisplayBufferEnabled() {
		v.displayBufferCarriageReturn()
	}
}

// Backspace moves cursor back one column (respecting left margin).
func (v *VTerm) Backspace() {
	v.wrapNext = false

	// Determine minimum column based on left margin
	minX := 0
	if v.leftRightMarginMode && v.cursorX >= v.marginLeft && v.cursorX <= v.marginRight {
		// Inside margins: stop at left margin
		minX = v.marginLeft
	}

	if v.cursorX > minX {
		v.SetCursorPos(v.cursorY, v.cursorX-1)
		// Sync display buffer cursor after SetCursorPos
		if !v.inAltScreen && v.IsDisplayBufferEnabled() {
			v.displayBufferSetCursorFromPhysical(true) // Backspace is a relative move
		}
	}
}

// Tab moves cursor to the next tab stop.
func (v *VTerm) Tab() {
	v.wrapNext = false
	for x := v.cursorX + 1; x < v.width; x++ {
		if v.tabStops[x] {
			v.SetCursorPos(v.cursorY, x)
			return
		}
	}
	v.SetCursorPos(v.cursorY, v.width-1)
}

// TabForward (CHT) moves cursor forward n tab stops.
// In DEC terminals (xterm), tabs stop at the right margin when DECLRMM is active.
func (v *VTerm) TabForward(n int) {
	v.wrapNext = false

	// Determine right boundary
	rightEdge := v.width - 1
	if v.leftRightMarginMode {
		rightEdge = v.marginRight
	}

	for i := 0; i < n; i++ {
		found := false
		for x := v.cursorX + 1; x <= rightEdge; x++ {
			if v.tabStops[x] {
				v.SetCursorPos(v.cursorY, x)
				found = true
				break
			}
		}
		if !found {
			// No more tab stops, move to right edge
			v.SetCursorPos(v.cursorY, rightEdge)
			break
		}
	}
}

// TabBackward (CBT) moves cursor backward n tab stops.
// CBT ignores left/right margins and can move all the way to column 1.
func (v *VTerm) TabBackward(n int) {
	v.wrapNext = false

	for i := 0; i < n; i++ {
		found := false
		// Search backward from current position
		for x := v.cursorX - 1; x >= 0; x-- {
			if v.tabStops[x] {
				v.SetCursorPos(v.cursorY, x)
				found = true
				break
			}
		}
		if !found {
			// No more tab stops, move to left edge
			v.SetCursorPos(v.cursorY, 0)
			break
		}
	}
}

// SetTabStop sets a tab stop at the current cursor column.
func (v *VTerm) SetTabStop() {
	v.tabStops[v.cursorX] = true
}

// ClearTabStop clears tab stops.
// mode 0 or default: clear tab at cursor
// mode 3: clear all tabs
func (v *VTerm) ClearTabStop(mode int) {
	switch mode {
	case 0:
		// Clear tab at cursor
		delete(v.tabStops, v.cursorX)
	case 3:
		// Clear all tabs
		v.tabStops = make(map[int]bool)
	}
}

// Index (IND) moves cursor down one line, scrolling if at bottom margin.
func (v *VTerm) Index() {
	// If wrapNext is set, clear the Wrapped flag on the current line
	// because an explicit line feed means this is a hard line break, not a wrap
	if v.wrapNext && !v.inAltScreen {
		logicalY := v.cursorY + v.getTopHistoryLine()
		line := v.getHistoryLine(logicalY)
		if line != nil && len(line) > v.cursorX && line[v.cursorX].Wrapped {
			line[v.cursorX].Wrapped = false
			v.setHistoryLine(logicalY, line)
		}
	}

	v.wrapNext = false
	// Check if cursor is outside left/right margins - if so, don't scroll
	outsideMargins := v.leftRightMarginMode && (v.cursorX < v.marginLeft || v.cursorX > v.marginRight)

	if v.cursorY == v.marginBottom {
		if !outsideMargins {
			v.scrollRegion(1, v.marginTop, v.marginBottom)
		}
		// If outside margins, stay at marginBottom (don't move past it)
	} else if v.cursorY < v.height-1 {
		v.SetCursorPos(v.cursorY+1, v.cursorX)
	}
}

// NextLine (NEL) moves cursor down one line and to the appropriate left position.
func (v *VTerm) NextLine() {
	// Save current position to detect if Index actually moved
	oldY := v.cursorY
	oldX := v.cursorX

	// First move down like Index
	v.Index()

	// Then determine horizontal position:
	// NEL always goes to left margin or column 0, EXCEPT:
	// - When cursor was LEFT of left margin and didn't move vertically (stay at column 0/1)
	if v.leftRightMarginMode {
		// With left/right margins active
		if v.cursorY == oldY && oldX < v.marginLeft {
			// Didn't move down and was left of margin: stay at current X
			// (This happens when at bottom and outside margins)
		} else {
			// Go to left margin in all other cases
			v.SetCursorPos(v.cursorY, v.marginLeft)
		}
	} else {
		// No left/right margins: always go to column 0
		v.SetCursorPos(v.cursorY, 0)
	}
}

// ReverseIndex (RI) moves cursor up one line, scrolling if at top margin.
func (v *VTerm) ReverseIndex() {
	v.wrapNext = false
	// Check if cursor is outside left/right margins - if so, don't scroll
	outsideMargins := v.leftRightMarginMode && (v.cursorX < v.marginLeft || v.cursorX > v.marginRight)

	if v.cursorY == v.marginTop {
		if !outsideMargins {
			v.scrollRegion(-1, v.marginTop, v.marginBottom)
		}
		// If outside margins, stay at marginTop (don't move past it)
	} else if v.cursorY > 0 {
		v.SetCursorPos(v.cursorY-1, v.cursorX)
	}
}

// BackIndex (DECBI) moves cursor back one column or scrolls content right.
// ESC 6 - VT420 feature for horizontal scrolling.
func (v *VTerm) BackIndex() {
	v.wrapNext = false

	// Determine effective left/right margins
	leftMargin := 0
	rightMargin := v.width - 1
	if v.leftRightMarginMode {
		leftMargin = v.marginLeft
		rightMargin = v.marginRight
	}

	// Check if cursor is outside top/bottom margins
	outsideVerticalMargins := v.cursorY < v.marginTop || v.cursorY > v.marginBottom

	// If at left margin and inside vertical margins, scroll content right
	if v.cursorX == leftMargin && !outsideVerticalMargins {
		v.scrollHorizontal(1, leftMargin, rightMargin, v.marginTop, v.marginBottom)
		// Cursor stays at left margin
	} else if v.cursorX > 0 {
		// Not at left margin or outside margins - just move cursor left
		v.SetCursorPos(v.cursorY, v.cursorX-1)
	}
	// If at column 0, stay there (can't move further left)
}

// ForwardIndex (DECFI) moves cursor forward one column or scrolls content left.
// ESC 9 - VT420 feature for horizontal scrolling.
func (v *VTerm) ForwardIndex() {
	v.wrapNext = false

	// Determine effective left/right margins
	leftMargin := 0
	rightMargin := v.width - 1
	if v.leftRightMarginMode {
		leftMargin = v.marginLeft
		rightMargin = v.marginRight
	}

	// Check if cursor is outside top/bottom margins
	outsideVerticalMargins := v.cursorY < v.marginTop || v.cursorY > v.marginBottom

	// If at right margin and inside vertical margins, scroll content left
	if v.cursorX == rightMargin && !outsideVerticalMargins {
		v.scrollHorizontal(-1, leftMargin, rightMargin, v.marginTop, v.marginBottom)
		// Cursor stays at right margin
	} else if v.cursorX < v.width-1 {
		// Not at right margin or outside margins - just move cursor right
		v.SetCursorPos(v.cursorY, v.cursorX+1)
	}
	// If at right edge of screen (width-1), stay there (can't move further right)
}

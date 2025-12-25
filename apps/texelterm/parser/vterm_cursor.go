// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_cursor.go
// Summary: Basic cursor operations - position, visibility, save/restore.
// Usage: Part of VTerm terminal emulator.

package parser

// SetCursorPos moves the cursor to the specified position, clamping to valid bounds.
func (v *VTerm) SetCursorPos(y, x int) {
	// Clamp coordinates first
	if x < 0 {
		x = 0
	}
	if x >= v.width {
		x = v.width - 1
	}
	if y < 0 {
		y = 0
	}
	if y >= v.height {
		y = v.height - 1
	}

	// Only clear wrapNext if we're actually moving to a different position
	if y != v.cursorY || x != v.cursorX {
		v.wrapNext = false
	}

	v.prevCursorX = v.cursorX
	v.prevCursorY = v.cursorY
	v.cursorX = x
	v.cursorY = y

	// NOTE: We do NOT call displayBufferSetCursorFromPhysical() here.
	// Character placement (placeChar) already advances the display buffer cursor,
	// so calling it here would cause double-advancement. Instead, cursor movement
	// escape sequences (CUB, CUF, CUP, etc.) call displayBufferSetCursorFromPhysical
	// explicitly via their handlers.

	v.MarkDirty(v.prevCursorY)
	v.MarkDirty(v.cursorY)
}
// GetCursorX returns the current cursor X position
func (v *VTerm) GetCursorX() int {
	return v.cursorX
}

// GetCursorY returns the current cursor Y position
func (v *VTerm) GetCursorY() int {
	return v.cursorY
}

// SaveCursor saves the current cursor position for either the main or alt screen.
func (v *VTerm) SaveCursor() {
	if v.inAltScreen {
		v.savedAltCursorX, v.savedAltCursorY = v.cursorX, v.cursorY
	} else {
		v.savedMainCursorX, v.savedMainCursorY = v.cursorX, v.cursorY
	}
}

// RestoreCursor restores the cursor position for either the main or alt screen.
// According to xterm behavior, DECRC also resets origin mode.
func (v *VTerm) RestoreCursor() {
	v.wrapNext = false
	// Reset origin mode (xterm behavior)
	v.originMode = false
	if v.inAltScreen {
		v.SetCursorPos(v.savedAltCursorY, v.savedAltCursorX)
	} else {
		v.SetCursorPos(v.savedMainCursorY, v.savedMainCursorX)
	}
}

// SetCursorVisible sets the cursor visibility state.
func (v *VTerm) SetCursorVisible(visible bool) {
	if v.cursorVisible != visible {
		v.cursorVisible = visible
		v.MarkDirty(v.cursorY)
	}
}

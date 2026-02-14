// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_modes.go
// Summary: ANSI and DEC private mode handling.
// Usage: Part of VTerm terminal emulator.

package parser

// processANSIMode handles standard ANSI mode setting/resetting (SM/RM).
func (v *VTerm) processANSIMode(command rune, params []int) {
	if len(params) == 0 {
		return
	}
	mode := params[0]
	switch command {
	case 'h': // SM - Set Mode
		switch mode {
		case 4: // IRM - Insert/Replace Mode
			v.insertMode = true
		}
	case 'l': // RM - Reset Mode
		switch mode {
		case 4: // IRM - Insert/Replace Mode
			v.insertMode = false
		}
	}
}

// processPrivateCSI handles terminal-specific CSI sequences (starting with '?').
func (v *VTerm) processPrivateCSI(command rune, params []int) {
	if len(params) == 0 {
		return
	}
	mode := params[0]
	switch command {
	case 'h': // SET
		switch mode {
		case 1:
			v.appCursorKeys = true
		case 6: // DECOM - Origin Mode
			v.originMode = true
			// Move cursor to home position of scroll region
			v.SetCursorPos(v.marginTop, v.marginLeft)
		case 7:
			v.autoWrapMode = true
		case 12: // SET Blinking Cursor - ignored
		case 25:
			v.SetCursorVisible(true)
		case 69: // DECLRMM - Enable left/right margin mode
			v.leftRightMarginMode = true
		case 1002, 1004, 1006:
			// Ignore mouse and focus reporting for now
		case 2004: // Enable bracketed paste mode
			if !v.bracketedPasteMode {
				v.bracketedPasteMode = true
				if v.OnBracketedPasteModeChange != nil {
					v.OnBracketedPasteModeChange(true)
				}
			}
		case 1049: // Switch to Alt Workspace
			if v.inAltScreen {
				v.logDebug("[ALT] Already in alt screen, ignoring DECSET 1049")
				return
			}
			v.logDebug("[ALT] Entering alt screen (DECSET 1049), saving cursor (%d,%d)", v.cursorX, v.cursorY)
			v.inAltScreen = true
			if v.OnAltScreenChange != nil {
				v.OnAltScreenChange(true)
			}
			v.savedMainCursorX, v.savedMainCursorY = v.cursorX, v.cursorY //+v.getTopHistoryLine()
			v.altBuffer = make([][]Cell, v.height)
			for i := range v.altBuffer {
				v.altBuffer[i] = make([]Cell, v.width)
				// Initialize all cells with proper default colors
				for j := range v.altBuffer[i] {
					v.altBuffer[i][j] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
				}
			}
			v.ClearScreen()
		case 2026: // START Synchronized Update
			v.InSynchronizedUpdate = true
		}
	case 'l': // RESET
		switch mode {
		case 1:
			v.appCursorKeys = false
		case 6: // DECOM - Reset Origin Mode
			v.originMode = false
			// Move cursor to absolute home position
			v.SetCursorPos(0, 0)
		case 7:
			v.autoWrapMode = false
		case 12: // RESET Steady Cursor - ignored
		case 25:
			v.SetCursorVisible(false)
		case 69: // DECLRMM - Disable left/right margin mode
			v.leftRightMarginMode = false
			// Reset margins to full width
			v.marginLeft = 0
			v.marginRight = v.width - 1
		case 1002, 1004, 1006, 2031, 2048:
			// Ignore mouse and focus reporting for now
		case 2004: // Disable bracketed paste mode
			if v.bracketedPasteMode {
				v.bracketedPasteMode = false
				if v.OnBracketedPasteModeChange != nil {
					v.OnBracketedPasteModeChange(false)
				}
			}
		case 1049: // Switch back to Main Workspace
			if !v.inAltScreen {
				v.logDebug("[ALT] Not in alt screen, ignoring DECRST 1049")
				return
			}
			v.logDebug("[ALT] Exiting alt screen (DECRST 1049), restoring cursor (%d,%d)", v.savedMainCursorX, v.savedMainCursorY)
			v.inAltScreen = false
			if v.OnAltScreenChange != nil {
				v.OnAltScreenChange(false)
			}
			v.altBuffer = nil
			physicalY := v.savedMainCursorY // - v.getTopHistoryLine()
			v.SetCursorPos(physicalY, v.savedMainCursorX)
			v.MarkAllDirty()
			if v.ScreenRestored != nil {
				v.ScreenRestored()
			}
		case 2026: // END Synchronized Update
			v.InSynchronizedUpdate = false
		}
	}
}

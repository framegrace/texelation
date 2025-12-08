// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_clear.go
// Summary: Screen and buffer clearing operations.
// Usage: Part of VTerm terminal emulator.

package parser

import (
	"fmt"
	"os"
)

// ClearScreen clears the entire screen and resets history (for main screen).
func (v *VTerm) ClearScreen() {
	v.MarkAllDirty()
	if v.inAltScreen {
		// Use default colors, not currentFG/BG which might be from previous content
		for y := range v.altBuffer {
			for x := range v.altBuffer[y] {
				v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
			}
		}
		v.SetCursorPos(0, 0)
	} else {
		if v.historyManager != nil {
			// DEBUG: Log history state before and after
			lenBefore := v.historyManager.Length()
			// Using HistoryManager - just append first line
			v.historyManager.AppendLine(make([]Cell, 0, v.width))
			lenAfter := v.historyManager.Length()
			fmt.Fprintf(os.Stderr, "[CLEARSCREEN DEBUG] histLen before=%d, after=%d\n", lenBefore, lenAfter)
		} else {
			// Legacy circular buffer
			v.historyBuffer = make([][]Cell, v.maxHistorySize)
			v.historyHead = 0
			v.historyLen = 1
			v.historyBuffer[0] = make([]Cell, 0, v.width)
		}
		v.viewOffset = 0
		v.SetCursorPos(0, 0)
	}
}

// ClearVisibleScreen clears just the visible display (ED 2).
// Preserves scrollback history and cursor position.
func (v *VTerm) ClearVisibleScreen() {
	v.MarkAllDirty()
	if v.inAltScreen {
		// Use default colors for cleared cells
		for y := range v.altBuffer {
			for x := range v.altBuffer[y] {
				v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
			}
		}
		// Cursor position unchanged
	} else {
		// For main screen, clear all visible lines
		logicalTop := v.getTopHistoryLine()
		blankLine := make([]Cell, v.width)
		for x := 0; x < v.width; x++ {
			blankLine[x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}
		for y := 0; y < v.height; y++ {
			logicalY := logicalTop + y
			if logicalY < v.getHistoryLen() {
				v.setHistoryLine(logicalY, append([]Cell(nil), blankLine...))
			}
		}
		// Cursor position unchanged
	}
}

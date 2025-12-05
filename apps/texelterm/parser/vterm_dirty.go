// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_dirty.go
// Summary: Dirty line tracking for optimized rendering.
// Usage: Part of VTerm terminal emulator.

package parser

// MarkDirty marks a specific line as dirty (needs rerendering).
func (v *VTerm) MarkDirty(line int) {
	if line >= 0 && line < v.height {
		v.dirtyLines[line] = true
	}
}

// MarkAllDirty marks all lines as dirty.
func (v *VTerm) MarkAllDirty() { v.allDirty = true }

// GetDirtyLines returns the dirty line map and the all-dirty flag.
func (v *VTerm) GetDirtyLines() (map[int]bool, bool) {
	return v.dirtyLines, v.allDirty
}

// ClearDirty resets the dirty tracking, but always marks cursor lines.
func (v *VTerm) ClearDirty() {
	v.allDirty = false
	v.dirtyLines = make(map[int]bool)
	// Always mark cursor lines to handle blinking and movement
	v.MarkDirty(v.prevCursorY)
	v.MarkDirty(v.cursorY)
}

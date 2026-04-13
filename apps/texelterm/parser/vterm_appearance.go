// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_appearance.go
// Summary: Visual appearance handling - dirty line tracking, screen clearing, and SGR attributes.
// Usage: Part of VTerm terminal emulator.

package parser

// --- Dirty Line Tracking ---

// MarkDirty marks a specific line as dirty (needs rerendering).
func (v *VTerm) MarkDirty(line int) {
	if line >= 0 && line < v.height {
		v.dirtyLines[line] = true
	}
}

// MarkAllDirty marks all lines as dirty.
func (v *VTerm) MarkAllDirty() { v.allDirty = true }

// DirtyLines returns the dirty line map and the all-dirty flag.
func (v *VTerm) DirtyLines() (map[int]bool, bool) {
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

// --- Screen Clearing ---

// ClearScreen clears the entire screen and resets history (for main screen).
func (v *VTerm) ClearScreen() {
	v.MarkAllDirty()
	if v.inAltScreen {
		// Use default colors, not currentFG/BG which might be from previous content
		v.altBufferClearRegion(0, 0, v.width-1, v.height-1, v.defaultFG, v.defaultBG)
		v.SetCursorPos(0, 0)
	} else {
		// Use memory buffer erase
		v.mainScreenEraseScreen(2)
		v.SetCursorPos(0, 0)
	}
}

// ClearVisibleScreen clears just the visible display (ED 2).
// Preserves scrollback history and cursor position.
func (v *VTerm) ClearVisibleScreen() {
	v.MarkAllDirty()
	if v.inAltScreen {
		// Use default colors for cleared cells
		v.altBufferClearRegion(0, 0, v.width-1, v.height-1, v.defaultFG, v.defaultBG)
		// Cursor position unchanged
	} else {
		// For main screen, use memory buffer to clear visible area
		v.mainScreenEraseScreen(2)
		// Cursor position unchanged
	}
}

// --- SGR Attributes ---

// handleSGR processes SGR (Select Graphic Rendition) escape sequences.
// Handles text attributes (bold, underline, reverse) and colors (standard, 256-color, RGB).
// params is [][]int where each element is a colon-separated group of subparameters.
// For example, "\e[1;4:3;38:2:255:0:0m" produces:
//
//	[[1], [4,3], [38,2,255,0,0]]
//
// This correctly distinguishes semicolons (top-level separators) from colons (subparameters).
func (v *VTerm) handleSGR(params [][]int) {
	if len(params) == 0 {
		params = [][]int{{0}}
	}
	i := 0
	for i < len(params) {
		group := params[i]
		if len(group) == 0 {
			i++
			continue
		}
		p := group[0]
		switch {
		case p == 0:
			v.ResetAttributes()
		case p == 1:
			v.SetAttribute(AttrBold)
		case p == 2:
			v.SetAttribute(AttrDim)
		case p == 3:
			v.SetAttribute(AttrItalic)
		case p == 4:
			// SGR 4 = underline. Subparam selects style: 4:0=none, 4:1=single,
			// 4:2=double, 4:3=curly, 4:4=dotted, 4:5=dashed.
			// Without subparam (plain SGR 4), default is single underline.
			if len(group) > 1 && group[1] == 0 {
				v.ClearAttribute(AttrUnderline) // 4:0 = no underline
			} else {
				v.SetAttribute(AttrUnderline) // 4 or 4:1-5 = underline on
			}
		case p == 7:
			v.SetAttribute(AttrReverse)
		case p == 22:
			v.ClearAttribute(AttrBold | AttrDim)
		case p == 23:
			v.ClearAttribute(AttrItalic)
		case p == 24:
			v.ClearAttribute(AttrUnderline)
		case p == 27:
			v.ClearAttribute(AttrReverse)
		case p >= 30 && p <= 37:
			v.currentFG = Color{Mode: ColorModeStandard, Value: uint8(p - 30)}
		case p == 39:
			v.currentFG = v.defaultFG
		case p >= 40 && p <= 47:
			v.currentBG = Color{Mode: ColorModeStandard, Value: uint8(p - 40)}
		case p == 49:
			v.currentBG = v.defaultBG
		case p == 38: // Set extended foreground color
			if v.parseExtendedColor(group, params, i, &v.currentFG, &i) {
				// i already advanced by parseExtendedColor
			}
		case p == 48: // Set extended background color
			if v.parseExtendedColor(group, params, i, &v.currentBG, &i) {
				// i already advanced by parseExtendedColor
			}
		case p >= 90 && p <= 97: // Bright foreground
			v.currentFG = Color{Mode: ColorModeStandard, Value: uint8(p - 90 + 8)}
		case p >= 100 && p <= 107: // Bright background
			v.currentBG = Color{Mode: ColorModeStandard, Value: uint8(p - 100 + 8)}
		}
		i++
	}
}

// parseExtendedColor handles extended color sequences for both colon and semicolon forms:
//   - Colon form (ITU T.416): 38:5:idx or 38:2:r:g:b (all in one group)
//   - Semicolon form (legacy): 38;5;idx or 38;2;r;g;b (spread across groups)
//
// Returns true if a color was parsed. Advances *idx past consumed groups for semicolon form.
func (v *VTerm) parseExtendedColor(group []int, params [][]int, idx int, target *Color, outIdx *int) bool {
	// Colon form: everything is in a single group, e.g. [38,5,196] or [38,2,255,0,0]
	if len(group) >= 3 && group[1] == 5 {
		val := uint8(group[2])
		if val < 8 {
			*target = Color{Mode: ColorModeStandard, Value: val}
		} else {
			*target = Color{Mode: ColorMode256, Value: val}
		}
		return true
	}
	if len(group) >= 5 && group[1] == 2 {
		*target = Color{Mode: ColorModeRGB, R: uint8(group[2]), G: uint8(group[3]), B: uint8(group[4])}
		return true
	}

	// Semicolon form: values are in subsequent groups, e.g. [38],[5],[196]
	if idx+2 < len(params) && len(params[idx+1]) > 0 && params[idx+1][0] == 5 && len(params[idx+2]) > 0 {
		val := uint8(params[idx+2][0])
		if val < 8 {
			*target = Color{Mode: ColorModeStandard, Value: val}
		} else {
			*target = Color{Mode: ColorMode256, Value: val}
		}
		*outIdx = idx + 2
		return true
	}
	if idx+4 < len(params) && len(params[idx+1]) > 0 && params[idx+1][0] == 2 {
		r, g, b := uint8(0), uint8(0), uint8(0)
		if len(params[idx+2]) > 0 {
			r = uint8(params[idx+2][0])
		}
		if len(params[idx+3]) > 0 {
			g = uint8(params[idx+3][0])
		}
		if len(params[idx+4]) > 0 {
			b = uint8(params[idx+4][0])
		}
		*target = Color{Mode: ColorModeRGB, R: r, G: g, B: b}
		*outIdx = idx + 4
		return true
	}
	return false
}

// SetAttribute sets a text attribute (bold, underline, reverse).
func (v *VTerm) SetAttribute(a Attribute) { v.currentAttr |= a }

// ClearAttribute clears a text attribute.
func (v *VTerm) ClearAttribute(a Attribute) { v.currentAttr &^= a }

// ResetAttributes resets all text attributes and colors to defaults.
func (v *VTerm) ResetAttributes() {
	v.currentFG = v.defaultFG
	v.currentBG = v.defaultBG
	v.currentAttr = 0
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_sgr.go
// Summary: SGR (Select Graphic Rendition) - text attributes and colors.
// Usage: Part of VTerm terminal emulator.

package parser

// handleSGR processes SGR (Select Graphic Rendition) escape sequences.
// Handles text attributes (bold, underline, reverse) and colors (standard, 256-color, RGB).
func (v *VTerm) handleSGR(params []int) {
	i := 0
	if len(params) == 0 {
		params = []int{0}
	}
	for i < len(params) {
		p := params[i]
		switch {
		case p == 0:
			v.ResetAttributes()
		case p == 1:
			v.SetAttribute(AttrBold)
		case p == 4:
			v.SetAttribute(AttrUnderline)
		case p == 7:
			v.SetAttribute(AttrReverse)
		case p == 22:
			v.ClearAttribute(AttrBold)
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
			if i+2 < len(params) && params[i+1] == 5 { // 256-color palette
				v.currentFG = Color{Mode: ColorMode256, Value: uint8(params[i+2])}
				i += 2
			} else if i+4 < len(params) && params[i+1] == 2 { // RGB true-color
				v.currentFG = Color{Mode: ColorModeRGB, R: uint8(params[i+2]), G: uint8(params[i+3]), B: uint8(params[i+4])}
				i += 4
			}
		case p == 48: // Set extended background color
			if i+2 < len(params) && params[i+1] == 5 { // 256-color palette
				v.currentBG = Color{Mode: ColorMode256, Value: uint8(params[i+2])}
				i += 2
			} else if i+4 < len(params) && params[i+1] == 2 { // RGB true-color
				v.currentBG = Color{Mode: ColorModeRGB, R: uint8(params[i+2]), G: uint8(params[i+3]), B: uint8(params[i+4])}
				i += 4
			}
		case p >= 90 && p <= 97: // Bright foreground
			v.currentFG = Color{Mode: ColorModeStandard, Value: uint8(p - 90 + 8)}
		case p >= 100 && p <= 107: // Bright background
			v.currentBG = Color{Mode: ColorModeStandard, Value: uint8(p - 100 + 8)}
		}
		i++
	}
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

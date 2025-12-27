// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/widgets/colorpicker/mode.go
// Summary: Interface for color picker selection modes.

package colorpicker

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texelui/core"
)

// ModePicker defines the interface for color selection modes.
type ModePicker interface {
	// Draw renders the mode's UI within the given rect.
	Draw(painter *core.Painter, rect core.Rect)

	// HandleKey processes keyboard input. Returns true if handled.
	HandleKey(ev *tcell.EventKey) bool

	// HandleMouse processes mouse input. Returns true if handled.
	HandleMouse(ev *tcell.EventMouse, rect core.Rect) bool

	// GetResult returns the currently selected color.
	GetResult() PickerResult

	// PreferredSize returns the preferred width and height for this mode.
	PreferredSize() (int, int)

	// SetColor sets the current color (for initialization).
	SetColor(color tcell.Color)

	// ResetFocus resets internal focus to the first tab stop.
	// Called when entering the mode from the tab bar.
	ResetFocus()
}

// PickerResult represents a selected color from a mode.
type PickerResult struct {
	Color  tcell.Color
	Source string // e.g., "text.primary", "@lavender", "oklch(0.7,0.15,300)"
	R, G, B int32
}

// MakeResult is a helper to construct a PickerResult.
func MakeResult(color tcell.Color, source string) PickerResult {
	r, g, b := color.RGB()
	return PickerResult{
		Color:  color,
		Source: source,
		R:      r,
		G:      g,
		B:      b,
	}
}

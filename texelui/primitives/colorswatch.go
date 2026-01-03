// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/primitives/colorswatch.go
// Summary: A simple widget that displays a colored rectangle.
// Used as a preview for color pickers and palette selections.

package primitives

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
)

// ColorSwatch displays a colored rectangle with an optional label.
// Useful for showing the currently selected color in a color picker.
type ColorSwatch struct {
	core.BaseWidget

	Color tcell.Color // The color to display
	Label string      // Optional label (e.g., hex code)

	// Styling
	ShowBorder  bool        // Draw border around swatch
	BorderStyle tcell.Style // Border style (uses theme default if zero)

	inv func(core.Rect)
}

// NewColorSwatch creates a new color swatch at the given position.
func NewColorSwatch(x, y, w, h int, color tcell.Color) *ColorSwatch {
	cs := &ColorSwatch{
		Color:      color,
		ShowBorder: true,
	}
	cs.SetPosition(x, y)
	cs.Resize(w, h)
	cs.SetFocusable(true)

	// Configure focus style from theme
	tm := theme.Get()
	fg := tm.GetSemanticColor("border.active")
	bg := tm.GetSemanticColor("bg.surface")
	cs.SetFocusedStyle(tcell.StyleDefault.Foreground(fg).Background(bg), true)

	return cs
}

// SetColor updates the displayed color.
func (cs *ColorSwatch) SetColor(c tcell.Color) {
	if cs.Color == c {
		return
	}
	cs.Color = c
	cs.invalidate()
}

// SetLabel updates the displayed label.
func (cs *ColorSwatch) SetLabel(label string) {
	if cs.Label == label {
		return
	}
	cs.Label = label
	cs.invalidate()
}

// Draw renders the color swatch.
func (cs *ColorSwatch) Draw(p *core.Painter) {
	tm := theme.Get()

	// Determine border style
	borderStyle := cs.BorderStyle
	if borderStyle == tcell.StyleDefault {
		fg := tm.GetSemanticColor("border.default")
		bg := tm.GetSemanticColor("bg.surface")
		borderStyle = tcell.StyleDefault.Foreground(fg).Background(bg)
	}
	if cs.IsFocused() {
		fg := tm.GetSemanticColor("border.active")
		_, bg, _ := borderStyle.Decompose()
		borderStyle = tcell.StyleDefault.Foreground(fg).Background(bg)
	}

	// Calculate inner area
	innerX, innerY := cs.Rect.X, cs.Rect.Y
	innerW, innerH := cs.Rect.W, cs.Rect.H

	if cs.ShowBorder && cs.Rect.W >= 3 && cs.Rect.H >= 3 {
		// Draw border
		charset := [6]rune{'─', '│', '╭', '╮', '╰', '╯'}
		p.DrawBorder(cs.Rect, borderStyle, charset)
		innerX++
		innerY++
		innerW -= 2
		innerH -= 2
	}

	if innerW <= 0 || innerH <= 0 {
		return
	}

	// Fill inner area with the color
	colorStyle := tcell.StyleDefault.Background(cs.Color).Foreground(cs.contrastFG())

	// If we have a label and enough space, show it centered
	if cs.Label != "" && innerW >= len(cs.Label) {
		// Center the label
		labelY := innerY + innerH/2
		labelX := innerX + (innerW-len(cs.Label))/2

		for y := innerY; y < innerY+innerH; y++ {
			for x := innerX; x < innerX+innerW; x++ {
				if y == labelY && x >= labelX && x < labelX+len(cs.Label) {
					idx := x - labelX
					p.SetCell(x, y, rune(cs.Label[idx]), colorStyle)
				} else {
					p.SetCell(x, y, ' ', colorStyle)
				}
			}
		}
	} else {
		// Just fill with color
		for y := innerY; y < innerY+innerH; y++ {
			for x := innerX; x < innerX+innerW; x++ {
				p.SetCell(x, y, ' ', colorStyle)
			}
		}
	}
}

// contrastFG returns a foreground color that contrasts with the swatch color.
func (cs *ColorSwatch) contrastFG() tcell.Color {
	r, g, b := cs.Color.RGB()
	// Use relative luminance to determine contrast
	luminance := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 255.0
	if luminance > 0.5 {
		return tcell.ColorBlack
	}
	return tcell.ColorWhite
}

// HandleKey processes keyboard input.
// ColorSwatch doesn't handle any keys by default.
func (cs *ColorSwatch) HandleKey(ev *tcell.EventKey) bool {
	return false
}

// HandleMouse processes mouse input.
func (cs *ColorSwatch) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !cs.HitTest(x, y) {
		return false
	}
	// Just consume clicks to prevent bubbling
	return ev.Buttons() == tcell.Button1
}

// SetInvalidator sets the invalidation callback.
func (cs *ColorSwatch) SetInvalidator(fn func(core.Rect)) {
	cs.inv = fn
}

// invalidate marks the widget as needing redraw.
func (cs *ColorSwatch) invalidate() {
	if cs.inv != nil {
		cs.inv(cs.Rect)
	}
}

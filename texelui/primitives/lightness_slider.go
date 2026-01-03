// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/primitives/lightness_slider.go
// Summary: A vertical slider for lightness in OKLCH color space.
// Shows a gradient from dark (bottom) to light (top) using current H and C.

package primitives

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/color"
	"texelation/texelui/core"
)

// LightnessSlider is a vertical slider for adjusting lightness in OKLCH.
// The slider shows a gradient from dark (bottom) to light (top) using
// the current hue and chroma values.
type LightnessSlider struct {
	core.BaseWidget

	// OKLCH values
	L float64 // Lightness: 0.0-1.0
	H float64 // Hue: 0-360 (for gradient preview)
	C float64 // Chroma: 0.0-0.4 (for gradient preview)

	// Callbacks
	OnChange func(l float64) // Called when lightness changes

	inv func(core.Rect)
}

// NewLightnessSlider creates a vertical lightness slider at the given position.
// Width should be at least 3 (border, content, border).
func NewLightnessSlider(x, y, w, h int) *LightnessSlider {
	s := &LightnessSlider{
		L: 0.7,  // Default mid-high lightness
		H: 270,  // Default hue (purple)
		C: 0.15, // Default chroma
	}
	s.SetPosition(x, y)
	s.Resize(w, h)
	s.SetFocusable(true)

	// Configure focus style from theme
	tm := theme.Get()
	fg := tm.GetSemanticColor("border.active")
	bg := tm.GetSemanticColor("bg.surface")
	s.SetFocusedStyle(tcell.StyleDefault.Foreground(fg).Background(bg), true)

	return s
}

// SetLightness sets the lightness value.
func (s *LightnessSlider) SetLightness(l float64) {
	if l < 0 {
		l = 0
	}
	if l > 1 {
		l = 1
	}
	if s.L == l {
		return
	}
	s.L = l
	s.notifyChange()
	s.invalidate()
}

// SetHC sets the hue and chroma values for gradient preview.
func (s *LightnessSlider) SetHC(h, c float64) {
	if s.H == h && s.C == c {
		return
	}
	s.H = h
	s.C = c
	s.invalidate()
}

// Draw renders the lightness slider.
func (s *LightnessSlider) Draw(painter *core.Painter) {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	borderStyle := tcell.StyleDefault.Foreground(fg).Background(bg)

	w, h := s.Rect.W, s.Rect.H
	if w < 1 || h < 1 {
		return
	}

	focused := s.IsFocused()

	// Calculate thumb position: top = 1.0 (bright), bottom = 0.0 (dark)
	thumbPos := 0
	if h > 1 {
		thumbPos = int((1.0 - s.L) * float64(h-1))
	}

	for y := 0; y < h; y++ {
		// Calculate lightness for this row
		rowL := 1.0 - float64(y)/float64(h-1)
		if h == 1 {
			rowL = s.L
		}

		// Get color for this lightness level
		rgb := color.OKLCHToRGB(rowL, s.C, s.H)
		sliderColor := tcell.NewRGBColor(rgb.R, rgb.G, rgb.B)

		// Draw left border
		painter.SetCell(s.Rect.X, s.Rect.Y+y, '│', borderStyle)

		// Draw slider content
		ch := '█'
		style := tcell.StyleDefault.Foreground(sliderColor).Background(bg)
		if y == thumbPos {
			if focused {
				ch = '◆' // Active thumb
				style = style.Reverse(true)
			} else {
				ch = '◇' // Inactive thumb
			}
		}

		// Draw content (middle columns)
		for x := 1; x < w-1; x++ {
			painter.SetCell(s.Rect.X+x, s.Rect.Y+y, ch, style)
		}

		// Draw right border
		if w >= 2 {
			painter.SetCell(s.Rect.X+w-1, s.Rect.Y+y, '│', borderStyle)
		}
	}
}

// HandleKey processes keyboard input for lightness adjustment.
func (s *LightnessSlider) HandleKey(ev *tcell.EventKey) bool {
	// Check for Shift modifier for fine control
	fine := ev.Modifiers()&tcell.ModShift != 0
	step := 0.05 // 5% increments
	if fine {
		step = 0.01 // 1% fine control
	}

	switch ev.Key() {
	case tcell.KeyUp:
		s.L += step
		if s.L > 1.0 {
			s.L = 1.0
		}
		s.notifyChange()
		s.invalidate()
		return true

	case tcell.KeyDown:
		s.L -= step
		if s.L < 0.0 {
			s.L = 0.0
		}
		s.notifyChange()
		s.invalidate()
		return true

	case tcell.KeyHome:
		s.L = 1.0
		s.notifyChange()
		s.invalidate()
		return true

	case tcell.KeyEnd:
		s.L = 0.0
		s.notifyChange()
		s.invalidate()
		return true
	}

	return false
}

// HandleMouse processes mouse input for direct selection.
func (s *LightnessSlider) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !s.HitTest(x, y) {
		return false
	}

	if ev.Buttons() == tcell.Button1 {
		relY := y - s.Rect.Y
		if s.Rect.H > 1 {
			s.L = 1.0 - float64(relY)/float64(s.Rect.H-1)
			if s.L < 0 {
				s.L = 0
			}
			if s.L > 1 {
				s.L = 1
			}
			s.notifyChange()
			s.invalidate()
		}
		return true
	}

	return false
}

// notifyChange calls the OnChange callback if set.
func (s *LightnessSlider) notifyChange() {
	if s.OnChange != nil {
		s.OnChange(s.L)
	}
}

// SetInvalidator sets the invalidation callback.
func (s *LightnessSlider) SetInvalidator(fn func(core.Rect)) {
	s.inv = fn
}

// invalidate marks the widget as needing redraw.
func (s *LightnessSlider) invalidate() {
	if s.inv != nil {
		s.inv(s.Rect)
	}
}

// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/primitives/hcplane.go
// Summary: A 2D hue × chroma selector for OKLCH color space.
// X-axis represents hue (0-360°), Y-axis represents chroma (0.0-0.4).

package primitives

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/color"
	"texelation/texelui/core"
)

// HCPlane is a 2D color selector for hue and chroma in OKLCH color space.
// The X-axis represents hue (0-360°), the Y-axis represents chroma (0.0-0.4).
// Colors are rendered using the current lightness value.
type HCPlane struct {
	core.BaseWidget

	// OKLCH values
	H float64 // Hue: 0-360
	C float64 // Chroma: 0.0-0.4
	L float64 // Lightness: 0.0-1.0 (used for rendering, not selectable here)

	// Cursor position
	cursorX int
	cursorY int

	// Callbacks
	OnChange func(h, c float64) // Called when hue or chroma changes

	inv func(core.Rect)
}

// NewHCPlane creates a hue × chroma plane at the given position.
func NewHCPlane(x, y, w, h int) *HCPlane {
	p := &HCPlane{
		L:       0.7,  // Default lightness for visibility
		H:       270,  // Default hue (purple)
		C:       0.15, // Default chroma
		cursorX: 15,   // 270/360 * 20 ≈ 15
		cursorY: 6,    // (1 - 0.15/0.4) * 10 ≈ 6
	}
	p.SetPosition(x, y)
	p.Resize(w, h)
	p.SetFocusable(true)

	// Configure focus style from theme
	tm := theme.Get()
	fg := tm.GetSemanticColor("border.active")
	bg := tm.GetSemanticColor("bg.surface")
	p.SetFocusedStyle(tcell.StyleDefault.Foreground(fg).Background(bg), true)

	return p
}

// SetHC sets the hue and chroma values and updates cursor position.
func (p *HCPlane) SetHC(h, c float64) {
	p.H = h
	p.C = c
	p.updateCursorFromValues()
	p.invalidate()
}

// SetLightness sets the lightness value used for rendering colors.
func (p *HCPlane) SetLightness(l float64) {
	if l < 0 {
		l = 0
	}
	if l > 1 {
		l = 1
	}
	if p.L == l {
		return
	}
	p.L = l
	p.invalidate()
}

// Resize updates the size and recalculates cursor position.
func (p *HCPlane) Resize(w, h int) {
	oldW, oldH := p.Rect.W, p.Rect.H
	p.BaseWidget.Resize(w, h)

	// Scale cursor position to new dimensions
	if oldW > 1 && w > 1 {
		p.cursorX = p.cursorX * (w - 1) / (oldW - 1)
	}
	if oldH > 1 && h > 1 {
		p.cursorY = p.cursorY * (h - 1) / (oldH - 1)
	}

	// Clamp cursor
	if p.cursorX >= w {
		p.cursorX = w - 1
	}
	if p.cursorX < 0 {
		p.cursorX = 0
	}
	if p.cursorY >= h {
		p.cursorY = h - 1
	}
	if p.cursorY < 0 {
		p.cursorY = 0
	}
}

// Draw renders the hue × chroma plane.
func (p *HCPlane) Draw(painter *core.Painter) {
	tm := theme.Get()
	bg := tm.GetSemanticColor("bg.surface")

	w, h := p.Rect.W, p.Rect.H
	if w <= 1 || h <= 1 {
		return
	}

	focused := p.IsFocused()

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Calculate OKLCH values for this cell
			// X-axis: Hue 0-360
			// Y-axis: Chroma 0.0-0.4 (inverted: top = max chroma)
			cellH := float64(x) / float64(w-1) * 360.0
			cellC := (1.0 - float64(y)/float64(h-1)) * 0.4

			// Convert OKLCH to RGB
			rgb := color.OKLCHToRGB(p.L, cellC, cellH)
			cellColor := tcell.NewRGBColor(rgb.R, rgb.G, rgb.B)

			// Determine character
			ch := '░' // Light shade for color tiles
			if x == p.cursorX && y == p.cursorY {
				if focused {
					ch = '█' // Full block for active cursor
				} else {
					ch = '▓' // Dark shade for inactive cursor
				}
			}

			style := tcell.StyleDefault.Foreground(cellColor).Background(bg)
			painter.SetCell(p.Rect.X+x, p.Rect.Y+y, ch, style)
		}
	}
}

// HandleKey processes keyboard input for hue/chroma selection.
func (p *HCPlane) HandleKey(ev *tcell.EventKey) bool {
	// Check for Shift modifier for fine control
	fine := ev.Modifiers()&tcell.ModShift != 0

	switch ev.Key() {
	case tcell.KeyLeft:
		if fine {
			// Fine control: decrement H by 1°
			p.H -= 1.0
			if p.H < 0 {
				p.H = 0
			}
			p.updateCursorFromValues()
			p.notifyChange()
		} else if p.cursorX > 0 {
			p.cursorX--
			p.updateFromCursor()
			p.notifyChange()
		}
		p.invalidate()
		return true

	case tcell.KeyRight:
		if fine {
			// Fine control: increment H by 1°
			p.H += 1.0
			if p.H > 360 {
				p.H = 360
			}
			p.updateCursorFromValues()
			p.notifyChange()
		} else if p.cursorX < p.Rect.W-1 {
			p.cursorX++
			p.updateFromCursor()
			p.notifyChange()
		}
		p.invalidate()
		return true

	case tcell.KeyUp:
		if fine {
			// Fine control: increment C by 0.01
			p.C += 0.01
			if p.C > 0.4 {
				p.C = 0.4
			}
			p.updateCursorFromValues()
			p.notifyChange()
		} else if p.cursorY > 0 {
			p.cursorY--
			p.updateFromCursor()
			p.notifyChange()
		}
		p.invalidate()
		return true

	case tcell.KeyDown:
		if fine {
			// Fine control: decrement C by 0.01
			p.C -= 0.01
			if p.C < 0 {
				p.C = 0
			}
			p.updateCursorFromValues()
			p.notifyChange()
		} else if p.cursorY < p.Rect.H-1 {
			p.cursorY++
			p.updateFromCursor()
			p.notifyChange()
		}
		p.invalidate()
		return true

	case tcell.KeyHome:
		p.cursorX = 0
		p.updateFromCursor()
		p.notifyChange()
		p.invalidate()
		return true

	case tcell.KeyEnd:
		p.cursorX = p.Rect.W - 1
		p.updateFromCursor()
		p.notifyChange()
		p.invalidate()
		return true
	}

	return false
}

// HandleMouse processes mouse input for direct selection.
func (p *HCPlane) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !p.HitTest(x, y) {
		return false
	}

	if ev.Buttons() == tcell.Button1 {
		p.cursorX = x - p.Rect.X
		p.cursorY = y - p.Rect.Y

		// Clamp cursor
		if p.cursorX < 0 {
			p.cursorX = 0
		}
		if p.cursorX >= p.Rect.W {
			p.cursorX = p.Rect.W - 1
		}
		if p.cursorY < 0 {
			p.cursorY = 0
		}
		if p.cursorY >= p.Rect.H {
			p.cursorY = p.Rect.H - 1
		}

		p.updateFromCursor()
		p.notifyChange()
		p.invalidate()
		return true
	}

	return false
}

// updateFromCursor updates H and C from cursor position.
func (p *HCPlane) updateFromCursor() {
	if p.Rect.W > 1 {
		p.H = float64(p.cursorX) / float64(p.Rect.W-1) * 360.0
	}
	if p.Rect.H > 1 {
		p.C = (1.0 - float64(p.cursorY)/float64(p.Rect.H-1)) * 0.4
	}
}

// updateCursorFromValues updates cursor position from H and C values.
func (p *HCPlane) updateCursorFromValues() {
	if p.Rect.W > 1 {
		p.cursorX = int(p.H / 360.0 * float64(p.Rect.W-1))
		if p.cursorX < 0 {
			p.cursorX = 0
		}
		if p.cursorX >= p.Rect.W {
			p.cursorX = p.Rect.W - 1
		}
	}
	if p.Rect.H > 1 {
		p.cursorY = int((1.0 - p.C/0.4) * float64(p.Rect.H-1))
		if p.cursorY < 0 {
			p.cursorY = 0
		}
		if p.cursorY >= p.Rect.H {
			p.cursorY = p.Rect.H - 1
		}
	}
}

// notifyChange calls the OnChange callback if set.
func (p *HCPlane) notifyChange() {
	if p.OnChange != nil {
		p.OnChange(p.H, p.C)
	}
}

// SetInvalidator sets the invalidation callback.
func (p *HCPlane) SetInvalidator(fn func(core.Rect)) {
	p.inv = fn
}

// invalidate marks the widget as needing redraw.
func (p *HCPlane) invalidate() {
	if p.inv != nil {
		p.inv(p.Rect)
	}
}

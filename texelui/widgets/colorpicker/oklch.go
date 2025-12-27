// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/widgets/colorpicker/oklch.go
// Summary: OKLCH custom color selection mode.

package colorpicker

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/color"
	"texelation/texelui/core"
)

// OKLCHControl identifies which control is active in OKLCH picker.
type OKLCHControl int

const (
	OKLCHControlPlane     OKLCHControl = iota // Hue x Chroma plane
	OKLCHControlLightness                     // Lightness slider
)

// OKLCHPicker provides a custom color picker using OKLCH color space.
// Layout:
//   - H×C (hue×chroma) plane: 2D grid (20x10)
//   - L (lightness) slider: vertical on right
//   - Live preview at bottom
type OKLCHPicker struct {
	// OKLCH values
	L float64 // Lightness: 0.0 - 1.0
	C float64 // Chroma: 0.0 - 0.4
	H float64 // Hue: 0 - 360

	// UI state
	activeControl OKLCHControl
	planeW        int // Hue axis width
	planeH        int // Chroma axis height
	cursorX       int // Cursor X position (hue)
	cursorY       int // Cursor Y position (chroma)
}

// NewOKLCHPicker creates an OKLCH color picker.
func NewOKLCHPicker() *OKLCHPicker {
	return &OKLCHPicker{
		L:             0.7,  // Mid-high lightness for visibility
		C:             0.15, // Moderate chroma
		H:             270,  // Purple hue (like mauve)
		activeControl: OKLCHControlPlane,
		planeW:        20,
		planeH:        10,
		cursorX:       15, // 270/360 * 20 ≈ 15
		cursorY:       6,  // (1 - 0.15/0.4) * 10 ≈ 6
	}
}

func (op *OKLCHPicker) Draw(painter *core.Painter, rect core.Rect) {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	baseStyle := tcell.StyleDefault.Foreground(fg).Background(bg)

	// Fill background
	painter.Fill(rect, ' ', baseStyle)

	// Layout:
	// [   Hue × Chroma Plane   ] [L]
	// [        20 × 10         ] [│]
	// [                        ] [│]
	// [ Preview: [███] OKLCH   ]

	planeRect := core.Rect{X: rect.X, Y: rect.Y, W: op.planeW, H: op.planeH}
	sliderX := rect.X + op.planeW + 2
	sliderRect := core.Rect{X: sliderX, Y: rect.Y, W: 3, H: op.planeH}
	previewRect := core.Rect{X: rect.X, Y: rect.Y + op.planeH + 1, W: rect.W, H: 2}

	// Draw Hue × Chroma plane
	op.drawPlane(painter, planeRect, bg)

	// Draw Lightness slider
	op.drawLightnessSlider(painter, sliderRect, fg, bg)

	// Draw preview
	op.drawPreview(painter, previewRect, fg, bg)

	// Draw labels
	painter.DrawText(rect.X, rect.Y+op.planeH, "H→", baseStyle)
	if op.activeControl == OKLCHControlPlane {
		painter.DrawText(sliderX, rect.Y+op.planeH, "L", baseStyle.Dim(true))
	} else {
		painter.DrawText(sliderX, rect.Y+op.planeH, "L", baseStyle.Bold(true))
	}
}

func (op *OKLCHPicker) drawPlane(painter *core.Painter, rect core.Rect, bg tcell.Color) {
	// X-axis: Hue 0-360
	// Y-axis: Chroma 0.0-0.4 (inverted: top = max chroma)

	// Guard against division by zero
	if rect.W <= 1 || rect.H <= 1 {
		return
	}

	for y := 0; y < rect.H; y++ {
		for x := 0; x < rect.W; x++ {
			// Calculate OKLCH values for this cell
			h := float64(x) / float64(rect.W-1) * 360.0
			c := (1.0 - float64(y)/float64(rect.H-1)) * 0.4 // Inverted Y

			// Convert OKLCH to RGB
			rgb := color.OKLCHToRGB(op.L, c, h)
			cellColor := tcell.NewRGBColor(rgb.R, rgb.G, rgb.B)

			// Determine character
			ch := '·'
			if x == op.cursorX && y == op.cursorY {
				if op.activeControl == OKLCHControlPlane {
					ch = '●' // Active cursor
				} else {
					ch = '○' // Inactive cursor
				}
			}

			style := tcell.StyleDefault.Foreground(cellColor).Background(bg)
			painter.SetCell(rect.X+x, rect.Y+y, ch, style)
		}
	}
}

func (op *OKLCHPicker) drawLightnessSlider(painter *core.Painter, rect core.Rect, fg, bg tcell.Color) {
	// Guard against division by zero
	if rect.H <= 1 {
		return
	}

	// Vertical slider: top = 1.0 (bright), bottom = 0.0 (dark)
	sliderPos := int((1.0 - op.L) * float64(rect.H-1))

	for y := 0; y < rect.H; y++ {
		l := 1.0 - float64(y)/float64(rect.H-1)

		// Use current H and C for preview
		rgb := color.OKLCHToRGB(l, op.C, op.H)
		sliderColor := tcell.NewRGBColor(rgb.R, rgb.G, rgb.B)

		// Left border
		painter.SetCell(rect.X, rect.Y+y, '│', tcell.StyleDefault.Foreground(fg).Background(bg))

		// Slider content
		ch := '█'
		style := tcell.StyleDefault.Foreground(sliderColor).Background(bg)
		if y == sliderPos {
			if op.activeControl == OKLCHControlLightness {
				ch = '◆' // Active thumb
				style = style.Reverse(true)
			} else {
				ch = '◇' // Inactive thumb
			}
		}
		painter.SetCell(rect.X+1, rect.Y+y, ch, style)

		// Right border
		painter.SetCell(rect.X+2, rect.Y+y, '│', tcell.StyleDefault.Foreground(fg).Background(bg))
	}
}

func (op *OKLCHPicker) drawPreview(painter *core.Painter, rect core.Rect, fg, bg tcell.Color) {
	result := op.GetResult()
	baseStyle := tcell.StyleDefault.Foreground(fg).Background(bg)

	y := rect.Y
	x := rect.X

	// Draw color sample: [███]
	painter.SetCell(x, y, '[', baseStyle)
	x++
	for i := 0; i < 3; i++ {
		painter.SetCell(x, y, '█', tcell.StyleDefault.Foreground(result.Color).Background(bg))
		x++
	}
	painter.SetCell(x, y, ']', baseStyle)
	x += 2

	// Draw OKLCH values
	preview := fmt.Sprintf("L:%.2f C:%.2f H:%.0f°", op.L, op.C, op.H)
	painter.DrawText(x, y, preview, baseStyle)

	// Second line: RGB values
	y++
	x = rect.X
	rgbStr := fmt.Sprintf("#%02x%02x%02x RGB(%d,%d,%d)", result.R, result.G, result.B, result.R, result.G, result.B)
	painter.DrawText(x, y, rgbStr, baseStyle.Dim(true))
}

func (op *OKLCHPicker) HandleKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyTab, tcell.KeyBacktab:
		// Toggle between plane and slider
		if op.activeControl == OKLCHControlPlane {
			op.activeControl = OKLCHControlLightness
		} else {
			op.activeControl = OKLCHControlPlane
		}
		return true
	}

	if op.activeControl == OKLCHControlPlane {
		return op.handlePlaneKey(ev)
	}
	return op.handleLightnessKey(ev)
}

func (op *OKLCHPicker) handlePlaneKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyLeft:
		if op.cursorX > 0 {
			op.cursorX--
			op.updateFromCursor()
		}
		return true
	case tcell.KeyRight:
		if op.cursorX < op.planeW-1 {
			op.cursorX++
			op.updateFromCursor()
		}
		return true
	case tcell.KeyUp:
		if op.cursorY > 0 {
			op.cursorY--
			op.updateFromCursor()
		}
		return true
	case tcell.KeyDown:
		if op.cursorY < op.planeH-1 {
			op.cursorY++
			op.updateFromCursor()
		}
		return true
	case tcell.KeyHome:
		op.cursorX = 0
		op.updateFromCursor()
		return true
	case tcell.KeyEnd:
		op.cursorX = op.planeW - 1
		op.updateFromCursor()
		return true
	}
	return false
}

func (op *OKLCHPicker) handleLightnessKey(ev *tcell.EventKey) bool {
	step := 0.05

	switch ev.Key() {
	case tcell.KeyUp:
		op.L += step
		if op.L > 1.0 {
			op.L = 1.0
		}
		return true
	case tcell.KeyDown:
		op.L -= step
		if op.L < 0.0 {
			op.L = 0.0
		}
		return true
	case tcell.KeyHome:
		op.L = 1.0
		return true
	case tcell.KeyEnd:
		op.L = 0.0
		return true
	}
	return false
}

func (op *OKLCHPicker) updateFromCursor() {
	// Update H and C from cursor position
	if op.planeW > 1 {
		op.H = float64(op.cursorX) / float64(op.planeW-1) * 360.0
	}
	if op.planeH > 1 {
		op.C = (1.0 - float64(op.cursorY)/float64(op.planeH-1)) * 0.4
	}
}

func (op *OKLCHPicker) HandleMouse(ev *tcell.EventMouse, rect core.Rect) bool {
	x, y := ev.Position()
	if x < rect.X || y < rect.Y || x >= rect.X+rect.W || y >= rect.Y+rect.H {
		return false
	}

	planeRect := core.Rect{X: rect.X, Y: rect.Y, W: op.planeW, H: op.planeH}
	sliderX := rect.X + op.planeW + 2
	sliderRect := core.Rect{X: sliderX, Y: rect.Y, W: 3, H: op.planeH}

	// Check if clicking in plane
	if ev.Buttons() == tcell.Button1 {
		if x >= planeRect.X && x < planeRect.X+planeRect.W &&
			y >= planeRect.Y && y < planeRect.Y+planeRect.H {
			op.activeControl = OKLCHControlPlane
			op.cursorX = x - planeRect.X
			op.cursorY = y - planeRect.Y
			op.updateFromCursor()
			return true
		}

		// Check if clicking in slider
		if x >= sliderRect.X && x < sliderRect.X+sliderRect.W &&
			y >= sliderRect.Y && y < sliderRect.Y+sliderRect.H {
			op.activeControl = OKLCHControlLightness
			relY := y - sliderRect.Y
			if sliderRect.H > 1 {
				op.L = 1.0 - float64(relY)/float64(sliderRect.H-1)
				if op.L < 0.0 {
					op.L = 0.0
				}
				if op.L > 1.0 {
					op.L = 1.0
				}
			}
			return true
		}
	}

	return false
}

func (op *OKLCHPicker) GetResult() PickerResult {
	rgb := color.OKLCHToRGB(op.L, op.C, op.H)
	tcellColor := tcell.NewRGBColor(rgb.R, rgb.G, rgb.B)
	source := fmt.Sprintf("oklch(%.2f,%.2f,%.0f)", op.L, op.C, op.H)

	return PickerResult{
		Color:  tcellColor,
		Source: source,
		R:      rgb.R,
		G:      rgb.G,
		B:      rgb.B,
	}
}

func (op *OKLCHPicker) PreferredSize() (int, int) {
	// Plane (20) + spacing (2) + slider (3) = 25 width
	// Plane (10) + label (1) + preview (2) = 13 height
	return 28, 13
}

func (op *OKLCHPicker) SetColor(c tcell.Color) {
	r, g, b := c.RGB()
	op.L, op.C, op.H = color.RGBToOKLCH(r, g, b)

	// Update cursor position
	op.cursorX = int(op.H / 360.0 * float64(op.planeW-1))
	op.cursorY = int((1.0 - op.C/0.4) * float64(op.planeH-1))

	// Clamp cursor
	if op.cursorX < 0 {
		op.cursorX = 0
	}
	if op.cursorX >= op.planeW {
		op.cursorX = op.planeW - 1
	}
	if op.cursorY < 0 {
		op.cursorY = 0
	}
	if op.cursorY >= op.planeH {
		op.cursorY = op.planeH - 1
	}
}

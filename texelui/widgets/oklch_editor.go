// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/widgets/oklch_editor.go
// Summary: Widget-based OKLCH color editor composing HCPlane and LightnessSlider.

package widgets

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/color"
	"texelation/texelui/core"
	"texelation/texelui/primitives"
)

// OKLCHEditorFocus identifies which component has focus.
type OKLCHEditorFocus int

const (
	OKLCHFocusPlane  OKLCHEditorFocus = iota // HCPlane has focus
	OKLCHFocusSlider                         // LightnessSlider has focus
)

// OKLCHEditor is a widget for editing colors in OKLCH color space.
// It composes an HCPlane (for hue/chroma) and a LightnessSlider.
type OKLCHEditor struct {
	core.BaseWidget

	// Child widgets
	hcPlane  *primitives.HCPlane
	lSlider  *primitives.LightnessSlider

	// State
	focus OKLCHEditorFocus

	// Callbacks
	OnChange func(tcell.Color) // Called when the color changes

	inv func(core.Rect)
}

// NewOKLCHEditor creates an OKLCH editor at the given position.
func NewOKLCHEditor(x, y, w, h int) *OKLCHEditor {
	oe := &OKLCHEditor{
		focus: OKLCHFocusPlane,
	}
	oe.SetPosition(x, y)
	oe.Resize(w, h)
	oe.SetFocusable(true)

	// Create child widgets (they will be positioned in Resize)
	oe.hcPlane = primitives.NewHCPlane(0, 0, 10, 10)
	oe.lSlider = primitives.NewLightnessSlider(0, 0, 3, 10)

	// Wire up callbacks to keep components in sync
	oe.hcPlane.OnChange = func(h, c float64) {
		oe.lSlider.SetHC(h, c)
		oe.notifyChange()
	}
	oe.lSlider.OnChange = func(l float64) {
		oe.hcPlane.SetLightness(l)
		oe.notifyChange()
	}

	// Configure focus style from theme
	tm := theme.Get()
	fg := tm.GetSemanticColor("border.active")
	bg := tm.GetSemanticColor("bg.surface")
	oe.SetFocusedStyle(tcell.StyleDefault.Foreground(fg).Background(bg), true)

	return oe
}

// SetPosition updates the position and propagates to children.
func (oe *OKLCHEditor) SetPosition(x, y int) {
	oe.BaseWidget.SetPosition(x, y)
	oe.layoutChildren()
}

// Resize updates the size and lays out children.
func (oe *OKLCHEditor) Resize(w, h int) {
	oe.BaseWidget.Resize(w, h)
	oe.layoutChildren()
}

// layoutChildren positions and sizes the HCPlane and LightnessSlider.
// Layout:
//   [   HCPlane   ] [L]
//   [ fills most  ] [│]
//   [   space     ] [│]
//   [H→ label row ] [L]
func (oe *OKLCHEditor) layoutChildren() {
	if oe.hcPlane == nil || oe.lSlider == nil {
		return
	}

	// Reserve: 2 (gap) + 3 (slider width) = 5 chars on right
	// Reserve: 3 rows at bottom for labels/preview
	planeW := oe.Rect.W - 5
	planeH := oe.Rect.H - 3

	if planeW < 5 {
		planeW = 5
	}
	if planeH < 3 {
		planeH = 3
	}

	sliderX := oe.Rect.X + planeW + 2

	oe.hcPlane.SetPosition(oe.Rect.X, oe.Rect.Y)
	oe.hcPlane.Resize(planeW, planeH)

	oe.lSlider.SetPosition(sliderX, oe.Rect.Y)
	oe.lSlider.Resize(3, planeH)
}

// SetColor sets the current color by converting from RGB to OKLCH.
func (oe *OKLCHEditor) SetColor(c tcell.Color) {
	r, g, b := c.RGB()
	l, ch, h := color.RGBToOKLCH(r, g, b)

	oe.hcPlane.SetHC(h, ch)
	oe.hcPlane.SetLightness(l)
	oe.lSlider.SetLightness(l)
	oe.lSlider.SetHC(h, ch)
	oe.invalidate()
}

// GetColor returns the current color.
func (oe *OKLCHEditor) GetColor() tcell.Color {
	l := oe.lSlider.L
	h := oe.hcPlane.H
	c := oe.hcPlane.C
	rgb := color.OKLCHToRGB(l, c, h)
	return tcell.NewRGBColor(rgb.R, rgb.G, rgb.B)
}

// GetSource returns a string representation of the current OKLCH values.
func (oe *OKLCHEditor) GetSource() string {
	l := oe.lSlider.L
	h := oe.hcPlane.H
	c := oe.hcPlane.C
	return fmt.Sprintf("oklch(%.2f,%.2f,%.0f)", l, c, h)
}

// Focus focuses the editor and the active child.
func (oe *OKLCHEditor) Focus() {
	oe.BaseWidget.Focus()
	oe.updateChildFocus()
}

// Blur blurs the editor and all children.
func (oe *OKLCHEditor) Blur() {
	oe.BaseWidget.Blur()
	oe.hcPlane.Blur()
	oe.lSlider.Blur()
}

// updateChildFocus updates which child has focus.
func (oe *OKLCHEditor) updateChildFocus() {
	oe.hcPlane.Blur()
	oe.lSlider.Blur()

	switch oe.focus {
	case OKLCHFocusPlane:
		oe.hcPlane.Focus()
	case OKLCHFocusSlider:
		oe.lSlider.Focus()
	}
}

// Draw renders the OKLCH editor.
func (oe *OKLCHEditor) Draw(p *core.Painter) {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	baseStyle := tcell.StyleDefault.Foreground(fg).Background(bg)

	// Fill background
	p.Fill(oe.Rect, ' ', baseStyle)

	// Draw child widgets
	oe.hcPlane.Draw(p)
	oe.lSlider.Draw(p)

	// Draw labels below the widgets
	planeH := oe.hcPlane.Rect.H
	labelY := oe.Rect.Y + planeH

	// H→ label under plane
	p.DrawText(oe.Rect.X, labelY, "H→", baseStyle)

	// L label under slider
	sliderX := oe.lSlider.Rect.X
	lStyle := baseStyle
	if oe.focus == OKLCHFocusSlider {
		lStyle = lStyle.Bold(true)
	} else {
		lStyle = lStyle.Dim(true)
	}
	p.DrawText(sliderX, labelY, "L", lStyle)

	// Draw preview on next row
	previewY := labelY + 1
	oe.drawPreview(p, oe.Rect.X, previewY, baseStyle, bg)
}

// drawPreview draws the color preview with OKLCH values.
func (oe *OKLCHEditor) drawPreview(p *core.Painter, x, y int, baseStyle tcell.Style, bg tcell.Color) {
	currentColor := oe.GetColor()
	l := oe.lSlider.L
	h := oe.hcPlane.H
	c := oe.hcPlane.C

	// Draw color sample: [███]
	p.SetCell(x, y, '[', baseStyle)
	x++
	for i := 0; i < 3; i++ {
		p.SetCell(x, y, '█', tcell.StyleDefault.Foreground(currentColor).Background(bg))
		x++
	}
	p.SetCell(x, y, ']', baseStyle)
	x += 2

	// Draw OKLCH values
	preview := fmt.Sprintf("L:%.2f C:%.2f H:%.0f°", l, c, h)
	p.DrawText(x, y, preview, baseStyle)

	// Second line: RGB values
	y++
	x = oe.Rect.X
	r, g, b := currentColor.RGB()
	rgbStr := fmt.Sprintf("#%02x%02x%02x RGB(%d,%d,%d)", r, g, b, r, g, b)
	p.DrawText(x, y, rgbStr, baseStyle.Dim(true))
}

// HandleKey processes keyboard input.
// Note: Tab is NOT handled here - parent should use CycleFocus for focus navigation.
func (oe *OKLCHEditor) HandleKey(ev *tcell.EventKey) bool {
	// Don't handle Tab - let parent use CycleFocus for consistent behavior
	if ev.Key() == tcell.KeyTab {
		return false
	}

	// Route to focused child
	var handled bool
	switch oe.focus {
	case OKLCHFocusPlane:
		handled = oe.hcPlane.HandleKey(ev)
	case OKLCHFocusSlider:
		handled = oe.lSlider.HandleKey(ev)
	}

	if handled {
		oe.invalidate()
	}
	return handled
}

// HandleMouse processes mouse input.
func (oe *OKLCHEditor) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !oe.HitTest(x, y) {
		return false
	}

	// Check if click is in HCPlane
	if oe.hcPlane.HitTest(x, y) {
		if oe.focus != OKLCHFocusPlane {
			oe.focus = OKLCHFocusPlane
			oe.updateChildFocus()
		}
		handled := oe.hcPlane.HandleMouse(ev)
		if handled {
			oe.invalidate()
		}
		return true
	}

	// Check if click is in LightnessSlider
	if oe.lSlider.HitTest(x, y) {
		if oe.focus != OKLCHFocusSlider {
			oe.focus = OKLCHFocusSlider
			oe.updateChildFocus()
		}
		handled := oe.lSlider.HandleMouse(ev)
		if handled {
			oe.invalidate()
		}
		return true
	}

	return true
}

// HitTest returns true if the point is within the editor.
func (oe *OKLCHEditor) HitTest(x, y int) bool {
	return oe.Rect.Contains(x, y)
}

// VisitChildren implements core.ChildContainer.
func (oe *OKLCHEditor) VisitChildren(f func(core.Widget)) {
	f(oe.hcPlane)
	f(oe.lSlider)
}

// CycleFocus implements core.FocusCycler.
func (oe *OKLCHEditor) CycleFocus(forward bool) bool {
	if forward {
		switch oe.focus {
		case OKLCHFocusPlane:
			oe.focus = OKLCHFocusSlider
			oe.updateChildFocus()
			oe.invalidate()
			return true
		case OKLCHFocusSlider:
			// At boundary
			return false
		}
	} else {
		switch oe.focus {
		case OKLCHFocusSlider:
			oe.focus = OKLCHFocusPlane
			oe.updateChildFocus()
			oe.invalidate()
			return true
		case OKLCHFocusPlane:
			// At boundary
			return false
		}
	}
	return false
}

// TrapsFocus implements core.FocusCycler.
func (oe *OKLCHEditor) TrapsFocus() bool {
	return false // Let parent handle cycling
}

// SetInvalidator sets the invalidation callback.
func (oe *OKLCHEditor) SetInvalidator(fn func(core.Rect)) {
	oe.inv = fn
	oe.hcPlane.SetInvalidator(fn)
	oe.lSlider.SetInvalidator(fn)
}

// invalidate marks the widget as needing redraw.
func (oe *OKLCHEditor) invalidate() {
	if oe.inv != nil {
		oe.inv(oe.Rect)
	}
}

// notifyChange calls the OnChange callback if set.
func (oe *OKLCHEditor) notifyChange() {
	if oe.OnChange != nil {
		oe.OnChange(oe.GetColor())
	}
}

// ResetFocus resets focus to the first element (HCPlane).
func (oe *OKLCHEditor) ResetFocus() {
	oe.focus = OKLCHFocusPlane
	oe.updateChildFocus()
}

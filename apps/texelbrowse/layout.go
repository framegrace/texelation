// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelbrowse/layout.go
// Summary: Adaptive layout manager that arranges widgets vertically
//          according to the current display mode (reading or form).

package texelbrowse

import (
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
)

// LayoutManager arranges widgets vertically according to the current
// display mode. Reading mode uses narrow margins and tight spacing;
// form mode uses wider margins with extra spacing between interactive
// elements.
type LayoutManager struct {
	width, height int
	mode          DisplayMode
}

// NewLayoutManager creates a layout manager for the given dimensions.
func NewLayoutManager(width, height int) *LayoutManager {
	return &LayoutManager{
		width:  width,
		height: height,
		mode:   ModeReading,
	}
}

// SetMode changes the display mode used for layout.
func (lm *LayoutManager) SetMode(mode DisplayMode) {
	lm.mode = mode
}

// Mode returns the current display mode.
func (lm *LayoutManager) Mode() DisplayMode {
	return lm.mode
}

// Resize updates the available dimensions.
func (lm *LayoutManager) Resize(width, height int) {
	lm.width = width
	lm.height = height
}

// Arrange positions and sizes all widgets according to the current mode.
func (lm *LayoutManager) Arrange(ws []core.Widget) {
	if len(ws) == 0 {
		return
	}
	switch lm.mode {
	case ModeForm:
		lm.arrangeForm(ws)
	default:
		lm.arrangeReading(ws)
	}
}

// arrangeReading lays out widgets for reading mode: 1 char margin on
// each side, widgets stacked vertically with single spacing.
func (lm *LayoutManager) arrangeReading(ws []core.Widget) {
	const margin = 1
	contentWidth := max(lm.width-2*margin, 1)

	y := 0
	for _, w := range ws {
		if isInputWidget(w) {
			w.Resize(contentWidth, 1)
		} else {
			nw, nh := w.Size()
			nw = min(nw, contentWidth)
			nh = max(nh, 1)
			w.Resize(nw, nh)
		}
		w.SetPosition(margin, y)
		_, h := w.Size()
		y += h
	}
}

// arrangeForm lays out widgets for form mode: 2 char margin on each
// side, inputs get full content width (min 40), buttons get 1/3 content
// width, extra vertical spacing between interactive elements.
func (lm *LayoutManager) arrangeForm(ws []core.Widget) {
	const margin = 2
	contentWidth := max(lm.width-2*margin, 1)
	minInputWidth := min(40, contentWidth)
	buttonWidth := max(contentWidth/3, 1)

	y := 0
	for i, w := range ws {
		switch {
		case isInputWidget(w):
			ww := max(contentWidth, minInputWidth)
			w.Resize(ww, 1)
		case isButtonWidget(w):
			w.Resize(buttonWidth, 1)
		default:
			// Text elements keep natural width, clamped to content width.
			nw, nh := w.Size()
			nw = min(nw, contentWidth)
			nh = max(nh, 1)
			w.Resize(nw, nh)
		}

		// Extra vertical spacing before interactive elements (not the first).
		if i > 0 && isInteractiveWidget(w) {
			y += 2
		}

		w.SetPosition(margin, y)
		_, h := w.Size()
		y += h
	}
}

// isInputWidget returns true for Input-type widgets.
func isInputWidget(w core.Widget) bool {
	_, ok := w.(*widgets.Input)
	return ok
}

// isButtonWidget returns true for Button-type widgets.
func isButtonWidget(w core.Widget) bool {
	_, ok := w.(*widgets.Button)
	return ok
}

// isInteractiveWidget returns true for widgets that represent
// interactive form controls (inputs, buttons, checkboxes).
func isInteractiveWidget(w core.Widget) bool {
	switch w.(type) {
	case *widgets.Input, *widgets.Button, *widgets.Checkbox:
		return true
	}
	return false
}

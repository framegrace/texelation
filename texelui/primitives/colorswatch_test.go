// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package primitives

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"texelation/texelui/core"
)

func TestColorSwatch_NewWithDefaults(t *testing.T) {
	cs := NewColorSwatch(10, 20, 8, 3, tcell.ColorRed)

	x, y := cs.Position()
	if x != 10 || y != 20 {
		t.Errorf("position = (%d, %d), want (10, 20)", x, y)
	}

	w, h := cs.Size()
	if w != 8 || h != 3 {
		t.Errorf("size = (%d, %d), want (8, 3)", w, h)
	}

	if cs.Color != tcell.ColorRed {
		t.Errorf("color = %v, want tcell.ColorRed", cs.Color)
	}

	if !cs.ShowBorder {
		t.Error("ShowBorder should default to true")
	}

	if !cs.Focusable() {
		t.Error("ColorSwatch should be focusable by default")
	}
}

func TestColorSwatch_SetColor(t *testing.T) {
	cs := NewColorSwatch(0, 0, 8, 3, tcell.ColorRed)

	invalidated := false
	cs.SetInvalidator(func(_ core.Rect) {
		invalidated = true
	})

	cs.SetColor(tcell.ColorBlue)
	if cs.Color != tcell.ColorBlue {
		t.Errorf("color = %v, want tcell.ColorBlue", cs.Color)
	}
	if !invalidated {
		t.Error("SetColor should invalidate the widget")
	}

	// Setting same color should not invalidate
	invalidated = false
	cs.SetColor(tcell.ColorBlue)
	if invalidated {
		t.Error("SetColor with same color should not invalidate")
	}
}

func TestColorSwatch_SetLabel(t *testing.T) {
	cs := NewColorSwatch(0, 0, 8, 3, tcell.ColorRed)

	invalidated := false
	cs.SetInvalidator(func(_ core.Rect) {
		invalidated = true
	})

	cs.SetLabel("#FF0000")
	if cs.Label != "#FF0000" {
		t.Errorf("label = %q, want \"#FF0000\"", cs.Label)
	}
	if !invalidated {
		t.Error("SetLabel should invalidate the widget")
	}

	// Setting same label should not invalidate
	invalidated = false
	cs.SetLabel("#FF0000")
	if invalidated {
		t.Error("SetLabel with same label should not invalidate")
	}
}

func TestColorSwatch_ContrastFG(t *testing.T) {
	tests := []struct {
		color   tcell.Color
		wantFG  tcell.Color
		message string
	}{
		{tcell.ColorWhite, tcell.ColorBlack, "white background should have black text"},
		{tcell.ColorBlack, tcell.ColorWhite, "black background should have white text"},
		{tcell.NewRGBColor(255, 255, 0), tcell.ColorBlack, "yellow background should have black text"},
		{tcell.NewRGBColor(0, 0, 128), tcell.ColorWhite, "dark blue background should have white text"},
	}

	for _, tt := range tests {
		cs := NewColorSwatch(0, 0, 8, 3, tt.color)
		fg := cs.contrastFG()
		if fg != tt.wantFG {
			t.Errorf("%s: got %v, want %v", tt.message, fg, tt.wantFG)
		}
	}
}

func TestColorSwatch_HandleMouse(t *testing.T) {
	cs := NewColorSwatch(10, 10, 8, 3, tcell.ColorRed)

	// Click inside should return true
	ev := tcell.NewEventMouse(12, 11, tcell.Button1, tcell.ModNone)
	if !cs.HandleMouse(ev) {
		t.Error("click inside should be handled")
	}

	// Click outside should return false
	ev = tcell.NewEventMouse(5, 5, tcell.Button1, tcell.ModNone)
	if cs.HandleMouse(ev) {
		t.Error("click outside should not be handled")
	}

	// Non-click events inside should return false
	ev = tcell.NewEventMouse(12, 11, tcell.ButtonNone, tcell.ModNone)
	if cs.HandleMouse(ev) {
		t.Error("non-click inside should not be handled")
	}
}

func TestColorSwatch_HandleKeyReturnsfalse(t *testing.T) {
	cs := NewColorSwatch(0, 0, 8, 3, tcell.ColorRed)

	ev := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	if cs.HandleKey(ev) {
		t.Error("ColorSwatch should not handle keys")
	}
}

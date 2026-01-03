// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package widgets

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"texelation/texelui/core"
)

func TestOKLCHEditor_NewWithDefaults(t *testing.T) {
	oe := NewOKLCHEditor(10, 20, 30, 15)

	x, y := oe.Position()
	if x != 10 || y != 20 {
		t.Errorf("position = (%d, %d), want (10, 20)", x, y)
	}

	w, h := oe.Size()
	if w != 30 || h != 15 {
		t.Errorf("size = (%d, %d), want (30, 15)", w, h)
	}

	if !oe.Focusable() {
		t.Error("OKLCHEditor should be focusable")
	}

	if oe.focus != OKLCHFocusPlane {
		t.Error("default focus should be on plane")
	}
}

func TestOKLCHEditor_SetColor(t *testing.T) {
	oe := NewOKLCHEditor(0, 0, 30, 15)

	color := tcell.NewRGBColor(255, 0, 0) // Red
	oe.SetColor(color)

	// Get color should return similar color (may have small rounding)
	result := oe.GetColor()
	r, g, b := result.RGB()

	// Allow small tolerance due to color space conversion
	if r < 250 || g > 10 || b > 10 {
		t.Errorf("GetColor = RGB(%d,%d,%d), expected approximately (255,0,0)", r, g, b)
	}
}

func TestOKLCHEditor_GetSource(t *testing.T) {
	oe := NewOKLCHEditor(0, 0, 30, 15)

	source := oe.GetSource()
	if len(source) == 0 {
		t.Error("GetSource should return non-empty string")
	}

	// Should start with "oklch("
	if source[:6] != "oklch(" {
		t.Errorf("GetSource = %q, should start with 'oklch('", source)
	}
}

func TestOKLCHEditor_FocusCycling(t *testing.T) {
	oe := NewOKLCHEditor(0, 0, 30, 15)
	oe.Focus()

	// Initial focus on plane
	if oe.focus != OKLCHFocusPlane {
		t.Error("initial focus should be on plane")
	}

	// CycleFocus forward should move to slider
	if !oe.CycleFocus(true) {
		t.Error("CycleFocus(true) should return true from plane")
	}
	if oe.focus != OKLCHFocusSlider {
		t.Error("focus should be on slider after forward cycle")
	}

	// CycleFocus forward again should return false (at boundary)
	if oe.CycleFocus(true) {
		t.Error("CycleFocus(true) should return false at slider boundary")
	}

	// CycleFocus backward should return to plane
	if !oe.CycleFocus(false) {
		t.Error("CycleFocus(false) should return true from slider")
	}
	if oe.focus != OKLCHFocusPlane {
		t.Error("focus should be on plane after backward cycle")
	}

	// CycleFocus backward again should return false (at boundary)
	if oe.CycleFocus(false) {
		t.Error("CycleFocus(false) should return false at plane boundary")
	}
}

func TestOKLCHEditor_TabNavigation(t *testing.T) {
	oe := NewOKLCHEditor(0, 0, 30, 15)
	oe.Focus()

	// Tab should NOT be handled by HandleKey - parent uses CycleFocus
	ev := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	handled := oe.HandleKey(ev)
	if handled {
		t.Error("Tab should NOT be handled by HandleKey - parent uses CycleFocus")
	}

	// Use CycleFocus to move from plane to slider
	if !oe.CycleFocus(true) {
		t.Error("CycleFocus(true) should move from plane to slider")
	}
	if oe.focus != OKLCHFocusSlider {
		t.Error("focus should be on slider after CycleFocus(true)")
	}

	// CycleFocus backward from slider to plane
	if !oe.CycleFocus(false) {
		t.Error("CycleFocus(false) should move from slider to plane")
	}
	if oe.focus != OKLCHFocusPlane {
		t.Error("focus should be on plane after CycleFocus(false)")
	}
}

func TestOKLCHEditor_MouseClickFocuses(t *testing.T) {
	oe := NewOKLCHEditor(0, 0, 30, 15)
	oe.Focus()

	// Click on plane first to verify current focus
	planeX, planeY := oe.hcPlane.Position()
	ev := tcell.NewEventMouse(planeX+1, planeY+1, tcell.Button1, tcell.ModNone)
	handled := oe.HandleMouse(ev)
	if !handled {
		t.Error("mouse click on plane should be handled")
	}
	if oe.focus != OKLCHFocusPlane {
		t.Error("focus should be on plane after clicking on it")
	}

	// Now Tab to slider and verify focus changes
	oe.CycleFocus(true)
	if oe.focus != OKLCHFocusSlider {
		t.Error("focus should be on slider after cycling")
	}
}

func TestOKLCHEditor_OnChangeCallback(t *testing.T) {
	oe := NewOKLCHEditor(0, 0, 30, 15)

	changeCount := 0
	oe.OnChange = func(c tcell.Color) {
		changeCount++
	}

	// Move cursor in HCPlane to trigger change - need to be within bounds
	oe.Focus()

	// Start with H at a position where we can move right
	oe.hcPlane.SetHC(180, 0.2) // Middle position

	ev := tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)
	handled := oe.HandleKey(ev)

	if !handled {
		t.Error("Right key should be handled")
	}
	if changeCount != 1 {
		t.Errorf("OnChange called %d times, want 1", changeCount)
	}
}

func TestOKLCHEditor_ResetFocus(t *testing.T) {
	oe := NewOKLCHEditor(0, 0, 30, 15)
	oe.focus = OKLCHFocusSlider

	oe.ResetFocus()

	if oe.focus != OKLCHFocusPlane {
		t.Error("ResetFocus should set focus to plane")
	}
}

func TestOKLCHEditor_TrapsFocus(t *testing.T) {
	oe := NewOKLCHEditor(0, 0, 30, 15)

	if oe.TrapsFocus() {
		t.Error("OKLCHEditor should not trap focus")
	}
}

func TestOKLCHEditor_VisitChildren(t *testing.T) {
	oe := NewOKLCHEditor(0, 0, 30, 15)

	count := 0
	oe.VisitChildren(func(w core.Widget) {
		count++
	})

	if count != 2 {
		t.Errorf("VisitChildren found %d children, want 2", count)
	}
}

// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package primitives

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestLightnessSlider_NewWithDefaults(t *testing.T) {
	s := NewLightnessSlider(10, 20, 3, 10)

	x, y := s.Position()
	if x != 10 || y != 20 {
		t.Errorf("position = (%d, %d), want (10, 20)", x, y)
	}

	w, h := s.Size()
	if w != 3 || h != 10 {
		t.Errorf("size = (%d, %d), want (3, 10)", w, h)
	}

	if !s.Focusable() {
		t.Error("LightnessSlider should be focusable")
	}

	// Default values
	if s.L != 0.7 {
		t.Errorf("L = %f, want 0.7", s.L)
	}
}

func TestLightnessSlider_SetLightness(t *testing.T) {
	s := NewLightnessSlider(0, 0, 3, 10)

	changeCount := 0
	s.OnChange = func(l float64) {
		changeCount++
	}

	s.SetLightness(0.5)
	if s.L != 0.5 {
		t.Errorf("L = %f, want 0.5", s.L)
	}
	if changeCount != 1 {
		t.Errorf("OnChange called %d times, want 1", changeCount)
	}

	// Setting same value should not trigger change
	s.SetLightness(0.5)
	if changeCount != 1 {
		t.Errorf("OnChange called %d times after same value, want 1", changeCount)
	}

	// Clamp to valid range
	s.SetLightness(-0.1)
	if s.L != 0 {
		t.Errorf("L = %f, want 0 (clamped)", s.L)
	}

	s.SetLightness(1.5)
	if s.L != 1 {
		t.Errorf("L = %f, want 1 (clamped)", s.L)
	}
}

func TestLightnessSlider_SetHC(t *testing.T) {
	s := NewLightnessSlider(0, 0, 3, 10)

	s.SetHC(180, 0.2)

	if s.H != 180 {
		t.Errorf("H = %f, want 180", s.H)
	}
	if s.C != 0.2 {
		t.Errorf("C = %f, want 0.2", s.C)
	}
}

func TestLightnessSlider_KeyNavigation(t *testing.T) {
	s := NewLightnessSlider(0, 0, 3, 10)
	s.L = 0.5 // Start in middle

	changeCount := 0
	s.OnChange = func(l float64) {
		changeCount++
	}

	// Move up (increase lightness)
	ev := tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	handled := s.HandleKey(ev)
	if !handled {
		t.Error("Up key should be handled")
	}
	if s.L != 0.55 { // 0.5 + 0.05
		t.Errorf("L = %f, want 0.55 after up", s.L)
	}
	if changeCount != 1 {
		t.Errorf("OnChange called %d times, want 1", changeCount)
	}

	// Move down (decrease lightness)
	ev = tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	s.HandleKey(ev)
	if s.L != 0.5 { // 0.55 - 0.05
		t.Errorf("L = %f, want 0.5 after down", s.L)
	}

	// Fine control with Shift (1% increment)
	ev = tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModShift)
	s.HandleKey(ev)
	if s.L != 0.51 { // 0.5 + 0.01
		t.Errorf("L = %f, want 0.51 after shift+up", s.L)
	}

	// Home goes to max
	ev = tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone)
	s.HandleKey(ev)
	if s.L != 1.0 {
		t.Errorf("L = %f, want 1.0 after Home", s.L)
	}

	// End goes to min
	ev = tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
	s.HandleKey(ev)
	if s.L != 0.0 {
		t.Errorf("L = %f, want 0.0 after End", s.L)
	}
}

func TestLightnessSlider_Clamping(t *testing.T) {
	s := NewLightnessSlider(0, 0, 3, 10)
	s.L = 0.98

	// Should not exceed 1.0
	ev := tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	s.HandleKey(ev)
	if s.L != 1.0 {
		t.Errorf("L = %f, want 1.0 (clamped)", s.L)
	}

	s.L = 0.02
	ev = tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	s.HandleKey(ev)
	if s.L != 0.0 {
		t.Errorf("L = %f, want 0.0 (clamped)", s.L)
	}
}

func TestLightnessSlider_MouseClick(t *testing.T) {
	s := NewLightnessSlider(10, 10, 3, 10)

	changeCount := 0
	s.OnChange = func(l float64) {
		changeCount++
	}

	// Click at top (y=10) should give L close to 1.0
	ev := tcell.NewEventMouse(11, 10, tcell.Button1, tcell.ModNone)
	handled := s.HandleMouse(ev)
	if !handled {
		t.Error("click should be handled")
	}
	if s.L != 1.0 {
		t.Errorf("L = %f, want 1.0 after click at top", s.L)
	}
	if changeCount != 1 {
		t.Errorf("OnChange called %d times, want 1", changeCount)
	}

	// Click at bottom (y=19) should give L close to 0.0
	ev = tcell.NewEventMouse(11, 19, tcell.Button1, tcell.ModNone)
	s.HandleMouse(ev)
	if s.L != 0.0 {
		t.Errorf("L = %f, want 0.0 after click at bottom", s.L)
	}

	// Click in middle (y=14/15) should give L around 0.5
	ev = tcell.NewEventMouse(11, 14, tcell.Button1, tcell.ModNone)
	s.HandleMouse(ev)
	// y=14, relY=4, H=10, L = 1 - 4/9 ≈ 0.556
	expectedL := 1.0 - float64(4)/float64(9)
	tolerance := 0.01
	if s.L < expectedL-tolerance || s.L > expectedL+tolerance {
		t.Errorf("L = %f, want approximately %f after click in middle", s.L, expectedL)
	}

	// Click outside should not be handled
	ev = tcell.NewEventMouse(5, 15, tcell.Button1, tcell.ModNone)
	handled = s.HandleMouse(ev)
	if handled {
		t.Error("click outside should not be handled")
	}
}

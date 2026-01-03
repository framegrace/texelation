// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package primitives

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestHCPlane_NewWithDefaults(t *testing.T) {
	p := NewHCPlane(10, 20, 30, 15)

	x, y := p.Position()
	if x != 10 || y != 20 {
		t.Errorf("position = (%d, %d), want (10, 20)", x, y)
	}

	w, h := p.Size()
	if w != 30 || h != 15 {
		t.Errorf("size = (%d, %d), want (30, 15)", w, h)
	}

	if !p.Focusable() {
		t.Error("HCPlane should be focusable")
	}

	// Default values
	if p.L != 0.7 {
		t.Errorf("L = %f, want 0.7", p.L)
	}
	if p.H != 270 {
		t.Errorf("H = %f, want 270", p.H)
	}
	if p.C != 0.15 {
		t.Errorf("C = %f, want 0.15", p.C)
	}
}

func TestHCPlane_SetHC(t *testing.T) {
	p := NewHCPlane(0, 0, 20, 10)

	p.SetHC(180, 0.2)

	if p.H != 180 {
		t.Errorf("H = %f, want 180", p.H)
	}
	if p.C != 0.2 {
		t.Errorf("C = %f, want 0.2", p.C)
	}

	// Cursor should be updated
	// H=180 on a 20-wide plane: 180/360 * 19 = 9.5 ≈ 9
	expectedX := 9 // int(180.0 / 360.0 * 19)
	if p.cursorX != expectedX {
		t.Errorf("cursorX = %d, want %d", p.cursorX, expectedX)
	}
}

func TestHCPlane_SetLightness(t *testing.T) {
	p := NewHCPlane(0, 0, 20, 10)

	p.SetLightness(0.5)
	if p.L != 0.5 {
		t.Errorf("L = %f, want 0.5", p.L)
	}

	// Clamp to valid range
	p.SetLightness(-0.1)
	if p.L != 0 {
		t.Errorf("L = %f, want 0 (clamped)", p.L)
	}

	p.SetLightness(1.5)
	if p.L != 1 {
		t.Errorf("L = %f, want 1 (clamped)", p.L)
	}
}

func TestHCPlane_KeyNavigation(t *testing.T) {
	p := NewHCPlane(0, 0, 20, 10)
	p.SetHC(180, 0.2) // Start in middle

	startX, startY := p.cursorX, p.cursorY
	startH, startC := p.H, p.C

	// Move right
	ev := tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)
	handled := p.HandleKey(ev)
	if !handled {
		t.Error("Right key should be handled")
	}
	if p.cursorX != startX+1 {
		t.Errorf("cursorX = %d, want %d after right", p.cursorX, startX+1)
	}
	if p.H <= startH {
		t.Error("H should increase after moving right")
	}

	// Move down
	startY = p.cursorY
	startC = p.C
	ev = tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	p.HandleKey(ev)
	if p.cursorY != startY+1 {
		t.Errorf("cursorY = %d, want %d after down", p.cursorY, startY+1)
	}
	if p.C >= startC {
		t.Error("C should decrease after moving down")
	}

	// Fine control with Shift
	startH = p.H
	ev = tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModShift)
	p.HandleKey(ev)
	if p.H != startH+1 {
		t.Errorf("H = %f, want %f after shift+right (1° increment)", p.H, startH+1)
	}
}

func TestHCPlane_MouseClick(t *testing.T) {
	p := NewHCPlane(10, 10, 20, 10)

	changeCount := 0
	p.OnChange = func(h, c float64) {
		changeCount++
	}

	// Click in the middle of the plane
	ev := tcell.NewEventMouse(20, 15, tcell.Button1, tcell.ModNone)
	handled := p.HandleMouse(ev)

	if !handled {
		t.Error("click inside should be handled")
	}
	if p.cursorX != 10 { // 20 - 10 = 10
		t.Errorf("cursorX = %d, want 10", p.cursorX)
	}
	if p.cursorY != 5 { // 15 - 10 = 5
		t.Errorf("cursorY = %d, want 5", p.cursorY)
	}
	if changeCount != 1 {
		t.Errorf("OnChange called %d times, want 1", changeCount)
	}

	// Click outside should not be handled
	ev = tcell.NewEventMouse(5, 5, tcell.Button1, tcell.ModNone)
	handled = p.HandleMouse(ev)
	if handled {
		t.Error("click outside should not be handled")
	}
}

func TestHCPlane_Resize(t *testing.T) {
	p := NewHCPlane(0, 0, 20, 10)
	p.SetHC(180, 0.2) // Middle

	oldX, oldY := p.cursorX, p.cursorY

	// Resize larger
	p.Resize(40, 20)

	// Cursor should scale proportionally
	expectedX := oldX * (40 - 1) / (20 - 1)
	expectedY := oldY * (20 - 1) / (10 - 1)

	if p.cursorX != expectedX {
		t.Errorf("cursorX after resize = %d, want %d", p.cursorX, expectedX)
	}
	if p.cursorY != expectedY {
		t.Errorf("cursorY after resize = %d, want %d", p.cursorY, expectedY)
	}
}

func TestHCPlane_HomEnd(t *testing.T) {
	p := NewHCPlane(0, 0, 20, 10)
	p.cursorX = 10

	// Home goes to start
	ev := tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone)
	p.HandleKey(ev)
	if p.cursorX != 0 {
		t.Errorf("cursorX after Home = %d, want 0", p.cursorX)
	}

	// End goes to end
	ev = tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
	p.HandleKey(ev)
	if p.cursorX != 19 {
		t.Errorf("cursorX after End = %d, want 19", p.cursorX)
	}
}

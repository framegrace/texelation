// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package texelterm

import (
	"testing"
	"time"
)

func TestClickDetector_SingleClick(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)
	if got := cd.DetectClick(5, 10); got != SingleClick {
		t.Errorf("expected SingleClick, got %v", got)
	}
}

func TestClickDetector_DoubleClick(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)
	cd.DetectClick(5, 10)
	if got := cd.DetectClick(5, 10); got != DoubleClick {
		t.Errorf("expected DoubleClick, got %v", got)
	}
}

func TestClickDetector_TripleClick(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)
	cd.DetectClick(5, 10)
	cd.DetectClick(5, 10)
	if got := cd.DetectClick(5, 10); got != TripleClick {
		t.Errorf("expected TripleClick, got %v", got)
	}
}

func TestClickDetector_QuadrupleClick(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)
	cd.DetectClick(5, 10)
	cd.DetectClick(5, 10)
	cd.DetectClick(5, 10)
	if got := cd.DetectClick(5, 10); got != QuadrupleClick {
		t.Errorf("expected QuadrupleClick, got %v", got)
	}
}

func TestClickDetector_QuintupleClick(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)
	for i := 0; i < 4; i++ {
		cd.DetectClick(5, 10)
	}
	if got := cd.DetectClick(5, 10); got != QuintupleClick {
		t.Errorf("expected QuintupleClick, got %v", got)
	}
}

func TestClickDetector_SaturatesAtMax(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)
	for i := 0; i < int(MaxClickType); i++ {
		cd.DetectClick(5, 10)
	}
	// Sixth and seventh clicks must stay at QuintupleClick — no
	// rollover to SingleClick. This is the user-visible difference
	// from the old behaviour: holding-and-clicking past the largest
	// gesture must not silently flip you back to char selection.
	if got := cd.DetectClick(5, 10); got != QuintupleClick {
		t.Errorf("6th click: expected QuintupleClick, got %v", got)
	}
	if got := cd.DetectClick(5, 10); got != QuintupleClick {
		t.Errorf("7th click: expected QuintupleClick, got %v", got)
	}
}

func TestClickDetector_TimeoutResetsCount(t *testing.T) {
	cd := NewClickDetector(50 * time.Millisecond)
	cd.DetectClick(5, 10)
	time.Sleep(100 * time.Millisecond)
	if got := cd.DetectClick(5, 10); got != SingleClick {
		t.Errorf("expected SingleClick after timeout, got %v", got)
	}
}

func TestClickDetector_PositionChangeResetsCount(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)
	cd.DetectClick(5, 10)
	if got := cd.DetectClick(5, 11); got != SingleClick {
		t.Errorf("expected SingleClick at new col, got %v", got)
	}
}

func TestClickDetector_LineChangeResetsCount(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)
	cd.DetectClick(5, 10)
	if got := cd.DetectClick(6, 10); got != SingleClick {
		t.Errorf("expected SingleClick on new line, got %v", got)
	}
}

func TestClickDetector_Reset(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)
	cd.DetectClick(5, 10)
	cd.DetectClick(5, 10)
	cd.Reset()
	if got := cd.DetectClick(5, 10); got != SingleClick {
		t.Errorf("expected SingleClick after Reset(), got %v", got)
	}
}

func TestClickDetector_LastClickPosition(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)
	cd.DetectClick(5, 10)
	if line, col := cd.LastClickPosition(); line != 5 || col != 10 {
		t.Errorf("LastClickPosition()=(%d,%d), want (5,10)", line, col)
	}
}

func TestClickDetector_ClickCount(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)
	if got := cd.ClickCount(); got != 0 {
		t.Errorf("initial count=%d, want 0", got)
	}
	for i := 1; i <= int(MaxClickType); i++ {
		cd.DetectClick(5, 10)
		if got := cd.ClickCount(); got != i {
			t.Errorf("count after click %d=%d, want %d", i, got, i)
		}
	}
	// One past max stays saturated.
	cd.DetectClick(5, 10)
	if got := cd.ClickCount(); got != int(MaxClickType) {
		t.Errorf("saturated count=%d, want %d", got, MaxClickType)
	}
}

func TestClickDetector_MixedSequence(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)
	if got := cd.DetectClick(5, 10); got != SingleClick {
		t.Errorf("step 1: got %v, want SingleClick", got)
	}
	if got := cd.DetectClick(5, 10); got != DoubleClick {
		t.Errorf("step 2: got %v, want DoubleClick", got)
	}
	// Different position resets.
	if got := cd.DetectClick(7, 12); got != SingleClick {
		t.Errorf("step 3: got %v, want SingleClick at new pos", got)
	}
	for i := 0; i < 4; i++ {
		cd.DetectClick(7, 12)
	}
	if got := cd.ClickCount(); got != int(MaxClickType) {
		t.Errorf("after rapid 5x at (7,12): count=%d, want %d", got, MaxClickType)
	}
}

func TestClickType_Values(t *testing.T) {
	cases := []struct {
		ct   ClickType
		want int
	}{
		{SingleClick, 1},
		{DoubleClick, 2},
		{TripleClick, 3},
		{QuadrupleClick, 4},
		{QuintupleClick, 5},
	}
	for _, c := range cases {
		if int(c.ct) != c.want {
			t.Errorf("ClickType=%d, want %d", c.ct, c.want)
		}
	}
	if MaxClickType != QuintupleClick {
		t.Errorf("MaxClickType=%v, want QuintupleClick", MaxClickType)
	}
}

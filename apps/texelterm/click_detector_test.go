// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/click_detector_test.go
// Summary: Comprehensive tests for ClickDetector multi-click detection.

package texelterm

import (
	"testing"
	"time"
)

// TestClickDetector_SingleClick tests that a single click is detected correctly.
func TestClickDetector_SingleClick(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)

	clickType := cd.DetectClick(5, 10)
	if clickType != SingleClick {
		t.Errorf("expected SingleClick, got %v", clickType)
	}
}

// TestClickDetector_DoubleClick tests that two clicks in same position are detected as double-click.
func TestClickDetector_DoubleClick(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)

	cd.DetectClick(5, 10)
	clickType := cd.DetectClick(5, 10)
	if clickType != DoubleClick {
		t.Errorf("expected DoubleClick, got %v", clickType)
	}
}

// TestClickDetector_TripleClick tests that three clicks are detected as triple-click.
func TestClickDetector_TripleClick(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)

	cd.DetectClick(5, 10)
	cd.DetectClick(5, 10)
	clickType := cd.DetectClick(5, 10)
	if clickType != TripleClick {
		t.Errorf("expected TripleClick, got %v", clickType)
	}
}

// TestClickDetector_CycleAfterTriple tests that clicks cycle back to single after triple.
func TestClickDetector_CycleAfterTriple(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)

	// First cycle
	if ct := cd.DetectClick(5, 10); ct != SingleClick {
		t.Errorf("click 1: expected SingleClick, got %v", ct)
	}
	if ct := cd.DetectClick(5, 10); ct != DoubleClick {
		t.Errorf("click 2: expected DoubleClick, got %v", ct)
	}
	if ct := cd.DetectClick(5, 10); ct != TripleClick {
		t.Errorf("click 3: expected TripleClick, got %v", ct)
	}

	// Fourth click should reset to single
	if ct := cd.DetectClick(5, 10); ct != SingleClick {
		t.Errorf("click 4: expected SingleClick after cycle, got %v", ct)
	}
}

// TestClickDetector_TimeoutResetsCount tests that clicks separated by timeout reset to single.
func TestClickDetector_TimeoutResetsCount(t *testing.T) {
	cd := NewClickDetector(50 * time.Millisecond) // Short timeout for testing

	cd.DetectClick(5, 10) // Single

	// Wait for timeout
	time.Sleep(100 * time.Millisecond)

	// Should be single again after timeout
	clickType := cd.DetectClick(5, 10)
	if clickType != SingleClick {
		t.Errorf("expected SingleClick after timeout, got %v", clickType)
	}
}

// TestClickDetector_PositionChangeResetsCount tests that clicking different position resets count.
func TestClickDetector_PositionChangeResetsCount(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)

	cd.DetectClick(5, 10)

	// Different position should reset
	clickType := cd.DetectClick(5, 11) // Different column
	if clickType != SingleClick {
		t.Errorf("expected SingleClick at new position, got %v", clickType)
	}
}

// TestClickDetector_LineChangeResetsCount tests that clicking different line resets count.
func TestClickDetector_LineChangeResetsCount(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)

	cd.DetectClick(5, 10)

	// Different line should reset
	clickType := cd.DetectClick(6, 10) // Different line
	if clickType != SingleClick {
		t.Errorf("expected SingleClick on new line, got %v", clickType)
	}
}

// TestClickDetector_Reset tests that Reset() clears the state.
func TestClickDetector_Reset(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)

	cd.DetectClick(5, 10)
	cd.DetectClick(5, 10) // Double click

	cd.Reset()

	// Should be single again after reset
	clickType := cd.DetectClick(5, 10)
	if clickType != SingleClick {
		t.Errorf("expected SingleClick after Reset(), got %v", clickType)
	}
}

// TestClickDetector_LastClickPosition tests position tracking.
func TestClickDetector_LastClickPosition(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)

	cd.DetectClick(5, 10)

	line, col := cd.LastClickPosition()
	if line != 5 || col != 10 {
		t.Errorf("LastClickPosition() = (%d, %d), want (5, 10)", line, col)
	}
}

// TestClickDetector_LastClickTime tests time tracking.
func TestClickDetector_LastClickTime(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)

	before := time.Now()
	cd.DetectClick(5, 10)
	after := time.Now()

	clickTime := cd.LastClickTime()
	if clickTime.Before(before) || clickTime.After(after) {
		t.Errorf("LastClickTime() not in expected range")
	}
}

// TestClickDetector_ClickCount tests click count tracking.
func TestClickDetector_ClickCount(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)

	// Initially zero
	if count := cd.ClickCount(); count != 0 {
		t.Errorf("expected initial count 0, got %d", count)
	}

	cd.DetectClick(5, 10)
	if count := cd.ClickCount(); count != 1 {
		t.Errorf("expected count 1 after single click, got %d", count)
	}

	cd.DetectClick(5, 10)
	if count := cd.ClickCount(); count != 2 {
		t.Errorf("expected count 2 after double click, got %d", count)
	}

	cd.DetectClick(5, 10)
	if count := cd.ClickCount(); count != 3 {
		t.Errorf("expected count 3 after triple click, got %d", count)
	}

	// After fourth click, count should be 1
	cd.DetectClick(5, 10)
	if count := cd.ClickCount(); count != 1 {
		t.Errorf("expected count 1 after cycle, got %d", count)
	}
}

// TestClickDetector_ResetClearsAll tests that Reset clears all state.
func TestClickDetector_ResetClearsAll(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)

	cd.DetectClick(5, 10)
	cd.DetectClick(5, 10)

	cd.Reset()

	if count := cd.ClickCount(); count != 0 {
		t.Errorf("expected count 0 after Reset, got %d", count)
	}

	line, col := cd.LastClickPosition()
	if line != 0 || col != 0 {
		t.Errorf("LastClickPosition() after Reset = (%d, %d), want (0, 0)", line, col)
	}

	if !cd.LastClickTime().IsZero() {
		t.Error("expected zero time after Reset")
	}
}

// TestClickDetector_DefaultTimeout tests using the default timeout constant.
func TestClickDetector_DefaultTimeout(t *testing.T) {
	cd := NewClickDetector(DefaultMultiClickTimeout)

	// Just verify it works with the default
	ct := cd.DetectClick(5, 10)
	if ct != SingleClick {
		t.Errorf("expected SingleClick, got %v", ct)
	}

	ct = cd.DetectClick(5, 10)
	if ct != DoubleClick {
		t.Errorf("expected DoubleClick, got %v", ct)
	}
}

// TestClickDetector_BoundaryPositions tests detection at edge positions.
func TestClickDetector_BoundaryPositions(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)

	// Position (0, 0)
	ct := cd.DetectClick(0, 0)
	if ct != SingleClick {
		t.Errorf("expected SingleClick at (0,0), got %v", ct)
	}

	ct = cd.DetectClick(0, 0)
	if ct != DoubleClick {
		t.Errorf("expected DoubleClick at (0,0), got %v", ct)
	}

	// Large positions
	cd.Reset()
	cd.DetectClick(10000, 5000)
	ct = cd.DetectClick(10000, 5000)
	if ct != DoubleClick {
		t.Errorf("expected DoubleClick at large position, got %v", ct)
	}
}

// TestClickDetector_MixedSequence tests a realistic mixed sequence.
func TestClickDetector_MixedSequence(t *testing.T) {
	cd := NewClickDetector(500 * time.Millisecond)

	// Click at (5, 10) - single
	if ct := cd.DetectClick(5, 10); ct != SingleClick {
		t.Errorf("step 1: expected SingleClick, got %v", ct)
	}

	// Click at (5, 10) - double
	if ct := cd.DetectClick(5, 10); ct != DoubleClick {
		t.Errorf("step 2: expected DoubleClick, got %v", ct)
	}

	// Click at different position - single (resets)
	if ct := cd.DetectClick(7, 12); ct != SingleClick {
		t.Errorf("step 3: expected SingleClick at new pos, got %v", ct)
	}

	// Click at same (7, 12) - double
	if ct := cd.DetectClick(7, 12); ct != DoubleClick {
		t.Errorf("step 4: expected DoubleClick, got %v", ct)
	}

	// Click at (7, 12) - triple
	if ct := cd.DetectClick(7, 12); ct != TripleClick {
		t.Errorf("step 5: expected TripleClick, got %v", ct)
	}

	// Click at (7, 12) - back to single (cycle)
	if ct := cd.DetectClick(7, 12); ct != SingleClick {
		t.Errorf("step 6: expected SingleClick after cycle, got %v", ct)
	}
}

// TestClickType_Values tests the ClickType constants.
func TestClickType_Values(t *testing.T) {
	if SingleClick != 1 {
		t.Errorf("SingleClick should be 1, got %d", SingleClick)
	}
	if DoubleClick != 2 {
		t.Errorf("DoubleClick should be 2, got %d", DoubleClick)
	}
	if TripleClick != 3 {
		t.Errorf("TripleClick should be 3, got %d", TripleClick)
	}
}

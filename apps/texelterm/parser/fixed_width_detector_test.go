// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import "testing"

func TestFixedWidthDetector_DefaultConfig(t *testing.T) {
	config := DefaultFixedWidthDetectorConfig()

	if config.JumpThreshold != 2 {
		t.Errorf("JumpThreshold = %d, want 2", config.JumpThreshold)
	}
	if config.MinJumpDistance != 2 {
		t.Errorf("MinJumpDistance = %d, want 2", config.MinJumpDistance)
	}
	if !config.FlagOnScrollRegion {
		t.Error("FlagOnScrollRegion should be true by default")
	}
	if !config.FlagOnCursorJumps {
		t.Error("FlagOnCursorJumps should be true by default")
	}
}

func TestFixedWidthDetector_ScrollRegion(t *testing.T) {
	mb := NewMemoryBuffer(DefaultMemoryBufferConfig())
	mb.SetTermWidth(80)
	d := NewFixedWidthDetector(mb)

	// Full-screen scroll region should NOT activate TUI mode
	d.OnScrollRegionSet(0, 23, 24)
	if d.InScrollRegion() {
		t.Error("Full-screen scroll region should not activate TUI mode")
	}
	if d.IsInTUIMode() {
		t.Error("IsInTUIMode should be false for full-screen region")
	}

	// Non-full-screen scroll region SHOULD activate TUI mode
	d.OnScrollRegionSet(2, 20, 24) // Top 2 rows reserved
	if !d.InScrollRegion() {
		t.Error("Non-full-screen scroll region should activate TUI mode")
	}
	if !d.IsInTUIMode() {
		t.Error("IsInTUIMode should be true for non-full-screen region")
	}
	if d.SignalCount() != 1 {
		t.Errorf("SignalCount = %d, want 1", d.SignalCount())
	}

	// Clear scroll region
	d.OnScrollRegionClear()
	if d.InScrollRegion() {
		t.Error("InScrollRegion should be false after clear")
	}
}

func TestFixedWidthDetector_WriteInScrollRegion(t *testing.T) {
	mb := NewMemoryBuffer(DefaultMemoryBufferConfig())
	mb.SetTermWidth(80)
	d := NewFixedWidthDetector(mb)

	// Ensure lines exist
	line5 := mb.EnsureLine(5)
	line6 := mb.EnsureLine(6)

	// Write outside scroll region - should NOT flag
	d.OnWrite(5, 80)
	if line5.FixedWidth != 0 {
		t.Errorf("Line 5 FixedWidth = %d, want 0 (not in scroll region)", line5.FixedWidth)
	}

	// Enter non-full-screen scroll region
	d.OnScrollRegionSet(2, 20, 24)

	// Write inside scroll region - SHOULD flag
	d.OnWrite(6, 80)
	if line6.FixedWidth != 80 {
		t.Errorf("Line 6 FixedWidth = %d, want 80 (in scroll region)", line6.FixedWidth)
	}
}

func TestFixedWidthDetector_CursorJumps(t *testing.T) {
	mb := NewMemoryBuffer(DefaultMemoryBufferConfig())
	mb.SetTermWidth(80)
	d := NewFixedWidthDetector(mb)

	// Ensure some lines exist
	for i := int64(0); i < 25; i++ {
		mb.EnsureLine(i)
	}

	// Normal movement (1 row) - should not trigger
	d.OnCursorMove(0)
	d.OnCursorMove(1)
	if d.ConsecutiveJumps() != 0 {
		t.Errorf("ConsecutiveJumps = %d, want 0 after normal movement", d.ConsecutiveJumps())
	}

	// Large jump (5 rows)
	d.OnCursorMove(6)
	if d.ConsecutiveJumps() != 1 {
		t.Errorf("ConsecutiveJumps = %d, want 1 after first large jump", d.ConsecutiveJumps())
	}

	// Second large jump - should trigger TUI mode
	mb.SetCursor(15, 0) // Position cursor for flagging
	d.OnCursorMove(15)
	if d.ConsecutiveJumps() != 2 {
		t.Errorf("ConsecutiveJumps = %d, want 2 after second large jump", d.ConsecutiveJumps())
	}
	if !d.IsInTUIMode() {
		t.Error("IsInTUIMode should be true after threshold reached")
	}

	// Line should be flagged
	line15 := mb.GetLine(15)
	if line15 == nil {
		t.Fatal("Line 15 should exist")
	}
	if line15.FixedWidth != 80 {
		t.Errorf("Line 15 FixedWidth = %d, want 80 (flagged by cursor jump)", line15.FixedWidth)
	}

	// Single-line movement does NOT reset jump counter (only same-position does)
	d.OnCursorMove(16)
	if d.ConsecutiveJumps() != 2 {
		t.Errorf("ConsecutiveJumps = %d, want 2 (single-line movement preserves count)", d.ConsecutiveJumps())
	}

	// Same-position movement DOES reset jump counter
	d.OnCursorMove(16)
	if d.ConsecutiveJumps() != 0 {
		t.Errorf("ConsecutiveJumps = %d, want 0 after same-position movement", d.ConsecutiveJumps())
	}
}

func TestFixedWidthDetector_NormalUsage(t *testing.T) {
	mb := NewMemoryBuffer(DefaultMemoryBufferConfig())
	mb.SetTermWidth(80)
	d := NewFixedWidthDetector(mb)

	// Simulate normal shell usage: sequential line movements
	for i := 0; i < 10; i++ {
		mb.EnsureLine(int64(i))
		d.OnCursorMove(i)
		d.OnWrite(int64(i), 80)
	}

	// No lines should be flagged as fixed-width
	for i := int64(0); i < 10; i++ {
		line := mb.GetLine(i)
		if line != nil && line.FixedWidth != 0 {
			t.Errorf("Line %d FixedWidth = %d, want 0 (normal shell usage)", i, line.FixedWidth)
		}
	}

	// Should not be in TUI mode
	if d.IsInTUIMode() {
		t.Error("Should not be in TUI mode during normal shell usage")
	}
}

func TestFixedWidthDetector_CursorVisibility(t *testing.T) {
	mb := NewMemoryBuffer(DefaultMemoryBufferConfig())
	d := NewFixedWidthDetector(mb)

	// Initial state
	if d.CursorHidden() {
		t.Error("Cursor should not be hidden initially")
	}

	// Hide cursor
	d.OnCursorVisibilityChange(true)
	if !d.CursorHidden() {
		t.Error("Cursor should be hidden after OnCursorVisibilityChange(true)")
	}
	if d.SignalCount() != 1 {
		t.Errorf("SignalCount = %d, want 1", d.SignalCount())
	}

	// Show cursor (no signal increment for showing)
	d.OnCursorVisibilityChange(false)
	if d.CursorHidden() {
		t.Error("Cursor should be visible after OnCursorVisibilityChange(false)")
	}
	if d.SignalCount() != 1 {
		t.Errorf("SignalCount = %d, want 1 (show doesn't increment)", d.SignalCount())
	}

	// Duplicate call should not increment
	d.OnCursorVisibilityChange(false)
	if d.SignalCount() != 1 {
		t.Errorf("SignalCount = %d, want 1 (duplicate call)", d.SignalCount())
	}
}

func TestFixedWidthDetector_Resize(t *testing.T) {
	mb := NewMemoryBuffer(DefaultMemoryBufferConfig())
	d := NewFixedWidthDetector(mb)

	// Initial dimensions
	if d.termWidth != DefaultWidth || d.termHeight != DefaultHeight {
		t.Errorf("Initial dimensions = (%d, %d), want (%d, %d)",
			d.termWidth, d.termHeight, DefaultWidth, DefaultHeight)
	}

	// Resize
	d.OnResize(120, 40)
	if d.termWidth != 120 || d.termHeight != 40 {
		t.Errorf("After resize = (%d, %d), want (120, 40)",
			d.termWidth, d.termHeight)
	}
}

func TestFixedWidthDetector_ManualControl(t *testing.T) {
	mb := NewMemoryBuffer(DefaultMemoryBufferConfig())
	d := NewFixedWidthDetector(mb)

	// Ensure line exists
	mb.EnsureLine(10)

	// Force flag
	d.ForceFixedWidth(10, 80)
	line := mb.GetLine(10)
	if line.FixedWidth != 80 {
		t.Errorf("FixedWidth = %d, want 80 after ForceFixedWidth", line.FixedWidth)
	}

	// Clear flag
	d.ClearFixedWidth(10)
	if line.FixedWidth != 0 {
		t.Errorf("FixedWidth = %d, want 0 after ClearFixedWidth", line.FixedWidth)
	}
}

func TestFixedWidthDetector_String(t *testing.T) {
	mb := NewMemoryBuffer(DefaultMemoryBufferConfig())
	d := NewFixedWidthDetector(mb)

	s := d.String()
	if s == "" {
		t.Error("String() should not be empty")
	}
	// Should contain key state info
	if !contains(s, "inScrollRegion=false") {
		t.Errorf("String() = %q, should contain 'inScrollRegion=false'", s)
	}
}

func TestFixedWidthDetector_MixedContent(t *testing.T) {
	mb := NewMemoryBuffer(DefaultMemoryBufferConfig())
	mb.SetTermWidth(80)
	d := NewFixedWidthDetector(mb)

	// Simulate mixed usage: shell, then TUI, then shell
	// 1. Normal shell usage (lines 0-4)
	for i := 0; i < 5; i++ {
		mb.EnsureLine(int64(i))
		d.OnCursorMove(i)
		d.OnWrite(int64(i), 80)
	}

	// 2. TUI app starts (sets scroll region)
	d.OnScrollRegionSet(2, 20, 24)

	// 3. TUI writes (lines 5-9 should be flagged)
	for i := 5; i < 10; i++ {
		mb.EnsureLine(int64(i))
		d.OnWrite(int64(i), 80)
	}

	// 4. TUI app exits
	d.OnScrollRegionClear()

	// 5. Normal shell usage resumes (lines 10-14)
	for i := 10; i < 15; i++ {
		mb.EnsureLine(int64(i))
		d.OnCursorMove(i)
		d.OnWrite(int64(i), 80)
	}

	// Verify flagging
	for i := int64(0); i < 5; i++ {
		line := mb.GetLine(i)
		if line != nil && line.FixedWidth != 0 {
			t.Errorf("Line %d should NOT be fixed (pre-TUI)", i)
		}
	}
	for i := int64(5); i < 10; i++ {
		line := mb.GetLine(i)
		if line == nil || line.FixedWidth != 80 {
			t.Errorf("Line %d SHOULD be fixed (TUI), got %v", i, line)
		}
	}
	for i := int64(10); i < 15; i++ {
		line := mb.GetLine(i)
		if line != nil && line.FixedWidth != 0 {
			t.Errorf("Line %d should NOT be fixed (post-TUI)", i)
		}
	}
}

func TestFixedWidthDetector_DisabledFlags(t *testing.T) {
	mb := NewMemoryBuffer(DefaultMemoryBufferConfig())
	mb.SetTermWidth(80)

	// Create detector with flagging disabled
	config := FixedWidthDetectorConfig{
		JumpThreshold:      2,
		MinJumpDistance:    2,
		FlagOnScrollRegion: false,
		FlagOnCursorJumps:  false,
	}
	d := NewFixedWidthDetectorWithConfig(mb, config)

	// Set up lines
	for i := int64(0); i < 20; i++ {
		mb.EnsureLine(i)
	}

	// Scroll region should not flag
	d.OnScrollRegionSet(2, 20, 24)
	d.OnWrite(5, 80)
	line5 := mb.GetLine(5)
	if line5.FixedWidth != 0 {
		t.Errorf("Line 5 FixedWidth = %d, want 0 (FlagOnScrollRegion=false)", line5.FixedWidth)
	}

	// Cursor jumps should not flag
	d.OnCursorMove(0)
	mb.SetCursor(10, 0)
	d.OnCursorMove(10)
	mb.SetCursor(20, 0)
	d.OnCursorMove(20) // Third large jump
	line20 := mb.GetLine(20)
	if line20 != nil && line20.FixedWidth != 0 {
		t.Errorf("Line 20 FixedWidth = %d, want 0 (FlagOnCursorJumps=false)", line20.FixedWidth)
	}
}

func TestFixedWidthDetector_NilMemoryBuffer(t *testing.T) {
	// Detector should handle nil MemoryBuffer gracefully
	d := NewFixedWidthDetector(nil)

	// These should not panic
	d.OnCursorMove(10)
	d.OnWrite(5, 80)
	d.OnScrollRegionSet(2, 20, 24)

	// Should be in TUI mode due to non-full-screen scroll region
	if !d.IsInTUIMode() {
		t.Error("IsInTUIMode should be true after scroll region set")
	}

	d.OnScrollRegionClear()

	// After clearing, should not be in TUI mode
	if d.IsInTUIMode() {
		t.Error("IsInTUIMode should be false after scroll region clear")
	}

	d.OnCursorVisibilityChange(true)
	d.OnResize(100, 50)
	d.ForceFixedWidth(5, 80)
	d.ClearFixedWidth(5)

	// String() should not panic
	_ = d.String()
}

// helper function
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

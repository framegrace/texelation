// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/freeze_tracker_test.go

package parser

import (
	"testing"
)

func TestFreezeTracker_MarkFrozen(t *testing.T) {
	ft := NewFreezeTracker()

	// Mark a range as frozen
	ft.MarkFrozen(10, 20)

	if !ft.IsFrozen(10) {
		t.Error("Expected line 10 to be frozen")
	}
	if !ft.IsFrozen(15) {
		t.Error("Expected line 15 to be frozen")
	}
	if !ft.IsFrozen(20) {
		t.Error("Expected line 20 to be frozen")
	}
	if ft.IsFrozen(9) {
		t.Error("Expected line 9 to NOT be frozen")
	}
	if ft.IsFrozen(21) {
		t.Error("Expected line 21 to NOT be frozen")
	}
}

func TestFreezeTracker_FrozenCount(t *testing.T) {
	ft := NewFreezeTracker()

	ft.MarkFrozen(10, 20) // 11 lines
	ft.MarkFrozen(30, 34) // 5 lines

	count := ft.FrozenCount()
	if count != 16 {
		t.Errorf("Expected 16 frozen lines, got %d", count)
	}
}

func TestFreezeTracker_Compact(t *testing.T) {
	ft := NewFreezeTracker()

	// Add overlapping and adjacent ranges
	ft.MarkFrozen(10, 15)
	ft.MarkFrozen(16, 20) // Adjacent to previous
	ft.MarkFrozen(18, 25) // Overlaps with previous

	// Should be compacted into single range [10, 25]
	ranges := ft.GetRanges()
	if len(ranges) != 1 {
		t.Errorf("Expected 1 compacted range, got %d: %v", len(ranges), ranges)
		return
	}
	if ranges[0].Start != 10 || ranges[0].End != 25 {
		t.Errorf("Expected [10, 25], got [%d, %d]", ranges[0].Start, ranges[0].End)
	}
}

func TestFreezeTracker_Adjust(t *testing.T) {
	ft := NewFreezeTracker()

	ft.MarkFrozen(10, 20)
	ft.MarkFrozen(30, 40)

	// Shift all ranges down by 5
	ft.Adjust(5)

	ranges := ft.GetRanges()
	if len(ranges) != 2 {
		t.Errorf("Expected 2 ranges, got %d", len(ranges))
		return
	}
	if ranges[0].Start != 5 || ranges[0].End != 15 {
		t.Errorf("Expected first range [5, 15], got [%d, %d]", ranges[0].Start, ranges[0].End)
	}
	if ranges[1].Start != 25 || ranges[1].End != 35 {
		t.Errorf("Expected second range [25, 35], got [%d, %d]", ranges[1].Start, ranges[1].End)
	}
}

func TestFreezeTracker_AdjustRemovesNegativeRanges(t *testing.T) {
	ft := NewFreezeTracker()

	ft.MarkFrozen(5, 10)
	ft.MarkFrozen(20, 25)

	// Shift by 15 - first range becomes negative
	ft.Adjust(15)

	ranges := ft.GetRanges()
	if len(ranges) != 1 {
		t.Errorf("Expected 1 range (first removed), got %d: %v", len(ranges), ranges)
		return
	}
	if ranges[0].Start != 5 || ranges[0].End != 10 {
		t.Errorf("Expected [5, 10], got [%d, %d]", ranges[0].Start, ranges[0].End)
	}
}

func TestFreezeTracker_TruncateTo(t *testing.T) {
	ft := NewFreezeTracker()

	ft.MarkFrozen(10, 20)
	ft.MarkFrozen(30, 40)

	// Truncate at 35 - should cap second range
	ft.TruncateTo(35)

	ranges := ft.GetRanges()
	if len(ranges) != 2 {
		t.Errorf("Expected 2 ranges, got %d", len(ranges))
		return
	}
	if ranges[1].End != 34 {
		t.Errorf("Expected second range capped at 34, got %d", ranges[1].End)
	}

	// Truncate at 25 - should remove second range
	ft.TruncateTo(25)
	ranges = ft.GetRanges()
	if len(ranges) != 1 {
		t.Errorf("Expected 1 range after truncation, got %d: %v", len(ranges), ranges)
	}
}

func TestFreezeTracker_Clear(t *testing.T) {
	ft := NewFreezeTracker()

	ft.MarkFrozen(10, 20)
	ft.Clear()

	if ft.IsFrozen(15) {
		t.Error("Expected no frozen lines after Clear")
	}
	if ft.FrozenCount() != 0 {
		t.Error("Expected 0 frozen count after Clear")
	}
}

func TestFreezeTracker_LastFrozenEnd(t *testing.T) {
	ft := NewFreezeTracker()

	if ft.LastFrozenEnd() != -1 {
		t.Error("Expected -1 for empty tracker")
	}

	ft.MarkFrozen(10, 20)
	if ft.LastFrozenEnd() != 20 {
		t.Errorf("Expected 20, got %d", ft.LastFrozenEnd())
	}

	ft.MarkFrozen(30, 40)
	if ft.LastFrozenEnd() != 40 {
		t.Errorf("Expected 40, got %d", ft.LastFrozenEnd())
	}
}

func TestFreezeTracker_FirstFrozenStart(t *testing.T) {
	ft := NewFreezeTracker()

	if ft.FirstFrozenStart() != -1 {
		t.Error("Expected -1 for empty tracker")
	}

	ft.MarkFrozen(30, 40)
	ft.MarkFrozen(10, 20)

	if ft.FirstFrozenStart() != 10 {
		t.Errorf("Expected 10 (after sorting), got %d", ft.FirstFrozenStart())
	}
}

func TestFreezeTracker_InvalidRange(t *testing.T) {
	ft := NewFreezeTracker()

	// Invalid range (start > end) should be ignored
	ft.MarkFrozen(20, 10)

	if ft.RangeCount() != 0 {
		t.Error("Expected invalid range to be ignored")
	}
}

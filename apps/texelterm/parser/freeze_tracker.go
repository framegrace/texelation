// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/freeze_tracker.go
// Summary: Tracks frozen line ranges for TUI content preservation.
//
// FreezeTracker maintains metadata about which history lines were frozen
// (committed from TUI viewport) vs normally committed. This enables
// TruncateTo to selectively remove frozen lines while preserving normal
// history, and prevents duplicate freezing of the same content.

package parser

import (
	"sort"
	"sync"
)

// FreezeTracker tracks which history line ranges contain frozen TUI content.
// Frozen lines are those committed via CommitFrozenLines during TUI mode.
// Uses a compact range representation to minimize memory overhead.
type FreezeTracker struct {
	// ranges stores inclusive [Start, End] pairs of frozen line ranges.
	// Sorted by Start, non-overlapping after Compact().
	ranges []FrozenRange
	mu     sync.RWMutex
}

// FrozenRange represents a contiguous range of frozen lines.
type FrozenRange struct {
	Start int64 // Global history line index (inclusive)
	End   int64 // Global history line index (inclusive)
}

// NewFreezeTracker creates a new freeze tracker.
func NewFreezeTracker() *FreezeTracker {
	return &FreezeTracker{
		ranges: make([]FrozenRange, 0),
	}
}

// MarkFrozen records that lines [start, end] are frozen.
// Ranges are merged with existing overlapping/adjacent ranges.
func (f *FreezeTracker) MarkFrozen(start, end int64) {
	if start > end {
		return // Invalid range
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.ranges = append(f.ranges, FrozenRange{Start: start, End: end})
	f.compactLocked()
}

// IsFrozen returns true if the given line index is within a frozen range.
// Uses binary search for O(log n) lookup.
func (f *FreezeTracker) IsFrozen(lineIndex int64) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Binary search for a range that might contain lineIndex
	i := sort.Search(len(f.ranges), func(i int) bool {
		return f.ranges[i].End >= lineIndex
	})

	if i < len(f.ranges) && f.ranges[i].Start <= lineIndex {
		return true
	}
	return false
}

// FrozenCount returns the total number of frozen lines across all ranges.
func (f *FreezeTracker) FrozenCount() int64 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var count int64
	for _, r := range f.ranges {
		count += r.End - r.Start + 1
	}
	return count
}

// RangeCount returns the number of frozen ranges (for testing/debugging).
func (f *FreezeTracker) RangeCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.ranges)
}

// GetRanges returns a copy of all frozen ranges (for testing/debugging).
func (f *FreezeTracker) GetRanges() []FrozenRange {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make([]FrozenRange, len(f.ranges))
	copy(result, f.ranges)
	return result
}

// Compact merges overlapping and adjacent ranges.
// This is called automatically after MarkFrozen.
func (f *FreezeTracker) Compact() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.compactLocked()
}

// compactLocked merges ranges. Caller must hold the lock.
func (f *FreezeTracker) compactLocked() {
	if len(f.ranges) < 2 {
		return
	}

	// Sort by start position
	sort.Slice(f.ranges, func(i, j int) bool {
		return f.ranges[i].Start < f.ranges[j].Start
	})

	// Merge overlapping/adjacent ranges
	merged := make([]FrozenRange, 0, len(f.ranges))
	current := f.ranges[0]

	for i := 1; i < len(f.ranges); i++ {
		next := f.ranges[i]
		// Check if next overlaps or is adjacent to current
		// Adjacent means current.End + 1 == next.Start
		if next.Start <= current.End+1 {
			// Merge: extend current to include next
			if next.End > current.End {
				current.End = next.End
			}
		} else {
			// No overlap: save current, start new
			merged = append(merged, current)
			current = next
		}
	}
	merged = append(merged, current)

	f.ranges = merged
}

// Adjust shifts all range indices down by the given amount.
// Used after history truncation to keep indices valid.
// Ranges that fall entirely below 0 are removed.
func (f *FreezeTracker) Adjust(removedCount int64) {
	if removedCount <= 0 {
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Shift all ranges and filter out those that became invalid
	kept := f.ranges[:0]
	for _, r := range f.ranges {
		newStart := r.Start - removedCount
		newEnd := r.End - removedCount

		if newEnd < 0 {
			// Entire range is now below 0, drop it
			continue
		}
		if newStart < 0 {
			newStart = 0
		}

		kept = append(kept, FrozenRange{Start: newStart, End: newEnd})
	}
	f.ranges = kept
}

// Clear removes all tracked ranges.
func (f *FreezeTracker) Clear() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ranges = f.ranges[:0]
}

// TruncateTo removes all frozen range data for lines >= the given index.
// Used when history is truncated.
func (f *FreezeTracker) TruncateTo(newLen int64) {
	if newLen < 0 {
		newLen = 0
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Adjust ranges to cap at newLen-1
	kept := f.ranges[:0]
	for _, r := range f.ranges {
		if r.Start >= newLen {
			// Entire range is beyond truncation point, drop it
			continue
		}
		if r.End >= newLen {
			// Range extends beyond truncation, cap it
			r.End = newLen - 1
		}
		if r.End >= r.Start {
			kept = append(kept, r)
		}
	}
	f.ranges = kept
}

// LastFrozenEnd returns the end index of the last frozen range.
// Returns -1 if no frozen ranges exist.
func (f *FreezeTracker) LastFrozenEnd() int64 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if len(f.ranges) == 0 {
		return -1
	}
	return f.ranges[len(f.ranges)-1].End
}

// FirstFrozenStart returns the start index of the first frozen range.
// Returns -1 if no frozen ranges exist.
func (f *FreezeTracker) FirstFrozenStart() int64 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if len(f.ranges) == 0 {
		return -1
	}
	return f.ranges[0].Start
}

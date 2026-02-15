// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/viewport_physical_index.go
// Summary: PhysicalLineIndex maintains a cached index of physical line counts.
//
// Architecture:
//
//	PhysicalLineIndex provides O(1) total physical line count and O(log N)
//	physical-to-logical line mapping by maintaining per-line physical counts
//	and a prefix sum array.
//
//	Instead of calling WrapToWidth() (which allocates PhysicalLine slices),
//	it uses pure arithmetic: ceil(len(Cells) / width) for normal lines,
//	1 for fixed-width or empty lines. This is ~100x cheaper per line.
//
//	The index tracks content changes via ContentReader.ContentVersion()
//	and supports incremental updates for appends and evictions.

package parser

import "sort"

// PhysicalLineIndex maintains a cached mapping from logical lines to
// physical line counts, enabling O(1) total and O(log N) lookups.
type PhysicalLineIndex struct {
	// perLine stores the physical line count for each in-memory logical line.
	// Indexed by (globalIdx - baseOffset). int16 is safe: max physical lines
	// per logical line at width 1 would be len(Cells), but practical max is
	// ~10000 (100k char line at width 10). int16 max is 32767.
	perLine []int16

	// prefixSum[i] = sum of perLine[0..i-1]. Length = count + 1.
	// prefixSum[0] = 0, prefixSum[count] = cachedTotal.
	prefixSum []int64

	// cachedTotal is the sum of all perLine entries. Always kept in sync.
	cachedTotal int64

	// baseOffset is the global index of the first tracked line.
	baseOffset int64

	// count is the number of tracked lines.
	count int

	// width is the terminal width this index was built for.
	width int

	// contentVersion is the ContentReader.ContentVersion() at last full build.
	contentVersion int64

	// prefixDirty indicates the prefix sum needs rebuilding.
	prefixDirty bool

	// showOverlay tracks whether overlay mode is active for line counting.
	showOverlay bool

	// reader provides access to logical lines.
	reader ContentReader
}

// NewPhysicalLineIndex creates a new index for the given reader and width.
func NewPhysicalLineIndex(reader ContentReader, width int) *PhysicalLineIndex {
	return &PhysicalLineIndex{
		reader: reader,
		width:  width,
	}
}

// physicalLinesFor computes the number of physical lines a logical line
// produces at the given width. This is pure arithmetic — no allocations.
// Mirrors the logic in LogicalLine.ActiveWrapToWidth().
func physicalLinesFor(line *LogicalLine, width int, showOverlay bool) int {
	if line == nil {
		return 1
	}

	if !showOverlay {
		// Original mode: synthetic lines are hidden
		if line.Synthetic {
			return 0
		}
		if len(line.Cells) == 0 {
			return 1
		}
		if line.FixedWidth > 0 {
			return 1
		}
		return (len(line.Cells) + width - 1) / width
	}

	// Overlay mode: use overlay if present
	if line.Overlay != nil {
		// Overlay is always fixed-width
		return 1
	}

	// No overlay: fall back to original cells
	if len(line.Cells) == 0 {
		return 1
	}
	if line.FixedWidth > 0 {
		return 1
	}
	return (len(line.Cells) + width - 1) / width
}

// Build performs a full rebuild of the index from the content reader.
// This iterates all in-memory lines but does no allocations per line.
func (idx *PhysicalLineIndex) Build() {
	memOffset := idx.reader.MemoryBufferOffset()
	globalEnd := idx.reader.GlobalEnd()
	n := int(globalEnd - memOffset)

	// Resize perLine if needed
	if cap(idx.perLine) >= n {
		idx.perLine = idx.perLine[:n]
	} else {
		idx.perLine = make([]int16, n)
	}

	var total int64
	for i := range n {
		line := idx.reader.GetLine(memOffset + int64(i))
		c := int16(physicalLinesFor(line, idx.width, idx.showOverlay))
		idx.perLine[i] = c
		total += int64(c)
	}

	idx.cachedTotal = total
	idx.baseOffset = memOffset
	idx.count = n
	idx.contentVersion = idx.reader.ContentVersion()
	idx.prefixDirty = true
}

// TotalPhysicalLines returns the cached total in O(1).
func (idx *PhysicalLineIndex) TotalPhysicalLines() int64 {
	return idx.cachedTotal
}

// Width returns the width this index was built for.
func (idx *PhysicalLineIndex) Width() int {
	return idx.width
}

// ContentVersion returns the content version at last build.
func (idx *PhysicalLineIndex) ContentVersion() int64 {
	return idx.contentVersion
}

// BaseOffset returns the base offset of tracked lines.
func (idx *PhysicalLineIndex) BaseOffset() int64 {
	return idx.baseOffset
}

// Count returns the number of tracked lines.
func (idx *PhysicalLineIndex) Count() int {
	return idx.count
}

// Invalidate marks the index for a full rebuild (e.g., on width change).
func (idx *PhysicalLineIndex) Invalidate() {
	idx.contentVersion = -1
}

// HandleEviction adjusts the index when lines are evicted from memory.
// newBaseOffset is the new MemoryBufferOffset after eviction.
// evictedCount is the number of lines evicted from the front.
func (idx *PhysicalLineIndex) HandleEviction(newBaseOffset int64, evictedCount int) {
	if evictedCount <= 0 || evictedCount > idx.count {
		// Evicted everything or invalid — need full rebuild
		idx.Invalidate()
		return
	}

	// Subtract evicted counts from total
	for i := range evictedCount {
		idx.cachedTotal -= int64(idx.perLine[i])
	}

	// Shift perLine left
	remaining := idx.count - evictedCount
	copy(idx.perLine[:remaining], idx.perLine[evictedCount:])
	idx.perLine = idx.perLine[:remaining]

	idx.baseOffset = newBaseOffset
	idx.count = remaining
	idx.prefixDirty = true
}

// HandleAppend extends the index for newly appended lines.
// newEnd is the new GlobalEnd() after the append.
func (idx *PhysicalLineIndex) HandleAppend(newEnd int64) {
	oldEnd := idx.baseOffset + int64(idx.count)
	appendCount := int(newEnd - oldEnd)
	if appendCount <= 0 {
		return
	}

	// Grow perLine
	for i := range appendCount {
		globalIdx := oldEnd + int64(i)
		line := idx.reader.GetLine(globalIdx)
		c := int16(physicalLinesFor(line, idx.width, idx.showOverlay))

		idx.perLine = append(idx.perLine, c)
		idx.cachedTotal += int64(c)
	}

	idx.count += appendCount
	idx.prefixDirty = true
}

// ensurePrefixSum rebuilds the prefix sum array if dirty.
func (idx *PhysicalLineIndex) ensurePrefixSum() {
	if !idx.prefixDirty {
		return
	}

	n := idx.count
	needed := n + 1
	if cap(idx.prefixSum) >= needed {
		idx.prefixSum = idx.prefixSum[:needed]
	} else {
		idx.prefixSum = make([]int64, needed)
	}

	idx.prefixSum[0] = 0
	for i := range n {
		idx.prefixSum[i+1] = idx.prefixSum[i] + int64(idx.perLine[i])
	}

	idx.prefixDirty = false
}

// PhysicalToLogical converts a physical line index (relative to the start
// of in-memory content) to a logical line global index and the offset
// within that logical line.
// Uses binary search on the prefix sum array: O(log N).
func (idx *PhysicalLineIndex) PhysicalToLogical(physicalIdx int64) (logicalGlobalIdx int64, offsetInLogical int) {
	if idx.count == 0 {
		return idx.baseOffset, 0
	}

	idx.ensurePrefixSum()

	// Binary search: find the largest i such that prefixSum[i] <= physicalIdx
	// sort.Search returns the smallest i where f(i) is true.
	// We want: prefixSum[i+1] > physicalIdx, i.e. the line that contains physicalIdx.
	i := sort.Search(idx.count, func(j int) bool {
		return idx.prefixSum[j+1] > physicalIdx
	})

	if i >= idx.count {
		i = idx.count - 1
	}

	logicalGlobalIdx = idx.baseOffset + int64(i)
	offsetInLogical = int(physicalIdx - idx.prefixSum[i])
	return logicalGlobalIdx, offsetInLogical
}

// PhysicalCountFor returns the physical line count for a specific
// logical line (by global index). Returns 0 if out of range.
func (idx *PhysicalLineIndex) PhysicalCountFor(globalIdx int64) int {
	local := int(globalIdx - idx.baseOffset)
	if local < 0 || local >= idx.count {
		return 0
	}
	return int(idx.perLine[local])
}

// PrefixSumAt returns the cumulative physical line count up to (but not
// including) the logical line at localIdx. Used for range calculations.
func (idx *PhysicalLineIndex) PrefixSumAt(localIdx int) int64 {
	idx.ensurePrefixSum()
	if localIdx < 0 {
		return 0
	}
	if localIdx > idx.count {
		localIdx = idx.count
	}
	return idx.prefixSum[localIdx]
}

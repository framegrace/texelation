// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/viewport_content_reader.go
// Summary: ContentReader abstracts read access to terminal content.
//
// Architecture:
//
//	ContentReader is an interface that abstracts read access to terminal content.
//	This allows ViewportWindow to work with different storage backends
//	(MemoryBuffer for production, mocks for testing, potentially disk-backed
//	storage in the future).
//
//	The MemoryBufferReader is the production adapter that delegates all calls
//	to MemoryBuffer methods.

package parser

// blankLine returns an empty LogicalLine suitable for rendering gap positions.
func blankLine() *LogicalLine {
	return &LogicalLine{Cells: nil}
}

// ContentReader abstracts read access to terminal content.
// This interface allows ViewportWindow to work with different storage backends.
type ContentReader interface {
	// GetLine returns the logical line at the given global index.
	// Returns nil if the line doesn't exist or has been evicted.
	GetLine(globalIdx int64) *LogicalLine

	// GetLineRange returns lines from start (inclusive) to end (exclusive).
	// Skips lines that don't exist.
	GetLineRange(start, end int64) []*LogicalLine

	// GlobalEnd returns the global index just past the last line.
	// This is the "live edge" where new content appears.
	GlobalEnd() int64

	// GlobalOffset returns the global index of the oldest readable line.
	// With disk storage, this may return 0 (can read full history).
	GlobalOffset() int64

	// MemoryBufferOffset returns the oldest line currently in memory.
	// Use this for performance-sensitive calculations.
	MemoryBufferOffset() int64

	// DiskStoredLinesBelow returns the count of disk-resident lines whose
	// global index is strictly less than the given index. Used by scroll
	// math to count scrollable history without counting sparse gaps.
	DiskStoredLinesBelow(globalIdx int64) int64

	// DiskGlobalIdxAtPosition returns the globalIdx of the Nth stored
	// disk line (0-based). Used by scroll math to map a physical scroll
	// position to a real sparse globalIdx, skipping gap indices that
	// have no stored content. Returns -1 if pos is out of range.
	DiskGlobalIdxAtPosition(pos int64) int64

	// TotalLines returns the total number of lines currently in memory.
	TotalLines() int64

	// ContentVersion returns a monotonically increasing version number.
	// Changes whenever content is modified, used for cache invalidation.
	ContentVersion() int64
}

// MemoryBufferReader adapts MemoryBuffer to the ContentReader interface.
// This is the production adapter used by ViewportWindow.
//
// When a PageStore is provided, the reader falls back to disk for lines
// that have been evicted from memory. This enables reading the full history
// while keeping only recent lines in RAM.
type MemoryBufferReader struct {
	buffer    *MemoryBuffer
	pageStore *PageStore // optional fallback for evicted lines
}

// NewMemoryBufferReader creates a new reader wrapping the given MemoryBuffer.
func NewMemoryBufferReader(buffer *MemoryBuffer) *MemoryBufferReader {
	return &MemoryBufferReader{buffer: buffer}
}

// NewMemoryBufferReaderWithPageStore creates a reader with disk fallback.
// When lines are evicted from the MemoryBuffer, they can still be read
// from the PageStore. This enables ViewportWindow to display any line
// in the history, not just those currently in memory.
func NewMemoryBufferReaderWithPageStore(buffer *MemoryBuffer, pageStore *PageStore) *MemoryBufferReader {
	return &MemoryBufferReader{buffer: buffer, pageStore: pageStore}
}

// SetPageStore enables disk fallback for evicted lines.
// This can be called after construction to add PageStore support
// when the PageStore becomes available later in initialization.
func (r *MemoryBufferReader) SetPageStore(pageStore *PageStore) {
	r.pageStore = pageStore
}

// GetLine returns the logical line at the given global index.
// If the line has been evicted from memory and a PageStore is available,
// falls back to reading from disk.
func (r *MemoryBufferReader) GetLine(globalIdx int64) *LogicalLine {
	line := r.buffer.GetLine(globalIdx)
	if line != nil {
		return line
	}

	// Fallback to PageStore if available
	if r.pageStore != nil {
		line, _ = r.pageStore.ReadLine(globalIdx)
		return line
	}

	return nil
}

// GetLineRange returns lines from start (inclusive) to end (exclusive).
// Handles ranges that span both memory and disk storage.
//
// For the disk portion, only actually-stored lines are returned (sparse
// gaps are skipped, NOT padded with blank placeholders). This keeps the
// viewport renderer from materializing thousands of empty rows for the
// gap regions in the global-index space.
func (r *MemoryBufferReader) GetLineRange(start, end int64) []*LogicalLine {
	if r.pageStore == nil {
		// No PageStore, use memory buffer directly
		return r.buffer.GetLineRange(start, end)
	}

	// With PageStore, we may need to fetch from both sources
	memOffset := r.buffer.GlobalOffset()
	memEnd := r.buffer.GlobalEnd()

	// Case 1: Entire range is in memory
	if start >= memOffset && end <= memEnd {
		return r.buffer.GetLineRange(start, end)
	}

	// Case 2: Entire range is on disk (before memory)
	if end <= memOffset {
		lines, _ := r.pageStore.ReadLineRange(start, end)
		return compactNonNil(lines)
	}

	// Case 3: Range spans disk and memory
	result := make([]*LogicalLine, 0, end-start)

	// First, get lines from disk (if any) — compact, no nil padding.
	if start < memOffset {
		diskLines, _ := r.pageStore.ReadLineRange(start, memOffset)
		result = append(result, compactNonNil(diskLines)...)
	}

	// Then, get lines from memory
	memStart := start
	if memStart < memOffset {
		memStart = memOffset
	}
	if memStart < end {
		memLines := r.buffer.GetLineRange(memStart, end)
		result = append(result, memLines...)
	}

	return result
}

// compactNonNil returns the non-nil entries from lines, preserving order.
// Used to skip sparse gap positions in disk reads so the viewport renderer
// doesn't produce empty rows for indices that have no stored content.
func compactNonNil(lines []*LogicalLine) []*LogicalLine {
	out := make([]*LogicalLine, 0, len(lines))
	for _, line := range lines {
		if line != nil {
			out = append(out, line)
		}
	}
	return out
}

// GlobalEnd returns the global index just past the last line.
func (r *MemoryBufferReader) GlobalEnd() int64 {
	return r.buffer.GlobalEnd()
}

// GlobalOffset returns the global index of the oldest readable line.
// With PageStore, returns 0 (can read from the very beginning).
// Without PageStore, returns the memory buffer's offset.
func (r *MemoryBufferReader) GlobalOffset() int64 {
	if r.pageStore != nil {
		return 0
	}
	return r.buffer.GlobalOffset()
}

// MemoryBufferOffset returns the memory buffer's actual offset.
// Use this for performance-sensitive calculations that should only
// consider in-memory content (e.g., physical line counting).
func (r *MemoryBufferReader) MemoryBufferOffset() int64 {
	return r.buffer.GlobalOffset()
}

// DiskStoredLinesBelow returns the count of disk-resident lines whose
// global index is strictly less than the given index. Used by the scroll
// manager to compute scrollable history without counting sparse gaps.
// Without a PageStore, returns 0 (no disk content).
func (r *MemoryBufferReader) DiskStoredLinesBelow(globalIdx int64) int64 {
	if r.pageStore == nil {
		return 0
	}
	return r.pageStore.StoredLineCountBelow(globalIdx)
}

// DiskGlobalIdxAtPosition returns the globalIdx of the Nth stored disk
// line (0-based). Without a PageStore, returns -1.
func (r *MemoryBufferReader) DiskGlobalIdxAtPosition(pos int64) int64 {
	if r.pageStore == nil {
		return -1
	}
	return r.pageStore.GlobalIdxAtStoredPosition(pos)
}

// TotalLines returns the total number of lines currently in memory.
// Note: With PageStore, more lines exist on disk, but this returns the
// memory count to keep scroll calculations efficient.
func (r *MemoryBufferReader) TotalLines() int64 {
	return r.buffer.TotalLines()
}

// ContentVersion returns the current content version number.
func (r *MemoryBufferReader) ContentVersion() int64 {
	return r.buffer.ContentVersion()
}

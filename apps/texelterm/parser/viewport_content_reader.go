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

	// GlobalOffset returns the global index of the oldest line in memory.
	GlobalOffset() int64

	// TotalLines returns the total number of lines currently in memory.
	TotalLines() int64

	// ContentVersion returns a monotonically increasing version number.
	// Changes whenever content is modified, used for cache invalidation.
	ContentVersion() int64
}

// MemoryBufferReader adapts MemoryBuffer to the ContentReader interface.
// This is the production adapter used by ViewportWindow.
type MemoryBufferReader struct {
	buffer *MemoryBuffer
}

// NewMemoryBufferReader creates a new reader wrapping the given MemoryBuffer.
func NewMemoryBufferReader(buffer *MemoryBuffer) *MemoryBufferReader {
	return &MemoryBufferReader{buffer: buffer}
}

// GetLine returns the logical line at the given global index.
func (r *MemoryBufferReader) GetLine(globalIdx int64) *LogicalLine {
	return r.buffer.GetLine(globalIdx)
}

// GetLineRange returns lines from start (inclusive) to end (exclusive).
func (r *MemoryBufferReader) GetLineRange(start, end int64) []*LogicalLine {
	return r.buffer.GetLineRange(start, end)
}

// GlobalEnd returns the global index just past the last line.
func (r *MemoryBufferReader) GlobalEnd() int64 {
	return r.buffer.GlobalEnd()
}

// GlobalOffset returns the global index of the oldest line in memory.
func (r *MemoryBufferReader) GlobalOffset() int64 {
	return r.buffer.GlobalOffset()
}

// TotalLines returns the total number of lines currently in memory.
func (r *MemoryBufferReader) TotalLines() int64 {
	return r.buffer.TotalLines()
}

// ContentVersion returns the current content version number.
func (r *MemoryBufferReader) ContentVersion() int64 {
	return r.buffer.ContentVersion()
}

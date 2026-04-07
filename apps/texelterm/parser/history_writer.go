// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/history_writer.go
// Summary: Interface for disk persistence backends.
//
// HistoryWriter defines the interface that persistence backends must implement.
// PageStore implements this interface for page-based terminal history storage.

package parser

import "time"

// HistoryWriter is the interface for disk persistence backends.
// PageStore is the primary implementation using page-based storage.
type HistoryWriter interface {
	// AppendLineWithGlobalIdx writes a logical line at the given global index.
	// The global index must be >= LineCount() (append) or equal to an existing
	// index (overwrite via UpdateLine). Gaps between LineCount() and globalIdx
	// are allowed; those slots will read as nil.
	AppendLineWithGlobalIdx(globalIdx int64, line *LogicalLine, timestamp time.Time) error

	// ReadLine reads a single line by global index.
	// Returns nil if index is out of bounds.
	ReadLine(index int64) (*LogicalLine, error)

	// ReadLineRange reads a range of lines [start, end).
	// Returns a dense slice of length (end-start) with nil entries for gaps.
	ReadLineRange(start, end int64) ([]*LogicalLine, error)

	// LineCount returns the total number of lines stored (logical end index).
	LineCount() int64

	// Close finalizes storage and releases resources.
	Close() error

	// Path returns the storage path (file or directory).
	Path() string
}

// HistoryWriterWithTimestamp extends HistoryWriter with timestamp support.
// PageStore implements this interface.
type HistoryWriterWithTimestamp interface {
	HistoryWriter

	// ReadLineWithTimestamp reads a line and its timestamp.
	ReadLineWithTimestamp(index int64) (*LogicalLine, time.Time, error)

	// GetTimestamp returns the timestamp for a line by global index.
	GetTimestamp(index int64) (time.Time, error)

	// FindLineAt returns the global line index closest to the given time.
	// If exact match not found, returns the line just before the time.
	FindLineAt(t time.Time) (int64, error)
}

// Compile-time interface checks
var (
	_ HistoryWriter              = (*PageStore)(nil)
	_ HistoryWriterWithTimestamp = (*PageStore)(nil)
)

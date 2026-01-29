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
	// AppendLine writes a logical line to the storage.
	// Uses the current time as the timestamp.
	AppendLine(line *LogicalLine) error

	// ReadLine reads a single line by global index.
	// Returns nil if index is out of bounds.
	ReadLine(index int64) (*LogicalLine, error)

	// ReadLineRange reads a range of lines [start, end).
	// Returns lines that exist within the range.
	ReadLineRange(start, end int64) ([]*LogicalLine, error)

	// LineCount returns the total number of lines stored.
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

	// AppendLineWithTimestamp writes a line with an explicit timestamp.
	AppendLineWithTimestamp(line *LogicalLine, timestamp time.Time) error

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

// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/persist_mode.go
// Summary: PersistMode defines the persistence strategy for AdaptivePersistence.

package parser

// PersistMode represents the current persistence strategy.
type PersistMode int

const (
	// PersistWriteThrough writes immediately (normal shell usage, < 10 writes/sec)
	PersistWriteThrough PersistMode = iota

	// PersistDebounced batches writes with a time window (10-100 writes/sec)
	PersistDebounced

	// PersistBestEffort only flushes on idle or explicit request (> 100 writes/sec)
	PersistBestEffort
)

// String returns the mode name for debugging.
func (pm PersistMode) String() string {
	switch pm {
	case PersistWriteThrough:
		return "WriteThrough"
	case PersistDebounced:
		return "Debounced"
	case PersistBestEffort:
		return "BestEffort"
	default:
		return "Unknown"
	}
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/history_loader.go
// Summary: Implements HistoryLoader interface for on-demand loading from disk.

package parser

// indexedFileLoader implements HistoryLoader using an IndexedHistoryFile.
// This loads lines directly from disk on demand, without keeping them in memory.
type indexedFileLoader struct {
	file *IndexedHistoryFile

	// loadedUpTo tracks how many lines from the END of file have been loaded
	// into the ScrollbackHistory. Lines 0..totalLines-loadedUpTo-1 are still on disk.
	loadedUpTo int64

	// totalLines is the total number of lines in the file
	totalLines int64
}

// NewIndexedFileLoader creates a loader backed by an IndexedHistoryFile.
// initiallyLoaded is how many lines from the end have already been loaded into memory.
func NewIndexedFileLoader(file *IndexedHistoryFile, initiallyLoaded int64) HistoryLoader {
	if file == nil {
		return nil
	}
	return &indexedFileLoader{
		file:       file,
		loadedUpTo: initiallyLoaded,
		totalLines: file.LineCount(),
	}
}

// LoadLinesAbove loads up to 'count' logical lines that are older than what's
// currently loaded. Returns lines in chronological order (oldest first).
func (l *indexedFileLoader) LoadLinesAbove(count int) []*LogicalLine {
	if l.file == nil {
		return nil
	}

	// Calculate which lines to load
	// Lines available on disk: 0 to totalLines-loadedUpTo-1
	available := l.totalLines - l.loadedUpTo
	if available <= 0 {
		return nil
	}

	// Load from the end of the available range (most recent unloaded)
	toLoad := int64(min(int64(count), available))
	startIdx := available - toLoad
	endIdx := available

	// Read lines from disk
	lines, err := l.file.ReadLineRange(startIdx, endIdx)
	if err != nil {
		// Log error but don't crash
		return nil
	}

	// Update tracking
	l.loadedUpTo += toLoad

	return lines
}

// HasMoreAbove returns true if there's more history available on disk.
func (l *indexedFileLoader) HasMoreAbove() bool {
	if l.file == nil {
		return false
	}
	return l.loadedUpTo < l.totalLines
}

// TotalLines returns the total number of lines in history.
func (l *indexedFileLoader) TotalLines() int {
	return int(l.totalLines)
}

// historyManagerLoader implements HistoryLoader by pulling from a HistoryManager.
// This is a fallback for when indexed files aren't available.
type historyManagerLoader struct {
	hm *HistoryManager

	// loadedUpTo tracks how many lines from the END of history have been loaded
	// into the ScrollbackHistory. Lines 0..totalLines-loadedUpTo-1 are still available.
	loadedUpTo int

	// totalLines is the total number of lines in HistoryManager
	totalLines int
}

// NewHistoryManagerLoader creates a loader backed by a HistoryManager.
// initiallyLoaded is how many lines from the end have already been loaded.
func NewHistoryManagerLoader(hm *HistoryManager, initiallyLoaded int) HistoryLoader {
	if hm == nil {
		return nil
	}
	return &historyManagerLoader{
		hm:         hm,
		loadedUpTo: initiallyLoaded,
		totalLines: hm.Length(),
	}
}

// LoadLinesAbove loads up to 'count' logical lines that are older than what's
// currently loaded. Returns lines in chronological order (oldest first).
func (l *historyManagerLoader) LoadLinesAbove(count int) []*LogicalLine {
	if l.hm == nil {
		return nil
	}

	// Calculate which lines to load
	// Lines available: 0 to totalLines-loadedUpTo-1
	available := l.totalLines - l.loadedUpTo
	if available <= 0 {
		return nil
	}

	// Load from the end of the available range (most recent unloaded)
	toLoad := min(count, available)
	startIdx := available - toLoad
	endIdx := available

	// Extract physical lines from HistoryManager
	physicalLines := make([][]Cell, endIdx-startIdx)
	for i := startIdx; i < endIdx; i++ {
		physicalLines[i-startIdx] = l.hm.GetLine(i)
	}

	// Convert to logical lines
	logicalLines := ConvertPhysicalToLogical(physicalLines)

	// Update tracking
	l.loadedUpTo += toLoad

	return logicalLines
}

// HasMoreAbove returns true if there's more history available to load.
func (l *historyManagerLoader) HasMoreAbove() bool {
	if l.hm == nil {
		return false
	}
	return l.loadedUpTo < l.totalLines
}

// TotalLines returns the total number of lines in history.
func (l *historyManagerLoader) TotalLines() int {
	return l.totalLines
}

// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"sync"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// storeLine is the wrapper around a row of cells in the sparse Store.
// A missing map entry represents "no content at this globalIdx" — reads of
// missing globalIdxs return blank cells.
type storeLine struct {
	cells []parser.Cell
}

// Store is a sparse, globalIdx-keyed cell storage.
//
// A cell at globalIdx X is just a cell at globalIdx X. There is no viewport
// concept, no cursor, no scrollback/viewport distinction. Reads of unwritten
// globalIdxs return blank cells. Writes at arbitrary globalIdxs are allowed.
//
// Store is safe for concurrent use.
type Store struct {
	mu         sync.RWMutex
	width      int
	lines      map[int64]*storeLine
	contentEnd int64 // highest globalIdx ever written; -1 means empty
}

// NewStore creates an empty Store for a terminal of the given column width.
func NewStore(width int) *Store {
	return &Store{
		width:      width,
		lines:      make(map[int64]*storeLine),
		contentEnd: -1,
	}
}

// Width returns the column width the Store was created with.
// width is set in NewStore and never mutated, so no lock is needed.
func (s *Store) Width() int {
	return s.width
}

// Max returns the highest globalIdx ever written. Returns -1 if the Store
// has never been written to.
func (s *Store) Max() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.contentEnd
}

// Get returns the Cell at (globalIdx, col). Returns a zero-value Cell if the
// globalIdx has never been written to or if col is outside the line's current
// length.
func (s *Store) Get(globalIdx int64, col int) parser.Cell {
	s.mu.RLock()
	defer s.mu.RUnlock()
	line, ok := s.lines[globalIdx]
	if !ok {
		return parser.Cell{}
	}
	if col < 0 || col >= len(line.cells) {
		return parser.Cell{}
	}
	return line.cells[col]
}

// Set writes a single Cell at (globalIdx, col). The target line is
// automatically extended to cover col if it did not already.
func (s *Store) Set(globalIdx int64, col int, cell parser.Cell) {
	if col < 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	line, ok := s.lines[globalIdx]
	if !ok {
		line = &storeLine{}
		s.lines[globalIdx] = line
	}
	if col >= len(line.cells) {
		needed := col + 1
		newCap := cap(line.cells) * 2
		if newCap < needed {
			newCap = needed
		}
		// Safety clamp: prevent absurd allocations from buggy cursor state
		if newCap > s.width*4+16 {
			newCap = s.width*4 + 16
		}
		if needed > newCap {
			return
		}
		grown := make([]parser.Cell, needed, newCap)
		copy(grown, line.cells)
		line.cells = grown
	}
	line.cells[col] = cell
	if globalIdx > s.contentEnd {
		s.contentEnd = globalIdx
	}
}

// GetLine returns a copy of the cells at globalIdx. Returns nil if the
// globalIdx has never been written to. The returned slice is safe to mutate
// — it does not alias Store internal state.
func (s *Store) GetLine(globalIdx int64) []parser.Cell {
	s.mu.RLock()
	defer s.mu.RUnlock()
	line, ok := s.lines[globalIdx]
	if !ok {
		return nil
	}
	out := make([]parser.Cell, len(line.cells))
	copy(out, line.cells)
	return out
}

// SetLine replaces the cells at globalIdx with a copy of cells. Any existing
// content at that globalIdx is overwritten in full. To preserve alignment
// with column 0, callers must pass cells starting at column 0.
func (s *Store) SetLine(globalIdx int64, cells []parser.Cell) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line, ok := s.lines[globalIdx]
	if !ok {
		line = &storeLine{}
		s.lines[globalIdx] = line
	}
	line.cells = make([]parser.Cell, len(cells))
	copy(line.cells, cells)
	if globalIdx > s.contentEnd {
		s.contentEnd = globalIdx
	}
}

// ClearRange removes every line in the closed interval [lo, hi]. Lines
// outside the interval are untouched. contentEnd is not decreased — a
// cleared range still counts as "ever been written" for the high-water mark.
func (s *Store) ClearRange(lo, hi int64) {
	if lo > hi {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Iterate the interval directly. This is O(hi-lo+1) rather than O(len(lines)),
	// which is efficient when the range is dense. If callers need to evict large
	// sparse ranges, prefer iterating s.lines keys instead.
	for k := lo; k <= hi; k++ {
		delete(s.lines, k)
	}
}

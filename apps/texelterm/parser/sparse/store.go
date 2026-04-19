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
//
// written tracks which cells were placed by an explicit write (Set or SetLine
// with content from the shell) versus filled implicitly when Set extended the
// slice past its old length. This distinguishes typed content from positional
// gaps left by CUF/CUP cursor jumps and lets the reflow logic skip slicing
// rows whose only "wide" extent is positional padding (issue #193 / #197).
//
// written and cells are kept the same length. WrittenCount caches the popcount
// for O(1) WrittenExtent queries.
type storeLine struct {
	cells        []parser.Cell
	written      []bool
	writtenCount int
	nowrap       bool
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
// automatically extended to cover col if it did not already; intermediate
// cells (positional gap from a cursor jump) stay marked unwritten so the
// reflow layer can distinguish them from typed content.
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
		grownWritten := make([]bool, needed, newCap)
		copy(grownWritten, line.written)
		line.written = grownWritten
	}
	line.cells[col] = cell
	if !line.written[col] {
		line.written[col] = true
		line.writtenCount++
	}
	if globalIdx > s.contentEnd {
		s.contentEnd = globalIdx
	}
}

// GetLine returns a copy of the cells at globalIdx. Returns nil if the
// globalIdx has never been written to. Returns a non-nil (possibly zero-length)
// slice for a row that exists but has no cells (e.g. created by SetRowNoWrap).
// The returned slice is safe to mutate — it does not alias Store internal state.
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
// content at that globalIdx is overwritten in full. The NoWrap flag is
// preserved — use SetLineWithNoWrap to set an explicit flag value.
// To preserve alignment with column 0, callers must pass cells starting at
// column 0.
//
// Every cell in the replacement is marked written — SetLine is the bulk-replace
// API used by reflow shifts and persistence reload, neither of which produce
// positional gaps.
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
	line.written = make([]bool, len(cells))
	for i := range line.written {
		line.written[i] = true
	}
	line.writtenCount = len(cells)
	if globalIdx > s.contentEnd {
		s.contentEnd = globalIdx
	}
}

// RowNoWrap reports whether the row at globalIdx is marked NoWrap.
// Missing rows return false.
func (s *Store) RowNoWrap(globalIdx int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	line, ok := s.lines[globalIdx]
	if !ok {
		return false
	}
	return line.nowrap
}

// SetRowNoWrap sets the NoWrap flag on the row at globalIdx. The flag is
// sticky: passing false does NOT clear it. Callers that need to clear must
// replace the row via SetLineWithNoWrap.
//
// If the row does not yet exist, it is created empty so the flag sticks.
func (s *Store) SetRowNoWrap(globalIdx int64, nowrap bool) {
	if !nowrap {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	line, ok := s.lines[globalIdx]
	if !ok {
		line = &storeLine{}
		s.lines[globalIdx] = line
	}
	line.nowrap = true
	if globalIdx > s.contentEnd {
		s.contentEnd = globalIdx
	}
}

// SetLineWithNoWrap replaces both cells and the NoWrap flag at globalIdx.
// Used by IL/DL/scroll shifts that move a row (and its NoWrap semantics)
// from one globalIdx to another. Like SetLine, every replacement cell is
// marked written.
func (s *Store) SetLineWithNoWrap(globalIdx int64, cells []parser.Cell, nowrap bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line, ok := s.lines[globalIdx]
	if !ok {
		line = &storeLine{}
		s.lines[globalIdx] = line
	}
	line.cells = make([]parser.Cell, len(cells))
	copy(line.cells, cells)
	line.written = make([]bool, len(cells))
	for i := range line.written {
		line.written[i] = true
	}
	line.writtenCount = len(cells)
	line.nowrap = nowrap
	if globalIdx > s.contentEnd {
		s.contentEnd = globalIdx
	}
}

// WrittenExtent returns (writtenCount, lastWrittenCol) for the row at
// globalIdx. writtenCount is the number of cells placed by Set or SetLine;
// lastWrittenCol is the highest column index that was written, or -1 when
// no cells were written. Missing rows return (0, -1).
//
// rowHasPositionalGap uses this to detect rows with cursor-positioned
// fillers — cells whose len(cells) extends past actual typed content.
func (s *Store) WrittenExtent(globalIdx int64) (count int, lastCol int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	line, ok := s.lines[globalIdx]
	if !ok {
		return 0, -1
	}
	last := -1
	for i := len(line.written) - 1; i >= 0; i-- {
		if line.written[i] {
			last = i
			break
		}
	}
	return line.writtenCount, last
}

// EraseCell clears the cell at (globalIdx, col), unmarking it as written.
// Unlike Set, EraseCell never extends the row past its current length —
// erasing a column that was never written is a no-op. The supplied cell
// value is stored verbatim (so callers can preserve a colored background)
// but the written bit is cleared so reflow's gap detector treats the cell
// as positional padding rather than typed content.
func (s *Store) EraseCell(globalIdx int64, col int, cell parser.Cell) {
	if col < 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	line, ok := s.lines[globalIdx]
	if !ok {
		return
	}
	if col >= len(line.cells) {
		return
	}
	line.cells[col] = cell
	if line.written[col] {
		line.written[col] = false
		line.writtenCount--
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

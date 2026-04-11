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
func (s *Store) Width() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.width
}

// Max returns the highest globalIdx ever written. Returns -1 if the Store
// has never been written to.
func (s *Store) Max() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.contentEnd
}

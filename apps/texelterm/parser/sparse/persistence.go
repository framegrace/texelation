// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"time"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// Persistence adapts a sparse.Store / sparse.Terminal to the existing
// PageStore on-disk layer. The same globalIdx is used on both sides, so a
// line in sparse.Store at globalIdx 42 is persisted at PageStore globalIdx 42.
//
// This is a thin forward-only adapter: it does not own lifecycle, it does not
// manage flush scheduling. Those concerns stay in AdaptivePersistence. The
// adapter only knows how to take a list of "dirty" globalIdxs and push them
// to PageStore, and how to save/load MainScreenState.
type Persistence struct {
	page *parser.PageStore
}

// NewPersistence creates a new adapter writing to the given PageStore.
func NewPersistence(ps *parser.PageStore) *Persistence {
	return &Persistence{page: ps}
}

// FlushLines forwards each listed globalIdx's current content in the Store
// to the PageStore. Missing lines (gaps) are skipped. Lines that already
// exist in PageStore are updated via UpdateLine; new lines are appended via
// AppendLineWithGlobalIdx.
func (p *Persistence) FlushLines(store *Store, globalIdxs []int64) error {
	now := time.Now()
	for _, gi := range globalIdxs {
		cells := store.GetLine(gi)
		if cells == nil {
			continue
		}
		line := &parser.LogicalLine{Cells: cells}
		if p.page.HasLine(gi) {
			if err := p.page.UpdateLine(gi, line, now); err != nil {
				return err
			}
		} else {
			if err := p.page.AppendLineWithGlobalIdx(gi, line, now); err != nil {
				return err
			}
		}
	}
	return nil
}

// SnapshotState captures the current Terminal state into a MainScreenState
// suitable for WAL persistence.
func SnapshotState(tm *Terminal) parser.MainScreenState {
	gi, col := tm.Cursor()
	return parser.MainScreenState{
		WriteTop:        tm.WriteTop(),
		ContentEnd:      tm.ContentEnd(),
		CursorGlobalIdx: gi,
		CursorCol:       col,
		PromptStartLine: -1,
		SavedAt:         time.Now(),
	}
}

// RestoreState applies a MainScreenState to an existing Terminal, overwriting
// cursor and writeTop. The ViewWindow is put into autoFollow mode snapped to
// the new writeBottom.
func RestoreState(tm *Terminal, state parser.MainScreenState) {
	tm.RestoreWriteState(state.WriteTop, state.CursorGlobalIdx, state.CursorCol)
}

// LoadStore reads every line currently present in the PageStore into the
// given sparse.Store. Used on startup to rebuild the in-memory state from
// disk. Existing entries in the Store are overwritten when their globalIdx
// matches; unrelated entries are untouched.
func LoadStore(store *Store, ps *parser.PageStore) error {
	count := ps.LineCount()
	for gi := int64(0); gi < count; gi++ {
		if !ps.HasLine(gi) {
			continue
		}
		line, err := ps.ReadLine(gi)
		if err != nil {
			return err
		}
		if line == nil {
			continue
		}
		store.SetLine(gi, line.Cells)
	}
	return nil
}

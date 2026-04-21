// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"fmt"

	"github.com/framegrace/texelation/apps/texelterm/parser/sparse"
	"github.com/framegrace/texelation/protocol"
)

// fetchRangeProvider is implemented by apps that can serve scrollback fetches.
// Live implementation: *texelterm.TexelTerm.
type fetchRangeProvider interface {
	InAltScreen() bool
	SparseStore() *sparse.Store
}

// ServeFetchRange reads rows [lo, hi) from the sparse store and returns a
// FetchRangeResponse stamped with the provided revision. Cold pages are
// expected to have been faulted in by the Store's own persistence bridge
// before this call; ServeFetchRange does not drive page loads itself.
//
// If any row in the range has globalIdx below Store.OldestRetained(), the
// response flags include FetchRangeBelowRetention.
func ServeFetchRange(st *sparse.Store, req protocol.FetchRange, revision uint32) (protocol.FetchRangeResponse, error) {
	resp := protocol.FetchRangeResponse{
		RequestID: req.RequestID,
		PaneID:    req.PaneID,
		Revision:  revision,
	}
	if req.LoIdx >= req.HiIdx {
		resp.Flags |= protocol.FetchRangeEmpty
		return resp, nil
	}
	oldest := st.OldestRetained()
	if oldest != -1 && req.LoIdx < oldest {
		resp.Flags |= protocol.FetchRangeBelowRetention
	}
	table := newStyleTable()
	for idx := req.LoIdx; idx < req.HiIdx; idx++ {
		cells := st.GetLine(idx)
		if cells == nil {
			continue
		}
		row := protocol.LogicalRow{
			GlobalIdx: idx,
			NoWrap:    st.RowNoWrap(idx),
		}
		spans, encErr := encodeParserCellsToSpans(cells, table)
		if encErr != nil {
			return resp, fmt.Errorf("encode spans: %w", encErr)
		}
		row.Spans = spans
		if n := len(cells); n > 0 && cells[n-1].Wrapped {
			row.Wrapped = true
		}
		resp.Rows = append(resp.Rows, row)
	}
	resp.Styles = table.entries()
	if len(resp.Rows) == 0 && resp.Flags&protocol.FetchRangeBelowRetention == 0 {
		resp.Flags |= protocol.FetchRangeEmpty
	}
	return resp, nil
}

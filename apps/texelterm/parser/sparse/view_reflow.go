// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import "github.com/framegrace/texelation/apps/texelterm/parser"

// walkChain returns the end globalIdx of the Wrapped chain starting at
// startGI, plus whether any row in the chain is marked NoWrap (chain
// propagation). Walks at most maxSteps rows to bound pathological inputs.
//
// A chain is defined as a sequence of globalIdxs where every row except
// the last has its final cell Wrapped=true and the next row exists in
// the store. A missing row terminates the chain at the current idx.
func walkChain(s *Store, startGI int64, maxSteps int) (end int64, nowrap bool) {
	end = startGI
	nowrap = s.RowNoWrap(startGI)
	for steps := 1; steps < maxSteps; steps++ {
		cells := s.GetLine(end)
		if len(cells) == 0 || !cells[len(cells)-1].Wrapped {
			return end, nowrap
		}
		next := end + 1
		if s.GetLine(next) == nil {
			return end, nowrap
		}
		end = next
		if s.RowNoWrap(end) {
			nowrap = true
		}
	}
	return end, nowrap
}

// reflowChain returns the reflowed physical rows of the chain [startGI, endGI]
// at viewWidth. Each returned slice has length ≤ viewWidth (the caller pads
// to viewWidth via clipRow as needed).
//
// Concatenates all cells in the chain, then slices at viewWidth. Trailing
// empty rows within the chain (wrap continuations the cursor sits on before
// any content has been written) are emitted as explicit blank rows so the
// chain's reflowed row count matches its physical-row footprint.
func reflowChain(s *Store, startGI, endGI int64, viewWidth int) [][]parser.Cell {
	if viewWidth <= 0 {
		return nil
	}
	var logical []parser.Cell
	for gi := startGI; gi <= endGI; gi++ {
		logical = append(logical, s.GetLine(gi)...)
	}
	trailing := trailingEmptyRows(s, startGI, endGI)
	if len(logical) == 0 && trailing == 0 {
		return [][]parser.Cell{nil}
	}
	var rows [][]parser.Cell
	for off := 0; off < len(logical); off += viewWidth {
		end := off + viewWidth
		if end > len(logical) {
			end = len(logical)
		}
		row := make([]parser.Cell, end-off)
		copy(row, logical[off:end])
		rows = append(rows, row)
	}
	for i := 0; i < trailing; i++ {
		rows = append(rows, nil)
	}
	return rows
}

// trailingEmptyRows counts empty rows at the tail of the chain [start..end].
// Only counts rows strictly after start (start itself is the chain head and
// may legitimately be empty). These represent wrap continuations where the
// cursor has newlined past a Wrapped full row but hasn't written any cell.
func trailingEmptyRows(s *Store, start, end int64) int {
	n := 0
	for r := end; r > start; r-- {
		if len(s.GetLine(r)) > 0 {
			break
		}
		n++
	}
	return n
}

// chainReflowedRowCount returns the number of physical rows the chain
// [start..end] occupies when reflowed at width. Matches reflowChain's output
// row count, including trailing empty continuation rows.
func chainReflowedRowCount(s *Store, start, end int64, width int, nowrap bool) int {
	if nowrap {
		return int(end - start + 1)
	}
	total := 0
	for r := start; r <= end; r++ {
		total += len(s.GetLine(r))
	}
	var rows int
	if total == 0 {
		rows = 1
	} else {
		rows = (total + width - 1) / width
	}
	return rows + trailingEmptyRows(s, start, end)
}

// findChainStart walks backward from gi to the head of its wrap chain. A
// chain head is a globalIdx whose predecessor is either absent, empty, or
// does not have Wrapped=true on its last cell. Empty rows are themselves
// chain starts.
func findChainStart(s *Store, gi int64, maxSteps int) int64 {
	cells := s.GetLine(gi)
	if len(cells) == 0 {
		return gi
	}
	start := gi
	for steps := 0; steps < maxSteps && start > 0; steps++ {
		prev := start - 1
		prevCells := s.GetLine(prev)
		if len(prevCells) == 0 {
			break
		}
		if !prevCells[len(prevCells)-1].Wrapped {
			break
		}
		start = prev
	}
	return start
}

// clipRow returns cells truncated or padded to viewWidth. Used for NoWrap
// rows that render 1:1 inside the viewport.
func clipRow(cells []parser.Cell, viewWidth int) []parser.Cell {
	out := make([]parser.Cell, viewWidth)
	for i := 0; i < viewWidth; i++ {
		if i < len(cells) {
			out[i] = cells[i]
		}
	}
	return out
}

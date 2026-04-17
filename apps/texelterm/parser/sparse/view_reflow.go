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
// Concatenates all cells in the chain, then slices at viewWidth.
func reflowChain(s *Store, startGI, endGI int64, viewWidth int) [][]parser.Cell {
	if viewWidth <= 0 {
		return nil
	}
	var logical []parser.Cell
	for gi := startGI; gi <= endGI; gi++ {
		logical = append(logical, s.GetLine(gi)...)
	}
	if len(logical) == 0 {
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
	return rows
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

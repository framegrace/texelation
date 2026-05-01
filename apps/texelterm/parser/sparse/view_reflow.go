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
//
// A single-row chain whose stored cells contain a positional gap (some cell
// before the last-written col is unwritten — e.g., a powerline prompt that
// jumped via `ESC[500C ESC[17D` and wrote at col 89 with cols 0..88 still
// unwritten) renders as exactly one row at the cells' original positions.
// Off-viewport cells clip naturally via clipRow. Without this, the stored
// cell count would be sliced into multiple physical rows on shrink, surfacing
// the right-side content on a NEW row (issue #193). Lines without gaps
// (everything autowrap or shell-typed contiguously from col 0) reflow
// normally — `ls -l` output etc. continues to wrap on shrink (issue #197).
func reflowChain(s *Store, startGI, endGI int64, viewWidth int) [][]parser.Cell {
	if viewWidth <= 0 {
		return nil
	}
	if startGI == endGI && rowHasPositionalGap(s, startGI) {
		return [][]parser.Cell{s.GetLine(startGI)}
	}
	var logical []parser.Cell
	for gi := startGI; gi <= endGI; gi++ {
		logical = append(logical, s.GetLine(gi)...)
	}
	logical = trimTrailingPadding(logical)
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

// trimTrailingPadding drops trailing PADDING cells (space rune, default
// bg, no attributes) from the end of the logical chain. Padding cells
// are visually indistinguishable from "no cell at all", so dropping them
// is safe.
//
// Motivation: TUIs (Claude Code, anything that right-pads a frame to
// viewport width) end lines with runs of spaces. On horizontal-resize
// narrower, naïve reflow puts those spaces on a SECOND visual row below
// the content — phantom blank line. Clipping at the chain's last non-
// padding cell makes the visual line end where the visible content ends.
//
// Coloured / attributed trailing spaces (BG != default, or Attr != 0)
// are NOT padding — they carry visual meaning (a TUI's coloured bar
// drawn with spaces, a reverse-video selection / cursor cell). Real
// terminals like tmux preserve them, so divergence here would surface
// as parser-bug-flavoured diffs in reference comparisons.
//
// Embedded whitespace (spaces between content) is untouched — only the
// strictly-trailing run is trimmed.
func trimTrailingPadding(cells []parser.Cell) []parser.Cell {
	n := len(cells)
	for n > 0 && isPaddingCell(cells[n-1]) {
		n--
	}
	return cells[:n]
}

// chainTrailingPaddingCount returns how many padding cells are at the
// tail of the chain [start..end]. Walks across stored row boundaries,
// so a chain whose last stored row is all padding AND whose previous
// row also ends in padding collapses both into the count. Matches what
// reflowChain trims via trimTrailingPadding so chainReflowedRowCount
// can predict the reflowed row count without rebuilding the slice.
func chainTrailingPaddingCount(s *Store, start, end int64) int {
	n := 0
	for r := end; r >= start; r-- {
		cells := s.GetLine(r)
		if len(cells) == 0 {
			continue
		}
		i := len(cells) - 1
		for i >= 0 && isPaddingCell(cells[i]) {
			i--
		}
		n += len(cells) - 1 - i
		if i >= 0 {
			return n
		}
	}
	return n
}

// isPaddingCell reports whether the cell is a space with default fg,
// default bg, and no attributes — strictly indistinguishable from a
// zero-valued empty cell.
//
// FG matters even though spaces render no glyph: tmux and other
// reference terminals retain colour state on trailing spaces because it
// persists into subsequent writes (the next character at this column
// inherits the colour) and into selection / reverse-video rendering.
// Trimming a coloured-fg space would surface as fg-divergence vs the
// reference terminal output. Keeping the predicate strict — only
// zero-equivalent cells trim — gives the wrap-of-padding fix without
// parser divergence.
func isPaddingCell(c parser.Cell) bool {
	return c.Rune == ' ' &&
		c.FG.Mode == parser.ColorModeDefault &&
		c.BG.Mode == parser.ColorModeDefault &&
		c.Attr == 0
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
// [start..end] occupies when reflowed at width. Matches reflowChain's
// output row count exactly, including trailing empty continuation rows.
//
// The off-by-one trap: reflowChain returns 1 nil row only when both
// logical AND trailing are empty (the early return). When logical is
// empty but trailing > 0, it returns trailing rows, not trailing+1. We
// must mirror that — adding "+1 for empty content" plus trailing would
// over-count by 1 row, which breaks RecomputeLiveAnchor's offset math
// and surfaces as a visible duplicate row at the resize boundary.
//
// See reflowChain for the positional-gap rationale (issue #193 / #197).
func chainReflowedRowCount(s *Store, start, end int64, width int, nowrap bool) int {
	if nowrap {
		return int(end - start + 1)
	}
	if start == end && rowHasPositionalGap(s, start) {
		return 1
	}
	total := 0
	for r := start; r <= end; r++ {
		total += len(s.GetLine(r))
	}
	// Subtract trailing padding cells so the row count matches what
	// reflowChain produces after trimTrailingPadding.
	total -= chainTrailingPaddingCount(s, start, end)
	if total < 0 {
		total = 0
	}
	trailing := trailingEmptyRows(s, start, end)
	var rows int
	switch {
	case total > 0:
		rows = (total + width - 1) / width
	case trailing == 0:
		// Empty chain with no trailing continuations — reflowChain emits
		// one nil row.
		rows = 1
	default:
		// Empty chain with trailing continuations — reflowChain emits
		// only the `trailing` rows; no extra "1 row for the empty
		// chain head".
		rows = 0
	}
	return rows + trailing
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

// rowHasPositionalGap reports whether the row at gi was placed via cursor
// positioning (CUF/CUP) past existing content rather than written contiguously
// from col 0. Concretely: the count of cells actually written via WriteCell
// is less than (lastWrittenCol + 1), i.e. there's at least one unwritten cell
// before the last-written cell. Such rows must NOT be sliced into multiple
// physical rows on shrink — the unwritten cells aren't real content and
// shouldn't drag content past the viewport edge onto a new physical row.
//
// An empty row (no cells) and a row with no written cells are not "positional"
// — they're just blank.
func rowHasPositionalGap(s *Store, gi int64) bool {
	written, lastCol := s.WrittenExtent(gi)
	if written == 0 {
		return false
	}
	return written < lastCol+1
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

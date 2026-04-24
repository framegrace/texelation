// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"sync"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// ViewWindow is the user-facing portion of a sparse terminal. It owns the
// viewBottom anchor and the autoFollow flag, and it responds to write-window
// events when following.
//
// ViewWindow does not read from the Store directly — it only tracks the
// coordinate pair (viewTop, viewBottom) for the caller to project.
// ViewWindow is safe for concurrent use.
type ViewWindow struct {
	mu         sync.Mutex
	width      int
	height     int
	viewBottom int64
	autoFollow bool

	// Reflow state (2026-04-16 resize-reflow)
	viewAnchor       int64
	viewAnchorOffset int
	globalReflowOff  bool
	autoJumpOnInput  bool
}

// NewViewWindow creates a ViewWindow in autoFollow mode. viewBottom starts
// at height-1 so a fresh terminal projects rows [0, height-1].
func NewViewWindow(width, height int) *ViewWindow {
	return &ViewWindow{
		width:           width,
		height:          height,
		viewBottom:      int64(height - 1),
		autoFollow:      true,
		autoJumpOnInput: true,
	}
}

// SetViewAnchor sets the chain globalIdx and sub-row offset that the view
// begins at. offset skips reflowed sub-rows within the first chain.
func (v *ViewWindow) SetViewAnchor(globalIdx int64, offset int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.viewAnchor = globalIdx
	v.viewAnchorOffset = offset
}

// Anchor returns the current (viewAnchor, viewAnchorOffset) pair.
func (v *ViewWindow) Anchor() (int64, int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.viewAnchor, v.viewAnchorOffset
}

// ScrollBy moves the viewAnchor by dRows (negative scrolls up into history,
// positive scrolls down toward the live edge). Disables autoFollow so the
// view stays put instead of snapping back. viewAnchor is clamped to >= 0 and
// viewAnchorOffset is reset to 0.
func (v *ViewWindow) ScrollBy(s *Store, dRows int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.autoFollow = false
	v.viewAnchor += int64(dRows)
	if v.viewAnchor < 0 {
		v.viewAnchor = 0
	}
	v.viewAnchorOffset = 0
}

// SetGlobalReflowOff toggles reflow off globally. When true, all chains
// render 1:1 (clipped), ignoring the Wrapped flag.
func (v *ViewWindow) SetGlobalReflowOff(off bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.globalReflowOff = off
}

// SetAutoJumpOnInput controls whether OnInput snaps the view back to the
// live edge. When false, the user's scroll position is preserved when they
// type; when true (default), any input re-engages autoFollow at writeBottom.
func (v *ViewWindow) SetAutoJumpOnInput(enabled bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.autoJumpOnInput = enabled
}

// Render projects the viewport by walking chains from viewAnchor. Each
// chain is reflowed to viewWidth (unless NoWrap or globalReflowOff is set,
// in which case rows render 1:1 via clipRow). Returns exactly viewHeight
// rows, padded with empty cells if content is exhausted, paired with a
// parallel per-row globalIdx slice: rowGI[y] is the store globalIdx that
// row y corresponds to (chain-head gi for reflowed sub-rows), or -1 if
// the row is a blank-pad row with no underlying store position.
//
// Returning the rowGI slice alongside the rendered rows keeps the two in
// lockstep even under concurrent Render calls — callers receive a
// self-consistent pair, and no per-instance state has to straddle the
// walk/publish boundary.
func (v *ViewWindow) Render(s *Store) ([][]parser.Cell, []int64) {
	v.mu.Lock()
	width := v.width
	height := v.height
	anchor := v.viewAnchor
	skip := v.viewAnchorOffset
	reflowOff := v.globalReflowOff
	v.mu.Unlock()

	out := make([][]parser.Cell, 0, height)
	rowGI := make([]int64, 0, height)
	maxSteps := 4 * height
	if maxSteps < 4 {
		maxSteps = 4
	}

	gi := anchor
	first := true
	for len(out) < height {
		// Gap / past content: emit a blank row for this gi and continue.
		// Live mode may have interior gaps (EL/ED erasing lines inside the
		// writeTop..writeTop+h-1 window), and the old Grid() path surfaced
		// them as blank rows. We preserve that behavior and rely on the
		// caller's viewport bounds (via anchor+height) to stop the walk.
		if len(s.GetLine(gi)) == 0 && !s.RowNoWrap(gi) {
			if first {
				first = false
				// A skip on an empty first chain is a no-op.
			}
			out = append(out, make([]parser.Cell, width))
			// Blank/pad rows with no real content track as -1 so callers
			// don't conflate them with a written row at that globalIdx.
			rowGI = append(rowGI, -1)
			gi++
			continue
		}
		end, nowrap := walkChain(s, gi, maxSteps)

		var rows [][]parser.Cell
		var rowsGI []int64
		if reflowOff || nowrap {
			// Each physical row is its own globalIdx: gi, gi+1, ..., end.
			for r := gi; r <= end; r++ {
				rows = append(rows, clipRow(s.GetLine(r), width))
				rowsGI = append(rowsGI, r)
			}
		} else {
			// Wrapped chain reflowed to this viewport's width: all reflowed
			// sub-rows share the chain's head globalIdx. Sub-row resolution
			// would require tracking cell-range provenance through
			// reflowChain; the publisher only needs "does this row belong
			// to a real store position" which the chain head answers.
			reflowed := reflowChain(s, gi, end, width)
			for _, row := range reflowed {
				rows = append(rows, clipRow(row, width))
				rowsGI = append(rowsGI, gi)
			}
		}

		if first {
			first = false
			if skip < len(rows) {
				rows = rows[skip:]
				rowsGI = rowsGI[skip:]
			} else {
				rows = nil
				rowsGI = nil
			}
		}

		for i, row := range rows {
			if len(out) >= height {
				break
			}
			out = append(out, row)
			rowGI = append(rowGI, rowsGI[i])
		}
		gi = end + 1
	}

	for len(out) < height {
		out = append(out, make([]parser.Cell, width))
		rowGI = append(rowGI, -1)
	}

	// Trim to height in case any loop appended one extra entry.
	if len(rowGI) > height {
		rowGI = rowGI[:height]
	}

	return out, rowGI
}

// RecomputeLiveAnchor repositions viewAnchor/viewAnchorOffset so that the
// cursor's chain sits on the bottom of the viewport. Called once per render
// pass when autoFollow is active: the view is a trailing window over the
// write activity, and the cursor's chain is what the user needs to see.
//
// Algorithm: find the chain containing cursorGI (walk back while the previous
// row's last cell has Wrapped=true), then walk backward one chain at a time
// accumulating reflowed rows per chain. Stop when the accumulated total
// covers the viewport; anchor at that chain with an offset to trim the top.
//
// writeTop is the globalIdx of the top of the live write window; the backward
// walk clamps at writeTop so scrollback never leaks into the live viewport on
// horizontal widening (where old wrapped chains reflow smaller and the walk
// would otherwise reach past writeTop to fill height). See #48.
func (v *ViewWindow) RecomputeLiveAnchor(s *Store, cursorGI int64, cursorCol int, writeTop int64) {
	v.mu.Lock()
	height := v.height
	width := v.width
	reflowOff := v.globalReflowOff
	autoFollow := v.autoFollow
	v.mu.Unlock()

	if !autoFollow {
		return
	}
	if height <= 0 || width <= 0 {
		return
	}

	maxSteps := 4 * height
	if maxSteps < 4 {
		maxSteps = 4
	}

	// Find the start of the chain containing cursorGI: walk backward while
	// the prior row's last cell has Wrapped=true and exists. Clamp at
	// writeTop so a chain that spans writeTop renders as its live-side
	// portion only (scrollback side stays in scrollback).
	chainStart := cursorGI
	for steps := 0; steps < maxSteps && chainStart > writeTop; steps++ {
		prev := chainStart - 1
		prevCells := s.GetLine(prev)
		if len(prevCells) == 0 {
			break
		}
		if !prevCells[len(prevCells)-1].Wrapped {
			break
		}
		chainStart = prev
	}

	// Walk chains backward, accumulating reflowed row counts. Empty rows
	// (blank-line separators in plain output like `ls -lR`, or erased lines)
	// are not chain starts, but they still occupy one physical row. Treat them
	// as 1-row "chains" and continue walking rather than bailing — bailing on
	// the first blank above the cursor would pin the viewport to the top of
	// history on perfectly ordinary output.
	accumulated := 0
	gi := chainStart
	for {
		cells := s.GetLine(gi)
		if len(cells) == 0 && !s.RowNoWrap(gi) {
			accumulated++
			if accumulated >= height {
				offset := accumulated - height
				v.mu.Lock()
				v.viewAnchor = gi
				v.viewAnchorOffset = offset
				v.mu.Unlock()
				return
			}
			if gi <= writeTop {
				break
			}
			// Walk to the start of the previous chain (mirrors the non-empty
			// branch below). Without this, gi-- could land in the middle of a
			// wrapped chain and walkChain would count only its tail row,
			// leading the next iteration to count the same chain again.
			prevGI := gi - 1
			prevCells := s.GetLine(prevGI)
			if len(prevCells) == 0 {
				gi = prevGI
				continue
			}
			prevChainStart := prevGI
			for steps := 0; steps < maxSteps && prevChainStart > writeTop; steps++ {
				pp := prevChainStart - 1
				ppCells := s.GetLine(pp)
				if len(ppCells) == 0 {
					break
				}
				if !ppCells[len(ppCells)-1].Wrapped {
					break
				}
				prevChainStart = pp
			}
			gi = prevChainStart
			continue
		}
		end, nowrap := walkChain(s, gi, maxSteps)
		if reflowOff {
			nowrap = true
		}
		chainRows := chainReflowedRowCount(s, gi, end, width, nowrap)
		accumulated += chainRows
		if accumulated >= height {
			offset := accumulated - height
			v.mu.Lock()
			v.viewAnchor = gi
			v.viewAnchorOffset = offset
			v.mu.Unlock()
			return
		}
		if gi <= writeTop {
			break
		}
		// Walk to the start of the previous chain. An empty prev row is
		// itself the "previous chain" — fall through to the top of the loop
		// to count it as 1 row. Clamp the chain-start walk at writeTop so a
		// chain that spans writeTop doesn't pull its scrollback portion into
		// the live viewport.
		prevGI := gi - 1
		prevCells := s.GetLine(prevGI)
		if len(prevCells) == 0 {
			gi = prevGI
			continue
		}
		prevChainStart := prevGI
		for steps := 0; steps < maxSteps && prevChainStart > writeTop; steps++ {
			pp := prevChainStart - 1
			ppCells := s.GetLine(pp)
			if len(ppCells) == 0 {
				break
			}
			if !ppCells[len(ppCells)-1].Wrapped {
				break
			}
			prevChainStart = pp
		}
		gi = prevChainStart
	}

	// First-stage walk hit writeTop without filling the viewport. If the
	// cursor sits at or below the natural bottom of a height-sized window
	// rooted at writeTop, pull scrollback (rows < writeTop) to refill the
	// top so the cursor stays pinned at the viewport bottom (issue #197).
	//
	// Guard: if the cursor is well above that bottom, the live region is
	// not full — we're in a fresh-resize state where the application (a
	// TUI repainting via SIGWINCH, or a script that reset the cursor) will
	// fill the live region itself. Pulling scrollback here would duplicate
	// content the application is about to overwrite (#48).
	if cursorGI < writeTop+int64(height)-1 {
		v.mu.Lock()
		v.viewAnchor = writeTop
		v.viewAnchorOffset = 0
		v.mu.Unlock()
		return
	}

	// Skip any chain whose tail crosses writeTop — its live-side portion
	// was already counted in the first stage, and re-counting the
	// scrollback portion would duplicate cells.
	gi = writeTop - 1
	for accumulated < height && gi >= 0 {
		cells := s.GetLine(gi)
		if len(cells) == 0 && !s.RowNoWrap(gi) {
			accumulated++
			if accumulated >= height {
				offset := accumulated - height
				v.mu.Lock()
				v.viewAnchor = gi
				v.viewAnchorOffset = offset
				v.mu.Unlock()
				return
			}
			gi--
			continue
		}
		chainStart := findChainStart(s, gi, maxSteps)
		end, nowrap := walkChain(s, chainStart, maxSteps)
		if end >= writeTop {
			gi = chainStart - 1
			continue
		}
		if reflowOff {
			nowrap = true
		}
		chainRows := chainReflowedRowCount(s, chainStart, end, width, nowrap)
		accumulated += chainRows
		if accumulated >= height {
			offset := accumulated - height
			v.mu.Lock()
			v.viewAnchor = chainStart
			v.viewAnchorOffset = offset
			v.mu.Unlock()
			return
		}
		gi = chainStart - 1
	}

	// Ran out of content entirely: anchor at the earliest available row
	// (0 if we exhausted scrollback, writeTop otherwise) so Render starts
	// from the top of what's available. For a fresh session (writeTop=0)
	// this degenerates to the old "anchor at top" behavior.
	v.mu.Lock()
	if gi < 0 {
		v.viewAnchor = 0
	} else {
		v.viewAnchor = writeTop
	}
	v.viewAnchorOffset = 0
	v.mu.Unlock()
}

// CursorToView maps a store (globalIdx, col) to (viewRow, viewCol) within
// the current view. Returns ok=false if the cursor is not inside the visible
// chain walk.
func (v *ViewWindow) CursorToView(s *Store, cursorGI int64, cursorCol int) (viewRow, viewCol int, ok bool) {
	v.mu.Lock()
	height, width := v.height, v.width
	anchor, offset := v.viewAnchor, v.viewAnchorOffset
	reflowOff := v.globalReflowOff
	v.mu.Unlock()

	emitted := 0
	gi := anchor
	maxSteps := 4 * height
	for emitted < height {
		end, nowrap := walkChain(s, gi, maxSteps)
		if reflowOff {
			nowrap = true
		}
		if cursorGI >= gi && cursorGI <= end {
			// In this chain.
			if nowrap {
				rowInChain := int(cursorGI - gi)
				startAt := 0
				if gi == anchor {
					startAt = offset
				}
				if rowInChain < startAt {
					return 0, 0, false
				}
				vr := emitted + (rowInChain - startAt)
				if vr >= height {
					return 0, 0, false
				}
				vc := cursorCol
				if vc >= width {
					vc = width - 1
				}
				return vr, vc, true
			}
			// Reflowed path. If the cursor sits on an empty trailing row
			// (cursorGI > chain head and cursorGI's line is empty), the
			// logical-column calculation can't express its position — at
			// widths where the preceding content fits without wrapping, every
			// post-content row would collapse to logicalCol/width = 0. Compute
			// the row as contentRows + (count of empty rows before cursorGI).
			if cursorGI > gi && len(s.GetLine(cursorGI)) == 0 {
				total := 0
				for r := gi; r < cursorGI; r++ {
					total += len(s.GetLine(r))
				}
				contentRows := 0
				if total == 0 {
					contentRows = 1
				} else {
					contentRows = (total + width - 1) / width
				}
				emptiesBefore := 0
				for r := gi + 1; r < cursorGI; r++ {
					if len(s.GetLine(r)) == 0 {
						emptiesBefore++
					}
				}
				rowInChain := contentRows + emptiesBefore
				startAt := 0
				if gi == anchor {
					startAt = offset
				}
				if rowInChain < startAt {
					return 0, 0, false
				}
				vr := emitted + (rowInChain - startAt)
				if vr >= height {
					return 0, 0, false
				}
				vc := cursorCol
				if vc >= width {
					vc = width - 1
				}
				return vr, vc, true
			}
			// Reflowed: compute logical column.
			logicalCol := 0
			for r := gi; r < cursorGI; r++ {
				logicalCol += len(s.GetLine(r))
			}
			logicalCol += cursorCol
			rowInChain := logicalCol / width
			colInRow := logicalCol % width
			startAt := 0
			if gi == anchor {
				startAt = offset
			}
			if rowInChain < startAt {
				return 0, 0, false
			}
			vr := emitted + (rowInChain - startAt)
			if vr >= height {
				return 0, 0, false
			}
			return vr, colInRow, true
		}
		// Advance past chain.
		chainRows := chainReflowedRowCount(s, gi, end, width, nowrap)
		startAt := 0
		if gi == anchor {
			startAt = offset
		}
		emitted += chainRows - startAt
		gi = end + 1
		if s.GetLine(gi) == nil {
			break
		}
	}
	return 0, 0, false
}

// ViewToCursor maps (viewRow, viewCol) to (globalIdx, col) in the store.
// If viewRow is past content end, returns a fabricated "blank area" result
// (globalIdx beyond writeTop, col=viewCol).
func (v *ViewWindow) ViewToCursor(s *Store, viewRow, viewCol int) (globalIdx int64, col int, ok bool) {
	v.mu.Lock()
	height, width := v.height, v.width
	anchor, offset := v.viewAnchor, v.viewAnchorOffset
	reflowOff := v.globalReflowOff
	v.mu.Unlock()

	if viewRow < 0 || viewRow >= height {
		return 0, 0, false
	}

	emitted := 0
	gi := anchor
	maxSteps := 4 * height
	for emitted < height {
		if s.GetLine(gi) == nil {
			break
		}
		end, nowrap := walkChain(s, gi, maxSteps)
		if reflowOff {
			nowrap = true
		}
		chainRows := chainReflowedRowCount(s, gi, end, width, nowrap)
		startAt := 0
		if gi == anchor {
			startAt = offset
		}
		rowsFromThisChain := chainRows - startAt
		if viewRow < emitted+rowsFromThisChain {
			rowInChain := (viewRow - emitted) + startAt
			if nowrap {
				return gi + int64(rowInChain), viewCol, true
			}
			// Walk cells to find (gi, col)
			logicalCol := rowInChain*width + viewCol
			for r := gi; r <= end; r++ {
				rowLen := len(s.GetLine(r))
				if logicalCol < rowLen {
					return r, logicalCol, true
				}
				logicalCol -= rowLen
			}
			// viewCol past end of logical — return at end of chain
			return end, len(s.GetLine(end)), true
		}
		emitted += rowsFromThisChain
		gi = end + 1
	}
	// Past content.
	return gi + int64(viewRow-emitted), viewCol, true
}

// Width returns the current column width.
func (v *ViewWindow) Width() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.width
}

// Height returns the current row height.
func (v *ViewWindow) Height() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.height
}

// IsFollowing reports whether the view is tracking the write window.
func (v *ViewWindow) IsFollowing() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.autoFollow
}

// VisibleRange returns the (top, bottom) globalIdx pair that the caller
// should project from the Store.
func (v *ViewWindow) VisibleRange() (top, bottom int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.viewBottom - int64(v.height) + 1, v.viewBottom
}

// OnWriteBottomChanged is called when the bottom of the write window moves.
// newWriteBottom is the new WriteWindow.WriteBottom() value. If autoFollow
// is true, viewBottom advances to match — but never retreats. A resize that
// shrinks writeBottom must not pull viewBottom back; the view stays anchored
// until new content pushes past the old position.
func (v *ViewWindow) OnWriteBottomChanged(newWriteBottom int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.autoFollow && newWriteBottom > v.viewBottom {
		v.viewBottom = newWriteBottom
	}
}

// ScrollUpRows detaches from the live edge and moves the view back by n
// reflowed rows. Unlike ScrollUp (which decrements viewAnchor by n globalIdx
// units and so can land mid-chain, producing a partial-chain fragment at the
// top of the viewport), ScrollUpRows walks chains in reflowed-row units so
// viewAnchor always lands at a chain start with viewAnchorOffset tracking the
// sub-row position. viewBottom is decremented by the number of rows actually
// walked — NOT by n — so a clamped scroll (viewAnchor hit 0 with remaining > 0)
// doesn't push viewBottom into a stale state that lets a subsequent
// ScrollDownRows with velocity snap prematurely to the live edge.
func (v *ViewWindow) ScrollUpRows(s *Store, n int) {
	if n <= 0 {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.autoFollow = false

	remaining := n
	walked := 0
	if v.viewAnchorOffset >= remaining {
		v.viewAnchorOffset -= remaining
		walked = remaining
		v.viewBottom -= int64(walked)
		v.clampViewBottom()
		return
	}
	walked += v.viewAnchorOffset
	remaining -= v.viewAnchorOffset
	v.viewAnchorOffset = 0

	width := v.width
	reflowOff := v.globalReflowOff
	maxSteps := 4 * v.height
	if maxSteps < 4 {
		maxSteps = 4
	}
	for remaining > 0 && v.viewAnchor > 0 {
		prevGI := v.viewAnchor - 1
		prevStart := findChainStart(s, prevGI, maxSteps)
		end, nowrap := walkChain(s, prevStart, maxSteps)
		if reflowOff {
			nowrap = true
		}
		rows := chainReflowedRowCount(s, prevStart, end, width, nowrap)
		if rows > remaining {
			v.viewAnchor = prevStart
			v.viewAnchorOffset = rows - remaining
			walked += remaining
			v.viewBottom -= int64(walked)
			v.clampViewBottom()
			return
		}
		remaining -= rows
		walked += rows
		v.viewAnchor = prevStart
	}
	// Loop exits either because remaining == 0 (scrolled the full requested
	// amount and viewAnchor is now at a correct chain start) or because we
	// ran off the top of history. Only in the latter case do we pin to 0;
	// otherwise preserve the viewAnchor the loop computed.
	if remaining > 0 {
		v.viewAnchor = 0
		v.viewAnchorOffset = 0
	}
	v.viewBottom -= int64(walked)
	v.clampViewBottom()
}

// clampViewBottom enforces viewBottom >= height-1. Must be called with mu held.
func (v *ViewWindow) clampViewBottom() {
	minBottom := int64(v.height - 1)
	if v.viewBottom < minBottom {
		v.viewBottom = minBottom
	}
}

// ScrollDownRows moves the view forward by n reflowed rows toward the live
// edge. Walks chains the same way ScrollUpRows does but in the forward
// direction. Live-edge detection is based on how far we're currently scrolled
// back (writeBottom - viewBottom), not on incrementing viewBottom by n
// up-front — a velocity-multiplied n would otherwise overshoot and snap to the
// live edge after a single click. Only when the viewAnchor walk reaches a
// position that covers writeBottom do we re-engage autoFollow.
func (v *ViewWindow) ScrollDownRows(s *Store, n int, writeBottom int64) {
	if n <= 0 {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()

	// Cap n to however many rows we're currently scrolled back. Anything
	// beyond that is a snap to live edge.
	scrolledBack := writeBottom - v.viewBottom
	if scrolledBack < 0 {
		scrolledBack = 0
	}
	capped := int64(n)
	snapToLive := false
	if capped >= scrolledBack {
		capped = scrolledBack
		snapToLive = true
	}

	width := v.width
	reflowOff := v.globalReflowOff
	maxSteps := 4 * v.height
	if maxSteps < 4 {
		maxSteps = 4
	}
	remaining := int(capped)
	walked := 0
	for remaining > 0 {
		cells := s.GetLine(v.viewAnchor)
		if len(cells) == 0 && !s.RowNoWrap(v.viewAnchor) {
			// 1-row empty chain.
			avail := 1 - v.viewAnchorOffset
			if avail <= 0 {
				v.viewAnchor++
				v.viewAnchorOffset = 0
				continue
			}
			if remaining < avail {
				v.viewAnchorOffset += remaining
				walked += remaining
				remaining = 0
				break
			}
			remaining -= avail
			walked += avail
			v.viewAnchor++
			v.viewAnchorOffset = 0
			continue
		}
		end, nowrap := walkChain(s, v.viewAnchor, maxSteps)
		if reflowOff {
			nowrap = true
		}
		rows := chainReflowedRowCount(s, v.viewAnchor, end, width, nowrap)
		avail := rows - v.viewAnchorOffset
		if remaining < avail {
			v.viewAnchorOffset += remaining
			walked += remaining
			remaining = 0
			break
		}
		remaining -= avail
		walked += avail
		v.viewAnchor = end + 1
		v.viewAnchorOffset = 0
	}
	v.viewBottom += int64(walked)
	if snapToLive {
		v.viewBottom = writeBottom
		v.autoFollow = true
	}
}

// scrollUp is the legacy (globalIdx-unit) scroll-back used by tests only.
// Production scrolls go through Terminal.ScrollUp -> ScrollUpRows, which works
// in reflowed-row units and walks wrap chains. Kept private so test helpers
// can continue to exercise the anchor-decrement path.
func (v *ViewWindow) scrollUp(n int) {
	if n <= 0 {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.autoFollow = false
	v.viewBottom -= int64(n)
	minBottom := int64(v.height - 1)
	if v.viewBottom < minBottom {
		v.viewBottom = minBottom
	}
	v.viewAnchor -= int64(n)
	if v.viewAnchor < 0 {
		v.viewAnchor = 0
	}
	v.viewAnchorOffset = 0
}

// scrollDown is the legacy (globalIdx-unit) scroll-forward used by tests only.
// Production scrolls go through Terminal.ScrollDown -> ScrollDownRows.
func (v *ViewWindow) scrollDown(n int, writeBottom int64) {
	if n <= 0 {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.viewBottom += int64(n)
	v.viewAnchor += int64(n)
	v.viewAnchorOffset = 0
	if v.viewBottom >= writeBottom {
		v.viewBottom = writeBottom
		v.autoFollow = true
	}
}

// ScrollToBottom snaps viewBottom to writeBottom and re-engages autoFollow.
// viewAnchor is left alone here — the next RenderReflow will call
// RecomputeLiveAnchor (which now runs because autoFollow is true) and
// reposition the anchor to the cursor's chain.
func (v *ViewWindow) ScrollToBottom(writeBottom int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.viewBottom = writeBottom
	v.autoFollow = true
}

// OnInput is called when the user types or clicks in the pane. Re-engages
// autoFollow at the current writeBottom unless autoJumpOnInput has been
// disabled, in which case the current scroll position is preserved.
func (v *ViewWindow) OnInput(writeBottom int64) {
	v.mu.Lock()
	jump := v.autoJumpOnInput
	v.mu.Unlock()
	if !jump {
		return
	}
	v.ScrollToBottom(writeBottom)
}

// Resize changes the viewport dimensions. When autoFollow is active,
// viewBottom snaps to the write window's bottom so that the view always
// shows the same range the shell writes into. When scrolled back
// (autoFollow off), viewBottom stays fixed — the user's scroll position
// is preserved.
func (v *ViewWindow) Resize(newWidth, newHeight int, newWriteBottom int64) {
	if newWidth <= 0 || newHeight <= 0 {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.width = newWidth
	v.height = newHeight
	if v.autoFollow {
		v.viewBottom = newWriteBottom
	}
	// If expansion would show negative globalIdxs, raise viewBottom.
	minBottom := int64(v.height - 1)
	if v.viewBottom < minBottom {
		v.viewBottom = minBottom
	}
}

// WalkPolicy describes the outcome of WalkUpwardFromBottom.
type WalkPolicy uint8

const (
	// WalkPolicyAnchorInStore means viewBottom was resolvable in the store;
	// (anchor, offset) position the view as requested.
	WalkPolicyAnchorInStore WalkPolicy = iota
	// WalkPolicyMissingAnchor means viewBottom < Store.OldestRetained();
	// anchor is set to OldestRetained and the caller MUST force
	// autoFollow=false to honor the user's scroll-back intent (Policy A).
	WalkPolicyMissingAnchor
)

// WalkUpwardFromBottom walks chains upward starting from the wrapSeg-th sub-row
// of the chain containing viewBottom. It accumulates `rows` reflowed sub-rows
// at display width `width`, respecting NoWrap and the global-reflow-off
// override. Returns the (chain-head anchor, sub-row offset) pair to pass to
// ViewWindow.SetViewAnchor.
//
// Missing anchor: if viewBottom < Store.OldestRetained(), returns
// (OldestRetained(), 0, WalkPolicyMissingAnchor). Caller must force
// autoFollow=false before applying.
func WalkUpwardFromBottom(s *Store, viewBottom int64, wrapSeg uint16, rows, width int, reflowOff bool) (int64, int, WalkPolicy) {
	if viewBottom < s.OldestRetained() {
		return s.OldestRetained(), 0, WalkPolicyMissingAnchor
	}
	if rows <= 0 {
		return viewBottom, int(wrapSeg), WalkPolicyAnchorInStore
	}
	maxSteps := 4 * rows
	if maxSteps < 4 {
		maxSteps = 4
	}
	chainStart := findChainStart(s, viewBottom, maxSteps)
	end, nowrap := walkChain(s, chainStart, maxSteps)
	if reflowOff {
		nowrap = true
	}
	bottomSubRows := chainReflowedRowCount(s, chainStart, end, width, nowrap)
	ws := int(wrapSeg)
	if ws >= bottomSubRows {
		ws = bottomSubRows - 1
	}
	if ws < 0 {
		ws = 0
	}
	remaining := rows - (ws + 1)
	anchor := chainStart
	if remaining <= 0 {
		offset := ws - (rows - 1)
		if offset < 0 {
			offset = 0
		}
		return anchor, offset, WalkPolicyAnchorInStore
	}
	for remaining > 0 && anchor > 0 {
		prevGI := anchor - 1
		prevStart := findChainStart(s, prevGI, maxSteps)
		prevEnd, prevNoWrap := walkChain(s, prevStart, maxSteps)
		if reflowOff {
			prevNoWrap = true
		}
		prevRows := chainReflowedRowCount(s, prevStart, prevEnd, width, prevNoWrap)
		if prevRows >= remaining {
			anchor = prevStart
			offset := prevRows - remaining
			return anchor, offset, WalkPolicyAnchorInStore
		}
		remaining -= prevRows
		anchor = prevStart
	}
	if anchor < s.OldestRetained() {
		anchor = s.OldestRetained()
	}
	return anchor, 0, WalkPolicyAnchorInStore
}

// SetAutoFollow explicitly sets the autoFollow flag. Used on resume to
// honor the client's saved autoFollow state.
func (v *ViewWindow) SetAutoFollow(enabled bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.autoFollow = enabled
}

// ApplyResumeState atomically sets viewAnchor + viewAnchorOffset + viewBottom +
// autoFollow under a single mutex acquisition. Used by Terminal.RestoreViewport
// to prevent torn state: if the publisher's render path interleaves between
// separate SetViewAnchor / SetViewBottom / SetAutoFollow calls, RecomputeLiveAnchor
// can observe autoFollow=true (not yet updated) and clobber the just-written
// anchor back to live-edge. This method guarantees that by the time any reader
// observes a non-default autoFollow flag, the anchor + viewBottom also reflect
// the resumed state.
func (v *ViewWindow) ApplyResumeState(anchor int64, offset int, viewBottom int64, autoFollow bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.viewAnchor = anchor
	v.viewAnchorOffset = offset
	v.viewBottom = viewBottom
	v.clampViewBottom()
	v.autoFollow = autoFollow
}

// SetViewBottom explicitly positions viewBottom (the globalIdx of the
// bottom display row — for a wrapped chain, the chain-head gid the bottom
// row belongs to). Used by the resume path alongside SetViewAnchor /
// SetAutoFollow to bring all three internal pieces of state into a
// consistent post-restore configuration. Clamped to height-1 to avoid
// negative scroll semantics (via clampViewBottom, already in the file).
func (v *ViewWindow) SetViewBottom(viewBottom int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.viewBottom = viewBottom
	v.clampViewBottom()
}

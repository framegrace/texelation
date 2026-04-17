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

// SetGlobalReflowOff toggles reflow off globally. When true, all chains
// render 1:1 (clipped), ignoring the Wrapped flag.
func (v *ViewWindow) SetGlobalReflowOff(off bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.globalReflowOff = off
}

// Render projects the viewport by walking chains from viewAnchor. Each
// chain is reflowed to viewWidth (unless NoWrap or globalReflowOff is set,
// in which case rows render 1:1 via clipRow). Returns exactly viewHeight
// rows, padded with empty cells if content is exhausted.
func (v *ViewWindow) Render(s *Store) [][]parser.Cell {
	v.mu.Lock()
	width := v.width
	height := v.height
	anchor := v.viewAnchor
	skip := v.viewAnchorOffset
	reflowOff := v.globalReflowOff
	v.mu.Unlock()

	out := make([][]parser.Cell, 0, height)
	maxSteps := 4 * height
	if maxSteps < 4 {
		maxSteps = 4
	}

	gi := anchor
	first := true
	for len(out) < height {
		// Past content: stop.
		if s.GetLine(gi) == nil && !s.RowNoWrap(gi) {
			break
		}
		end, nowrap := walkChain(s, gi, maxSteps)

		var rows [][]parser.Cell
		if reflowOff || nowrap {
			for r := gi; r <= end; r++ {
				rows = append(rows, clipRow(s.GetLine(r), width))
			}
		} else {
			reflowed := reflowChain(s, gi, end, width)
			for _, row := range reflowed {
				rows = append(rows, clipRow(row, width))
			}
		}

		if first {
			first = false
			if skip < len(rows) {
				rows = rows[skip:]
			} else {
				rows = nil
			}
		}

		for _, row := range rows {
			if len(out) >= height {
				break
			}
			out = append(out, row)
		}
		gi = end + 1
	}

	for len(out) < height {
		out = append(out, make([]parser.Cell, width))
	}
	return out
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
		chainRows := int(end - gi + 1)
		if !nowrap {
			total := 0
			for r := gi; r <= end; r++ {
				total += len(s.GetLine(r))
			}
			if total == 0 {
				chainRows = 1
			} else {
				chainRows = (total + width - 1) / width
			}
		}
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
		var chainRows int
		if nowrap {
			chainRows = int(end - gi + 1)
		} else {
			total := 0
			for r := gi; r <= end; r++ {
				total += len(s.GetLine(r))
			}
			if total == 0 {
				chainRows = 1
			} else {
				chainRows = (total + width - 1) / width
			}
		}
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

// ScrollUp detaches from the live edge and moves viewBottom up by n lines.
// viewBottom is clamped to at least height-1 (can't show negative globalIdxs
// as the view bottom).
func (v *ViewWindow) ScrollUp(n int) {
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
}

// ScrollDown moves viewBottom down by n lines toward the live edge. writeBottom
// is the current WriteWindow bottom; ScrollDown will not move past it. If
// viewBottom reaches writeBottom, autoFollow is automatically re-engaged.
func (v *ViewWindow) ScrollDown(n int, writeBottom int64) {
	if n <= 0 {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.viewBottom += int64(n)
	if v.viewBottom >= writeBottom {
		v.viewBottom = writeBottom
		v.autoFollow = true
	}
}

// ScrollToBottom snaps viewBottom to writeBottom and re-engages autoFollow.
func (v *ViewWindow) ScrollToBottom(writeBottom int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.viewBottom = writeBottom
	v.autoFollow = true
}

// OnInput is called when the user types or clicks in the pane. Re-engages
// autoFollow at the current writeBottom.
func (v *ViewWindow) OnInput(writeBottom int64) {
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

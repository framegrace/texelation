// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/selection_wrap_resize_test.go
// Summary: Integration test driving applySelectionHighlightLocked across a
// width-change resize that re-wraps the selected line.
// Issue #224 plan, Task 16: confirms (gid, col) anchors held in
// SelectionStateMachine survive a resize because ContentToViewport projects
// them onto the post-resize wrap chain.

package texelterm

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	texelcore "github.com/framegrace/texelui/core"

	"github.com/framegrace/texelation/internal/theming"
)

// TestSelection_HighlightTracksWrappedContentAfterResize types a 100-char line
// that wraps at width 80, drives a double-click selection on the second visual
// row, captures the highlighted text, then narrows the terminal so the line
// re-wraps over more rows. The same selection (anchored in (gid, col) content
// coordinates) must paint over cells that contain the same text.
func TestSelection_HighlightTracksWrappedContentAfterResize(t *testing.T) {
	const (
		wideCols   = 80
		narrowCols = 40
		rows       = 24
	)

	tt := NewTestTerm(wideCols, rows)
	a := tt.term

	// Install the mouse coordinator so applySelectionHighlightLocked has
	// somewhere to consult for the active selection. NewTestTerm intentionally
	// skips the heavy initializeVTermFirstRun path (no PTY, no config), so we
	// wire the minimum surface ourselves.
	a.mouseCoordinator = NewMouseCoordinator(
		NewVTermAdapter(a.vterm),
		NewVTermGridAdapter(a.vterm),
		nil, // no wheel handler
		AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15},
	)
	a.mouseCoordinator.SetSize(wideCols, rows)
	a.mouseCoordinator.SetCallbacks(func() {}, func() {})

	// Type a 100-char single token (no separators, so a double-click selects
	// the whole token). At width 80 with autowrap on the line occupies row 0
	// (cols 0..79) and row 1 (cols 0..19).
	const line = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" +
		"0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijkl"
	if len(line) != 100 {
		t.Fatalf("test fixture: expected 100-char line, got %d", len(line))
	}
	tt.Write([]byte(line))

	// Force a render so mainScreenRowOrigin is populated. Render returns
	// vterm rows + a status-bar row; with NewTestTerm there is no statusBar,
	// so we just take rendered slice as-is.
	bufBefore := a.Render()
	if len(bufBefore) == 0 {
		t.Fatal("Render produced no buffer before resize")
	}

	// Drive a double-click on viewport row 1, col 5 — the wrap continuation
	// row. press → (synthesised single-click), press → (synthesised double-
	// click via DetectClick at the same row/col), release → finishes as a
	// multi-click selection that stays rendered.
	pressDouble(t, a.mouseCoordinator, 5, 1)

	bufHi := a.Render()
	highlightBg := highlightBgColor()

	cellsBefore := highlightedCells(bufHi, highlightBg)
	if len(cellsBefore) == 0 {
		t.Fatalf("no highlighted cells in pre-resize render; selection didn't activate")
	}
	textBefore := selectionText(bufHi, cellsBefore)
	if strings.TrimSpace(textBefore) == "" {
		t.Fatalf("highlighted text empty before resize; got runes=%q", textBefore)
	}

	// Sanity: selectionMachine should still be reporting the multi-click
	// selection — that's what carries the (gid,col) anchors across resize.
	if !a.mouseCoordinator.IsSelectionRendered() {
		t.Fatal("expected selection to remain rendered after double-click")
	}

	// Narrow the viewport. We bypass TexelTerm.Resize (which would touch
	// statusbar / config panel state we never initialised) and resize the
	// vterm + mouse coordinator directly. Either path is sufficient: the
	// invariant under test is that ContentToViewport, applied during the
	// next Render, projects the still-anchored (gid,col) onto the new
	// row layout.
	a.vterm.Resize(narrowCols, rows)
	a.mouseCoordinator.SetSize(narrowCols, rows)
	a.width = narrowCols
	a.height = rows

	bufAfter := a.Render()
	if len(bufAfter) == 0 {
		t.Fatal("Render produced no buffer after resize")
	}
	cellsAfter := highlightedCells(bufAfter, highlightBg)
	if len(cellsAfter) == 0 {
		t.Fatalf("highlight disappeared after resize — selection mapping regressed")
	}
	textAfter := selectionText(bufAfter, cellsAfter)

	// The text under the highlight must be identical before/after — same
	// content, just laid out across a different number of rows.
	if textAfter != textBefore {
		t.Errorf("highlighted text drifted after resize:\n  before=%q\n  after =%q",
			textBefore, textAfter)
	}
}

// pressDouble drives the mouseCoordinator with a button-press / button-release
// pair at (col, row), waits long enough for the click detector to escalate to
// a double click on the second press, and finishes the selection. Because the
// multi-click decision is owned entirely by ClickDetector inside the
// coordinator (timing-based), we synthesise two press/release cycles at the
// same coordinate.
func pressDouble(t *testing.T, m *MouseCoordinator, col, row int) {
	t.Helper()

	// First click: press + release.
	m.HandleMouse(tcell.NewEventMouse(col, row, tcell.Button1, 0))
	m.HandleMouse(tcell.NewEventMouse(col, row, tcell.ButtonNone, 0))

	// Second click: press + release. This second press is detected as
	// DoubleClick by ClickDetector (same row/col, within multi-click
	// timeout), which routes Start through SelectionModeSpaceWord →
	// StateMultiClickHeld and leaves the selection rendered after release.
	m.HandleMouse(tcell.NewEventMouse(col, row, tcell.Button1, 0))
	m.HandleMouse(tcell.NewEventMouse(col, row, tcell.ButtonNone, 0))
}

// highlightBgColor returns the configured selection.highlight_bg color, in the
// exact form applySelectionHighlightLocked uses (.TrueColor()) so equality
// comparisons against painted cells work.
func highlightBgColor() tcell.Color {
	cfg := theming.ForApp("texelterm")
	defaultBg := tcell.NewRGBColor(232, 217, 255)
	highlight := cfg.GetColor("selection", "highlight_bg", defaultBg)
	if !highlight.Valid() {
		highlight = defaultBg
	}
	return highlight.TrueColor()
}

// highlightedCells walks the rendered buffer and returns (y, x) for every
// cell whose background equals the configured highlight color. Used to find
// the painted selection extent without depending on the SelectionRange API.
func highlightedCells(buf [][]texelcore.Cell, hi tcell.Color) [][2]int {
	var out [][2]int
	for y, row := range buf {
		for x, cell := range row {
			_, bg, _ := cell.Style.Decompose()
			if bg == hi {
				out = append(out, [2]int{y, x})
			}
		}
	}
	return out
}

// selectionText reads the runes at the given highlighted cell positions in
// row-major order, joining consecutive cells on the same row and inserting a
// newline between rows. NUL runes (cleared cells) are skipped at the trailing
// edge of a row but preserved internally as spaces, matching what a user
// would see.
func selectionText(buf [][]texelcore.Cell, cells [][2]int) string {
	if len(cells) == 0 {
		return ""
	}
	// Group by row, collecting min/max x per row.
	rowMin := map[int]int{}
	rowMax := map[int]int{}
	for _, p := range cells {
		y, x := p[0], p[1]
		if v, ok := rowMin[y]; !ok || x < v {
			rowMin[y] = x
		}
		if v, ok := rowMax[y]; !ok || x > v {
			rowMax[y] = x
		}
	}
	// Emit rows in ascending y order.
	ys := make([]int, 0, len(rowMin))
	for y := range rowMin {
		ys = append(ys, y)
	}
	// Tiny insertion sort; we never have many rows here.
	for i := 1; i < len(ys); i++ {
		for j := i; j > 0 && ys[j-1] > ys[j]; j-- {
			ys[j-1], ys[j] = ys[j], ys[j-1]
		}
	}
	var b strings.Builder
	for i, y := range ys {
		if i > 0 {
			b.WriteByte('\n')
		}
		row := buf[y]
		for x := rowMin[y]; x <= rowMax[y] && x < len(row); x++ {
			r := row[x].Ch
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

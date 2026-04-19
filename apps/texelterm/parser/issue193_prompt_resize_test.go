// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Issue #193: prompt line wraps/unwraps on resize.
//
// Reproduces the bug from a real user session captured via
// TEXELTERM_CAPTURE. A powerline-style prompt right-aligns its tail
// segment with `ESC[500C ESC[17D` then writes 18 visible cells.
// At width 107, that fills cells [89..106] of the prompt's tail row.
// On shrink, the row reflows into multiple physical rows even though
// the logical line never wrapped — visible to the user as the prompt
// "scrolling up" by an extra row each shrink event.

package parser

import (
	"testing"
)

// powerlineTailFromCapture is the byte-for-byte tail of the right-aligned
// prompt segment from /tmp/prompt-resize.txrec.bytes (offsets 0x110..0x186),
// trailing CR/CR/LF included exactly as the shell emitted them.
//
//	ESC[500C ESC[17D <SGR> <chevron> <SGR> " 21:45:14 " <SGR>
//	<SGR> <chevron> <SGR> " marc " <SGR><SGR><SGR> CR CR LF
//
// 18 visible cells starting at col=width-17.
const powerlineTailFromCapture = "\x1b[500C\x1b[17D" +
	"\x1b[38;5;240m\ue0b2\x1b[0m" +
	"\x1b[48;5;240m 21:45:14 \x1b[0m\x1b[m\x1b[0m" +
	"\x1b[38;5;32;48;5;240m\ue0b2\x1b[0m" +
	"\x1b[48;5;32m marc \x1b[0m\x1b[m\x1b[0m" +
	"\r\r\n"

// TestIssue193_PowerlinePromptDoesNotWrapOnShrink: write a powerline
// right-aligned prompt at width 107, then resize narrower. The single
// logical row must stay a single physical row regardless of where the
// cursor ends up — the row's positional gap (cells 0..88 unwritten,
// 89..106 written) is detected from the row itself.
func TestIssue193_PowerlinePromptDoesNotWrapOnShrink(t *testing.T) {
	const initialWidth = 107
	const height = 36

	v := NewVTerm(initialWidth, height)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	parseString(p, powerlineTailFromCapture)

	// The trailing CR/LF moves the cursor down one row; the prompt row is
	// the row above the cursor.
	gi, _ := v.CursorGlobalIdx()
	promptRow := gi - 1
	cells := v.mainScreen.ReadLine(promptRow)
	if len(cells) == 0 {
		t.Fatalf("prompt row %d empty after write", promptRow)
	}
	if cells[len(cells)-1].Wrapped {
		t.Errorf("prompt row %d last cell has Wrapped=true; the powerline write should not have set Wrapped on its tail cell", promptRow)
	}
	t.Logf("prompt row %d: %d cells, last.Wrapped=%v", promptRow, len(cells), cells[len(cells)-1].Wrapped)

	// At the initial width 107, the row's content occupies cells [89..106]
	// — all visible content is in the right portion. After shrinking to a
	// width less than 89, the visible portion of the original row is all
	// blank (cols 0..viewWidth-1 hold no content). The "marc" / clock
	// segment lives at cells [89..106] which is OUTSIDE the new viewport.
	// Expected behavior on shrink: the prompt row stays a single physical
	// row, clipped to viewWidth, so any width < 89 renders as a blank
	// prompt row (content scrolled off the right). Bug: reflowChain treats
	// the 107-cell row as wrappable and slices it into ceil(107/w) physical
	// rows, surfacing the right-side content on a NEW row.

	for _, w := range []int{77, 76, 63, 40} {
		v.Resize(w, height)
		grid := v.Grid()

		// At width < 89, no original content is visible — the entire 18
		// visible-cell segment at cols 89..106 falls outside the viewport.
		if firstVisibleRow(grid) >= 0 {
			t.Errorf("at width %d, content from cells [89..106] surfaces on row %d (want clipped — no visible row)", w, firstVisibleRow(grid))
			dumpGrid(t, grid)
		}
	}
}

// firstVisibleRow returns the index of the first grid row containing a
// non-blank rune, or -1 if none.
func firstVisibleRow(grid [][]Cell) int {
	for i, row := range grid {
		if hasVisibleContent(row) {
			return i
		}
	}
	return -1
}

// hasVisibleContent returns true if any cell in the row has a non-space rune.
func hasVisibleContent(row []Cell) bool {
	for _, c := range row {
		if c.Rune != 0 && c.Rune != ' ' {
			return true
		}
	}
	return false
}

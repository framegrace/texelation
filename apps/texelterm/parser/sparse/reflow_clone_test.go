// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// End-to-end tests for the trimTrailingPadding + reflow path: drive a
// realistic Claude-like content set into the Store, render through
// ViewWindow at multiple widths, and check that no visual row appears
// duplicated in the rendered grid. The clone bug surfaces here if the
// trim's row count diverges from anywhere downstream.

package sparse

import (
	"strings"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// fillPaddedRow writes content + spaces-to-width as a single non-wrapping
// stored row (no Wrapped flag). Mirrors what a TUI like Claude does when
// it knows the viewport width and right-pads each line.
func fillPaddedRow(s *Store, gi int64, content string, width int) {
	if len(content) > width {
		content = content[:width]
	}
	cells := make([]parser.Cell, width)
	for i, r := range content {
		cells[i] = parser.Cell{Rune: r}
	}
	for i := len(content); i < width; i++ {
		cells[i] = parser.Cell{Rune: ' '}
	}
	s.SetLine(gi, cells)
}

// TestRender_PaddedLines_NoCloneAtWiderViewport — populate 24 padded lines
// at storage width 80, render through ViewWindow at the same width.
// No content row should appear duplicated.
func TestRender_PaddedLines_NoCloneAtWiderViewport(t *testing.T) {
	const storageWidth = 80
	const lineCount = 24
	s := NewStore(storageWidth)
	for i := int64(0); i < lineCount; i++ {
		fillPaddedRow(s, i, "Line-"+padNum(int(i), 2), storageWidth)
	}

	vw := NewViewWindow(storageWidth, lineCount)
	vw.SetViewAnchor(0, 0)
	out, _ := vw.Render(s)

	if len(out) != lineCount {
		t.Fatalf("got %d rows, want %d", len(out), lineCount)
	}

	seen := make(map[string]int, lineCount)
	for y, row := range out {
		text := strings.TrimRight(cellsToStringSparse(row), " ")
		if text == "" {
			continue
		}
		if first, dup := seen[text]; dup {
			t.Errorf("clone: %q appears at row %d AND row %d", text, first, y)
		}
		seen[text] = y
	}
}

// TestRender_PaddedLines_NoCloneAtNarrowerViewport — same content, but
// rendered at half the width. Each padded line should still be a single
// visible row (the trim removes the trailing pad). No clones.
func TestRender_PaddedLines_NoCloneAtNarrowerViewport(t *testing.T) {
	const storageWidth = 80
	const renderWidth = 40
	const lineCount = 24
	s := NewStore(storageWidth)
	for i := int64(0); i < lineCount; i++ {
		fillPaddedRow(s, i, "Line-"+padNum(int(i), 2), storageWidth)
	}

	vw := NewViewWindow(renderWidth, lineCount)
	vw.SetViewAnchor(0, 0)
	out, gi := vw.Render(s)

	if len(out) != lineCount {
		t.Fatalf("got %d rows, want %d", len(out), lineCount)
	}

	seen := make(map[string]int, lineCount)
	for y, row := range out {
		text := strings.TrimRight(cellsToStringSparse(row), " ")
		if text == "" {
			continue
		}
		if first, dup := seen[text]; dup {
			t.Errorf("clone: %q appears at row %d (gi=%d) AND row %d (gi=%d)",
				text, first, gi[first], y, gi[y])
		}
		seen[text] = y
	}
}

// TestRender_PaddedLines_ResizeTransition — render at width 80, then
// resize the view to width 40 and render again. Both renders must be
// internally consistent (no row duplication WITHIN a single render).
func TestRender_PaddedLines_ResizeTransition(t *testing.T) {
	const storageWidth = 80
	const lineCount = 24
	s := NewStore(storageWidth)
	for i := int64(0); i < lineCount; i++ {
		fillPaddedRow(s, i, "Line-"+padNum(int(i), 2), storageWidth)
	}

	// Initial render at 80.
	vw := NewViewWindow(storageWidth, lineCount)
	vw.SetViewAnchor(0, 0)
	out80, _ := vw.Render(s)
	checkNoClonesInRender(t, "width 80", out80)

	// Resize narrower, re-render.
	vw.Resize(40, lineCount, int64(lineCount-1))
	vw.SetViewAnchor(0, 0)
	out40, _ := vw.Render(s)
	checkNoClonesInRender(t, "width 40", out40)

	// Resize back to 80, re-render. Make sure post-resize render is also
	// clone-free (catches state leaking between renders).
	vw.Resize(80, lineCount, int64(lineCount-1))
	vw.SetViewAnchor(0, 0)
	out80b, _ := vw.Render(s)
	checkNoClonesInRender(t, "width 80 again", out80b)
}

// TestRender_AutoFollowResize_NoClones — same scenario but with
// auto-follow on, exercising RecomputeLiveAnchor's chainReflowedRowCount
// path. This is closer to what the texelterm app actually does each
// frame.
func TestRender_AutoFollowResize_NoClones(t *testing.T) {
	const storageWidth = 80
	const lineCount = 100 // more lines than the viewport so anchor walk runs
	s := NewStore(storageWidth)
	for i := int64(0); i < lineCount; i++ {
		fillPaddedRow(s, i, "Line-"+padNum(int(i), 3), storageWidth)
	}

	const viewHeight = 24
	vw := NewViewWindow(storageWidth, viewHeight)
	// Mimic auto-follow: cursor at the live edge, RecomputeLiveAnchor
	// will walk back from there.
	cursorGI := int64(lineCount - 1)
	vw.RecomputeLiveAnchor(s, cursorGI, 6, 0) // writeTop=0, cursor at last row col 6

	out80, _ := vw.Render(s)
	checkNoClonesInRender(t, "auto-follow width 80", out80)

	// Narrow the viewport.
	vw.Resize(40, viewHeight, cursorGI)
	vw.RecomputeLiveAnchor(s, cursorGI, 6, 0)
	out40, _ := vw.Render(s)
	checkNoClonesInRender(t, "auto-follow width 40", out40)

	// Widen back.
	vw.Resize(80, viewHeight, cursorGI)
	vw.RecomputeLiveAnchor(s, cursorGI, 6, 0)
	out80b, _ := vw.Render(s)
	checkNoClonesInRender(t, "auto-follow width 80 again", out80b)
}

func checkNoClonesInRender(t *testing.T, label string, out [][]parser.Cell) {
	t.Helper()
	seen := make(map[string]int, len(out))
	for y, row := range out {
		text := strings.TrimRight(cellsToStringSparse(row), " ")
		if text == "" {
			continue
		}
		if first, dup := seen[text]; dup {
			t.Errorf("%s: clone %q at rows %d and %d", label, text, first, y)
		}
		seen[text] = y
	}
}

func padNum(n, width int) string {
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// TestRender_AutoFollowResize_WrappedContent_NoClones — simulates the
// user's reported scenario: many chains with content that genuinely
// wraps when the viewport narrows (autowrap chain across 2 stored
// rows + trailing padding). Resize from wide to narrow with auto-
// follow, and check that the rendered viewport has no duplicate rows.
// This is the path most likely to hit a count mismatch between
// chainReflowedRowCount and reflowChain in the wrap+padding
// interaction.
func TestRender_AutoFollowResize_WrappedContent_NoClones(t *testing.T) {
	const storageWidth = 80
	const lineCount = 50
	s := NewStore(storageWidth)

	// Build 25 chains, each spanning 2 stored rows (autowrap-style).
	// Row 2k:   80 cells of distinguishable content "[k]...", last cell Wrapped=true.
	// Row 2k+1: 20 cells of content "/[k]" + 60 padding, last cell NOT Wrapped.
	// Content is distinguishable per chain AND per cell so spurious
	// "same row content" matches don't trigger the clone detector on
	// natural reflow of repeating chars.
	for k := int64(0); k < lineCount/2; k++ {
		gi := 2 * k
		row0 := make([]parser.Cell, 80)
		for i := 0; i < 80; i++ {
			row0[i] = parser.Cell{Rune: rune('A' + k%26 + (int64(i)/10)%2*32)} // alternating per 10
		}
		// Distinguishing prefix
		for i, r := range []rune{'k', '0' + rune(k/10), '0' + rune(k%10), ':'} {
			if i < len(row0) {
				row0[i] = parser.Cell{Rune: r}
			}
		}
		row0[len(row0)-1].Wrapped = true
		s.SetLine(gi, row0)

		row1 := make([]parser.Cell, 80)
		for i, r := range []rune{'/', '0' + rune(k/10), '0' + rune(k%10), '/'} {
			if i < 20 {
				row1[i] = parser.Cell{Rune: r}
			}
		}
		for i := 4; i < 20; i++ {
			row1[i] = parser.Cell{Rune: rune('a' + k%26)}
		}
		for i := 20; i < 80; i++ {
			row1[i] = parser.Cell{Rune: ' '}
		}
		s.SetLine(gi+1, row1)
	}

	const viewHeight = 24
	vw := NewViewWindow(storageWidth, viewHeight)
	cursorGI := int64(lineCount - 1)

	// Render at storage width (no narrowing). Validate via gi monotonicity:
	// real clones manifest as the same chain head gi appearing at non-
	// adjacent positions in the rendered output. Same-character chunks
	// from narrow-width reflow look identical but have monotone gi (all
	// from the same chain), so this is the correct invariant to check.
	vw.RecomputeLiveAnchor(s, cursorGI, 19, 0)
	_, gi80 := vw.Render(s)
	checkGIMonotone(t, "wrap+padding width 80", gi80)

	// Narrow to 40 — content width 100 chars per chain reflows to 3 rows.
	vw.Resize(40, viewHeight, cursorGI)
	vw.RecomputeLiveAnchor(s, cursorGI, 19, 0)
	_, gi40 := vw.Render(s)
	checkGIMonotone(t, "wrap+padding width 40", gi40)

	// Narrow to 5 — extreme case (very narrow).
	vw.Resize(5, viewHeight, cursorGI)
	vw.RecomputeLiveAnchor(s, cursorGI, 4, 0)
	_, gi5 := vw.Render(s)
	checkGIMonotone(t, "wrap+padding width 5", gi5)
}

// checkGIMonotone verifies the rendered gi sequence is non-decreasing
// (ignoring -1 padding rows). Real clones (the same chain rendered at
// two different viewport positions with content in between) would surface
// as a gi value reappearing after a higher one.
func checkGIMonotone(t *testing.T, label string, gis []int64) {
	t.Helper()
	prev := int64(-1)
	for y, g := range gis {
		if g < 0 {
			continue
		}
		if g < prev {
			t.Errorf("%s: gi sequence non-monotone at row %d: gi=%d but previous non-pad gi=%d (clone candidate)",
				label, y, g, prev)
		}
		prev = g
	}
}


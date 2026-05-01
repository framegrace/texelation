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

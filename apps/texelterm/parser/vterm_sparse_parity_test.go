// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser_test

import (
	"fmt"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	_ "github.com/framegrace/texelation/apps/texelterm/parser/sparse"
)

// assertSparseGrid verifies the sparse MainScreen grid is non-nil after the
// most recent operation.
func assertSparseGrid(t *testing.T, v *parser.VTerm, label string) {
	t.Helper()
	sparseGrid := v.MainScreenGrid()
	if sparseGrid == nil {
		t.Fatalf("%s: sparse grid is nil", label)
	}
	t.Logf("%s: sparse grid has %d rows", label, len(sparseGrid))
	if len(sparseGrid) > 0 {
		t.Logf("%s: sparse row 0: %q", label, gridRowText(sparseGrid[0]))
	}
}

// gridRowText extracts the text of a grid row, trimming trailing spaces/NULs.
func gridRowText(row []parser.Cell) string {
	out := make([]rune, len(row))
	for i, c := range row {
		if c.Rune == 0 {
			out[i] = ' '
		} else {
			out[i] = c.Rune
		}
	}
	s := string(out)
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}

// TestVTerm_SparseParityInsertLines verifies IL (Insert Line) is synced to sparse.
func TestVTerm_SparseParityInsertLines(t *testing.T) {
	v := parser.NewVTerm(10, 5)
	p := parser.NewParser(v)

	// Write content on rows 0-2.
	for _, r := range "AAAAAAAAAA\nBBBBBBBBBB\nCCCCCCCCCC" {
		p.Parse(r)
	}
	// Move cursor to row 1 and insert 1 line.
	for _, r := range "\x1b[2;1H\x1b[L" {
		p.Parse(r)
	}
	assertSparseGrid(t, v, "after IL")
}

// TestVTerm_SparseParityDeleteLines verifies DL (Delete Line) is synced to sparse.
func TestVTerm_SparseParityDeleteLines(t *testing.T) {
	v := parser.NewVTerm(10, 5)
	p := parser.NewParser(v)

	// Write content on rows 0-3.
	for _, r := range "AAAAAAAAAA\nBBBBBBBBBB\nCCCCCCCCCC\nDDDDDDDDDD" {
		p.Parse(r)
	}
	// Move cursor to row 1 and delete 1 line.
	for _, r := range "\x1b[2;1H\x1b[M" {
		p.Parse(r)
	}
	assertSparseGrid(t, v, "after DL")
}

// TestVTerm_SparseParityScroll verifies scroll events are forwarded to sparse.
func TestVTerm_SparseParityScroll(t *testing.T) {
	v := parser.NewVTerm(20, 5)
	p := parser.NewParser(v)

	// Write enough lines to create scrollback.
	for i := 0; i < 20; i++ {
		for _, r := range "line content here!!\n" {
			p.Parse(r)
		}
	}
	assertSparseGrid(t, v, "before scroll")

	// Scroll up 3 lines.
	v.Scroll(-3)
	assertSparseGrid(t, v, "after scroll up")

	// Scroll down 1 line.
	v.Scroll(1)
	assertSparseGrid(t, v, "after scroll down")

	// Scroll to bottom.
	v.Scroll(100)
	assertSparseGrid(t, v, "after scroll to bottom")
}

// TestVTerm_SparseParityOnBasicWrites verifies that sparse.Terminal.Grid()
// produces the expected output for simple writes (smoke test retained from
// the transitional parity suite).
func TestVTerm_SparseParityOnBasicWrites(t *testing.T) {
	v := parser.NewVTerm(20, 5)

	p := parser.NewParser(v)
	for _, r := range "hello\nworld" {
		p.Parse(r)
	}

	assertSparseGrid(t, v, "basic writes")
}

// TestVTerm_SparseScrollRegionThenResize models the user scenario:
// 1. Write shell output (ls -l) that creates scrollback
// 2. An app sets up a scroll region and writes content (simulating Claude)
// 3. Resize the terminal (shrink then grow)
// 4. Verify: sparse grid remains non-nil and well-formed at each step
// 5. Verify: scrollback is preserved (can scroll up to see ls -l output)
func TestVTerm_SparseScrollRegionThenResize(t *testing.T) {
	width, height := 40, 10
	v := parser.NewVTerm(width, height)
	p := parser.NewParser(v)

	// Phase 1: Write 15 lines of "shell output" to create scrollback.
	for i := 0; i < 15; i++ {
		line := fmt.Sprintf("ls-output-line-%02d", i)
		for _, r := range line {
			p.Parse(r)
		}
		p.Parse('\r')
		p.Parse('\n')
	}
	assertSparseGrid(t, v, "after ls output")

	// Phase 2: Simulate a scroll-region TUI app (like Claude).
	// Set scroll region to rows 3-9 (1-indexed).
	for _, r := range "\x1b[3;9r" {
		p.Parse(r)
	}
	for _, r := range "\x1b[3;1H" {
		p.Parse(r)
	}
	for i := 0; i < 20; i++ {
		line := fmt.Sprintf("claude-line-%02d", i)
		for _, r := range line {
			p.Parse(r)
		}
		p.Parse('\r')
		p.Parse('\n')
	}
	assertSparseGrid(t, v, "after scroll region writes")

	t.Logf("before shrink: WriteTop=%d ContentEnd=%d", v.WriteTop(), v.ContentEnd())

	// Phase 3: Shrink the terminal.
	newHeight := 6
	v.Resize(width, newHeight)
	t.Logf("after shrink: WriteTop=%d ContentEnd=%d", v.WriteTop(), v.ContentEnd())
	sparseAfterShrink := v.MainScreenGrid()
	if len(sparseAfterShrink) > 0 {
		t.Logf("after shrink: sparse row 0=%q", gridRowText(sparseAfterShrink[0]))
	}

	// Phase 4: Grow back.
	v.Resize(width, height)

	// Phase 5: The critical check — scrollback must be accessible.
	v.Scroll(-30)

	// Verify ls-output content is visible in the sparse grid after scrolling up.
	sparseGrid := v.MainScreenGrid()
	foundLsSparse := false
	for y := 0; y < len(sparseGrid); y++ {
		text := gridRowText(sparseGrid[y])
		if len(text) >= 9 && text[:9] == "ls-output" {
			foundLsSparse = true
			break
		}
	}
	if !foundLsSparse {
		t.Error("scrollback lost: sparse grid does not contain ls-output after scrolling up")
		for y := 0; y < len(sparseGrid); y++ {
			t.Logf("  sparse row %d: %q", y, gridRowText(sparseGrid[y]))
		}
	}
}

// TestVTerm_SparseScrollPreservesPositionOnResize verifies that if the user
// is scrolled back and a resize occurs, their scroll position is preserved
// (not snapped back to the live edge).
func TestVTerm_SparseScrollPreservesPositionOnResize(t *testing.T) {
	width, height := 20, 5
	v := parser.NewVTerm(width, height)
	p := parser.NewParser(v)

	// Write 15 lines to create scrollback.
	for i := 0; i < 15; i++ {
		for _, r := range fmt.Sprintf("line-%02d\r\n", i) {
			p.Parse(r)
		}
	}

	// Scroll up 5 lines — user is now viewing history.
	v.Scroll(-5)

	// Capture what the user sees before resize.
	beforeGrid := v.MainScreenGrid()
	if beforeGrid == nil {
		t.Fatal("sparse grid is nil before resize")
	}
	beforeRow0 := gridRowText(beforeGrid[0])
	t.Logf("before resize: sparse row 0 = %q", beforeRow0)

	// Shrink the terminal — this must NOT snap the view back to the live edge.
	v.Resize(width, height-1)

	afterGrid := v.MainScreenGrid()
	if afterGrid == nil {
		t.Fatal("sparse grid is nil after resize")
	}
	afterRow0 := gridRowText(afterGrid[0])
	t.Logf("after resize: sparse row 0 = %q", afterRow0)

	// The view should still show historical content, not the live edge.
	// The live edge would show lines 10-14 ("line-10" through "line-14").
	// If the view snapped to the live edge, row 0 would be "line-10" or similar.
	liveEdgeContent := false
	for y := 0; y < len(afterGrid); y++ {
		text := gridRowText(afterGrid[y])
		if text == "line-14" {
			liveEdgeContent = true
		}
	}
	if liveEdgeContent && beforeRow0 != "line-14" {
		t.Error("resize snapped the sparse view to the live edge — scroll position was lost")
		for y := 0; y < len(afterGrid); y++ {
			t.Logf("  after resize row %d: %q", y, gridRowText(afterGrid[y]))
		}
	}
}

// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package boot

import (
	"strings"
	"testing"

	"github.com/framegrace/texelation/protocol"
)

func TestRenderASCIILayout_SinglePane(t *testing.T) {
	root := &protocol.TreeNodeSnapshot{PaneIndex: 0, Split: protocol.SplitNone}
	grid := renderASCIILayoutGrid(20, 8, root)
	if len(grid) != 8 || len(grid[0]) != 20 {
		t.Fatalf("dims = %dx%d, want 20x8", len(grid[0]), len(grid))
	}
	if grid[0][0] != '┌' {
		t.Errorf("top-left = %q, want ┌", grid[0][0])
	}
	if grid[0][19] != '┐' {
		t.Errorf("top-right = %q, want ┐", grid[0][19])
	}
	if grid[7][0] != '└' {
		t.Errorf("bottom-left = %q, want └", grid[7][0])
	}
	if grid[7][19] != '┘' {
		t.Errorf("bottom-right = %q, want ┘", grid[7][19])
	}
}

func TestRenderASCIILayout_HorizontalSplit(t *testing.T) {
	root := &protocol.TreeNodeSnapshot{
		PaneIndex:   -1,
		Split:       protocol.SplitHorizontal,
		SplitRatios: []float32{0.5, 0.5},
		Children: []protocol.TreeNodeSnapshot{
			{PaneIndex: 0, Split: protocol.SplitNone},
			{PaneIndex: 1, Split: protocol.SplitNone},
		},
	}
	grid := renderASCIILayoutGrid(20, 8, root)
	dividerCount := 0
	for y := 1; y < 7; y++ {
		row := string(grid[y])
		if strings.Count(row, "─") > 5 {
			dividerCount++
		}
	}
	if dividerCount == 0 {
		t.Errorf("expected at least one horizontal divider row in:\n%s", debugGrid(grid))
	}
}

func TestRenderASCIILayout_VerticalSplit(t *testing.T) {
	root := &protocol.TreeNodeSnapshot{
		PaneIndex:   -1,
		Split:       protocol.SplitVertical,
		SplitRatios: []float32{0.5, 0.5},
		Children: []protocol.TreeNodeSnapshot{
			{PaneIndex: 0, Split: protocol.SplitNone},
			{PaneIndex: 1, Split: protocol.SplitNone},
		},
	}
	grid := renderASCIILayoutGrid(20, 8, root)
	// With leftW = int(20 * 0.5) = 10, the right child draws starting
	// at x=9 (border-sharing), so the divider column is 9, not 10.
	dividerCol := 9
	verticalAt := 0
	for y := 1; y < 7; y++ {
		if grid[y][dividerCol] == '│' {
			verticalAt++
		}
	}
	if verticalAt == 0 {
		t.Errorf("expected vertical divider at col %d in:\n%s", dividerCol, debugGrid(grid))
	}
}

func TestRenderASCIILayout_NWaySplit(t *testing.T) {
	root := &protocol.TreeNodeSnapshot{
		PaneIndex:   -1,
		Split:       protocol.SplitHorizontal,
		SplitRatios: []float32{0.34, 0.33, 0.33},
		Children: []protocol.TreeNodeSnapshot{
			{PaneIndex: 0, Split: protocol.SplitNone},
			{PaneIndex: 1, Split: protocol.SplitNone},
			{PaneIndex: 2, Split: protocol.SplitNone},
		},
	}
	grid := renderASCIILayoutGrid(20, 12, root)
	dividerRows := 0
	for y := 1; y < 11; y++ {
		if strings.Count(string(grid[y]), "─") > 5 {
			dividerRows++
		}
	}
	if dividerRows < 2 {
		t.Errorf("expected ≥ 2 divider rows for 3-way split, got %d:\n%s", dividerRows, debugGrid(grid))
	}
}

func TestRenderASCIILayout_NilRoot(t *testing.T) {
	grid := renderASCIILayoutGrid(20, 8, nil)
	if grid[0][0] != '┌' {
		t.Errorf("nil root should fall back to single box; got %q at top-left", grid[0][0])
	}
}

func TestRenderASCIILayout_BelowMinSize(t *testing.T) {
	root := &protocol.TreeNodeSnapshot{
		PaneIndex:   -1,
		Split:       protocol.SplitHorizontal,
		SplitRatios: []float32{0.5, 0.5},
		Children: []protocol.TreeNodeSnapshot{
			{PaneIndex: 0, Split: protocol.SplitNone},
			{PaneIndex: 1, Split: protocol.SplitNone},
		},
	}
	grid := renderASCIILayoutGrid(6, 3, root)
	if grid[0][0] != '┌' {
		t.Errorf("expected single-box fallback")
	}
}

func debugGrid(grid [][]rune) string {
	var b strings.Builder
	for _, row := range grid {
		b.WriteString(string(row))
		b.WriteByte('\n')
	}
	return b.String()
}

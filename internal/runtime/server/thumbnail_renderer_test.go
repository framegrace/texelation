// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"testing"

	texelcore "github.com/framegrace/texelui/core"
)

func TestComposePaneGrid_SinglePane(t *testing.T) {
	pane := paneRender{
		x: 0, y: 0, w: 4, h: 2,
		rows: [][]texelcore.Cell{
			{{Ch: 'a'}, {Ch: 'b'}, {Ch: 'c'}, {Ch: 'd'}},
			{{Ch: 'e'}, {Ch: 'f'}, {Ch: 'g'}, {Ch: 'h'}},
		},
	}
	grid := composePaneGrid(4, 2, []paneRender{pane})
	if len(grid) != 2 || len(grid[0]) != 4 {
		t.Fatalf("dims = %dx%d, want 4x2", len(grid[0]), len(grid))
	}
	if grid[0][0].Ch != 'a' || grid[1][3].Ch != 'h' {
		t.Errorf("content mismatch: grid=%v", grid)
	}
}

func TestComposePaneGrid_TwoPanesSideBySide(t *testing.T) {
	left := paneRender{
		x: 0, y: 0, w: 2, h: 1,
		rows: [][]texelcore.Cell{{{Ch: 'L'}, {Ch: '1'}}},
	}
	right := paneRender{
		x: 2, y: 0, w: 2, h: 1,
		rows: [][]texelcore.Cell{{{Ch: 'R'}, {Ch: '1'}}},
	}
	grid := composePaneGrid(4, 1, []paneRender{left, right})
	if grid[0][0].Ch != 'L' || grid[0][1].Ch != '1' {
		t.Errorf("left mis-painted: %v", grid[0])
	}
	if grid[0][2].Ch != 'R' || grid[0][3].Ch != '1' {
		t.Errorf("right mis-painted: %v", grid[0])
	}
}

func TestComposePaneGrid_OverflowClipped(t *testing.T) {
	pane := paneRender{
		x: 0, y: 0, w: 10, h: 1,
		rows: [][]texelcore.Cell{{{Ch: 'x'}, {Ch: 'y'}, {Ch: 'z'}}},
	}
	grid := composePaneGrid(10, 1, []paneRender{pane})
	if grid[0][0].Ch != 'x' || grid[0][1].Ch != 'y' || grid[0][2].Ch != 'z' {
		t.Errorf("front-pad mismatch: %v", grid[0])
	}
	for i := 3; i < 10; i++ {
		if grid[0][i].Ch != ' ' && grid[0][i].Ch != 0 {
			t.Errorf("expected blank at col %d, got %q", i, grid[0][i].Ch)
		}
	}
}

func TestComposePaneGrid_OutOfBoundsPaneIgnored(t *testing.T) {
	pane := paneRender{
		x: 100, y: 100, w: 4, h: 1,
		rows: [][]texelcore.Cell{{{Ch: 'a'}, {Ch: 'b'}, {Ch: 'c'}, {Ch: 'd'}}},
	}
	_ = composePaneGrid(4, 1, []paneRender{pane}) // should not panic
}

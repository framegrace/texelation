// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser_test

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	_ "github.com/framegrace/texelation/apps/texelterm/parser/sparse"
)

// normalizeRune treats zero rune the same as space for comparison purposes.
// The legacy grid fills unwritten cells with ' ' while the sparse grid
// leaves them as '\x00'.
func normalizeRune(r rune) rune {
	if r == 0 {
		return ' '
	}
	return r
}

// TestVTerm_SparseParityOnBasicWrites verifies that during the integration
// window the legacy memoryBufferGrid() and the new sparse.Terminal.Grid()
// produce the same output for simple writes.
func TestVTerm_SparseParityOnBasicWrites(t *testing.T) {
	v := parser.NewVTerm(20, 5)

	p := parser.NewParser(v)
	for _, r := range "hello\nworld" {
		p.Parse(r)
	}

	// Grid() returns the legacy grid (via memoryBufferGrid).
	// MainScreenGrid() returns the sparse grid.
	legacyGrid := v.Grid()
	sparseGrid := v.MainScreenGrid()

	if sparseGrid == nil {
		t.Fatal("sparse grid is nil — MainScreenFactory not registered")
	}

	if len(legacyGrid) != len(sparseGrid) {
		t.Fatalf("row count mismatch: legacy=%d sparse=%d",
			len(legacyGrid), len(sparseGrid))
	}
	for y := range legacyGrid {
		if len(legacyGrid[y]) != len(sparseGrid[y]) {
			t.Errorf("row %d width mismatch: legacy=%d sparse=%d",
				y, len(legacyGrid[y]), len(sparseGrid[y]))
			continue
		}
		for x := range legacyGrid[y] {
			lr := normalizeRune(legacyGrid[y][x].Rune)
			sr := normalizeRune(sparseGrid[y][x].Rune)
			if lr != sr {
				t.Errorf("cell (%d,%d): legacy=%q sparse=%q",
					x, y, lr, sr)
			}
		}
	}
}

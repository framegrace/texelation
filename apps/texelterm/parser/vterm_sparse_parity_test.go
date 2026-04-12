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

// assertGridParity compares the legacy grid against the sparse grid,
// normalizing NUL→space differences. Returns true if they match.
func assertGridParity(t *testing.T, v *parser.VTerm, label string) {
	t.Helper()
	legacyGrid := v.Grid()
	sparseGrid := v.MainScreenGrid()
	if sparseGrid == nil {
		t.Fatalf("%s: sparse grid is nil", label)
	}
	if len(legacyGrid) != len(sparseGrid) {
		t.Fatalf("%s: row count mismatch: legacy=%d sparse=%d",
			label, len(legacyGrid), len(sparseGrid))
	}
	for y := range legacyGrid {
		if len(legacyGrid[y]) != len(sparseGrid[y]) {
			t.Errorf("%s: row %d width mismatch: legacy=%d sparse=%d",
				label, y, len(legacyGrid[y]), len(sparseGrid[y]))
			continue
		}
		for x := range legacyGrid[y] {
			lr := normalizeRune(legacyGrid[y][x].Rune)
			sr := normalizeRune(sparseGrid[y][x].Rune)
			if lr != sr {
				t.Errorf("%s: cell (%d,%d): legacy=%q sparse=%q",
					label, x, y, lr, sr)
			}
		}
	}
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
	assertGridParity(t, v, "after IL")
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
	assertGridParity(t, v, "after DL")
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

	assertGridParity(t, v, "basic writes")
}

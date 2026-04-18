// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// When a row is shifted within a scroll region (via IL/DL/NewlineInRegion),
// its NoWrap flag must travel with its cells, not stay on the old globalIdx.
func TestStore_RowShift_NoWrapFollows(t *testing.T) {
	s := NewStore(80)
	s.Set(10, 0, parser.Cell{Rune: 'A'})
	s.SetRowNoWrap(10, true)

	// Simulate "move row at 10 to 11" via explicit shift helpers.
	cells := s.GetLine(10)
	nw := s.RowNoWrap(10)
	s.SetLineWithNoWrap(11, cells, nw)
	s.ClearRange(10, 10)

	if !s.RowNoWrap(11) {
		t.Errorf("NoWrap must follow cells to new row")
	}
	if s.RowNoWrap(10) {
		t.Errorf("old row should be cleared (NoWrap=false)")
	}
}

// TestWriteWindow_InsertLines_NoWrapFollows verifies that when InsertLines
// shifts rows down, the NoWrap flag travels with the cells.
func TestWriteWindow_InsertLines_NoWrapFollows(t *testing.T) {
	s := NewStore(80)
	ww := NewWriteWindow(s, 80, 10)

	// Write content on rows 2 and 3 (both relative to writeTop=0).
	s.Set(2, 0, parser.Cell{Rune: 'A'})
	s.SetRowNoWrap(2, true)
	s.Set(3, 0, parser.Cell{Rune: 'B'})
	// Row 3 does NOT have NoWrap.

	// Insert 1 blank line at cursorRow=2, within scroll region [0, 9].
	// This should shift row 2 -> 3, row 3 -> 4. Row 2 is cleared (blank inserted).
	ww.InsertLines(1, 2, 0, 9)

	// Row 3 (was row 2) must carry NoWrap.
	if !s.RowNoWrap(3) {
		t.Errorf("after InsertLines: row 3 (was row 2) should have NoWrap=true")
	}
	// Row 2 is the newly-inserted blank — NoWrap must be false.
	if s.RowNoWrap(2) {
		t.Errorf("after InsertLines: newly-inserted row 2 should have NoWrap=false")
	}
	// Row 4 (was row 3) must NOT have NoWrap.
	if s.RowNoWrap(4) {
		t.Errorf("after InsertLines: row 4 (was row 3) should have NoWrap=false")
	}
	// Content must have moved with NoWrap.
	if got := s.Get(3, 0).Rune; got != 'A' {
		t.Errorf("cell at row 3, col 0 = %q, want 'A'", got)
	}
}

// TestWriteWindow_DeleteLines_NoWrapFollows verifies that when DeleteLines
// shifts rows up, the NoWrap flag travels with the cells.
func TestWriteWindow_DeleteLines_NoWrapFollows(t *testing.T) {
	s := NewStore(80)
	ww := NewWriteWindow(s, 80, 10)

	// Write content on row 3 with NoWrap, and row 2 without.
	s.Set(2, 0, parser.Cell{Rune: 'X'})
	// Row 2 does NOT have NoWrap.
	s.Set(3, 0, parser.Cell{Rune: 'Y'})
	s.SetRowNoWrap(3, true)

	// Delete 1 line at cursorRow=2, within scroll region [0, 9].
	// This shifts row 3 -> 2, row 4 -> 3, etc. Bottom row (9) is cleared.
	ww.DeleteLines(1, 2, 0, 9)

	// Row 2 (was row 3) must carry NoWrap.
	if !s.RowNoWrap(2) {
		t.Errorf("after DeleteLines: row 2 (was row 3) should have NoWrap=true")
	}
	// Content must have moved.
	if got := s.Get(2, 0).Rune; got != 'Y' {
		t.Errorf("cell at row 2, col 0 = %q, want 'Y'", got)
	}
}

// TestWriteWindow_NewlineInRegion_NoWrapFollows verifies that when
// NewlineInRegion scrolls a region, the NoWrap flag travels with cells.
func TestWriteWindow_NewlineInRegion_NoWrapFollows(t *testing.T) {
	s := NewStore(80)
	ww := NewWriteWindow(s, 80, 10)

	// Row 5 has content and NoWrap; row 4 does not.
	s.Set(4, 0, parser.Cell{Rune: 'P'})
	s.Set(5, 0, parser.Cell{Rune: 'Q'})
	s.SetRowNoWrap(5, true)

	// Newline in region [3, 6]: rows shift up by 1, bottom (6) is cleared.
	// Row 5 -> 4, row 6 -> 5, row 6 cleared.
	ww.NewlineInRegion(3, 6)

	// Row 4 (was row 5) must carry NoWrap.
	if !s.RowNoWrap(4) {
		t.Errorf("after NewlineInRegion: row 4 (was row 5) should have NoWrap=true")
	}
	// Row 6 is the newly-cleared bottom — must not have NoWrap.
	if s.RowNoWrap(6) {
		t.Errorf("after NewlineInRegion: cleared row 6 should have NoWrap=false")
	}
	// Content must have moved.
	if got := s.Get(4, 0).Rune; got != 'Q' {
		t.Errorf("cell at row 4, col 0 = %q, want 'Q'", got)
	}
}

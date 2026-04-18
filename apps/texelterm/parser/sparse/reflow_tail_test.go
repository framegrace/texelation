// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"strings"
	"testing"
)

// TestCursor_EmptyContinuationAfterWrappedFullRow reproduces the "cursor
// jumps below the prompt on clear lines after resize" bug. When a chain has
// a Wrapped full row followed by an empty continuation row (cursor just
// newlined past a wrap, hasn't written yet), the reflow concatenation drops
// the empty row's implicit physical slot — so the cursor's computed row
// overshoots by one. reflowChain must emit a blank row for the trailing
// empty continuation, and the row-count helper must count it.
func TestCursor_EmptyContinuationAfterWrappedFullRow(t *testing.T) {
	s := NewStore(80)
	// Line 0: 80 cells, Wrapped (fills the row; cursor would newline past).
	text80 := strings.Repeat("x", 80)
	fillRow(s, 0, text80, true)
	// Line 1: empty — cursor sits at (1, 0) awaiting input.
	s.SetLine(1, nil)

	vw := NewViewWindow(80, 5)
	vw.SetViewAnchor(0, 0)

	// Cursor at (1, 0): visually one row below line 0.
	vr, vc, ok := vw.CursorToView(s, 1, 0)
	if !ok {
		t.Fatalf("CursorToView(1,0)=ok=false, want ok=true")
	}
	if vr != 1 || vc != 0 {
		t.Errorf("CursorToView(1,0)=(%d,%d); want (1,0)", vr, vc)
	}
}

// TestCursor_EmptyContinuationNarrower simulates a resize that forces the
// prompt row to wrap, putting the cursor on an empty continuation row.
func TestCursor_EmptyContinuationNarrower(t *testing.T) {
	s := NewStore(80)
	// Line 0: 80 cells Wrapped; line 1 empty — cursor at (1, 0).
	text80 := strings.Repeat("x", 80)
	fillRow(s, 0, text80, true)
	s.SetLine(1, nil)

	// Reflow at width 40: line 0 → 2 reflowed rows; cursor's continuation
	// should be row 2 (the third row), col 0.
	vw := NewViewWindow(40, 5)
	vw.SetViewAnchor(0, 0)

	vr, vc, ok := vw.CursorToView(s, 1, 0)
	if !ok {
		t.Fatalf("CursorToView(1,0)=ok=false, want ok=true")
	}
	if vr != 2 || vc != 0 {
		t.Errorf("CursorToView(1,0)@w40=(%d,%d); want (2,0)", vr, vc)
	}
}

// TestCursor_EmptyContinuationWider exercises the width-dependent cursor-flip:
// when the view is wider than the stored line, the Wrapped flag on line 0
// still conceptually means "followed by a continuation row" but the physical
// cells now fit in 1 row — so the cursor on gi=1 (empty) must land on row 1,
// not row 0. Previously logicalCol=80/width was < 1, putting the cursor back
// on row 0 (visually above the actual cursor position).
func TestCursor_EmptyContinuationWider(t *testing.T) {
	s := NewStore(80)
	text80 := strings.Repeat("x", 80)
	fillRow(s, 0, text80, true)
	s.SetLine(1, nil)

	// Width 81: content fits row 0 (80 cells), trailing empty row is row 1.
	vw := NewViewWindow(81, 5)
	vw.SetViewAnchor(0, 0)

	vr, vc, ok := vw.CursorToView(s, 1, 0)
	if !ok {
		t.Fatalf("CursorToView(1,0)=ok=false, want ok=true")
	}
	if vr != 1 || vc != 0 {
		t.Errorf("CursorToView(1,0)@w81=(%d,%d); want (1,0)", vr, vc)
	}
}

// TestCursor_EmptyContinuationPartialLine tests a partial-length Wrapped line
// followed by empty continuation. Stored content: 50 cells Wrapped, then empty.
// At width 80: content=1 row, trailing=1 row. Cursor at (gi=1,col=0) must land
// on row 1 (the trailing empty).
func TestCursor_EmptyContinuationPartialLine(t *testing.T) {
	s := NewStore(80)
	text50 := strings.Repeat("x", 50)
	fillRow(s, 0, text50, true)
	s.SetLine(1, nil)

	vw := NewViewWindow(80, 5)
	vw.SetViewAnchor(0, 0)

	vr, vc, ok := vw.CursorToView(s, 1, 0)
	if !ok {
		t.Fatalf("CursorToView(1,0)=ok=false, want ok=true")
	}
	if vr != 1 || vc != 0 {
		t.Errorf("CursorToView(1,0) partial @w80=(%d,%d); want (1,0)", vr, vc)
	}
}

// TestRender_EmptyContinuationEmitsRow ensures Render produces an actual
// physical row for the empty continuation — otherwise the cursor position
// above would match what's painted, but the row below line 0 would be the
// NEXT chain's content (visually wrong).
func TestRender_EmptyContinuationEmitsRow(t *testing.T) {
	s := NewStore(80)
	text80 := strings.Repeat("x", 80)
	fillRow(s, 0, text80, true)
	s.SetLine(1, nil)
	fillRow(s, 2, "next", false)

	vw := NewViewWindow(80, 5)
	vw.SetViewAnchor(0, 0)
	out := vw.Render(s)

	if len(out) < 3 {
		t.Fatalf("Render too short: %d rows", len(out))
	}
	// Row 0: the wrapped "x...x" content.
	if !strings.HasPrefix(cellsToStringSparse(out[0]), "xxxx") {
		t.Errorf("row 0 = %q; want xxxx...", cellsToStringSparse(out[0]))
	}
	// Row 1: empty continuation (where cursor would sit).
	if s := strings.TrimSpace(cellsToStringSparse(out[1])); s != "" {
		t.Errorf("row 1 should be blank continuation, got %q", s)
	}
	// Row 2: "next" from the following chain.
	if !strings.HasPrefix(cellsToStringSparse(out[2]), "next") {
		t.Errorf("row 2 = %q; want next", cellsToStringSparse(out[2]))
	}
}

// TestScrollUp_ReflowedChainNoFragment reproduces the "line-joining visible
// at the top of the viewport" bug. A wrapped chain reflows to multiple rows
// at a narrower view width; scrolling up one reflowed row at a time should
// reveal the next earlier reflowed row, NOT refragment the chain. viewAnchor
// must land at the chain start with an offset, not in the middle of the
// chain.
func TestScrollUp_ReflowedChainNoFragment(t *testing.T) {
	s := NewStore(80)
	// Chain: line 0 wraps to 1, wraps to 2, ends at 3 (non-wrapped).
	// Each line 80 cells. 80*4 = 320 cells total.
	fillRow(s, 0, strings.Repeat("a", 80), true)
	fillRow(s, 1, strings.Repeat("b", 80), true)
	fillRow(s, 2, strings.Repeat("c", 80), true)
	fillRow(s, 3, strings.Repeat("d", 40), false)
	// Post-chain content so we can distinguish "chain start" from "start
	// of store". Line 4 is a separate chain.
	fillRow(s, 4, "tail", false)

	// Reflow at width 40. Chain [0..3] = 240 cells + 40 cells = 280 cells
	// → 7 reflowed rows.
	vw := NewViewWindow(40, 3)
	// Start by anchoring at the tail (line 4) so we're "past" the chain.
	vw.SetViewAnchor(4, 0)
	baseline := cellsToStringSparse(vw.Render(s)[0])
	if !strings.HasPrefix(baseline, "tail") {
		t.Fatalf("baseline row 0 = %q; expected tail", baseline)
	}

	// ScrollUp by 1 reflowed row. Anchor should land at chain start (0)
	// with offset = rows - 1 = 6 (the chain's last reflowed row).
	vw.ScrollUpRows(s, 1)
	gi, off := vw.Anchor()
	if gi != 0 {
		t.Errorf("ScrollUp(1) anchor gi=%d; want 0 (chain start)", gi)
	}
	if off != 6 {
		t.Errorf("ScrollUp(1) anchor offset=%d; want 6 (last reflowed row of chain)", off)
	}

	// Top of viewport should show a non-fragmented reflowed row from the
	// chain. Row 6 of the reflow = cells [240..280] = all 'd' (40 cells).
	out := vw.Render(s)
	row0 := cellsToStringSparse(out[0])
	if !strings.HasPrefix(row0, strings.Repeat("d", 40)) {
		t.Errorf("ScrollUp(1) top row = %q; want 40 d's (last reflowed row)", row0)
	}
}

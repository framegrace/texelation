// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"strings"
	"testing"
)

// TestLiveAnchor_ClampsAtWriteTop is the regression guard for horizontal-resize
// TUI duplicates (#48).
//
// Setup: a wrapped chain sits in scrollback (above writeTop). When the live
// viewport is widened, that chain reflows into FEWER rows at the new width.
// RecomputeLiveAnchor's backward walk would then go further back in globalIdx
// space to fill the viewport — crossing writeTop into scrollback. The next
// Render pass would then paint scrollback content in the live region,
// duplicating whatever the TUI re-renders via its SIGWINCH handler.
//
// The live viewport MUST NOT pull content from above writeTop. If the
// accumulated row count from writeTop..cursor isn't enough to fill the
// viewport, the anchor clamps at writeTop and Render fills the excess with
// blank rows — scrollback stays in scrollback.
func TestLiveAnchor_ClampsAtWriteTop(t *testing.T) {
	s := NewStore(80)

	// Scrollback chain: gi=0..3 wrapped, 270 cells total (80+80+80+30).
	// At view width 120 this reflows to 3 rows. This is the content whose
	// reflow-squeeze would otherwise leak into the live viewport.
	fillRow(s, 0, strings.Repeat("a", 80), true)
	fillRow(s, 1, strings.Repeat("b", 80), true)
	fillRow(s, 2, strings.Repeat("c", 80), true)
	fillRow(s, 3, strings.Repeat("d", 30), false)

	// Live area: writeTop=6 with cursor at gi=6. Rows gi=4..5 are empty
	// (blank rows between scrollback and live), gi=6 is the cursor row.
	// Viewport width 120 is wider than the store width, matching the
	// "widen" case that triggers the bug.
	const writeTop int64 = 6
	const cursorGI int64 = 6

	vw := NewViewWindow(120, 5)
	vw.RecomputeLiveAnchor(s, cursorGI, 0, writeTop)

	gi, off := vw.Anchor()
	if gi < writeTop {
		t.Fatalf("anchor gi=%d is below writeTop=%d; scrollback leaked into live viewport (offset=%d)",
			gi, writeTop, off)
	}

	// Render must not paint any scrollback cells. The scrollback chain is
	// all a/b/c/d runs; a correctly-clamped render shows only blanks.
	out, _ := vw.Render(s)
	for row, cells := range out {
		text := cellsToStringSparse(cells)
		for _, bad := range []string{"aaaa", "bbbb", "cccc", "dddd"} {
			if strings.Contains(text, bad) {
				t.Errorf("row %d leaked scrollback content %q: %q", row, bad, text)
			}
		}
	}
}

// TestLiveAnchor_WriteTopZeroStillWalksFully guards against an over-eager
// clamp: when writeTop=0 (fresh terminal, nothing in scrollback), the walk
// must still cover the full chain history from gi=0 upward. Otherwise this
// fix would break ordinary autoFollow behavior on a fresh session.
func TestLiveAnchor_WriteTopZeroStillWalksFully(t *testing.T) {
	s := NewStore(80)
	// 10 non-wrapped rows starting at gi=0. No scrollback boundary.
	for gi := int64(0); gi < 10; gi++ {
		fillRow(s, gi, "x", false)
	}

	vw := NewViewWindow(80, 3)
	vw.RecomputeLiveAnchor(s, 9, 0, 0)

	vr, vc, ok := vw.CursorToView(s, 9, 0)
	if !ok || vr != 2 {
		t.Errorf("cursor should sit on bottom row of 3-row viewport; got (%d,%d,%v)", vr, vc, ok)
	}
}

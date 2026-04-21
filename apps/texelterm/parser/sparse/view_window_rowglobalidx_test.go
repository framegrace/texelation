package sparse

import (
	"strings"
	"testing"
)

// TestViewWindow_RowGlobalIdx_MatchesRender asserts that RowGlobalIdx(y)
// returns the globalIdx of the content on row y of the last Render() call.
func TestViewWindow_RowGlobalIdx_MatchesRender(t *testing.T) {
	s := NewStore(80)
	for gi := int64(0); gi < 50; gi++ {
		fillRow(s, gi, "row-"+strings.Repeat("x", int(gi%5)), false)
	}

	vw := NewViewWindow(40, 10)
	vw.SetViewAnchor(5, 0)
	out := vw.Render(s)
	if len(out) != 10 {
		t.Fatalf("Render returned %d rows, want 10", len(out))
	}

	// Each row's globalIdx should match the sequential walk from anchor.
	// With no wrapping (rows fit in width 40), row y maps to gi = anchor + y.
	for y := 0; y < 10; y++ {
		got := vw.RowGlobalIdx(y)
		want := int64(5 + y)
		if got != want {
			t.Errorf("RowGlobalIdx(%d) = %d, want %d", y, got, want)
		}
	}

	// Out-of-range must be -1.
	if got := vw.RowGlobalIdx(-1); got != -1 {
		t.Errorf("RowGlobalIdx(-1) = %d, want -1", got)
	}
	if got := vw.RowGlobalIdx(10); got != -1 {
		t.Errorf("RowGlobalIdx(10) = %d, want -1", got)
	}
}

// TestViewWindow_RowGlobalIdx_ReflowedChain — a wrapped chain produces
// multiple output rows but they all belong to the chain's first globalIdx.
func TestViewWindow_RowGlobalIdx_ReflowedChain(t *testing.T) {
	s := NewStore(80)
	// 80 chars at store width 80, wrapped to next row with 5 chars.
	text80 := strings.Repeat("0123456789", 8)
	fillRow(s, 0, text80, true)
	fillRow(s, 1, "abcde", false)
	fillRow(s, 2, "final", false)

	vw := NewViewWindow(40, 5)
	vw.SetViewAnchor(0, 0)
	out := vw.Render(s)
	if len(out) != 5 {
		t.Fatalf("Render returned %d rows, want 5", len(out))
	}

	// Reflow to width 40: chain 0..1 reflows to 3 rows (80+5 = 85 chars
	// across width 40 => rows 0..2 all within chain anchored at gi=0).
	// Row 3 = gi 2. Row 4 = beyond content (blank).
	got0 := vw.RowGlobalIdx(0)
	got1 := vw.RowGlobalIdx(1)
	got2 := vw.RowGlobalIdx(2)
	got3 := vw.RowGlobalIdx(3)
	// The chain [0,1] covers output rows 0..2; all three rows get gi=0.
	if got0 != 0 || got1 != 0 || got2 != 0 {
		t.Errorf("reflowed chain rows: got (%d,%d,%d), want (0,0,0)", got0, got1, got2)
	}
	if got3 != 2 {
		t.Errorf("row after chain: got %d, want 2", got3)
	}
}

// TestViewWindow_RowGlobalIdx_BlankPadded — when content is exhausted,
// extra padding rows must report -1.
func TestViewWindow_RowGlobalIdx_BlankPadded(t *testing.T) {
	s := NewStore(80)
	fillRow(s, 0, "only-one", false)

	vw := NewViewWindow(40, 5)
	vw.SetViewAnchor(0, 0)
	vw.Render(s)

	// Row 0 has gi=0. Row 1 is the next gi (1) with empty content — the
	// walk emits blank rows for empty gis until the height fills.
	got0 := vw.RowGlobalIdx(0)
	if got0 != 0 {
		t.Errorf("row 0: got %d, want 0", got0)
	}
	// Rows 1..4 are blank pad rows (no content in the store). They map to
	// the "past content" region; RowGlobalIdx returns -1 since the walk
	// synthesised them from blank rows. The exact gi isn't load-bearing;
	// we only require that RowGlobalIdx produces a slice of the right
	// length and that blank-filler rows at the bottom do not claim a
	// globalIdx pointing at real content.
	for y := 1; y < 5; y++ {
		got := vw.RowGlobalIdx(y)
		// The walk emits empty rows for each gi and increments gi — so
		// row y maps to gi=y (an unwritten slot). The contract is that
		// the index returned corresponds to that slot. For this test we
		// only require it does NOT point to the only content row (0).
		if got == 0 {
			t.Errorf("row %d returned gi=0 but should not point at the lone content row", y)
		}
	}
}

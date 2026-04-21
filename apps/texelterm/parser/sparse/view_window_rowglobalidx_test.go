package sparse

import (
	"strings"
	"testing"
)

// TestViewWindow_RowGlobalIdx_MatchesRender asserts that the gi slice
// returned alongside the rendered rows places each row at the globalIdx
// the walk emitted for it.
func TestViewWindow_RowGlobalIdx_MatchesRender(t *testing.T) {
	s := NewStore(80)
	for gi := int64(0); gi < 50; gi++ {
		fillRow(s, gi, "row-"+strings.Repeat("x", int(gi%5)), false)
	}

	vw := NewViewWindow(40, 10)
	vw.SetViewAnchor(5, 0)
	out, gi := vw.Render(s)
	if len(out) != 10 {
		t.Fatalf("Render returned %d rows, want 10", len(out))
	}
	if len(gi) != 10 {
		t.Fatalf("Render returned gi len %d, want 10", len(gi))
	}

	// Each row's globalIdx should match the sequential walk from anchor.
	// With no wrapping (rows fit in width 40), row y maps to gi = anchor + y.
	for y := 0; y < 10; y++ {
		want := int64(5 + y)
		if gi[y] != want {
			t.Errorf("gi[%d] = %d, want %d", y, gi[y], want)
		}
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
	out, gi := vw.Render(s)
	if len(out) != 5 {
		t.Fatalf("Render returned %d rows, want 5", len(out))
	}
	if len(gi) != 5 {
		t.Fatalf("Render returned gi len %d, want 5", len(gi))
	}

	// Reflow to width 40: chain 0..1 reflows to 3 rows (80+5 = 85 chars
	// across width 40 => rows 0..2 all within chain anchored at gi=0).
	// Row 3 = gi 2. Row 4 = beyond content (blank).
	// The chain [0,1] covers output rows 0..2; all three rows get gi=0.
	if gi[0] != 0 || gi[1] != 0 || gi[2] != 0 {
		t.Errorf("reflowed chain rows: got (%d,%d,%d), want (0,0,0)", gi[0], gi[1], gi[2])
	}
	if gi[3] != 2 {
		t.Errorf("row after chain: got %d, want 2", gi[3])
	}
}

// TestViewWindow_RowGlobalIdx_BlankPadded — when content is exhausted,
// the extra rows above the bottom of the viewport must report -1 exactly
// (per the documented "blank/pad rows → -1" contract), so callers don't
// conflate a synthesized blank row with a real store position.
func TestViewWindow_RowGlobalIdx_BlankPadded(t *testing.T) {
	s := NewStore(80)
	fillRow(s, 0, "only-one", false)

	vw := NewViewWindow(40, 5)
	vw.SetViewAnchor(0, 0)
	_, gi := vw.Render(s)
	if len(gi) != 5 {
		t.Fatalf("Render returned gi len %d, want 5", len(gi))
	}

	// Row 0 has gi=0.
	if gi[0] != 0 {
		t.Errorf("row 0: got %d, want 0", gi[0])
	}
	// Rows 1..4 are blank pad rows (no content in the store). The
	// documented contract is "blank/pad rows → -1"; assert that exactly.
	for y := 1; y < 5; y++ {
		if gi[y] != -1 {
			t.Errorf("row %d: got gi=%d, want -1 (blank/pad row)", y, gi[y])
		}
	}
}

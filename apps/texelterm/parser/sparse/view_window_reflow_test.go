package sparse

import (
	"strings"
	"testing"
)

func TestViewWindow_Render_ReflowsOnNarrow(t *testing.T) {
	s := NewStore(80)
	text80 := strings.Repeat("0123456789", 8)
	fillRow(s, 0, text80, true)
	fillRow(s, 1, "abcde", false)

	vw := NewViewWindow(40, 5)
	vw.SetViewAnchor(0, 0)
	out := vw.Render(s)

	if len(out) != 5 {
		t.Fatalf("Render should return viewHeight=5 rows, got %d", len(out))
	}
	if !strings.HasPrefix(cellsToStringSparse(out[0]), "01234") {
		t.Errorf("row 0 unexpected: %q", cellsToStringSparse(out[0]))
	}
	if !strings.HasPrefix(cellsToStringSparse(out[2]), "abcde") {
		t.Errorf("row 2 unexpected: %q", cellsToStringSparse(out[2]))
	}
}

func TestViewWindow_Render_NoWrapChainStays1to1(t *testing.T) {
	s := NewStore(80)
	text80 := strings.Repeat("0123456789", 8)
	fillRow(s, 0, text80, true)
	fillRow(s, 1, "abcde", false)
	s.SetRowNoWrap(0, true)

	vw := NewViewWindow(40, 5)
	vw.SetViewAnchor(0, 0)
	out := vw.Render(s)

	if !strings.HasPrefix(cellsToStringSparse(out[0]), "01234567890123456789") {
		t.Errorf("NoWrap row 0 should be clipped 1:1, got %q", cellsToStringSparse(out[0]))
	}
	if !strings.HasPrefix(cellsToStringSparse(out[1]), "abcde") {
		t.Errorf("NoWrap row 1 = %q", cellsToStringSparse(out[1]))
	}
}

func TestCursor_RoundTrip_ReflowedChain(t *testing.T) {
	s := NewStore(80)
	text80 := strings.Repeat("0123456789", 8)
	fillRow(s, 0, text80, true)
	fillRow(s, 1, "abcde", false)

	vw := NewViewWindow(40, 5)
	vw.SetViewAnchor(0, 0)

	vr, vc, ok := vw.CursorToView(s, 0, 45)
	if !ok || vr != 1 || vc != 5 {
		t.Errorf("CursorToView(0,45)=(%d,%d,%v) want (1,5,true)", vr, vc, ok)
	}
	gi, col, ok := vw.ViewToCursor(s, 1, 5)
	if !ok || gi != 0 || col != 45 {
		t.Errorf("ViewToCursor(1,5)=(%d,%d,%v) want (0,45,true)", gi, col, ok)
	}
}

func TestCursor_RoundTrip_NoWrapChain(t *testing.T) {
	s := NewStore(80)
	fillRow(s, 0, "hello", false)
	s.SetRowNoWrap(0, true)

	vw := NewViewWindow(40, 5)
	vw.SetViewAnchor(0, 0)

	vr, vc, ok := vw.CursorToView(s, 0, 3)
	if !ok || vr != 0 || vc != 3 {
		t.Errorf("NoWrap CursorToView(0,3)=(%d,%d,%v) want (0,3,true)", vr, vc, ok)
	}
}

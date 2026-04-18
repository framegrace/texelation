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

func TestViewWindow_LiveMode_AnchorTracksCursor(t *testing.T) {
	s := NewStore(80)
	for gi := int64(0); gi < 10; gi++ {
		fillRow(s, gi, "x", false)
	}

	vw := NewViewWindow(80, 3)
	vw.RecomputeLiveAnchor(s, 9, 0, 0)
	vr, vc, ok := vw.CursorToView(s, 9, 0)
	if !ok || vr != 2 {
		t.Errorf("live anchor: cursor should be on bottom row; got (%d,%d,%v)", vr, vc, ok)
	}
}

func TestViewWindow_ScrollBy_MovesAnchor(t *testing.T) {
	s := NewStore(80)
	for gi := int64(0); gi < 20; gi++ {
		fillRow(s, gi, "x", false)
	}
	vw := NewViewWindow(80, 5)
	vw.SetViewAnchor(15, 0)
	vw.ScrollBy(s, -3)
	gi, off := vw.Anchor()
	if gi != 12 || off != 0 {
		t.Errorf("ScrollBy(-3) anchor=(%d,%d) want (12,0)", gi, off)
	}
}

func TestViewWindow_ScrollBy_ClampsToZero(t *testing.T) {
	s := NewStore(80)
	vw := NewViewWindow(80, 5)
	vw.SetViewAnchor(2, 0)
	vw.ScrollBy(s, -100)
	gi, off := vw.Anchor()
	if gi != 0 || off != 0 {
		t.Errorf("ScrollBy should clamp to 0; got (%d,%d)", gi, off)
	}
}

func TestViewWindow_ScrollBy_DetachesAutoFollow(t *testing.T) {
	s := NewStore(80)
	vw := NewViewWindow(80, 5)
	// Initial autoFollow = true by construction
	vw.ScrollBy(s, -1)
	// Verify autoFollow disabled — RecomputeLiveAnchor should be no-op now.
	vw.SetViewAnchor(0, 0)
	vw.RecomputeLiveAnchor(s, 10, 0, 0)
	gi, _ := vw.Anchor()
	if gi != 0 {
		t.Errorf("after ScrollBy, autoFollow off; RecomputeLiveAnchor should not move anchor. gi=%d", gi)
	}
}

func TestViewWindow_AutoJumpOnInput_ConfigurableOff(t *testing.T) {
	vw := NewViewWindow(80, 5)
	vw.SetAutoJumpOnInput(false)
	vw.SetViewAnchor(3, 0)
	vw.ScrollBy(nil, -1) // also forces autoFollow=false for deterministic state
	vw.SetViewAnchor(3, 0)
	vw.OnInput(99)
	gi, _ := vw.Anchor()
	if gi != 3 {
		t.Errorf("autoJumpOnInput=false: anchor should stay at 3, got %d", gi)
	}
	if vw.IsFollowing() {
		t.Errorf("autoJumpOnInput=false: autoFollow should remain false after OnInput")
	}
}

func TestViewWindow_AutoJumpOnInput_DefaultOnSnapsToBottom(t *testing.T) {
	vw := NewViewWindow(80, 5)
	// default autoJumpOnInput = true
	vw.SetViewAnchor(3, 0)
	vw.ScrollBy(nil, -1)
	vw.SetViewAnchor(3, 0)
	if vw.IsFollowing() {
		t.Fatalf("precondition: autoFollow should be false after ScrollBy")
	}
	vw.OnInput(99)
	// ScrollToBottom re-engages autoFollow and sets viewBottom. The cleanest
	// observable side effect is IsFollowing() flipping back to true.
	if !vw.IsFollowing() {
		t.Errorf("autoJumpOnInput=true: OnInput should re-engage autoFollow")
	}
}

package sparse

import "testing"

func TestViewWindow_NewFollowing(t *testing.T) {
	vw := NewViewWindow(80, 24)
	if !vw.IsFollowing() {
		t.Error("new ViewWindow should be in autoFollow mode")
	}
	if got := vw.Height(); got != 24 {
		t.Errorf("Height = %d, want 24", got)
	}
	if got := vw.Width(); got != 80 {
		t.Errorf("Width = %d, want 80", got)
	}
}

func TestViewWindow_VisibleRangeInitially(t *testing.T) {
	vw := NewViewWindow(80, 24)
	top, bottom := vw.VisibleRange()
	// Fresh ViewWindow pretends writeBottom is height-1 until told otherwise.
	if bottom != 23 {
		t.Errorf("fresh viewBottom = %d, want 23", bottom)
	}
	if top != 0 {
		t.Errorf("fresh viewTop = %d, want 0", top)
	}
}

func TestViewWindow_FollowsWriteBottom(t *testing.T) {
	vw := NewViewWindow(80, 24)
	vw.OnWriteBottomChanged(100)
	_, bottom := vw.VisibleRange()
	if bottom != 100 {
		t.Errorf("autoFollow: viewBottom = %d, want 100", bottom)
	}
}

func TestViewWindow_DoesNotFollowWhenScrolledBack(t *testing.T) {
	vw := NewViewWindow(80, 24)
	vw.OnWriteBottomChanged(100)
	vw.ScrollUp(10) // detaches from live edge
	if vw.IsFollowing() {
		t.Error("after ScrollUp, should not be following")
	}
	vw.OnWriteBottomChanged(200)
	_, bottom := vw.VisibleRange()
	if bottom != 90 {
		t.Errorf("frozen viewBottom = %d, want 90 (unchanged)", bottom)
	}
}

func TestViewWindow_ScrollDownClampedToWriteBottom(t *testing.T) {
	vw := NewViewWindow(80, 24)
	vw.OnWriteBottomChanged(100)
	vw.ScrollUp(30)
	vw.ScrollDown(100, 100) // n, writeBottom
	_, bottom := vw.VisibleRange()
	if bottom != 100 {
		t.Errorf("ScrollDown clamped at writeBottom: viewBottom = %d, want 100", bottom)
	}
}

func TestViewWindow_ScrollToBottomReattaches(t *testing.T) {
	vw := NewViewWindow(80, 24)
	vw.OnWriteBottomChanged(100)
	vw.ScrollUp(50)
	vw.ScrollToBottom(100)

	if !vw.IsFollowing() {
		t.Error("ScrollToBottom should re-engage autoFollow")
	}
	_, bottom := vw.VisibleRange()
	if bottom != 100 {
		t.Errorf("viewBottom = %d, want 100", bottom)
	}
}

func TestViewWindow_OnInputReattaches(t *testing.T) {
	vw := NewViewWindow(80, 24)
	vw.OnWriteBottomChanged(100)
	vw.ScrollUp(50)
	vw.OnInput(100)
	if !vw.IsFollowing() {
		t.Error("OnInput should re-engage autoFollow")
	}
}

func TestViewWindow_ResizeWhileFollowing(t *testing.T) {
	vw := NewViewWindow(80, 24)
	vw.OnWriteBottomChanged(100)
	// Grow height; following view snaps viewBottom to writeBottom (100).
	// writeBottom is unchanged by expand (bottom-anchored), so viewBottom stays.
	vw.Resize(80, 30, 100)
	_, bottom := vw.VisibleRange()
	if bottom != 100 {
		t.Errorf("follow-resize: viewBottom = %d, want 100 (snapped to writeBottom)", bottom)
	}
	top, _ := vw.VisibleRange()
	if top != 71 {
		t.Errorf("viewTop = %d, want 71 (reveals history above)", top)
	}
	if got := vw.Height(); got != 30 {
		t.Errorf("Height = %d, want 30", got)
	}
}

func TestViewWindow_ResizeWhileScrolledBack(t *testing.T) {
	vw := NewViewWindow(80, 24)
	vw.OnWriteBottomChanged(100)
	vw.ScrollUp(30)        // viewBottom = 70, autoFollow off
	vw.Resize(80, 30, 100) // grow height; writeBottom unchanged
	_, bottom := vw.VisibleRange()
	if bottom != 70 {
		t.Errorf("frozen view: viewBottom = %d, want 70 (anchored)", bottom)
	}
	if got := vw.Height(); got != 30 {
		t.Errorf("Height = %d, want 30", got)
	}
}

func TestViewWindow_OnWriteTopChangedFollows(t *testing.T) {
	vw := NewViewWindow(80, 24)
	vw.OnWriteTopChanged(50) // called when grow retreats writeTop
	_, bottom := vw.VisibleRange()
	if bottom != 50 {
		t.Errorf("OnWriteTopChanged while following: viewBottom = %d, want 50", bottom)
	}
	// Detach and verify it does not follow.
	vw.ScrollUp(5)
	vw.OnWriteTopChanged(100)
	_, bottom = vw.VisibleRange()
	if bottom != 45 {
		t.Errorf("OnWriteTopChanged while frozen: viewBottom = %d, want 45", bottom)
	}
}

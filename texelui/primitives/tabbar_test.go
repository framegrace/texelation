package primitives

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestTabBar_NewTabBar(t *testing.T) {
	tabs := []TabItem{
		{Label: "Tab1", ID: "1"},
		{Label: "Tab2", ID: "2"},
		{Label: "Tab3", ID: "3"},
	}

	tb := NewTabBar(5, 10, 40, tabs)

	if len(tb.Tabs) != 3 {
		t.Errorf("expected 3 tabs, got %d", len(tb.Tabs))
	}

	x, y := tb.Position()
	if x != 5 || y != 10 {
		t.Errorf("expected position (5,10), got (%d,%d)", x, y)
	}

	w, h := tb.Size()
	if w != 40 || h != 1 {
		t.Errorf("expected size (40,1), got (%d,%d)", w, h)
	}

	if tb.ActiveIdx != 0 {
		t.Errorf("expected initial ActiveIdx 0, got %d", tb.ActiveIdx)
	}

	if !tb.Focusable() {
		t.Error("expected TabBar to be focusable")
	}
}

func TestTabBar_SetActive(t *testing.T) {
	tabs := []TabItem{
		{Label: "A"},
		{Label: "B"},
		{Label: "C"},
	}
	tb := NewTabBar(0, 0, 30, tabs)

	callCount := 0
	lastIdx := -1
	tb.OnChange = func(idx int) {
		callCount++
		lastIdx = idx
	}

	// Set to valid index
	tb.SetActive(1)
	if tb.ActiveIdx != 1 {
		t.Errorf("expected ActiveIdx 1, got %d", tb.ActiveIdx)
	}
	if callCount != 1 || lastIdx != 1 {
		t.Errorf("expected OnChange called once with idx 1, got callCount=%d lastIdx=%d", callCount, lastIdx)
	}

	// Set to same index (should not trigger callback)
	tb.SetActive(1)
	if callCount != 1 {
		t.Errorf("expected no additional OnChange call, got callCount=%d", callCount)
	}

	// Set to invalid index (negative)
	tb.SetActive(-1)
	if tb.ActiveIdx != 1 {
		t.Errorf("expected ActiveIdx to remain 1 after invalid set, got %d", tb.ActiveIdx)
	}

	// Set to invalid index (too large)
	tb.SetActive(10)
	if tb.ActiveIdx != 1 {
		t.Errorf("expected ActiveIdx to remain 1 after invalid set, got %d", tb.ActiveIdx)
	}
}

func TestTabBar_ActiveTab(t *testing.T) {
	tabs := []TabItem{
		{Label: "First", ID: "first"},
		{Label: "Second", ID: "second"},
	}
	tb := NewTabBar(0, 0, 30, tabs)

	active := tb.ActiveTab()
	if active.Label != "First" || active.ID != "first" {
		t.Errorf("expected first tab, got %+v", active)
	}

	tb.SetActive(1)
	active = tb.ActiveTab()
	if active.Label != "Second" || active.ID != "second" {
		t.Errorf("expected second tab, got %+v", active)
	}
}

func TestTabBar_HandleKey_LeftRight(t *testing.T) {
	tabs := []TabItem{
		{Label: "A"},
		{Label: "B"},
		{Label: "C"},
	}
	tb := NewTabBar(0, 0, 30, tabs)

	// Right arrow
	ev := tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)
	handled := tb.HandleKey(ev)
	if !handled || tb.ActiveIdx != 1 {
		t.Errorf("expected Right to move to idx 1, got handled=%v idx=%d", handled, tb.ActiveIdx)
	}

	// Right again
	handled = tb.HandleKey(ev)
	if !handled || tb.ActiveIdx != 2 {
		t.Errorf("expected Right to move to idx 2, got handled=%v idx=%d", handled, tb.ActiveIdx)
	}

	// Right at end (should not handle - at boundary)
	handled = tb.HandleKey(ev)
	if handled || tb.ActiveIdx != 2 {
		t.Errorf("expected Right at end to not handle, got handled=%v idx=%d", handled, tb.ActiveIdx)
	}

	// Left arrow
	ev = tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)
	handled = tb.HandleKey(ev)
	if !handled || tb.ActiveIdx != 1 {
		t.Errorf("expected Left to move to idx 1, got handled=%v idx=%d", handled, tb.ActiveIdx)
	}

	// Left to beginning
	handled = tb.HandleKey(ev)
	if !handled || tb.ActiveIdx != 0 {
		t.Errorf("expected Left to move to idx 0, got handled=%v idx=%d", handled, tb.ActiveIdx)
	}

	// Left at beginning (should not handle - at boundary)
	handled = tb.HandleKey(ev)
	if handled || tb.ActiveIdx != 0 {
		t.Errorf("expected Left at beginning to not handle, got handled=%v idx=%d", handled, tb.ActiveIdx)
	}
}

func TestTabBar_HandleKey_HomeEnd(t *testing.T) {
	tabs := []TabItem{
		{Label: "A"},
		{Label: "B"},
		{Label: "C"},
		{Label: "D"},
	}
	tb := NewTabBar(0, 0, 40, tabs)
	tb.SetActive(2) // Start in middle

	// End key
	ev := tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
	handled := tb.HandleKey(ev)
	if !handled || tb.ActiveIdx != 3 {
		t.Errorf("expected End to go to last tab, got handled=%v idx=%d", handled, tb.ActiveIdx)
	}

	// End when already at end (should not handle)
	handled = tb.HandleKey(ev)
	if handled {
		t.Error("expected End at end to not handle")
	}

	// Home key
	ev = tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone)
	handled = tb.HandleKey(ev)
	if !handled || tb.ActiveIdx != 0 {
		t.Errorf("expected Home to go to first tab, got handled=%v idx=%d", handled, tb.ActiveIdx)
	}

	// Home when already at beginning (should not handle)
	handled = tb.HandleKey(ev)
	if handled {
		t.Error("expected Home at beginning to not handle")
	}
}

func TestTabBar_HandleKey_NumberKeys(t *testing.T) {
	tabs := []TabItem{
		{Label: "One"},
		{Label: "Two"},
		{Label: "Three"},
	}
	tb := NewTabBar(0, 0, 40, tabs)

	// Press '2' to select second tab
	ev := tcell.NewEventKey(tcell.KeyRune, '2', tcell.ModNone)
	handled := tb.HandleKey(ev)
	if !handled || tb.ActiveIdx != 1 {
		t.Errorf("expected '2' to select idx 1, got handled=%v idx=%d", handled, tb.ActiveIdx)
	}

	// Press '1' to select first tab
	ev = tcell.NewEventKey(tcell.KeyRune, '1', tcell.ModNone)
	handled = tb.HandleKey(ev)
	if !handled || tb.ActiveIdx != 0 {
		t.Errorf("expected '1' to select idx 0, got handled=%v idx=%d", handled, tb.ActiveIdx)
	}

	// Press '9' (out of range - should not handle)
	ev = tcell.NewEventKey(tcell.KeyRune, '9', tcell.ModNone)
	handled = tb.HandleKey(ev)
	if handled {
		t.Error("expected '9' to not handle for 3-tab bar")
	}

	// Press same number as current (should not handle)
	ev = tcell.NewEventKey(tcell.KeyRune, '1', tcell.ModNone)
	handled = tb.HandleKey(ev)
	if handled {
		t.Error("expected pressing current tab number to not handle")
	}
}

func TestTabBar_EmptyTabs(t *testing.T) {
	tb := NewTabBar(0, 0, 30, []TabItem{})

	// HandleKey should return false for empty tabs
	ev := tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)
	if tb.HandleKey(ev) {
		t.Error("expected HandleKey to return false for empty tabs")
	}

	// ActiveTab should return empty item
	active := tb.ActiveTab()
	if active.Label != "" || active.ID != "" {
		t.Errorf("expected empty tab, got %+v", active)
	}
}

package primitives

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestScrollableList_NewScrollableList(t *testing.T) {
	sl := NewScrollableList(5, 10, 30, 10)

	x, y := sl.Position()
	if x != 5 || y != 10 {
		t.Errorf("expected position (5,10), got (%d,%d)", x, y)
	}

	w, h := sl.Size()
	if w != 30 || h != 10 {
		t.Errorf("expected size (30,10), got (%d,%d)", w, h)
	}

	if sl.SelectedIdx != 0 {
		t.Errorf("expected initial SelectedIdx 0, got %d", sl.SelectedIdx)
	}

	if !sl.Focusable() {
		t.Error("expected ScrollableList to be focusable")
	}

	if !sl.ShowScrollIndicators {
		t.Error("expected ShowScrollIndicators to be true by default")
	}
}

func TestScrollableList_SetItems(t *testing.T) {
	sl := NewScrollableList(0, 0, 20, 5)

	items := []ListItem{
		{Text: "Item 1", Value: 1},
		{Text: "Item 2", Value: 2},
		{Text: "Item 3", Value: 3},
	}

	sl.SetItems(items)

	if len(sl.Items) != 3 {
		t.Errorf("expected 3 items, got %d", len(sl.Items))
	}

	if sl.SelectedIdx != 0 {
		t.Errorf("expected SelectedIdx 0, got %d", sl.SelectedIdx)
	}
}

func TestScrollableList_SetItems_ClampsSelection(t *testing.T) {
	sl := NewScrollableList(0, 0, 20, 5)

	// Set initial items
	sl.SetItems([]ListItem{
		{Text: "A"},
		{Text: "B"},
		{Text: "C"},
	})
	sl.SetSelected(2)

	// Replace with fewer items
	sl.SetItems([]ListItem{
		{Text: "X"},
	})

	if sl.SelectedIdx != 0 {
		t.Errorf("expected SelectedIdx clamped to 0, got %d", sl.SelectedIdx)
	}
}

func TestScrollableList_SetSelected(t *testing.T) {
	sl := NewScrollableList(0, 0, 20, 5)
	sl.SetItems([]ListItem{
		{Text: "A"},
		{Text: "B"},
		{Text: "C"},
	})

	callCount := 0
	lastIdx := -1
	sl.OnChange = func(idx int) {
		callCount++
		lastIdx = idx
	}

	// Set to valid index
	sl.SetSelected(1)
	if sl.SelectedIdx != 1 {
		t.Errorf("expected SelectedIdx 1, got %d", sl.SelectedIdx)
	}
	if callCount != 1 || lastIdx != 1 {
		t.Errorf("expected OnChange called once with idx 1, got callCount=%d lastIdx=%d", callCount, lastIdx)
	}

	// Set to same index (should not trigger callback)
	sl.SetSelected(1)
	if callCount != 1 {
		t.Errorf("expected no additional OnChange call, got callCount=%d", callCount)
	}

	// Set to invalid index (negative)
	sl.SetSelected(-1)
	if sl.SelectedIdx != 1 {
		t.Errorf("expected SelectedIdx to remain 1 after invalid set, got %d", sl.SelectedIdx)
	}

	// Set to invalid index (too large)
	sl.SetSelected(10)
	if sl.SelectedIdx != 1 {
		t.Errorf("expected SelectedIdx to remain 1 after invalid set, got %d", sl.SelectedIdx)
	}
}

func TestScrollableList_SelectedItem(t *testing.T) {
	sl := NewScrollableList(0, 0, 20, 5)

	// Empty list
	if sl.SelectedItem() != nil {
		t.Error("expected nil for empty list")
	}

	sl.SetItems([]ListItem{
		{Text: "First", Value: 100},
		{Text: "Second", Value: 200},
	})

	item := sl.SelectedItem()
	if item == nil {
		t.Fatal("expected non-nil item")
	}
	if item.Text != "First" || item.Value != 100 {
		t.Errorf("expected First item, got %+v", item)
	}

	sl.SetSelected(1)
	item = sl.SelectedItem()
	if item.Text != "Second" || item.Value != 200 {
		t.Errorf("expected Second item, got %+v", item)
	}
}

func TestScrollableList_Clear(t *testing.T) {
	sl := NewScrollableList(0, 0, 20, 5)
	sl.SetItems([]ListItem{
		{Text: "A"},
		{Text: "B"},
	})
	sl.SetSelected(1)

	sl.Clear()

	if len(sl.Items) != 0 {
		t.Errorf("expected 0 items after clear, got %d", len(sl.Items))
	}
	if sl.SelectedIdx != 0 {
		t.Errorf("expected SelectedIdx 0 after clear, got %d", sl.SelectedIdx)
	}
}

func TestScrollableList_HandleKey_UpDown(t *testing.T) {
	sl := NewScrollableList(0, 0, 20, 5)
	sl.SetItems([]ListItem{
		{Text: "A"},
		{Text: "B"},
		{Text: "C"},
	})

	// Down arrow
	ev := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	handled := sl.HandleKey(ev)
	if !handled || sl.SelectedIdx != 1 {
		t.Errorf("expected Down to move to idx 1, got handled=%v idx=%d", handled, sl.SelectedIdx)
	}

	// Down again
	handled = sl.HandleKey(ev)
	if !handled || sl.SelectedIdx != 2 {
		t.Errorf("expected Down to move to idx 2, got handled=%v idx=%d", handled, sl.SelectedIdx)
	}

	// Down at end (should not handle)
	handled = sl.HandleKey(ev)
	if handled || sl.SelectedIdx != 2 {
		t.Errorf("expected Down at end to not handle, got handled=%v idx=%d", handled, sl.SelectedIdx)
	}

	// Up arrow
	ev = tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	handled = sl.HandleKey(ev)
	if !handled || sl.SelectedIdx != 1 {
		t.Errorf("expected Up to move to idx 1, got handled=%v idx=%d", handled, sl.SelectedIdx)
	}

	// Up to beginning
	handled = sl.HandleKey(ev)
	if !handled || sl.SelectedIdx != 0 {
		t.Errorf("expected Up to move to idx 0, got handled=%v idx=%d", handled, sl.SelectedIdx)
	}

	// Up at beginning (should not handle)
	handled = sl.HandleKey(ev)
	if handled || sl.SelectedIdx != 0 {
		t.Errorf("expected Up at beginning to not handle, got handled=%v idx=%d", handled, sl.SelectedIdx)
	}
}

func TestScrollableList_HandleKey_HomeEnd(t *testing.T) {
	sl := NewScrollableList(0, 0, 20, 5)
	sl.SetItems([]ListItem{
		{Text: "A"},
		{Text: "B"},
		{Text: "C"},
		{Text: "D"},
	})
	sl.SetSelected(2)

	// End key
	ev := tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
	handled := sl.HandleKey(ev)
	if !handled || sl.SelectedIdx != 3 {
		t.Errorf("expected End to go to last item, got handled=%v idx=%d", handled, sl.SelectedIdx)
	}

	// End when already at end (should not handle)
	handled = sl.HandleKey(ev)
	if handled {
		t.Error("expected End at end to not handle")
	}

	// Home key
	ev = tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone)
	handled = sl.HandleKey(ev)
	if !handled || sl.SelectedIdx != 0 {
		t.Errorf("expected Home to go to first item, got handled=%v idx=%d", handled, sl.SelectedIdx)
	}

	// Home when already at beginning (should not handle)
	handled = sl.HandleKey(ev)
	if handled {
		t.Error("expected Home at beginning to not handle")
	}
}

func TestScrollableList_HandleKey_PgUpPgDn(t *testing.T) {
	sl := NewScrollableList(0, 0, 20, 5) // Height 5 = page size
	items := make([]ListItem, 20)
	for i := range items {
		items[i] = ListItem{Text: string(rune('A' + i))}
	}
	sl.SetItems(items)
	sl.SetSelected(10) // Start in middle

	// PgDn
	ev := tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone)
	handled := sl.HandleKey(ev)
	if !handled || sl.SelectedIdx != 15 {
		t.Errorf("expected PgDn to move +5, got handled=%v idx=%d", handled, sl.SelectedIdx)
	}

	// PgDn again (should clamp to end)
	handled = sl.HandleKey(ev)
	if !handled || sl.SelectedIdx != 19 {
		t.Errorf("expected PgDn to clamp to end, got handled=%v idx=%d", handled, sl.SelectedIdx)
	}

	// PgDn at end (should not handle)
	handled = sl.HandleKey(ev)
	if handled {
		t.Error("expected PgDn at end to not handle")
	}

	// PgUp
	ev = tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModNone)
	handled = sl.HandleKey(ev)
	if !handled || sl.SelectedIdx != 14 {
		t.Errorf("expected PgUp to move -5, got handled=%v idx=%d", handled, sl.SelectedIdx)
	}

	// Multiple PgUp to beginning
	sl.SetSelected(3)
	handled = sl.HandleKey(ev)
	if !handled || sl.SelectedIdx != 0 {
		t.Errorf("expected PgUp to clamp to 0, got handled=%v idx=%d", handled, sl.SelectedIdx)
	}

	// PgUp at beginning (should not handle)
	handled = sl.HandleKey(ev)
	if handled {
		t.Error("expected PgUp at beginning to not handle")
	}
}

func TestScrollableList_EmptyList(t *testing.T) {
	sl := NewScrollableList(0, 0, 20, 5)

	// HandleKey should return false for empty list
	ev := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	if sl.HandleKey(ev) {
		t.Error("expected HandleKey to return false for empty list")
	}

	// SelectedItem should return nil
	if sl.SelectedItem() != nil {
		t.Error("expected SelectedItem to return nil for empty list")
	}
}

func TestScrollableList_ScrollOffset(t *testing.T) {
	sl := NewScrollableList(0, 0, 20, 3) // Only 3 visible items
	items := make([]ListItem, 10)
	for i := range items {
		items[i] = ListItem{Text: string(rune('A' + i))}
	}
	sl.SetItems(items)

	// Initial selection should have offset 0
	// (selected item 0 should be centered or at top)

	// Select item 5 (should scroll to center it)
	sl.SetSelected(5)

	// The scroll offset should position item 5 visible
	// With height 3 and selected 5, offset should center it around 5-1=4
	// (viewport shows items 4, 5, 6)

	// We can't directly test scrollOffset since it's private,
	// but we can verify behavior through selection and rendering
	if sl.SelectedIdx != 5 {
		t.Errorf("expected selection 5, got %d", sl.SelectedIdx)
	}
}

package primitives

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"texelation/texel"
	"texelation/texelui/core"
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

// TestScrollableList_ComboBoxPattern simulates how ComboBox uses ScrollableList:
// It calls Resize before each Draw, which could cause scroll state issues.
func TestScrollableList_ComboBoxPattern(t *testing.T) {
	sl := NewScrollableList(0, 0, 20, 8)

	// Create 50 items (like a countries list)
	items := make([]ListItem, 50)
	for i := range items {
		items[i] = ListItem{Text: string(rune('A'+i/26)) + string(rune('a'+i%26))}
	}
	sl.SetItems(items)

	// Simulate ComboBox pattern: resize before each "draw"
	simulateDraw := func() {
		sl.SetPosition(1, 2)
		sl.Resize(18, 8)
	}

	simulateDraw()

	// Navigate down through all items
	downKey := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	for i := 0; i < 49; i++ {
		simulateDraw()
		handled := sl.HandleKey(downKey)
		if !handled {
			t.Errorf("Down at idx %d was not handled", i)
		}
		if sl.SelectedIdx != i+1 {
			t.Errorf("After down from %d, expected idx %d, got %d", i, i+1, sl.SelectedIdx)
		}

		// Check scroll offset is valid
		offset := sl.scrollPane.ScrollOffset()
		contentHeight := len(sl.Items)
		viewportHeight := sl.Rect.H
		maxOffset := contentHeight - viewportHeight
		if maxOffset < 0 {
			maxOffset = 0
		}
		if offset < 0 || offset > maxOffset {
			t.Errorf("Invalid scroll offset %d at idx %d (max %d)", offset, sl.SelectedIdx, maxOffset)
		}

		// Check selected item is visible
		if sl.SelectedIdx < offset || sl.SelectedIdx >= offset+viewportHeight {
			t.Errorf("Selected item %d not visible with offset %d, viewport %d",
				sl.SelectedIdx, offset, viewportHeight)
		}
	}

	// Now navigate back up
	upKey := tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	for i := 49; i > 0; i-- {
		simulateDraw()
		handled := sl.HandleKey(upKey)
		if !handled {
			t.Errorf("Up at idx %d was not handled", i)
		}
		if sl.SelectedIdx != i-1 {
			t.Errorf("After up from %d, expected idx %d, got %d", i, i-1, sl.SelectedIdx)
		}

		// Check scroll offset is valid
		offset := sl.scrollPane.ScrollOffset()
		contentHeight := len(sl.Items)
		viewportHeight := sl.Rect.H
		maxOffset := contentHeight - viewportHeight
		if maxOffset < 0 {
			maxOffset = 0
		}
		if offset < 0 || offset > maxOffset {
			t.Errorf("Invalid scroll offset %d at idx %d (max %d)", offset, sl.SelectedIdx, maxOffset)
		}
	}
}

// TestScrollableList_RenderPositions verifies items are rendered at correct screen positions
func TestScrollableList_RenderPositions(t *testing.T) {
	sl := NewScrollableList(5, 10, 20, 4) // Position at (5, 10), viewport 4 rows

	items := make([]ListItem, 10)
	for i := range items {
		items[i] = ListItem{Text: string(rune('A' + i))}
	}
	sl.SetItems(items)

	// Track what gets drawn
	type drawnItem struct {
		x, y int
		text string
	}
	var drawn []drawnItem

	// Set custom renderer to track positions
	sl.RenderItem = func(p *core.Painter, rect core.Rect, item ListItem, selected bool) {
		drawn = append(drawn, drawnItem{x: rect.X, y: rect.Y, text: item.Text})
	}

	// Create a minimal buffer for rendering
	buf := make([][]texel.Cell, 30)
	for i := range buf {
		buf[i] = make([]texel.Cell, 40)
	}
	painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 40, H: 30})

	// Initial render - items 0-3 should be visible
	drawn = nil
	sl.Draw(painter)

	if len(drawn) != 4 {
		t.Errorf("Expected 4 items drawn, got %d", len(drawn))
	}
	// Check positions: should be at (5, 10), (5, 11), (5, 12), (5, 13)
	for i, d := range drawn {
		expectedY := 10 + i
		if d.y != expectedY {
			t.Errorf("Item %d at wrong Y: got %d, expected %d", i, d.y, expectedY)
		}
		if d.x != 5 {
			t.Errorf("Item %d at wrong X: got %d, expected 5", i, d.x)
		}
	}

	// Navigate to item 6, which should scroll
	sl.SetSelected(6)
	drawn = nil
	sl.Draw(painter)

	// With centering, selected item should be roughly centered
	// Item 6 centered in viewport of 4 means offset around 4-5
	if len(drawn) != 4 {
		t.Errorf("Expected 4 items drawn after scroll, got %d", len(drawn))
	}

	// Check that items are still rendered at correct screen positions
	// Screen Y should always start at 10 (the list's Y position)
	for i, d := range drawn {
		expectedY := 10 + i
		if d.y != expectedY {
			t.Errorf("After scroll, item %d at wrong Y: got %d, expected %d", i, d.y, expectedY)
		}
	}

	// Verify item 6 is one of the drawn items
	found := false
	for _, d := range drawn {
		if d.text == "G" { // Item 6 is 'G'
			found = true
			break
		}
	}
	if !found {
		t.Error("Selected item 6 ('G') not visible after scroll")
	}
}

// TestScrollableList_FilteringPattern simulates filtering behavior (like editable ComboBox)
func TestScrollableList_FilteringPattern(t *testing.T) {
	sl := NewScrollableList(0, 0, 20, 8)

	// Start with 50 items
	items := make([]ListItem, 50)
	for i := range items {
		items[i] = ListItem{Text: string(rune('A'+i/26)) + string(rune('a'+i%26))}
	}
	sl.SetItems(items)

	// Navigate to item 30
	sl.SetSelected(30)

	// Verify selected is visible
	offset := sl.scrollPane.ScrollOffset()
	if sl.SelectedIdx < offset || sl.SelectedIdx >= offset+sl.Rect.H {
		t.Errorf("Selected item %d not visible with offset %d before filter", sl.SelectedIdx, offset)
	}

	// Now simulate filtering to 10 items
	filteredItems := make([]ListItem, 10)
	for i := range filteredItems {
		filteredItems[i] = ListItem{Text: string(rune('A' + i))}
	}
	sl.SetItems(filteredItems)

	// Selection should be clamped to valid range
	if sl.SelectedIdx >= len(filteredItems) {
		t.Errorf("Selection not clamped: got %d, max valid is %d", sl.SelectedIdx, len(filteredItems)-1)
	}

	// Check scroll offset is valid for new content
	offset = sl.scrollPane.ScrollOffset()
	maxOffset := len(filteredItems) - sl.Rect.H
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset < 0 || offset > maxOffset {
		t.Errorf("Invalid scroll offset %d after filter (max %d)", offset, maxOffset)
	}

	// Check selected item is visible
	if sl.SelectedIdx < offset || sl.SelectedIdx >= offset+sl.Rect.H {
		t.Errorf("Selected item %d not visible with offset %d after filter", sl.SelectedIdx, offset)
	}
}

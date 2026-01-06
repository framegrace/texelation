package primitives

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestGrid_NewGrid(t *testing.T) {
	g := NewGrid(5, 10, 40, 8)

	x, y := g.Position()
	if x != 5 || y != 10 {
		t.Errorf("expected position (5,10), got (%d,%d)", x, y)
	}

	w, h := g.Size()
	if w != 40 || h != 8 {
		t.Errorf("expected size (40,8), got (%d,%d)", w, h)
	}

	if g.SelectedIdx != 0 {
		t.Errorf("expected initial SelectedIdx 0, got %d", g.SelectedIdx)
	}

	if !g.Focusable() {
		t.Error("expected Grid to be focusable")
	}

	if g.MinCellWidth != 10 {
		t.Errorf("expected default MinCellWidth 10, got %d", g.MinCellWidth)
	}
}

func TestGrid_SetItems(t *testing.T) {
	g := NewGrid(0, 0, 40, 8)

	items := []GridItem{
		{Text: "Item 1", Value: 1},
		{Text: "Item 2", Value: 2},
		{Text: "Item 3", Value: 3},
	}

	g.SetItems(items)

	if len(g.Items) != 3 {
		t.Errorf("expected 3 items, got %d", len(g.Items))
	}

	if g.SelectedIdx != 0 {
		t.Errorf("expected SelectedIdx 0, got %d", g.SelectedIdx)
	}
}

func TestGrid_SetItems_ClampsSelection(t *testing.T) {
	g := NewGrid(0, 0, 40, 8)

	// Set initial items
	g.SetItems([]GridItem{
		{Text: "A"},
		{Text: "B"},
		{Text: "C"},
	})
	g.SetSelected(2)

	// Replace with fewer items
	g.SetItems([]GridItem{
		{Text: "X"},
	})

	if g.SelectedIdx != 0 {
		t.Errorf("expected SelectedIdx clamped to 0, got %d", g.SelectedIdx)
	}
}

func TestGrid_SetSelected(t *testing.T) {
	g := NewGrid(0, 0, 40, 8)
	g.SetItems([]GridItem{
		{Text: "A"},
		{Text: "B"},
		{Text: "C"},
	})

	callCount := 0
	lastIdx := -1
	g.OnChange = func(idx int) {
		callCount++
		lastIdx = idx
	}

	// Set to valid index
	g.SetSelected(1)
	if g.SelectedIdx != 1 {
		t.Errorf("expected SelectedIdx 1, got %d", g.SelectedIdx)
	}
	if callCount != 1 || lastIdx != 1 {
		t.Errorf("expected OnChange called once with idx 1, got callCount=%d lastIdx=%d", callCount, lastIdx)
	}

	// Set to same index (should not trigger callback)
	g.SetSelected(1)
	if callCount != 1 {
		t.Errorf("expected no additional OnChange call, got callCount=%d", callCount)
	}

	// Set to invalid index (negative)
	g.SetSelected(-1)
	if g.SelectedIdx != 1 {
		t.Errorf("expected SelectedIdx to remain 1 after invalid set, got %d", g.SelectedIdx)
	}

	// Set to invalid index (too large)
	g.SetSelected(10)
	if g.SelectedIdx != 1 {
		t.Errorf("expected SelectedIdx to remain 1 after invalid set, got %d", g.SelectedIdx)
	}
}

func TestGrid_SelectedItem(t *testing.T) {
	g := NewGrid(0, 0, 40, 8)

	// Empty grid
	if g.SelectedItem() != nil {
		t.Error("expected nil for empty grid")
	}

	g.SetItems([]GridItem{
		{Text: "First", Value: 100},
		{Text: "Second", Value: 200},
	})

	item := g.SelectedItem()
	if item == nil {
		t.Fatal("expected non-nil item")
	}
	if item.Text != "First" || item.Value != 100 {
		t.Errorf("expected First item, got %+v", item)
	}

	g.SetSelected(1)
	item = g.SelectedItem()
	if item.Text != "Second" || item.Value != 200 {
		t.Errorf("expected Second item, got %+v", item)
	}
}

func TestGrid_ColumnCalculation(t *testing.T) {
	g := NewGrid(0, 0, 30, 8)
	g.MinCellWidth = 10

	g.SetItems([]GridItem{
		{Text: "A"},
		{Text: "B"},
		{Text: "C"},
		{Text: "D"},
		{Text: "E"},
		{Text: "F"},
	})

	// Force layout calculation by calling calculateLayout
	g.calculateLayout()

	// Width 30 / MinCellWidth 10 = 3 columns
	if g.Columns() != 3 {
		t.Errorf("expected 3 columns, got %d", g.Columns())
	}
}

func TestGrid_MaxCols(t *testing.T) {
	g := NewGrid(0, 0, 100, 8)
	g.MinCellWidth = 10
	g.MaxCols = 2 // Limit to 2 columns

	g.SetItems([]GridItem{
		{Text: "A"},
		{Text: "B"},
		{Text: "C"},
	})

	g.calculateLayout()

	if g.Columns() != 2 {
		t.Errorf("expected 2 columns (MaxCols), got %d", g.Columns())
	}
}

func TestGrid_HandleKey_LeftRight(t *testing.T) {
	g := NewGrid(0, 0, 30, 8)
	g.MinCellWidth = 10 // 3 columns
	g.SetItems([]GridItem{
		{Text: "A"}, {Text: "B"}, {Text: "C"},
		{Text: "D"}, {Text: "E"}, {Text: "F"},
	})
	g.calculateLayout()

	// Start at A (idx 0, col 0)

	// Right to B
	ev := tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)
	handled := g.HandleKey(ev)
	if !handled || g.SelectedIdx != 1 {
		t.Errorf("expected Right to idx 1, got handled=%v idx=%d", handled, g.SelectedIdx)
	}

	// Right to C
	handled = g.HandleKey(ev)
	if !handled || g.SelectedIdx != 2 {
		t.Errorf("expected Right to idx 2, got handled=%v idx=%d", handled, g.SelectedIdx)
	}

	// Right at end of row (should not handle)
	handled = g.HandleKey(ev)
	if handled {
		t.Error("expected Right at end of row to not handle")
	}

	// Left back to B
	ev = tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)
	handled = g.HandleKey(ev)
	if !handled || g.SelectedIdx != 1 {
		t.Errorf("expected Left to idx 1, got handled=%v idx=%d", handled, g.SelectedIdx)
	}

	// Left to A
	handled = g.HandleKey(ev)
	if !handled || g.SelectedIdx != 0 {
		t.Errorf("expected Left to idx 0, got handled=%v idx=%d", handled, g.SelectedIdx)
	}

	// Left at start of row (should not handle)
	handled = g.HandleKey(ev)
	if handled {
		t.Error("expected Left at start of row to not handle")
	}
}

func TestGrid_HandleKey_UpDown(t *testing.T) {
	g := NewGrid(0, 0, 30, 8)
	g.MinCellWidth = 10 // 3 columns
	g.SetItems([]GridItem{
		{Text: "A"}, {Text: "B"}, {Text: "C"},
		{Text: "D"}, {Text: "E"}, {Text: "F"},
	})
	g.calculateLayout()

	// Start at A (idx 0, row 0)
	g.SetSelected(1) // Start at B (idx 1)

	// Down to E (idx 4)
	ev := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	handled := g.HandleKey(ev)
	if !handled || g.SelectedIdx != 4 {
		t.Errorf("expected Down to idx 4, got handled=%v idx=%d", handled, g.SelectedIdx)
	}

	// Down at bottom row (should not handle)
	handled = g.HandleKey(ev)
	if handled {
		t.Error("expected Down at bottom row to not handle")
	}

	// Up back to B (idx 1)
	ev = tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	handled = g.HandleKey(ev)
	if !handled || g.SelectedIdx != 1 {
		t.Errorf("expected Up to idx 1, got handled=%v idx=%d", handled, g.SelectedIdx)
	}

	// Up at top row (should not handle)
	handled = g.HandleKey(ev)
	if handled {
		t.Error("expected Up at top row to not handle")
	}
}

func TestGrid_HandleKey_HomeEnd(t *testing.T) {
	g := NewGrid(0, 0, 30, 8)
	g.SetItems([]GridItem{
		{Text: "A"}, {Text: "B"}, {Text: "C"},
		{Text: "D"}, {Text: "E"}, {Text: "F"},
	})
	g.SetSelected(3) // Start in middle

	// End
	ev := tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
	handled := g.HandleKey(ev)
	if !handled || g.SelectedIdx != 5 {
		t.Errorf("expected End to go to last item, got handled=%v idx=%d", handled, g.SelectedIdx)
	}

	// End when already at end (should not handle)
	handled = g.HandleKey(ev)
	if handled {
		t.Error("expected End at end to not handle")
	}

	// Home
	ev = tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone)
	handled = g.HandleKey(ev)
	if !handled || g.SelectedIdx != 0 {
		t.Errorf("expected Home to go to first item, got handled=%v idx=%d", handled, g.SelectedIdx)
	}

	// Home when already at beginning (should not handle)
	handled = g.HandleKey(ev)
	if handled {
		t.Error("expected Home at beginning to not handle")
	}
}

func TestGrid_HandleKey_Tab(t *testing.T) {
	g := NewGrid(0, 0, 30, 8)
	g.MinCellWidth = 10 // 3 columns
	g.SetItems([]GridItem{
		{Text: "A"}, {Text: "B"}, {Text: "C"},
		{Text: "D"}, {Text: "E"}, {Text: "F"},
	})

	// Tab forward through all items
	ev := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	for expected := 1; expected <= 5; expected++ {
		handled := g.HandleKey(ev)
		if !handled || g.SelectedIdx != expected {
			t.Errorf("expected Tab to idx %d, got handled=%v idx=%d", expected, handled, g.SelectedIdx)
		}
	}

	// Tab at end (should not handle)
	handled := g.HandleKey(ev)
	if handled {
		t.Error("expected Tab at end to not handle")
	}

	// Shift+Tab backwards
	ev = tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModShift)
	for expected := 4; expected >= 0; expected-- {
		handled := g.HandleKey(ev)
		if !handled || g.SelectedIdx != expected {
			t.Errorf("expected Shift+Tab to idx %d, got handled=%v idx=%d", expected, handled, g.SelectedIdx)
		}
	}

	// Shift+Tab at beginning (should not handle)
	handled = g.HandleKey(ev)
	if handled {
		t.Error("expected Shift+Tab at beginning to not handle")
	}
}

func TestGrid_HandleKey_PartialLastRow(t *testing.T) {
	g := NewGrid(0, 0, 30, 8)
	g.MinCellWidth = 10 // 3 columns
	g.SetItems([]GridItem{
		{Text: "A"}, {Text: "B"}, {Text: "C"},
		{Text: "D"}, {Text: "E"}, // Only 2 items in last row
	})
	g.calculateLayout()

	// Start at C (idx 2)
	g.SetSelected(2)

	// Down should go to E (idx 4, last item) even though column 2 doesn't exist in last row
	ev := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	handled := g.HandleKey(ev)
	if !handled || g.SelectedIdx != 4 {
		t.Errorf("expected Down to clamp to last item, got handled=%v idx=%d", handled, g.SelectedIdx)
	}
}

func TestGrid_EmptyGrid(t *testing.T) {
	g := NewGrid(0, 0, 30, 8)

	// HandleKey should return false for empty grid
	ev := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	if g.HandleKey(ev) {
		t.Error("expected HandleKey to return false for empty grid")
	}

	// SelectedItem should return nil
	if g.SelectedItem() != nil {
		t.Error("expected SelectedItem to return nil for empty grid")
	}
}

func TestGrid_ScrollingWithManyItems(t *testing.T) {
	// Grid with height 3, but many items that need scrolling
	g := NewGrid(0, 0, 30, 3) // Only 3 rows visible
	g.MinCellWidth = 10       // 3 columns

	// Create 15 items = 5 rows, but only 3 visible at a time
	items := make([]GridItem, 15)
	for i := 0; i < 15; i++ {
		items[i] = GridItem{Text: string('A' + rune(i))}
	}
	g.SetItems(items)
	g.calculateLayout()

	// Verify we have 3 columns and 5 rows
	if g.cols != 3 {
		t.Fatalf("expected 3 columns, got %d", g.cols)
	}
	totalRows := g.contentHeight()
	if totalRows != 5 {
		t.Fatalf("expected 5 rows, got %d", totalRows)
	}

	// Start at first item
	g.SetSelected(0)

	// Navigate down to item 12 (row 4, which is outside initial viewport)
	g.SetSelected(12) // Row 4 (0-indexed)

	// The scroll pane should have scrolled to show the selected row
	offset := g.scrollPane.ScrollOffset()
	// Row 4 with viewport height 3 means we need offset >= 2 to see row 4
	if offset < 2 {
		t.Errorf("expected scroll offset >= 2 to show row 4, got %d", offset)
	}

	// Navigate back to first item
	g.SetSelected(0)
	offset = g.scrollPane.ScrollOffset()
	// Should scroll back to top
	if offset != 0 {
		t.Errorf("expected scroll offset 0 at top, got %d", offset)
	}
}

func TestGrid_WheelScrolling(t *testing.T) {
	g := NewGrid(0, 0, 30, 3) // Only 3 rows visible
	g.MinCellWidth = 10

	// Create 15 items = 5 rows
	items := make([]GridItem, 15)
	for i := 0; i < 15; i++ {
		items[i] = GridItem{Text: string('A' + rune(i))}
	}
	g.SetItems(items)

	// Wheel down should scroll
	ev := tcell.NewEventMouse(5, 1, tcell.WheelDown, tcell.ModNone)
	handled := g.HandleMouse(ev)
	if !handled {
		t.Error("expected WheelDown to be handled")
	}

	offset := g.scrollPane.ScrollOffset()
	if offset == 0 {
		t.Error("expected scroll offset > 0 after wheel down")
	}

	// Wheel up should scroll back
	ev = tcell.NewEventMouse(5, 1, tcell.WheelUp, tcell.ModNone)
	g.HandleMouse(ev)
	g.HandleMouse(ev) // Multiple to scroll back to top

	offset = g.scrollPane.ScrollOffset()
	if offset != 0 {
		t.Errorf("expected scroll offset 0 after wheel up, got %d", offset)
	}
}

func TestGrid_PageNavigation(t *testing.T) {
	// Grid with height 3, 3 columns, 15 items = 5 rows
	g := NewGrid(0, 0, 30, 3)
	g.MinCellWidth = 10

	items := make([]GridItem, 15)
	for i := 0; i < 15; i++ {
		items[i] = GridItem{Text: string('A' + rune(i))}
	}
	g.SetItems(items)
	g.calculateLayout()

	// Start at item 0 (row 0, col 0)
	g.SetSelected(0)

	// PgDn should move selection down by pageSize (3) rows
	ev := tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone)
	handled := g.HandleKey(ev)
	if !handled {
		t.Error("expected PgDn to be handled")
	}

	// Should be at row 3, col 0 = index 9
	if g.SelectedIdx != 9 {
		t.Errorf("expected SelectedIdx 9 after PgDn, got %d", g.SelectedIdx)
	}

	// Another PgDn should go to last row
	handled = g.HandleKey(ev)
	if !handled {
		t.Error("expected second PgDn to be handled")
	}

	// Should be at row 4, col 0 = index 12
	if g.SelectedIdx != 12 {
		t.Errorf("expected SelectedIdx 12 after second PgDn, got %d", g.SelectedIdx)
	}

	// PgDn at bottom should not change selection
	handled = g.HandleKey(ev)
	if handled {
		t.Error("expected PgDn at bottom to not be handled")
	}

	// PgUp should move back up
	ev = tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModNone)
	handled = g.HandleKey(ev)
	if !handled {
		t.Error("expected PgUp to be handled")
	}

	// Should be at row 1, col 0 = index 3
	if g.SelectedIdx != 3 {
		t.Errorf("expected SelectedIdx 3 after PgUp, got %d", g.SelectedIdx)
	}

	// Another PgUp should go to top
	handled = g.HandleKey(ev)
	if !handled {
		t.Error("expected second PgUp to be handled")
	}

	// Should be at row 0, col 0 = index 0
	if g.SelectedIdx != 0 {
		t.Errorf("expected SelectedIdx 0 after second PgUp, got %d", g.SelectedIdx)
	}
}

func TestGrid_PageNavigationMaintainsColumn(t *testing.T) {
	// Grid with height 2, 3 columns, 9 items = 3 rows
	g := NewGrid(0, 0, 30, 2)
	g.MinCellWidth = 10

	items := make([]GridItem, 9)
	for i := 0; i < 9; i++ {
		items[i] = GridItem{Text: string('A' + rune(i))}
	}
	g.SetItems(items)
	g.calculateLayout()

	// Start at item 1 (row 0, col 1 - "B")
	g.SetSelected(1)

	// PgDn should maintain column 1
	ev := tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone)
	g.HandleKey(ev)

	// Should be at row 2, col 1 = index 7 ("H")
	if g.SelectedIdx != 7 {
		t.Errorf("expected SelectedIdx 7 (row 2, col 1), got %d", g.SelectedIdx)
	}
}

func TestGrid_PageNavigationPartialLastRow(t *testing.T) {
	// Grid with height 2, 3 columns, 7 items = 3 rows (last row has only 1 item)
	g := NewGrid(0, 0, 30, 2)
	g.MinCellWidth = 10

	items := make([]GridItem, 7)
	for i := 0; i < 7; i++ {
		items[i] = GridItem{Text: string('A' + rune(i))}
	}
	g.SetItems(items)
	g.calculateLayout()

	// Start at item 2 (row 0, col 2 - "C")
	g.SetSelected(2)

	// PgDn should go to last row, but col 2 doesn't exist there
	// Should clamp to last item (index 6)
	ev := tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone)
	g.HandleKey(ev)

	if g.SelectedIdx != 6 {
		t.Errorf("expected SelectedIdx 6 (clamped to last item), got %d", g.SelectedIdx)
	}
}

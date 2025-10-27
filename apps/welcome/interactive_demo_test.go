package welcome

import "testing"

func TestInteractiveDemoTablePopulated(t *testing.T) {
	demo := &interactiveDemoApp{}
	demo.createLayout()
	demo.updateTable()

	cell := demo.table.GetCell(1, 0)
	if cell == nil || cell.Text != "Counter" {
		t.Fatalf("expected first data row to be Counter, got %+v", cell)
	}
}

package widgets

import (
	"testing"
	"texelation/texel"
	"texelation/texelui/core"

	"github.com/gdamore/tcell/v2"
)

// createTestBuffer creates a buffer for testing widget rendering.
func createTestBuffer(w, h int) [][]texel.Cell {
	buf := make([][]texel.Cell, h)
	for y := 0; y < h; y++ {
		buf[y] = make([]texel.Cell, w)
	}
	return buf
}

// newTestInput creates an Input for testing with the given width.
func newTestInput(w int) *Input {
	input := NewInput()
	input.Resize(w, 1)
	return input
}

func TestLabelCreation(t *testing.T) {
	label := NewLabel("Test")
	if label.Text != "Test" {
		t.Errorf("expected text 'Test', got '%s'", label.Text)
	}
	x, y := label.Position()
	if x != 0 || y != 0 {
		t.Errorf("expected position (0,0), got (%d,%d)", x, y)
	}
}

func TestLabelDraw(t *testing.T) {
	buf := createTestBuffer(20, 3)
	label := NewLabel("Hello")
	label.Resize(10, 1)
	painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 20, H: 3})
	label.Draw(painter)

	// Check that some cells were written
	cellsWritten := 0
	for y := 0; y < 3; y++ {
		for x := 0; x < 20; x++ {
			if buf[y][x].Ch != 0 {
				cellsWritten++
			}
		}
	}
	if cellsWritten == 0 {
		t.Error("expected label to write cells")
	}
}

func TestButtonCreation(t *testing.T) {
	clicked := false
	button := NewButton("Click")
	button.OnClick = func() {
		clicked = true
	}

	if button.Text != "Click" {
		t.Errorf("expected text 'Click', got '%s'", button.Text)
	}

	// Simulate key press
	ev := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	button.HandleKey(ev)

	if !clicked {
		t.Error("expected button to trigger OnClick on Enter key")
	}
}

func TestButtonDraw(t *testing.T) {
	buf := createTestBuffer(20, 3)
	button := NewButton("Test")
	button.Resize(15, 1)
	painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 20, H: 3})
	button.Draw(painter)

	// Check that cells were written
	cellsWritten := 0
	for y := 0; y < 3; y++ {
		for x := 0; x < 20; x++ {
			if buf[y][x].Ch != 0 {
				cellsWritten++
			}
		}
	}
	if cellsWritten == 0 {
		t.Error("expected button to write cells")
	}
}

func TestInputCreation(t *testing.T) {
	input := newTestInput(20)
	if input.Text != "" {
		t.Errorf("expected empty text, got '%s'", input.Text)
	}
	if input.CaretPos != 0 {
		t.Errorf("expected caret at 0, got %d", input.CaretPos)
	}
}

func TestInputTextEntry(t *testing.T) {
	input := newTestInput(20)
	input.SetFocusable(true)
	input.Focus()

	// Simulate typing "Hi"
	ev1 := tcell.NewEventKey(tcell.KeyRune, 'H', tcell.ModNone)
	ev2 := tcell.NewEventKey(tcell.KeyRune, 'i', tcell.ModNone)

	input.HandleKey(ev1)
	input.HandleKey(ev2)

	if input.Text != "Hi" {
		t.Errorf("expected text 'Hi', got '%s'", input.Text)
	}
	if input.CaretPos != 2 {
		t.Errorf("expected caret at 2, got %d", input.CaretPos)
	}
}

func TestInputBackspace(t *testing.T) {
	input := newTestInput(20)
	input.Text = "Test"
	input.CaretPos = 4

	// Backspace should delete 't'
	ev := tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModNone)
	input.HandleKey(ev)

	if input.Text != "Tes" {
		t.Errorf("expected text 'Tes', got '%s'", input.Text)
	}
	if input.CaretPos != 3 {
		t.Errorf("expected caret at 3, got %d", input.CaretPos)
	}
}

func TestInputNavigation(t *testing.T) {
	input := newTestInput(20)
	input.Text = "Hello"
	input.CaretPos = 5

	// Left arrow
	ev := tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)
	input.HandleKey(ev)
	if input.CaretPos != 4 {
		t.Errorf("expected caret at 4, got %d", input.CaretPos)
	}

	// Home
	ev = tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone)
	input.HandleKey(ev)
	if input.CaretPos != 0 {
		t.Errorf("expected caret at 0, got %d", input.CaretPos)
	}

	// End
	ev = tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
	input.HandleKey(ev)
	if input.CaretPos != 5 {
		t.Errorf("expected caret at 5, got %d", input.CaretPos)
	}
}

func TestCheckboxCreation(t *testing.T) {
	checkbox := NewCheckbox(0, 0, "Enable feature")
	if checkbox.Label != "Enable feature" {
		t.Errorf("expected label 'Enable feature', got '%s'", checkbox.Label)
	}
	if checkbox.Checked {
		t.Error("expected checkbox to be unchecked by default")
	}
}

func TestCheckboxToggle(t *testing.T) {
	changeCount := 0
	var lastState bool
	checkbox := NewCheckbox(0, 0, "Test")
	checkbox.OnChange = func(checked bool) {
		changeCount++
		lastState = checked
	}

	// Toggle with space key
	ev := tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)
	checkbox.HandleKey(ev)

	if !checkbox.Checked {
		t.Error("expected checkbox to be checked after toggle")
	}
	if changeCount != 1 {
		t.Errorf("expected OnChange to be called once, got %d", changeCount)
	}
	if !lastState {
		t.Error("expected OnChange to receive true")
	}

	// Toggle again
	checkbox.HandleKey(ev)
	if checkbox.Checked {
		t.Error("expected checkbox to be unchecked after second toggle")
	}
	if changeCount != 2 {
		t.Errorf("expected OnChange to be called twice, got %d", changeCount)
	}
	if lastState {
		t.Error("expected OnChange to receive false")
	}
}

func TestCheckboxDraw(t *testing.T) {
	buf := createTestBuffer(30, 3)
	checkbox := NewCheckbox(0, 0, "Test option")
	painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 30, H: 3})

	// Draw unchecked
	checkbox.Draw(painter)

	// Check that cells were written
	cellsWritten := 0
	for y := 0; y < 3; y++ {
		for x := 0; x < 30; x++ {
			if buf[y][x].Ch != 0 {
				cellsWritten++
			}
		}
	}
	if cellsWritten == 0 {
		t.Error("expected checkbox to write cells")
	}

	// Clear buffer and draw checked
	buf = createTestBuffer(30, 3)
	checkbox.Checked = true
	painter = core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 30, H: 3})
	checkbox.Draw(painter)

	// Verify cells were written again
	cellsWritten = 0
	for y := 0; y < 3; y++ {
		for x := 0; x < 30; x++ {
			if buf[y][x].Ch != 0 {
				cellsWritten++
			}
		}
	}
	if cellsWritten == 0 {
		t.Error("expected checked checkbox to write cells")
	}
}

func TestInputInsertReplaceMode(t *testing.T) {
	input := newTestInput(20)
	input.Text = "Hello"
	input.CaretPos = 1

	// Start in insert mode (default)
	if input.replaceMode {
		t.Error("expected insert mode by default")
	}

	// Type in insert mode - should insert
	ev := tcell.NewEventKey(tcell.KeyRune, 'X', tcell.ModNone)
	input.HandleKey(ev)

	if input.Text != "HXello" {
		t.Errorf("expected 'HXello' after insert, got '%s'", input.Text)
	}
	if input.CaretPos != 2 {
		t.Errorf("expected caret at 2, got %d", input.CaretPos)
	}

	// Toggle to replace mode
	evInsert := tcell.NewEventKey(tcell.KeyInsert, 0, tcell.ModNone)
	input.HandleKey(evInsert)

	if !input.replaceMode {
		t.Error("expected replace mode after Insert key")
	}

	// Type in replace mode - should overwrite
	ev = tcell.NewEventKey(tcell.KeyRune, 'Y', tcell.ModNone)
	input.HandleKey(ev)

	if input.Text != "HXYllo" {
		t.Errorf("expected 'HXYllo' after replace, got '%s'", input.Text)
	}
	if input.CaretPos != 3 {
		t.Errorf("expected caret at 3, got %d", input.CaretPos)
	}

	// Toggle back to insert mode
	input.HandleKey(evInsert)

	if input.replaceMode {
		t.Error("expected insert mode after second Insert key")
	}

	// Type in insert mode again
	ev = tcell.NewEventKey(tcell.KeyRune, 'Z', tcell.ModNone)
	input.HandleKey(ev)

	if input.Text != "HXYZllo" {
		t.Errorf("expected 'HXYZllo' after insert, got '%s'", input.Text)
	}
}

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
	checkbox := NewCheckbox("Enable feature")
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
	checkbox := NewCheckbox("Test")
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
	checkbox := NewCheckbox("Test option")
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

// --- VBox / HBox Tests ---

func TestVBoxNaturalSize(t *testing.T) {
	vbox := NewVBox()
	vbox.Spacing = 0

	// Add two buttons with known sizes
	b1 := NewButton("A")  // 5 chars wide (A + 4 padding)
	b2 := NewButton("BB") // 6 chars wide

	vbox.AddChild(b1)
	vbox.AddChild(b2)

	// Natural size should be: width = max(5, 6) = 6, height = 1 + 1 = 2
	w, h := vbox.Size()
	if w != 6 {
		t.Errorf("VBox width: expected 6, got %d", w)
	}
	if h != 2 {
		t.Errorf("VBox height: expected 2, got %d", h)
	}
}

func TestVBoxNaturalSizeWithSpacing(t *testing.T) {
	vbox := NewVBox()
	vbox.Spacing = 1

	b1 := NewButton("A") // height 1
	b2 := NewButton("B") // height 1

	vbox.AddChild(b1)
	vbox.AddChild(b2)

	// Natural size with spacing: height = 1 + 1 + 1 (spacing) = 3
	_, h := vbox.Size()
	if h != 3 {
		t.Errorf("VBox height with spacing: expected 3, got %d", h)
	}
}

func TestHBoxNaturalSize(t *testing.T) {
	hbox := NewHBox()
	hbox.Spacing = 0

	b1 := NewButton("A")  // 5 chars wide
	b2 := NewButton("BB") // 6 chars wide

	hbox.AddChild(b1)
	hbox.AddChild(b2)

	// Natural size should be: width = 5 + 6 = 11, height = max(1, 1) = 1
	w, h := hbox.Size()
	if w != 11 {
		t.Errorf("HBox width: expected 11, got %d", w)
	}
	if h != 1 {
		t.Errorf("HBox height: expected 1, got %d", h)
	}
}

func TestHBoxNaturalSizeWithSpacing(t *testing.T) {
	hbox := NewHBox()
	hbox.Spacing = 2

	b1 := NewButton("A") // 5 chars wide
	b2 := NewButton("B") // 5 chars wide

	hbox.AddChild(b1)
	hbox.AddChild(b2)

	// Natural size with spacing: width = 5 + 2 + 5 = 12
	w, _ := hbox.Size()
	if w != 12 {
		t.Errorf("HBox width with spacing: expected 12, got %d", w)
	}
}

func TestNestedVBoxInHBox(t *testing.T) {
	// This tests the bug where VBox inside HBox reported size (1,1) instead of natural size
	leftVBox := NewVBox()
	leftVBox.Spacing = 0
	leftVBox.AddChild(NewButton("L1")) // 6 chars
	leftVBox.AddChild(NewButton("L2")) // 6 chars

	rightVBox := NewVBox()
	rightVBox.Spacing = 0
	rightVBox.AddChild(NewButton("R1")) // 6 chars
	rightVBox.AddChild(NewButton("R2")) // 6 chars

	hbox := NewHBox()
	hbox.Spacing = 2
	hbox.AddChild(leftVBox)
	hbox.AddChild(rightVBox)

	// VBox natural sizes should be (6, 2) each
	lw, lh := leftVBox.Size()
	if lw != 6 || lh != 2 {
		t.Errorf("Left VBox size: expected (6, 2), got (%d, %d)", lw, lh)
	}

	rw, rh := rightVBox.Size()
	if rw != 6 || rh != 2 {
		t.Errorf("Right VBox size: expected (6, 2), got (%d, %d)", rw, rh)
	}

	// HBox natural size should be: width = 6 + 2 + 6 = 14, height = 2
	hw, hh := hbox.Size()
	if hw != 14 {
		t.Errorf("HBox width: expected 14, got %d", hw)
	}
	if hh != 2 {
		t.Errorf("HBox height: expected 2, got %d", hh)
	}
}

func TestNestedVBoxInHBoxLayout(t *testing.T) {
	// Test that after resizing the HBox, children are positioned correctly
	leftVBox := NewVBox()
	leftVBox.Spacing = 0
	b1 := NewButton("L1")
	b2 := NewButton("L2")
	leftVBox.AddChild(b1)
	leftVBox.AddChild(b2)

	rightVBox := NewVBox()
	rightVBox.Spacing = 0
	b3 := NewButton("R1")
	b4 := NewButton("R2")
	rightVBox.AddChild(b3)
	rightVBox.AddChild(b4)

	hbox := NewHBox()
	hbox.Spacing = 2
	hbox.AddChild(leftVBox)
	hbox.AddChild(rightVBox)

	// Resize HBox to a larger size
	hbox.SetPosition(0, 0)
	hbox.Resize(40, 10)

	// Left VBox should be at (0, 0) with width = 6 (natural width)
	if leftVBox.Rect.X != 0 || leftVBox.Rect.Y != 0 {
		t.Errorf("Left VBox position: expected (0, 0), got (%d, %d)", leftVBox.Rect.X, leftVBox.Rect.Y)
	}
	if leftVBox.Rect.W != 6 {
		t.Errorf("Left VBox width: expected 6, got %d", leftVBox.Rect.W)
	}

	// Right VBox should be at (6 + 2 = 8, 0) with width = 6
	if rightVBox.Rect.X != 8 || rightVBox.Rect.Y != 0 {
		t.Errorf("Right VBox position: expected (8, 0), got (%d, %d)", rightVBox.Rect.X, rightVBox.Rect.Y)
	}
	if rightVBox.Rect.W != 6 {
		t.Errorf("Right VBox width: expected 6, got %d", rightVBox.Rect.W)
	}

	// Buttons inside VBoxes should have correct positions and full VBox width
	if b1.Rect.X != 0 || b1.Rect.Y != 0 {
		t.Errorf("Button L1 position: expected (0, 0), got (%d, %d)", b1.Rect.X, b1.Rect.Y)
	}
	if b1.Rect.W != 6 {
		t.Errorf("Button L1 width: expected 6, got %d", b1.Rect.W)
	}

	if b3.Rect.X != 8 || b3.Rect.Y != 0 {
		t.Errorf("Button R1 position: expected (8, 0), got (%d, %d)", b3.Rect.X, b3.Rect.Y)
	}
	if b3.Rect.W != 6 {
		t.Errorf("Button R1 width: expected 6, got %d", b3.Rect.W)
	}
}

func TestNestedHBoxInVBox(t *testing.T) {
	// Test the reverse nesting: HBox inside VBox
	topHBox := NewHBox()
	topHBox.Spacing = 1
	topHBox.AddChild(NewButton("A")) // 5 chars
	topHBox.AddChild(NewButton("B")) // 5 chars
	// HBox natural size: (5 + 1 + 5 = 11, 1)

	bottomHBox := NewHBox()
	bottomHBox.Spacing = 1
	bottomHBox.AddChild(NewButton("CC")) // 6 chars
	bottomHBox.AddChild(NewButton("DD")) // 6 chars
	// HBox natural size: (6 + 1 + 6 = 13, 1)

	vbox := NewVBox()
	vbox.Spacing = 0
	vbox.AddChild(topHBox)
	vbox.AddChild(bottomHBox)

	// VBox natural size should be: width = max(11, 13) = 13, height = 1 + 1 = 2
	w, h := vbox.Size()
	if w != 13 {
		t.Errorf("VBox width: expected 13, got %d", w)
	}
	if h != 2 {
		t.Errorf("VBox height: expected 2, got %d", h)
	}
}

func TestBoxWithFlexChild(t *testing.T) {
	hbox := NewHBox()
	hbox.Spacing = 0

	b1 := NewButton("A") // 5 chars, natural size
	b2 := NewButton("B") // 5 chars, flex

	hbox.AddChild(b1)
	hbox.AddFlexChild(b2)

	// Flex children are skipped in natural size calculation
	// Natural size should just include b1: (5, 1)
	w, h := hbox.Size()
	if w != 5 {
		t.Errorf("HBox width with flex: expected 5, got %d", w)
	}
	if h != 1 {
		t.Errorf("HBox height with flex: expected 1, got %d", h)
	}

	// After resize, flex child should expand
	hbox.SetPosition(0, 0)
	hbox.Resize(20, 3)

	// b1 should have width 5, b2 should have remaining width (20 - 5 = 15)
	if b1.Rect.W != 5 {
		t.Errorf("b1 width after resize: expected 5, got %d", b1.Rect.W)
	}
	if b2.Rect.W != 15 {
		t.Errorf("b2 (flex) width after resize: expected 15, got %d", b2.Rect.W)
	}
}

func TestBoxWithFixedSizeChild(t *testing.T) {
	hbox := NewHBox()
	hbox.Spacing = 0

	b1 := NewButton("A") // natural 5 chars, but fixed to 10

	hbox.AddChildWithSize(b1, 10)

	hbox.SetPosition(0, 0)
	hbox.Resize(30, 3)

	// b1 should have fixed width of 10, not natural 5
	if b1.Rect.W != 10 {
		t.Errorf("b1 fixed width: expected 10, got %d", b1.Rect.W)
	}
}

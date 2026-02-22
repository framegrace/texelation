// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package texelbrowse

import (
	"testing"

	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
)

func TestLayout_ReadingMode(t *testing.T) {
	lm := NewLayoutManager(80, 24)
	lm.SetMode(ModeReading)

	if lm.Mode() != ModeReading {
		t.Fatalf("expected ModeReading, got %d", lm.Mode())
	}

	ws := []core.Widget{
		widgets.NewLabel("# Welcome"),
		widgets.NewLabel("Some paragraph text"),
		widgets.NewInput(),
		widgets.NewLink("Click me"),
	}

	lm.Arrange(ws)

	// Margin is 1 on each side, so content width is 78.
	const margin = 1
	contentWidth := 80 - 2*margin

	// Verify all widgets are at x=margin.
	for i, w := range ws {
		x, _ := w.Position()
		if x != margin {
			t.Errorf("widget[%d]: x = %d, want %d", i, x, margin)
		}
	}

	// Verify widgets are positioned vertically in sequence.
	prevY := -1
	for i, w := range ws {
		_, y := w.Position()
		if y <= prevY {
			t.Errorf("widget[%d]: y = %d, not > previous y = %d", i, y, prevY)
		}
		prevY = y
	}

	// Input should get full content width.
	inp := ws[2].(*widgets.Input)
	iw, _ := inp.Size()
	if iw != contentWidth {
		t.Errorf("input width = %d, want %d", iw, contentWidth)
	}

	// Label should keep its natural width (clamped to content width).
	lbl := ws[0].(*widgets.Label)
	lw, _ := lbl.Size()
	if lw > contentWidth {
		t.Errorf("label width = %d, exceeds content width %d", lw, contentWidth)
	}
	// "# Welcome" is 9 chars, should stay at natural width.
	if lw != len("# Welcome") {
		t.Errorf("label width = %d, want %d", lw, len("# Welcome"))
	}
}

func TestLayout_ReadingMode_NarrowTerminal(t *testing.T) {
	lm := NewLayoutManager(10, 24)
	lm.SetMode(ModeReading)

	longLabel := widgets.NewLabel("This label text is very long and exceeds the terminal width")
	ws := []core.Widget{longLabel}

	lm.Arrange(ws)

	// Content width = 10 - 2 = 8.
	w, _ := longLabel.Size()
	if w > 8 {
		t.Errorf("label width = %d, should be clamped to content width 8", w)
	}
}

func TestLayout_FormMode(t *testing.T) {
	lm := NewLayoutManager(80, 24)
	lm.SetMode(ModeForm)

	if lm.Mode() != ModeForm {
		t.Fatalf("expected ModeForm, got %d", lm.Mode())
	}

	label := widgets.NewLabel("Login Form")
	input1 := widgets.NewInput()
	input2 := widgets.NewInput()
	btn := widgets.NewButton("Submit")

	ws := []core.Widget{label, input1, input2, btn}
	lm.Arrange(ws)

	const margin = 2
	contentWidth := 80 - 2*margin

	// All widgets should be at x=margin.
	for i, w := range ws {
		x, _ := w.Position()
		if x != margin {
			t.Errorf("widget[%d]: x = %d, want %d", i, x, margin)
		}
	}

	// Inputs should have width >= 40 and equal to content width.
	for i, inp := range []*widgets.Input{input1, input2} {
		iw, _ := inp.Size()
		if iw < 40 {
			t.Errorf("input[%d] width = %d, want >= 40", i, iw)
		}
		if iw != contentWidth {
			t.Errorf("input[%d] width = %d, want %d", i, iw, contentWidth)
		}
	}

	// Button should get 1/3 content width.
	bw, _ := btn.Size()
	expectedBtnWidth := contentWidth / 3
	if bw != expectedBtnWidth {
		t.Errorf("button width = %d, want %d", bw, expectedBtnWidth)
	}

	// Verify vertical ordering.
	prevY := -1
	for i, w := range ws {
		_, y := w.Position()
		if y <= prevY {
			t.Errorf("widget[%d]: y = %d, not > previous y = %d", i, y, prevY)
		}
		prevY = y
	}
}

func TestLayout_FormMode_ExtraSpacing(t *testing.T) {
	lm := NewLayoutManager(80, 24)
	lm.SetMode(ModeForm)

	label := widgets.NewLabel("Title")
	input := widgets.NewInput()
	btn := widgets.NewButton("Go")

	ws := []core.Widget{label, input, btn}
	lm.Arrange(ws)

	_, labelY := label.Position()
	_, inputY := input.Position()
	_, btnY := btn.Position()

	// Label is at y=0, height 1.
	if labelY != 0 {
		t.Errorf("label y = %d, want 0", labelY)
	}

	// Input is interactive, preceded by non-interactive label.
	// Extra spacing of 2 rows should be added before input.
	_, labelH := label.Size()
	expectedInputY := labelH + 2
	if inputY != expectedInputY {
		t.Errorf("input y = %d, want %d (label bottom + 2 spacing)", inputY, expectedInputY)
	}

	// Button is interactive, preceded by interactive input.
	// Extra spacing of 2 rows should be added before button.
	_, inputH := input.Size()
	expectedBtnY := inputY + inputH + 2
	if btnY != expectedBtnY {
		t.Errorf("button y = %d, want %d (input bottom + 2 spacing)", btnY, expectedBtnY)
	}
}

func TestLayout_FormMode_MinInputWidth(t *testing.T) {
	// Terminal narrower than 44 (min input 40 + 2*2 margin).
	lm := NewLayoutManager(30, 24)
	lm.SetMode(ModeForm)

	input := widgets.NewInput()
	ws := []core.Widget{input}
	lm.Arrange(ws)

	// Content width = 30 - 4 = 26, which is < 40.
	// Min input width is clamped to content width.
	iw, _ := input.Size()
	if iw != 26 {
		t.Errorf("input width = %d, want 26 (content width when < 40)", iw)
	}
}

func TestLayout_Resize(t *testing.T) {
	lm := NewLayoutManager(80, 24)
	lm.SetMode(ModeReading)

	input := widgets.NewInput()
	ws := []core.Widget{input}

	lm.Arrange(ws)
	w1, _ := input.Size()

	lm.Resize(60, 20)
	lm.Arrange(ws)
	w2, _ := input.Size()

	// After resize, input width should change.
	if w1 == w2 {
		t.Errorf("input width did not change after Resize: %d == %d", w1, w2)
	}
	expectedW2 := 60 - 2 // 1 margin each side
	if w2 != expectedW2 {
		t.Errorf("input width after resize = %d, want %d", w2, expectedW2)
	}
}

func TestLayout_EmptyWidgetList(t *testing.T) {
	lm := NewLayoutManager(80, 24)
	// Should not panic on empty list.
	lm.Arrange(nil)
	lm.Arrange([]core.Widget{})
}

func TestLayout_CheckboxInFormMode(t *testing.T) {
	lm := NewLayoutManager(80, 24)
	lm.SetMode(ModeForm)

	label := widgets.NewLabel("Options")
	cb := widgets.NewCheckbox("Accept terms")

	ws := []core.Widget{label, cb}
	lm.Arrange(ws)

	// Checkbox is interactive, so extra spacing before it.
	_, labelY := label.Position()
	_, cbY := cb.Position()
	_, labelH := label.Size()

	expectedCbY := labelY + labelH + 2
	if cbY != expectedCbY {
		t.Errorf("checkbox y = %d, want %d (label bottom + 2 spacing)", cbY, expectedCbY)
	}
}

func TestLayout_DefaultMode(t *testing.T) {
	lm := NewLayoutManager(80, 24)
	if lm.Mode() != ModeReading {
		t.Errorf("default mode = %d, want ModeReading", lm.Mode())
	}
}

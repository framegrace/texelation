// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package boot

import (
	"strings"
	"testing"
	"time"
)

// rowToString flattens a row of runes into a string for equality checks.
func rowToString(row []rune) string {
	return string(row)
}

// findCenteredText locates the first row that contains text. Used to
// confirm the splash actually wrote our status string somewhere on the
// canvas without baking specific row coordinates into the test (which
// would couple to box geometry).
func findCenteredText(frame Frame, needle string) (row int, ok bool) {
	for y, runes := range frame.Runes {
		if strings.Contains(rowToString(runes), needle) {
			return y, true
		}
	}
	return -1, false
}

func TestApp_RenderFrame_DimensionsMatch(t *testing.T) {
	a := New("Texelation")
	a.SetSize(80, 24)
	frame := a.RenderFrame()

	if frame.Width != 80 || frame.Height != 24 {
		t.Fatalf("frame size mismatch: got %dx%d, want 80x24", frame.Width, frame.Height)
	}
	if len(frame.Runes) != 24 || len(frame.Styles) != 24 {
		t.Fatalf("frame row counts mismatch: runes=%d styles=%d", len(frame.Runes), len(frame.Styles))
	}
	for y := 0; y < frame.Height; y++ {
		if got := len(frame.Runes[y]); got != 80 {
			t.Errorf("row %d rune width: got %d, want 80", y, got)
		}
		if got := len(frame.Styles[y]); got != 80 {
			t.Errorf("row %d style width: got %d, want 80", y, got)
		}
	}
}

func TestApp_RenderFrame_ZeroSizeReturnsEmpty(t *testing.T) {
	a := New("Texelation")
	frame := a.RenderFrame()
	if frame.Width != 0 || frame.Height != 0 {
		t.Fatalf("expected zero-size frame, got %dx%d", frame.Width, frame.Height)
	}
	if frame.Runes != nil || frame.Styles != nil {
		t.Errorf("expected nil grids on zero-size frame")
	}
}

func TestApp_RenderFrame_TitleAndStageAppear(t *testing.T) {
	a := New("Texelation")
	a.SetSize(80, 24)
	a.SetStage(StageStarting)
	frame := a.RenderFrame()

	if _, ok := findCenteredText(frame, "Texelation"); !ok {
		t.Errorf("expected title 'Texelation' on canvas")
	}
	if _, ok := findCenteredText(frame, string(StageStarting)); !ok {
		t.Errorf("expected stage %q on canvas", StageStarting)
	}
}

func TestApp_RenderFrame_DetailAppearsBelowStage(t *testing.T) {
	a := New("Texelation")
	a.SetSize(80, 24)
	a.SetStage(StageResuming)
	a.SetDetail("2/3 panes")
	frame := a.RenderFrame()

	stageRow, ok := findCenteredText(frame, string(StageResuming))
	if !ok {
		t.Fatalf("stage missing")
	}
	detailRow, ok := findCenteredText(frame, "2/3 panes")
	if !ok {
		t.Fatalf("detail missing")
	}
	if detailRow != stageRow+1 {
		t.Errorf("detail row %d should be directly below stage row %d", detailRow, stageRow)
	}
}

func TestApp_SetStage_ClearsDetail(t *testing.T) {
	a := New("Texelation")
	a.SetSize(80, 24)
	a.SetStage(StageStarting)
	a.SetDetail("transient")
	a.SetStage(StageConnecting)

	frame := a.RenderFrame()
	if _, ok := findCenteredText(frame, "transient"); ok {
		t.Errorf("stale detail leaked into next stage")
	}
}

func TestApp_RenderFrame_NarrowCanvasFitsInsideBox(t *testing.T) {
	a := New("Texelation server")
	a.SetSize(20, 10)
	frame := a.RenderFrame()

	if frame.Width != 20 || frame.Height != 10 {
		t.Fatalf("frame %dx%d", frame.Width, frame.Height)
	}
	for y, row := range frame.Runes {
		if len(row) != 20 {
			t.Errorf("row %d width drift: %d", y, len(row))
		}
	}
}

func TestApp_RenderFrame_ErrorStageStyledRed(t *testing.T) {
	a := New("Texelation")
	a.SetSize(80, 24)
	a.SetStage(StageError)
	a.SetDetail("connect refused")
	frame := a.RenderFrame()

	row, ok := findCenteredText(frame, string(StageError))
	if !ok {
		t.Fatalf("error stage text missing")
	}
	rowText := rowToString(frame.Runes[row])
	idx := strings.Index(rowText, string(StageError))
	if idx < 0 {
		t.Fatalf("could not locate stage text within row")
	}
	style := frame.Styles[row][idx]
	fg, _, _ := style.Decompose()
	// We don't assert the exact RGB triple, just confirm the error
	// path picked a non-default foreground so the user can see
	// something happened — exact values are an implementation
	// detail of stagePulse / the error-stage style.
	if fg == 0 {
		t.Errorf("expected error stage to set a foreground color, got default")
	}
}

func TestApp_FormatElapsed(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "(0.0s)"},
		{400 * time.Millisecond, "(0.4s)"},
		{12 * time.Second, "(12s)"},
		{75 * time.Second, "(1m 15s)"},
		{63*time.Second + 500*time.Millisecond, "(1m 03s)"},
	}
	for _, tc := range cases {
		if got := formatElapsed(tc.d); got != tc.want {
			t.Errorf("formatElapsed(%v): got %q, want %q", tc.d, got, tc.want)
		}
	}
}

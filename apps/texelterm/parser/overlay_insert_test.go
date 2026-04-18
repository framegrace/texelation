// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/overlay_insert_test.go
// Summary: Sparse-native ports of overlay / synthetic-line tests from
// vterm_memory_buffer_test.go. Covers GetContentText reads from the sparse
// store, RequestLineOverlay/SetOverlay writes, and cursor-follow behavior
// when RequestLineInsert inserts synthetic lines into a partially-filled
// viewport.
//
// Tests dropped relative to pre-sparse:
//   - `TestGetContentText_PrefersOverlayWhenEnabled` and
//     `TestLineHasContent_SyntheticAndOverlay` — pre-sparse modeled
//     `LogicalLine.Overlay` / `LogicalLine.Synthetic` as separate fields
//     from `Cells` and toggled between them with `ShowOverlay`. The sparse
//     store has no such distinction: overlay content is written directly to
//     the store via `SetOverlay` / `SetLine` and `ReadLine` always returns
//     those cells. The pre-sparse toggle semantics and the `Synthetic`/
//     `Overlay` discriminators no longer exist.
//   - `TestRequestLineOverlay_MarksDirtyAndInvalidatesCache` — sparse has no
//     viewport cache (the ViewWindow computes the grid from the store on
//     demand), so the "invalidate cache" half of the assertion is vacuous.
//     Dirty-tracking of overlay writes is covered indirectly by existing
//     sparse tests.

package parser

import (
	"fmt"
	"strings"
	"testing"
)

// TestGetContentText_OverlayWrittenViaSetOverlay verifies that content
// written through RequestLineOverlay lands in the sparse store and is
// returned by GetContentText. This is the sparse-native analogue of
// TestGetContentText_SyntheticLineWithOverlay: there is no Cells/Overlay
// split, so the test simply checks that overlay writes round-trip.
func TestGetContentText_OverlayWrittenViaSetOverlay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	width, height := 40, 10

	v := NewVTerm(width, height)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write real content on line 0.
	parseString(p, "real line\r\n")
	parseString(p, "another line\r\n")

	// Overlay synthetic text onto line 0 via the transformer API.
	writeTop := v.mainScreen.WriteTop()
	line0 := writeTop
	overlayText := "--- table border ---"
	overlayCells := makeCells(overlayText)
	v.RequestLineOverlay(line0, overlayCells)

	// GetContentText must return the overlay content (that's what the store holds now).
	got := v.GetContentText(line0, 0, line0, len(overlayCells))
	if got != overlayText {
		t.Errorf("overlay-written line: got %q, want %q", got, overlayText)
	}

	// Line 1 (real content, never overlaid) is unchanged.
	line1 := writeTop + 1
	got1 := v.GetContentText(line1, 0, line1, width)
	if !strings.Contains(got1, "another line") {
		t.Errorf("line 1 unchanged: got %q, want substring %q", got1, "another line")
	}

	// Multi-line range stitches overlay + real content.
	multi := v.GetContentText(line0, 0, line1, width)
	if !strings.Contains(multi, overlayText) || !strings.Contains(multi, "another line") {
		t.Errorf("multi-line range: got %q, want both overlay and real content", multi)
	}
}

// TestRequestLineInsert_CursorPositionNotFullViewport verifies that after
// inserting synthetic lines before the cursor in a not-full viewport, the
// grid row at cursorY still shows the cursor's content. The insert must
// shift the VTerm cursor down by the number of inserts so subsequent
// writes land at the right row (this was a production regression fix —
// pre-sparse commit bfb35e4 — that the sparse cutover had dropped and we
// restored in RequestLineInsert).
func TestRequestLineInsert_CursorPositionNotFullViewport(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	width, height := 40, 24

	v := NewVTerm(width, height)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Viewport NOT full: only 6 logical rows of content in a 24-row viewport.
	parseString(p, "line 0\r\n")
	parseString(p, "line 1\r\n")
	parseString(p, "line 2\r\n")
	parseString(p, "line 3\r\n")
	parseString(p, "line 4\r\n")
	parseString(p, "prompt$ ") // no \n — cursor is on this row

	cursorYBefore := v.cursorY
	writeTopBefore := v.mainScreen.WriteTop()
	cursorGlobalBefore := writeTopBefore + int64(cursorYBefore)

	// Sanity: grid row at cursorY must contain the prompt before any insert.
	gridBefore := v.Grid()
	if !strings.Contains(cellsToString(gridBefore[cursorYBefore]), "prompt$") {
		t.Fatalf("before insert: row %d should contain prompt, got %q",
			cursorYBefore, cellsToString(gridBefore[cursorYBefore]))
	}

	// Insert 2 synthetic lines between "line 2" and "line 3" (i.e. globalIdx
	// writeTop+3). Both inserts are before the cursor row, so the cursor's
	// logical row shifts down by 2 with the inserted content.
	insertAt := writeTopBefore + 3
	for i := 0; i < 2; i++ {
		cells := makeCells(fmt.Sprintf("--- border %d ---", i))
		v.RequestLineInsert(insertAt+int64(i), cells)
	}

	cursorYAfter := v.cursorY
	writeTopAfter := v.mainScreen.WriteTop()
	cursorGlobalAfter := writeTopAfter + int64(cursorYAfter)

	// Cursor globalIdx should have shifted by 2 (the inserts were before it).
	if cursorGlobalAfter != cursorGlobalBefore+2 {
		t.Errorf("cursorGlobal: before=%d after=%d (want shift by 2)",
			cursorGlobalBefore, cursorGlobalAfter)
	}

	// The grid row at the new cursorY must still show the prompt content.
	gridAfter := v.Grid()
	rowAtCursor := cellsToString(gridAfter[cursorYAfter])
	if !strings.Contains(rowAtCursor, "prompt$") {
		t.Errorf("after insert: row %d should contain prompt, got %q", cursorYAfter, rowAtCursor)
		for y := 0; y < height; y++ {
			row := cellsToString(gridAfter[y])
			if strings.TrimRight(row, " ") != "" {
				marker := "  "
				if y == cursorYAfter {
					marker = ">>"
				}
				t.Logf("  %s [%2d] %q", marker, y, row)
			}
		}
	}
}

// TestRequestLineInsert_CursorAtBottomRow pins down the semantic of
// RequestLineInsert when the cursor is on the very last row of the viewport.
// That is the common case for an interactive shell (cursor sits at the
// prompt, viewport is full).
//
// Current behavior — which this test captures so any change becomes visible:
//
//   - InsertLines with cursorRow == marginBottom does not shift any rows
//     (the shift loop is empty) and simply clears the cursor's row.
//   - The cursor-follow guard `v.cursorY < v.height-1` is false, so the VTerm
//     cursor does NOT advance, and cursorGlobalIdx stays where it was.
//   - SetLine then writes the caller's cells at beforeIdx (i.e. the cursor's
//     row). The cursor's pre-insert content is lost.
//
// Consequence: the next keystroke writes on top of the inserted cells. This
// is a known limitation; we document it here rather than let it drift. If
// the semantic changes (e.g. by newline-scrolling the write window first),
// this test will fail and needs to be rewritten around the new contract.
func TestRequestLineInsert_CursorAtBottomRow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	width, height := 40, 24

	v := NewVTerm(width, height)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Fill the viewport so the cursor sits on the bottom row when the
	// prompt is rendered: height-1 full lines + one unterminated prompt.
	for i := 0; i < height-1; i++ {
		parseString(p, fmt.Sprintf("line %d\r\n", i))
	}
	parseString(p, "prompt$ ")

	if v.cursorY != height-1 {
		t.Fatalf("setup: expected cursorY == %d (bottom row), got %d", height-1, v.cursorY)
	}

	cursorGlobalBefore := v.cursorGlobalIdx()
	gridBefore := v.Grid()
	if !strings.Contains(cellsToString(gridBefore[height-1]), "prompt$") {
		t.Fatalf("setup: bottom row should contain prompt, got %q",
			cellsToString(gridBefore[height-1]))
	}

	// Insert at the cursor row itself. This is the code path where both the
	// content and the cursor-follow guard matter.
	insertedText := "--- inserted ---"
	v.RequestLineInsert(cursorGlobalBefore, makeCells(insertedText))

	// Cursor did not advance: guard blocked the follow.
	if got := v.cursorY; got != height-1 {
		t.Errorf("cursorY: got %d, want unchanged %d (guard should block follow at bottom row)",
			got, height-1)
	}
	if got := v.cursorGlobalIdx(); got != cursorGlobalBefore {
		t.Errorf("cursorGlobalIdx: got %d, want unchanged %d", got, cursorGlobalBefore)
	}

	// Bottom row now shows the inserted cells; the original prompt content
	// was clobbered by the insert.
	gridAfter := v.Grid()
	rowAtBottom := cellsToString(gridAfter[height-1])
	if !strings.Contains(rowAtBottom, insertedText) {
		t.Errorf("after insert: bottom row should contain %q, got %q",
			insertedText, rowAtBottom)
	}
	if strings.Contains(rowAtBottom, "prompt$") {
		t.Errorf("after insert: bottom row still contains prompt %q — expected it to be clobbered",
			rowAtBottom)
	}
}

// TestRequestLineInsert_BothBranchesFire covers a single RequestLineInsert
// call where the insert point is at or before BOTH the cursor's globalIdx
// and PromptStartGlobalLine. Both adjustments must fire on that one call:
// cursorY advances (so subsequent writes land at the right row) and
// PromptStartGlobalLine shifts (so prompt-aware ops stay pointed at the
// real prompt line).
//
// Rationale: individual branches are exercised by
// TestRequestLineInsert_CursorPositionNotFullViewport (cursor follow) and
// TestRequestLineInsert_PromptStartShiftsWithInsertsBeforeIt (prompt shift),
// but nothing pins down that both fire together on the same call.
func TestRequestLineInsert_BothBranchesFire(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := NewVTerm(40, 24)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write some output, then mark the prompt start at the current cursor.
	for i := 0; i < 3; i++ {
		parseString(p, fmt.Sprintf("output %d\r\n", i))
	}
	v.MarkPromptStart()

	cursorGlobalBefore := v.cursorGlobalIdx()
	promptBefore := v.PromptStartGlobalLine
	cursorYBefore := v.cursorY

	if promptBefore < 0 {
		t.Fatalf("MarkPromptStart did not set PromptStartGlobalLine")
	}
	if promptBefore != cursorGlobalBefore {
		t.Fatalf("setup: expected prompt at cursorGlobalIdx %d, got %d",
			cursorGlobalBefore, promptBefore)
	}

	// Insert one line at the prompt position. beforeIdx == promptStart ==
	// cursorGlobal, so both `beforeIdx <= cursorGlobal` and
	// `beforeIdx <= PromptStartGlobalLine` are true on this single call.
	v.RequestLineInsert(promptBefore, makeCells("S"))

	if got := v.cursorY; got != cursorYBefore+1 {
		t.Errorf("cursorY: got %d, want %d (cursor-follow branch should fire)",
			got, cursorYBefore+1)
	}
	if got := v.PromptStartGlobalLine; got != promptBefore+1 {
		t.Errorf("PromptStartGlobalLine: got %d, want %d (prompt-shift branch should fire)",
			got, promptBefore+1)
	}
}

// TestRequestLineInsert_PromptStartShiftsWithInsertsBeforeIt verifies the
// minimal invariant covered by TestPromptPositionAfterTransformerInsert but
// without the reload round-trip: each RequestLineInsert at or before
// PromptStartGlobalLine must increment PromptStartGlobalLine by 1.
func TestRequestLineInsert_PromptStartShiftsWithInsertsBeforeIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := NewVTerm(40, 24)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	for i := 0; i < 3; i++ {
		parseString(p, fmt.Sprintf("output %d\r\n", i))
	}

	v.MarkPromptStart()
	before := v.PromptStartGlobalLine
	if before < 0 {
		t.Fatalf("PromptStartGlobalLine not set after MarkPromptStart")
	}

	// Insert 2 synthetic lines, each at the current prompt position.
	for i := 0; i < 2; i++ {
		v.RequestLineInsert(v.PromptStartGlobalLine, makeCells("S"))
	}
	if got := v.PromptStartGlobalLine; got != before+2 {
		t.Errorf("PromptStartGlobalLine: got %d, want %d", got, before+2)
	}

	// An insert AFTER the prompt must NOT shift the prompt.
	v.RequestLineInsert(v.PromptStartGlobalLine+1, makeCells("X"))
	if got := v.PromptStartGlobalLine; got != before+2 {
		t.Errorf("PromptStartGlobalLine: insert after prompt shifted it: got %d, want %d", got, before+2)
	}
}

// TestRequestLineInsert_InputStartShiftsWithInsertsBeforeIt mirrors the prompt
// shift invariant for OSC 133;B (InputStartGlobalLine). Inserts at or before
// the input-start anchor must shift it by 1; inserts after must not.
func TestRequestLineInsert_InputStartShiftsWithInsertsBeforeIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := NewVTerm(40, 24)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	for i := 0; i < 3; i++ {
		parseString(p, fmt.Sprintf("output %d\r\n", i))
	}

	v.MarkInputStart()
	before := v.InputStartGlobalLine
	if before < 0 {
		t.Fatalf("InputStartGlobalLine not set after MarkInputStart")
	}

	for i := 0; i < 2; i++ {
		v.RequestLineInsert(v.InputStartGlobalLine, makeCells("S"))
	}
	if got := v.InputStartGlobalLine; got != before+2 {
		t.Errorf("InputStartGlobalLine: got %d, want %d", got, before+2)
	}

	v.RequestLineInsert(v.InputStartGlobalLine+1, makeCells("X"))
	if got := v.InputStartGlobalLine; got != before+2 {
		t.Errorf("InputStartGlobalLine: insert after shifted it: got %d, want %d", got, before+2)
	}
}

// TestRequestLineInsert_CommandStartShiftsWithInsertsBeforeIt mirrors the
// same invariant for OSC 133;C (CommandStartGlobalLine). Without this shift
// the ED-2 rewind anchor drifts off the current command's frame top after a
// synthetic line insert, causing subsequent repaints to rewind too far.
func TestRequestLineInsert_CommandStartShiftsWithInsertsBeforeIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := NewVTerm(40, 24)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	for i := 0; i < 3; i++ {
		parseString(p, fmt.Sprintf("output %d\r\n", i))
	}

	v.MarkCommandStart()
	before := v.CommandStartGlobalLine
	if before < 0 {
		t.Fatalf("CommandStartGlobalLine not set after MarkCommandStart")
	}

	for i := 0; i < 2; i++ {
		v.RequestLineInsert(v.CommandStartGlobalLine, makeCells("S"))
	}
	if got := v.CommandStartGlobalLine; got != before+2 {
		t.Errorf("CommandStartGlobalLine: got %d, want %d", got, before+2)
	}

	v.RequestLineInsert(v.CommandStartGlobalLine+1, makeCells("X"))
	if got := v.CommandStartGlobalLine; got != before+2 {
		t.Errorf("CommandStartGlobalLine: insert after shifted it: got %d, want %d", got, before+2)
	}
}

// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/prompt_overwrite_recovery_test.go
// Summary: Regression for issue #188. After closing a VTerm with a stored
// OSC 133;A prompt and reopening, RepositionForPromptOverwrite must place
// the cursor at column 0 of the prompt's global line so the freshly-spawned
// shell's first PS1 prompt overwrites the stored prompt 1:1 instead of
// rendering below it.

package parser

import (
	"fmt"
	"testing"
)

// TestRepositionForPromptOverwrite_ViewportHoldsPrompt covers the common
// case where the whole history still fits in the viewport after reload
// (writeTop stays at 0). The cursor must move onto the prompt row — NOT
// rewinding writeTop, which would invalidate the rows above the prompt.
func TestRepositionForPromptOverwrite_ViewportHoldsPrompt(t *testing.T) {
	dir := t.TempDir()
	id := "prompt-overwrite-viewport"
	const cols, rows = 40, 10

	v1 := newTestVTerm(t, cols, rows, dir, id)
	p1 := NewParser(v1)

	parseString(p1, "banner\r\n")
	v1.MarkPromptStart()
	parseString(p1, "$ ")
	v1.MarkInputStart()
	parseString(p1, "echo hi\r\n")
	v1.MarkCommandStart()
	parseString(p1, "hi\r\n")

	storedPrompt := v1.PromptStartGlobalLine
	if storedPrompt < 0 {
		t.Fatalf("setup: MarkPromptStart did not set PromptStartGlobalLine")
	}

	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	v2 := newTestVTerm(t, cols, rows, dir, id)
	defer v2.CloseMemoryBuffer()

	if v2.LastPromptLine() != storedPrompt {
		t.Fatalf("LastPromptLine after reload: got %d, want %d", v2.LastPromptLine(), storedPrompt)
	}
	if got := v2.LastPromptHeight(); got != 1 {
		t.Errorf("LastPromptHeight single-line: got %d, want 1", got)
	}

	topBefore := v2.mainScreen.WriteTop()
	if topBefore > storedPrompt {
		t.Fatalf("setup: expected writeTop (%d) <= storedPrompt (%d) for in-viewport case", topBefore, storedPrompt)
	}

	v2.RepositionForPromptOverwrite(storedPrompt)

	if got := v2.mainScreen.WriteTop(); got != topBefore {
		t.Errorf("writeTop should not move when prompt is already in viewport: got %d, want %d", got, topBefore)
	}
	wantRow := int(storedPrompt - topBefore)
	if v2.cursorY != wantRow || v2.cursorX != 0 {
		t.Errorf("cursor: got (%d, %d), want (%d, 0)", v2.cursorY, v2.cursorX, wantRow)
	}
}

// TestRepositionForPromptOverwrite_PromptAboveWriteTop covers the
// scrollback-overflow case where the stored prompt is older than the
// current viewport. writeTop must rewind to the prompt line and the
// cursor must land at row 0.
func TestRepositionForPromptOverwrite_PromptAboveWriteTop(t *testing.T) {
	dir := t.TempDir()
	id := "prompt-overwrite-scrolled"
	const cols, rows = 40, 5

	v1 := newTestVTerm(t, cols, rows, dir, id)
	p1 := NewParser(v1)

	v1.MarkPromptStart()
	parseString(p1, "$ ")
	v1.MarkInputStart()
	parseString(p1, "cmd\r\n")
	v1.MarkCommandStart()

	// Push past the viewport so writeTop advances past the stored prompt.
	for i := range rows * 3 {
		parseString(p1, fmt.Sprintf("output line %d\r\n", i))
	}

	storedPrompt := v1.PromptStartGlobalLine
	if storedPrompt < 0 {
		t.Fatalf("setup: PromptStartGlobalLine not set")
	}
	if v1.mainScreen.WriteTop() <= storedPrompt {
		t.Fatalf("setup: writeTop (%d) did not advance past storedPrompt (%d)",
			v1.mainScreen.WriteTop(), storedPrompt)
	}

	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	v2 := newTestVTerm(t, cols, rows, dir, id)
	defer v2.CloseMemoryBuffer()

	if v2.mainScreen.WriteTop() <= storedPrompt {
		t.Fatalf("setup: reloaded writeTop (%d) should be past storedPrompt (%d)",
			v2.mainScreen.WriteTop(), storedPrompt)
	}

	v2.RepositionForPromptOverwrite(storedPrompt)

	if got := v2.mainScreen.WriteTop(); got != storedPrompt {
		t.Errorf("writeTop should rewind to prompt: got %d, want %d", got, storedPrompt)
	}
	if v2.cursorY != 0 || v2.cursorX != 0 {
		t.Errorf("cursor after rewind: got (%d, %d), want (0, 0)", v2.cursorY, v2.cursorX)
	}
}

// TestRepositionForPromptOverwrite_MultiLinePromptHeight verifies
// LastPromptHeight reflects the span between PromptStart and InputStart.
// Repositioning always targets PromptStart (the top of the prompt); bash
// will redraw every subsequent row of its own multi-line prompt.
func TestRepositionForPromptOverwrite_MultiLinePromptHeight(t *testing.T) {
	dir := t.TempDir()
	id := "prompt-overwrite-multi"
	const cols, rows = 40, 10

	v1 := newTestVTerm(t, cols, rows, dir, id)
	p1 := NewParser(v1)

	parseString(p1, "banner\r\n")
	v1.MarkPromptStart()
	parseString(p1, "line1 of prompt\r\n")
	parseString(p1, "line2 of prompt $ ")
	v1.MarkInputStart()
	parseString(p1, "cmd\r\n")
	v1.MarkCommandStart()

	promptStart := v1.PromptStartGlobalLine
	inputStart := v1.InputStartGlobalLine
	if promptStart < 0 || inputStart < promptStart {
		t.Fatalf("setup: anchors invalid, prompt=%d input=%d", promptStart, inputStart)
	}
	wantHeight := int(inputStart-promptStart) + 1

	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	v2 := newTestVTerm(t, cols, rows, dir, id)
	defer v2.CloseMemoryBuffer()

	if got := v2.LastPromptHeight(); got != wantHeight {
		t.Errorf("LastPromptHeight multi-line: got %d, want %d", got, wantHeight)
	}

	topBefore := v2.mainScreen.WriteTop()
	v2.RepositionForPromptOverwrite(v2.LastPromptLine())

	wantRow := max(int(promptStart-topBefore), 0)
	if v2.cursorY != wantRow || v2.cursorX != 0 {
		t.Errorf("cursor: got (%d, %d), want (%d, 0)", v2.cursorY, v2.cursorX, wantRow)
	}
}

// TestRepositionForPromptOverwrite_UnknownPromptNoOp verifies the
// repositioning is a no-op when the prompt line is unknown. Without this,
// a stray call from a shell that doesn't emit OSC 133;A would wipe state.
func TestRepositionForPromptOverwrite_UnknownPromptNoOp(t *testing.T) {
	dir := t.TempDir()
	id := "prompt-overwrite-noop"
	const cols, rows = 40, 10

	v := newTestVTerm(t, cols, rows, dir, id)
	defer v.CloseMemoryBuffer()
	p := NewParser(v)

	parseString(p, "line1\r\nline2\r\nline3\r\n")
	topBefore := v.mainScreen.WriteTop()
	cx, cy := v.cursorX, v.cursorY

	v.RepositionForPromptOverwrite(-1)

	if got := v.mainScreen.WriteTop(); got != topBefore {
		t.Errorf("writeTop after no-op: got %d, want %d", got, topBefore)
	}
	if v.cursorX != cx || v.cursorY != cy {
		t.Errorf("cursor after no-op: got (%d, %d), want (%d, %d)", v.cursorY, v.cursorX, cy, cx)
	}
}

// TestRepositionForPromptOverwrite_PromptPastViewportBottom is a defensive
// no-op when the stored prompt somehow refers to a row below the current
// viewport — a malformed state that callers shouldn't synthesize but that
// the helper must survive without corrupting cursor coordinates.
func TestRepositionForPromptOverwrite_PromptPastViewportBottom(t *testing.T) {
	dir := t.TempDir()
	id := "prompt-overwrite-past-bottom"
	const cols, rows = 40, 5

	v := newTestVTerm(t, cols, rows, dir, id)
	defer v.CloseMemoryBuffer()
	p := NewParser(v)

	parseString(p, "a\r\n")
	topBefore := v.mainScreen.WriteTop()

	// Target a line well past writeTop + height-1.
	v.RepositionForPromptOverwrite(topBefore + int64(rows) + 10)

	if got := v.mainScreen.WriteTop(); got != topBefore {
		t.Errorf("writeTop must not move on past-bottom target: got %d, want %d", got, topBefore)
	}
}

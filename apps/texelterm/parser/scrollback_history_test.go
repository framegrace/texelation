// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/scrollback_history_test.go
// Summary: Sparse-native ports of the scrollback, history, and metadata
// tests from vterm_memory_buffer_test.go. Covers user scrollback navigation,
// total-line counting, terminal-color reset on reload, prompt-position
// persistence, and synthetic line insertion before the prompt.
//
// One pre-sparse test has been dropped rather than ported:
// `TestLoadHistory_TrimsBlankTailLines` asserted that a persisted metadata
// WriteTop past the last non-empty line is clamped down. In the sparse
// model, WriteTop is restored verbatim from metadata (validated only
// against PageStore.LineCount via vterm_main_screen.go); the blank-tail
// trim heuristic was a pre-sparse quality-of-life hack that the sparse
// design explicitly does not replicate.

package parser

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestVTerm_UserScroll verifies that a user-initiated scroll up leaves
// auto-follow mode and ScrollToBottom returns to it, exercising the
// ViewWindow autoFollow flag.
func TestVTerm_UserScroll(t *testing.T) {
	v := NewVTerm(80, 10, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Write enough lines to build history.
	for i := 0; i < 20; i++ {
		p.Parse('A' + rune(i%26))
		p.Parse('\n')
		p.Parse('\r')
	}

	if !v.mainScreen.IsFollowing() {
		t.Error("should be auto-following after writing")
	}

	v.Scroll(-5)
	if v.mainScreen.IsFollowing() {
		t.Error("should not be auto-following after scrolling up")
	}

	v.mainScreen.ScrollToBottom()
	if !v.mainScreen.IsFollowing() {
		t.Error("should be auto-following after ScrollToBottom")
	}
}

// TestVTerm_TotalLines verifies that ContentEnd advances as lines are
// appended, matching the pre-sparse memoryBufferTotalLines semantics.
func TestVTerm_TotalLines(t *testing.T) {
	v := NewVTerm(80, 24, WithMemoryBuffer())
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// ContentEnd starts at -1 (empty) or 0 (cursor row occupied).
	initial := v.ContentEnd()
	if initial > 0 {
		t.Errorf("initial ContentEnd should be ≤0, got %d", initial)
	}

	// Write 50 lines.
	for i := 0; i < 50; i++ {
		p.Parse('X')
		p.Parse('\n')
		p.Parse('\r')
	}

	end := v.ContentEnd()
	// ContentEnd + 1 is the exclusive total line count. After 50 LFs we
	// expect at least 25 visible-or-scrollback lines in the sparse store.
	if end < 25 {
		t.Errorf("ContentEnd too small after 50 writes: got %d, want ≥25", end)
	}
	if v.TotalPhysicalLines() < 25 {
		t.Errorf("TotalPhysicalLines too small: got %d", v.TotalPhysicalLines())
	}
}

// TestLoadHistory_ResetsTerminalColors verifies that after reload, the VTerm
// current drawing colors are reset to DefaultFG/DefaultBG rather than the
// zero-value Color{} (which would render as black and make new output
// invisible on a dark theme).
func TestLoadHistory_ResetsTerminalColors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	diskPath := filepath.Join(tmpDir, "test_color_reset.hist3")
	terminalID := "test-color-reset"

	{
		v := NewVTerm(80, 24, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk: %v", err)
		}
		p := NewParser(v)
		parseString(p, "Hello from session 1\r\n")
		v.CloseMemoryBuffer()
	}

	{
		v := NewVTerm(80, 24, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk (reload): %v", err)
		}
		defer v.CloseMemoryBuffer()

		if v.currentFG != DefaultFG {
			t.Errorf("currentFG: got %v, want DefaultFG (%v)", v.currentFG, DefaultFG)
		}
		if v.currentBG != DefaultBG {
			t.Errorf("currentBG: got %v, want DefaultBG (%v)", v.currentBG, DefaultBG)
		}
		if v.currentAttr != 0 {
			t.Errorf("currentAttr: got %v, want 0", v.currentAttr)
		}
	}
}

// TestPromptPositionOnReload verifies that PromptStartGlobalLine and the
// working directory persist across a close/reopen cycle, and that the cursor
// is restored to its saved (col, row) position.
//
// Pre-sparse difference: the pre-sparse implementation re-positioned the
// cursor at (0, promptRow) on reload and erased from the prompt row down
// so a new shell prompt would overwrite the old one in place. The sparse
// cutover (PR #179) dropped that behavior — cursor is restored exactly to
// where it was saved. A new shell prompt will appear after the old one
// rather than over it. If we want the pre-sparse behavior back, we need a
// design decision and explicit erase-from-prompt-row logic in
// EnableMemoryBufferWithDisk.
func TestPromptPositionOnReload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	diskPath := filepath.Join(tmpDir, "test_prompt_pos.hist3")
	terminalID := "test-prompt-pos"
	width, height := 80, 24

	var savedPromptLine int64

	{
		v := NewVTerm(width, height, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk: %v", err)
		}
		p := NewParser(v)

		for i := 0; i < 10; i++ {
			parseString(p, fmt.Sprintf("output line %d", i))
			p.Parse('\n')
			p.Parse('\r')
		}

		v.MarkPromptStart()
		savedPromptLine = v.PromptStartGlobalLine
		if savedPromptLine < 0 {
			t.Fatalf("PromptStartGlobalLine not set after MarkPromptStart: %d", savedPromptLine)
		}

		parseString(p, "$ ")
		v.setWorkingDirectory("file://localhost/tmp/test-session")

		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("CloseMemoryBuffer: %v", err)
		}
	}

	{
		v := NewVTerm(width, height, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk (reload): %v", err)
		}
		defer v.CloseMemoryBuffer()

		// 10 output lines + prompt at global 10, all within viewport.
		// writeTop=0 so prompt row = 10. Sparse restores cursor to its
		// saved position; we wrote "$ " (2 cols) before close, so cursorX=2.
		if v.cursorY != 10 || v.cursorX != 2 {
			t.Errorf("cursor after reload: got (%d, %d), want (2, 10)", v.cursorX, v.cursorY)
		}
		if v.PromptStartGlobalLine != savedPromptLine {
			t.Errorf("PromptStartGlobalLine: got %d, want %d", v.PromptStartGlobalLine, savedPromptLine)
		}
		if v.CurrentWorkingDir != "/tmp/test-session" {
			t.Errorf("CurrentWorkingDir: got %q, want %q", v.CurrentWorkingDir, "/tmp/test-session")
		}
	}
}

// TestPromptPositionAfterTransformerInsert verifies that PromptStartGlobalLine
// is shifted forward when RequestLineInsert inserts synthetic lines before
// the recorded prompt position. Without this, a reload would position the
// cursor at the stale pre-insert prompt line, erasing lines that should be
// preserved (e.g. the first line of a multi-line prompt).
func TestPromptPositionAfterTransformerInsert(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	diskPath := filepath.Join(tmpDir, "test_transformer_prompt.hist3")
	terminalID := "test-transformer-prompt"
	width, height := 80, 24

	{
		v := NewVTerm(width, height, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk: %v", err)
		}
		p := NewParser(v)

		for i := 0; i < 5; i++ {
			parseString(p, fmt.Sprintf("output line %d", i))
			p.Parse('\n')
			p.Parse('\r')
		}

		v.MarkPromptStart()
		promptBefore := v.PromptStartGlobalLine
		if promptBefore < 0 {
			t.Fatalf("PromptStartGlobalLine not set")
		}

		// Transformer inserts 3 synthetic lines before the prompt, simulating
		// a table transformer flushing its rendered output via OnLineCommit.
		for i := 0; i < 3; i++ {
			cells := []Cell{{Rune: 'T'}}
			v.RequestLineInsert(promptBefore+int64(i), cells)
		}

		promptAfter := v.PromptStartGlobalLine
		if promptAfter != promptBefore+3 {
			t.Errorf("PromptStartGlobalLine not adjusted: before=%d after=%d want %d",
				promptBefore, promptAfter, promptBefore+3)
		}

		// Write a 2-line prompt at the (new) prompt position.
		parseString(p, "prompt-line-1")
		p.Parse('\n')
		p.Parse('\r')
		parseString(p, "prompt-line-2 $ ")

		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("CloseMemoryBuffer: %v", err)
		}
	}

	{
		v := NewVTerm(width, height, WithMemoryBuffer())
		err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		})
		if err != nil {
			t.Fatalf("EnableMemoryBufferWithDisk (reload): %v", err)
		}
		defer v.CloseMemoryBuffer()

		// 5 output lines + 3 synthetic 'T' + prompt-line-1 + prompt-line-2 → 10 rows.
		// After CR/LF at end of prompt-line-1, cursor is at row 9; we wrote
		// "prompt-line-2 $ " there, so cursor is at col 16 row 9 on close.
		// Sparse restores cursor to its exact saved position.
		//
		// Pre-sparse difference: pre-sparse re-positioned the cursor at
		// (0, promptRow=8) on reload and erased from row 8 down, so the old
		// prompt would be overwritten by the new shell's prompt. That logic
		// was dropped by the sparse cutover (PR #179).
		expectedCursorRow := 9
		if v.cursorY != expectedCursorRow {
			t.Errorf("cursor row: got %d, want %d", v.cursorY, expectedCursorRow)
		}

		// Every line before the first prompt row (5 output + 3 synthetic)
		// should still have content in the sparse store — the important
		// property is that the insert-adjustment preserved the lines that
		// used to be at the stale pre-insert prompt position (row 5).
		firstPromptRow := 8
		writeTop := v.mainScreen.WriteTop()
		for i := int64(0); i < int64(firstPromptRow); i++ {
			gi := writeTop + i
			cells := v.mainScreen.ReadLine(gi)
			if cells == nil {
				t.Errorf("line %d (global %d) is nil, expected content", i, gi)
				continue
			}
			if !lineHasSparseContent(cells) {
				t.Errorf("line %d (global %d) empty, expected output or synthetic", i, gi)
			}
		}
	}
}

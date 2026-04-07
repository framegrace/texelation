// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Investigation: empirically measure where phantom blank lines come from
// during normal terminal output. Feed known content, count resulting
// pageStore line counts, identify which lines are blank and why.

package parser

import (
	"fmt"
	"strings"
	"testing"
)

// feedAndCount runs a sequence of bytes through a VTerm with persistence
// enabled, then closes, reopens, and reports the resulting pageStore stats.
func feedAndCount(t *testing.T, name string, height int, payload string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	terminalID := "phantom-" + name

	v := NewVTerm(80, height, WithMemoryBuffer())
	if err := v.EnableMemoryBufferWithDisk(dir, MemoryBufferOptions{
		MaxLines:   50000,
		TerminalID: terminalID,
	}); err != nil {
		t.Fatalf("EnableMemoryBufferWithDisk: %v", err)
	}

	// Count input newlines for comparison.
	inputLines := strings.Count(payload, "\n")

	p := NewParser(v)
	for _, r := range payload {
		p.Parse(r)
	}

	// Close to flush everything.
	if err := v.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	// Reopen page store directly to inspect what got persisted.
	cfg := DefaultPageStoreConfig(dir, terminalID)
	ps, err := OpenPageStore(cfg)
	if err != nil {
		t.Fatalf("OpenPageStore: %v", err)
	}
	if ps == nil {
		t.Fatalf("OpenPageStore returned nil")
	}
	defer ps.Close()

	stored := ps.LineCount()

	// Count blank vs content lines.
	blank := int64(0)
	content := int64(0)
	for i := int64(0); i < stored; i++ {
		line, _ := ps.ReadLine(i)
		if line == nil {
			continue
		}
		hasContent := false
		for _, c := range line.Cells {
			if c.Rune != ' ' && c.Rune != 0 {
				hasContent = true
				break
			}
		}
		if hasContent {
			content++
		} else {
			blank++
		}
	}

	t.Logf("[%s] input newlines=%d, stored=%d (content=%d, blank=%d), inflation=%d",
		name, inputLines, stored, content, blank, stored-int64(inputLines))
}

// TestPhantomGaps_OpenWriteOneClose verifies the close-path overhead in
// isolation: a single content line + close, no escape codes.
func TestPhantomGaps_OpenWriteOneClose(t *testing.T) {
	feedAndCount(t, "open_one_close", 24, "hello\r\n")
}

// TestPhantomGaps_DenseLines feeds N lines each with content, no escape codes.
// Expectation: stored count == input lines, zero blanks.
func TestPhantomGaps_DenseLines(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "line %d content\r\n", i)
	}
	feedAndCount(t, "dense100", 24, b.String())
}

// TestPhantomGaps_DenseLargeLines like above but enough to trigger eviction.
func TestPhantomGaps_DenseLargeLines(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 60000; i++ {
		fmt.Fprintf(&b, "line %d content\r\n", i)
	}
	feedAndCount(t, "dense60k", 24, b.String())
}

// TestPhantomGaps_LineFeedWithoutWrites issues only newlines, no characters.
// Expectation: depends on whether LF-without-write creates phantom writes.
func TestPhantomGaps_LineFeedWithoutWrites(t *testing.T) {
	feedAndCount(t, "lf_only_100", 24, strings.Repeat("\r\n", 100))
}

// TestPhantomGaps_CursorJumpThenWrite uses CSI cursor positioning to jump
// far down without writing intermediate rows, then writes a single char.
func TestPhantomGaps_CursorJumpThenWrite(t *testing.T) {
	// Write something at row 1, then jump to row 20 and write again.
	payload := "first\r\n" + "\x1b[20;1H" + "twentieth"
	feedAndCount(t, "cursor_jump", 24, payload)
}

// TestPhantomGaps_ClearScreen sends ESC[2J ESC[H (clear screen + home).
func TestPhantomGaps_ClearScreen(t *testing.T) {
	payload := "first\r\n" + "\x1b[2J\x1b[H" + "after clear"
	feedAndCount(t, "clear_screen", 24, payload)
}

// TestPhantomGaps_MultiSession_Reload reproduces a multi-session scenario:
// session 1 writes some lines, closes; session 2 reopens, writes more, closes;
// then we report the cumulative line count and inflation.
func TestPhantomGaps_MultiSession_Reload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	terminalID := "phantom-multisession"

	feed := func(payload string) {
		v := NewVTerm(80, 24, WithMemoryBuffer())
		if err := v.EnableMemoryBufferWithDisk(dir, MemoryBufferOptions{
			MaxLines:   50000,
			TerminalID: terminalID,
		}); err != nil {
			t.Fatalf("Enable: %v", err)
		}
		p := NewParser(v)
		for _, r := range payload {
			p.Parse(r)
		}
		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	// Session 1: dense content, 50 lines.
	var s1 strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&s1, "session1 line %d\r\n", i)
	}
	feed(s1.String())

	// Session 2: dense content, 50 lines.
	var s2 strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&s2, "session2 line %d\r\n", i)
	}
	feed(s2.String())

	// Session 3: dense content, 50 lines.
	var s3 strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&s3, "session3 line %d\r\n", i)
	}
	feed(s3.String())

	cfg := DefaultPageStoreConfig(dir, terminalID)
	ps, _ := OpenPageStore(cfg)
	defer ps.Close()

	stored := ps.LineCount()
	blank := int64(0)
	content := int64(0)
	for i := int64(0); i < stored; i++ {
		line, _ := ps.ReadLine(i)
		if line == nil {
			continue
		}
		has := false
		for _, c := range line.Cells {
			if c.Rune != ' ' && c.Rune != 0 {
				has = true
				break
			}
		}
		if has {
			content++
		} else {
			blank++
		}
	}
	t.Logf("[multisession] 3x50=150 input lines, stored=%d (content=%d, blank=%d)",
		stored, content, blank)
}

// TestPhantomGaps_ScrollRegion exercises a custom scroll region (TUI-like).
func TestPhantomGaps_ScrollRegion(t *testing.T) {
	// Set scroll region rows 5..15, then write some lines and LFs.
	var b strings.Builder
	b.WriteString("\x1b[5;15r") // DECSTBM scroll region 5..15
	b.WriteString("\x1b[5;1H")   // cursor to row 5
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "scroll %d\r\n", i)
	}
	b.WriteString("\x1b[r") // reset scroll region
	feedAndCount(t, "scroll_region", 24, b.String())
}

// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/ed2_rewind_test.go
// Summary: Regression tests for ED 2 (ESC[2J) rewind-to-anchor behaviour in
// non-alt-screen mode. Covers the three-tier anchor fallback chain
// (CommandStart > InputStart+1 > PromptStart+1), the alt-screen guard, and
// that stale rows past the anchor are cleared so they don't linger in
// scrollback after rewind. See project_sparse_ed2_anchor.md.

package parser

import (
	"fmt"
	"strings"
	"testing"
)

// writeFullFrameOverflow writes nRows lines of "frame-<i> ..." content through
// the parser. When nRows > viewport height, the write window overflows and
// writeTop advances — the exact condition that makes ED 2 interesting.
func writeFullFrameOverflow(p *Parser, nRows int) {
	for i := 0; i < nRows; i++ {
		parseString(p, fmt.Sprintf("frame-%02d filler %s\r\n", i, strings.Repeat("x", 20)))
	}
}

// TestED2_RewindUsesCommandStartWhenActive verifies that when a command is
// running (OSC 133;C set, no 133;D yet), ED 2 rewinds writeTop to
// CommandStartGlobalLine — even when PromptStart / InputStart are also set
// and point somewhere higher.
func TestED2_RewindUsesCommandStartWhenActive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := NewVTerm(40, 10)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Simulate a bash prompt + input + command start, then a TUI frame.
	parseString(p, "shell banner\r\n")
	v.MarkPromptStart()
	parseString(p, "prompt> ")
	v.MarkInputStart()
	parseString(p, "claude\r\n")
	v.MarkCommandStart()
	cmdAnchor := v.CommandStartGlobalLine
	if cmdAnchor < 0 {
		t.Fatalf("MarkCommandStart did not set CommandStartGlobalLine")
	}

	// Overflow the viewport so writeTop advances past the anchor.
	writeFullFrameOverflow(p, 30)
	if got := v.mainScreen.WriteTop(); got <= cmdAnchor {
		t.Fatalf("setup: writeTop=%d did not advance past CommandStart=%d", got, cmdAnchor)
	}

	// ED 2 (clear entire display).
	parseString(p, "\x1b[2J")

	if got := v.mainScreen.WriteTop(); got != cmdAnchor {
		t.Errorf("writeTop: got %d, want %d (CommandStart anchor)", got, cmdAnchor)
	}
}

// TestED2_FallsBackToInputStartWhenNoCommand verifies that when no 133;C
// anchor is set (idle shell, no foreground TUI), ED 2 falls back to
// InputStart+1.
func TestED2_FallsBackToInputStartWhenNoCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := NewVTerm(40, 10)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	parseString(p, "banner\r\n")
	v.MarkPromptStart()
	parseString(p, "prompt> ")
	v.MarkInputStart()
	inputAnchor := v.InputStartGlobalLine
	if inputAnchor < 0 {
		t.Fatalf("MarkInputStart did not set InputStartGlobalLine")
	}
	if v.CommandStartGlobalLine >= 0 {
		t.Fatalf("CommandStartGlobalLine should be -1 (no 133;C), got %d", v.CommandStartGlobalLine)
	}

	writeFullFrameOverflow(p, 30)
	if got := v.mainScreen.WriteTop(); got <= inputAnchor+1 {
		t.Fatalf("setup: writeTop=%d did not advance past InputStart+1=%d", got, inputAnchor+1)
	}

	parseString(p, "\x1b[2J")

	want := inputAnchor + 1
	if got := v.mainScreen.WriteTop(); got != want {
		t.Errorf("writeTop: got %d, want %d (InputStart+1 anchor)", got, want)
	}
}

// TestED2_FallsBackToPromptStartWhenNoInput verifies that for shells that
// emit only OSC 133;A, ED 2 rewinds to PromptStart+1.
func TestED2_FallsBackToPromptStartWhenNoInput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := NewVTerm(40, 10)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	parseString(p, "banner\r\n")
	v.MarkPromptStart()
	promptAnchor := v.PromptStartGlobalLine
	if promptAnchor < 0 {
		t.Fatalf("MarkPromptStart did not set PromptStartGlobalLine")
	}
	if v.InputStartGlobalLine >= 0 {
		t.Fatalf("InputStartGlobalLine should be -1, got %d", v.InputStartGlobalLine)
	}
	if v.CommandStartGlobalLine >= 0 {
		t.Fatalf("CommandStartGlobalLine should be -1, got %d", v.CommandStartGlobalLine)
	}

	writeFullFrameOverflow(p, 30)
	if got := v.mainScreen.WriteTop(); got <= promptAnchor+1 {
		t.Fatalf("setup: writeTop=%d did not advance past PromptStart+1=%d", got, promptAnchor+1)
	}

	parseString(p, "\x1b[2J")

	want := promptAnchor + 1
	if got := v.mainScreen.WriteTop(); got != want {
		t.Errorf("writeTop: got %d, want %d (PromptStart+1 anchor)", got, want)
	}
}

// TestED2_NoRewindInAltScreen guards against regressing alt-screen TUIs
// (vim, less, htop) where ED 2 is the normal clear-screen primitive and
// rewinding would corrupt scrollback.
func TestED2_NoRewindInAltScreen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := NewVTerm(40, 10)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Set all three anchors to prove rewind *would* fire in main screen.
	parseString(p, "banner\r\n")
	v.MarkPromptStart()
	parseString(p, "prompt> ")
	v.MarkInputStart()
	parseString(p, "vim\r\n")
	v.MarkCommandStart()

	writeFullFrameOverflow(p, 20)
	topBeforeAlt := v.mainScreen.WriteTop()

	// Enter alt screen (DECSET 1049).
	parseString(p, "\x1b[?1049h")
	if !v.inAltScreen {
		t.Fatalf("DECSET 1049 did not enter alt screen")
	}

	// Now the TUI issues ED 2. Must NOT rewind the main-screen writeTop.
	parseString(p, "\x1b[2J")

	if got := v.mainScreen.WriteTop(); got != topBeforeAlt {
		t.Errorf("writeTop changed in alt screen: got %d, want %d", got, topBeforeAlt)
	}
}

// TestED2_ClearsStaleRowsAboveAnchor pins the second half of the rewind:
// after moving writeTop back, the [anchor, HWM] range must be cleared so
// that scrolling up through the rewound area does not surface stale content
// from the previous overflow cycle.
func TestED2_ClearsStaleRowsAboveAnchor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := NewVTerm(40, 10)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	parseString(p, "banner\r\n")
	v.MarkPromptStart()
	parseString(p, "prompt> ")
	v.MarkInputStart()
	parseString(p, "claude\r\n")
	v.MarkCommandStart()
	anchor := v.CommandStartGlobalLine

	// Overflow with distinctive content we can hunt for after the rewind.
	for i := 0; i < 30; i++ {
		parseString(p, fmt.Sprintf("STALE-%03d filler\r\n", i))
	}
	hwm := v.mainScreen.WriteBottomHWM()
	if hwm <= anchor {
		t.Fatalf("setup: HWM=%d should be above anchor=%d", hwm, anchor)
	}

	parseString(p, "\x1b[2J")

	// Every globalIdx in [anchor, HWM] must be blank — if any STALE-xxx
	// row survives above the new writeTop, ClearRange regressed.
	for gi := anchor; gi <= hwm; gi++ {
		cells := v.mainScreen.ReadLine(gi)
		text := trimRight(cellsToString(cells))
		if strings.Contains(text, "STALE-") {
			t.Errorf("stale row survived at globalIdx %d: %q", gi, text)
		}
	}
}

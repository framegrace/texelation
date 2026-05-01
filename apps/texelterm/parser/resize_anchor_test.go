// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/resize_anchor_test.go
// Summary: Regression tests for writeTop snapping to the prompt anchor on
// resize. Without this, two failure modes appear in non-alt-screen TUIs
// like Claude Code:
//
//  1. Shrink advances writeTop past the prompt; a TUI that doesn't emit
//     ED 2 on SIGWINCH paints below the prompt and leaves the previous
//     frame in scrollback (visible duplicates).
//  2. Expand uses HWM-anchor formula and pulls writeTop into pre-window
//     scrollback; the post-resize redraw overwrites committed history
//     above the prompt (visible "eaten history").
//
// Both are fixed by snapping writeTop to the latest prompt anchor in
// VTerm.mainScreenResize. See vterm_main_screen.go.

package parser

import "testing"

// TestResize_Shrink_RewindsToCommandStartAnchor — when the user shrinks
// the pane while a command is running, writeTop is pulled back to the
// CommandStart anchor so a SIGWINCH redraw lands at the prompt.
func TestResize_Shrink_RewindsToCommandStartAnchor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := NewVTerm(40, 24)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	parseString(p, "banner\r\n")
	v.MarkPromptStart()
	parseString(p, "$ ")
	v.MarkInputStart()
	parseString(p, "claude\r\n")
	v.MarkCommandStart()
	cmdAnchor := v.CommandStartGlobalLine
	if cmdAnchor < 0 {
		t.Fatalf("MarkCommandStart did not set CommandStartGlobalLine")
	}

	// Fill the viewport with TUI content so writeTop has advanced past
	// the anchor when we shrink (cursor must move with content).
	writeFullFrameOverflow(p, 30)
	if got := v.mainScreen.WriteTop(); got <= cmdAnchor {
		t.Fatalf("setup: writeTop=%d did not advance past CommandStart=%d", got, cmdAnchor)
	}

	// Shrink the viewport. Without the anchor snap, writeTop would
	// advance further (to keep cursor visible at the new bottom).
	v.Resize(40, 8)

	if got := v.mainScreen.WriteTop(); got != cmdAnchor {
		t.Errorf("after shrink writeTop = %d, want %d (CommandStart anchor)", got, cmdAnchor)
	}
}

// TestResize_Expand_AdvancesPastPreWindowScrollback — when the user
// enlarges the pane, the HWM-anchor formula in WriteWindow.Resize can
// pull writeTop *below* the prompt anchor (into pre-window scrollback).
// The redraw would then overwrite that scrollback. The anchor snap in
// mainScreenResize must push writeTop forward to the anchor.
func TestResize_Expand_AdvancesPastPreWindowScrollback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Start at a small height so HWM stays small.
	v := NewVTerm(40, 8)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Build up scrollback ABOVE the prompt.
	for i := 0; i < 5; i++ {
		parseString(p, "old scrollback line\r\n")
	}
	v.MarkPromptStart()
	parseString(p, "$ ")
	v.MarkInputStart()
	parseString(p, "claude\r\n")
	v.MarkCommandStart()
	cmdAnchor := v.CommandStartGlobalLine

	// A few rows of TUI output, but stay within the small window so
	// HWM stays small. After this, HWM ≈ writeTop + 8 - 1, and the
	// HWM-anchor formula on a later expand will land writeTop at
	// HWM - 24 + 1 — far below cmdAnchor.
	parseString(p, "TUI line 1\r\nTUI line 2\r\n")

	// Enlarge to a height bigger than the cumulative content so far.
	// HWM-anchor would put writeTop = HWM - 24 + 1, deep in scrollback.
	v.Resize(40, 24)

	if got := v.mainScreen.WriteTop(); got < cmdAnchor {
		t.Errorf("after expand writeTop = %d, must be >= %d (CommandStart anchor) — expand pulled writeTop into pre-anchor scrollback",
			got, cmdAnchor)
	}
}

// TestResize_AltScreen_DoesNotSnapAnchor — alt-screen TUIs (vim, less,
// htop) live entirely in the alt screen and must not have their main
// screen disturbed by resize. The anchor snap is gated on !inAltScreen.
func TestResize_AltScreen_DoesNotSnapAnchor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := NewVTerm(40, 24)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	v.MarkPromptStart()
	parseString(p, "$ ")
	v.MarkInputStart()
	parseString(p, "vim\r\n")
	v.MarkCommandStart()

	// Switch to alt screen.
	parseString(p, "\x1b[?1049h")
	if !v.inAltScreen {
		t.Fatalf("setup: failed to enter alt screen")
	}

	// Snapshot main-screen writeTop, then resize.
	beforeTop := v.mainScreen.WriteTop()
	v.Resize(40, 10)

	if got := v.mainScreen.WriteTop(); got != beforeTop {
		t.Errorf("alt-screen resize moved main-screen writeTop from %d to %d (must not snap to anchor)",
			beforeTop, got)
	}
}

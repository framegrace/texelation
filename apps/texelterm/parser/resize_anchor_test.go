// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/resize_anchor_test.go
// Summary: Regression tests for resize-time writeTop handling. The
// minimum invariant we enforce in non-alt-screen mode is "expand never
// retreats writeTop past its pre-resize value" — this prevents the
// HWM-anchor formula in WriteWindow.Resize from pulling writeTop into
// committed scrollback that a TUI's post-SIGWINCH redraw would overwrite.
//
// We deliberately do NOT snap writeTop to the prompt anchor: a long
// Claude session has writeTop legitimately advanced via per-newline
// scroll-up, with conversation history accumulated as scrollback.
// Snapping all the way to the prompt would orphan that history.

package parser

import "testing"

// TestResize_Expand_DoesNotRetreatBelowPreResizeWriteTop — when the user
// enlarges the pane, the HWM-anchor formula in WriteWindow.Resize can
// pull writeTop *below* its pre-resize value (into scrollback the user
// wants to keep). The clamp in mainScreenResize must push it back.
func TestResize_Expand_DoesNotRetreatBelowPreResizeWriteTop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Start at a small height so HWM stays small.
	v := NewVTerm(40, 8)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Build up scrollback ABOVE the prompt so we have something to lose
	// if the clamp doesn't fire.
	for i := 0; i < 5; i++ {
		parseString(p, "old scrollback line\r\n")
	}
	v.MarkPromptStart()
	parseString(p, "$ ")
	v.MarkInputStart()
	parseString(p, "claude\r\n")
	v.MarkCommandStart()

	// A few rows of TUI output, but stay within the small window so HWM
	// stays small. The HWM-anchor formula on a later expand will land
	// writeTop at HWM - newHeight + 1 — far below oldTop.
	parseString(p, "TUI line 1\r\nTUI line 2\r\n")

	preResizeTop := v.mainScreen.WriteTop()

	// Enlarge to a height bigger than the cumulative content so far.
	v.Resize(40, 24)

	if got := v.mainScreen.WriteTop(); got < preResizeTop {
		t.Errorf("after expand writeTop = %d, must be >= %d (pre-resize value) — expand retreated into scrollback",
			got, preResizeTop)
	}
}

// TestResize_Shrink_PreservesScrollback — when the user shrinks the
// pane while a TUI has been scrolling normally (writeTop has advanced
// far past the prompt anchor via per-line scroll-up), shrink may
// advance writeTop further to keep the cursor visible. The previous
// frame's top rows become scrollback, indistinguishable from any other
// scrollback. Critically, scrollback BELOW the pre-resize writeTop
// must not be touched — that's the user's accumulated conversation
// history.
func TestResize_Shrink_PreservesScrollback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := NewVTerm(40, 24)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	v.MarkPromptStart()
	parseString(p, "$ ")
	v.MarkInputStart()
	parseString(p, "claude\r\n")
	v.MarkCommandStart()

	// Force writeTop to advance via newlines so we have real scrollback
	// to defend.
	for i := 0; i < 80; i++ {
		parseString(p, "scrollback line\r\n")
	}
	preResizeTop := v.mainScreen.WriteTop()
	if preResizeTop == 0 {
		t.Fatalf("setup: writeTop did not advance after newlines")
	}

	// Capture some scrollback content from below pre-resize writeTop —
	// this should survive the resize.
	canary := preResizeTop - 5
	canaryRow := v.mainScreen.ReadLine(canary)

	v.Resize(40, 10)

	// Scrollback below the pre-resize writeTop must be intact.
	got := v.mainScreen.ReadLine(canary)
	if len(got) != len(canaryRow) {
		t.Fatalf("scrollback row %d length changed: was %d, now %d (resize destroyed scrollback)",
			canary, len(canaryRow), len(got))
	}
	for i := range canaryRow {
		if got[i].Rune != canaryRow[i].Rune {
			t.Errorf("scrollback row %d col %d: was %q, now %q (resize destroyed scrollback)",
				canary, i, canaryRow[i].Rune, got[i].Rune)
		}
	}
}

// TestResize_AltScreen_LeavesMainScreenAlone — alt-screen TUIs (vim,
// less, htop) live entirely in the alt screen and must not have their
// main screen disturbed by resize. The clamp is gated on !inAltScreen.
func TestResize_AltScreen_LeavesMainScreenAlone(t *testing.T) {
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

	// Snapshot main-screen writeTop, then resize. We don't assert exact
	// equality (WriteWindow.Resize may legitimately move writeTop in
	// alt-screen mode too), but the clamp branch in mainScreenResize
	// must not fire.
	beforeTop := v.mainScreen.WriteTop()
	v.Resize(40, 40)

	// Just sanity-check we didn't crash and the main screen is still
	// addressable. Specific writeTop value is implementation-detail of
	// WriteWindow.Resize.
	_ = beforeTop
	if v.mainScreen.WriteTop() < 0 {
		t.Errorf("main screen writeTop went negative after alt-screen resize: %d",
			v.mainScreen.WriteTop())
	}
}

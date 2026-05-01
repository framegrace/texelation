// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Regression tests for the BSU (CSI ?2026h) frame-anchor logic in
// vterm_main_screen.go: beginFrameAnchor / resetFrameAnchor. The bug
// these guard against is duplicate scrollback rows accumulating when a
// non-alt-screen TUI (Claude Code is the canonical case) repaints full
// frames bracketed by sync-update markers and the painted height
// exceeds the viewport. Each frame's autoscroll commits the same N
// rows of overflow into scrollback, so 12 repaints stack 12× the
// overflow content.

package parser_test

import (
	"strings"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	_ "github.com/framegrace/texelation/apps/texelterm/parser/sparse"
)

// feedString runs each rune of s through the parser. Must keep CSI/OSC
// sequences intact so the test exercises the real BSU/ESU handlers.
func feedString(p *parser.Parser, s string) {
	for _, r := range s {
		p.Parse(r)
	}
}

// TestBSU_CollapsesOverflowAccumulation simulates the exact pattern
// captured from a Claude Code session at narrow width: BSU, paint
// content that overflows the viewport, ESU; repeat. Without the
// frame-anchor fix, scrollback contains N copies of the overflow
// rows. With the fix, scrollback has only the lines genuinely scrolled
// before the FIRST BSU's paint.
func TestBSU_CollapsesOverflowAccumulation(t *testing.T) {
	const cols, rows = 18, 5 // narrow + small viewport so overflow is forced
	v := parser.NewVTerm(cols, rows)
	v.EnableMemoryBuffer()
	p := parser.NewParser(v)

	// Pretend the shell forked a TUI: emit OSC 133;C so frameAnchor
	// resets to a clean state and the writeTop-rewind logic gates by
	// the frame anchor (not by some prior unrelated chain head).
	feedString(p, "\x1b]133;C\x1b\\")

	// Emit 5 sync-update frames, each painting 8 lines into a 5-row
	// viewport. Each frame overflows by 3 rows. Without the anchor
	// fix, the 5 frames stack 5×3 = 15 lines of overflow into
	// scrollback. With the fix, each frame's overflow is collapsed
	// against the previous frame's anchor, so scrollback retains only
	// the FIRST frame's overflow (3 rows).
	for f := 0; f < 5; f++ {
		feedString(p, "\x1b[?2026h") // BSU
		feedString(p, "\x1b[H")      // cursor home
		for i := 1; i <= 8; i++ {
			feedString(p, "\x1b[2K")           // EL 2 (clear line)
			feedString(p, "L")                 // distinguishable per line
			feedString(p, string(rune('0'+i))) // line number
			feedString(p, "\r\n")
		}
		feedString(p, "\x1b[?2026l") // ESU
	}

	// After 5 frames, scroll back several screenfuls and count "L1"
	// occurrences across all scrolled views. With the fix L1 should
	// appear at most once total (the first frame's overflow). Without
	// the fix it would appear 5 times.
	maxInOneView := 0
	for offset := int64(0); offset <= 50; offset += int64(rows) {
		v.SetScrollOffset(offset)
		grid := v.Grid()
		count := 0
		for _, row := range grid {
			text := strings.TrimRight(cellsToString(row), " \x00")
			if text == "L1" {
				count++
			}
		}
		if count > maxInOneView {
			maxInOneView = count
		}
	}
	if maxInOneView > 1 {
		t.Errorf("L1 appears %d times in a single scrollback view; expected ≤1 (overflow across frames must be collapsed)",
			maxInOneView)
	}
}

// TestBSU_FirstFrameDoesNotRewind — the very first BSU after a fresh
// command must NOT rewind anything; there's no previous frame to
// collapse against. Otherwise legitimate content emitted before the
// first BSU (e.g. the shell prompt that ran the TUI) would be wiped.
func TestBSU_FirstFrameDoesNotRewind(t *testing.T) {
	const cols, rows = 80, 24
	v := parser.NewVTerm(cols, rows)
	v.EnableMemoryBuffer()
	p := parser.NewParser(v)

	// Pre-existing content (shell prompt, command output etc).
	feedString(p, "shell prompt line 1\r\n")
	feedString(p, "shell prompt line 2\r\n")
	feedString(p, "\x1b]133;C\x1b\\") // command starts (TUI launches)

	cursorBefore, _ := v.CursorGlobalIdx()

	// First BSU after the command starts. Cursor hasn't advanced yet,
	// so nothing for the anchor to collapse — must be a no-op on
	// writeTop.
	feedString(p, "\x1b[?2026h") // BSU
	cursorAfter, _ := v.CursorGlobalIdx()
	if cursorAfter != cursorBefore {
		t.Errorf("first BSU moved cursor (was %d, now %d) — should be a no-op",
			cursorBefore, cursorAfter)
	}
}

// TestBSU_CommandEndResetsAnchor — frame anchor must NOT survive
// across command boundaries (OSC 133;D). A new command's first BSU
// gets a fresh anchor; if the prior command's anchor leaked through,
// the new command's paint would collapse against an unrelated
// position from a previous TUI.
func TestBSU_CommandEndResetsAnchor(t *testing.T) {
	const cols, rows = 18, 5
	v := parser.NewVTerm(cols, rows)
	v.EnableMemoryBuffer()
	p := parser.NewParser(v)

	feedString(p, "\x1b]133;C\x1b\\")
	feedString(p, "\x1b[?2026h")
	feedString(p, "claude content\r\n")
	feedString(p, "\x1b[?2026l")
	feedString(p, "\x1b]133;D\x1b\\") // command ends — anchor reset

	// Some shell output accumulates after the TUI exits.
	for i := 0; i < 10; i++ {
		feedString(p, "shell output\r\n")
	}

	cursorBefore, _ := v.CursorGlobalIdx()
	// New command starts with another TUI.
	feedString(p, "\x1b]133;C\x1b\\")
	feedString(p, "\x1b[?2026h") // first BSU of NEW command
	cursorAfter, _ := v.CursorGlobalIdx()

	// Must not have rewound writeTop into the prior shell-output
	// region. If the anchor weren't reset on 133;D, this BSU might
	// rewind back to the prior TUI's frame position.
	if cursorAfter < cursorBefore-1 {
		t.Errorf("BSU after fresh OSC 133;C rewound writeTop unexpectedly (cursor %d → %d) — frame anchor leaked across command boundary",
			cursorBefore, cursorAfter)
	}
}

// cellsToString helper.
func cellsToString(cells []parser.Cell) string {
	var b strings.Builder
	for _, c := range cells {
		if c.Rune == 0 {
			b.WriteByte(' ')
		} else {
			b.WriteRune(c.Rune)
		}
	}
	return b.String()
}

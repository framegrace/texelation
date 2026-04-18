// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"fmt"
	"testing"
)

// writeEmptyLine writes a bare Newline (no cells) — used to simulate a blank
// line in output like `ls -lR` section separators.
func writeEmptyLine(tm *Terminal) {
	tm.Newline()
}

// TestTerminal_LiveFollowThroughBlankLines reproduces the user's
// "ls -lR viewport stuck on top" symptom. `ls -lR` emits blank lines
// between directory sections, which land as empty rows in the sparse store.
// If RecomputeLiveAnchor's backward walk trips on the first empty row above
// the cursor, it falls through to "ran out of content" and snaps viewAnchor
// to 0 — pinning the viewport to the top of history even though autoFollow
// is still on.
func TestTerminal_LiveFollowThroughBlankLines(t *testing.T) {
	const height = 5
	tm := NewTerminal(20, height)

	for i := range 10 {
		writeTagged(tm, fmt.Sprintf("A%02d", i))
	}
	writeEmptyLine(tm)
	for i := range 10 {
		writeTagged(tm, fmt.Sprintf("B%02d", i))
	}

	grid := tm.RenderReflow()
	tags := gridTags(grid)

	// Last non-blank row should reference B09 (last written content).
	lastNonBlank := ""
	for i := len(tags) - 1; i >= 0; i-- {
		if tags[i] != "" {
			lastNonBlank = tags[i]
			break
		}
	}
	if lastNonBlank != "B09" {
		t.Errorf("blank-line trap: last visible tag = %q, want B09 (tags=%v)", lastNonBlank, tags)
	}
}

// TestTerminal_FastScrollDownNeverLandsAtTop reproduces the user's "fast
// scrollwheel down sometimes jumps back to the top of history" symptom. After
// scrolling deep into history, rapid ScrollDown calls with render-simulated
// RecomputeLiveAnchor checkpoints must never leave the viewport anchored at
// globalIdx 0 unless we genuinely arrived there — we're moving toward the
// live edge, not away from it.
func TestTerminal_FastScrollDownNeverLandsAtTop(t *testing.T) {
	const height = 5
	tm := NewTerminal(20, height)

	// 100 lines of output with a blank-line section break every 10 lines.
	for i := range 100 {
		writeTagged(tm, fmt.Sprintf("L%03d", i))
		if i%10 == 9 {
			writeEmptyLine(tm)
		}
	}

	// Scroll to top of history.
	tm.ScrollUp(200)
	if tags := gridTags(tm.RenderReflow()); tags[0] != "L000" {
		t.Fatalf("after ScrollUp(200), expected to be at top. tags=%v", tags)
	}

	// Rapid scroll-down: 20 ticks of 3 rows each. Render between each to let
	// RecomputeLiveAnchor run.
	for range 20 {
		tm.ScrollDown(3)
		tags := gridTags(tm.RenderReflow())
		// At no point should the viewport regress to "L000" once we've started
		// scrolling down — the test's whole premise is forward motion.
		if tags[0] == "L000" {
			t.Errorf("scroll-down regressed to top of history: tags=%v", tags)
			break
		}
	}

	// Final: large ScrollDown should leave us at the live edge.
	tm.ScrollDown(500)
	tags := gridTags(tm.RenderReflow())
	lastNonBlank := ""
	for i := len(tags) - 1; i >= 0; i-- {
		if tags[i] != "" {
			lastNonBlank = tags[i]
			break
		}
	}
	if lastNonBlank != "L099" {
		t.Errorf("after full scroll-down, last visible = %q, want L099 (tags=%v)", lastNonBlank, tags)
	}
	if !tm.IsFollowing() {
		t.Error("after full scroll-down, IsFollowing should be true")
	}
}

// TestTerminal_LiveFollowManyBlankLines exercises multiple blank-line
// separators scattered through the output, matching real `ls -lR`
// behavior across many subdirectories.
func TestTerminal_LiveFollowManyBlankLines(t *testing.T) {
	const height = 10
	tm := NewTerminal(40, height)

	for section := range 5 {
		writeTagged(tm, fmt.Sprintf("./dir%d:", section))
		for i := range 5 {
			writeTagged(tm, fmt.Sprintf("S%dF%02d entry", section, i))
		}
		writeEmptyLine(tm)
	}

	grid := tm.RenderReflow()
	tags := gridTags(grid)

	// Expect some S4-prefixed (last section) entries visible.
	sawLast := false
	for _, tag := range tags {
		if len(tag) >= 3 && tag[:2] == "S4" {
			sawLast = true
			break
		}
	}
	if !sawLast {
		t.Errorf("blank-line trap: no S4* entries visible. tags=%v", tags)
	}
}

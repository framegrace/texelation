// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"fmt"
	"strings"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// writeTagged writes "<tag>\n" via WriteCell/Newline. Each line is a tag that
// uniquely identifies a source globalIdx so tests can assert which logical
// rows landed in the viewport.
func writeTagged(tm *Terminal, tag string) {
	for _, r := range tag {
		tm.WriteCell(parser.Cell{Rune: r})
	}
	tm.Newline()
}

// gridRowToTag extracts the leading non-blank rune sequence of a grid row.
func gridRowToTag(row []parser.Cell) string {
	var b strings.Builder
	for _, c := range row {
		if c.Rune == 0 || c.Rune == ' ' {
			if b.Len() == 0 {
				continue
			}
			break
		}
		b.WriteRune(c.Rune)
	}
	return b.String()
}

// gridTags returns every row's leading tag, in order.
func gridTags(grid [][]parser.Cell) []string {
	out := make([]string, len(grid))
	for i, row := range grid {
		out[i] = gridRowToTag(row)
	}
	return out
}

// TestTerminal_RenderReflowLiveFollow verifies that RenderReflow on a freshly
// filled terminal shows the most-recent N lines (the live edge), not stale
// history anchored at globalIdx 0. This is Regression A from manual testing:
// the viewport stopped following output.
func TestTerminal_RenderReflowLiveFollow(t *testing.T) {
	const height = 5
	tm := NewTerminal(20, height)

	// Write 20 tagged lines. Each newline advances the cursor; once the cursor
	// exceeds the viewport, writeTop scrolls.
	for i := range 20 {
		writeTagged(tm, fmt.Sprintf("L%02d", i))
	}

	grid := tm.RenderReflow()
	if len(grid) != height {
		t.Fatalf("RenderReflow rows = %d, want %d", len(grid), height)
	}

	tags := gridTags(grid)
	// Cursor just newlined to row 20 (blank). Expected viewport rows 16..20.
	want := []string{"L16", "L17", "L18", "L19", ""}
	for i, w := range want {
		if tags[i] != w {
			t.Errorf("row %d: got %q, want %q (full tags=%v)", i, tags[i], w, tags)
		}
	}
}

// TestTerminal_ScrollUpChangesRenderedView is the core Regression B test: a
// user-initiated ScrollUp must make older content visible via RenderReflow.
// Before the fix, ScrollUp only mutates the legacy viewBottom field while
// RenderReflow reads from viewAnchor — so the rendered grid was unchanged.
func TestTerminal_ScrollUpChangesRenderedView(t *testing.T) {
	const height = 5
	tm := NewTerminal(20, height)
	for i := range 20 {
		writeTagged(tm, fmt.Sprintf("L%02d", i))
	}

	before := gridTags(tm.RenderReflow())

	tm.ScrollUp(3)

	after := gridTags(tm.RenderReflow())

	if equalSlices(before, after) {
		t.Fatalf("ScrollUp(3) did not change RenderReflow output: %v", before)
	}
	if tm.IsFollowing() {
		t.Error("after ScrollUp, IsFollowing should be false")
	}

	// Explicit shape: scrolled 3 rows back from the live edge, so the
	// viewport should now end 3 rows earlier. before ended at blank-cursor-row
	// (L16..L19,""); after should end at L16 (or thereabouts).
	lastNonBlank := ""
	for i := len(after) - 1; i >= 0; i-- {
		if after[i] != "" {
			lastNonBlank = after[i]
			break
		}
	}
	if lastNonBlank == "" {
		t.Fatalf("after ScrollUp, all rows blank: %v", after)
	}
	if lastNonBlank == "L19" {
		t.Errorf("after ScrollUp(3), last visible tag is still %q — view did not move", lastNonBlank)
	}
}

// TestTerminal_ScrollDownDoesNotSnapPrematurely exercises the symmetry bug
// between ScrollUpRows and ScrollDownRows. After scrolling up by several
// reflowed rows, a single ScrollDown should advance by one reflowed row —
// NOT snap to the live edge. The previous failure was that viewBottom
// diverged from the actual scrolled amount (reflow walked farther than
// viewBottom was decremented), so ScrollDown hit writeBottom after one click.
func TestTerminal_ScrollDownDoesNotSnapPrematurely(t *testing.T) {
	const height = 5
	tm := NewTerminal(20, height)
	for i := range 20 {
		writeTagged(tm, fmt.Sprintf("L%02d", i))
	}

	tm.ScrollUp(5)
	if tm.IsFollowing() {
		t.Fatalf("after ScrollUp(5), IsFollowing should be false")
	}
	afterUp := gridTags(tm.RenderReflow())

	tm.ScrollDown(1)
	afterDown1 := gridTags(tm.RenderReflow())

	if tm.IsFollowing() {
		t.Errorf("after ScrollDown(1) from 5-back, IsFollowing should still be false; view=%v", afterDown1)
	}
	if equalSlices(afterUp, afterDown1) {
		t.Errorf("ScrollDown(1) from 5-rows-back did not move view\nafterUp=  %v\nafterDown=%v", afterUp, afterDown1)
	}
}

// TestTerminal_ScrollUpDownRoundTrip verifies that scrolling up N reflowed
// rows then down N rows returns to the live view.
func TestTerminal_ScrollUpDownRoundTrip(t *testing.T) {
	const height = 5
	tm := NewTerminal(20, height)
	for i := range 20 {
		writeTagged(tm, fmt.Sprintf("L%02d", i))
	}
	live := gridTags(tm.RenderReflow())

	for range 4 {
		tm.ScrollUp(1)
	}
	for range 4 {
		tm.ScrollDown(1)
	}
	after := gridTags(tm.RenderReflow())

	if !equalSlices(live, after) {
		t.Errorf("ScrollUp(1)x4 then ScrollDown(1)x4 did not round-trip\nlive:  %v\nafter: %v", live, after)
	}
}

// TestTerminal_ScrollWithWrappedChainRoundTrip verifies that when the live
// edge sits on a wrapped chain (cursor position advances the view past
// multiple reflowed rows per globalIdx), ScrollUp(1) followed by ScrollDown(1)
// returns to the same view — testing the viewBottom / viewAnchor divergence.
func TestTerminal_ScrollWithWrappedChainRoundTrip(t *testing.T) {
	const width, height = 20, 5
	tm := NewTerminal(width, height)
	// 5 plain lines, then a 60-char line that'll wrap 3 rows at width 20.
	for i := range 5 {
		writeTagged(tm, fmt.Sprintf("L%02d", i))
	}
	long := strings.Repeat("x", 60)
	for _, r := range long {
		tm.WriteCell(parser.Cell{Rune: r})
	}
	tm.Newline()
	writeTagged(tm, "END")

	live := gridTags(tm.RenderReflow())
	tm.ScrollUp(1)
	tm.ScrollDown(1)
	after := gridTags(tm.RenderReflow())
	if !equalSlices(live, after) {
		t.Errorf("ScrollUp(1)+ScrollDown(1) near wrapped chain did not round-trip\nlive:  %v\nafter: %v", live, after)
	}
}

// TestTerminal_ScrollUpClampedThenScrollDown reproduces the velocity-overshoot
// regression: ScrollUp with a large n hits the top (viewAnchor=0) with n
// unsatisfied. Previously viewBottom was decremented by the full n, so a
// subsequent ScrollDown(1) saw viewBottom way below writeBottom and took
// many clicks to reach the live edge. The fix: only decrement viewBottom by
// rows actually walked.
func TestTerminal_ScrollUpClampedThenScrollDown(t *testing.T) {
	const height = 5
	tm := NewTerminal(20, height)
	for i := range 10 {
		writeTagged(tm, fmt.Sprintf("L%02d", i))
	}
	// 10 lines written, height 5, so only ~5 rows of scrollback above the
	// live view. Scroll up by a large amount; the clamp should kick in.
	tm.ScrollUp(50)

	// Now ScrollDown(1) should move the view down by 1 — not snap to live
	// edge from the phantom "scrolled 50 back" state.
	tm.ScrollDown(1)
	if tm.IsFollowing() {
		t.Errorf("ScrollDown(1) after clamped ScrollUp(50) snapped to live edge prematurely")
	}
}

// TestTerminal_VelocityScrollDownDoesNotOvershoot simulates the scenario where
// a user scrolls up by small amounts, then scrolls down with a velocity-
// multiplied click. The velocity n can exceed the distance to the live edge;
// the scroll down should snap to live — but only because we're at live, not
// because viewBottom was in a stale state.
func TestTerminal_VelocityScrollDownDoesNotOvershoot(t *testing.T) {
	const height = 5
	tm := NewTerminal(20, height)
	for i := range 20 {
		writeTagged(tm, fmt.Sprintf("L%02d", i))
	}
	// Scroll up 2 rows.
	tm.ScrollUp(2)
	if tm.IsFollowing() {
		t.Fatalf("after ScrollUp(2), IsFollowing should be false")
	}

	// Scroll down with velocity-multiplied n (10). Should snap to live edge.
	tm.ScrollDown(10)
	if !tm.IsFollowing() {
		t.Errorf("ScrollDown(10) past live edge should snap + re-engage autoFollow")
	}
}

// TestTerminal_ScrollToBottomReturnsToLiveEdge verifies that after scrolling
// back and re-engaging auto-follow, RenderReflow shows the latest content.
func TestTerminal_ScrollToBottomReturnsToLiveEdge(t *testing.T) {
	const height = 5
	tm := NewTerminal(20, height)
	for i := range 20 {
		writeTagged(tm, fmt.Sprintf("L%02d", i))
	}
	live := gridTags(tm.RenderReflow())

	tm.ScrollUp(5)
	scrolled := gridTags(tm.RenderReflow())
	if equalSlices(live, scrolled) {
		t.Fatal("ScrollUp had no effect on RenderReflow")
	}

	tm.ScrollToBottom()
	restored := gridTags(tm.RenderReflow())

	if !equalSlices(live, restored) {
		t.Errorf("ScrollToBottom did not restore live view\nlive:     %v\nrestored: %v", live, restored)
	}
	if !tm.IsFollowing() {
		t.Error("after ScrollToBottom, IsFollowing should be true")
	}
}

// TestTerminal_NewContentAdvancesLiveView verifies that while auto-following,
// writing new content advances the rendered viewport.
func TestTerminal_NewContentAdvancesLiveView(t *testing.T) {
	const height = 5
	tm := NewTerminal(20, height)
	for i := range 10 {
		writeTagged(tm, fmt.Sprintf("L%02d", i))
	}
	before := gridTags(tm.RenderReflow())

	for i := 10; i < 20; i++ {
		writeTagged(tm, fmt.Sprintf("L%02d", i))
	}
	after := gridTags(tm.RenderReflow())

	if equalSlices(before, after) {
		t.Fatalf("live-follow: viewport did not advance after 10 new lines: %v", before)
	}
	// Expect the last non-blank row to now reference L19.
	lastNonBlank := ""
	for i := len(after) - 1; i >= 0; i-- {
		if after[i] != "" {
			lastNonBlank = after[i]
			break
		}
	}
	if lastNonBlank != "L19" {
		t.Errorf("live-follow: last visible tag = %q, want L19 (full tags=%v)", lastNonBlank, after)
	}
}

// TestTerminal_ScrollUpSingleRowDoesNotJumpToTop is the core "binary jump"
// regression. Previously, ScrollUpRows' post-loop cleanup unconditionally
// reset viewAnchor = 0 after the walk loop terminated, even when the loop
// exited normally because `remaining` had been fully consumed. Consequence:
// a single ScrollUp(1) through a chain that consumed exactly one reflowed
// row in one iteration landed viewAnchor at the chain start (correct), then
// the cleanup overwrote it to 0 (top of history). Reflowed rows should move
// incrementally — not teleport to the top on any non-partial-chain walk.
func TestTerminal_ScrollUpSingleRowDoesNotJumpToTop(t *testing.T) {
	const height = 5
	tm := NewTerminal(20, height)
	// 20 single-row chains. At live edge, viewAnchor is L15, offset=0 for
	// a height-5 viewport showing L15..L19. ScrollUp(1) should walk back to
	// L14, NOT to L00.
	for i := range 20 {
		writeTagged(tm, fmt.Sprintf("L%02d", i))
	}
	_ = tm.RenderReflow() // Triggers RecomputeLiveAnchor.

	tm.ScrollUp(1)
	after := gridTags(tm.RenderReflow())
	// Live view was L16..L19, "" (blank cursor row). ScrollUp(1) shifts the
	// viewport back by exactly one reflowed row → L15..L19. The binary-jump
	// bug previously teleported viewAnchor to 0 here (L00..L04).
	want := []string{"L15", "L16", "L17", "L18", "L19"}
	for i, w := range want {
		if after[i] != w {
			t.Errorf("row %d: got %q, want %q (full=%v)", i, after[i], w, after)
		}
	}
}

// TestTerminal_ScrollDownSingleRowDoesNotJumpToLive verifies the same
// per-row granularity holds for the symmetric direction after several
// ScrollUp clicks: ScrollDown(1) should advance by exactly one reflowed
// row, not snap to the live edge on the first click.
func TestTerminal_ScrollDownSingleRowDoesNotJumpToLive(t *testing.T) {
	const height = 5
	tm := NewTerminal(20, height)
	for i := range 20 {
		writeTagged(tm, fmt.Sprintf("L%02d", i))
	}
	// Scroll up 5 rows from live edge so we have room to scroll down by 1.
	for range 5 {
		tm.ScrollUp(1)
	}
	before := gridTags(tm.RenderReflow())

	tm.ScrollDown(1)
	after := gridTags(tm.RenderReflow())

	if equalSlices(before, after) {
		t.Fatalf("ScrollDown(1) did not move view\nbefore=%v\nafter=%v", before, after)
	}
	if tm.IsFollowing() {
		t.Errorf("ScrollDown(1) 5-back snapped to live edge prematurely")
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"testing"
)

// TestAltScreenPreservesScrollback verifies that scrollback history is preserved
// when entering and exiting alt screen (like vim, less, htop).
func TestAltScreenPreservesScrollback(t *testing.T) {
	v := NewVTerm(80, 24)
	p := NewParser(v)

	// Fill screen with content to create scrollback
	// Write more than 24 lines to ensure some go into scrollback
	for i := 0; i < 40; i++ {
		line := []byte("Line " + string('A'+byte(i%26)) + " - scrollback test\r\n")
		for _, ch := range line {
			p.Parse(rune(ch))
		}
	}

	// Verify we have scrollback
	historyLen := v.HistoryLength()
	t.Logf("History length before alt screen: %d", historyLen)
	if historyLen < 40 {
		t.Errorf("Expected at least 40 lines in history, got %d", historyLen)
	}

	// Check that we can get some early lines
	line0 := v.getHistoryLine(0)
	if line0 == nil || len(line0) == 0 {
		t.Error("Line 0 should exist before alt screen")
	} else {
		t.Logf("Line 0 content: %q", gridLineToString(line0))
	}

	// Enter alt screen (DECSET 1049)
	for _, ch := range "\x1b[?1049h" {
		p.Parse(ch)
	}
	if !v.InAltScreen() {
		t.Error("Should be in alt screen after DECSET 1049")
	}

	// Write some content in alt screen
	for _, ch := range "Alt screen content - this is vim or less\r\n" {
		p.Parse(ch)
	}

	// Exit alt screen (DECRST 1049)
	for _, ch := range "\x1b[?1049l" {
		p.Parse(ch)
	}
	if v.InAltScreen() {
		t.Error("Should not be in alt screen after DECRST 1049")
	}

	// *** KEY TEST: Scrollback should still be available ***
	historyLenAfter := v.HistoryLength()
	t.Logf("History length after alt screen exit: %d", historyLenAfter)
	if historyLenAfter < historyLen {
		t.Errorf("History shrunk from %d to %d after alt screen exit", historyLen, historyLenAfter)
	}

	// Check that early lines are still accessible
	line0After := v.getHistoryLine(0)
	if line0After == nil || len(line0After) == 0 {
		t.Error("Line 0 should still exist after alt screen exit")
	} else {
		content := gridLineToString(line0After)
		t.Logf("Line 0 content after: %q", content)
		if content != gridLineToString(line0) {
			t.Error("Line 0 content changed after alt screen roundtrip")
		}
	}

	// Verify grid shows correct content (should be last lines of scrollback)
	grid := v.Grid()
	if grid == nil {
		t.Fatal("Grid returned nil after alt screen exit")
	}
	t.Logf("Grid[0]: %q", gridLineToString(grid[0]))
}

// TestAltScreenWithEraseBeforeEntry tests apps that clear screen before entering alt screen
func TestAltScreenWithEraseBeforeEntry(t *testing.T) {
	v := NewVTerm(80, 24)
	p := NewParser(v)

	// Fill screen with content
	for i := 0; i < 40; i++ {
		line := []byte("Line " + string('A'+byte(i%26)) + " - content\r\n")
		for _, ch := range line {
			p.Parse(rune(ch))
		}
	}

	historyLen := v.HistoryLength()
	t.Logf("History length before: %d", historyLen)

	line0 := v.getHistoryLine(0)
	line0Content := gridLineToString(line0)
	t.Logf("Line 0 before: %q", line0Content)

	// Some apps send ED 2 (clear entire screen) BEFORE entering alt screen
	// This should NOT clear scrollback
	for _, ch := range "\x1b[2J" {
		p.Parse(ch)
	}

	// Then enter alt screen
	for _, ch := range "\x1b[?1049h" {
		p.Parse(ch)
	}

	// Alt screen content
	for _, ch := range "App running...\r\n" {
		p.Parse(ch)
	}

	// Exit alt screen
	for _, ch := range "\x1b[?1049l" {
		p.Parse(ch)
	}

	// Scrollback should still be available
	historyLenAfter := v.HistoryLength()
	t.Logf("History length after: %d", historyLenAfter)

	line0After := v.getHistoryLine(0)
	if line0After == nil {
		t.Error("Line 0 should still exist after ED 2 + alt screen roundtrip")
	} else {
		line0AfterContent := gridLineToString(line0After)
		t.Logf("Line 0 after: %q", line0AfterContent)
		// Note: ED 2 might have cleared the visible portion, but scrollback should remain
		if historyLenAfter == 0 {
			t.Error("History was completely cleared - scrollback should be preserved")
		}
	}
}

// TestAltScreenScrollingAfterExit verifies that scrolling still works after exiting alt screen
func TestAltScreenScrollingAfterExit(t *testing.T) {
	v := NewVTerm(80, 24)
	p := NewParser(v)

	// Fill with enough content to have scrollback
	for i := 0; i < 50; i++ {
		line := []byte("Line " + string('A'+byte(i%26)) + " number " + string('0'+byte(i/10)) + string('0'+byte(i%10)) + "\r\n")
		for _, ch := range line {
			p.Parse(rune(ch))
		}
	}

	historyLen := v.HistoryLength()
	t.Logf("Before alt screen: HistoryLength=%d", historyLen)

	// Verify we're at live edge before alt screen
	if !v.AtLiveEdge() {
		t.Error("Should be at live edge before alt screen")
	}

	// Enter alt screen
	for _, ch := range "\x1b[?1049h" {
		p.Parse(ch)
	}

	// Do something in alt screen
	for _, ch := range "Running less...\r\n" {
		p.Parse(ch)
	}

	// Exit alt screen
	for _, ch := range "\x1b[?1049l" {
		p.Parse(ch)
	}

	t.Logf("After alt screen: HistoryLength=%d", v.HistoryLength())

	// *** KEY TEST: Should have scrollback available ***
	// History should be longer than visible viewport (24 lines)
	if v.HistoryLength() <= 24 {
		t.Errorf("Expected history > 24, got %d", v.HistoryLength())
	}

	// Try to scroll up using the Scroll method
	v.Scroll(-5) // Negative to scroll up (back in history)

	// If we scrolled, we should no longer be at live edge
	if v.AtLiveEdge() {
		t.Error("Should not be at live edge after scrolling up")
	}

	// Check grid shows older content after scrolling
	grid := v.Grid()
	t.Logf("Grid[0] after scroll: %q", gridLineToString(grid[0]))
}

// TestScrollRegionPreservesScrollback tests that scroll regions (like codex uses)
// don't lose scrollback history from before the TUI started.
// This is different from alt screen - codex runs on the main screen with scroll regions.
func TestScrollRegionPreservesScrollback(t *testing.T) {
	v := NewVTerm(80, 24)
	p := NewParser(v)

	// Create scrollback history BEFORE codex starts
	for i := 0; i < 50; i++ {
		line := "Pre-TUI line " + string('A'+byte(i%26)) + " number " + string('0'+byte(i/10)) + string('0'+byte(i%10)) + "\r\n"
		for _, ch := range line {
			p.Parse(ch)
		}
	}

	historyLenBefore := v.HistoryLength()
	t.Logf("Before TUI: HistoryLength=%d, liveEdgeBase=%d", historyLenBefore, v.memBufState.liveEdgeBase)

	// Verify we have scrollback
	line0Before := v.getHistoryLine(0)
	t.Logf("Line 0 before: %q", gridLineToString(line0Before))

	// Now simulate codex starting - it sets a scroll region
	// Set scroll region to rows 1-12 (1-indexed) = rows 0-11 (0-indexed)
	for _, ch := range "\x1b[1;12r" {
		p.Parse(ch)
	}

	t.Logf("After scroll region set: cursor at (%d, %d)", v.CursorX(), v.CursorY())

	// Codex draws some UI content within the scroll region
	for i := 0; i < 10; i++ {
		line := "Codex UI line " + string('0'+byte(i)) + "\r\n"
		for _, ch := range line {
			p.Parse(ch)
		}
	}

	t.Logf("After codex UI: HistoryLength=%d, liveEdgeBase=%d", v.HistoryLength(), v.memBufState.liveEdgeBase)

	// Codex exits - clears from cursor to end of screen and resets margins
	// First, move cursor to row 13 (below the scroll region)
	for _, ch := range "\x1b[13;1H" {
		p.Parse(ch)
	}
	// ED 0 - erase from cursor to end
	for _, ch := range "\x1b[J" {
		p.Parse(ch)
	}
	// Reset scroll region (DECSTBM with no params = full screen)
	for _, ch := range "\x1b[r" {
		p.Parse(ch)
	}

	historyLenAfter := v.HistoryLength()
	t.Logf("After TUI exit: HistoryLength=%d, liveEdgeBase=%d", historyLenAfter, v.memBufState.liveEdgeBase)

	// *** KEY TEST: Original scrollback should still be accessible ***
	line0After := v.getHistoryLine(0)
	if line0After == nil {
		t.Error("Line 0 should still exist after TUI exit")
	} else {
		t.Logf("Line 0 after: %q", gridLineToString(line0After))
	}

	// Try to scroll up - should be able to reach pre-TUI content
	v.Scroll(-20) // Scroll up 20 lines
	if v.AtLiveEdge() {
		t.Error("Should not be at live edge after scrolling up")
	}

	grid := v.Grid()
	firstVisibleLine := gridLineToString(grid[0])
	t.Logf("First visible line after scroll: %q", firstVisibleLine)

	// The visible content should include pre-TUI lines
	if len(firstVisibleLine) > 0 && firstVisibleLine[0:min(7, len(firstVisibleLine))] != "Pre-TUI" {
		// This could also be empty lines or codex content - let's check if we can reach pre-TUI
		v.Scroll(-50) // Scroll way up
		grid = v.Grid()
		t.Logf("After max scroll, line 0: %q", gridLineToString(grid[0]))
	}
}

// TestScrollRegionDetailedState tests detailed state before/after scroll region operations
func TestScrollRegionDetailedState(t *testing.T) {
	v := NewVTerm(80, 24)
	p := NewParser(v)

	// Create scrollback history BEFORE codex starts
	for i := 0; i < 50; i++ {
		line := "Pre-TUI line " + string('A'+byte(i%26)) + " number " + string('0'+byte(i/10)) + string('0'+byte(i%10)) + "\r\n"
		for _, ch := range line {
			p.Parse(ch)
		}
	}

	mb := v.memBufState.memBuf
	t.Logf("Before TUI: GlobalOffset=%d, GlobalEnd=%d, TotalLines=%d, liveEdgeBase=%d",
		mb.GlobalOffset(), mb.GlobalEnd(), mb.TotalLines(), v.memBufState.liveEdgeBase)

	// Set scroll region and do some scrolling like codex would
	for _, ch := range "\x1b[1;12r" {
		p.Parse(ch)
	}
	for _, ch := range "\x1b[H" { // Move to home
		p.Parse(ch)
	}

	// Simulate some scrolling within the region
	for i := 0; i < 5; i++ {
		for _, ch := range "Codex content line\r\n" {
			p.Parse(ch)
		}
	}

	t.Logf("After codex activity: GlobalOffset=%d, GlobalEnd=%d, TotalLines=%d, liveEdgeBase=%d",
		mb.GlobalOffset(), mb.GlobalEnd(), mb.TotalLines(), v.memBufState.liveEdgeBase)

	// Reset scroll region
	for _, ch := range "\x1b[r" {
		p.Parse(ch)
	}

	t.Logf("After scroll region reset: GlobalOffset=%d, GlobalEnd=%d, TotalLines=%d, liveEdgeBase=%d",
		mb.GlobalOffset(), mb.GlobalEnd(), mb.TotalLines(), v.memBufState.liveEdgeBase)

	// Check viewport state
	vw := v.memBufState.viewport
	t.Logf("Viewport: TotalPhysicalLines=%d, IsAtLiveEdge=%v, CanScrollUp=%v",
		vw.TotalPhysicalLines(), vw.IsAtLiveEdge(), vw.CanScrollUp())

	// Try scrolling - should be able to scroll quite a bit
	totalPhysical := vw.TotalPhysicalLines()
	if totalPhysical < 50 {
		t.Errorf("TotalPhysicalLines should be at least 50 (have 50+ lines), got %d", totalPhysical)
	}
}

// Helper to convert grid line to string for debugging
func gridLineToString(cells []Cell) string {
	if cells == nil {
		return "<nil>"
	}
	result := make([]rune, len(cells))
	for i, cell := range cells {
		if cell.Rune == 0 {
			result[i] = ' '
		} else {
			result[i] = cell.Rune
		}
	}
	// Trim trailing spaces
	end := len(result)
	for end > 0 && result[end-1] == ' ' {
		end--
	}
	return string(result[:end])
}

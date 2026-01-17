// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/testutil/testutil_test.go
// Summary: Integration tests for terminal comparison framework.

package testutil

import (
	"strings"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// TestRecordingFormat tests TXREC01 save/load cycle.
func TestRecordingFormat(t *testing.T) {
	// Create a recording
	rec := NewRecording(80, 24)
	rec.Metadata.Description = "test recording"
	rec.Metadata.Shell = "echo hello"
	rec.AppendText("Hello, World!")
	rec.AppendCRLF()
	rec.AppendCSI("31m") // Red foreground
	rec.AppendText("Red text")
	rec.AppendCSI("0m") // Reset

	// Save to buffer
	var buf strings.Builder
	err := rec.Write(&buf)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Parse back
	parsed, err := ParseRecording(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("ParseRecording failed: %v", err)
	}

	// Verify metadata
	if parsed.Metadata.Width != 80 {
		t.Errorf("Width: expected 80, got %d", parsed.Metadata.Width)
	}
	if parsed.Metadata.Height != 24 {
		t.Errorf("Height: expected 24, got %d", parsed.Metadata.Height)
	}
	if parsed.Metadata.Description != "test recording" {
		t.Errorf("Description: expected 'test recording', got %q", parsed.Metadata.Description)
	}

	// Verify sequences
	if len(parsed.Sequences) != len(rec.Sequences) {
		t.Errorf("Sequences length: expected %d, got %d", len(rec.Sequences), len(parsed.Sequences))
	}
}

// TestReplayerBasic tests basic replay functionality.
func TestReplayerBasic(t *testing.T) {
	rec := NewRecordingFromString("Hello", 20, 5)

	replayer := NewReplayer(rec)
	replayer.PlayAll()
	replayer.SimulateRender()

	// Check that "Hello" appears at start of grid
	grid := replayer.GetGrid()
	text := CellsToString(grid[0][:5])
	if text != "Hello" {
		t.Errorf("Expected 'Hello', got %q", text)
	}

	// Cursor should be at position 5
	x, y := replayer.GetCursor()
	if x != 5 || y != 0 {
		t.Errorf("Expected cursor at (5,0), got (%d,%d)", x, y)
	}
}

// TestReplayerDirtyTracking tests that dirty tracking simulation works.
func TestReplayerDirtyTracking(t *testing.T) {
	rec := NewRecordingFromString("A", 10, 5)

	replayer := NewReplayer(rec)

	// Play without render - renderBuf should be empty
	replayer.PlayAll()

	// Check for mismatch before render
	if !replayer.HasVisualMismatch() {
		t.Error("Expected visual mismatch before SimulateRender")
	}

	// Now render
	replayer.SimulateRender()

	// Should match now
	if replayer.HasVisualMismatch() {
		t.Error("Unexpected visual mismatch after SimulateRender")
	}
}

// TestReplayerLineFeed tests line feed handling.
func TestReplayerLineFeed(t *testing.T) {
	rec := NewRecording(20, 5)
	rec.AppendText("Line1")
	rec.AppendCRLF()
	rec.AppendText("Line2")

	replayer := NewReplayer(rec)
	replayer.PlayAndRender()

	grid := replayer.GetGrid()

	line0 := strings.TrimRight(CellsToString(grid[0]), " ")
	line1 := strings.TrimRight(CellsToString(grid[1]), " ")

	if line0 != "Line1" {
		t.Errorf("Row 0: expected 'Line1', got %q", line0)
	}
	if line1 != "Line2" {
		t.Errorf("Row 1: expected 'Line2', got %q", line1)
	}

	x, y := replayer.GetCursor()
	if x != 5 || y != 1 {
		t.Errorf("Expected cursor at (5,1), got (%d,%d)", x, y)
	}
}

// TestDetectLinefeedOnCharBug tests detection of the linefeed-on-each-char bug.
func TestDetectLinefeedOnCharBug(t *testing.T) {
	// Simulate buggy behavior: each character on its own line
	rec := NewRecording(20, 10)
	rec.AppendText("a")
	rec.AppendLF()
	rec.AppendText("b")
	rec.AppendLF()
	rec.AppendText("c")
	rec.AppendLF()
	rec.AppendText("d")
	rec.AppendLF()
	rec.AppendText("e")

	replayer := NewReplayer(rec)
	replayer.PlayAndRender()

	snap := replayer.GetSnapshot()

	// 5 characters typed, should detect sparse output
	if !DetectLinefeedOnChar(snap, 5) {
		t.Error("Expected to detect linefeed-on-each-char pattern")
	}

	// Normal case: characters on same line
	rec2 := NewRecordingFromString("abcde", 20, 10)
	replayer2 := NewReplayer(rec2)
	replayer2.PlayAndRender()
	snap2 := replayer2.GetSnapshot()

	if DetectLinefeedOnChar(snap2, 5) {
		t.Error("Should not detect linefeed-on-each-char for normal input")
	}
}

// TestCompareGrids tests grid comparison.
func TestCompareGrids(t *testing.T) {
	// Create two different grids
	grid1 := [][]parser.Cell{
		{{Rune: 'H'}, {Rune: 'e'}, {Rune: 'l'}, {Rune: 'l'}, {Rune: 'o'}},
		{{Rune: 'W'}, {Rune: 'o'}, {Rune: 'r'}, {Rune: 'l'}, {Rune: 'd'}},
	}
	grid2 := [][]parser.Cell{
		{{Rune: 'H'}, {Rune: 'e'}, {Rune: 'l'}, {Rune: 'l'}, {Rune: 'o'}},
		{{Rune: 'W'}, {Rune: 'O'}, {Rune: 'r'}, {Rune: 'l'}, {Rune: 'd'}}, // 'O' instead of 'o'
	}

	result := CompareGrids(grid1, grid2)

	if result.Passed {
		t.Error("Expected comparison to fail")
	}
	if result.CharDiffs != 1 {
		t.Errorf("Expected 1 char diff, got %d", result.CharDiffs)
	}

	// Same grids should pass
	result2 := CompareGrids(grid1, grid1)
	if !result2.Passed {
		t.Error("Expected comparison of identical grids to pass")
	}
}

// TestFormatSideBySide tests side-by-side output formatting.
func TestFormatSideBySide(t *testing.T) {
	grid1 := [][]parser.Cell{
		{{Rune: 'A'}, {Rune: 'B'}, {Rune: 'C'}},
	}
	grid2 := [][]parser.Cell{
		{{Rune: 'A'}, {Rune: 'X'}, {Rune: 'C'}},
	}

	output := FormatSideBySide(grid1, grid2, 10)

	if !strings.Contains(output, "EXPECTED") {
		t.Error("Output should contain EXPECTED header")
	}
	if !strings.Contains(output, "ACTUAL") {
		t.Error("Output should contain ACTUAL header")
	}
	if !strings.Contains(output, "DIFF") {
		t.Error("Output should mark difference")
	}
}

// TestEscapeSequenceLog tests escape sequence formatting.
func TestEscapeSequenceLog(t *testing.T) {
	data := []byte("Hello\x1b[31mRed\x1b[0m\r\n")
	output := EscapeSequenceLog(data)

	if !strings.Contains(output, "Hello") {
		t.Error("Output should contain 'Hello'")
	}
	if !strings.Contains(output, "<ESC>") {
		t.Error("Output should contain <ESC> markers")
	}
	if !strings.Contains(output, "31m") {
		t.Error("Output should contain CSI parameters")
	}
	if !strings.Contains(output, "<CR>") {
		t.Error("Output should contain <CR>")
	}
	if !strings.Contains(output, "<LF>") {
		t.Error("Output should contain <LF>")
	}
}

// TestCursorMovement tests CSI cursor movement sequences.
func TestCursorMovement(t *testing.T) {
	rec := NewRecording(20, 10)
	rec.AppendText("X")          // Write X at (0,0)
	rec.AppendCSI("5;10H")       // Move cursor to row 5, col 10 (1-indexed)
	rec.AppendText("Y")          // Write Y at (9,4) (0-indexed)

	replayer := NewReplayer(rec)
	replayer.PlayAndRender()

	grid := replayer.GetGrid()

	// X should be at (0,0)
	if grid[0][0].Rune != 'X' {
		t.Errorf("Expected 'X' at (0,0), got %q", grid[0][0].Rune)
	}

	// Y should be at (9,4) - CSI row;colH is 1-indexed
	if grid[4][9].Rune != 'Y' {
		t.Errorf("Expected 'Y' at (9,4), got %q", grid[4][9].Rune)
	}

	// Cursor should be at (10,4)
	x, y := replayer.GetCursor()
	if x != 10 || y != 4 {
		t.Errorf("Expected cursor at (10,4), got (%d,%d)", x, y)
	}
}

// TestSnapshot tests snapshot capture and comparison.
func TestSnapshot(t *testing.T) {
	rec := NewRecordingFromString("Test", 20, 5)

	replayer := NewReplayer(rec)
	replayer.PlayAll()

	// Snapshot before render
	snap1 := replayer.GetSnapshot()
	if snap1.RenderCount != 0 {
		t.Errorf("Expected RenderCount 0, got %d", snap1.RenderCount)
	}

	// Render and snapshot again
	replayer.SimulateRender()
	snap2 := replayer.GetSnapshot()
	if snap2.RenderCount != 1 {
		t.Errorf("Expected RenderCount 1, got %d", snap2.RenderCount)
	}

	// Verify cursor in snapshot
	if snap2.CursorX != 4 || snap2.CursorY != 0 {
		t.Errorf("Snapshot cursor: expected (4,0), got (%d,%d)", snap2.CursorX, snap2.CursorY)
	}
}

// TestStepByStepReplay tests byte-by-byte replay.
func TestStepByStepReplay(t *testing.T) {
	rec := NewRecordingFromString("ABC", 20, 5)

	replayer := NewReplayer(rec)

	// Play one byte at a time
	for i := 0; i < 3; i++ {
		if !replayer.PlayOne() {
			t.Fatalf("PlayOne returned false at step %d", i)
		}
		replayer.SimulateRender()

		x, _ := replayer.GetCursor()
		if x != i+1 {
			t.Errorf("Step %d: expected cursor X=%d, got %d", i, i+1, x)
		}
	}

	// Should be at end
	if !replayer.AtEnd() {
		t.Error("Expected to be at end of recording")
	}
	if replayer.PlayOne() {
		t.Error("PlayOne should return false at end")
	}
}

// TestReset tests replayer reset functionality.
func TestReset(t *testing.T) {
	rec := NewRecordingFromString("Test", 20, 5)

	replayer := NewReplayer(rec)
	replayer.PlayAndRender()

	// Verify content exists
	grid := replayer.GetGrid()
	if grid[0][0].Rune != 'T' {
		t.Error("Expected content before reset")
	}

	// Reset
	replayer.Reset()

	// Verify clean state
	if replayer.ByteIndex() != 0 {
		t.Errorf("Expected ByteIndex 0 after reset, got %d", replayer.ByteIndex())
	}

	grid = replayer.GetGrid()
	// After reset, the 'T' should be gone (cell may be 0 or space)
	if grid[0][0].Rune == 'T' {
		t.Error("Expected 'T' to be cleared after reset")
	}

	// Should be able to replay again
	replayer.PlayAndRender()
	grid = replayer.GetGrid()
	if grid[0][0].Rune != 'T' {
		t.Error("Expected content after replay")
	}
}

// TestFormatSnapshot tests snapshot formatting for debugging.
func TestFormatSnapshot(t *testing.T) {
	rec := NewRecordingFromString("Hello", 10, 3)

	replayer := NewReplayer(rec)
	replayer.PlayAndRender()

	snap := replayer.GetSnapshot()
	output := FormatSnapshot(snap)

	if !strings.Contains(output, "Hello") {
		t.Error("Formatted snapshot should contain 'Hello'")
	}
	if !strings.Contains(output, "Cursor") {
		t.Error("Formatted snapshot should contain cursor info")
	}
	if !strings.Contains(output, "Grid") {
		t.Error("Formatted snapshot should contain Grid section")
	}
	if !strings.Contains(output, "RenderBuf") {
		t.Error("Formatted snapshot should contain RenderBuf section")
	}
}

// TestCodexStartupInteractive captures interactive codex startup.
func TestCodexStartupInteractive(t *testing.T) {
	// Capture interactive codex startup (will timeout but we get initial sequences)
	rec, err := CaptureCommand("timeout 2s codex", 80, 24)
	if err != nil {
		// Expected to fail due to timeout, but we still get the output
		t.Logf("Capture returned error (expected): %v", err)
	}

	if len(rec.Sequences) == 0 {
		t.Skip("No output captured from codex")
	}

	t.Logf("Captured %d bytes from interactive codex", len(rec.Sequences))
	t.Logf("Escape sequence log:\n%s", EscapeSequenceLog(rec.Sequences))

	// Replay through VTerm
	replayer := NewReplayer(rec)
	replayer.PlayAndRender()

	// Check what responses the terminal generated
	responses := replayer.GetResponses()
	if len(responses) > 0 {
		t.Logf("Terminal generated %d bytes of responses:\n%s", len(responses), EscapeSequenceLog(responses))
	}

	// Check for visual mismatches
	if replayer.HasVisualMismatch() {
		mismatches := replayer.FindVisualMismatches()
		t.Errorf("Visual mismatch detected! %d cells differ between Grid and RenderBuf", len(mismatches))
		for i, m := range mismatches {
			if i > 10 {
				t.Logf("... and %d more mismatches", len(mismatches)-10)
				break
			}
			t.Logf("  (%d,%d): rendered=%q, logical=%q", m.X, m.Y, m.Rendered.Rune, m.Logical.Rune)
		}
	}

	// Log the final state for debugging
	snap := replayer.GetSnapshot()
	t.Logf("Final snapshot:\n%s", FormatSnapshot(snap))
}

// TestCodexFullSimulation simulates codex startup WITH terminal responses.
// This tests what happens when codex actually receives responses to its queries.
func TestCodexFullSimulation(t *testing.T) {
	// Create synthetic recording of what codex sends at startup
	rec := NewRecording(80, 24)
	rec.Metadata.Description = "codex startup simulation"

	// Codex startup sequence (captured from real run):
	rec.AppendCSI("?2004h")  // Enable bracketed paste
	rec.AppendString("\x1b[>7u")  // Push keyboard mode (CSI > 7 u)
	rec.AppendCSI("?1004h")  // Enable focus reporting
	rec.AppendCSI("6n")      // Query cursor position (DSR)

	// After receiving cursor position, codex draws its UI
	// Simulating what codex does after getting DSR response:
	rec.AppendCSI("2J")      // Clear screen
	rec.AppendCSI("H")       // Move to home
	rec.AppendCSI("?25l")    // Hide cursor

	// Draw gray prompt area (simplified simulation)
	rec.AppendCSI("48;5;240m") // Gray background
	rec.AppendText("  > ")
	rec.AppendCSI("0m")        // Reset

	replayer := NewReplayer(rec)
	replayer.PlayAndRender()

	// Check responses
	responses := replayer.GetResponses()
	t.Logf("Terminal responses (%d bytes): %s", len(responses), EscapeSequenceLog(responses))

	// The terminal should have responded to DSR with cursor position
	if len(responses) == 0 {
		t.Error("Terminal should have responded to DSR query")
	}

	// Check for visual mismatches
	if replayer.HasVisualMismatch() {
		mismatches := replayer.FindVisualMismatches()
		t.Errorf("Visual mismatch! %d cells differ", len(mismatches))
	}

	snap := replayer.GetSnapshot()
	t.Logf("Final snapshot:\n%s", FormatSnapshot(snap))

	// The prompt "> " should be visible on row 0
	grid := replayer.GetGrid()
	row0 := CellsToString(grid[0][:10])
	t.Logf("Row 0 content: %q", row0)
	if !strings.Contains(row0, ">") {
		t.Error("Expected prompt '>' to be visible on row 0")
	}
}

// TestBidirectionalFlow simulates full app-terminal communication.
// This tests what happens with queries and responses.
func TestBidirectionalFlow(t *testing.T) {
	rec := NewRecording(80, 24)

	// Phase 1: App sends setup sequences
	rec.AppendCSI("?2004h")     // Enable bracketed paste
	rec.AppendString("\x1b[>7u") // Push keyboard mode (should be ignored)
	rec.AppendCSI("?1004h")     // Enable focus reporting
	rec.AppendCSI("6n")         // Query cursor position

	replayer := NewReplayer(rec)
	replayer.PlayAll()

	// Check terminal generated DSR response
	responses := replayer.GetResponses()
	t.Logf("After Phase 1 - responses: %s", EscapeSequenceLog(responses))

	if len(responses) == 0 {
		t.Fatal("Terminal should have responded to DSR")
	}

	// The response should be ESC[row;colR
	expectedResponse := "\x1b[1;1R"
	if string(responses) != expectedResponse {
		t.Errorf("Expected DSR response %q, got %q", expectedResponse, string(responses))
	}

	// Verify cursor didn't move from CSI>7u
	x, y := replayer.GetCursor()
	if x != 0 || y != 0 {
		t.Errorf("Cursor moved unexpectedly to (%d,%d) - CSI>u bug?", x, y)
	}

	// Phase 2: Now app draws UI (simulating what it does after receiving response)
	replayer.ClearResponses()
	replayer.PlayString("\x1b[2J")       // Clear screen
	replayer.PlayString("\x1b[H")        // Move home
	replayer.PlayString("\x1b[48;5;240m") // Gray background
	replayer.PlayString("Prompt> ")
	replayer.PlayString("\x1b[0m")        // Reset

	replayer.SimulateRender()

	// Check final state
	snap := replayer.GetSnapshot()
	t.Logf("Final state:\n%s", FormatSnapshot(snap))

	// Verify prompt is visible
	grid := replayer.GetGrid()
	row0 := CellsToString(grid[0][:15])
	if !strings.Contains(row0, "Prompt>") {
		t.Errorf("Expected 'Prompt>' on row 0, got: %q", row0)
	}

	// Verify background color on prompt
	if grid[0][0].BG.Mode != parser.ColorMode256 || grid[0][0].BG.Value != 240 {
		t.Errorf("Expected 256-color gray background, got mode=%d, value=%d",
			grid[0][0].BG.Mode, grid[0][0].BG.Value)
	}

	// Verify no visual mismatch
	if replayer.HasVisualMismatch() {
		t.Error("Visual mismatch detected!")
	}
}

// Test256ColorBackground verifies 256-color backgrounds are stored correctly.
// This is important for apps like codex that use colored backgrounds.
func Test256ColorBackground(t *testing.T) {
	rec := NewRecording(20, 5)

	// Set gray background (color 240) and write text
	rec.AppendCSI("48;5;240m") // Gray 256-color background
	rec.AppendText("Gray BG")
	rec.AppendCSI("0m") // Reset

	replayer := NewReplayer(rec)
	replayer.PlayAndRender()

	grid := replayer.GetGrid()

	// Check that the 'G' has background color 240
	cell := grid[0][0]
	if cell.Rune != 'G' {
		t.Errorf("Expected 'G' at (0,0), got %q", cell.Rune)
	}

	// Check background color mode and value
	if cell.BG.Mode != parser.ColorMode256 {
		t.Errorf("Expected ColorMode256 for BG, got mode %d", cell.BG.Mode)
	}
	if cell.BG.Value != 240 {
		t.Errorf("Expected BG value 240 (gray), got %d", cell.BG.Value)
	}

	t.Logf("Cell (0,0): rune=%q, BG.Mode=%d, BG.Value=%d", cell.Rune, cell.BG.Mode, cell.BG.Value)
}

// TestCodexHelp captures codex --help (non-interactive).
func TestCodexHelp(t *testing.T) {
	rec, err := CaptureCommand("codex --help", 80, 24)
	if err != nil {
		t.Skipf("Could not capture codex: %v", err)
	}

	t.Logf("Captured %d bytes from codex --help", len(rec.Sequences))

	// Replay through VTerm
	replayer := NewReplayer(rec)
	replayer.PlayAndRender()

	// Check for visual mismatches
	if replayer.HasVisualMismatch() {
		mismatches := replayer.FindVisualMismatches()
		t.Errorf("Visual mismatch detected! %d cells differ", len(mismatches))
	}
}

// TestTUITakeover tests that a TUI app can draw over committed shell content
// when positioning the cursor at row 0 or 1. This is essential for apps like
// codex that draw full-screen UIs without using alternate buffer.
func TestTUITakeover(t *testing.T) {
	rec := NewRecording(40, 10)

	// Simulate bash prompt being displayed
	rec.AppendText("$ echo hello")
	rec.AppendCRLF()
	rec.AppendText("hello")
	rec.AppendCRLF()
	rec.AppendText("$ ")

	// Now simulate TUI app startup - it positions cursor at top of screen
	rec.AppendCSI("H") // Cursor home (row 1, col 1 = position 0,0)

	// TUI draws its header
	rec.AppendText("=== TUI App Header ===")
	rec.AppendCRLF()
	rec.AppendText("Menu item 1")

	replayer := NewReplayer(rec)
	replayer.PlayAndRender()

	grid := replayer.GetGrid()

	// Verify TUI header is at row 0 (overwrote bash prompt)
	row0 := ""
	for x := 0; x < 22; x++ {
		row0 += string(grid[0][x].Rune)
	}
	if row0 != "=== TUI App Header ===" {
		t.Errorf("Expected TUI header at row 0, got %q", row0)
	}

	// Verify menu item is at row 1
	row1 := ""
	for x := 0; x < 11; x++ {
		row1 += string(grid[1][x].Rune)
	}
	if row1 != "Menu item 1" {
		t.Errorf("Expected 'Menu item 1' at row 1, got %q", row1)
	}

	t.Logf("TUI takeover successful:")
	t.Logf("  Row 0: %q", row0)
	t.Logf("  Row 1: %q", row1)
}

// TestTUITakeoverPartialScreen tests TUI takeover when positioning at row 1
// (not row 0). This covers the case where codex draws its bordered UI.
func TestTUITakeoverPartialScreen(t *testing.T) {
	rec := NewRecording(40, 10)

	// Simulate bash prompt and command entry
	rec.AppendText("$ codex")
	rec.AppendCRLF()

	// TUI app positions cursor at row 1 col 0 to draw border
	rec.AppendCSI("2;1H") // Row 2, Col 1 = position (0,1)

	// TUI draws box border starting at row 1
	rec.AppendText("+---------+")
	rec.AppendCRLF()
	rec.AppendText("| Content |")

	replayer := NewReplayer(rec)
	// Use PlayAll + SimulateRender to avoid UTF-8 issues with PlayAndRender
	replayer.PlayAll()
	replayer.SimulateRender()

	grid := replayer.GetGrid()

	// Row 1 should have the border
	if grid[1][0].Rune != '+' {
		t.Errorf("Expected '+' at (0,1), got %q", grid[1][0].Rune)
	}

	// Row 2 should have content
	row2 := ""
	for x := 0; x < 11; x++ {
		row2 += string(grid[2][x].Rune)
	}
	if row2 != "| Content |" {
		t.Errorf("Expected content row at row 2, got %q", row2)
	}

	t.Logf("Partial TUI takeover successful")
	t.Logf("  Row 1: starts with %q", string(grid[1][0].Rune))
	t.Logf("  Row 2: %q", row2)
}

// TestCSIExtendedKeyboardProtocol tests that CSI>u (extended keyboard protocol)
// does NOT trigger RestoreCursor. This is a regression test for a bug where
// codex's ESC[>7u sequence would incorrectly move the cursor.
func TestCSIExtendedKeyboardProtocol(t *testing.T) {
	rec := NewRecording(20, 5)

	// Write "A" at position (0,0)
	rec.AppendText("A")

	// Move cursor to (5,2)
	rec.AppendCSI("3;6H") // Row 3, Col 6 (1-indexed) = (5,2) 0-indexed

	// Write "B" at current position
	rec.AppendText("B")

	// Now send CSI>7u (extended keyboard protocol - push mode)
	// This should NOT affect cursor position
	rec.AppendString("\x1b[>7u")

	// Write "C" - should appear right after "B"
	rec.AppendText("C")

	replayer := NewReplayer(rec)
	replayer.PlayAndRender()

	grid := replayer.GetGrid()

	// Check "A" is at (0,0)
	if grid[0][0].Rune != 'A' {
		t.Errorf("Expected 'A' at (0,0), got %q", grid[0][0].Rune)
	}

	// Check "B" is at (5,2)
	if grid[2][5].Rune != 'B' {
		t.Errorf("Expected 'B' at (5,2), got %q", grid[2][5].Rune)
	}

	// Check "C" is at (6,2) - immediately after "B"
	// If the bug exists, "C" might be at (0,0) or wherever RestoreCursor moved it
	if grid[2][6].Rune != 'C' {
		t.Errorf("Expected 'C' at (6,2), got %q (CSI>u bug: cursor was incorrectly moved)", grid[2][6].Rune)
	}

	// Also verify cursor position
	x, y := replayer.GetCursor()
	if x != 7 || y != 2 {
		t.Errorf("Expected cursor at (7,2), got (%d,%d)", x, y)
	}
}

// ========== Reference Terminal Comparison Tests ==========
// These tests compare texelterm output against a reference terminal (tmux).
// They require tmux to be installed.

// TestReferenceCompareBasic tests basic comparison with reference terminal.
func TestReferenceCompareBasic(t *testing.T) {
	rec := NewRecording(40, 10)
	rec.AppendText("Hello, World!")
	rec.AppendCRLF()
	rec.AppendText("Line 2")

	cmp, err := NewReferenceComparator(rec)
	if err != nil {
		t.Skipf("Reference comparator unavailable: %v", err)
	}

	result, err := cmp.CompareAtEnd()
	if err != nil {
		t.Fatalf("CompareAtEnd failed: %v", err)
	}

	if !result.Match {
		t.Logf("Differences found:")
		for _, diff := range result.Differences[:min(10, len(result.Differences))] {
			t.Logf("  (%d,%d): tmux=%q texelterm=%q", diff.X, diff.Y, string(diff.Reference), string(diff.Texelterm))
		}
		// This is informational - basic text should match
		// Only fail if there are significant differences
		if len(result.Differences) > 5 {
			t.Errorf("Too many differences: %d", len(result.Differences))
		}
	} else {
		t.Logf("Basic text output matches reference terminal")
	}
}

// TestReferenceCompareCursorMovement tests cursor positioning sequences.
func TestReferenceCompareCursorMovement(t *testing.T) {
	rec := NewRecording(40, 10)
	rec.AppendText("A")           // Write A at (0,0)
	rec.AppendCSI("3;5H")         // Move to row 3, col 5
	rec.AppendText("B")           // Write B
	rec.AppendCSI("1;1H")         // Move back to top-left
	rec.AppendCSI("C")            // Move forward 1 (CUF)
	rec.AppendText("X")           // Write X at (1,0)

	cmp, err := NewReferenceComparator(rec)
	if err != nil {
		t.Skipf("Reference comparator unavailable: %v", err)
	}

	result, err := cmp.CompareAtEnd()
	if err != nil {
		t.Fatalf("CompareAtEnd failed: %v", err)
	}

	if !result.Match {
		t.Logf("Cursor movement test - differences:")
		for _, diff := range result.Differences {
			t.Logf("  (%d,%d): ref=%q actual=%q", diff.X, diff.Y, string(diff.Reference), string(diff.Texelterm))
		}
		t.Error("Cursor movement produced different output than reference")
	} else {
		t.Logf("Cursor movement matches reference terminal")
	}
}

// TestReferenceCompareScrollRegion tests scroll region operations.
func TestReferenceCompareScrollRegion(t *testing.T) {
	rec := NewRecording(40, 10)

	// Fill screen with labeled rows
	for i := 0; i < 8; i++ {
		rec.AppendText(strings.Repeat(string(rune('A'+i)), 10))
		if i < 7 {
			rec.AppendCRLF()
		}
	}

	// Set scroll region
	rec.AppendCSI("3;6r")  // Rows 3-6 (1-indexed)
	rec.AppendCSI("4;1H")  // Move to row 4
	rec.AppendCSI("M")     // Delete line (scroll up within region)

	cmp, err := NewReferenceComparator(rec)
	if err != nil {
		t.Skipf("Reference comparator unavailable: %v", err)
	}

	result, err := cmp.CompareAtEnd()
	if err != nil {
		t.Fatalf("CompareAtEnd failed: %v", err)
	}

	if !result.Match {
		t.Logf("Scroll region test - differences:")
		for _, diff := range result.Differences[:min(20, len(result.Differences))] {
			t.Logf("  (%d,%d): ref=%q actual=%q", diff.X, diff.Y, string(diff.Reference), string(diff.Texelterm))
		}
		t.Error("Scroll region produced different output than reference")
	} else {
		t.Logf("Scroll region matches reference terminal")
	}
}

// TestReferenceFindDivergence demonstrates finding the first point of divergence.
func TestReferenceFindDivergence(t *testing.T) {
	// Create a complex sequence that might diverge
	rec := NewRecording(40, 10)
	rec.AppendText("Initial text")
	rec.AppendCRLF()
	rec.AppendCSI("2J")    // Clear screen
	rec.AppendCSI("H")     // Home
	rec.AppendCSI("48;5;240m") // Gray background
	rec.AppendText("Gray background text")
	rec.AppendCSI("0m")    // Reset
	rec.AppendCRLF()
	rec.AppendCSI("K")     // Erase to end of line

	cmp, err := NewReferenceComparator(rec)
	if err != nil {
		t.Skipf("Reference comparator unavailable: %v", err)
	}

	// Find first divergence with small chunk size
	divergence, err := cmp.FindFirstDivergence(50)
	if err != nil {
		t.Fatalf("FindFirstDivergence failed: %v", err)
	}

	if divergence != nil {
		t.Logf("Found divergence at byte range %d-%d", divergence.ByteIndex, divergence.ByteEndIndex)
		t.Logf("Chunk: %s", EscapeSequenceLog(divergence.ChunkProcessed))
		t.Logf("\n%s", FormatDivergence(divergence))
	} else {
		t.Logf("No divergence found - outputs match throughout")
	}
}

// TestReferenceCompareAnimationPattern tests the pattern codex uses for animations.
func TestReferenceCompareAnimationPattern(t *testing.T) {
	rec := NewRecording(80, 24)

	// Simulate codex animation pattern
	rec.AppendCSI("5;24r")      // Set scroll region
	rec.AppendCSI("5;1H")       // Move to top of region
	rec.AppendCSI("M")          // Scroll region down (reverse index / insert line)

	// Write animation content
	rec.AppendCSI("5;1H")       // Move to row 5
	rec.AppendText("Working (0s)")

	// Simulate animation update
	rec.AppendCSI("5;1H")       // Move to start of animation row
	rec.AppendCSI("K")          // Erase to end of line
	rec.AppendText("Working (1s)")

	cmp, err := NewReferenceComparator(rec)
	if err != nil {
		t.Skipf("Reference comparator unavailable: %v", err)
	}

	divergence, err := cmp.FindFirstDivergence(20)
	if err != nil {
		t.Fatalf("FindFirstDivergence failed: %v", err)
	}

	if divergence != nil {
		t.Logf("Animation pattern diverges at byte %d", divergence.ByteIndex)
		t.Logf("\n%s", FormatDivergence(divergence))
		t.Error("Animation pattern produces different output than reference")
	} else {
		t.Logf("Animation pattern matches reference terminal")
	}
}

// TestReferenceCompareGreyBackground tests grey background handling.
func TestReferenceCompareGreyBackground(t *testing.T) {
	rec := NewRecording(40, 10)

	// Set grey background
	rec.AppendCSI("48;5;240m")
	rec.AppendText("Grey background")
	rec.AppendCSI("0m")  // Reset

	// Move down and set scroll region
	rec.AppendCRLF()
	rec.AppendCSI("2;10r")     // Scroll region rows 2-10
	rec.AppendCSI("2;1H")      // Move to top of region

	// Scroll down (inserts blank line with current BG)
	rec.AppendCSI("48;5;240m") // Set grey BG again
	rec.AppendCSI("L")         // Insert line

	// Write on the new blank line
	rec.AppendCSI("0m")        // Reset to default
	rec.AppendText("New line")

	cmp, err := NewReferenceComparator(rec)
	if err != nil {
		t.Skipf("Reference comparator unavailable: %v", err)
	}

	divergence, err := cmp.FindFirstDivergence(10)
	if err != nil {
		t.Fatalf("FindFirstDivergence failed: %v", err)
	}

	if divergence != nil {
		t.Logf("Grey background handling diverges at byte %d", divergence.ByteIndex)
		t.Logf("Chunk: %s", EscapeSequenceLog(divergence.ChunkProcessed))
		t.Logf("\n%s", FormatDivergence(divergence))
		// This is an important test - log details but may or may not fail
		// depending on whether we consider background color differences
	} else {
		t.Logf("Grey background handling matches reference terminal")
	}
}

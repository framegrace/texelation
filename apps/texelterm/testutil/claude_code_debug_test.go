// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/testutil/claude_code_debug_test.go
// Summary: Debug test for Claude Code rendering differences between texelterm and tmux.
//
// Strategy: Capture Claude Code running DIRECTLY in a PTY (what texelterm sees)
// and Claude Code running INSIDE tmux in a PTY (what texelterm sees when tmux
// mediates). Compare the two VTerm-parsed grids to find exactly what escape
// sequences texelterm handles differently from tmux.

package testutil_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/apps/texelterm/testutil"
)

const (
	// Wide terminal to trigger the two-section layout (separate bottom status bar)
	// that appears at larger widths. The user's screenshot was ~250+ cols.
	claudeCodeWidth  = 250
	claudeCodeHeight = 25
	claudeCodeWait   = 15 * time.Second // Wait after trust prompt for welcome screen to render
)

// clearClaudeEnv temporarily unsets CLAUDECODE env var to allow nested capture.
// Claude Code refuses to launch inside another Claude Code session.
// Returns a cleanup function to restore the original value.
func clearClaudeEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"CLAUDECODE", "CLAUDE_CODE"} {
		if orig, ok := os.LookupEnv(key); ok {
			os.Unsetenv(key)
			t.Cleanup(func() { os.Setenv(key, orig) })
		}
	}
}

// captureClaudeCodeDirect captures Claude Code running directly in a PTY.
// Launches from HOME directory to reproduce the home-dir welcome screen layout
// (which has a separate bottom status bar with narrow right sections).
func captureClaudeCodeDirect(t *testing.T, width, height int, wait time.Duration) *testutil.Recording {
	t.Helper()
	clearClaudeEnv(t)
	home := os.Getenv("HOME")
	ic, err := testutil.NewInteractiveCapture("sh", []string{"-c", "cd " + home + " && claude"}, width, height)
	if err != nil {
		t.Fatalf("Failed to start direct Claude Code capture: %v", err)
	}
	// Claude Code may show a trust prompt for the home directory.
	// Accept it by pressing Enter, then wait for the welcome screen.
	if ic.WaitForOutput("trust", 15*time.Second) {
		t.Log("Trust prompt detected, sending Enter to accept")
		ic.SendEnter()
	}
	time.Sleep(wait)
	rec := ic.ToRecording()
	ic.Close()
	rec.Metadata.Description = "claude code direct from HOME (no tmux)"
	return rec
}

// captureClaudeCodeViaTmux captures Claude Code running inside tmux in a PTY.
// This is what texelterm sees when tmux mediates - the "correct" rendering.
// Uses a minimal tmux config and a dedicated server socket for isolation.
func captureClaudeCodeViaTmux(t *testing.T, width, height int, wait time.Duration) *testutil.Recording {
	t.Helper()
	clearClaudeEnv(t)

	tmpConf, err := os.CreateTemp("", "tmux-clean-*.conf")
	if err != nil {
		t.Fatalf("Failed to create temp tmux config: %v", err)
	}
	tmpConf.WriteString("set -g status off\nset -g default-terminal \"xterm-256color\"\n")
	tmpConf.Close()
	defer os.Remove(tmpConf.Name())

	home := os.Getenv("HOME")
	serverName := fmt.Sprintf("claude-test-%d", time.Now().UnixNano())
	ic, err := testutil.NewInteractiveCapture(
		"tmux", []string{
			"-L", serverName,
			"-f", tmpConf.Name(),
			"new-session",
			"-x", fmt.Sprintf("%d", width),
			"-y", fmt.Sprintf("%d", height),
			"-c", home, // Start in HOME directory
			"claude",
		},
		width, height,
	)
	if err != nil {
		t.Fatalf("Failed to start tmux+claude capture: %v", err)
	}
	// Handle trust prompt if it appears
	if ic.WaitForOutput("trust", 15*time.Second) {
		t.Log("Trust prompt detected in tmux, sending Enter to accept")
		ic.SendEnter()
	}
	time.Sleep(wait)
	rec := ic.ToRecording()
	ic.Close()

	exec.Command("tmux", "-L", serverName, "kill-server").Run()

	rec.Metadata.Description = "claude code via tmux from HOME (reference)"
	return rec
}

// TestClaudeCodeDirectVsTmux is the main debugging test.
// It captures Claude Code both directly and via tmux, replays both through VTerm,
// and compares the grids to find rendering differences.
//
// Run: go test -v -run TestClaudeCodeDirectVsTmux -timeout 60s ./apps/texelterm/testutil/
func TestClaudeCodeDirectVsTmux(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not found in PATH")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not found in PATH")
	}

	// === Step 1: Capture both ===
	t.Log("=== Capturing Claude Code DIRECTLY (what texelterm sees) ===")
	directRec := captureClaudeCodeDirect(t, claudeCodeWidth, claudeCodeHeight, claudeCodeWait)
	t.Logf("Direct capture: %d bytes", len(directRec.Sequences))

	t.Log("\n=== Capturing Claude Code VIA TMUX (correct rendering) ===")
	tmuxRec := captureClaudeCodeViaTmux(t, claudeCodeWidth, claudeCodeHeight, claudeCodeWait)
	t.Logf("Tmux capture: %d bytes", len(tmuxRec.Sequences))

	// Save both recordings
	directRec.Save("/tmp/claude-code-direct.txrec")
	tmuxRec.Save("/tmp/claude-code-tmux.txrec")
	t.Log("Recordings saved to /tmp/claude-code-direct.txrec and /tmp/claude-code-tmux.txrec")

	// === Step 2: Replay both through VTerm ===
	t.Log("\n=== Replaying both through VTerm parser ===")

	directReplay := testutil.NewReplayer(directRec)
	directReplay.PlayAll()
	directReplay.SimulateRender()
	directGrid := directReplay.Grid()
	directCurX, directCurY := directReplay.Cursor()

	tmuxReplay := testutil.NewReplayer(tmuxRec)
	tmuxReplay.PlayAll()
	tmuxReplay.SimulateRender()
	tmuxGrid := tmuxReplay.Grid()
	tmuxCurX, tmuxCurY := tmuxReplay.Cursor()

	t.Logf("Direct cursor: (%d,%d)", directCurX, directCurY)
	t.Logf("Tmux cursor:   (%d,%d)", tmuxCurX, tmuxCurY)

	// === Step 3: Show both grids ===
	t.Log("\n=== DIRECT Grid (what texelterm renders from Claude Code) ===")
	t.Log(testutil.GridToStringWithCursor(directGrid, directCurX, directCurY))

	t.Log("\n=== TMUX Grid (what texelterm renders from tmux+Claude Code) ===")
	t.Log(testutil.GridToStringWithCursor(tmuxGrid, tmuxCurX, tmuxCurY))

	// === Step 4: Compare grids with full color/attr support ===
	t.Log("\n=== Grid Comparison (tmux=reference, direct=actual) ===")
	result := testutil.EnhancedCompareGrids(tmuxGrid, directGrid, claudeCodeWidth, claudeCodeHeight)
	result.BytesProcessed = len(directRec.Sequences)

	t.Logf("Match: %v", result.Match)
	t.Logf("Summary: %s", result.Summary)

	if result.Match {
		t.Log("Both grids match! VTerm produces identical output for direct and tmux-mediated Claude Code.")
		return
	}

	// Detailed analysis
	t.Log(testutil.FormatEnhancedResult(result))

	// Show diffs with full cell info
	maxDiffs := 50
	if len(result.Differences) < maxDiffs {
		maxDiffs = len(result.Differences)
	}
	for i := 0; i < maxDiffs; i++ {
		d := result.Differences[i]
		t.Logf("(%3d,%2d) [%-8s] %s", d.X, d.Y, d.DiffType, d.DiffDesc)
	}
	if len(result.Differences) > maxDiffs {
		t.Logf("... and %d more differences", len(result.Differences)-maxDiffs)
	}

	// Row summary
	rowDiffs := map[int]int{}
	for _, d := range result.Differences {
		rowDiffs[d.Y]++
	}
	t.Log("\n=== Rows with differences ===")
	for y := range claudeCodeHeight {
		if count, ok := rowDiffs[y]; ok {
			t.Logf("  Row %2d: %d diffs", y, count)
		}
	}

	// === Step 5: Show escape sequence differences ===
	t.Log("\n=== Escape Sequence Comparison ===")
	t.Logf("Direct sequences (first 3000 bytes):\n%s",
		testutil.EscapeSequenceLog(directRec.Sequences[:min(3000, len(directRec.Sequences))]))
	t.Logf("\nTmux sequences (first 3000 bytes):\n%s",
		testutil.EscapeSequenceLog(tmuxRec.Sequences[:min(3000, len(tmuxRec.Sequences))]))

	// === Step 6: Check dirty tracking for direct recording ===
	t.Log("\n=== Dirty Tracking Analysis (direct) ===")
	if directReplay.HasVisualMismatch() {
		mismatches := directReplay.FindVisualMismatches()
		t.Logf("DIRTY TRACKING MISMATCHES: %d", len(mismatches))
		for i, m := range mismatches {
			if i >= 20 {
				t.Logf("... and %d more", len(mismatches)-20)
				break
			}
			t.Logf("  (%d,%d): rendered=%q logical=%q", m.X, m.Y,
				string(m.Rendered.Rune), string(m.Logical.Rune))
		}
	} else {
		t.Log("No dirty tracking mismatches in direct recording")
	}

	// Color detail for rows with diffs
	t.Log("\n=== Color Detail on Differing Rows ===")
	for y := range rowDiffs {
		if y < len(directGrid) && y < len(tmuxGrid) {
			t.Logf("Row %2d (direct): %s", y, testutil.FormatGridRowWithColors(directGrid[y], claudeCodeWidth))
			t.Logf("Row %2d (tmux):   %s", y, testutil.FormatGridRowWithColors(tmuxGrid[y], claudeCodeWidth))
		}
	}

	// Save JSON
	if jsonBytes, err := result.ToJSONPretty(); err == nil {
		os.WriteFile("/tmp/claude-code-direct-vs-tmux.json", jsonBytes, 0644)
		t.Log("\nJSON saved to /tmp/claude-code-direct-vs-tmux.json")
	}
}

// TestClaudeCodeSavedCompare loads saved recordings and re-compares.
// Useful for iterating on parser fixes without re-capturing.
//
// Run: go test -v -run TestClaudeCodeSavedCompare -timeout 10s ./apps/texelterm/testutil/
func TestClaudeCodeSavedCompare(t *testing.T) {
	directRec, err := testutil.LoadRecording("/tmp/claude-code-direct.txrec")
	if err != nil {
		t.Skipf("No direct recording: %v (run TestClaudeCodeDirectVsTmux first)", err)
	}
	tmuxRec, err := testutil.LoadRecording("/tmp/claude-code-tmux.txrec")
	if err != nil {
		t.Skipf("No tmux recording: %v (run TestClaudeCodeDirectVsTmux first)", err)
	}

	directReplay := testutil.NewReplayer(directRec)
	directReplay.PlayAll()
	directReplay.SimulateRender()

	tmuxReplay := testutil.NewReplayer(tmuxRec)
	tmuxReplay.PlayAll()
	tmuxReplay.SimulateRender()

	result := testutil.EnhancedCompareGrids(
		tmuxReplay.Grid(), directReplay.Grid(),
		directRec.Metadata.Width, directRec.Metadata.Height,
	)

	// Claude Code randomizes some content each session, so rows with character
	// differences are expected. We only care about rows where the TEXT matches
	// but rendering (colors/attributes) differs, since that indicates a real
	// VTerm parser bug.
	rowsWithCharDiffs := map[int]bool{}
	for _, d := range result.Differences {
		if d.DiffType == testutil.DiffTypeChar || d.DiffType == testutil.DiffTypeCombined {
			rowsWithCharDiffs[d.Y] = true
		}
	}

	var significant []testutil.EnhancedDiff
	for _, d := range result.Differences {
		if rowsWithCharDiffs[d.Y] {
			continue
		}
		significant = append(significant, d)
	}

	t.Logf("Match: %v (total diffs: %d, significant: %d, skipped %d rows with char diffs)",
		result.Match, result.DiffCount, len(significant), len(rowsWithCharDiffs))

	if len(significant) > 0 {
		for _, d := range significant {
			t.Logf("  (%d,%d) [%s] %s", d.X, d.Y, d.DiffType, d.DiffDesc)
		}
		t.Errorf("Significant visual differences: %d (color/attr diffs on rows with matching text)", len(significant))
	}
}

// TestClaudeCodeGridDump dumps the VTerm grid from saved recordings for visual inspection.
//
// Run: go test -v -run TestClaudeCodeGridDump -timeout 10s ./apps/texelterm/testutil/
func TestClaudeCodeGridDump(t *testing.T) {
	for _, name := range []string{"direct", "tmux"} {
		path := fmt.Sprintf("/tmp/claude-code-%s.txrec", name)
		rec, err := testutil.LoadRecording(path)
		if err != nil {
			t.Logf("Skipping %s: %v", name, err)
			continue
		}

		replayer := testutil.NewReplayer(rec)
		replayer.PlayAll()
		replayer.SimulateRender()

		grid := replayer.Grid()
		x, y := replayer.Cursor()

		t.Logf("\n=== %s Grid (%dx%d, %d bytes) ===", name,
			rec.Metadata.Width, rec.Metadata.Height, len(rec.Sequences))
		t.Log(testutil.GridToStringWithCursor(grid, x, y))

		// Row color summary for content rows
		for row := range grid {
			hasContent := false
			for col := range grid[row] {
				if grid[row][col].Rune != 0 && grid[row][col].Rune != ' ' {
					hasContent = true
					break
				}
			}
			if hasContent {
				t.Logf("Row %2d: %s", row, testutil.FormatGridRowWithColors(grid[row], rec.Metadata.Width))
			}
		}
	}
}

// TestClaudeCodeStatusBarInspection performs focused inspection of the bottom
// rows of the Claude Code UI. Compares direct vs tmux cell by cell, showing
// Unicode codepoints and BG colors for every differing cell. This targets
// the specific rendering bug area (extra box-drawing characters in status bar).
//
// Run: go test -v -run TestClaudeCodeStatusBarInspection -timeout 10s ./apps/texelterm/testutil/
func TestClaudeCodeStatusBarInspection(t *testing.T) {
	directRec, err := testutil.LoadRecording("/tmp/claude-code-direct.txrec")
	if err != nil {
		t.Skipf("No direct recording: %v (run TestClaudeCodeDirectVsTmux first)", err)
	}
	tmuxRec, err := testutil.LoadRecording("/tmp/claude-code-tmux.txrec")
	if err != nil {
		t.Skipf("No tmux recording: %v (run TestClaudeCodeDirectVsTmux first)", err)
	}

	directReplay := testutil.NewReplayer(directRec)
	directReplay.PlayAll()
	directReplay.SimulateRender()
	directGrid := directReplay.Grid()

	tmuxReplay := testutil.NewReplayer(tmuxRec)
	tmuxReplay.PlayAll()
	tmuxReplay.SimulateRender()
	tmuxGrid := tmuxReplay.Grid()

	height := directRec.Metadata.Height
	width := directRec.Metadata.Width

	// Inspect bottom 15 rows (status bar area)
	startRow := max(height-15, 0)

	t.Logf("=== Status Bar Inspection (rows %d-%d) ===", startRow, height-1)

	for row := startRow; row < height; row++ {
		if row >= len(directGrid) || row >= len(tmuxGrid) {
			break
		}

		// Build text representations for both
		var directText, tmuxText strings.Builder
		for col := range width {
			dr := directGrid[row][col].Rune
			tr := tmuxGrid[row][col].Rune
			if dr == 0 {
				dr = ' '
			}
			if tr == 0 {
				tr = ' '
			}
			directText.WriteRune(dr)
			tmuxText.WriteRune(tr)
		}

		t.Logf("\nRow %2d direct: %s", row, strings.TrimRight(directText.String(), " "))
		t.Logf("Row %2d tmux:   %s", row, strings.TrimRight(tmuxText.String(), " "))

		// Cell-by-cell comparison for this row
		diffCount := 0
		for col := range width {
			if col >= len(directGrid[row]) || col >= len(tmuxGrid[row]) {
				break
			}
			dc := directGrid[row][col]
			tc := tmuxGrid[row][col]

			dr := dc.Rune
			tr := tc.Rune
			if dr == 0 {
				dr = ' '
			}
			if tr == 0 {
				tr = ' '
			}

			charDiff := dr != tr
			bgDiff := dc.BG != tc.BG
			fgDiff := dc.FG != tc.FG
			attrDiff := dc.Attr != tc.Attr

			if charDiff || bgDiff || fgDiff || attrDiff {
				diffCount++
				var parts []string
				if charDiff {
					parts = append(parts, fmt.Sprintf("char: U+%04X %q vs U+%04X %q",
						dr, string(dr), tr, string(tr)))
				}
				if bgDiff {
					parts = append(parts, fmt.Sprintf("bg: %s vs %s",
						formatColor(dc.BG), formatColor(tc.BG)))
				}
				if fgDiff {
					parts = append(parts, fmt.Sprintf("fg: %s vs %s",
						formatColor(dc.FG), formatColor(tc.FG)))
				}
				if attrDiff {
					parts = append(parts, fmt.Sprintf("attr: %s vs %s",
						dc.Attr.String(), tc.Attr.String()))
				}
				t.Logf("  col %3d: %s", col, strings.Join(parts, "; "))
			}
		}
		if diffCount > 0 {
			t.Logf("  -> %d differing cells on row %d", diffCount, row)
		}
	}
}

// TestClaudeCodeFrameByFrame steps through the direct capture frame by frame,
// splitting on ESC[?2026l (synchronized output end) boundaries. Dumps the grid
// after each frame, focusing on frames that contain box-drawing characters
// (U+2500-U+257F range) which are relevant to the status bar rendering bug.
//
// Run: go test -v -run TestClaudeCodeFrameByFrame -timeout 30s ./apps/texelterm/testutil/
func TestClaudeCodeFrameByFrame(t *testing.T) {
	directRec, err := testutil.LoadRecording("/tmp/claude-code-direct.txrec")
	if err != nil {
		t.Skipf("No direct recording: %v (run TestClaudeCodeDirectVsTmux first)", err)
	}

	// Split on ESC[?2026l (end of synchronized update)
	endSync := []byte("\x1b[?2026l")
	frames := bytes.Split(directRec.Sequences, endSync)

	t.Logf("Total frames (sync boundaries): %d", len(frames))
	t.Logf("Total bytes: %d", len(directRec.Sequences))

	// Create a single replayer and feed frames sequentially
	replayer := testutil.NewReplayer(directRec)

	width := directRec.Metadata.Width
	height := directRec.Metadata.Height
	byteOffset := 0

	for frameIdx, frame := range frames {
		// Feed this frame through the parser
		replayer.PlayString(string(frame))
		// Also feed the sync boundary (except for the last fragment)
		if frameIdx < len(frames)-1 {
			replayer.PlayString(string(endSync))
		}

		replayer.SimulateRender()
		grid := replayer.Grid()

		// Check if this frame's grid contains box-drawing characters
		hasBoxDrawing := false
		boxDrawingRows := map[int]bool{}
		for y := 0; y < height && y < len(grid); y++ {
			for x := 0; x < width && x < len(grid[y]); x++ {
				r := grid[y][x].Rune
				if r >= 0x2500 && r <= 0x257F {
					hasBoxDrawing = true
					boxDrawingRows[y] = true
				}
			}
		}

		frameEnd := byteOffset + len(frame)
		if frameIdx < len(frames)-1 {
			frameEnd += len(endSync)
		}

		if hasBoxDrawing {
			t.Logf("\n=== Frame %d (bytes %d-%d, %d bytes) - HAS BOX DRAWING ===",
				frameIdx, byteOffset, frameEnd, len(frame))

			// Show rows that contain box-drawing characters
			for y := 0; y < height && y < len(grid); y++ {
				if !boxDrawingRows[y] {
					continue
				}
				var rowText strings.Builder
				for x := 0; x < width && x < len(grid[y]); x++ {
					r := grid[y][x].Rune
					if r == 0 {
						r = ' '
					}
					rowText.WriteRune(r)
				}
				t.Logf("  Row %2d: %s", y, strings.TrimRight(rowText.String(), " "))
				t.Logf("          %s", testutil.FormatGridRowWithColors(grid[y], width))

				// Show box-drawing character details
				for x := 0; x < width && x < len(grid[y]); x++ {
					r := grid[y][x].Rune
					if r >= 0x2500 && r <= 0x257F {
						cell := grid[y][x]
						t.Logf("          col %3d: U+%04X %q bg=%s fg=%s attr=%s",
							x, r, string(r),
							formatColor(cell.BG), formatColor(cell.FG),
							cell.Attr.String())
					}
				}
			}

			// Also show escape sequence excerpt for this frame
			excerptLen := len(frame)
			if excerptLen > 500 {
				excerptLen = 500
			}
			t.Logf("  Sequences (first %d bytes): %s",
				excerptLen, testutil.EscapeSequenceLog(frame[:excerptLen]))
		} else if frameIdx < 5 || frameIdx == len(frames)-1 {
			// Always show the first few frames and the last one
			t.Logf("\n=== Frame %d (bytes %d-%d, %d bytes) - no box drawing ===",
				frameIdx, byteOffset, frameEnd, len(frame))
		}

		byteOffset = frameEnd
	}
}

// formatColor formats a parser.Color for display.
func formatColor(c parser.Color) string {
	switch c.Mode {
	case parser.ColorModeDefault:
		return "default"
	case parser.ColorModeStandard:
		return fmt.Sprintf("std(%d)", c.Value)
	case parser.ColorMode256:
		return fmt.Sprintf("256(%d)", c.Value)
	case parser.ColorModeRGB:
		return fmt.Sprintf("rgb(%d,%d,%d)", c.R, c.G, c.B)
	default:
		return "unknown"
	}
}

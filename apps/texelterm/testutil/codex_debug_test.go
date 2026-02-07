// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/testutil/codex_debug_test.go
// Summary: Debug test for codex rendering differences between texelterm and tmux.
//
// Strategy: Capture codex running DIRECTLY in a PTY (what texelterm sees) and
// codex running INSIDE tmux in a PTY (what texelterm sees when tmux mediates).
// Compare the two VTerm-parsed grids to find exactly what escape sequences
// texelterm handles differently from tmux.

package testutil_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/apps/texelterm/testutil"
	"github.com/framegrace/texelation/internal/theming"
	"github.com/framegrace/texelui/theme"
	"github.com/gdamore/tcell/v2"
)

const (
	codexWidth  = 120
	codexHeight = 40
	codexWait   = 5 * time.Second
)

// captureCodexDirect captures codex running directly in a PTY.
// This is what texelterm sees when running codex directly.
func captureCodexDirect(t *testing.T, width, height int, wait time.Duration) *testutil.Recording {
	t.Helper()
	ic, err := testutil.NewInteractiveCapture("codex", nil, width, height)
	if err != nil {
		t.Fatalf("Failed to start direct codex capture: %v", err)
	}
	time.Sleep(wait)
	rec := ic.ToRecording()
	ic.Close()
	rec.Metadata.Description = "codex direct (no tmux)"
	return rec
}

// captureCodexViaTmux captures codex running inside tmux in a PTY.
// This is what texelterm sees when tmux mediates - the "correct" rendering.
// Uses an empty tmux config (-f /dev/null) and disables status bar to avoid
// interference from user tmux plugins/session restore.
func captureCodexViaTmux(t *testing.T, width, height int, wait time.Duration) *testutil.Recording {
	t.Helper()

	// Write a minimal tmux config that just disables the status bar
	tmpConf, err := os.CreateTemp("", "tmux-clean-*.conf")
	if err != nil {
		t.Fatalf("Failed to create temp tmux config: %v", err)
	}
	tmpConf.WriteString("set -g status off\nset -g default-terminal \"xterm-256color\"\n")
	tmpConf.Close()
	defer os.Remove(tmpConf.Name())

	// Run tmux with codex inside it, using a DEDICATED server socket (-L)
	// to ensure complete isolation from any running tmux server.
	// An existing tmux server ignores -f config, so we must use a separate server.
	serverName := fmt.Sprintf("codex-test-%d", time.Now().UnixNano())
	ic, err := testutil.NewInteractiveCapture(
		"tmux", []string{
			"-L", serverName,
			"-f", tmpConf.Name(),
			"new-session",
			"-x", fmt.Sprintf("%d", width),
			"-y", fmt.Sprintf("%d", height),
			"codex",
		},
		width, height,
	)
	if err != nil {
		t.Fatalf("Failed to start tmux+codex capture: %v", err)
	}
	time.Sleep(wait)
	rec := ic.ToRecording()
	ic.Close()

	// Clean up the dedicated tmux server
	exec.Command("tmux", "-L", serverName, "kill-server").Run()

	rec.Metadata.Description = "codex via tmux (reference)"
	return rec
}

// TestCodexDirectVsTmux is the main debugging test.
// It captures codex both directly and via tmux, replays both through VTerm,
// and compares the grids to find rendering differences.
//
// Run: go test -v -run TestCodexDirectVsTmux -timeout 30s ./apps/texelterm/testutil/
func TestCodexDirectVsTmux(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex not found in PATH")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not found in PATH")
	}

	// === Step 1: Capture both ===
	t.Log("=== Capturing codex DIRECTLY (what texelterm sees) ===")
	directRec := captureCodexDirect(t, codexWidth, codexHeight, codexWait)
	t.Logf("Direct capture: %d bytes", len(directRec.Sequences))

	t.Log("\n=== Capturing codex VIA TMUX (correct rendering) ===")
	tmuxRec := captureCodexViaTmux(t, codexWidth, codexHeight, codexWait)
	t.Logf("Tmux capture: %d bytes", len(tmuxRec.Sequences))

	// Save both recordings
	directRec.Save("/tmp/codex-direct.txrec")
	tmuxRec.Save("/tmp/codex-tmux.txrec")
	t.Log("Recordings saved to /tmp/codex-direct.txrec and /tmp/codex-tmux.txrec")

	// === Step 2: Replay both through VTerm ===
	t.Log("\n=== Replaying both through VTerm parser ===")

	directReplay := testutil.NewReplayer(directRec)
	directReplay.PlayAll()
	directReplay.SimulateRender()
	directGrid := directReplay.GetGrid()
	directCurX, directCurY := directReplay.GetCursor()

	tmuxReplay := testutil.NewReplayer(tmuxRec)
	tmuxReplay.PlayAll()
	tmuxReplay.SimulateRender()
	tmuxGrid := tmuxReplay.GetGrid()
	tmuxCurX, tmuxCurY := tmuxReplay.GetCursor()

	t.Logf("Direct cursor: (%d,%d)", directCurX, directCurY)
	t.Logf("Tmux cursor:   (%d,%d)", tmuxCurX, tmuxCurY)

	// === Step 3: Show both grids ===
	t.Log("\n=== DIRECT Grid (what texelterm renders from codex) ===")
	t.Log(testutil.GridToStringWithCursor(directGrid, directCurX, directCurY))

	t.Log("\n=== TMUX Grid (what texelterm renders from tmux+codex) ===")
	t.Log(testutil.GridToStringWithCursor(tmuxGrid, tmuxCurX, tmuxCurY))

	// === Step 4: Compare grids with full color/attr support ===
	t.Log("\n=== Grid Comparison (tmux=reference, direct=actual) ===")
	result := testutil.EnhancedCompareGrids(tmuxGrid, directGrid, codexWidth, codexHeight)
	result.BytesProcessed = len(directRec.Sequences)

	t.Logf("Match: %v", result.Match)
	t.Logf("Summary: %s", result.Summary)

	if result.Match {
		t.Log("Both grids match! VTerm produces identical output for direct and tmux-mediated codex.")
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
	for y := 0; y < codexHeight; y++ {
		if count, ok := rowDiffs[y]; ok {
			t.Logf("  Row %2d: %d diffs", y, count)
		}
	}

	// Save JSON
	if jsonBytes, err := result.ToJSONPretty(); err == nil {
		os.WriteFile("/tmp/codex-direct-vs-tmux.json", jsonBytes, 0644)
		t.Log("\nJSON saved to /tmp/codex-direct-vs-tmux.json")
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
			t.Logf("Row %2d (direct): %s", y, testutil.FormatGridRowWithColors(directGrid[y], codexWidth))
			t.Logf("Row %2d (tmux):   %s", y, testutil.FormatGridRowWithColors(tmuxGrid[y], codexWidth))
		}
	}
}

// TestCodexSavedCompare loads saved recordings and re-compares.
// Useful for iterating on parser fixes without re-capturing.
//
// Run: go test -v -run TestCodexSavedCompare -timeout 10s ./apps/texelterm/testutil/
func TestCodexSavedCompare(t *testing.T) {
	directRec, err := testutil.LoadRecording("/tmp/codex-direct.txrec")
	if err != nil {
		t.Skipf("No direct recording: %v (run TestCodexDirectVsTmux first)", err)
	}
	tmuxRec, err := testutil.LoadRecording("/tmp/codex-tmux.txrec")
	if err != nil {
		t.Skipf("No tmux recording: %v (run TestCodexDirectVsTmux first)", err)
	}

	directReplay := testutil.NewReplayer(directRec)
	directReplay.PlayAll()
	directReplay.SimulateRender()

	tmuxReplay := testutil.NewReplayer(tmuxRec)
	tmuxReplay.PlayAll()
	tmuxReplay.SimulateRender()

	result := testutil.EnhancedCompareGrids(
		tmuxReplay.GetGrid(), directReplay.GetGrid(),
		directRec.Metadata.Width, directRec.Metadata.Height,
	)

	// Codex randomizes tip text and prompt placeholder each session,
	// so rows with character differences are expected. We only care about
	// rows where the TEXT matches but rendering (colors/attributes) differs,
	// since that indicates a real VTerm parser bug.
	rowsWithCharDiffs := map[int]bool{}
	for _, d := range result.Differences {
		if d.DiffType == testutil.DiffTypeChar || d.DiffType == testutil.DiffTypeCombined {
			rowsWithCharDiffs[d.Y] = true
		}
	}

	var significant []testutil.EnhancedDiff
	for _, d := range result.Differences {
		if rowsWithCharDiffs[d.Y] {
			continue // Skip rows with character diffs (random content)
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

// TestCodexGridDump dumps the VTerm grid from a saved recording for visual inspection.
//
// Run: go test -v -run TestCodexGridDump -timeout 10s ./apps/texelterm/testutil/
func TestCodexGridDump(t *testing.T) {
	for _, name := range []string{"direct", "tmux"} {
		path := fmt.Sprintf("/tmp/codex-%s.txrec", name)
		rec, err := testutil.LoadRecording(path)
		if err != nil {
			t.Logf("Skipping %s: %v", name, err)
			continue
		}

		replayer := testutil.NewReplayer(rec)
		replayer.PlayAll()
		replayer.SimulateRender()

		grid := replayer.GetGrid()
		x, y := replayer.GetCursor()

		t.Logf("\n=== %s Grid (%dx%d, %d bytes) ===", name,
			rec.Metadata.Width, rec.Metadata.Height, len(rec.Sequences))
		t.Log(testutil.GridToStringWithCursor(grid, x, y))

		// Row color summary for content rows
		for row := 0; row < len(grid); row++ {
			hasContent := false
			for col := 0; col < len(grid[row]); col++ {
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestCodexPromptCellInspection replays the codex recording and inspects
// the BG color of every cell on key rows to diagnose the "grey background
// only on letters" issue.
//
// Run: go test -v -run TestCodexPromptCellInspection -timeout 10s ./apps/texelterm/testutil/
func TestCodexPromptCellInspection(t *testing.T) {
	rec, err := testutil.LoadRecording("/tmp/codex-direct.txrec")
	if err != nil {
		t.Skipf("No recording: %v (run TestCodexDirectVsTmux first)", err)
	}

	replayer := testutil.NewReplayer(rec)
	replayer.PlayAll()
	replayer.SimulateRender()
	grid := replayer.GetGrid()
	curX, curY := replayer.GetCursor()

	t.Logf("Grid: %dx%d, Cursor: (%d,%d)", rec.Metadata.Width, rec.Metadata.Height, curX, curY)

	// Inspect rows with content - dump BG mode for every cell
	inspectRows := []int{0, 1, 2, 3, 4, 5, curY}
	for _, row := range inspectRows {
		if row >= len(grid) {
			continue
		}

		// Count BG types
		bgDefault, bgStd, bg256, bgRGB := 0, 0, 0, 0
		var details []string
		for col := 0; col < len(grid[row]) && col < rec.Metadata.Width; col++ {
			cell := grid[row][col]
			ch := cell.Rune
			if ch == 0 {
				ch = '·'
			}

			switch cell.BG.Mode {
			case parser.ColorModeDefault:
				bgDefault++
			case parser.ColorModeStandard:
				bgStd++
			case parser.ColorMode256:
				bg256++
			case parser.ColorModeRGB:
				bgRGB++
			}

			// Log detail for first 60 cells
			if col < 60 {
				details = append(details, fmt.Sprintf("%c:bg=%s", ch, colorModeStr(cell.BG)))
			}
		}

		label := fmt.Sprintf("Row %2d", row)
		if row == curY {
			label = fmt.Sprintf("Row %2d (CURSOR)", row)
		}
		t.Logf("%s: bgDefault=%d bgStd=%d bg256=%d bgRGB=%d",
			label, bgDefault, bgStd, bg256, bgRGB)
		t.Logf("  Cells: %s", joinN(details, ", ", 20))
	}

	// Now simulate what applyParserStyle would do:
	// Check if all cells on the prompt line get the same treatment
	t.Log("\n=== Simulated applyParserStyle ===")
	promptRow := curY
	if promptRow < len(grid) {
		allSameBG := true
		firstBG := grid[promptRow][0].BG
		for col := 0; col < rec.Metadata.Width && col < len(grid[promptRow]); col++ {
			cell := grid[promptRow][col]
			if cell.BG != firstBG {
				allSameBG = false
				t.Logf("  Col %d: BG differs! %s vs %s (rune=%c)",
					col, colorModeStr(cell.BG), colorModeStr(firstBG), cell.Rune)
			}
		}
		if allSameBG {
			t.Logf("  Prompt row %d: ALL %d cells have identical BG: %s",
				promptRow, rec.Metadata.Width, colorModeStr(firstBG))
		}
	}

	// Check box rows too
	t.Log("\n=== Box Row BG Analysis ===")
	for row := 0; row <= 5 && row < len(grid); row++ {
		allSameBG := true
		firstBG := grid[row][0].BG
		for col := 0; col < rec.Metadata.Width && col < len(grid[row]); col++ {
			if grid[row][col].BG != firstBG {
				allSameBG = false
				break
			}
		}
		if allSameBG {
			t.Logf("  Row %d: ALL cells same BG: %s", row, colorModeStr(firstBG))
		} else {
			// Count distinct BGs
			bgMap := map[string]int{}
			for col := 0; col < rec.Metadata.Width && col < len(grid[row]); col++ {
				key := colorModeStr(grid[row][col].BG)
				bgMap[key]++
			}
			t.Logf("  Row %d: MIXED BGs: %v", row, bgMap)
		}
	}
}

func colorModeStr(c parser.Color) string {
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

func joinN(items []string, sep string, maxItems int) string {
	if len(items) <= maxItems {
		result := ""
		for i, item := range items {
			if i > 0 {
				result += sep
			}
			result += item
		}
		return result
	}
	result := ""
	for i := 0; i < maxItems; i++ {
		if i > 0 {
			result += sep
		}
		result += items[i]
	}
	return result + fmt.Sprintf(" ... +%d more", len(items)-maxItems)
}

// TestCodexRenderPipelineTrace traces the full rendering pipeline for the codex
// recording: VTerm grid → simulated applyParserStyle → final tcell styles.
// This reveals whether characters and spaces get different tcell backgrounds.
//
// Run: go test -v -run TestCodexRenderPipelineTrace -timeout 10s ./apps/texelterm/testutil/
func TestCodexRenderPipelineTrace(t *testing.T) {
	rec, err := testutil.LoadRecording("/tmp/codex-direct.txrec")
	if err != nil {
		t.Skipf("No recording: %v (run TestCodexDirectVsTmux first)", err)
	}

	replayer := testutil.NewReplayer(rec)
	replayer.PlayAll()
	replayer.SimulateRender()
	grid := replayer.GetGrid()
	curX, curY := replayer.GetCursor()

	t.Logf("Grid: %dx%d, Cursor: (%d,%d)", rec.Metadata.Width, rec.Metadata.Height, curX, curY)

	// Check palette initialization
	tm := theming.ForApp("texelterm")
	bgBase := tm.GetSemanticColor("bg.base")
	textPrimary := tm.GetSemanticColor("text.primary")
	paletteBase := theme.ResolveColorName("base")

	t.Logf("=== Palette Check ===")
	t.Logf("  theme.GetSemanticColor('bg.base') = %v (isDefault: %v)",
		bgBase, bgBase == tcell.ColorDefault)
	t.Logf("  theme.GetSemanticColor('text.primary') = %v (isDefault: %v)",
		textPrimary, textPrimary == tcell.ColorDefault)
	t.Logf("  theme.ResolveColorName('base') = %v (isDefault: %v)",
		paletteBase, paletteBase == tcell.ColorDefault)

	if bgBase != tcell.ColorDefault {
		r, g, b := bgBase.RGB()
		t.Logf("  bg.base RGB: (%d, %d, %d) = #%02x%02x%02x", r, g, b, r, g, b)
	}
	if textPrimary != tcell.ColorDefault {
		r, g, b := textPrimary.RGB()
		t.Logf("  text.primary RGB: (%d, %d, %d) = #%02x%02x%02x", r, g, b, r, g, b)
	}

	// Simulate applyParserStyle for each cell and check tcell styles
	// Use the same logic as term.go applyParserStyle
	colorPalette257 := bgBase // This is what newDefaultPalette sets for slot 257

	t.Logf("\n=== Simulated tcell Style Trace (rows 0-10) ===")
	for row := 0; row <= 10 && row < len(grid); row++ {
		if row >= len(grid) {
			break
		}

		// Track unique styles on this row
		type styleKey struct {
			bgIsDefault bool
			bgR, bgG, bgB int32
			dim, bold, italic bool
			runeIsSpace bool
		}
		styleCounts := map[styleKey]int{}
		var firstNonSpace styleKey
		var firstSpace styleKey
		foundNonSpace := false
		foundSpace := false

		for col := 0; col < rec.Metadata.Width && col < len(grid[row]); col++ {
			cell := grid[row][col]

			// Simulate applyParserStyle BG mapping
			var bgColor tcell.Color
			if cell.BG.Mode == parser.ColorModeDefault {
				bgColor = colorPalette257
			} else {
				switch cell.BG.Mode {
				case parser.ColorModeStandard, parser.ColorMode256:
					bgColor = tcell.PaletteColor(int(cell.BG.Value))
				case parser.ColorModeRGB:
					bgColor = tcell.NewRGBColor(int32(cell.BG.R), int32(cell.BG.G), int32(cell.BG.B))
				default:
					bgColor = tcell.ColorDefault
				}
			}

			isDim := cell.Attr&parser.AttrDim != 0
			isBold := cell.Attr&parser.AttrBold != 0
			isItalic := cell.Attr&parser.AttrItalic != 0
			isSpace := cell.Rune == 0 || cell.Rune == ' '

			var r, g, b int32
			isDefault := bgColor == tcell.ColorDefault
			if !isDefault {
				r, g, b = bgColor.RGB()
			}

			key := styleKey{
				bgIsDefault: isDefault,
				bgR: r, bgG: g, bgB: b,
				dim: isDim, bold: isBold, italic: isItalic,
				runeIsSpace: isSpace,
			}
			styleCounts[key]++

			if !isSpace && !foundNonSpace {
				firstNonSpace = key
				foundNonSpace = true
			}
			if isSpace && !foundSpace {
				firstSpace = key
				foundSpace = true
			}
		}

		// Report
		t.Logf("  Row %d: %d unique style combinations:", row, len(styleCounts))
		for key, count := range styleCounts {
			bgStr := "DEFAULT"
			if !key.bgIsDefault {
				bgStr = fmt.Sprintf("#%02x%02x%02x", key.bgR, key.bgG, key.bgB)
			}
			attrStr := "none"
			attrs := []string{}
			if key.dim { attrs = append(attrs, "dim") }
			if key.bold { attrs = append(attrs, "bold") }
			if key.italic { attrs = append(attrs, "italic") }
			if len(attrs) > 0 { attrStr = fmt.Sprintf("%v", attrs) }

			charType := "char"
			if key.runeIsSpace { charType = "space" }
			t.Logf("    [%s] bg=%s attr=%s : %d cells", charType, bgStr, attrStr, count)
		}

		// Check if characters and spaces have different BGs
		if foundNonSpace && foundSpace {
			if firstNonSpace.bgIsDefault != firstSpace.bgIsDefault ||
				firstNonSpace.bgR != firstSpace.bgR ||
				firstNonSpace.bgG != firstSpace.bgG ||
				firstNonSpace.bgB != firstSpace.bgB {
				t.Logf("    *** BG MISMATCH: chars have different BG than spaces!")
			} else if firstNonSpace.dim != firstSpace.dim {
				t.Logf("    *** ATTR MISMATCH: chars have dim=%v but spaces have dim=%v",
					firstNonSpace.dim, firstSpace.dim)
			}
		}
	}

	// Summary: what would the protocol see?
	t.Logf("\n=== Protocol Style Analysis ===")
	if colorPalette257 == tcell.ColorDefault {
		t.Logf("  WARNING: colorPalette[257] is tcell.ColorDefault!")
		t.Logf("  This means ALL cells with BG=default get tcell.ColorDefault as background")
		t.Logf("  In client-server mode, this maps to protocol.ColorModelDefault")
		t.Logf("  The client renderer will NOT replace these with state.defaultStyle")
		t.Logf("  (because style has Dim/Bold attrs set, making it != tcell.Style{})")
		t.Logf("  Result: cells get the outer terminal's default BG (usually black)")
	} else {
		r, g, b := colorPalette257.RGB()
		t.Logf("  colorPalette[257] = #%02x%02x%02x (proper RGB)", r, g, b)
		t.Logf("  All cells with BG=default will get this color as background")
		t.Logf("  Protocol encodes as ColorModelRGB")
	}
}

// TestCodexCaptureReplay replays the real codex-capture.log through VTerm
// and checks background colors on the prompt row.
func TestCodexCaptureReplay(t *testing.T) {
	// Try multiple paths to find the capture file
	capturePath := ""
	for _, p := range []string{
		"../../../../codex-capture.log",
		"../../../codex-capture.log",
		"/home/marc/projects/texel/texelation/codex-capture.log",
	} {
		if _, err := os.Stat(p); err == nil {
			capturePath = p
			break
		}
	}
	if capturePath == "" {
		t.Skip("codex-capture.log not found")
	}
	data, err := os.ReadFile(capturePath)
	if err != nil {
		t.Skipf("codex-capture.log not found: %v", err)
	}

	// Parse header for dimensions (COLUMNS="150" LINES="44")
	width, height := 150, 44

	// Skip first line (Script header)
	start := 0
	for i, b := range data {
		if b == '\n' {
			start = i + 1
			break
		}
	}

	// Create recording from raw capture data
	rec := testutil.NewRecording(width, height)
	rec.Sequences = data[start:]

	// Find the byte offset of "This is the place" in the raw data
	searchText := []byte("This is the place")
	textOffset := -1
	for i := 0; i < len(rec.Sequences)-len(searchText); i++ {
		if string(rec.Sequences[i:i+len(searchText)]) == string(searchText) {
			textOffset = i
			break
		}
	}
	if textOffset < 0 {
		t.Fatal("Could not find 'This is the place' in capture data")
	}
	t.Logf("Found 'This is the place' at byte offset %d of %d total", textOffset, len(rec.Sequences))

	// Find END_SYNC (ESC[?2026l) after the text to get a complete frame
	endSync := []byte("\x1b[?2026l")
	endSyncOffset := -1
	for i := textOffset; i < len(rec.Sequences)-len(endSync); i++ {
		if string(rec.Sequences[i:i+len(endSync)]) == string(endSync) {
			endSyncOffset = i + len(endSync)
			break
		}
	}
	if endSyncOffset < 0 {
		endSyncOffset = len(rec.Sequences)
	}
	t.Logf("Playing up to END_SYNC at offset %d", endSyncOffset)

	replayer := testutil.NewReplayer(rec)
	replayer.PlayString(string(rec.Sequences[:endSyncOffset]))
	v := replayer.VTerm()

	grid := v.Grid()
	t.Logf("Grid size: %d rows x %d cols (alt screen: checking)", len(grid), width)

	// Find the row containing "This is the place"
	promptRow := -1
	for y := 0; y < len(grid); y++ {
		var runes []rune
		for x := 0; x < len(grid[y]) && x < width; x++ {
			r := grid[y][x].Rune
			if r == 0 {
				r = ' '
			}
			runes = append(runes, r)
		}
		line := string(runes)
		if strings.Contains(line, "This is the place") {
			promptRow = y
		}
		// Print ALL rows for context
		if len(runes) > 80 {
			runes = runes[:80]
		}
		t.Logf("Row %2d: %q", y, string(runes))
	}

	if promptRow < 0 {
		t.Fatal("Could not find 'This is the place' in grid")
	}
	t.Logf("\n=== Prompt row: %d ===", promptRow)

	// Analyze BG colors on the prompt row and surrounding rows
	for row := promptRow - 3; row <= promptRow+3; row++ {
		if row < 0 || row >= len(grid) {
			continue
		}
		t.Logf("\n--- Row %d cell-by-cell BG analysis ---", row)

		// Group consecutive cells by BG
		type bgRun struct {
			startCol int
			endCol   int
			bg       parser.Color
			sample   rune
			attr     parser.Attribute
		}
		var runs []bgRun

		for col := 0; col < len(grid[row]) && col < width; col++ {
			cell := grid[row][col]
			if len(runs) > 0 {
				last := &runs[len(runs)-1]
				if last.bg == cell.BG && last.attr == cell.Attr {
					last.endCol = col
					if cell.Rune != 0 && cell.Rune != ' ' {
						last.sample = cell.Rune
					}
					continue
				}
			}
			sample := cell.Rune
			if sample == 0 {
				sample = ' '
			}
			runs = append(runs, bgRun{
				startCol: col,
				endCol:   col,
				bg:       cell.BG,
				sample:   sample,
				attr:     cell.Attr,
			})
		}

		for _, run := range runs {
			bgStr := "default"
			switch run.bg.Mode {
			case parser.ColorModeRGB:
				bgStr = fmt.Sprintf("RGB(%d,%d,%d)", run.bg.R, run.bg.G, run.bg.B)
			case parser.ColorModeStandard:
				bgStr = fmt.Sprintf("ANSI(%d)", run.bg.Value)
			case parser.ColorMode256:
				bgStr = fmt.Sprintf("256(%d)", run.bg.Value)
			}
			attrStr := ""
			if run.attr != 0 {
				attrStr = fmt.Sprintf(" attr=%s", run.attr)
			}
			count := run.endCol - run.startCol + 1
			t.Logf("  cols %3d-%3d (%3d cells): bg=%-20s sample=%q%s",
				run.startCol, run.endCol, count, bgStr, string(run.sample), attrStr)
		}
	}
}

// TestCodexCaptureScrollback replays the FULL codex-capture.log and examines
// scrollback state to diagnose the "empty space after Codex" bug.
func TestCodexCaptureScrollback(t *testing.T) {
	capturePath := ""
	for _, p := range []string{
		"../../../../codex-capture.log",
		"../../../codex-capture.log",
		"/home/marc/projects/texel/texelation/codex-capture.log",
	} {
		if _, err := os.Stat(p); err == nil {
			capturePath = p
			break
		}
	}
	if capturePath == "" {
		t.Skip("codex-capture.log not found")
	}
	data, err := os.ReadFile(capturePath)
	if err != nil {
		t.Skipf("codex-capture.log not found: %v", err)
	}

	width, height := 150, 44

	// Skip first line (Script header)
	start := 0
	for i, b := range data {
		if b == '\n' {
			start = i + 1
			break
		}
	}

	rec := testutil.NewRecording(width, height)
	rec.Sequences = data[start:]

	// Play the FULL capture
	replayer := testutil.NewReplayer(rec)
	replayer.PlayString(string(rec.Sequences))
	v := replayer.VTerm()

	mb := v.MemoryBuffer()
	if mb == nil {
		t.Fatal("MemoryBuffer is nil")
	}

	liveEdge := v.LiveEdgeBase()
	t.Logf("After full replay: liveEdgeBase=%d, GlobalOffset=%d, GlobalEnd=%d, TotalLines=%d",
		liveEdge, mb.GlobalOffset(), mb.GlobalEnd(), mb.TotalLines())
	t.Logf("marginTop=%d, marginBottom=%d, inAltScreen=%v",
		v.MarginTop(), v.MarginBottom(), v.InAltScreen())

	// Scan scrollback for empty gaps
	maxEmptyRun := 0
	currentEmptyRun := 0
	emptyRunStart := int64(-1)
	worstRunStart := int64(-1)
	for idx := mb.GlobalOffset(); idx < mb.GlobalEnd(); idx++ {
		line := mb.GetLine(idx)
		isEmpty := true
		if line != nil {
			for _, cell := range line.Cells {
				if cell.Rune != 0 && cell.Rune != ' ' {
					isEmpty = false
					break
				}
			}
		}
		if isEmpty {
			if currentEmptyRun == 0 {
				emptyRunStart = idx
			}
			currentEmptyRun++
			if currentEmptyRun > maxEmptyRun {
				maxEmptyRun = currentEmptyRun
				worstRunStart = emptyRunStart
			}
		} else {
			currentEmptyRun = 0
		}
	}

	t.Logf("Max consecutive empty lines: %d (starting at line %d)", maxEmptyRun, worstRunStart)

	// Print context around the worst empty run
	if maxEmptyRun > 3 && worstRunStart >= 0 {
		contextStart := worstRunStart - 3
		if contextStart < mb.GlobalOffset() {
			contextStart = mb.GlobalOffset()
		}
		contextEnd := worstRunStart + int64(maxEmptyRun) + 3
		if contextEnd > mb.GlobalEnd() {
			contextEnd = mb.GlobalEnd()
		}
		for idx := contextStart; idx < contextEnd; idx++ {
			line := mb.GetLine(idx)
			text := ""
			if line != nil {
				for _, cell := range line.Cells {
					if cell.Rune == 0 {
						text += " "
					} else {
						text += string(cell.Rune)
					}
				}
			}
			// Trim trailing spaces
			trimmed := strings.TrimRight(text, " ")
			marker := "  "
			if idx >= worstRunStart && idx < worstRunStart+int64(maxEmptyRun) {
				marker = ">>"
			}
			if idx == liveEdge {
				marker = "LE"
			}
			fixedWidth := 0
			if line != nil {
				fixedWidth = line.FixedWidth
			}
			t.Logf("%s Line %4d (fw=%d): %q", marker, idx, fixedWidth, trimmed)
		}
	}
}

// TestAltScreenEraseWithGreyBG is a minimal test for ESC[K] preserving BG color.
func TestAltScreenEraseWithGreyBG(t *testing.T) {
	width, height := 80, 24

	rec := testutil.NewRecording(width, height)

	// Enter alt screen
	rec.AppendCSI("?1049h")
	// Set scroll region 1-20
	rec.AppendCSI("1;20r")
	// Move to bottom of scroll region
	rec.AppendCSI("20;1H")
	// Set grey BG and erase to end of line
	rec.AppendCSI("48;2;57;57;71m")
	rec.AppendCSI("K")
	// Write some text
	rec.AppendText("Hello")
	// Reset
	rec.AppendCSI("0m")

	replayer := testutil.NewReplayer(rec)
	replayer.PlayAll()
	grid := replayer.VTerm().Grid()

	// Row 19 (0-indexed for row 20 1-based) should have grey BG everywhere
	row := 19
	t.Logf("Row %d analysis:", row)
	for col := 0; col < width; col++ {
		cell := grid[row][col]
		r := cell.Rune
		if r == 0 {
			r = ' '
		}
		if col < 10 || col == width-1 {
			t.Logf("  col %d: rune=%q BG=%+v", col, string(r), cell.BG)
		}
	}

	// Check that col 5 (after "Hello") has grey BG, not default
	if grid[row][5].BG.Mode != parser.ColorModeRGB ||
		grid[row][5].BG.R != 57 || grid[row][5].BG.G != 57 || grid[row][5].BG.B != 71 {
		t.Errorf("Col 5 should have grey BG RGB(57,57,71), got %+v", grid[row][5].BG)
	}

	// Check last col
	if grid[row][width-1].BG.Mode != parser.ColorModeRGB {
		t.Errorf("Col %d should have grey BG, got %+v", width-1, grid[row][width-1].BG)
	}
}

// TestAltScreenScrollThenErase tests ESC[K] after scroll (as codex does).
func TestAltScreenScrollThenErase(t *testing.T) {
	width, height := 80, 24

	rec := testutil.NewRecording(width, height)

	// Enter alt screen
	rec.AppendCSI("?1049h")
	// Set scroll region 1-20
	rec.AppendCSI("1;20r")
	// Move to bottom of scroll region
	rec.AppendCSI("20;1H")

	// Scroll by doing CR+LF at bottom of region
	rec.AppendCRLF() // CR LF → scroll
	// Reset
	rec.AppendCSI("0m")
	// Set grey BG
	rec.AppendCSI("39;48;2;57;57;71m")
	// Erase to end of line (should fill entire row with grey)
	rec.AppendCSI("K")
	// Write text
	rec.AppendCSI("1m") // bold
	rec.AppendCSI("2m") // dim
	rec.AppendCSI("39;48;2;57;57;71m") // grey BG
	rec.AppendText("Hello World")
	rec.AppendCSI("22m") // clear bold/dim
	rec.AppendCSI("39m")
	rec.AppendCSI("49m")
	rec.AppendCSI("0m")

	// Scroll again (text row moves up)
	rec.AppendCRLF()

	replayer := testutil.NewReplayer(rec)
	replayer.PlayAll()
	grid := replayer.VTerm().Grid()

	// The text row should have scrolled up to row 18 (0-indexed)
	// Find the row with "Hello World"
	textRow := -1
	for y := 0; y < height; y++ {
		line := ""
		for x := 0; x < width; x++ {
			r := grid[y][x].Rune
			if r == 0 {
				r = ' '
			}
			line += string(r)
		}
		if strings.Contains(line, "Hello World") {
			textRow = y
		}
	}
	if textRow < 0 {
		t.Fatal("Could not find 'Hello World' in grid")
	}
	t.Logf("Text row: %d", textRow)

	// Check BG colors on the text row
	for col := 0; col < width; col++ {
		cell := grid[textRow][col]
		r := cell.Rune
		if r == 0 {
			r = ' '
		}
		isGrey := cell.BG.Mode == parser.ColorModeRGB &&
			cell.BG.R == 57 && cell.BG.G == 57 && cell.BG.B == 71
		if !isGrey {
			t.Errorf("Col %d (%q): expected grey BG RGB(57,57,71), got %+v",
				col, string(r), cell.BG)
		}
	}
}

// TestCodexExactSequence reproduces the exact codex prompt escape sequence.
func TestCodexExactSequence(t *testing.T) {
	width, height := 150, 44

	rec := testutil.NewRecording(width, height)

	// Enter alt screen
	rec.AppendCSI("?1049h")
	// Set scroll region 1-39
	rec.AppendCSI("1;39r")
	// Move to bottom of scroll region (row 39 = 0-indexed 38)
	rec.AppendCSI("39;1H")

	// 1st scroll + default erase
	rec.AppendCRLF()
	rec.AppendCSI("39;49m")
	rec.AppendCSI("K")
	rec.AppendCSI("39m")
	rec.AppendCSI("49m")
	rec.AppendCSI("0m")

	// 2nd scroll + grey erase
	rec.AppendCRLF()
	rec.AppendCSI("39;48;2;57;57;71m")
	rec.AppendCSI("K")
	rec.AppendCSI("39m")
	rec.AppendCSI("49m")
	rec.AppendCSI("0m")

	// 3rd scroll + grey erase + text
	rec.AppendCRLF()
	rec.AppendCSI("39;48;2;57;57;71m")
	rec.AppendCSI("K")
	rec.AppendCSI("1m")
	rec.AppendCSI("2m")
	rec.AppendCSI("39;48;2;57;57;71m")
	rec.AppendString("\xe2\x80\xba ") // › + space
	rec.AppendCSI("22m")
	rec.AppendCSI("22m")
	rec.AppendText("This is the place")
	rec.AppendCSI("39m")
	rec.AppendCSI("49m")
	rec.AppendCSI("0m")

	// 4th scroll + grey erase (line after prompt)
	rec.AppendCRLF()
	rec.AppendCSI("39;48;2;57;57;71m")
	rec.AppendCSI("K")
	rec.AppendCSI("39m")
	rec.AppendCSI("49m")
	rec.AppendCSI("0m")

	// Reset scroll region
	rec.AppendCSI("r")

	replayer := testutil.NewReplayer(rec)
	replayer.PlayAll()
	grid := replayer.VTerm().Grid()

	// Find the prompt row
	promptRow := -1
	for y := 0; y < height; y++ {
		var runes []rune
		for x := 0; x < width; x++ {
			r := grid[y][x].Rune
			if r == 0 {
				r = ' '
			}
			runes = append(runes, r)
		}
		line := string(runes)
		if strings.Contains(line, "This is the place") {
			promptRow = y
			t.Logf("Row %d: %q", y, strings.TrimRight(line, " "))
		}
	}
	if promptRow < 0 {
		t.Fatal("Could not find prompt row")
	}

	// Check BG on the prompt row
	greyCount, defaultCount := 0, 0
	for col := 0; col < width; col++ {
		cell := grid[promptRow][col]
		isGrey := cell.BG.Mode == parser.ColorModeRGB &&
			cell.BG.R == 57 && cell.BG.G == 57 && cell.BG.B == 71
		if isGrey {
			greyCount++
		} else {
			defaultCount++
		}
	}
	t.Logf("Prompt row %d: %d grey cells, %d default cells", promptRow, greyCount, defaultCount)

	if defaultCount > 0 {
		// Show the BG transition point
		for col := 0; col < width; col++ {
			cell := grid[promptRow][col]
			isGrey := cell.BG.Mode == parser.ColorModeRGB
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			if col < 25 || col > width-5 || !isGrey {
				t.Logf("  col %3d: rune=%q bg=%+v attr=%s", col, string(r), cell.BG, cell.Attr)
				if !isGrey && col > 0 {
					break // Show first non-grey cell
				}
			}
		}
		t.Errorf("Expected ALL cells on prompt row to have grey BG, got %d default", defaultCount)
	}
}

// TestCodexStepThrough steps through the capture checking row state at key points.
func TestCodexStepThrough(t *testing.T) {
	capturePath := "/home/marc/projects/texel/texelation/codex-capture.log"
	data, err := os.ReadFile(capturePath)
	if err != nil {
		t.Skipf("capture not found: %v", err)
	}

	nl := 0
	for i, b := range data {
		if b == '\n' {
			nl = i + 1
			break
		}
	}
	seqData := data[nl:]
	width, height := 150, 44

	// Find the text in the raw data
	searchStr := "This is the place"
	textOffset := -1
	for i := 0; i < len(seqData)-len(searchStr); i++ {
		if string(seqData[i:i+len(searchStr)]) == searchStr {
			textOffset = i
			break
		}
	}
	if textOffset < 0 {
		t.Fatal("Could not find text in capture data")
	}

	// Find the BEGIN_SYNC before the text
	beginSyncStr := "\x1b[?2026h"
	syncStart := 0
	for i := textOffset; i >= 0; i-- {
		if i+len(beginSyncStr) <= len(seqData) {
			match := true
			for j := 0; j < len(beginSyncStr); j++ {
				if seqData[i+j] != beginSyncStr[j] {
					match = false
					break
				}
			}
			if match {
				syncStart = i + len(beginSyncStr)
				t.Logf("Found BEGIN_SYNC at offset %d", i)
				break
			}
		}
	}

	t.Logf("sync block starts at offset %d, text at %d", syncStart, textOffset)

	// Create replayer and play up to sync block start
	rec := testutil.NewRecording(width, height)
	rec.Sequences = seqData

	replayer := testutil.NewReplayer(rec)
	replayer.PlayString(string(seqData[:syncStart]))

	v := replayer.VTerm()
	cx, cy := v.Cursor()
	t.Logf("Before sync block: inAltScreen=%v, cursor=(%d,%d)", v.InAltScreen(), cx, cy)

	// Now play the sync block character by character and check cursor row
	// after each ESC[K]
	syncData := string(seqData[syncStart:textOffset+100])
	checkRow := func(label string) {
		grid := v.Grid()
		cx, cy := v.Cursor()
		greyCount := 0
		defaultCount := 0
		if cy >= 0 && cy < len(grid) {
			for col := 0; col < width && col < len(grid[cy]); col++ {
				if grid[cy][col].BG.Mode == parser.ColorModeRGB {
					greyCount++
				} else {
					defaultCount++
				}
			}
		}
		t.Logf("  %s: cursor=(%d,%d) grey=%d default=%d",
			label, cx, cy, greyCount, defaultCount)
	}

	// Feed character by character, pausing at ESC[K]
	escKcount := 0
	i := 0
	runes := []rune(syncData)
	for i < len(runes) {
		ch := runes[i]
		replayer.VTerm() // just to keep reference

		// Check if this starts an ESC[K
		if ch == '\x1b' && i+2 < len(runes) && runes[i+1] == '[' && runes[i+2] == 'K' {
			escKcount++
			// Feed the ESC[K
			for _, c := range "\x1b[K" {
				replayer.PlayString(string(c))
			}
			i += 3
			checkRow(fmt.Sprintf("after ESC[K #%d", escKcount))
			continue
		}

		replayer.PlayString(string(ch))
		i++
	}
}

// Ensure fmt is used
var _ = fmt.Sprintf

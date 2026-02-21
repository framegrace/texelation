# Claude Code Rendering Bug Investigation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Identify and fix the rendering bug that causes Claude Code CLI to display with extra `│` characters and misaligned boxes in texelterm.

**Architecture:** Capture Claude Code's raw terminal output via InteractiveCapture, replay through VTerm parser, compare grid against tmux reference, identify divergent escape sequences, fix parser.

**Tech Stack:** Go test infrastructure, `testutil.InteractiveCapture`, `testutil.Replayer`, `testutil.ReferenceComparator`, `parser.VTerm`

---

### Task 1: Capture Claude Code Session

**Files:**
- Create: `apps/texelterm/testutil/claude_code_debug_test.go`
- Reference: `apps/texelterm/testutil/codex_debug_test.go` (pattern to follow)
- Reference: `apps/texelterm/testutil/interactive_capture.go` (API)

**Step 1: Create the capture test file**

Create `apps/texelterm/testutil/claude_code_debug_test.go` with capture functions modeled on `codex_debug_test.go`. Key differences from Codex:

- Claude Code binary is `claude` (not `codex`)
- Claude Code takes longer to initialize (~8-10s for welcome screen)
- We need to wait for the welcome screen to fully render, then send Escape+`y` to exit (or Ctrl+C)
- Use 200x50 dimensions to match a realistic terminal size

```go
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
)

const (
	claudeCodeWidth  = 200
	claudeCodeHeight = 50
	claudeCodeWait   = 10 * time.Second
)

// captureClaudeCodeDirect captures claude running directly in a PTY.
func captureClaudeCodeDirect(t *testing.T, width, height int, wait time.Duration) *testutil.Recording {
	t.Helper()
	ic, err := testutil.NewInteractiveCapture("claude", nil, width, height)
	if err != nil {
		t.Fatalf("Failed to start direct claude capture: %v", err)
	}
	time.Sleep(wait)
	rec := ic.ToRecording()
	ic.Close()
	rec.Metadata.Description = "claude code direct (no tmux)"
	return rec
}

// captureClaudeCodeViaTmux captures claude running inside tmux.
func captureClaudeCodeViaTmux(t *testing.T, width, height int, wait time.Duration) *testutil.Recording {
	t.Helper()

	tmpConf, err := os.CreateTemp("", "tmux-clean-*.conf")
	if err != nil {
		t.Fatalf("Failed to create temp tmux config: %v", err)
	}
	tmpConf.WriteString("set -g status off\nset -g default-terminal \"xterm-256color\"\n")
	tmpConf.Close()
	defer os.Remove(tmpConf.Name())

	serverName := fmt.Sprintf("claude-test-%d", time.Now().UnixNano())
	ic, err := testutil.NewInteractiveCapture(
		"tmux", []string{
			"-L", serverName,
			"-f", tmpConf.Name(),
			"new-session",
			"-x", fmt.Sprintf("%d", width),
			"-y", fmt.Sprintf("%d", height),
			"claude",
		},
		width, height,
	)
	if err != nil {
		t.Fatalf("Failed to start tmux+claude capture: %v", err)
	}
	time.Sleep(wait)
	rec := ic.ToRecording()
	ic.Close()

	exec.Command("tmux", "-L", serverName, "kill-server").Run()

	rec.Metadata.Description = "claude code via tmux (reference)"
	return rec
}
```

**Step 2: Run test to verify captures work**

Run: `go test -v -run TestClaudeCodeDirectVsTmux -timeout 60s ./apps/texelterm/testutil/`
Expected: Both captures complete, recordings saved to /tmp/

**Step 3: Commit**

```bash
git add apps/texelterm/testutil/claude_code_debug_test.go
git commit -m "Add Claude Code rendering debug test infrastructure"
```

---

### Task 2: Compare Direct vs Tmux Grids

**Files:**
- Modify: `apps/texelterm/testutil/claude_code_debug_test.go`

**Step 1: Add the main comparison test**

Add `TestClaudeCodeDirectVsTmux` to the file created in Task 1. Follow the same pattern as `TestCodexDirectVsTmux` (lines 100-226 of `codex_debug_test.go`):

```go
// TestClaudeCodeDirectVsTmux captures claude both directly and via tmux,
// replays both through VTerm, and compares the grids.
//
// Run: go test -v -run TestClaudeCodeDirectVsTmux -timeout 60s ./apps/texelterm/testutil/
func TestClaudeCodeDirectVsTmux(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not found in PATH")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not found in PATH")
	}

	t.Log("=== Capturing claude DIRECTLY (what texelterm sees) ===")
	directRec := captureClaudeCodeDirect(t, claudeCodeWidth, claudeCodeHeight, claudeCodeWait)
	t.Logf("Direct capture: %d bytes", len(directRec.Sequences))

	t.Log("\n=== Capturing claude VIA TMUX (correct rendering) ===")
	tmuxRec := captureClaudeCodeViaTmux(t, claudeCodeWidth, claudeCodeHeight, claudeCodeWait)
	t.Logf("Tmux capture: %d bytes", len(tmuxRec.Sequences))

	// Save recordings for re-analysis without recapturing
	directRec.Save("/tmp/claude-code-direct.txrec")
	tmuxRec.Save("/tmp/claude-code-tmux.txrec")
	t.Log("Recordings saved to /tmp/claude-code-direct.txrec and /tmp/claude-code-tmux.txrec")

	// Replay both through VTerm
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

	// Show both grids
	t.Log("\n=== DIRECT Grid ===")
	t.Log(testutil.GridToStringWithCursor(directGrid, directCurX, directCurY))
	t.Log("\n=== TMUX Grid ===")
	t.Log(testutil.GridToStringWithCursor(tmuxGrid, tmuxCurX, tmuxCurY))

	// Compare with full color/attr support
	result := testutil.EnhancedCompareGrids(tmuxGrid, directGrid, claudeCodeWidth, claudeCodeHeight)
	result.BytesProcessed = len(directRec.Sequences)

	t.Logf("Match: %v", result.Match)
	t.Logf("Summary: %s", result.Summary)

	if result.Match {
		t.Log("Both grids match!")
		return
	}

	// Detailed diff analysis
	t.Log(testutil.FormatEnhancedResult(result))

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
	for y := 0; y < claudeCodeHeight; y++ {
		if count, ok := rowDiffs[y]; ok {
			t.Logf("  Row %2d: %d diffs", y, count)
		}
	}

	// Save JSON for detailed analysis
	if jsonBytes, err := result.ToJSONPretty(); err == nil {
		os.WriteFile("/tmp/claude-code-direct-vs-tmux.json", jsonBytes, 0644)
		t.Log("\nJSON saved to /tmp/claude-code-direct-vs-tmux.json")
	}

	// Escape sequence comparison (first 5000 bytes to see Claude Code's renderer)
	seqLen := 5000
	if seqLen > len(directRec.Sequences) {
		seqLen = len(directRec.Sequences)
	}
	t.Logf("\nDirect sequences (first %d bytes):\n%s",
		seqLen, testutil.EscapeSequenceLog(directRec.Sequences[:seqLen]))

	seqLen = 5000
	if seqLen > len(tmuxRec.Sequences) {
		seqLen = len(tmuxRec.Sequences)
	}
	t.Logf("\nTmux sequences (first %d bytes):\n%s",
		seqLen, testutil.EscapeSequenceLog(tmuxRec.Sequences[:seqLen]))

	// Dirty tracking analysis
	if directReplay.HasVisualMismatch() {
		mismatches := directReplay.FindVisualMismatches()
		t.Logf("\nDIRTY TRACKING MISMATCHES: %d", len(mismatches))
		for i, m := range mismatches {
			if i >= 20 {
				t.Logf("... and %d more", len(mismatches)-20)
				break
			}
			t.Logf("  (%d,%d): rendered=%q logical=%q", m.X, m.Y,
				string(m.Rendered.Rune), string(m.Logical.Rune))
		}
	} else {
		t.Log("\nNo dirty tracking mismatches")
	}

	// Color detail for rows with diffs
	t.Log("\n=== Color Detail on Differing Rows ===")
	for y := range rowDiffs {
		if y < len(directGrid) && y < len(tmuxGrid) {
			t.Logf("Row %2d (direct): %s", y, testutil.FormatGridRowWithColors(directGrid[y], claudeCodeWidth))
			t.Logf("Row %2d (tmux):   %s", y, testutil.FormatGridRowWithColors(tmuxGrid[y], claudeCodeWidth))
		}
	}
}
```

**Step 2: Add a saved-recording re-compare test**

```go
// TestClaudeCodeSavedCompare loads saved recordings and re-compares.
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

	// Claude Code randomizes some content each session, so filter those rows
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
		t.Errorf("Significant visual differences: %d", len(significant))
	}
}
```

**Step 3: Add a grid dump test for offline inspection**

```go
// TestClaudeCodeGridDump dumps VTerm grids from saved recordings.
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
```

**Step 4: Run capture test**

Run: `go test -v -run TestClaudeCodeDirectVsTmux -timeout 60s ./apps/texelterm/testutil/`
Expected: Captures both, shows grid comparison with specific differences identified.

**Step 5: Commit**

```bash
git add apps/texelterm/testutil/claude_code_debug_test.go
git commit -m "Add Claude Code capture and comparison tests"
```

---

### Task 3: Analyze Grid Differences

**Files:**
- Modify: `apps/texelterm/testutil/claude_code_debug_test.go`

This task is **diagnostic** — analyze the output from Task 2 to identify the root cause. The specific steps depend on what the grid comparison reveals.

**Step 1: Run the grid dump and inspect the bottom status bar rows**

Run: `go test -v -run TestClaudeCodeGridDump -timeout 10s ./apps/texelterm/testutil/`

Inspect the output for:
- Which rows contain the extra `│` characters in the direct capture but not in tmux
- Whether characters at those positions differ (char, color, or attribute)
- Whether there are cells with unexpected rune values (0, or wrong Unicode)

**Step 2: Add a focused cell inspection test for the status bar area**

Add a test that inspects the bottom ~10 rows cell by cell, comparing direct vs tmux at each position:

```go
// TestClaudeCodeStatusBarInspection inspects the bottom status bar area.
// Run: go test -v -run TestClaudeCodeStatusBarInspection -timeout 10s ./apps/texelterm/testutil/
func TestClaudeCodeStatusBarInspection(t *testing.T) {
	directRec, err := testutil.LoadRecording("/tmp/claude-code-direct.txrec")
	if err != nil {
		t.Skipf("No direct recording: %v", err)
	}
	tmuxRec, err := testutil.LoadRecording("/tmp/claude-code-tmux.txrec")
	if err != nil {
		t.Skipf("No tmux recording: %v", err)
	}

	directReplay := testutil.NewReplayer(directRec)
	directReplay.PlayAll()
	directGrid := directReplay.Grid()

	tmuxReplay := testutil.NewReplayer(tmuxRec)
	tmuxReplay.PlayAll()
	tmuxGrid := tmuxReplay.Grid()

	height := directRec.Metadata.Height
	width := directRec.Metadata.Width

	// Inspect last 15 rows (where status bar lives)
	startRow := height - 15
	if startRow < 0 {
		startRow = 0
	}

	for row := startRow; row < height; row++ {
		if row >= len(directGrid) || row >= len(tmuxGrid) {
			break
		}

		// Find columns where they differ
		var diffs []int
		for col := 0; col < width; col++ {
			dCol := ' '
			tCol := ' '
			if col < len(directGrid[row]) {
				dCol = directGrid[row][col].Rune
				if dCol == 0 { dCol = ' ' }
			}
			if col < len(tmuxGrid[row]) {
				tCol = tmuxGrid[row][col].Rune
				if tCol == 0 { tCol = ' ' }
			}
			if dCol != tCol {
				diffs = append(diffs, col)
			}
		}

		if len(diffs) == 0 {
			continue
		}

		t.Logf("\nRow %d: %d character differences", row, len(diffs))
		for _, col := range diffs {
			dCell := directGrid[row][col]
			tCell := tmuxGrid[row][col]
			dRune := dCell.Rune; if dRune == 0 { dRune = '·' }
			tRune := tCell.Rune; if tRune == 0 { tRune = '·' }

			t.Logf("  col %3d: direct=%q (U+%04X bg=%v) tmux=%q (U+%04X bg=%v)",
				col,
				string(dRune), dRune, dCell.BG,
				string(tRune), tRune, tCell.BG)
		}
	}
}
```

**Step 3: Run the inspection test**

Run: `go test -v -run TestClaudeCodeStatusBarInspection -timeout 10s ./apps/texelterm/testutil/`

Analyze the output to determine:
1. Are characters appearing at wrong columns? (cursor positioning bug)
2. Are extra characters appearing that tmux doesn't have? (escape sequence leak)
3. Are characters missing that tmux does have? (sequence not handled)
4. Are character widths different? (width calculation mismatch)

**Step 4: Add escape sequence trace for problematic area**

If the issue is in specific rows, add a test that replays the capture step by step, checking grid state at each synchronized update frame boundary (ESC[?2026l):

```go
// TestClaudeCodeFrameByFrame steps through sync frames.
// Run: go test -v -run TestClaudeCodeFrameByFrame -timeout 10s ./apps/texelterm/testutil/
func TestClaudeCodeFrameByFrame(t *testing.T) {
	rec, err := testutil.LoadRecording("/tmp/claude-code-direct.txrec")
	if err != nil {
		t.Skipf("No recording: %v", err)
	}

	data := rec.Sequences
	beginSync := []byte("\x1b[?2026h")
	endSync := []byte("\x1b[?2026l")

	replayer := testutil.NewReplayer(rec)
	frameNum := 0
	pos := 0

	for pos < len(data) {
		// Find next end-of-sync
		endIdx := bytes.Index(data[pos:], endSync)
		if endIdx < 0 {
			// Play remaining
			replayer.PlayString(string(data[pos:]))
			break
		}

		// Play up to and including end-sync
		chunkEnd := pos + endIdx + len(endSync)
		replayer.PlayString(string(data[pos:chunkEnd]))
		pos = chunkEnd
		frameNum++

		// Dump grid for first few frames and any frame with box drawing
		grid := replayer.VTerm().Grid()
		height := rec.Metadata.Height
		width := rec.Metadata.Width

		// Check if this frame has box-drawing characters
		hasBoxDrawing := false
		for row := 0; row < height && row < len(grid); row++ {
			for col := 0; col < width && col < len(grid[row]); col++ {
				r := grid[row][col].Rune
				if r >= 0x2500 && r <= 0x257F {
					hasBoxDrawing = true
					break
				}
			}
			if hasBoxDrawing { break }
		}

		if frameNum <= 5 || hasBoxDrawing {
			t.Logf("\n=== Frame %d (byte %d) ===", frameNum, chunkEnd)
			t.Log(testutil.GridToStringWithCursor(grid, replayer.VTerm().Cursor()))
		}
	}

	t.Logf("Total frames: %d", frameNum)
}
```

The `bytes` import will be needed — add it to the import block.

**Step 5: Commit diagnostic tests**

```bash
git add apps/texelterm/testutil/claude_code_debug_test.go
git commit -m "Add Claude Code status bar and frame-by-frame analysis"
```

---

### Task 4: Identify Root Cause

This task depends on the diagnostic output from Task 3. The analysis will reveal one of these scenarios:

**Scenario A: Missing escape sequence**
- Symptom: Characters appearing in tmux that are missing in direct, or vice versa
- Action: Find the unhandled sequence in the escape sequence log, add handling in `parser/vterm.go` or `parser/vterm_modes.go`

**Scenario B: Character width mismatch**
- Symptom: All characters after a certain column are shifted by 1+ positions
- Action: Find the character whose width differs between `go-runewidth` and Claude Code's `string-width`, add an override or fix

**Scenario C: Cursor positioning on primary screen**
- Symptom: Characters placed at wrong row/col positions
- Action: Fix CUP handling in `parser/vterm_memory_buffer.go`

**Scenario D: Erase operation issue**
- Symptom: Old characters persist where they should have been erased
- Action: Fix the relevant erase method in `parser/memory_buffer.go`

**Step 1: Document findings**

After running the diagnostic tests, document the exact root cause in the test file as comments.

**Step 2: Create a minimal reproduction**

Create a synthetic `Recording` that reproduces the bug with just the problematic escape sequences (no full Claude Code capture needed). Follow the pattern of `TestCodexExactSequence` (line 1051 of `codex_debug_test.go`):

```go
// TestClaudeCodeMinimalRepro reproduces the rendering bug with minimal sequences.
func TestClaudeCodeMinimalRepro(t *testing.T) {
	width, height := 200, 50
	rec := testutil.NewRecording(width, height)

	// Add the minimal escape sequences that reproduce the bug
	// (filled in after analysis in Task 3)
	// rec.AppendCSI("...")
	// rec.AppendText("...")

	replayer := testutil.NewReplayer(rec)
	replayer.PlayAll()
	grid := replayer.Grid()

	// Assert expected grid state
	// (specific assertions based on root cause)
	_ = grid
}
```

**Step 3: Commit**

```bash
git add apps/texelterm/testutil/claude_code_debug_test.go
git commit -m "Add minimal reproduction for Claude Code rendering bug"
```

---

### Task 5: Implement Fix

**Files:** Depends on root cause (Task 4). Most likely one of:
- `apps/texelterm/parser/vterm.go` — escape sequence handling
- `apps/texelterm/parser/vterm_modes.go` — mode handling
- `apps/texelterm/parser/vterm_memory_buffer.go` — primary screen operations
- `apps/texelterm/parser/memory_buffer.go` — memory buffer cell operations

**Step 1: Write the failing test**

The minimal reproduction from Task 4 should already fail. Verify:

Run: `go test -v -run TestClaudeCodeMinimalRepro -timeout 10s ./apps/texelterm/testutil/`
Expected: FAIL with specific assertion about wrong character at wrong position.

**Step 2: Implement the fix**

Apply the fix to the identified parser file. Keep the change minimal and focused.

**Step 3: Run the minimal repro to verify it passes**

Run: `go test -v -run TestClaudeCodeMinimalRepro -timeout 10s ./apps/texelterm/testutil/`
Expected: PASS

**Step 4: Run the full comparison to verify**

Run: `go test -v -run TestClaudeCodeSavedCompare -timeout 10s ./apps/texelterm/testutil/`
Expected: Fewer or no significant differences.

**Step 5: Run the full test suite to check for regressions**

Run: `make test`
Expected: All existing tests pass.

**Step 6: Commit**

```bash
git add <modified-parser-file>
git commit -m "Fix Claude Code rendering bug: <specific description>"
```

---

### Task 6: Verify Live

**Step 1: Build texelterm**

Run: `make build`

**Step 2: Run Claude Code inside texelterm and visually verify**

Launch texelterm, run `claude` from home directory, verify the welcome screen renders correctly:
- Outer box borders complete
- Bottom status bar with correct number of `│` characters
- No extra artifacts

**Step 3: Run the full capture comparison one more time**

Run: `go test -v -run TestClaudeCodeDirectVsTmux -timeout 60s ./apps/texelterm/testutil/`
Expected: Grid match (or only expected randomized-content differences).

**Step 4: Final commit with test data if needed**

If the test data file is small enough to check in:
```bash
git add apps/texelterm/parser/testdata/claude-code-session.txt
git commit -m "Add Claude Code session test data"
```

---

## Notes

- Claude Code exits cleanly with `/exit` command or Escape then `y`. The InteractiveCapture's `Close()` sends Ctrl+C which should also work.
- The `InteractiveCapture.handleDSR` responds to DA queries with `ESC[?1;2c` (basic VT100). Claude Code may query for more capabilities. Check if adding DECRQM responses for mode 2026 in the capture helps (the VTerm parser already handles this, but the capture's handleDSR may not).
- If Claude Code's welcome screen is timing-sensitive (renders in stages), increase `claudeCodeWait` or use `WaitForOutput` to detect when the welcome screen is complete.
- All `\tmp` recordings can be re-analyzed without recapturing using the `*Saved*` test variants.

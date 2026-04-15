// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/scroll_region_test.go
// Summary: Sparse-native ports of the DECSTBM scroll-region tests that used
// to live in vterm_memory_buffer_test.go. Covers both in-memory scroll
// behavior (header/footer preservation, full-screen vs partial-region
// advancement of writeTop) and reload integrity across CloseMemoryBuffer.
//
// Important sparse semantics that differ from the pre-sparse assertions:
//   - `NewlineInRegion` rotates content within [marginTop, marginBottom] and
//     does NOT advance writeTop; only full-screen LF (top==0 && bottom==h-1)
//     advances the write window. Pre-sparse tests asserted liveEdgeBase
//     growth for non-full-screen regions; those assertions are reframed here
//     around Grid() rotation and writeTop stability.
//   - After CloseMemoryBuffer()+reopen, the full pre-close content lives in
//     the PageStore plus the restored writeTop/cursor. Verifying "reload
//     correctness" therefore compares Grid() and MainScreenState across the
//     close/reopen boundary.

package parser

import (
	"fmt"
	"strings"
	"testing"
)

// captureGridStrings returns each row of Grid() as a trimmed string.
// Used to compare visible terminal state before and after reload.
func captureGridStrings(v *VTerm) []string {
	grid := v.Grid()
	out := make([]string, len(grid))
	for y := range grid {
		out[y] = trimRight(cellsToString(grid[y]))
	}
	return out
}

// TestVTerm_ScrollRegion_NoHeader verifies a scroll region pinned at the top
// (rows 0..height-2) rotates content in place while preserving the footer.
// In sparse, a non-full-screen region does NOT advance writeTop — the top
// line is overwritten by the rotation, not pushed into scrollback.
func TestVTerm_ScrollRegion_NoHeader(t *testing.T) {
	width, height := 40, 6
	v := NewVTerm(width, height)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Footer on the last row.
	parseString(p, fmt.Sprintf("\x1b[%d;1H", height))
	parseString(p, "FOOTER")

	// Scroll region: rows 1..5 (1-indexed) == rows 0..4 (0-indexed).
	parseString(p, "\x1b[1;5r")
	parseString(p, "\x1b[1;1H")

	writeTopBefore := v.mainScreen.WriteTop()

	// Write 7 lines into a 5-row region, forcing 2 in-region scrolls.
	for i := 0; i < 7; i++ {
		parseString(p, fmt.Sprintf("Line-%c", 'A'+rune(i)))
		if i < 6 {
			parseString(p, "\n\r")
		}
	}

	grid := v.Grid()

	// writeTop must not change for a non-full-screen region.
	if got := v.mainScreen.WriteTop(); got != writeTopBefore {
		t.Errorf("writeTop changed for partial scroll region: before=%d after=%d", writeTopBefore, got)
	}

	// Footer preserved at the last row.
	if got, want := cellsToString(grid[height-1][:6]), "FOOTER"; got != want {
		t.Errorf("Footer corrupted: got %q, want %q", got, want)
	}

	// Region rotated: after 7 writes in a 5-line region, rows 0..4 should hold
	// Line-C, Line-D, Line-E, Line-F, Line-G (the first two dropped off the top).
	for y := 0; y < 5; y++ {
		got := cellsToString(grid[y][:6])
		want := fmt.Sprintf("Line-%c", 'C'+rune(y))
		if got != want {
			t.Errorf("row %d: got %q, want %q", y, got, want)
		}
	}
}

// TestVTerm_ScrollRegion_NoFooter verifies a scroll region pinned at the
// bottom (rows 1..height-1) rotates content in place while preserving the
// header on row 0.
func TestVTerm_ScrollRegion_NoFooter(t *testing.T) {
	width, height := 40, 6
	v := NewVTerm(width, height)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Header on row 0.
	parseString(p, "HEADER")

	// Scroll region: rows 2..6 (1-indexed) == rows 1..5 (0-indexed).
	parseString(p, "\x1b[2;6r")
	parseString(p, "\x1b[2;1H")

	writeTopBefore := v.mainScreen.WriteTop()

	for i := 0; i < 7; i++ {
		parseString(p, fmt.Sprintf("Line-%c", 'A'+rune(i)))
		if i < 6 {
			parseString(p, "\n\r")
		}
	}

	grid := v.Grid()

	if got := v.mainScreen.WriteTop(); got != writeTopBefore {
		t.Errorf("writeTop changed for partial scroll region: before=%d after=%d", writeTopBefore, got)
	}

	// Header preserved.
	if got, want := cellsToString(grid[0][:6]), "HEADER"; got != want {
		t.Errorf("Header corrupted: got %q, want %q", got, want)
	}

	// Rows 1..5 hold Line-C..Line-G.
	for y := 1; y <= 5; y++ {
		got := cellsToString(grid[y][:6])
		want := fmt.Sprintf("Line-%c", 'C'+rune(y-1))
		if got != want {
			t.Errorf("row %d: got %q, want %q", y, got, want)
		}
	}
}

// TestVTerm_ScrollRegion_MultipleScrollN exercises CSI 3S (scroll up 3) within
// a scroll region and verifies the rotation amount without touching writeTop.
func TestVTerm_ScrollRegion_MultipleScrollN(t *testing.T) {
	width, height := 40, 8
	v := NewVTerm(width, height)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	parseString(p, "HEADER")
	parseString(p, fmt.Sprintf("\x1b[%d;1H", height))
	parseString(p, "FOOTER")

	// Scroll region: rows 2..7 (1-indexed) == rows 1..6 (0-indexed).
	parseString(p, "\x1b[2;7r")
	parseString(p, "\x1b[2;1H")
	for i := 0; i < 6; i++ {
		parseString(p, fmt.Sprintf("Line-%c", 'A'+rune(i)))
		if i < 5 {
			parseString(p, "\n\r")
		}
	}

	writeTopBefore := v.mainScreen.WriteTop()
	parseString(p, "\x1b[3S") // Scroll up by 3

	if got := v.mainScreen.WriteTop(); got != writeTopBefore {
		t.Errorf("writeTop changed after CSI 3S in partial region: before=%d after=%d", writeTopBefore, got)
	}

	grid := v.Grid()

	if got, want := cellsToString(grid[0][:6]), "HEADER"; got != want {
		t.Errorf("header corrupted after CSI 3S: got %q, want %q", got, want)
	}
	if got, want := cellsToString(grid[height-1][:6]), "FOOTER"; got != want {
		t.Errorf("footer corrupted after CSI 3S: got %q, want %q", got, want)
	}

	// After rotating up by 3 in a region of 6 rows, rows 1..3 hold
	// Line-D..Line-F and rows 4..6 are blank.
	for y := 1; y <= 3; y++ {
		got := cellsToString(grid[y][:6])
		want := fmt.Sprintf("Line-%c", 'D'+rune(y-1))
		if got != want {
			t.Errorf("row %d after CSI 3S: got %q, want %q", y, got, want)
		}
	}
	for y := 4; y <= 6; y++ {
		got := trimRight(cellsToString(grid[y]))
		if got != "" {
			t.Errorf("row %d after CSI 3S: expected blank, got %q", y, got)
		}
	}
}

// TestVTerm_ScrollRegion_FullScreenUnchanged verifies that full-screen LFs
// (no DECSTBM margins) advance writeTop, the sole path that pushes lines into
// scrollback in the sparse model.
func TestVTerm_ScrollRegion_FullScreenUnchanged(t *testing.T) {
	v := NewVTerm(40, 5)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	writeTopBefore := v.mainScreen.WriteTop()

	for i := 0; i < 8; i++ {
		parseString(p, fmt.Sprintf("Line-%d", i))
		parseString(p, "\r\n")
	}

	writeTopAfter := v.mainScreen.WriteTop()
	if writeTopAfter == writeTopBefore {
		t.Error("writeTop should have advanced for full-screen scrolling")
	}

	// Line-0 is in scrollback at globalIdx 0.
	if got := readSparseLine(v, 0); got != "Line-0" {
		t.Errorf("scrollback[0]: got %q, want %q", got, "Line-0")
	}
}

// TestVTerm_ScrollRegion_ScrollDownUnchanged verifies that CSI T (scroll
// down within region) shifts content down without advancing writeTop.
func TestVTerm_ScrollRegion_ScrollDownUnchanged(t *testing.T) {
	width, height := 40, 10
	v := NewVTerm(width, height)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Scroll region rows 2..8 (1-indexed) == rows 1..7 (0-indexed).
	parseString(p, "\x1b[2;8r")
	parseString(p, "\x1b[2;1H")
	parseString(p, "AAA\r\n")
	parseString(p, "BBB\r\n")
	parseString(p, "CCC")

	writeTopBefore := v.mainScreen.WriteTop()
	parseString(p, "\x1b[T") // Scroll down (insert blank at top)
	writeTopAfter := v.mainScreen.WriteTop()

	if writeTopAfter != writeTopBefore {
		t.Errorf("writeTop changed for CSI T: before=%d after=%d", writeTopBefore, writeTopAfter)
	}
}

// emulateLongCodexSession replays a realistic Codex-style TUI that uses a
// scroll region on the main screen. Used by the reload tests to produce
// enough scrollback to exercise the persistence pipeline.
func emulateLongCodexSession(p *Parser, _, height, totalOutputLines, regionChanges int) {
	parseString(p, "\x1b[1;1H")
	parseString(p, "\x1b[48;2;65;69;76m")
	parseString(p, " Codex (research-preview) working on task...")
	parseString(p, "\x1b[K")
	parseString(p, "\x1b[0m")

	parseString(p, fmt.Sprintf("\x1b[%d;1H", height))
	parseString(p, "\x1b[48;2;65;69;76m")
	parseString(p, " > thinking...")
	parseString(p, "\x1b[K")
	parseString(p, "\x1b[0m")

	rcDiv := regionChanges
	if rcDiv < 1 {
		rcDiv = 1
	}
	linesPerRegion := totalOutputLines / rcDiv
	linesEmitted := 0

	for rc := 0; rc < regionChanges; rc++ {
		regionTop := 2 + (rc % 3)
		regionBottom := height - 1 - (rc % 2)
		if regionTop >= regionBottom {
			regionTop = 2
			regionBottom = height - 1
		}

		parseString(p, fmt.Sprintf("\x1b[%d;%dr", regionTop, regionBottom))
		parseString(p, "\x1b[1;1H")
		parseString(p, "\x1b[48;2;65;69;76m")
		parseString(p, fmt.Sprintf(" Codex [zone %d] top=%d bot=%d", rc, regionTop, regionBottom))
		parseString(p, "\x1b[K")
		parseString(p, "\x1b[0m")
		parseString(p, fmt.Sprintf("\x1b[%d;1H", regionTop))

		regionLines := linesPerRegion
		if rc == regionChanges-1 {
			regionLines = totalOutputLines - linesEmitted
		}

		for i := 0; i < regionLines; i++ {
			lineNum := linesEmitted + i
			switch lineNum % 5 {
			case 0:
				parseString(p, fmt.Sprintf("  [%03d] Reading file src/components/widget_%d.tsx...", lineNum, lineNum))
			case 1:
				parseString(p, fmt.Sprintf("  [%03d] Analyzing code patterns and dependencies", lineNum))
			case 2:
				parseString(p, fmt.Sprintf("  [%03d] +++ Modified: internal/api/handler.go (line %d)", lineNum, lineNum*3))
			case 3:
				parseString(p, fmt.Sprintf("  [%03d] --- Removed: legacy/compat_%d.go", lineNum, lineNum/10))
			case 4:
				parseString(p, fmt.Sprintf("  [%03d] Running tests... %d passed, 0 failed", lineNum, lineNum+42))
			}
			parseString(p, "\x1b[K")
			if i < regionLines-1 {
				parseString(p, "\r\n")
			}
		}
		linesEmitted += regionLines
	}

	parseString(p, "\x1b[r") // Reset scroll region
	parseString(p, fmt.Sprintf("\x1b[%d;1H", height-2))
	parseString(p, "\x1b[J")
	parseString(p, fmt.Sprintf("Tokens: %d | Cost: $%.2f | Duration: 45s\r\n", totalOutputLines*150, float64(totalOutputLines)*0.003))
}

// TestVTerm_ScrollRegion_ReloadCorruption verifies that a Codex-style TUI
// session with a scroll region followed by normal shell output survives a
// close/reopen cycle: Grid() and the write anchor match on both sides.
func TestVTerm_ScrollRegion_ReloadCorruption(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	terminalID := "test-scroll-reload"
	width, height := 80, 24

	var session1Grid []string
	var session1WriteTop, session1ContentEnd int64

	{
		v := newTestVTerm(t, width, height, tmpDir, terminalID)
		p := NewParser(v)

		// Pre-TUI shell output.
		parseString(p, "$ echo 'before TUI'\r\n")
		parseString(p, "before TUI\r\n")
		parseString(p, "$ ls -la\r\n")
		parseString(p, "total 42\r\n")
		parseString(p, "drwxr-xr-x  5 user user 4096 Feb  9 file1.txt\r\n")
		parseString(p, "drwxr-xr-x  3 user user 4096 Feb  9 file2.txt\r\n")
		parseString(p, "$ codex 'do something'\r\n")

		// TUI header + footer + scroll region.
		parseString(p, "\x1b[1;1H")
		parseString(p, "\x1b[48;2;65;69;76m")
		parseString(p, " Codex (research-preview) ")
		parseString(p, "\x1b[K\x1b[0m")
		parseString(p, fmt.Sprintf("\x1b[%d;1H", height))
		parseString(p, "\x1b[48;2;65;69;76m")
		parseString(p, " > Enter a prompt...")
		parseString(p, "\x1b[K\x1b[0m")
		parseString(p, fmt.Sprintf("\x1b[2;%dr", height-1))
		parseString(p, "\x1b[2;1H")
		for i := 0; i < 40; i++ {
			parseString(p, fmt.Sprintf("  codex-output-%03d: Working on task...", i))
			if i < 39 {
				parseString(p, "\n\r")
			}
		}

		// TUI exits: reset region + clear + normal output.
		parseString(p, "\x1b[r")
		parseString(p, fmt.Sprintf("\x1b[%d;1H", height-2))
		parseString(p, "\x1b[J")
		parseString(p, "Tokens used: 4567 | Cost: $0.12\r\n")
		parseString(p, "$ echo 'after TUI'\r\n")
		parseString(p, "after TUI\r\n")
		parseString(p, "$ cat results.txt\r\n")
		for i := 0; i < 10; i++ {
			parseString(p, fmt.Sprintf("result-line-%03d: data here\r\n", i))
		}
		parseString(p, "$ ")

		session1Grid = captureGridStrings(v)
		session1WriteTop = v.mainScreen.WriteTop()
		session1ContentEnd = v.mainScreen.ContentEnd()

		t.Logf("Session 1: writeTop=%d contentEnd=%d", session1WriteTop, session1ContentEnd)
		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("CloseMemoryBuffer: %v", err)
		}
	}

	{
		v := newTestVTerm(t, width, height, tmpDir, terminalID)
		session2WriteTop := v.mainScreen.WriteTop()
		session2ContentEnd := v.mainScreen.ContentEnd()
		session2Grid := captureGridStrings(v)

		t.Logf("Session 2: writeTop=%d contentEnd=%d", session2WriteTop, session2ContentEnd)

		if session2WriteTop != session1WriteTop {
			t.Errorf("writeTop drift: before=%d after=%d", session1WriteTop, session2WriteTop)
		}
		if session2ContentEnd != session1ContentEnd {
			t.Errorf("contentEnd drift: before=%d after=%d", session1ContentEnd, session2ContentEnd)
		}

		// Grid must match row-for-row after reload.
		gridMismatches := 0
		for y := 0; y < len(session1Grid) && y < len(session2Grid); y++ {
			if session1Grid[y] != session2Grid[y] {
				gridMismatches++
				if gridMismatches <= 10 {
					t.Errorf("grid row %d: before=%q after=%q", y, session1Grid[y], session2Grid[y])
				}
			}
		}

		// Post-reload typing should be visible without a "reset" kick.
		p := NewParser(v)
		parseString(p, "echo 'new content after reload'\r\n")
		parseString(p, "new content after reload\r\n")
		parseString(p, "$ ")

		newGrid := captureGridStrings(v)
		foundContent := false
		foundPrompt := false
		for _, row := range newGrid {
			if strings.Contains(row, "new content after reload") {
				foundContent = true
			}
			if strings.Contains(row, "$ ") {
				foundPrompt = true
			}
		}
		if !foundContent {
			t.Errorf("post-reload content not visible in Grid()")
		}
		if !foundPrompt {
			t.Errorf("prompt not visible after new writes")
		}

		// Content markers from session 1 must survive in scrollback/PageStore.
		ps := v.mainScreenPageStore
		if ps == nil {
			t.Fatal("pageStore nil after reload")
		}
		stored := readAllPageStoreLines(ps)
		joined := strings.Join(stored, "\n")
		for _, marker := range []string{"after TUI", "result-line-", "Tokens used:", "codex-output-"} {
			if !strings.Contains(joined, marker) {
				t.Errorf("marker %q missing from PageStore after reload", marker)
			}
		}

		v.CloseMemoryBuffer()
	}
}

// TestVTerm_ScrollRegion_ReloadMultipleTUISessions runs two TUI sessions
// back-to-back with normal content between them, then verifies all content
// persists to the PageStore and Grid snapshots align on reload.
func TestVTerm_ScrollRegion_ReloadMultipleTUISessions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	terminalID := "test-multi-tui"
	width, height := 80, 24

	var session1WriteTop, session1ContentEnd int64
	var session1Grid []string

	{
		v := newTestVTerm(t, width, height, tmpDir, terminalID)
		p := NewParser(v)

		parseString(p, "$ echo 'session start'\r\n")
		parseString(p, "session start\r\n")

		// First TUI session.
		parseString(p, "$ tui-app-1\r\n")
		parseString(p, "\x1b[1;1H=TUI1-HEADER=")
		parseString(p, fmt.Sprintf("\x1b[%d;1H=TUI1-FOOTER=", height))
		parseString(p, fmt.Sprintf("\x1b[2;%dr", height-1))
		parseString(p, "\x1b[2;1H")
		for i := 0; i < 30; i++ {
			parseString(p, fmt.Sprintf("tui1-line-%03d", i))
			if i < 29 {
				parseString(p, "\n\r")
			}
		}
		parseString(p, "\x1b[r")
		parseString(p, fmt.Sprintf("\x1b[%d;1H\x1b[J", height-2))
		parseString(p, "TUI1 exited\r\n")

		// Normal content between sessions.
		parseString(p, "$ echo 'between sessions'\r\n")
		parseString(p, "between sessions\r\n")
		for i := 0; i < 5; i++ {
			parseString(p, fmt.Sprintf("normal-line-%03d\r\n", i))
		}

		// Second TUI session, longer.
		parseString(p, "$ tui-app-2\r\n")
		parseString(p, "\x1b[1;1H=TUI2-HEADER=")
		parseString(p, fmt.Sprintf("\x1b[%d;1H=TUI2-FOOTER=", height))
		parseString(p, fmt.Sprintf("\x1b[2;%dr", height-1))
		parseString(p, "\x1b[2;1H")
		for i := 0; i < 50; i++ {
			parseString(p, fmt.Sprintf("tui2-line-%03d: longer content with more detail", i))
			if i < 49 {
				parseString(p, "\n\r")
			}
		}
		parseString(p, "\x1b[r")
		parseString(p, fmt.Sprintf("\x1b[%d;1H\x1b[J", height-2))
		parseString(p, "TUI2 exited\r\n")

		parseString(p, "$ echo 'final output'\r\n")
		parseString(p, "final output\r\n")
		parseString(p, "$ ")

		session1WriteTop = v.mainScreen.WriteTop()
		session1ContentEnd = v.mainScreen.ContentEnd()
		session1Grid = captureGridStrings(v)

		t.Logf("Session 1: writeTop=%d contentEnd=%d", session1WriteTop, session1ContentEnd)
		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("CloseMemoryBuffer: %v", err)
		}
	}

	// Verify PageStore directly before reload.
	{
		config := DefaultPageStoreConfig(tmpDir+"/"+terminalID+".hist3", terminalID)
		ps, err := OpenPageStore(config)
		if err != nil {
			t.Fatalf("OpenPageStore: %v", err)
		}
		t.Logf("PageStore lineCount=%d", ps.LineCount())
		ps.Close()
	}

	{
		v := newTestVTerm(t, width, height, tmpDir, terminalID)
		session2WriteTop := v.mainScreen.WriteTop()
		session2ContentEnd := v.mainScreen.ContentEnd()
		session2Grid := captureGridStrings(v)

		if session2WriteTop != session1WriteTop {
			t.Errorf("writeTop drift: before=%d after=%d", session1WriteTop, session2WriteTop)
		}
		if session2ContentEnd != session1ContentEnd {
			t.Errorf("contentEnd drift: before=%d after=%d", session1ContentEnd, session2ContentEnd)
		}

		gridMismatches := 0
		for y := 0; y < len(session1Grid) && y < len(session2Grid); y++ {
			if session1Grid[y] != session2Grid[y] {
				gridMismatches++
				if gridMismatches <= 5 {
					t.Errorf("grid row %d: before=%q after=%q", y, session1Grid[y], session2Grid[y])
				}
			}
		}

		// Confirm scrollback markers from both TUI sessions survive.
		ps := v.mainScreenPageStore
		if ps == nil {
			t.Fatal("pageStore nil")
		}
		stored := readAllPageStoreLines(ps)
		joined := strings.Join(stored, "\n")
		for _, marker := range []string{"tui1-line-", "tui2-line-", "TUI2 exited", "final output"} {
			if !strings.Contains(joined, marker) {
				t.Errorf("marker %q not found in PageStore after reload", marker)
			}
		}

		// TUI1 content must come before TUI2 content in the PageStore.
		firstTUI1, firstTUI2, lastTUI1 := -1, -1, -1
		for i, line := range stored {
			if strings.Contains(line, "tui1-line-") {
				if firstTUI1 < 0 {
					firstTUI1 = i
				}
				lastTUI1 = i
			}
			if strings.Contains(line, "tui2-line-") && firstTUI2 < 0 {
				firstTUI2 = i
			}
		}
		if firstTUI1 >= 0 && firstTUI2 >= 0 && lastTUI1 >= firstTUI2 {
			t.Errorf("TUI1 and TUI2 content overlap in PageStore: TUI1=[%d..%d] TUI2 starts at %d",
				firstTUI1, lastTUI1, firstTUI2)
		}

		// No suspicious consecutive-duplicate runs.
		maxDupe, dupeRun, dupeText := 0, 1, ""
		for i := 1; i < len(stored); i++ {
			if stored[i] != "" && stored[i] == stored[i-1] {
				dupeRun++
				if dupeRun > maxDupe {
					maxDupe = dupeRun
					dupeText = stored[i]
				}
			} else {
				dupeRun = 1
			}
		}
		if maxDupe > 2 {
			t.Errorf("%d consecutive duplicate lines after reload: %q", maxDupe, dupeText)
		}

		v.CloseMemoryBuffer()
	}
}

// TestVTerm_ScrollRegion_ReloadLongSession exercises a long Codex-style
// session (200 output lines, 4 region changes) plus substantial post-TUI
// shell output, then verifies reload correctness at scale.
func TestVTerm_ScrollRegion_ReloadLongSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	terminalID := "test-long-codex"
	width, height := 120, 30

	var session1WriteTop, session1ContentEnd int64
	var session1Grid []string

	{
		v := newTestVTerm(t, width, height, tmpDir, terminalID)
		p := NewParser(v)

		for i := 0; i < 20; i++ {
			parseString(p, fmt.Sprintf("$ command-%03d --flag=value\r\n", i))
			parseString(p, fmt.Sprintf("output from command %d\r\n", i))
		}
		parseString(p, "$ codex 'implement feature X with full test coverage'\r\n")

		emulateLongCodexSession(p, width, height, 200, 4)

		parseString(p, "$ git status\r\n")
		parseString(p, "On branch feature/x\r\n")
		parseString(p, "Changes to be committed:\r\n")
		for i := 0; i < 30; i++ {
			parseString(p, fmt.Sprintf("  modified: src/file_%03d.go\r\n", i))
		}
		parseString(p, "\r\n$ go test ./...\r\n")
		for i := 0; i < 50; i++ {
			parseString(p, fmt.Sprintf("ok  \tpkg/module_%03d\t%.3fs\r\n", i, float64(i)*0.1+0.05))
		}
		parseString(p, "PASS\r\n")
		parseString(p, "$ echo 'all tests passed'\r\nall tests passed\r\n")

		parseString(p, "$ codex 'add error handling'\r\n")
		emulateLongCodexSession(p, width, height, 50, 2)

		parseString(p, "$ git diff --stat\r\n")
		for i := 0; i < 15; i++ {
			parseString(p, fmt.Sprintf(" src/file_%03d.go | %d +++--\r\n", i, i+3))
		}
		parseString(p, " 15 files changed, 234 insertions(+), 89 deletions(-)\r\n")
		parseString(p, "$ git commit -m 'implement feature X'\r\n")
		parseString(p, "[feature/x abc1234] implement feature X\r\n")
		parseString(p, "$ ")

		session1WriteTop = v.mainScreen.WriteTop()
		session1ContentEnd = v.mainScreen.ContentEnd()
		session1Grid = captureGridStrings(v)

		t.Logf("Session 1: writeTop=%d contentEnd=%d", session1WriteTop, session1ContentEnd)
		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("CloseMemoryBuffer: %v", err)
		}
	}

	{
		v := newTestVTerm(t, width, height, tmpDir, terminalID)
		session2WriteTop := v.mainScreen.WriteTop()
		session2ContentEnd := v.mainScreen.ContentEnd()
		session2Grid := captureGridStrings(v)

		if session2WriteTop != session1WriteTop {
			t.Errorf("writeTop drift: before=%d after=%d", session1WriteTop, session2WriteTop)
		}
		if session2ContentEnd != session1ContentEnd {
			t.Errorf("contentEnd drift: before=%d after=%d", session1ContentEnd, session2ContentEnd)
		}

		gridMismatches := 0
		for y := 0; y < len(session1Grid) && y < len(session2Grid); y++ {
			if session1Grid[y] != session2Grid[y] {
				gridMismatches++
				if gridMismatches <= 5 {
					t.Errorf("grid row %d: before=%q after=%q", y, session1Grid[y], session2Grid[y])
				}
			}
		}

		// Post-reload typing works.
		p := NewParser(v)
		parseString(p, "echo 'post-reload test'\r\npost-reload test\r\n$ second command\r\nsecond output\r\n$ ")

		postGrid := captureGridStrings(v)
		foundPostReload, foundPrompt := false, false
		for _, row := range postGrid {
			if strings.Contains(row, "post-reload test") {
				foundPostReload = true
			}
			if strings.Contains(row, "$ ") {
				foundPrompt = true
			}
		}
		if !foundPostReload {
			t.Errorf("post-reload content not visible")
		}
		if !foundPrompt {
			t.Errorf("prompt not visible after new writes")
		}

		// Critical markers must survive to the PageStore.
		ps := v.mainScreenPageStore
		if ps == nil {
			t.Fatal("pageStore nil")
		}
		joined := strings.Join(readAllPageStoreLines(ps), "\n")
		for _, marker := range []string{"implement feature X", "git diff --stat", "15 files changed"} {
			if !strings.Contains(joined, marker) {
				t.Errorf("marker %q missing after reload", marker)
			}
		}

		v.CloseMemoryBuffer()
	}
}

// TestVTerm_ScrollRegion_MultiCycleReload runs 4 open/close cycles with a
// TUI session in each, verifying every reload matches the prior session's
// grid exactly — the canonical "phantom empty lines after reload" regression.
func TestVTerm_ScrollRegion_MultiCycleReload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	terminalID := "test-multicycle"
	width, height := 120, 30

	type sessionSnapshot struct {
		writeTop   int64
		contentEnd int64
		cursorX    int
		cursorY    int
		gridLines  []string
	}

	const numCycles = 4
	snaps := make([]sessionSnapshot, numCycles)

	for cycle := 0; cycle < numCycles; cycle++ {
		v := newTestVTerm(t, width, height, tmpDir, terminalID)

		if cycle > 0 {
			gridLines := captureGridStrings(v)
			prev := snaps[cycle-1]
			mismatches := 0
			for y := 0; y < len(gridLines) && y < len(prev.gridLines); y++ {
				if gridLines[y] != prev.gridLines[y] {
					mismatches++
					if mismatches <= 5 {
						t.Errorf("cycle %d reload row %d: expected=%q got=%q",
							cycle, y, prev.gridLines[y], gridLines[y])
					}
				}
			}
		}

		p := NewParser(v)
		for i := 0; i < 5; i++ {
			parseString(p, fmt.Sprintf("$ cmd-cycle%d-%d\r\n", cycle, i))
			parseString(p, fmt.Sprintf("output-cycle%d-%d\r\n", cycle, i))
		}
		parseString(p, fmt.Sprintf("$ codex 'task for cycle %d'\r\n", cycle))
		emulateLongCodexSession(p, width, height, 20+cycle*10, 1+cycle%2)

		parseString(p, fmt.Sprintf("$ echo 'cycle %d done'\r\ncycle %d done\r\n", cycle, cycle))
		parseString(p, "$ ls\r\nfile1.txt  file2.txt  file3.txt  README.md  Makefile\r\n$ ")

		snaps[cycle] = sessionSnapshot{
			writeTop:   v.mainScreen.WriteTop(),
			contentEnd: v.mainScreen.ContentEnd(),
			cursorX:    v.cursorX,
			cursorY:    v.cursorY,
			gridLines:  captureGridStrings(v),
		}

		t.Logf("Cycle %d: writeTop=%d contentEnd=%d cursor=(%d,%d)",
			cycle, snaps[cycle].writeTop, snaps[cycle].contentEnd, snaps[cycle].cursorX, snaps[cycle].cursorY)

		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("cycle %d close: %v", cycle, err)
		}
	}

	// Final reload: must match the last session exactly.
	{
		v := newTestVTerm(t, width, height, tmpDir, terminalID)
		last := snaps[numCycles-1]

		if got := v.mainScreen.ContentEnd(); got != last.contentEnd {
			t.Errorf("final reload contentEnd: got=%d expected=%d", got, last.contentEnd)
		}
		if got := v.mainScreen.WriteTop(); got != last.writeTop {
			t.Errorf("final reload writeTop: got=%d expected=%d", got, last.writeTop)
		}

		gridLines := captureGridStrings(v)
		mismatches := 0
		for y := 0; y < len(gridLines) && y < len(last.gridLines); y++ {
			if gridLines[y] != last.gridLines[y] {
				mismatches++
				if mismatches <= 5 {
					t.Errorf("final reload row %d: expected=%q got=%q", y, last.gridLines[y], gridLines[y])
				}
			}
		}

		// No multi-row empty gaps between content and the bottom of the grid.
		lastNonEmpty := -1
		for y := len(gridLines) - 1; y >= 0; y-- {
			if gridLines[y] != "" {
				lastNonEmpty = y
				break
			}
		}
		emptyGap, maxEmptyGap := 0, 0
		for y := 0; y <= lastNonEmpty; y++ {
			if gridLines[y] == "" {
				emptyGap++
			} else {
				if emptyGap > maxEmptyGap {
					maxEmptyGap = emptyGap
				}
				emptyGap = 0
			}
		}
		if maxEmptyGap > 1 {
			t.Errorf("phantom %d-row empty gap in Grid after final reload", maxEmptyGap)
		}

		// Post-reload typing still lands adjacent to the prompt.
		p := NewParser(v)
		parseString(p, "echo 'final check'\r\nfinal check\r\n$ ")

		post := captureGridStrings(v)
		finalCheckRow, promptRow := -1, -1
		for y := len(post) - 1; y >= 0; y-- {
			if strings.Contains(post[y], "$ ") && promptRow < 0 {
				promptRow = y
			}
			if strings.Contains(post[y], "final check") && !strings.Contains(post[y], "echo") && finalCheckRow < 0 {
				finalCheckRow = y
			}
		}
		if finalCheckRow >= 0 && promptRow > finalCheckRow {
			gap := 0
			for y := finalCheckRow + 1; y < promptRow; y++ {
				if post[y] == "" {
					gap++
				}
			}
			if gap > 0 {
				t.Errorf("%d empty line(s) between 'final check' (row %d) and prompt (row %d)",
					gap, finalCheckRow, promptRow)
			}
		}

		v.CloseMemoryBuffer()
	}
}

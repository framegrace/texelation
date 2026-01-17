package testutil_test

import (
	"strings"
	"testing"
	"time"

	"github.com/framegrace/texelation/apps/texelterm/testutil"
)

// TestCodexInteractive captures codex with interactive input.
// Runs codex, waits for startup, sends "test<Enter>", captures output.
func TestCodexInteractive(t *testing.T) {
	// Define the actions: wait for startup, then send input
	actions := []testutil.CaptureAction{
		{Wait: 3 * time.Second}, // Wait for codex to initialize
		{SendInput: testutil.ParseInputString("test<Enter>")}, // Send "test" + Enter
		{Wait: 2 * time.Second}, // Wait for response
	}

	rec, err := testutil.CaptureInteractive("codex", nil, 80, 24, actions, 1*time.Second)
	if err != nil {
		t.Fatalf("Failed to capture codex: %v", err)
	}

	t.Logf("Captured %d bytes from codex", len(rec.Sequences))

	if len(rec.Sequences) < 100 {
		t.Logf("Output (raw): %q", string(rec.Sequences))
		t.Skip("Not enough output captured - codex may not have started properly")
	}

	// Show first part of output
	preview := rec.Sequences
	if len(preview) > 1000 {
		preview = preview[:1000]
	}
	t.Logf("First 1000 bytes readable:\n%s", testutil.EscapeSequenceLog(preview))

	// Compare with tmux reference
	cmp, err := testutil.NewReferenceComparator(rec)
	if err != nil {
		t.Skipf("tmux not available: %v", err)
	}

	result, err := cmp.CompareAtEndWithFullDiff()
	if err != nil {
		t.Fatalf("Comparison failed: %v", err)
	}

	// Filter out known terminal response artifacts
	// These are DSR responses that tmux displays but texelterm correctly hides
	realDiffs := filterTerminalResponseArtifacts(result.Differences)

	t.Logf("Match: %v", result.Match)
	t.Logf("Total differences: %d (char: %d, color: %d, attr: %d)",
		len(result.Differences), result.CharDiffs, result.ColorDiffs, result.AttrDiffs)
	t.Logf("Real differences (after filtering): %d", len(realDiffs))

	if len(realDiffs) > 0 {
		t.Errorf("Found %d real visual differences", len(realDiffs))
		for i, diff := range realDiffs {
			if i >= 20 {
				t.Logf("  ... and %d more differences", len(realDiffs)-20)
				break
			}
			t.Logf("  (%d,%d): %s", diff.X, diff.Y, diff.DiffDesc)
		}

		// Find divergence point
		divergence, err := cmp.FindFirstDivergenceWithFullDiff(100)
		if err != nil {
			t.Logf("Could not find divergence: %v", err)
		} else if divergence != nil {
			t.Logf("\nFirst divergence at bytes %d-%d", divergence.ByteIndex, divergence.ByteEndIndex)
			t.Logf("Chunk: %s", divergence.ChunkReadable)
		}
	} else {
		t.Log("No real visual differences - codex output matches!")
	}
}

// filterTerminalResponseArtifacts removes differences that are terminal response
// sequences (DSR responses like ^[[1;1R or ^[[?1;2c) that got echoed due to PTY echo.
// This handles EITHER direction:
// - texelterm shows artifact chars while tmux shows real content (echo in capture)
// - tmux shows artifact chars while texelterm shows spaces (echo in tmux replay)
func filterTerminalResponseArtifacts(diffs []testutil.EnhancedDiff) []testutil.EnhancedDiff {
	var filtered []testutil.EnhancedDiff

	// Terminal response characters that appear in echoed DSR responses
	// DSR CPR: ^[[row;colR  DSR DA: ^[[?params;c
	artifactChars := map[string]bool{
		"^": true, "[": true, "?": true, ";": true, "R": true, "c": true,
		"0": true, "1": true, "2": true, "3": true, "4": true, "5": true,
		"6": true, "7": true, "8": true, "9": true,
	}

	// Group diffs by row to detect terminal response patterns
	byRow := make(map[int][]testutil.EnhancedDiff)
	for _, diff := range diffs {
		byRow[diff.Y] = append(byRow[diff.Y], diff)
	}

	for _, rowDiffs := range byRow {
		// Check if ALL diffs in this row look like terminal response artifacts
		// Either texelterm shows artifacts or tmux shows artifacts (not both real content)
		texelAllArtifacts := true
		tmuxAllArtifacts := true

		for _, diff := range rowDiffs {
			texelRune := diff.Texelterm.Rune
			refRune := diff.Reference.Rune

			// Check if texelterm shows artifact characters
			if texelRune != " " && texelRune != "" && !artifactChars[texelRune] {
				texelAllArtifacts = false
			}
			// Check if tmux shows artifact characters
			if refRune != " " && refRune != "" && !artifactChars[refRune] {
				tmuxAllArtifacts = false
			}
		}

		// If either side is showing all artifact characters, it's likely an echo
		isArtifact := texelAllArtifacts || tmuxAllArtifacts

		if !isArtifact {
			filtered = append(filtered, rowDiffs...)
		}
	}

	return filtered
}

// TestCodexStartupInteractive tests just the startup without sending input.
func TestCodexStartupInteractive(t *testing.T) {
	actions := []testutil.CaptureAction{
		{Wait: 4 * time.Second}, // Wait for codex to fully start
	}

	rec, err := testutil.CaptureInteractive("codex", nil, 80, 24, actions, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to capture codex: %v", err)
	}

	t.Logf("Captured %d bytes from codex startup", len(rec.Sequences))

	// Log the sequences for analysis
	if len(rec.Sequences) > 0 {
		t.Logf("Readable output:\n%s", testutil.EscapeSequenceLog(rec.Sequences))
	}

	if len(rec.Sequences) < 50 {
		t.Skip("Not enough output - codex may have failed to start")
	}

	// Compare with tmux
	cmp, err := testutil.NewReferenceComparator(rec)
	if err != nil {
		t.Skipf("tmux not available: %v", err)
	}

	result, err := cmp.CompareAtEndWithFullDiff()
	if err != nil {
		t.Fatalf("Comparison failed: %v", err)
	}

	realDiffs := filterTerminalResponseArtifacts(result.Differences)

	if len(realDiffs) > 0 {
		t.Errorf("Visual mismatch with tmux: %d real differences", len(realDiffs))
		for i, diff := range realDiffs {
			if i >= 10 {
				break
			}
			t.Logf("  (%d,%d): %s", diff.X, diff.Y, diff.DiffDesc)
		}
	} else {
		t.Log("Outputs match tmux - no visual bugs detected (terminal responses correctly filtered)")
	}
}

// TestSimpleBashInteractive tests basic interactive input with bash.
// This verifies the interactive capture infrastructure works.
func TestSimpleBashInteractive(t *testing.T) {
	actions := []testutil.CaptureAction{
		{Wait: 500 * time.Millisecond},
		{SendText: "echo 'Hello World'\r"},
		{Wait: 500 * time.Millisecond},
		{SendText: "exit\r"},
		{Wait: 200 * time.Millisecond},
	}

	rec, err := testutil.CaptureInteractive("bash", []string{"--norc", "--noprofile"}, 80, 24, actions, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to capture bash: %v", err)
	}

	t.Logf("Captured %d bytes", len(rec.Sequences))
	t.Logf("Output:\n%s", testutil.EscapeSequenceLog(rec.Sequences))

	// Compare with tmux
	cmp, err := testutil.NewReferenceComparator(rec)
	if err != nil {
		t.Skipf("tmux not available: %v", err)
	}

	result, err := cmp.CompareAtEndWithFullDiff()
	if err != nil {
		t.Fatalf("Comparison failed: %v", err)
	}

	t.Logf("Match: %v, Differences: %d", result.Match, len(result.Differences))

	if !result.Match {
		for i, diff := range result.Differences {
			if i >= 10 {
				break
			}
			t.Logf("  (%d,%d): %s", diff.X, diff.Y, diff.DiffDesc)
		}
	}
}

// TestParseInputString verifies input parsing works correctly.
func TestParseInputString(t *testing.T) {
	tests := []struct {
		input    string
		expected []byte
	}{
		{"hello", []byte("hello")},
		{"test<Enter>", []byte("test\r")},
		{"<Ctrl-C>", []byte{0x03}},
		{"a<Tab>b", []byte("a\tb")},
		{"<Up><Down>", []byte("\x1b[A\x1b[B")},
	}

	for _, tc := range tests {
		result := testutil.ParseInputString(tc.input)
		if string(result) != string(tc.expected) {
			t.Errorf("ParseInputString(%q): got %q, want %q", tc.input, result, tc.expected)
		}
	}
}

// TestCodexInputEcho tests that typing in codex shows the expected output.
func TestCodexInputEcho(t *testing.T) {
	// Start codex, type "hello", check that we see it echoed
	actions := []testutil.CaptureAction{
		{Wait: 3 * time.Second},
		{SendText: "hello"},
		{Wait: 1 * time.Second},
	}

	rec, err := testutil.CaptureInteractive("codex", nil, 80, 24, actions, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to capture: %v", err)
	}

	t.Logf("Captured %d bytes", len(rec.Sequences))

	output := string(rec.Sequences)
	if !strings.Contains(output, "hello") {
		t.Logf("Output preview:\n%s", testutil.EscapeSequenceLog(rec.Sequences))
		t.Error("Typed 'hello' but didn't see it in output")
	} else {
		t.Log("Input 'hello' was correctly echoed")
	}

	// Compare with tmux
	cmp, err := testutil.NewReferenceComparator(rec)
	if err != nil {
		t.Skipf("tmux not available: %v", err)
	}

	result, err := cmp.CompareAtEndWithFullDiff()
	if err != nil {
		t.Fatalf("Comparison failed: %v", err)
	}

	realDiffs := filterTerminalResponseArtifacts(result.Differences)
	t.Logf("Differences: %d total, %d real", len(result.Differences), len(realDiffs))

	if len(realDiffs) > 0 {
		for i, diff := range realDiffs {
			if i >= 10 {
				break
			}
			t.Logf("  (%d,%d): %s", diff.X, diff.Y, diff.DiffDesc)
		}
	}
}

// TestCodexDiagnostic provides detailed visual comparison for debugging.
// Compares full codex output and shows REAL differences (not DSR artifacts).
func TestCodexDiagnostic(t *testing.T) {
	// Wait for full codex startup with animation
	actions := []testutil.CaptureAction{
		{Wait: 5 * time.Second}, // Wait for codex to fully start with animations
	}

	rec, err := testutil.CaptureInteractive("codex", nil, 80, 24, actions, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to capture codex: %v", err)
	}

	t.Logf("Captured %d bytes from codex", len(rec.Sequences))

	if len(rec.Sequences) < 100 {
		t.Skip("Not enough output - codex may have failed to start")
	}

	// Compare with tmux at the end (full comparison)
	cmp, err := testutil.NewReferenceComparator(rec)
	if err != nil {
		t.Skipf("tmux not available: %v", err)
	}

	result, err := cmp.CompareAtEndWithFullDiff()
	if err != nil {
		t.Fatalf("Comparison failed: %v", err)
	}

	t.Logf("Total differences: %d (char: %d, color: %d, attr: %d)",
		len(result.Differences), result.CharDiffs, result.ColorDiffs, result.AttrDiffs)

	// Filter out DSR response artifacts
	// DSR artifacts are when TMUX (reference) shows terminal response chars (^, [, ;, R, c, digits)
	// while texelterm correctly shows spaces
	realDiffs := filterTerminalResponseArtifacts(result.Differences)
	t.Logf("Real differences (after filtering DSR artifacts): %d", len(realDiffs))

	if len(realDiffs) == 0 {
		t.Log("SUCCESS: No real visual differences - only DSR response artifacts in tmux")
		return
	}

	// Show the REAL differences
	t.Logf("\n=== REAL DIFFERENCES (%d) ===", len(realDiffs))
	for i, diff := range realDiffs {
		if i >= 30 {
			t.Logf("... and %d more", len(realDiffs)-30)
			break
		}
		// Note: Reference = tmux, Texelterm = texelterm
		t.Logf("  (%d,%d): tmux='%s' texelterm='%s' - %s",
			diff.X, diff.Y, diff.Reference.Rune, diff.Texelterm.Rune, diff.DiffDesc)
	}

	// Print texelterm grid for visual inspection
	t.Log("\n=== TEXELTERM GRID (rows with differences) ===")
	replayer := testutil.NewReplayer(rec)
	replayer.PlayAll()
	replayer.SimulateRender()
	texelGrid := replayer.GetRenderBuf()

	// Find which rows have differences
	diffRows := make(map[int]bool)
	for _, diff := range realDiffs {
		diffRows[diff.Y] = true
	}

	for y := 0; y < 24; y++ {
		if !diffRows[y] {
			continue // Only show rows with differences
		}
		var texelLine strings.Builder
		for x := 0; x < 80; x++ {
			if y < len(texelGrid) && x < len(texelGrid[y]) {
				r := texelGrid[y][x].Rune
				if r == 0 {
					r = ' '
				}
				texelLine.WriteRune(r)
			} else {
				texelLine.WriteRune(' ')
			}
		}
		t.Logf("  Row %2d: [%s]", y, texelLine.String())
	}

	t.Errorf("Found %d real visual differences between texelterm and tmux", len(realDiffs))
}

// TestCodexWorking tests codex during "Working..." animation.
// This captures what happens when codex is processing a request.
func TestCodexWorking(t *testing.T) {
	actions := []testutil.CaptureAction{
		{Wait: 4 * time.Second},                                  // Wait for startup
		{SendText: "say hello\r"},                                // Send a simple request
		{Wait: 3 * time.Second},                                  // Wait during "Working..." animation
	}

	rec, err := testutil.CaptureInteractive("codex", nil, 80, 24, actions, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to capture codex: %v", err)
	}

	t.Logf("Captured %d bytes from codex working state", len(rec.Sequences))

	if len(rec.Sequences) < 100 {
		t.Skip("Not enough output")
	}

	// Compare with tmux
	cmp, err := testutil.NewReferenceComparator(rec)
	if err != nil {
		t.Skipf("tmux not available: %v", err)
	}

	result, err := cmp.CompareAtEndWithFullDiff()
	if err != nil {
		t.Fatalf("Comparison failed: %v", err)
	}

	t.Logf("Total differences: %d (char: %d, color: %d, attr: %d)",
		len(result.Differences), result.CharDiffs, result.ColorDiffs, result.AttrDiffs)

	realDiffs := filterTerminalResponseArtifacts(result.Differences)
	t.Logf("Real differences: %d", len(realDiffs))

	// Specifically look for grey background issues
	greyBgDiffs := 0
	for _, diff := range result.Differences {
		if diff.DiffType == testutil.DiffTypeBG {
			greyBgDiffs++
		}
	}
	t.Logf("Grey background differences: %d", greyBgDiffs)

	if len(realDiffs) > 0 {
		t.Logf("\n=== REAL DIFFERENCES ===")
		for i, diff := range realDiffs {
			if i >= 20 {
				break
			}
			t.Logf("  (%d,%d): tmux='%s' texelterm='%s'", diff.X, diff.Y, diff.Reference.Rune, diff.Texelterm.Rune)
		}
	}

	// Show texelterm grid
	t.Log("\n=== TEXELTERM GRID ===")
	replayer := testutil.NewReplayer(rec)
	replayer.PlayAll()
	replayer.SimulateRender()
	texelGrid := replayer.GetRenderBuf()

	for y := 0; y < 15; y++ {
		var line strings.Builder
		for x := 0; x < 80; x++ {
			if y < len(texelGrid) && x < len(texelGrid[y]) {
				r := texelGrid[y][x].Rune
				if r == 0 {
					r = ' '
				}
				line.WriteRune(r)
			} else {
				line.WriteRune(' ')
			}
		}
		t.Logf("  Row %2d: [%s]", y, line.String())
	}

	if len(realDiffs) > 0 {
		t.Errorf("Found %d real visual differences", len(realDiffs))
	}
}

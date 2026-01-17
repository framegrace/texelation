package testutil_test

import (
	"testing"
	"time"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/apps/texelterm/testutil"
)

// TestCodexDirtyTracking tests the dirty tracking during codex rendering.
// This test simulates the actual render flow: only updating dirty rows.
func TestCodexDirtyTracking(t *testing.T) {
	actions := []testutil.CaptureAction{
		{Wait: 3 * time.Second},
		{SendInput: testutil.ParseInputString("test<Enter>")},
		{Wait: 2 * time.Second},
	}

	rec, err := testutil.CaptureInteractive("codex", nil, 80, 24, actions, 1*time.Second)
	if err != nil {
		t.Fatalf("Failed to capture codex: %v", err)
	}

	t.Logf("Captured %d bytes from codex", len(rec.Sequences))

	if len(rec.Sequences) < 100 {
		t.Skip("Not enough output captured")
	}

	// Use the replayer with dirty tracking simulation
	replayer := testutil.NewReplayer(rec)

	// Play and render incrementally (this simulates actual render behavior)
	replayer.PlayAndRender()

	// Check if replayer detected any visual mismatches
	if replayer.HasVisualMismatch() {
		t.Error("Replayer detected visual mismatches (dirty tracking bug!):")
		vMismatches := replayer.FindVisualMismatches()
		for i, m := range vMismatches {
			if i >= 20 {
				t.Logf("  ... and %d more", len(vMismatches)-20)
				break
			}
			renderRune := m.Rendered.Rune
			logicalRune := m.Logical.Rune
			if renderRune == 0 {
				renderRune = ' '
			}
			if logicalRune == 0 {
				logicalRune = ' '
			}
			t.Logf("  (%d,%d): rendered='%c' (0x%x) vs logical='%c' (0x%x)",
				m.X, m.Y, renderRune, renderRune, logicalRune, logicalRune)
		}
	} else {
		t.Log("No dirty tracking mismatches - renderBuf matches Grid")
	}
}

// TestCodexStepByStepDirtyTracking steps through byte by byte looking for dirty issues
func TestCodexStepByStepDirtyTracking(t *testing.T) {
	actions := []testutil.CaptureAction{
		{Wait: 3 * time.Second},
	}

	rec, err := testutil.CaptureInteractive("codex", nil, 80, 24, actions, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to capture codex: %v", err)
	}

	t.Logf("Captured %d bytes from codex", len(rec.Sequences))

	if len(rec.Sequences) < 100 {
		t.Skip("Not enough output captured")
	}

	replayer := testutil.NewReplayer(rec)

	// Create render buffer
	renderBuf := make([][]parser.Cell, 24)
	for y := range renderBuf {
		renderBuf[y] = make([]parser.Cell, 80)
		for x := range renderBuf[y] {
			renderBuf[y][x] = parser.Cell{Rune: ' '}
		}
	}

	firstMismatch := -1
	stepWithMismatch := -1

	// Step through and render after each byte
	for step := 0; step < len(rec.Sequences); step++ {
		replayer.PlayBytes(1)

		// Simulate render
		grid := replayer.GetGrid()
		dirtyLines, allDirty := replayer.GetDirtyLines()

		if allDirty {
			for y := 0; y < 24 && y < len(grid); y++ {
				copy(renderBuf[y], grid[y])
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < 24 && y < len(grid) {
					copy(renderBuf[y], grid[y])
				}
			}
		}
		replayer.VTerm().ClearDirty()

		// Check for mismatches
		for y := 0; y < 24 && y < len(grid); y++ {
			for x := 0; x < 80 && x < len(grid[y]); x++ {
				renderedRune := renderBuf[y][x].Rune
				logicalRune := grid[y][x].Rune
				if renderedRune == 0 {
					renderedRune = ' '
				}
				if logicalRune == 0 {
					logicalRune = ' '
				}

				if renderedRune != logicalRune && firstMismatch < 0 {
					firstMismatch = step
					stepWithMismatch = step
					t.Logf("First mismatch at step %d, byte 0x%02x (%q)",
						step, rec.Sequences[step], string(rec.Sequences[step]))
					t.Logf("  Position (%d,%d): rendered='%c' vs logical='%c'",
						x, y, renderedRune, logicalRune)
					t.Logf("  allDirty=%v, dirtyLines=%v", allDirty, dirtyLines)

					// Show context around this byte
					start := step - 20
					if start < 0 {
						start = 0
					}
					end := step + 20
					if end > len(rec.Sequences) {
						end = len(rec.Sequences)
					}
					t.Logf("  Context: %s", testutil.EscapeSequenceLog(rec.Sequences[start:end]))
				}
			}
		}
	}

	if firstMismatch < 0 {
		t.Log("No dirty tracking issues found during step-by-step replay")
	} else {
		t.Errorf("Dirty tracking issue found at step %d", stepWithMismatch)
	}
}

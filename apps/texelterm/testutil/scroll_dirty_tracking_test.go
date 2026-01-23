package testutil_test

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/apps/texelterm/testutil"
)

// TestScrollRegionDirtyTracking tests that scroll regions properly mark lines dirty.
// This is important for TUI applications that use custom scroll regions.
func TestScrollRegionDirtyTracking(t *testing.T) {
	// Create a recording that simulates TUI-style scroll region usage
	rec := testutil.NewRecording(40, 10)

	// Fill the screen with identifiable content
	for i := 0; i < 10; i++ {
		rec.AppendText("Row")
		rec.AppendString(string('0' + byte(i)))
		rec.AppendCSI("K") // Clear to end of line
		if i < 9 {
			rec.AppendCRLF()
		}
	}

	// Set a scroll region (rows 4-8, 1-indexed = rows 3-7, 0-indexed)
	rec.AppendCSI("4;8r")
	// Move cursor to row 3 (0-indexed, which is row 4 in 1-indexed)
	rec.AppendCSI("4;1H")

	// Now do some reverse index operations (ESC M) like TUI apps do
	rec.AppendSequence([]byte{0x1b, 'M'}) // ESC M = Reverse Index
	rec.AppendSequence([]byte{0x1b, 'M'}) // Another one
	rec.AppendText("New") // Write some content

	replayer := testutil.NewReplayer(rec)

	// Create render buffer
	renderBuf := make([][]parser.Cell, 10)
	for y := range renderBuf {
		renderBuf[y] = make([]parser.Cell, 40)
		for x := range renderBuf[y] {
			renderBuf[y][x] = parser.Cell{Rune: ' '}
		}
	}

	// Step through and render after each byte
	for step := 0; step < len(rec.Sequences); step++ {
		replayer.PlayBytes(1)

		// Simulate render
		grid := replayer.GetGrid()
		dirtyLines, allDirty := replayer.DirtyLines()

		if allDirty {
			for y := 0; y < 10 && y < len(grid); y++ {
				copy(renderBuf[y], grid[y])
			}
		} else {
			for y := range dirtyLines {
				if y >= 0 && y < 10 && y < len(grid) {
					copy(renderBuf[y], grid[y])
				}
			}
		}
		replayer.VTerm().ClearDirty()

		// Check for mismatches
		for y := 0; y < 10 && y < len(grid); y++ {
			for x := 0; x < 40 && x < len(grid[y]); x++ {
				renderedRune := renderBuf[y][x].Rune
				logicalRune := grid[y][x].Rune
				if renderedRune == 0 {
					renderedRune = ' '
				}
				if logicalRune == 0 {
					logicalRune = ' '
				}

				if renderedRune != logicalRune {
					t.Errorf("Mismatch at step %d, position (%d,%d): rendered='%c' vs logical='%c'",
						step, x, y, renderedRune, logicalRune)
					t.Logf("  Byte at step: 0x%02x", rec.Sequences[step])
					t.Logf("  allDirty=%v, dirtyLines=%v", allDirty, dirtyLines)

					// Show context
					t.Log("  Grid state:")
					for row := 0; row < 10 && row < len(grid); row++ {
						var line string
						for col := 0; col < 40 && col < len(grid[row]); col++ {
							r := grid[row][col].Rune
							if r == 0 {
								r = ' '
							}
							line += string(r)
						}
						marker := " "
						if row == y {
							marker = ">"
						}
						t.Logf("  %s[%d]: %s", marker, row, line)
					}
					return
				}
			}
		}
	}

	t.Log("No dirty tracking issues found with scroll regions")
}

// TestTUIStyleScrolling simulates common scroll patterns used by TUI apps.
func TestTUIStyleScrolling(t *testing.T) {
	// Create a recording that matches common TUI scroll behavior
	rec := testutil.NewRecording(80, 24)

	// Initial content
	for i := 0; i < 24; i++ {
		rec.AppendText("Line")
		if i < 10 {
			rec.AppendString(string('0' + byte(i)))
		} else {
			rec.AppendString(string('0' + byte(i/10)))
			rec.AppendString(string('0' + byte(i%10)))
		}
		if i < 23 {
			rec.AppendCRLF()
		}
	}

	// Set full-screen scroll region then reset to custom
	rec.AppendCSI("r")       // Reset to full screen
	rec.AppendCSI("1;24r")   // Full screen scroll region

	// Now set a partial scroll region like TUI apps do
	rec.AppendCSI("1;8r")    // Scroll region lines 1-8

	// Move to top of region
	rec.AppendCSI("1;1H")

	// Multiple reverse index operations (scroll down effect)
	for i := 0; i < 8; i++ {
		rec.AppendSequence([]byte{0x1b, 'M'}) // ESC M = Reverse Index
	}

	// Reset scroll region
	rec.AppendCSI("r")

	replayer := testutil.NewReplayer(rec)

	// Play all and render
	replayer.PlayAndRender()

	// Check for visual mismatches
	if replayer.HasVisualMismatch() {
		t.Error("Visual mismatches found in TUI-style scrolling:")
		mismatches := replayer.FindVisualMismatches()
		for i, m := range mismatches {
			if i >= 20 {
				t.Logf("  ... and %d more", len(mismatches)-20)
				break
			}
			t.Logf("  (%d,%d): rendered='%c' vs logical='%c'",
				m.X, m.Y, m.Rendered.Rune, m.Logical.Rune)
		}
	} else {
		t.Log("No visual mismatches - TUI-style scrolling works correctly")
	}
}

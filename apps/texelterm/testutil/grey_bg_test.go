package testutil_test

import (
	"fmt"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/apps/texelterm/testutil"
)

// TestGreyBackgroundAfterScroll tests that grey background doesn't persist
// after scroll operations when the app redraws with default background.
// This simulates what TUI apps do during animations.
func TestGreyBackgroundAfterScroll(t *testing.T) {
	rec := testutil.NewRecording(50, 24)

	// 1. Set grey background (SGR 48;5;240 = grey256)
	rec.AppendCSI("48;5;240m")

	// 2. Draw content with grey background in rows 10-15
	for row := 10; row <= 15; row++ {
		rec.AppendCSI(fmt.Sprintf("%d;1H", row)) // Move to row
		rec.AppendText("Grey background content here")
	}

	// 3. Set scroll region covering these rows
	rec.AppendCSI("10;15r")

	// 4. Scroll down 2 lines (insert blanks at top of region)
	rec.AppendCSI("10;1H")
	rec.AppendCSI("2T")

	// 5. Reset scroll region
	rec.AppendCSI("r")

	// 6. Reset colors to default (SGR 0)
	rec.AppendCSI("0m")

	// 7. Redraw rows 10-12 with DEFAULT background
	for row := 10; row <= 12; row++ {
		rec.AppendCSI(fmt.Sprintf("%d;1H", row))
		rec.AppendCSI("K") // Erase to end of line (should use current/default BG)
		rec.AppendText("New content with default BG")
	}

	// Compare against tmux
	cmp, err := testutil.NewReferenceComparator(rec)
	if err != nil {
		t.Fatalf("Failed to create comparator: %v", err)
	}

	result, err := cmp.CompareAtEnd()
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}

	// Also get our grid for inspection
	replayer := testutil.NewReplayer(rec)
	replayer.PlayAll()
	grid := replayer.GetGrid()

	// Log row 10-15 content and BG colors
	t.Log("Inspecting rows 10-15:")
	for row := 10; row <= 15 && row < len(grid); row++ {
		var content string
		var hasBG string
		for x := 0; x < 30 && x < len(grid[row]); x++ {
			cell := grid[row][x]
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			content += string(r)
			if cell.BG.Mode != parser.ColorModeDefault {
				hasBG = "HAS_BG"
			}
		}
		t.Logf("  Row %d: %q %s", row, content, hasBG)
	}

	if !result.Match {
		t.Logf("Differences found: %d", len(result.Differences))
		for i, d := range result.Differences {
			if i > 15 {
				break
			}
			t.Logf("  (%d,%d): tmux=%q texel=%q", d.X, d.Y, string(d.Reference), string(d.Texelterm))
		}
		t.Error("Mismatch with tmux")
	} else {
		t.Log("Matches tmux - grey background handled correctly")
	}
}

// TestGreyBlocksAfterAnimationEnd simulates a common TUI pattern:
// Animation uses grey background, then ends and redraws normally.
func TestGreyBlocksAfterAnimationEnd(t *testing.T) {
	rec := testutil.NewRecording(80, 24)

	// === ANIMATION PHASE ===
	// Set grey background for animation area
	rec.AppendCSI("48;5;240m") // Grey background

	// Draw "Working..." animation box (rows 10-13)
	rec.AppendCSI("10;30H")
	rec.AppendText("┌─────────────┐")
	rec.AppendCSI("11;30H")
	rec.AppendText("│  Working..  │")
	rec.AppendCSI("12;30H")
	rec.AppendText("│             │")
	rec.AppendCSI("13;30H")
	rec.AppendText("└─────────────┘")

	// === ANIMATION ENDS ===
	// Reset to default colors
	rec.AppendCSI("0m")

	// Clear the animation area - this should use DEFAULT BG now
	for row := 10; row <= 13; row++ {
		rec.AppendCSI(fmt.Sprintf("%d;30H", row))
		rec.AppendCSI("15X") // Erase 15 characters (ECH)
	}

	// Draw new content
	rec.AppendCSI("10;30H")
	rec.AppendText("Done!")

	// Compare
	cmp, err := testutil.NewReferenceComparator(rec)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}

	result, err := cmp.CompareAtEnd()
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}

	replayer := testutil.NewReplayer(rec)
	replayer.PlayAll()
	grid := replayer.GetGrid()

	// Check if cells at animation location still have grey BG
	greyCount := 0
	t.Log("Checking animation area for residual grey background:")
	for row := 10; row <= 13 && row < len(grid); row++ {
		for x := 30; x < 45 && x < len(grid[row]); x++ {
			cell := grid[row][x]
			if cell.BG.Mode == parser.ColorMode256 && cell.BG.Value == 240 {
				greyCount++
				t.Logf("  GREY at (%d,%d): '%c'", x, row, cell.Rune)
			}
		}
	}

	if greyCount > 0 {
		t.Errorf("Found %d cells with residual grey background!", greyCount)
	}

	if !result.Match {
		t.Logf("Differences: %d", len(result.Differences))
		for i, d := range result.Differences {
			if i > 10 {
				break
			}
			t.Logf("  (%d,%d): tmux=%q texel=%q", d.X, d.Y, string(d.Reference), string(d.Texelterm))
		}
		t.Error("Mismatch with tmux")
	} else {
		t.Log("Matches tmux")
	}
}

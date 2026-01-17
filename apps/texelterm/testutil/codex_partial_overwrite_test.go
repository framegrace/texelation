package testutil_test

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/apps/texelterm/testutil"
)

// TestCodexPartialOverwrite tests the exact pattern from codex logs:
// 1. Row has content "› Implement {feature..."
// 2. Cursor moves to column 21, erase to end of line
// 3. Cursor moves to column 2, write "100% context left..."
// 4. Column 0-1 should have... what?
//
// In correct terminal behavior, column 0-1 retain their original content
// unless explicitly cleared. But codex expects them to be cleared somehow.
func TestCodexPartialOverwrite(t *testing.T) {
	rec := testutil.NewRecording(50, 24)

	// Set up row 20 with initial content
	rec.AppendCSI("21;1H") // Move to row 21 (1-indexed = row 20 0-indexed)
	rec.AppendText("› Implement {feature}")

	// Now do the partial overwrite pattern from codex:
	// 1. Move to column 21, erase to end
	rec.AppendCSI("21;22H") // Move to row 21, column 22 (1-indexed)
	rec.AppendCSI("K")      // Erase to end of line (EL 0)

	// 2. Move to column 2, write new content
	rec.AppendCSI("21;3H") // Move to row 21, column 3 (1-indexed = column 2 0-indexed)
	rec.AppendText("100% context left")

	// Compare with tmux
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

	// Show row 20 content
	row20 := ""
	for x := 0; x < 30 && x < len(grid[20]); x++ {
		r := grid[20][x].Rune
		if r == 0 {
			r = ' '
		}
		row20 += string(r)
	}
	t.Logf("Row 20 content: %q", row20)
	t.Logf("Column 0 char: %q", string(grid[20][0].Rune))
	t.Logf("Column 1 char: %q", string(grid[20][1].Rune))

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
		// Verify column 0-1 have original content (this is expected!)
		if grid[20][0].Rune == '›' {
			t.Log("Column 0 has '›' as expected (not cleared)")
		}
	}
}

// TestCodexPartialOverwriteWithLeadingErase tests if codex might be
// sending an erase from column 0 that we're not seeing/processing.
func TestCodexPartialOverwriteWithLeadingErase(t *testing.T) {
	rec := testutil.NewRecording(50, 24)

	// Set up row 20 with initial content
	rec.AppendCSI("21;1H")
	rec.AppendText("› Implement {feature}")

	// Pattern WITH proper leading erase:
	// 1. Move to column 0, erase to column 20
	rec.AppendCSI("21;1H")  // Move to start of row
	rec.AppendCSI("21X")    // Erase 21 characters (ECH) - columns 0-20

	// 2. Move to column 2, write new content
	rec.AppendCSI("21;3H")
	rec.AppendText("100% context left")

	// Compare with tmux
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

	// Show row 20 content
	row20 := ""
	for x := 0; x < 30 && x < len(grid[20]); x++ {
		r := grid[20][x].Rune
		if r == 0 {
			r = ' '
		}
		row20 += string(r)
	}
	t.Logf("Row 20 content: %q", row20)
	t.Logf("Column 0 char: %q (should be space)", string(grid[20][0].Rune))
	t.Logf("Column 1 char: %q (should be space)", string(grid[20][1].Rune))

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

// TestEL2EntireLine tests EL 2 (erase entire line)
func TestEL2EntireLine(t *testing.T) {
	rec := testutil.NewRecording(50, 24)

	// Set up row 20 with content
	rec.AppendCSI("21;1H")
	rec.AppendText("› Implement {feature}")

	// Use EL 2 to erase entire line
	rec.AppendCSI("21;22H") // Move somewhere in the middle
	rec.AppendCSI("2K")     // EL 2 - erase entire line

	// Write new content at column 2
	rec.AppendCSI("21;3H")
	rec.AppendText("100% context left")

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

	row20 := ""
	for x := 0; x < 30 && x < len(grid[20]); x++ {
		r := grid[20][x].Rune
		if r == 0 {
			r = ' '
		}
		row20 += string(r)
	}
	t.Logf("Row 20 after EL 2 and write: %q", row20)

	// After EL 2, entire line should be blank, then we write starting at column 2
	// So columns 0-1 should be spaces
	if grid[20][0].Rune != 0 && grid[20][0].Rune != ' ' {
		t.Errorf("Column 0 should be space after EL 2, got %q", string(grid[20][0].Rune))
	}

	if !result.Match {
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

// TestScrollWithPartialRedraw tests scroll followed by partial redraw
func TestScrollWithPartialRedraw(t *testing.T) {
	rec := testutil.NewRecording(50, 33)

	// Set up content at rows 18-20
	rec.AppendCSI("19;1H")
	rec.AppendText("› Row 18 original")
	rec.AppendCSI("20;1H")
	rec.AppendText("  Row 19 original")
	rec.AppendCSI("21;1H")
	rec.AppendText("  Row 20 original")

	// Set scroll region 15-33 and scroll down 2
	rec.AppendCSI("15;33r")
	rec.AppendCSI("15;1H")
	rec.AppendCSI("2T")  // Scroll down 2 lines
	rec.AppendCSI("r")   // Reset margins

	// Now row 20 should have what was at row 18: "› Row 18 original"
	// Redraw row 20 starting at column 2 (NOT column 0!)
	rec.AppendCSI("21;3H")  // Move to row 21, column 3 (0-indexed: row 20, col 2)
	rec.AppendText("New row 20 content")

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

	row20 := ""
	for x := 0; x < 30 && x < len(grid[20]); x++ {
		r := grid[20][x].Rune
		if r == 0 {
			r = ' '
		}
		row20 += string(r)
	}
	t.Logf("Row 20: %q", row20)
	t.Logf("Column 0: %q (from scroll)", string(grid[20][0].Rune))

	if !result.Match {
		t.Logf("Differences: %d", len(result.Differences))
		for i, d := range result.Differences {
			if i > 15 {
				break
			}
			t.Logf("  (%d,%d): tmux=%q texel=%q", d.X, d.Y, string(d.Reference), string(d.Texelterm))
		}
		t.Error("Mismatch with tmux - this indicates stale content handling differs")
	} else {
		t.Log("Matches tmux - stale content at column 0 is expected behavior")
		// The '›' at column 0 is expected - it was scrolled there and never cleared
		if grid[20][0].Rune == '›' {
			t.Log("Column 0 has '›' from scroll (expected per terminal semantics)")
		}
	}
}

func gridRowString(row []parser.Cell, maxLen int) string {
	s := ""
	for i := 0; i < maxLen && i < len(row); i++ {
		r := row[i].Rune
		if r == 0 {
			r = ' '
		}
		s += string(r)
	}
	return s
}

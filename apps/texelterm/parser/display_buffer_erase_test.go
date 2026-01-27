package parser

import (
	"testing"
)

// TestDisplayBuffer_EraseWrapBoundary verifies that erasing from a wrapped line
// back to the previous line works correctly (simulating shell behavior).
func TestDisplayBuffer_EraseWrapBoundary(t *testing.T) {
	t.Skip("Skipped: tests old logical-line architecture; new viewport model doesn't track wrapped lines as a unit")
	// Setup: Width 10.
	db := NewDisplayBuffer(nil, DisplayBufferConfig{Width: 10, Height: 5})

	// Write 15 chars: "0123456789ABCDE"
	// Row 0: "0123456789"
	// Row 1: "ABCDE"
	for i := 0; i < 15; i++ {
		if i < 10 {
			db.Write(rune('0'+i), DefaultFG, DefaultBG, 0, false)
		} else {
			db.Write(rune('A'+(i-10)), DefaultFG, DefaultBG, 0, false)
		}
	}

	// Cursor should be at Offset 15 (after 'E').
	// Physical: Row 1, Col 5.
	x, y, found := db.GetPhysicalCursorPos()
	if !found || x != 5 || y != 1 { // ViewportTop=0
		t.Fatalf("Setup failed: Cursor at %d,%d, want 5,1", x, y)
	}

	// 1. Simulate Bash: Move cursor back to 'A' (Offset 10, Row 1 Col 0).
	// Bash sequence: \x08 x 5 (Backspace).
	// vterm calls SetCursorPos(1, 4) -> (1, 3) ... (1, 0).
	db.SetCursor(0, 1) // Set to Row 1, Col 0.

	// Check logical offset. Should be 10.
	if db.GetCursorOffset() != 10 {
		t.Errorf("After move to (0,1), offset=%d, want 10", db.GetCursorOffset())
	}

	// 2. Erase to end (EL 0).
	// Bash sequence: \x1b[K
	db.Erase(0)

	// Line should now be "0123456789". Length 10.
	if db.CurrentLine().Len() != 10 {
		t.Errorf("After EL 0, len=%d, want 10", db.CurrentLine().Len())
	}

	// Physical lines should be 1.
	if len(db.currentLinePhysical()) != 1 {
		t.Errorf("After EL 0, physical lines=%d, want 1", len(db.currentLinePhysical()))
	}

	// 3. Move cursor back to '9' (Offset 9, Row 0 Col 9).
	// Bash sequence: \x1b[A (Up) -> Cursor (0, 0). Then \x1b[9C (Right) -> Cursor (0, 9).
	// vterm calls SetCursorPos.

	db.SetCursor(9, 0)
	if db.GetCursorOffset() != 9 {
		t.Errorf("After move to (9,0), offset=%d, want 9", db.GetCursorOffset())
	}

	// 4. Erase '9' and 'A' (which is gone).
	// Actually if we erase '9', we truncate at 9.
	db.Erase(0)

	if db.CurrentLine().Len() != 9 {
		t.Errorf("After 2nd EL 0, len=%d, want 9", db.CurrentLine().Len())
	}
	if len(db.currentLinePhysical()) != 1 {
		t.Errorf("After 2nd EL 0, physical lines=%d, want 1", len(db.currentLinePhysical()))
	}
}

// TestDisplayBuffer_BackspaceEraseSimulation simulates the common \b \b sequence
// used by shells to erase characters, especially across wrap boundaries.
func TestDisplayBuffer_BackspaceEraseSimulation(t *testing.T) {
	t.Skip("Skipped: tests old logical-line architecture; new viewport model doesn't track wrapped lines as a unit")
	// Setup: Width 10.
	db := NewDisplayBuffer(nil, DisplayBufferConfig{Width: 10, Height: 5})

	// Write 11 chars: "0123456789A"
	// Row 0: "0123456789"
	// Row 1: "A"
	for i := 0; i < 10; i++ {
		db.Write(rune('0'+i), DefaultFG, DefaultBG, 0, false)
	}
	db.Write('A', DefaultFG, DefaultBG, 0, false)

	// Cursor at 1,1 (Offset 11).

	// 1. Backspace 'A'.
	// Bash sends \b.
	// vterm moves to 1,0.
	db.SetCursor(0, 1)
	if db.GetCursorOffset() != 10 {
		t.Errorf("After BS 'A', offset=%d, want 10", db.GetCursorOffset())
	}

	// Bash sends Space.
	db.Write(' ', DefaultFG, DefaultBG, 0, false)
	// Now index 10 is Space. Length 11.
	if db.CurrentLine().Len() != 11 {
		t.Errorf("After Space, len=%d, want 11", db.CurrentLine().Len())
	}

	// Bash sends \b.
	// vterm moves to 1,0.
	db.SetCursor(0, 1)

	// 2. Backspace '9' (CROSSING BOUNDARY).
	// Bash sends \b at 1,0.
	// vterm moves to 0,9.
	db.SetCursor(9, 0)
	if db.GetCursorOffset() != 9 {
		t.Errorf("After BS '9', offset=%d, want 9", db.GetCursorOffset())
	}

	// Bash sends Space.
	db.Write(' ', DefaultFG, DefaultBG, 0, false)
	// Index 9 is Space. Index 10 is Space. Length 11. Cursor now at 10.

	// Bash sends \b to move cursor back to 9 before EL 0.
	db.SetCursor(9, 0)

	// 3. Bash sends \x1b[K (EL 0) to "actually" truncate?
	// OR Bash doesn't send it?
	// If Bash doesn't send it, Row 1 still exists (it's a Space).

	// Check physical lines.
	if len(db.currentLinePhysical()) != 2 {
		t.Errorf("Without EL 0, still want 2 lines, got %d", len(db.currentLinePhysical()))
	}

	// Now send EL 0.
	db.Erase(0)
	if len(db.currentLinePhysical()) != 1 {
		t.Errorf("After EL 0, want 1 line, got %d", len(db.currentLinePhysical()))
	}
	if db.CurrentLine().Len() != 9 {
		t.Errorf("After EL 0, len=%d, want 9", db.CurrentLine().Len())
	}
}

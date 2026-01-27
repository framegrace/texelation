package parser

import (
	"testing"
)

func TestCodexExitSequence(t *testing.T) {
	// Terminal is 25 lines high (as shown in the session)
	v := NewVTerm(153, 25)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Fill screen with some content to simulate codex UI
	for y := 0; y < 12; y++ {
		for x := 0; x < 40; x++ {
			p.Parse('X')
		}
		if y < 11 {
			p.Parse('\r')
			p.Parse('\n')
		}
	}

	t.Logf("After filling: cursor at (%d, %d)", v.CursorX(), v.CursorY())

	// Set scroll region to 1-12 (like codex does)
	for _, ch := range "\x1b[1;12r" {
		p.Parse(ch)
	}

	t.Logf("After scroll region: cursor at (%d, %d)",
		v.CursorX(), v.CursorY())

	// CUP to row 13, col 1 (1-indexed)
	for _, ch := range "\x1b[13;1H" {
		p.Parse(ch)
	}

	t.Logf("After CUP 13;1: cursor at (%d, %d)", v.CursorX(), v.CursorY())

	// Verify cursor is at row 12 (0-indexed)
	if v.CursorY() != 12 {
		t.Errorf("Expected cursor at row 12 (0-indexed), got %d", v.CursorY())
	}

	// ED 0 (erase from cursor to end of screen)
	for _, ch := range "\x1b[J" {
		p.Parse(ch)
	}

	t.Logf("After ED 0: cursor at (%d, %d)", v.CursorX(), v.CursorY())

	// Cursor should still be at row 12 after ED 0
	if v.CursorY() != 12 {
		t.Errorf("After ED 0: Expected cursor at row 12, got %d", v.CursorY())
	}

	// Now simulate shell drawing prompt
	for _, ch := range "$ " {
		p.Parse(ch)
	}

	t.Logf("After prompt: cursor at (%d, %d)", v.CursorX(), v.CursorY())

	// Check grid to see where content is
	grid := v.Grid()
	for y := 0; y < len(grid) && y < 15; y++ {
		var line string
		for x := 0; x < 45 && x < len(grid[y]); x++ {
			r := grid[y][x].Rune
			if r == 0 {
				r = ' '
			}
			line += string(r)
		}
		t.Logf("Row %2d: %q", y, line)
	}

	// The prompt should be on row 12, not row 0 or 1
	if grid[12][0].Rune != '$' {
		t.Errorf("Expected prompt '$' on row 12, got %q", string(grid[12][0].Rune))
	}
}

// TestCodexExitRealisticSequence tests the exact sequence from the codex session capture
func TestCodexExitRealisticSequence(t *testing.T) {
	// Terminal is 25 lines high, 153 columns (as shown in the session)
	v := NewVTerm(153, 25)
	v.EnableDisplayBuffer()
	p := NewParser(v)

	// Simulate the shell prompt before codex starts
	prompt := "$ codex"
	for _, ch := range prompt {
		p.Parse(ch)
	}
	p.Parse('\r')
	p.Parse('\n')

	t.Logf("Before codex: cursor at (%d, %d)", v.CursorX(), v.CursorY())

	// Codex startup sequences (simplified)
	// Set scroll region 5-25
	for _, ch := range "\x1b[5;25r" {
		p.Parse(ch)
	}
	// Move cursor to row 5
	for _, ch := range "\x1b[5;1H" {
		p.Parse(ch)
	}
	// Some reverse index operations (scroll down, making room at top)
	for i := 0; i < 8; i++ {
		p.Parse('\x1b')
		p.Parse('M') // Reverse Index
	}
	// Reset scroll region
	for _, ch := range "\x1b[r" {
		p.Parse(ch)
	}
	// Set scroll region 1-12
	for _, ch := range "\x1b[1;12r" {
		p.Parse(ch)
	}
	// Move to row 4
	for _, ch := range "\x1b[4;1H" {
		p.Parse(ch)
	}

	// Draw some codex UI content
	for y := 0; y < 8; y++ {
		for _, ch := range "╭────────────────────────────────────────╮" {
			p.Parse(ch)
		}
		p.Parse('\r')
		p.Parse('\n')
	}

	t.Logf("After codex UI: cursor at (%d, %d)", v.CursorX(), v.CursorY())

	// Now the exit sequence (from the session capture)
	// [13;1H[J[<1u[?2004l[?1004l[?25h[?2004h
	for _, ch := range "\x1b[13;1H" {
		p.Parse(ch)
	}
	t.Logf("After CUP 13;1: cursor at (%d, %d)", v.CursorX(), v.CursorY())

	for _, ch := range "\x1b[J" {
		p.Parse(ch)
	}
	t.Logf("After ED 0: cursor at (%d, %d)", v.CursorX(), v.CursorY())

	// The cursor should be at row 12 (0-indexed)
	if v.CursorY() != 12 {
		t.Errorf("Expected cursor at row 12 (0-indexed), got %d", v.CursorY())
	}

	// Disable bracketed paste mode, etc.
	for _, ch := range "\x1b[<1u" {
		p.Parse(ch)
	}
	for _, ch := range "\x1b[?2004l" {
		p.Parse(ch)
	}
	for _, ch := range "\x1b[?1004l" {
		p.Parse(ch)
	}
	for _, ch := range "\x1b[?25h" {
		p.Parse(ch)
	}
	for _, ch := range "\x1b[?2004h" {
		p.Parse(ch)
	}

	t.Logf("After mode changes: cursor at (%d, %d)", v.CursorX(), v.CursorY())

	// Now the shell draws the prompt (no explicit cursor positioning)
	for _, ch := range "$ " {
		p.Parse(ch)
	}

	t.Logf("After prompt: cursor at (%d, %d)", v.CursorX(), v.CursorY())

	// Check grid to see where content is
	grid := v.Grid()
	t.Log("Grid content:")
	for y := 0; y < len(grid); y++ {
		var line string
		hasContent := false
		for x := 0; x < 50 && x < len(grid[y]); x++ {
			r := grid[y][x].Rune
			if r == 0 {
				r = ' '
			}
			if r != ' ' {
				hasContent = true
			}
			line += string(r)
		}
		if hasContent || y < 15 {
			t.Logf("Row %2d: %q", y, line)
		}
	}

	// The prompt should be on row 12, not row 0 or 1
	if grid[12][0].Rune != '$' {
		t.Errorf("Expected prompt '$' on row 12, got %q (rune=%d)", string(grid[12][0].Rune), grid[12][0].Rune)
	}
}

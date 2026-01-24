// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/prompt_strategy_test.go
// Summary: Comprehensive tests for ShellAwarePromptStrategy and prompt detection.

package texelterm

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// mockVTermProviderForPrompt implements VTermProvider for prompt strategy tests.
type mockVTermProviderForPrompt struct {
	inputActive    bool
	inputStartLine int
	inputStartCol  int
	viewportRows   map[int][]parser.Cell
	historyLines   map[int][]parser.Cell
	currentLine    []parser.Cell
	grid           [][]parser.Cell
	contentText    string
}

func newMockVTermProviderForPrompt() *mockVTermProviderForPrompt {
	return &mockVTermProviderForPrompt{
		viewportRows: make(map[int][]parser.Cell),
		historyLines: make(map[int][]parser.Cell),
	}
}

func (m *mockVTermProviderForPrompt) InputActive() bool   { return m.inputActive }
func (m *mockVTermProviderForPrompt) InputStartLine() int { return m.inputStartLine }
func (m *mockVTermProviderForPrompt) InputStartCol() int  { return m.inputStartCol }
func (m *mockVTermProviderForPrompt) HistoryLineCopy(line int) []parser.Cell {
	return m.historyLines[line]
}
func (m *mockVTermProviderForPrompt) CurrentLineCells() []parser.Cell { return m.currentLine }
func (m *mockVTermProviderForPrompt) Grid() [][]parser.Cell           { return m.grid }
func (m *mockVTermProviderForPrompt) GetContentText(startLine int64, startOffset int, endLine int64, endOffset int) string {
	return m.contentText
}

func (m *mockVTermProviderForPrompt) ViewportRow(row int) []parser.Cell {
	if cells, ok := m.viewportRows[row]; ok {
		return cells
	}
	if m.grid != nil && row >= 0 && row < len(m.grid) {
		return m.grid[row]
	}
	return nil
}

// cellsFromStringPrompt creates cells from a string for testing.
func cellsFromStringPrompt(s string) []parser.Cell {
	cells := make([]parser.Cell, len(s))
	for i, r := range s {
		cells[i] = parser.Cell{Rune: r}
	}
	return cells
}

// TestDetectPromptEnd_CommonPrompts tests detection of common shell prompts.
func TestDetectPromptEnd_CommonPrompts(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected int
	}{
		{"bash dollar", "$ command", 2},
		{"root hash", "# command", 2},
		{"zsh percent", "% command", 2},
		{"csh greater", "> command", 2},
		{"user at host", "user@host:~$ command", 13},
		{"with path", "[user@host ~/dir]$ command", 19},
		{"fish prompt", "user@host ~/dir> command", 17},
		{"colon prompt", "prompt: command", 8},
		{"paren prompt", "(env) $ command", 6}, // Detects at ")" followed by space first
		{"bracket prompt", "[prompt]$ command", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cells := cellsFromStringPrompt(tt.line)
			got := DetectPromptEnd(cells)
			if got != tt.expected {
				t.Errorf("DetectPromptEnd(%q) = %d, want %d", tt.line, got, tt.expected)
			}
		})
	}
}

// TestDetectPromptEnd_NoPrompt tests lines that don't have prompts.
func TestDetectPromptEnd_NoPrompt(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"empty", ""},
		{"single char", "a"},
		{"command output", "file1.txt  file2.txt"},
		{"alphanumeric start", "hello world"},
		{"number start", "123 items"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cells := cellsFromStringPrompt(tt.line)
			got := DetectPromptEnd(cells)
			if got != 0 {
				t.Errorf("DetectPromptEnd(%q) = %d, want 0 (no prompt)", tt.line, got)
			}
		})
	}
}

// TestDetectPromptEnd_EdgeCases tests edge cases.
func TestDetectPromptEnd_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected int
	}{
		{"just dollar space", "$ ", 2},
		{"dollar at end", "$", 0},          // No space after, not detected
		{"space then dollar", " $ cmd", 3}, // Space before prompt
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cells := cellsFromStringPrompt(tt.line)
			got := DetectPromptEnd(cells)
			if got != tt.expected {
				t.Errorf("DetectPromptEnd(%q) = %d, want %d", tt.line, got, tt.expected)
			}
		})
	}
}

// TestShellAwarePromptStrategy_OSC133 tests prompt detection via OSC 133.
func TestShellAwarePromptStrategy_OSC133(t *testing.T) {
	mock := newMockVTermProviderForPrompt()
	mock.inputActive = true
	mock.inputStartLine = 5
	mock.inputStartCol = 10

	strategy := NewShellAwarePromptStrategy(mock)

	// Query the line with OSC 133 active
	promptEnd := strategy.GetPromptEnd(5)
	if promptEnd != 10 {
		t.Errorf("GetPromptEnd(5) with OSC 133 = %d, want 10", promptEnd)
	}

	// Different line should fall back to pattern detection
	mock.viewportRows[3] = cellsFromStringPrompt("$ command")
	promptEnd = strategy.GetPromptEnd(3)
	if promptEnd != 2 {
		t.Errorf("GetPromptEnd(3) without OSC 133 = %d, want 2", promptEnd)
	}
}

// TestShellAwarePromptStrategy_NoOSC133 tests fallback to pattern detection.
func TestShellAwarePromptStrategy_NoOSC133(t *testing.T) {
	mock := newMockVTermProviderForPrompt()
	mock.inputActive = false // OSC 133 not active
	mock.viewportRows[0] = cellsFromStringPrompt("user@host:~$ ls -la")

	strategy := NewShellAwarePromptStrategy(mock)

	promptEnd := strategy.GetPromptEnd(0)
	if promptEnd != 13 { // After "user@host:~$ "
		t.Errorf("GetPromptEnd(0) = %d, want 13", promptEnd)
	}
}

// TestShellAwarePromptStrategy_GetSelectionStart tests Shift modifier behavior.
func TestShellAwarePromptStrategy_GetSelectionStart(t *testing.T) {
	mock := newMockVTermProviderForPrompt()
	mock.inputActive = true
	mock.inputStartLine = 5
	mock.inputStartCol = 10

	strategy := NewShellAwarePromptStrategy(mock)

	// Without Shift: should skip prompt
	start := strategy.GetSelectionStart(5, false)
	if start != 10 {
		t.Errorf("GetSelectionStart without Shift = %d, want 10 (after prompt)", start)
	}

	// With Shift: should include prompt (start at 0)
	start = strategy.GetSelectionStart(5, true)
	if start != 0 {
		t.Errorf("GetSelectionStart with Shift = %d, want 0 (include prompt)", start)
	}
}

// TestShellAwarePromptStrategy_IsOnPrompt tests prompt area detection.
func TestShellAwarePromptStrategy_IsOnPrompt(t *testing.T) {
	mock := newMockVTermProviderForPrompt()
	mock.inputActive = true
	mock.inputStartLine = 5
	mock.inputStartCol = 10

	strategy := NewShellAwarePromptStrategy(mock)

	tests := []struct {
		line     int
		col      int
		expected bool
	}{
		{5, 0, true},   // On prompt
		{5, 5, true},   // On prompt
		{5, 9, true},   // On prompt
		{5, 10, false}, // At prompt end (not on prompt)
		{5, 15, false}, // After prompt
		{3, 0, false},  // Different line (no OSC 133)
	}

	for _, tt := range tests {
		got := strategy.IsOnPrompt(tt.line, tt.col)
		if got != tt.expected {
			t.Errorf("IsOnPrompt(line=%d, col=%d) = %v, want %v",
				tt.line, tt.col, got, tt.expected)
		}
	}
}

// TestShellAwarePromptStrategy_NilProvider tests handling of nil provider.
func TestShellAwarePromptStrategy_NilProvider(t *testing.T) {
	strategy := NewShellAwarePromptStrategy(nil)

	// Should not panic, should return 0
	promptEnd := strategy.GetPromptEnd(0)
	if promptEnd != 0 {
		t.Errorf("GetPromptEnd with nil provider = %d, want 0", promptEnd)
	}
}

// TestIsPromptEndingChar tests prompt ending character detection.
func TestIsPromptEndingChar(t *testing.T) {
	promptChars := []rune{'$', '>', '#', '%', ':', ')', ']'}
	nonPromptChars := []rune{'a', 'z', '0', '9', ' ', '-', '_', '@', '/'}

	for _, r := range promptChars {
		if !isPromptEndingChar(r) {
			t.Errorf("isPromptEndingChar(%q) = false, want true", r)
		}
	}

	for _, r := range nonPromptChars {
		if isPromptEndingChar(r) {
			t.Errorf("isPromptEndingChar(%q) = true, want false", r)
		}
	}
}

// TestIsAlphanumeric tests alphanumeric character detection.
func TestIsAlphanumeric(t *testing.T) {
	tests := []struct {
		r        rune
		expected bool
	}{
		{'a', true},
		{'z', true},
		{'A', true},
		{'Z', true},
		{'0', true},
		{'9', true},
		{'_', false}, // Underscore is not alphanumeric
		{'-', false},
		{' ', false},
		{'@', false},
		{'$', false},
	}

	for _, tt := range tests {
		got := isAlphanumeric(tt.r)
		if got != tt.expected {
			t.Errorf("isAlphanumeric(%q) = %v, want %v", tt.r, got, tt.expected)
		}
	}
}

// TestHasPromptPattern tests prompt pattern detection.
func TestHasPromptPattern(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{"with at", "user@host", true},
		{"with colon", "host:", true},
		{"with tilde", "~/dir", true},
		{"with slash", "/path/to", true},
		{"with brackets", "[env]", true},
		{"with parens", "(venv)", true},
		{"plain text", "hello", false},
		{"numbers", "12345", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cells := cellsFromStringPrompt(tt.line)
			got := hasPromptPattern(cells)
			if got != tt.expected {
				t.Errorf("hasPromptPattern(%q) = %v, want %v", tt.line, got, tt.expected)
			}
		})
	}
}

// TestVTermAdapter_NilVTerm tests that nil vterm is handled safely.
func TestVTermAdapter_NilVTerm(t *testing.T) {
	adapter := NewVTermAdapter(nil)
	if adapter != nil {
		t.Error("expected nil adapter for nil vterm")
	}
}

// TestVTermGridAdapter_NilVTerm tests that nil vterm is handled safely.
func TestVTermGridAdapter_NilVTerm(t *testing.T) {
	adapter := NewVTermGridAdapter(nil)
	if adapter != nil {
		t.Error("expected nil adapter for nil vterm")
	}
}

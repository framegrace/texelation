// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/prompt_strategy.go
// Summary: Prompt boundary detection with Shift modifier awareness.

package texelterm

import "github.com/framegrace/texelation/apps/texelterm/parser"

// PromptBoundaryStrategy determines where selection should start based on prompt detection.
// Implementations can use different strategies for detecting prompt boundaries.
type PromptBoundaryStrategy interface {
	// GetSelectionStart returns the starting column for selection on the given line.
	// If hasShift is true, returns 0 (include entire line including prompt).
	// If hasShift is false, returns the column after the prompt (skip prompt).
	GetSelectionStart(line int, hasShift bool) int

	// GetPromptEnd returns the column where the prompt ends on the given line.
	// Returns 0 if no prompt is detected.
	GetPromptEnd(line int) int

	// IsOnPrompt returns true if the given column is within the prompt area.
	IsOnPrompt(line, col int) bool
}

// VTermProvider is the interface for accessing terminal state.
// This abstracts VTerm so the strategy can be tested in isolation.
type VTermProvider interface {
	// InputActive returns true if shell integration indicates we're at input prompt.
	InputActive() bool
	// InputStartLine returns the line where shell input starts (OSC 133;B).
	InputStartLine() int
	// InputStartCol returns the column where shell input starts (OSC 133;B).
	InputStartCol() int
	// HistoryLineCopy returns a copy of the cells on the given history line.
	HistoryLineCopy(line int) []parser.Cell
	// ViewportRow returns cells from the given viewport row (0 to height-1).
	ViewportRow(row int) []parser.Cell
}

// ShellAwarePromptStrategy uses OSC 133 shell integration and pattern matching
// to detect prompt boundaries. Respects Shift modifier for including/skipping prompt.
type ShellAwarePromptStrategy struct {
	vtermProvider VTermProvider
}

// NewShellAwarePromptStrategy creates a new prompt strategy that uses shell integration.
func NewShellAwarePromptStrategy(provider VTermProvider) *ShellAwarePromptStrategy {
	return &ShellAwarePromptStrategy{vtermProvider: provider}
}

// GetSelectionStart returns the starting column for selection.
// With Shift: returns 0 (include prompt)
// Without Shift: returns column after prompt (skip prompt)
func (s *ShellAwarePromptStrategy) GetSelectionStart(line int, hasShift bool) int {
	if hasShift {
		return 0 // Include prompt
	}
	return s.GetPromptEnd(line)
}

// GetPromptEnd returns the column where the prompt ends on the given viewport row.
// First tries OSC 133 shell integration, then falls back to pattern detection.
// row is a viewport coordinate (0 to height-1).
func (s *ShellAwarePromptStrategy) GetPromptEnd(row int) int {
	if s.vtermProvider == nil {
		return 0
	}

	// Try OSC 133 shell integration first
	// Note: InputStartLine is also in viewport coordinates when at live edge
	if s.vtermProvider.InputActive() && row == s.vtermProvider.InputStartLine() {
		return s.vtermProvider.InputStartCol()
	}

	// Fallback: detect prompt pattern by scanning the viewport row
	cells := s.vtermProvider.ViewportRow(row)
	return DetectPromptEnd(cells)
}

// IsOnPrompt returns true if the column is within the prompt area.
func (s *ShellAwarePromptStrategy) IsOnPrompt(line, col int) bool {
	promptEnd := s.GetPromptEnd(line)
	return col < promptEnd
}

// DetectPromptEnd scans a line from the start and returns the column after the prompt.
// Returns 0 if no prompt pattern is detected.
//
// Prompt detection heuristic:
// - Look for non-alphanumeric characters at the start of line
// - If followed by a space, that's likely a prompt ending (e.g., "$ ", "> ", "% ")
// - More complex prompts like "user@host:~$ " are also detected
func DetectPromptEnd(cells []parser.Cell) int {
	if len(cells) < 2 {
		return 0
	}

	// Scan from start: look for prompt pattern
	// Prompts typically end with a special char + space (e.g., "$ ", "> ", "# ", ": ")
	for i := 0; i < len(cells); i++ {
		r := cells[i].Rune
		if r == ' ' {
			// Found a space - check if previous char was a prompt ending
			if i > 0 && isPromptEndingChar(cells[i-1].Rune) {
				return i + 1
			}
			// Continue looking for prompt pattern
			continue
		}
		// Check if alphanumeric character (not prompt)
		if isAlphanumeric(r) {
			// If we hit alphanumeric without finding prompt pattern, check if we're
			// after some prompt chars
			if i > 0 && hasPromptPattern(cells[:i]) {
				// Already found prompt-like chars, continue scanning
				continue
			}
			// No prompt pattern found, this might be output starting at column 0
			break
		}
	}

	// Simpler fallback: look for common prompt endings
	for i := 0; i < len(cells) && i < 50; i++ { // Limit scan to first 50 chars
		r := cells[i].Rune
		if r == ' ' && i > 0 {
			prev := cells[i-1].Rune
			// Common prompt endings: $ > # % : )
			if prev == '$' || prev == '>' || prev == '#' || prev == '%' || prev == ':' || prev == ')' {
				return i + 1
			}
		}
	}

	return 0
}

// isPromptEndingChar returns true if the rune commonly ends a shell prompt.
func isPromptEndingChar(r rune) bool {
	switch r {
	case '$', '>', '#', '%', ':', ')', ']':
		return true
	default:
		return false
	}
}

// isAlphanumeric returns true if the rune is a letter or digit.
func isAlphanumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// hasPromptPattern returns true if the cells contain typical prompt characters.
func hasPromptPattern(cells []parser.Cell) bool {
	for _, cell := range cells {
		r := cell.Rune
		// Prompt typically contains: @, :, ~, /, [, ], (, )
		if r == '@' || r == ':' || r == '~' || r == '/' || r == '[' || r == ']' || r == '(' || r == ')' {
			return true
		}
	}
	return false
}

// vtermAdapter wraps a VTerm to implement VTermProvider.
type vtermAdapter struct {
	vterm *parser.VTerm
}

// NewVTermAdapter creates a VTermProvider from a VTerm.
func NewVTermAdapter(vterm *parser.VTerm) VTermProvider {
	if vterm == nil {
		return nil
	}
	return &vtermAdapter{vterm: vterm}
}

func (a *vtermAdapter) InputActive() bool {
	return a.vterm.InputActive
}

func (a *vtermAdapter) InputStartLine() int {
	return a.vterm.InputStartLine
}

func (a *vtermAdapter) InputStartCol() int {
	return a.vterm.InputStartCol
}

func (a *vtermAdapter) HistoryLineCopy(line int) []parser.Cell {
	return a.vterm.HistoryLineCopy(line)
}

func (a *vtermAdapter) ViewportRow(row int) []parser.Cell {
	grid := a.vterm.Grid()
	if grid == nil || row < 0 || row >= len(grid) {
		return nil
	}
	return grid[row]
}

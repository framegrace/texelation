// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_test.go
// Summary: Tests for VTerm wrapping and reflow functionality.
// Usage: Run with `go test` to validate wrapping and resize behavior.
// Notes: Covers line wrapping, reflow, and cursor positioning during resize.

package parser

import (
	"strings"
	"testing"
)

// reconstructText rebuilds text from the history buffer, handling wrapped lines.
func reconstructText(v *VTerm) string {
	var result string
	for i := 0; i < v.HistoryLength(); i++ {
		line := v.HistoryLineCopy(i)
		for _, cell := range line {
			if cell.Rune != 0 {
				result += string(cell.Rune)
			}
		}
	}
	// Trim trailing spaces
	return strings.TrimRight(result, " ")
}

// TestLineWrapping verifies that lines wrap correctly when content exceeds terminal width.
func TestLineWrapping(t *testing.T) {
	v := NewVTerm(10, 5, WithWrap(true))

	// Type a line longer than the width (10 characters)
	text := "Hello World Test"
	for _, r := range text {
		v.placeChar(r)
	}

	// Check that we have multiple lines due to wrapping
	if v.HistoryLength() < 2 {
		t.Fatalf("expected at least 2 lines after wrapping, got %d", v.HistoryLength())
	}

	// First line should be marked as wrapped
	line0 := v.HistoryLineCopy(0)
	if len(line0) == 0 {
		t.Fatal("first line is empty")
	}
	if !line0[len(line0)-1].Wrapped {
		t.Error("first line should have Wrapped flag set on last cell")
	}

	// Reconstruct the full text
	reconstructed := reconstructText(v)

	if reconstructed != text {
		t.Errorf("expected reconstructed text %q, got %q", text, reconstructed)
	}
}

// TestNoWrappingWhenDisabled verifies that wrapping can be disabled.
func TestNoWrappingWhenDisabled(t *testing.T) {
	v := NewVTerm(10, 5, WithWrap(false))

	// Type a line longer than the width
	text := "Hello World Test"
	for _, r := range text {
		v.placeChar(r)
	}

	// Should only have 1 line (wrapping disabled)
	if v.HistoryLength() != 1 {
		t.Fatalf("expected 1 line when wrapping disabled, got %d", v.HistoryLength())
	}

	// Line should NOT be marked as wrapped
	line0 := v.HistoryLineCopy(0)
	if len(line0) > 0 && line0[len(line0)-1].Wrapped {
		t.Error("line should not have Wrapped flag when wrapping disabled")
	}
}

// TestReflowWider verifies that content reflows correctly when terminal gets wider.
func TestReflowWider(t *testing.T) {
	v := NewVTerm(10, 5, WithWrap(true), WithReflow(true))

	// Type enough text to wrap across multiple lines at width 10
	text := "Hello World Test Data"
	for _, r := range text {
		v.placeChar(r)
	}

	initialLines := v.HistoryLength()
	if initialLines < 2 {
		t.Fatalf("expected multiple lines with width 10, got %d", initialLines)
	}

	// Resize to width 25 (should fit on one line)
	v.Resize(25, 5)

	// Should now have fewer lines
	afterLines := v.HistoryLength()
	if afterLines >= initialLines {
		t.Errorf("expected fewer lines after widening: before=%d, after=%d", initialLines, afterLines)
	}

	// Verify content is preserved
	reconstructed := reconstructText(v)
	if reconstructed != text {
		t.Errorf("content not preserved after reflow: expected %q, got %q", text, reconstructed)
	}
}

// TestReflowNarrower verifies that content reflows correctly when terminal gets narrower.
func TestReflowNarrower(t *testing.T) {
	v := NewVTerm(25, 5, WithWrap(true), WithReflow(true))

	// Type text that fits on one line at width 25
	text := "Hello World Test Data"
	for _, r := range text {
		v.placeChar(r)
	}

	initialLines := v.HistoryLength()
	if initialLines != 1 {
		t.Fatalf("expected 1 line with width 25, got %d", initialLines)
	}

	// Resize to width 10 (should wrap to multiple lines)
	v.Resize(10, 5)

	// Should now have more lines
	afterLines := v.HistoryLength()
	if afterLines <= initialLines {
		t.Errorf("expected more lines after narrowing: before=%d, after=%d", initialLines, afterLines)
	}

	// First line should be marked as wrapped
	line0 := v.HistoryLineCopy(0)
	if len(line0) > 0 && !line0[len(line0)-1].Wrapped {
		t.Error("first line should be marked as wrapped after reflow to narrower width")
	}

	// Verify content is preserved
	reconstructed := reconstructText(v)
	if reconstructed != text {
		t.Errorf("content not preserved after reflow: expected %q, got %q", text, reconstructed)
	}
}

// TestCursorPositionAfterReflow verifies that cursor stays in correct position after reflow.
func TestCursorPositionAfterReflow(t *testing.T) {
	v := NewVTerm(20, 10, WithWrap(true), WithReflow(true))

	// Add some history lines
	for i := 0; i < 5; i++ {
		v.placeChar('L')
		v.placeChar('i')
		v.placeChar('n')
		v.placeChar('e')
		v.LineFeed()
		v.CarriageReturn()
	}

	// Type current line
	currentLine := "Current line here"
	for _, r := range currentLine {
		v.placeChar(r)
	}

	// Record position relative to end
	initialHistoryLen := v.HistoryLength()
	initialCursorY := v.GetCursorY()
	topLine := v.getTopHistoryLine()
	initialLogicalY := initialCursorY + topLine
	linesFromEnd := initialHistoryLen - initialLogicalY - 1

	// Resize to narrower width (causes reflow)
	v.Resize(10, 10)

	// Calculate new expected position
	newHistoryLen := v.HistoryLength()
	expectedLogicalY := newHistoryLen - linesFromEnd - 1

	// Check cursor is at correct position relative to end
	newCursorY := v.GetCursorY()
	newTopLine := v.getTopHistoryLine()
	newLogicalY := newCursorY + newTopLine

	if newLogicalY != expectedLogicalY {
		t.Errorf("cursor position incorrect after reflow: expected logicalY=%d, got %d", expectedLogicalY, newLogicalY)
	}

	// Verify the "Current line here" content is somewhere near the cursor
	// (it may have wrapped across multiple lines)
	foundCurrent := false
	// Check cursor line and a few lines around it
	for offset := -2; offset <= 2; offset++ {
		checkLine := newLogicalY + offset
		if checkLine < 0 || checkLine >= v.HistoryLength() {
			continue
		}
		line := v.HistoryLineCopy(checkLine)
		for _, cell := range line {
			if cell.Rune == 'C' || cell.Rune == 'u' || cell.Rune == 'r' {
				foundCurrent = true
				break
			}
		}
		if foundCurrent {
			break
		}
	}

	if !foundCurrent {
		t.Error("current line content not found near cursor after reflow")
	}
}

// TestReflowWithPromptAndContent verifies reflow handles lines with trailing spaces (like prompts).
func TestReflowWithPromptAndContent(t *testing.T) {
	v := NewVTerm(20, 5, WithWrap(true), WithReflow(true))

	// Simulate a short prompt "$ " followed by long content
	prompt := "$ "
	content := "some very long command that wraps"

	for _, r := range prompt {
		v.placeChar(r)
	}
	for _, r := range content {
		v.placeChar(r)
	}

	// Content should wrap
	initialLines := v.HistoryLength()
	if initialLines < 2 {
		t.Fatalf("expected content to wrap with width 20, got %d lines", initialLines)
	}

	// Resize wider - should combine back
	v.Resize(40, 5)

	afterLines := v.HistoryLength()
	if afterLines >= initialLines {
		t.Errorf("expected fewer lines after widening: before=%d, after=%d", initialLines, afterLines)
	}

	// Verify prompt and content are on same logical line
	reconstructed := reconstructText(v)
	expected := prompt + content // "$ some very long command that wraps"
	if reconstructed != expected {
		t.Errorf("prompt and content not properly reflowed: expected %q, got %q", expected, reconstructed)
	}
}

// TestMultipleReflows verifies that multiple resize operations work correctly.
func TestMultipleReflows(t *testing.T) {
	v := NewVTerm(30, 5, WithWrap(true), WithReflow(true))

	text := "This is a test of multiple reflow operations"
	for _, r := range text {
		v.placeChar(r)
	}

	// Narrow, then wide, then narrow again
	v.Resize(10, 5)
	afterNarrow1 := v.HistoryLength()

	v.Resize(30, 5)
	afterWide := v.HistoryLength()

	v.Resize(10, 5)
	afterNarrow2 := v.HistoryLength()

	// After multiple reflows, content should still be preserved
	reconstructed := reconstructText(v)
	if reconstructed != text {
		t.Errorf("content corrupted after multiple reflows: expected %q, got %q", text, reconstructed)
	}

	// Line counts should make sense
	if afterNarrow1 <= 1 {
		t.Errorf("expected multiple lines after first narrow, got %d", afterNarrow1)
	}
	if afterWide >= afterNarrow1 {
		t.Errorf("expected fewer lines after widening: narrow=%d, wide=%d", afterNarrow1, afterWide)
	}
	if afterNarrow2 <= afterWide {
		t.Errorf("expected more lines after narrowing again: wide=%d, narrow2=%d", afterWide, afterNarrow2)
	}
}

// TestReflowDoesNotCorruptData verifies that reflow doesn't corrupt cell data.
func TestReflowDoesNotCorruptData(t *testing.T) {
	v := NewVTerm(20, 5, WithWrap(true), WithReflow(true))

	// Type text with specific runes
	text := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	for _, r := range text {
		v.placeChar(r)
	}

	// Get content before reflow
	beforeReflow := reconstructText(v)

	// Resize multiple times
	v.Resize(10, 5)
	v.Resize(30, 5)
	v.Resize(15, 5)

	// Get content after reflow
	afterReflow := reconstructText(v)

	if beforeReflow != afterReflow {
		t.Errorf("data corrupted during reflow: expected %q, got %q", beforeReflow, afterReflow)
	}
}

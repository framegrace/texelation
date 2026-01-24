// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/selection_state_test.go
// Summary: Tests for SelectionStateMachine with mocked VTermProvider.

package texelterm

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/gdamore/tcell/v2"
)

// mockVTermProvider implements VTermProvider for testing.
type mockVTermProvider struct {
	inputActive    bool
	inputStartLine int
	inputStartCol  int
	historyLines   map[int][]parser.Cell
	currentLine    []parser.Cell
	grid           [][]parser.Cell
	contentText    string
}

func newMockVTermProvider() *mockVTermProvider {
	return &mockVTermProvider{
		historyLines: make(map[int][]parser.Cell),
	}
}

func (m *mockVTermProvider) InputActive() bool               { return m.inputActive }
func (m *mockVTermProvider) InputStartLine() int             { return m.inputStartLine }
func (m *mockVTermProvider) InputStartCol() int              { return m.inputStartCol }
func (m *mockVTermProvider) CurrentLineCells() []parser.Cell { return m.currentLine }
func (m *mockVTermProvider) Grid() [][]parser.Cell           { return m.grid }

func (m *mockVTermProvider) HistoryLineCopy(line int) []parser.Cell {
	if cells, ok := m.historyLines[line]; ok {
		out := make([]parser.Cell, len(cells))
		copy(out, cells)
		return out
	}
	return nil
}

func (m *mockVTermProvider) ViewportRow(row int) []parser.Cell {
	if m.grid != nil && row >= 0 && row < len(m.grid) {
		return m.grid[row]
	}
	return nil
}

func (m *mockVTermProvider) GetContentText(startLine int64, startOffset int, endLine int64, endOffset int) string {
	return m.contentText
}

// Helper to create cells from string
func cellsFromString(s string) []parser.Cell {
	cells := make([]parser.Cell, len(s))
	for i, r := range s {
		cells[i] = parser.Cell{Rune: r}
	}
	return cells
}

// newTestStateMachine creates a SelectionStateMachine with a mock provider.
func newTestStateMachine(mock *mockVTermProvider) *SelectionStateMachine {
	return &SelectionStateMachine{
		state:         StateIdle,
		vtermProvider: mock,
	}
}

// --- State Transition Tests ---

func TestSelectionStateMachine_InitialState(t *testing.T) {
	mock := newMockVTermProvider()
	sm := newTestStateMachine(mock)

	if sm.State() != StateIdle {
		t.Errorf("expected initial state to be StateIdle, got %v", sm.State())
	}
	if sm.IsActive() {
		t.Error("expected IsActive() to be false initially")
	}
	if sm.IsRendered() {
		t.Error("expected IsRendered() to be false initially")
	}
}

func TestSelectionStateMachine_SingleClickStartsDragging(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("hello world")
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	sm.Start(5, 3, 0, SingleClick, 0)

	if sm.State() != StateDragging {
		t.Errorf("expected StateDragging after single click, got %v", sm.State())
	}
	if !sm.IsActive() {
		t.Error("expected IsActive() to be true during drag")
	}
	if !sm.IsRendered() {
		t.Error("expected IsRendered() to be true during drag")
	}
}

func TestSelectionStateMachine_DoubleClickSelectsWord(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("hello world test")
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	// Double-click on "world" (positions 6-10)
	sm.Start(5, 8, 0, DoubleClick, 0)

	if sm.State() != StateMultiClickHeld {
		t.Errorf("expected StateMultiClickHeld after double click, got %v", sm.State())
	}

	startLine, startOffset, endLine, endOffset, ok := sm.GetSelectionRange()
	if !ok {
		t.Fatal("expected valid selection range")
	}
	if startLine != 5 || endLine != 5 {
		t.Errorf("expected line 5, got start=%d end=%d", startLine, endLine)
	}
	if startOffset != 6 {
		t.Errorf("expected startOffset 6 (start of 'world'), got %d", startOffset)
	}
	if endOffset != 11 { // "world" is 5 chars, offset is exclusive
		t.Errorf("expected endOffset 11 (end of 'world'), got %d", endOffset)
	}
}

func TestSelectionStateMachine_TripleClickSelectsLine(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("hello world test")
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	sm.Start(5, 8, 0, TripleClick, 0)

	if sm.State() != StateMultiClickHeld {
		t.Errorf("expected StateMultiClickHeld after triple click, got %v", sm.State())
	}

	startLine, startOffset, endLine, endOffset, ok := sm.GetSelectionRange()
	if !ok {
		t.Fatal("expected valid selection range")
	}
	if startLine != 5 || endLine != 5 {
		t.Errorf("expected line 5, got start=%d end=%d", startLine, endLine)
	}
	if startOffset != 0 {
		t.Errorf("expected startOffset 0 (start of line), got %d", startOffset)
	}
	if endOffset != 16 { // "hello world test" is 16 chars
		t.Errorf("expected endOffset 16 (end of line), got %d", endOffset)
	}
}

func TestSelectionStateMachine_Cancel(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("hello world")
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	sm.Start(5, 3, 0, SingleClick, 0)
	if sm.State() != StateDragging {
		t.Fatal("expected StateDragging before cancel")
	}

	sm.Cancel()

	if sm.State() != StateIdle {
		t.Errorf("expected StateIdle after cancel, got %v", sm.State())
	}
	if sm.IsActive() {
		t.Error("expected IsActive() to be false after cancel")
	}
	if sm.IsRendered() {
		t.Error("expected IsRendered() to be false after cancel")
	}
}

// --- Drag Selection Tests ---

func TestSelectionStateMachine_DragForward(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("hello world")
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	// Start at position 2
	sm.Start(5, 2, 0, SingleClick, 0)
	// Drag to position 8
	sm.Update(5, 8, 0, 0)

	startLine, startOffset, endLine, endOffset, ok := sm.GetSelectionRange()
	if !ok {
		t.Fatal("expected valid selection range")
	}
	if startOffset != 2 || endOffset != 8 {
		t.Errorf("expected range [2,8], got [%d,%d]", startOffset, endOffset)
	}
	if startLine != 5 || endLine != 5 {
		t.Errorf("expected line 5, got start=%d end=%d", startLine, endLine)
	}
}

func TestSelectionStateMachine_DragBackward(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("hello world")
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	// Start at position 8
	sm.Start(5, 8, 0, SingleClick, 0)
	// Drag backward to position 2
	sm.Update(5, 2, 0, 0)

	_, startOffset, _, endOffset, ok := sm.GetSelectionRange()
	if !ok {
		t.Fatal("expected valid selection range")
	}
	// GetSelectionRange normalizes so start < end
	if startOffset != 2 || endOffset != 8 {
		t.Errorf("expected normalized range [2,8], got [%d,%d]", startOffset, endOffset)
	}
}

func TestSelectionStateMachine_DragAcrossLines(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("first line")
	mock.historyLines[6] = cellsFromString("second line")
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	// Start at line 5, position 3
	sm.Start(5, 3, 0, SingleClick, 0)
	// Drag to line 6, position 7
	sm.Update(6, 7, 1, 0)

	startLine, startOffset, endLine, endOffset, ok := sm.GetSelectionRange()
	if !ok {
		t.Fatal("expected valid selection range")
	}
	if startLine != 5 || endLine != 6 {
		t.Errorf("expected lines [5,6], got [%d,%d]", startLine, endLine)
	}
	if startOffset != 3 || endOffset != 7 {
		t.Errorf("expected offsets [3,7], got [%d,%d]", startOffset, endOffset)
	}
}

// --- Finish Tests ---

func TestSelectionStateMachine_FinishSingleClick(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("hello world")
	mock.contentText = "llo wo"
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	sm.Start(5, 2, 0, SingleClick, 0)
	sm.Update(5, 8, 0, 0)

	mime, data, ok := sm.Finish(5, 8, 0, 0)

	if !ok {
		t.Fatal("expected successful finish")
	}
	if mime != "text/plain" {
		t.Errorf("expected mime 'text/plain', got %q", mime)
	}
	if string(data) != "llo wo" {
		t.Errorf("expected text 'llo wo', got %q", string(data))
	}

	// After single-click finish, selection should not be rendered
	if sm.IsRendered() {
		t.Error("expected IsRendered() to be false after single-click finish")
	}
	if sm.State() != StateIdle {
		t.Errorf("expected StateIdle after single-click finish, got %v", sm.State())
	}
}

func TestSelectionStateMachine_FinishMultiClick(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("hello world")
	mock.contentText = "world"
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	sm.Start(5, 8, 0, DoubleClick, 0)

	mime, data, ok := sm.Finish(5, 8, 0, 0)

	if !ok {
		t.Fatal("expected successful finish")
	}
	if mime != "text/plain" {
		t.Errorf("expected mime 'text/plain', got %q", mime)
	}
	if string(data) != "world" {
		t.Errorf("expected text 'world', got %q", string(data))
	}

	// After multi-click finish, selection should remain rendered
	if !sm.IsRendered() {
		t.Error("expected IsRendered() to be true after multi-click finish")
	}
	if sm.State() != StateFinished {
		t.Errorf("expected StateFinished after multi-click finish, got %v", sm.State())
	}
}

func TestSelectionStateMachine_FinishEmptySelection(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("hello world")
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	// Click without moving - empty selection
	sm.Start(5, 5, 0, SingleClick, 0)
	_, _, ok := sm.Finish(5, 5, 0, 0)

	if ok {
		t.Error("expected empty selection to return ok=false")
	}
}

// --- Word Selection Edge Cases ---

func TestSelectionStateMachine_WordSelectionAtLineStart(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("hello world")
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	sm.Start(5, 0, 0, DoubleClick, 0)

	_, startOffset, _, endOffset, ok := sm.GetSelectionRange()
	if !ok {
		t.Fatal("expected valid selection")
	}
	if startOffset != 0 || endOffset != 5 {
		t.Errorf("expected word 'hello' [0,5], got [%d,%d]", startOffset, endOffset)
	}
}

func TestSelectionStateMachine_WordSelectionAtLineEnd(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("hello world")
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	sm.Start(5, 10, 0, DoubleClick, 0) // Click on last 'd' of 'world'

	_, startOffset, _, endOffset, ok := sm.GetSelectionRange()
	if !ok {
		t.Fatal("expected valid selection")
	}
	if startOffset != 6 || endOffset != 11 {
		t.Errorf("expected word 'world' [6,11], got [%d,%d]", startOffset, endOffset)
	}
}

func TestSelectionStateMachine_WordSelectionOnWhitespace(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("hello world")
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	sm.Start(5, 5, 0, DoubleClick, 0) // Click on space between words

	_, startOffset, _, endOffset, ok := sm.GetSelectionRange()
	// Clicking on whitespace should result in empty or single-char selection
	if ok && startOffset != endOffset {
		t.Errorf("expected no meaningful selection on whitespace, got [%d,%d]", startOffset, endOffset)
	}
}

func TestSelectionStateMachine_WordWithUnderscore(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("hello_world test")
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	sm.Start(5, 7, 0, DoubleClick, 0) // Click in middle of 'hello_world'

	_, startOffset, _, endOffset, ok := sm.GetSelectionRange()
	if !ok {
		t.Fatal("expected valid selection")
	}
	if startOffset != 0 || endOffset != 11 {
		t.Errorf("expected word 'hello_world' [0,11], got [%d,%d]", startOffset, endOffset)
	}
}

func TestSelectionStateMachine_WordWithDash(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("hello-world test")
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	sm.Start(5, 7, 0, DoubleClick, 0) // Click in middle of 'hello-world'

	_, startOffset, _, endOffset, ok := sm.GetSelectionRange()
	if !ok {
		t.Fatal("expected valid selection")
	}
	if startOffset != 0 || endOffset != 11 {
		t.Errorf("expected word 'hello-world' [0,11], got [%d,%d]", startOffset, endOffset)
	}
}

// --- Update Only During Drag ---

func TestSelectionStateMachine_UpdateIgnoredWhenNotDragging(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("hello world")
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	// Start with double-click (StateMultiClickHeld, not StateDragging)
	sm.Start(5, 8, 0, DoubleClick, 0)
	originalSelection := sm.GetSelection()

	// Try to update - should be ignored
	sm.Update(5, 0, 0, 0)

	newSelection := sm.GetSelection()
	if newSelection.CurrentOffset != originalSelection.CurrentOffset {
		t.Error("Update should be ignored when not in StateDragging")
	}
}

// --- isWordChar Tests ---

func TestIsWordChar(t *testing.T) {
	tests := []struct {
		r    rune
		want bool
	}{
		{'a', true},
		{'z', true},
		{'A', true},
		{'Z', true},
		{'0', true},
		{'9', true},
		{'_', true},
		{'-', true},
		{' ', false},
		{'.', false},
		{'!', false},
		{'@', false},
		{'\t', false},
		{'\n', false},
	}

	for _, tt := range tests {
		got := isWordChar(tt.r)
		if got != tt.want {
			t.Errorf("isWordChar(%q) = %v, want %v", tt.r, got, tt.want)
		}
	}
}

// --- Current Line Selection Tests ---

func TestSelectionStateMachine_SelectWordOnCurrentLine(t *testing.T) {
	mock := newMockVTermProvider()
	mock.currentLine = cellsFromString("current line text")
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	// logicalLine = -1 means current line
	sm.Start(-1, 10, 0, DoubleClick, 0)

	startLine, startOffset, endLine, endOffset, ok := sm.GetSelectionRange()
	if !ok {
		t.Fatal("expected valid selection")
	}
	if startLine != -1 || endLine != -1 {
		t.Errorf("expected line -1 (current), got start=%d end=%d", startLine, endLine)
	}
	// "line" is at positions 8-11
	if startOffset != 8 || endOffset != 12 {
		t.Errorf("expected word 'line' [8,12], got [%d,%d]", startOffset, endOffset)
	}
}

// --- Modifiers (for future use) ---

func TestSelectionStateMachine_ModifiersPassedThrough(t *testing.T) {
	mock := newMockVTermProvider()
	mock.historyLines[5] = cellsFromString("hello world")
	mock.contentText = "llo wo" // Text from position 2-8
	sm := newTestStateMachine(mock)
	sm.SetSize(80, 24)

	// Currently modifiers aren't used, but the API accepts them
	// This test ensures the API doesn't break
	sm.Start(5, 2, 0, SingleClick, tcell.ModShift)
	sm.Update(5, 8, 0, tcell.ModShift|tcell.ModCtrl)
	_, _, ok := sm.Finish(5, 8, 0, tcell.ModShift)

	if !ok {
		t.Error("selection with modifiers should work")
	}
}

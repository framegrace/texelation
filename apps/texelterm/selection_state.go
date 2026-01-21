// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/selection_state.go
// Summary: Selection state machine with content coordinate support.

package texelterm

import (
	"strings"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/gdamore/tcell/v2"
)

// SelectionState represents the current state of the selection process.
type SelectionState int

const (
	// StateIdle means no selection is active.
	StateIdle SelectionState = iota
	// StateDragging means user is actively dragging to select (single-click drag).
	StateDragging
	// StateMultiClickHeld means a multi-click selection is active (word or line).
	StateMultiClickHeld
	// StateFinished means selection completed but may still be rendered.
	StateFinished
)

// Selection represents the current selection range in content coordinates.
// Content coordinates are (logicalLine, charOffset) which are stable across scrolling.
// logicalLine is -1 for the current uncommitted line.
type Selection struct {
	// AnchorLine is the logical line where selection started (-1 for current line).
	AnchorLine int64
	// AnchorOffset is the character offset within AnchorLine.
	AnchorOffset int
	// CurrentLine is the logical line where selection currently ends (-1 for current line).
	CurrentLine int64
	// CurrentOffset is the character offset within CurrentLine.
	CurrentOffset int
	// Rendered indicates if selection should be visually highlighted.
	Rendered bool
}

// SelectionStateMachine manages the selection lifecycle with clear state transitions.
type SelectionStateMachine struct {
	state         SelectionState
	selection     Selection
	vtermProvider VTermProvider
	vterm         *parser.VTerm
	width, height int
}

// NewSelectionStateMachine creates a new selection state machine.
func NewSelectionStateMachine(vterm *parser.VTerm) *SelectionStateMachine {
	return &SelectionStateMachine{
		state:         StateIdle,
		vtermProvider: NewVTermAdapter(vterm),
		vterm:         vterm,
	}
}

// SetSize updates the terminal dimensions for auto-scroll edge detection.
func (s *SelectionStateMachine) SetSize(width, height int) {
	s.width = width
	s.height = height
}

// Start begins a selection based on click type and content position.
// logicalLine is -1 for the current uncommitted line.
// viewportRow is used for coordinate conversion.
func (s *SelectionStateMachine) Start(logicalLine int64, charOffset int, viewportRow int, clickType ClickType, modifiers tcell.ModMask) {
	s.selection = Selection{
		Rendered: true,
	}

	switch clickType {
	case SingleClick:
		// Character-by-character selection
		s.selection.AnchorLine = logicalLine
		s.selection.AnchorOffset = charOffset
		s.selection.CurrentLine = logicalLine
		s.selection.CurrentOffset = charOffset
		s.state = StateDragging

	case DoubleClick:
		// Word selection
		s.selectWord(logicalLine, charOffset, viewportRow)
		s.state = StateMultiClickHeld

	case TripleClick:
		// Line selection
		s.selectLine(logicalLine, charOffset, viewportRow)
		s.state = StateMultiClickHeld
	}
}

// Update updates the selection endpoint during drag operations.
// Uses content coordinates (logicalLine, charOffset).
func (s *SelectionStateMachine) Update(logicalLine int64, charOffset int, viewportRow int, modifiers tcell.ModMask) {
	if s.state != StateDragging {
		return
	}

	s.selection.CurrentLine = logicalLine
	s.selection.CurrentOffset = charOffset
}

// Finish completes the selection and returns the selected text.
func (s *SelectionStateMachine) Finish(logicalLine int64, charOffset int, viewportRow int, modifiers tcell.ModMask) (mime string, data []byte, ok bool) {
	if s.state == StateIdle {
		return "", nil, false
	}

	// Update final position for dragging (multi-click already has correct range)
	if s.state == StateDragging {
		s.selection.CurrentLine = logicalLine
		s.selection.CurrentOffset = charOffset
	}

	text := s.buildSelectionText()

	// Keep multi-click selections visible after release
	isMultiClick := s.state == StateMultiClickHeld
	s.selection.Rendered = isMultiClick

	if isMultiClick {
		s.state = StateFinished
	} else {
		s.state = StateIdle
	}

	if text == "" {
		return "", nil, false
	}
	return "text/plain", []byte(text), true
}

// Cancel cancels any active selection.
func (s *SelectionStateMachine) Cancel() {
	s.selection = Selection{}
	s.state = StateIdle
}

// IsActive returns true if a selection is currently in progress.
func (s *SelectionStateMachine) IsActive() bool {
	return s.state == StateDragging || s.state == StateMultiClickHeld
}

// IsRendered returns true if a selection should be visually displayed.
func (s *SelectionStateMachine) IsRendered() bool {
	return s.selection.Rendered && (s.state != StateIdle || s.state == StateFinished)
}

// GetSelection returns the current selection for rendering.
func (s *SelectionStateMachine) GetSelection() Selection {
	return s.selection
}

// State returns the current state of the selection machine.
func (s *SelectionStateMachine) State() SelectionState {
	return s.state
}

// GetSelectionRange returns the normalized selection range in content coordinates.
// Returns (startLine, startOffset, endLine, endOffset, ok).
func (s *SelectionStateMachine) GetSelectionRange() (startLine int64, startOffset int, endLine int64, endOffset int, ok bool) {
	if !s.selection.Rendered {
		return 0, 0, 0, 0, false
	}

	startLine = s.selection.AnchorLine
	startOffset = s.selection.AnchorOffset
	endLine = s.selection.CurrentLine
	endOffset = s.selection.CurrentOffset

	// Normalize: ensure start is before end
	if startLine > endLine || (startLine == endLine && startOffset > endOffset) {
		startLine, endLine = endLine, startLine
		startOffset, endOffset = endOffset, startOffset
	}

	// Check for empty selection
	if startLine == endLine && startOffset == endOffset {
		return 0, 0, 0, 0, false
	}

	return startLine, startOffset, endLine, endOffset, true
}

// selectWord selects the word at the given content position.
func (s *SelectionStateMachine) selectWord(logicalLine int64, charOffset int, viewportRow int) {
	if s.vterm == nil {
		return
	}

	// Get cells for the full logical line (not just viewport row)
	var cells []parser.Cell
	if logicalLine < 0 {
		// Current line - get full line from display buffer
		cells = s.vterm.CurrentLineCells()
		if cells == nil {
			// Fallback to grid if display buffer not available
			grid := s.vterm.Grid()
			if grid != nil && viewportRow >= 0 && viewportRow < len(grid) {
				cells = grid[viewportRow]
			}
		}
	} else {
		// Historical line
		cells = s.vterm.HistoryLineCopy(int(logicalLine))
	}

	if len(cells) == 0 {
		s.selection.AnchorLine = logicalLine
		s.selection.AnchorOffset = charOffset
		s.selection.CurrentLine = logicalLine
		s.selection.CurrentOffset = charOffset
		return
	}

	// Clamp charOffset to valid range
	if charOffset >= len(cells) {
		charOffset = len(cells) - 1
	}
	if charOffset < 0 {
		charOffset = 0
	}

	// If clicking on whitespace, select nothing meaningful
	if !isWordChar(cells[charOffset].Rune) {
		s.selection.AnchorLine = logicalLine
		s.selection.AnchorOffset = charOffset
		s.selection.CurrentLine = logicalLine
		s.selection.CurrentOffset = charOffset
		return
	}

	// Find start of word
	start := charOffset
	for start > 0 && isWordChar(cells[start-1].Rune) {
		start--
	}

	// Find end of word
	end := charOffset
	for end < len(cells)-1 && isWordChar(cells[end+1].Rune) {
		end++
	}

	s.selection.AnchorLine = logicalLine
	s.selection.AnchorOffset = start
	s.selection.CurrentLine = logicalLine
	s.selection.CurrentOffset = end + 1 // +1 to make offset exclusive (like slice end)
}

// selectLine selects the entire logical line at the given position.
func (s *SelectionStateMachine) selectLine(logicalLine int64, charOffset int, viewportRow int) {
	if s.vterm == nil {
		return
	}

	// Get cells for the full logical line (not just viewport row)
	var cells []parser.Cell
	if logicalLine < 0 {
		// Current line - get full line from display buffer
		cells = s.vterm.CurrentLineCells()
		if cells == nil {
			// Fallback to grid if display buffer not available
			grid := s.vterm.Grid()
			if grid != nil && viewportRow >= 0 && viewportRow < len(grid) {
				cells = grid[viewportRow]
			}
		}
	} else {
		// Historical line
		cells = s.vterm.HistoryLineCopy(int(logicalLine))
	}

	// Select entire line from start to end
	s.selection.AnchorLine = logicalLine
	s.selection.AnchorOffset = 0
	s.selection.CurrentLine = logicalLine
	s.selection.CurrentOffset = len(cells)
}

// buildSelectionText extracts text from the current selection range.
func (s *SelectionStateMachine) buildSelectionText() string {
	if s.vterm == nil {
		return ""
	}

	startLine, startOffset, endLine, endOffset, ok := s.GetSelectionRange()
	if !ok {
		return ""
	}

	// Use VTerm's GetContentText for proper extraction
	return s.vterm.GetContentText(startLine, startOffset, endLine, endOffset)
}

// cellsToRunes converts a slice of cells to a slice of runes.
func (s *SelectionStateMachine) cellsToRunes(cells []parser.Cell) []rune {
	if len(cells) == 0 {
		return nil
	}
	out := make([]rune, len(cells))
	for i, cell := range cells {
		r := cell.Rune
		if r == 0 {
			r = ' '
		}
		out[i] = r
	}
	return out
}

// clamp restricts v to the range [min, max].
func clamp(v, minVal, maxVal int) int {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}

// trimTrailingSpaces removes trailing spaces from a string.
func trimTrailingSpaces(s string) string {
	return strings.TrimRight(s, " ")
}

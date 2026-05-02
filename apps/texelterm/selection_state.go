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

// isWordChar determines if a rune is part of a word (alphanumeric, underscore, or dash).
// Used by SelectionModeWord — punctuation breaks the word.
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '_' || r == '-'
}

// isSpaceWordChar treats only whitespace as a separator. Used by
// SelectionModeSpaceWord so that "path/to/file.txt" or "user@host" or
// "https://example.com/x?y=1" are selected as single units.
func isSpaceWordChar(r rune) bool {
	if r == 0 {
		return false
	}
	return r != ' ' && r != '\t' && r != '\n' && r != '\r'
}

// SelectionMode names the discrete selection modes the state machine
// supports. Both the click handler and a future floating menu (or
// keyboard binding) can request a mode by name; click count is purely
// the input-event side of the wiring.
type SelectionMode int

const (
	// SelectionModeChar — character-by-character drag (single click).
	SelectionModeChar SelectionMode = iota
	// SelectionModeSpaceWord — whitespace-bounded "atom" (double click):
	// selects "path/to/file.txt" as a single token. Useful for paths,
	// URLs, identifiers with punctuation.
	SelectionModeSpaceWord
	// SelectionModeWord — non-word-character bounded word (triple click):
	// the historical word selection. Letters / digits / "_" / "-" only;
	// punctuation breaks the word.
	SelectionModeWord
	// SelectionModeLine — entire visible line at the click position
	// (quadruple click).
	SelectionModeLine
	// SelectionModeCommand — the shell command the click fell within
	// (quintuple click): from the most recent OSC 133;A prompt anchor at
	// or before the click, to the next prompt or the end of the buffer.
	// Falls back to SelectionModeLine when no usable anchor is known.
	SelectionModeCommand
)

// String returns a human-readable label for the mode. Used by future
// floating-menu UI and for diagnostic logging.
func (m SelectionMode) String() string {
	switch m {
	case SelectionModeChar:
		return "char"
	case SelectionModeSpaceWord:
		return "space-word"
	case SelectionModeWord:
		return "word"
	case SelectionModeLine:
		return "line"
	case SelectionModeCommand:
		return "command"
	}
	return "unknown"
}

// AllSelectionModes returns the discrete selection modes that a future
// floating menu (or keyboard binding) should expose. SelectionModeChar
// is omitted: it's the default "drag to select" interaction and isn't
// a menu item users would pick. SelectionModeCommand is omitted while
// issue #223 is unimplemented — the resolver works only for clicks
// inside the current command, so surfacing it as a menu option would
// confuse users for the more common older-scrollback case. Add it back
// here once the gesture is reliable end-to-end.
func AllSelectionModes() []SelectionMode {
	return []SelectionMode{
		SelectionModeSpaceWord,
		SelectionModeWord,
		SelectionModeLine,
	}
}

// SelectionModeForClickCount maps a 1..MaxClickType count to its
// selection mode. Centralised so the click handler, future menu
// shortcuts, and tests stay in agreement. Quintuple click currently
// has no mapping — see issue #223 — and a 5+ count falls back to
// SelectionModeChar (zero-width selection at the click point).
func SelectionModeForClickCount(n int) SelectionMode {
	switch n {
	case 2:
		return SelectionModeSpaceWord
	case 3:
		return SelectionModeWord
	case 4:
		return SelectionModeLine
	default:
		return SelectionModeChar
	}
}

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
	width, height int
}

// NewSelectionStateMachine creates a new selection state machine from a VTerm.
func NewSelectionStateMachine(vterm *parser.VTerm) *SelectionStateMachine {
	return &SelectionStateMachine{
		state:         StateIdle,
		vtermProvider: NewVTermAdapter(vterm),
	}
}

// NewSelectionStateMachineWithProvider creates a new selection state machine with a VTermProvider.
// This allows for dependency injection and easier testing.
func NewSelectionStateMachineWithProvider(provider VTermProvider) *SelectionStateMachine {
	return &SelectionStateMachine{
		state:         StateIdle,
		vtermProvider: provider,
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
	s.StartMode(SelectionModeForClickCount(int(clickType)), logicalLine, charOffset, viewportRow, modifiers)
}

// StartMode applies a selection mode at the given content position.
// This is the public entry point for both click-driven selection (via
// Start) and any future input source — keyboard shortcuts, a floating
// "selection mode" menu, or programmatic API calls. Each mode resolves
// its own anchor / head against the VTermProvider; SelectionModeChar
// stays in StateDragging so the caller can extend by mouse drag.
func (s *SelectionStateMachine) StartMode(mode SelectionMode, logicalLine int64, charOffset int, viewportRow int, modifiers tcell.ModMask) {
	s.selection = Selection{Rendered: true}

	switch mode {
	case SelectionModeChar:
		s.selection.AnchorLine = logicalLine
		s.selection.AnchorOffset = charOffset
		s.selection.CurrentLine = logicalLine
		s.selection.CurrentOffset = charOffset
		s.state = StateDragging
	case SelectionModeSpaceWord:
		s.selectAtom(logicalLine, charOffset, viewportRow, isSpaceWordChar)
		s.state = StateMultiClickHeld
	case SelectionModeWord:
		s.selectAtom(logicalLine, charOffset, viewportRow, isWordChar)
		s.state = StateMultiClickHeld
	case SelectionModeLine:
		s.selectLine(logicalLine, charOffset, viewportRow)
		s.state = StateMultiClickHeld
	case SelectionModeCommand:
		s.selectCommand(logicalLine, charOffset, viewportRow)
		s.state = StateMultiClickHeld
	default:
		s.state = StateIdle
		s.selection = Selection{}
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

// Selection returns the current selection for rendering.
func (s *SelectionStateMachine) Selection() Selection {
	return s.selection
}

// State returns the current state of the selection machine.
func (s *SelectionStateMachine) State() SelectionState {
	return s.state
}

// SelectionRange returns the normalized selection range in content coordinates.
// Returns (startLine, startOffset, endLine, endOffset, ok).
func (s *SelectionStateMachine) SelectionRange() (startLine int64, startOffset int, endLine int64, endOffset int, ok bool) {
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

// selectAtom selects the maximal run of "atom" characters surrounding the
// click, using isAtom to define what counts as an atom character.
// SelectionModeSpaceWord uses isSpaceWordChar (whitespace-bounded);
// SelectionModeWord uses isWordChar (alphanumeric + _ -).
func (s *SelectionStateMachine) selectAtom(logicalLine int64, charOffset int, viewportRow int, isAtom func(r rune) bool) {
	if s.vtermProvider == nil {
		return
	}

	cells := s.lineCellsAt(logicalLine, viewportRow)
	if len(cells) == 0 {
		s.selection.AnchorLine = logicalLine
		s.selection.AnchorOffset = charOffset
		s.selection.CurrentLine = logicalLine
		s.selection.CurrentOffset = charOffset
		return
	}

	if charOffset >= len(cells) {
		charOffset = len(cells) - 1
	}
	if charOffset < 0 {
		charOffset = 0
	}

	if !isAtom(cells[charOffset].Rune) {
		// Click landed on a separator — degenerate zero-width selection.
		s.selection.AnchorLine = logicalLine
		s.selection.AnchorOffset = charOffset
		s.selection.CurrentLine = logicalLine
		s.selection.CurrentOffset = charOffset
		return
	}

	start := charOffset
	for start > 0 && isAtom(cells[start-1].Rune) {
		start--
	}
	end := charOffset
	for end < len(cells)-1 && isAtom(cells[end+1].Rune) {
		end++
	}

	s.selection.AnchorLine = logicalLine
	s.selection.AnchorOffset = start
	s.selection.CurrentLine = logicalLine
	s.selection.CurrentOffset = end + 1 // exclusive
}

// lineCellsAt returns the cells for the given logical line, falling back
// to the current grid row when the line is the uncommitted current line
// (logicalLine == -1).
func (s *SelectionStateMachine) lineCellsAt(logicalLine int64, viewportRow int) []parser.Cell {
	if logicalLine < 0 {
		cells := s.vtermProvider.CurrentLineCells()
		if cells != nil {
			return cells
		}
		grid := s.vtermProvider.Grid()
		if grid != nil && viewportRow >= 0 && viewportRow < len(grid) {
			return grid[viewportRow]
		}
		return nil
	}
	return s.vtermProvider.HistoryLineCopy(int(logicalLine))
}

// maxCommandLines is a defensive ceiling on SelectionModeCommand's
// resolved range. Even the "current command" branch can grow large
// (e.g. a long-running tail / build log); this cap keeps the OSC52
// clipboard write inside the band the host terminal tolerates.
//
// 1000 lines × ~80 cols ≈ 80 KB of plain text — comfortably below any
// terminal's OSC52 ceiling and a sensible "fits one command's output"
// budget. Larger ranges fall back to selectLine.
const maxCommandLines int64 = 1000

// selectCommand selects the shell command the click fell within.
//
// Properly bracketing a command requires the OSC 133;A anchor for the
// prompt at-or-before the click AND the anchor for the next prompt
// after it. VTerm currently tracks only the most recent anchor (issue
// #222 adds a per-prompt history). With one anchor we can only
// confidently resolve clicks in the *current* command:
//
//   - effective click line >= latest prompt: range is
//     [latestPrompt, contentEnd] — "current command output."
//   - effective click line  < latest prompt: we cannot bracket the
//     older command without history. Fall back to selectLine; we'd
//     rather select one line correctly than ship a huge capture
//     between two prompts that may not be the user's command.
//   - no anchor at all: selectLine.
//
// Once issue #222 lands the second case becomes correct ("find the
// enclosing prompt" / "find the next prompt") and the fallback can
// drop. The maxCommandLines safety cap stays — even tightly-bracketed
// commands could be enormous (build logs, dumps) and the host terminal
// still has its OSC52 limits.
func (s *SelectionStateMachine) selectCommand(logicalLine int64, charOffset int, viewportRow int) {
	if s.vtermProvider == nil {
		s.selectLine(logicalLine, charOffset, viewportRow)
		return
	}
	promptStart := s.vtermProvider.PromptStartLine()
	if promptStart < 0 {
		s.selectLine(logicalLine, charOffset, viewportRow)
		return
	}
	contentEnd := s.vtermProvider.ContentEndLine()

	// Resolve effective click line: clicks on the current uncommitted
	// line behave as if they landed at ContentEnd.
	effectiveLine := logicalLine
	if effectiveLine < 0 {
		effectiveLine = contentEnd
	}

	if effectiveLine < promptStart {
		// Older scrollback. Need prompt history (issue #222) to do
		// this right; until then fall back rather than guess at bounds.
		s.selectLine(logicalLine, charOffset, viewportRow)
		return
	}

	startLine := promptStart
	endLine := contentEnd
	if endLine-startLine+1 > maxCommandLines {
		// Even the current command can be huge (long-running tail,
		// build logs). Fall back to selectLine for the host terminal's
		// sanity; user can drag-select a larger range explicitly.
		s.selectLine(logicalLine, charOffset, viewportRow)
		return
	}

	endCells := s.lineCellsAt(endLine, viewportRow)
	s.selection.AnchorLine = startLine
	s.selection.AnchorOffset = 0
	s.selection.CurrentLine = endLine
	s.selection.CurrentOffset = len(endCells)
}

// selectLine selects the entire logical line at the given position.
func (s *SelectionStateMachine) selectLine(logicalLine int64, charOffset int, viewportRow int) {
	if s.vtermProvider == nil {
		return
	}

	// Get cells for the full logical line (not just viewport row)
	var cells []parser.Cell
	if logicalLine < 0 {
		// Current line - get full line from display buffer
		cells = s.vtermProvider.CurrentLineCells()
		if cells == nil {
			// Fallback to grid if display buffer not available
			grid := s.vtermProvider.Grid()
			if grid != nil && viewportRow >= 0 && viewportRow < len(grid) {
				cells = grid[viewportRow]
			}
		}
	} else {
		// Historical line
		cells = s.vtermProvider.HistoryLineCopy(int(logicalLine))
	}

	// Select entire line from start to end
	s.selection.AnchorLine = logicalLine
	s.selection.AnchorOffset = 0
	s.selection.CurrentLine = logicalLine
	s.selection.CurrentOffset = len(cells)
}

// buildSelectionText extracts text from the current selection range.
func (s *SelectionStateMachine) buildSelectionText() string {
	if s.vtermProvider == nil {
		return ""
	}

	startLine, startOffset, endLine, endOffset, ok := s.SelectionRange()
	if !ok {
		return ""
	}

	// Use VTerm's GetContentText for proper extraction
	return s.vtermProvider.GetContentText(startLine, startOffset, endLine, endOffset)
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

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/term_selection.go
// Summary: Text selection handling for the terminal emulator.
// Usage: Provides selection start, update, finish, and text extraction.

package texelterm

import (
	"strings"
	"time"

	texelcore "github.com/framegrace/texelui/core"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/internal/theming"
	"github.com/gdamore/tcell/v2"
)

// termSelection tracks the current text selection state and multi-click history.
//
// Selection behavior:
//   - Single-click: Start character-by-character selection
//   - Double-click: Select entire word at cursor (alphanumeric + _ + -)
//   - Triple-click: Select entire logical line (following wrapped lines)
//
// The selection uses two separate flags:
//   - active: true while mouse button is held (drag in progress)
//   - rendered: true while selection should be visually highlighted
//
// This separation allows multi-click selections to remain visible after mouse-up
// while still copying to clipboard, matching standard terminal behavior.
type termSelection struct {
	active        bool // true when drag operation is in progress
	rendered      bool // true when selection should be visually highlighted
	anchorLine    int  // history line index where selection started
	anchorCol     int  // column where selection started
	currentLine   int  // history line index where selection currently ends
	currentCol    int  // column where selection currently ends
	lastClickTime time.Time
	lastClickLine int
	lastClickCol  int
	clickCount    int
}

// isWordChar determines if a rune is part of a word (alphanumeric, underscore, or dash).
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '_' || r == '-'
}

// selectWordAtPositionLocked selects the word at the given position.
// Must be called with a.mu locked.
func (a *TexelTerm) selectWordAtPositionLocked(line, col int) {
	cells := a.vterm.HistoryLineCopy(line)
	if len(cells) == 0 {
		return
	}

	// Clamp col to valid range
	if col >= len(cells) {
		col = len(cells) - 1
	}
	if col < 0 {
		col = 0
	}

	// If clicking on whitespace, select nothing
	if col < len(cells) && !isWordChar(cells[col].Rune) {
		a.selection.anchorLine = line
		a.selection.anchorCol = col
		a.selection.currentLine = line
		a.selection.currentCol = col
		return
	}

	// Find start of word
	start := col
	for start > 0 && isWordChar(cells[start-1].Rune) {
		start--
	}

	// Find end of word
	end := col
	for end < len(cells)-1 && isWordChar(cells[end+1].Rune) {
		end++
	}

	a.selection.anchorLine = line
	a.selection.anchorCol = start
	a.selection.currentLine = line
	a.selection.currentCol = end
}

// detectPromptEnd scans a line from the start and returns the column after the prompt.
// Returns 0 if no prompt pattern is detected.
// Prompts are detected as: non-alphanumeric character(s) followed by a space.
func detectPromptEnd(cells []parser.Cell) int {
	if len(cells) < 2 {
		return 0
	}

	// Scan from start: count consecutive non-alphanumeric characters
	for i, cell := range cells {
		r := cell.Rune
		// Check if this is a non-alphanumeric character (potential prompt char)
		if (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			if r == ' ' && i > 0 {
				// Found space after special chars - this is the prompt end
				return i + 1
			}
			continue
		}
		// Hit alphanumeric - if we haven't found a space yet, no prompt
		break
	}
	return 0
}

// selectLineAtPositionLocked selects the entire logical line at the given position,
// following wrapped lines to capture the complete command/output.
// Must be called with a.mu locked.
func (a *TexelTerm) selectLineAtPositionLocked(line int) {
	historyLen := a.vterm.HistoryLength()
	if historyLen == 0 {
		return
	}

	// Find the start of the logical line by going backwards
	startLine := line
	for startLine > 0 {
		prevLine := a.vterm.HistoryLineCopy(startLine - 1)
		if len(prevLine) == 0 {
			break
		}
		if prevLine[len(prevLine)-1].Wrapped {
			startLine--
		} else {
			break
		}
	}

	// Find the end of the logical line by going forwards
	endLine := line
	for endLine < historyLen-1 {
		currentLine := a.vterm.HistoryLineCopy(endLine)
		if len(currentLine) == 0 {
			break
		}
		if currentLine[len(currentLine)-1].Wrapped {
			endLine++
		} else {
			break
		}
	}

	// Determine start column - skip prompt if selecting the current input line
	startCol := 0
	if a.vterm.InputActive && startLine == a.vterm.InputStartLine {
		startCol = a.vterm.InputStartCol
	} else {
		startLineCells := a.vterm.HistoryLineCopy(startLine)
		startCol = detectPromptEnd(startLineCells)
	}

	a.selection.anchorLine = startLine
	a.selection.anchorCol = startCol

	// Find the end column on the last line
	endCells := a.vterm.HistoryLineCopy(endLine)
	endCol := 0
	if len(endCells) > 0 {
		endCol = len(endCells) - 1
	}

	a.selection.currentLine = endLine
	a.selection.currentCol = endCol
}

// SelectionStart implements texelcore.SelectionHandler.
func (a *TexelTerm) SelectionStart(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vterm == nil {
		return false
	}
	line, col := a.screenToHistoryPosition(x, y)

	// Detect double/triple-click
	now := time.Now()
	samePosition := line == a.selection.lastClickLine && col == a.selection.lastClickCol
	withinTimeout := now.Sub(a.selection.lastClickTime) < multiClickTimeout

	var clickCount int
	if samePosition && withinTimeout {
		clickCount = a.selection.clickCount + 1
	} else {
		clickCount = 1
	}

	a.selection = termSelection{
		active:        true,
		rendered:      true,
		lastClickTime: now,
		lastClickLine: line,
		lastClickCol:  col,
		clickCount:    clickCount,
	}

	if clickCount == 2 {
		// Double-click: select word
		a.selectWordAtPositionLocked(line, col)
	} else if clickCount >= 3 {
		// Triple-click: select line
		a.selectLineAtPositionLocked(line)
	} else {
		// Single click: start normal selection
		a.selection.anchorLine = line
		a.selection.anchorCol = col
		a.selection.currentLine = line
		a.selection.currentCol = col
	}

	a.vterm.MarkAllDirty()
	a.requestRefresh()
	return true
}

// SelectionUpdate implements texelcore.SelectionHandler.
func (a *TexelTerm) SelectionUpdate(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vterm == nil || !a.selection.active {
		return
	}

	// Save mouse position for auto-scroll
	a.lastMouseX = x
	a.lastMouseY = y

	line, col := a.screenToHistoryPosition(x, y)
	if !a.selection.active {
		return
	}
	if line == a.selection.currentLine && col == a.selection.currentCol {
		// Position hasn't changed, but check for edge-based auto-scroll
		a.manageAutoScrollState(y)
		return
	}
	a.selection.currentLine = line
	a.selection.currentCol = col
	a.vterm.MarkAllDirty()
	a.requestRefresh()

	// Check if we need to start/stop auto-scroll based on mouse position
	a.manageAutoScrollState(y)
}

// SelectionFinish implements texelcore.SelectionHandler.
func (a *TexelTerm) SelectionFinish(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) (string, []byte, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vterm == nil || !a.selection.active {
		return "", nil, false
	}

	// Stop auto-scroll if active
	a.stopAutoScrollLocked()

	// For multi-click selections, keep the visual selection visible after mouse up
	isMultiClick := a.selection.clickCount >= 2

	// Only update selection position for single-click drag selections
	// Multi-click selections already have the correct range set
	if !isMultiClick {
		line, col := a.screenToHistoryPosition(x, y)
		a.selection.currentLine = line
		a.selection.currentCol = col
	}

	text := a.buildSelectionTextLocked()

	// Preserve click history and selection state for multi-click detection
	newSelection := termSelection{
		active:        false,
		rendered:      isMultiClick, // Keep visible for double/triple-click
		lastClickTime: a.selection.lastClickTime,
		lastClickLine: a.selection.lastClickLine,
		lastClickCol:  a.selection.lastClickCol,
		clickCount:    a.selection.clickCount,
	}

	// If multi-click, also preserve the selection range for rendering
	if isMultiClick {
		newSelection.anchorLine = a.selection.anchorLine
		newSelection.anchorCol = a.selection.anchorCol
		newSelection.currentLine = a.selection.currentLine
		newSelection.currentCol = a.selection.currentCol
	}

	a.selection = newSelection
	a.vterm.MarkAllDirty()
	a.requestRefresh()
	if text == "" {
		return "", nil, false
	}
	return "text/plain", []byte(text), true
}

// SelectionCancel implements texelcore.SelectionHandler.
func (a *TexelTerm) SelectionCancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.selection.active && !a.selection.rendered {
		return
	}

	// Stop auto-scroll if active
	a.stopAutoScrollLocked()

	// Preserve click history for multi-click detection
	a.selection = termSelection{
		active:        false,
		rendered:      false,
		lastClickTime: a.selection.lastClickTime,
		lastClickLine: a.selection.lastClickLine,
		lastClickCol:  a.selection.lastClickCol,
		clickCount:    a.selection.clickCount,
	}
	if a.vterm != nil {
		a.vterm.MarkAllDirty()
	}
	a.requestRefresh()
}

// selectionRangeLocked returns the normalized selection range (start <= end).
// Must be called with a.mu locked.
func (a *TexelTerm) selectionRangeLocked() (startLine, startCol, endLine, endCol int, valid bool) {
	if !a.selection.active && !a.selection.rendered {
		return 0, 0, 0, 0, false
	}
	startLine = a.selection.anchorLine
	startCol = a.selection.anchorCol
	endLine = a.selection.currentLine
	endCol = a.selection.currentCol

	// Normalize so start <= end
	if startLine > endLine || (startLine == endLine && startCol > endCol) {
		startLine, endLine = endLine, startLine
		startCol, endCol = endCol, startCol
	}

	// Empty selection
	if startLine == endLine && startCol == endCol {
		return 0, 0, 0, 0, false
	}
	return startLine, startCol, endLine, endCol + 1, true
}

// buildSelectionTextLocked extracts the selected text from history.
// Must be called with a.mu locked.
func (a *TexelTerm) buildSelectionTextLocked() string {
	if a.vterm == nil {
		return ""
	}
	startLine, startCol, endLine, endColExclusive, ok := a.selectionRangeLocked()
	if !ok {
		return ""
	}
	lines := make([]string, 0, endLine-startLine+1)
	inAlt := a.vterm.InAltScreen()

	for line := startLine; line <= endLine; line++ {
		var cells []parser.Cell
		if inAlt {
			cells = a.vterm.GetAltBufferLine(line)
		} else {
			cells = a.vterm.HistoryLineCopy(line)
		}
		runes := cellsToRunes(cells)
		lineStart := 0
		lineEnd := len(runes)
		if line == startLine {
			lineStart = clampInt(startCol, 0, lineEnd)
		}
		if line == endLine {
			target := clampInt(endColExclusive, lineStart, len(runes))
			lineEnd = target
		}
		if line > startLine && line < endLine {
			lineStart = 0
			lineEnd = len(runes)
		}
		segment := ""
		if lineEnd > lineStart {
			segment = string(runes[lineStart:lineEnd])
		}
		segment = strings.TrimRight(segment, " ")
		lines = append(lines, segment)
	}
	return strings.Join(lines, "\n")
}

// applySelectionHighlightLocked applies selection highlighting to the render buffer.
// Must be called with a.mu locked.
func (a *TexelTerm) applySelectionHighlightLocked(buf [][]texelcore.Cell) {
	if a.vterm == nil || !a.selection.rendered || len(buf) == 0 {
		return
	}
	startLine, startCol, endLine, endColExclusive, ok := a.selectionRangeLocked()
	if !ok {
		return
	}
	top := a.vterm.VisibleTop()
	cfg := theming.ForApp("texelterm")
	defaultBg := tcell.NewRGBColor(232, 217, 255)
	highlight := cfg.GetColor("selection", "highlight_bg", defaultBg)
	if !highlight.Valid() {
		highlight = defaultBg
	}
	highlight = highlight.TrueColor()
	fgColor := cfg.GetColor("selection", "highlight_fg", tcell.ColorBlack)
	if !fgColor.Valid() {
		fgColor = tcell.ColorBlack
	}
	fgColor = fgColor.TrueColor()
	for y := 0; y < len(buf); y++ {
		lineIdx := top + y
		if lineIdx < startLine || lineIdx > endLine {
			continue
		}
		row := buf[y]
		lineStart := 0
		lineEnd := len(row)
		if lineIdx == startLine {
			lineStart = clampInt(startCol, 0, lineEnd)
		}
		if lineIdx == endLine {
			lineEnd = clampInt(endColExclusive, lineStart, len(row))
		}
		if lineIdx > startLine && lineIdx < endLine {
			lineStart = 0
			lineEnd = len(row)
		}
		if lineIdx == startLine && lineIdx == endLine {
			lineEnd = clampInt(endColExclusive, lineStart, len(row))
		}
		for x := lineStart; x < lineEnd && x < len(row); x++ {
			row[x].Style = row[x].Style.Background(highlight).Foreground(fgColor)
		}
	}
}

// cellsToRunes converts a slice of cells to runes, replacing null chars with spaces.
func cellsToRunes(cells []parser.Cell) []rune {
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

// clampInt clamps an integer value to the given range.
func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

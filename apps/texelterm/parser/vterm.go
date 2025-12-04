// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm.go
// Summary: Implements vterm capabilities for the terminal parser module.
// Usage: Consumed by the terminal app when decoding VT sequences.
// Notes: Keeps parsing concerns isolated from rendering.

package parser

import (
	"fmt"
	"log"
)

const (
	defaultHistorySize = 2000
)

// VTerm represents the state of a virtual terminal, managing both the main screen
// with a scrollback buffer and an alternate screen for fullscreen applications.
type VTerm struct {
	width, height                      int
	cursorX, cursorY                   int
	savedMainCursorX, savedMainCursorY int
	savedAltCursorX, savedAltCursorY   int
	// Legacy circular buffer (deprecated in favor of historyManager)
	historyBuffer                      [][]Cell
	maxHistorySize                     int
	historyHead, historyLen            int
	// New infinite history system
	historyManager                     *HistoryManager
	viewOffset                         int
	inAltScreen                        bool
	altBuffer                          [][]Cell
	currentFG, currentBG               Color
	currentAttr                        Attribute
	tabStops                           map[int]bool
	cursorVisible                      bool
	wrapNext, autoWrapMode, insertMode bool
	wrapEnabled, reflowEnabled         bool // Line wrapping for main screen buffer
	appCursorKeys                      bool
	TitleChanged                       func(string)
	WriteToPty                         func([]byte)
	marginTop, marginBottom            int
	marginLeft, marginRight            int
	leftRightMarginMode                bool
	originMode                         bool
	defaultFG, defaultBG               Color
	DefaultFgChanged, DefaultBgChanged func(Color)
	QueryDefaultFg, QueryDefaultBg     func()
	ScreenRestored                     func()
	dirtyLines                         map[int]bool
	allDirty                           bool
	prevCursorY                        int
	InSynchronizedUpdate               bool
	lastGraphicChar                    rune // Last graphic character written (for REP command)
	// Shell integration (OSC 133)
	PromptActive                       bool
	InputActive                        bool
	CommandActive                      bool
	InputStartLine, InputStartCol      int
	OnPromptStart                      func()
	OnInputStart                       func()
	OnCommandStart                     func()
	OnCommandEnd                       func(exitCode int)
	// Bracketed paste mode (DECSET 2004)
	bracketedPasteMode                 bool
	OnBracketedPasteModeChange         func(bool)
}

// NewVTerm creates and initializes a new virtual terminal.
func NewVTerm(width, height int, opts ...Option) *VTerm {
	v := &VTerm{
		width:          width,
		height:         height,
		maxHistorySize: defaultHistorySize,
		historyBuffer:  make([][]Cell, defaultHistorySize),
		viewOffset:     0,
		currentFG:      DefaultFG,
		currentBG:      DefaultBG,
		tabStops:       make(map[int]bool),
		cursorVisible:  true,
		autoWrapMode:   true,
		wrapEnabled:         true,
		reflowEnabled:       true,
		marginTop:           0,
		marginBottom:        height - 1,
		marginLeft:          0,
		marginRight:         width - 1,
		leftRightMarginMode: false,
		defaultFG:           DefaultFG,
		defaultBG:           DefaultBG,
		dirtyLines:          make(map[int]bool),
		allDirty:            true,
	}
	for _, opt := range opts {
		opt(v)
	}
	for i := 0; i < width; i++ {
		if i%8 == 0 {
			v.tabStops[i] = true
		}
	}
	v.ClearScreen()
	return v
}

// --- Buffer & Grid Logic ---

// Grid returns the currently visible 2D buffer of cells.
// It dynamically constructs the view from the history buffer if on the main screen,
// or returns the alternate screen buffer directly.
func (v *VTerm) Grid() [][]Cell {
	if v.inAltScreen {
		return v.altBuffer
	}
	grid := make([][]Cell, v.height)
	topHistoryLine := v.getTopHistoryLine()
	histLen := v.historyLen
	if v.historyManager != nil {
		histLen = v.historyManager.Length()
	}
	for i := 0; i < v.height; i++ {
		historyIdx := topHistoryLine + i
		grid[i] = make([]Cell, v.width)
		var logicalLine []Cell
		if historyIdx >= 0 && historyIdx < histLen {
			logicalLine = v.getHistoryLine(historyIdx)
		}
		// Fill the grid line, padding with default cells if the history line is short.
		for x := 0; x < v.width; x++ {
			if logicalLine != nil && x < len(logicalLine) {
				grid[i][x] = logicalLine[x]
			} else {
				grid[i][x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
			}
		}
	}
	return grid
}

// IsBracketedPasteModeEnabled returns whether bracketed paste mode is enabled.
func (v *VTerm) IsBracketedPasteModeEnabled() bool {
	return v.bracketedPasteMode
}

// placeChar puts a rune at the current cursor position, handling wrapping and insert mode.
func (v *VTerm) placeChar(r rune) {
	// Track last graphic character for REP command
	v.lastGraphicChar = r

	if v.wrapNext {
		// Wrap to next line for both alt and main screen
		// If left/right margins are active, wrap to left margin
		if v.leftRightMarginMode {
			v.cursorX = v.marginLeft
		} else {
			v.cursorX = 0
		}
		v.LineFeed()
		v.wrapNext = false
	}

	if v.inAltScreen {
		if v.cursorY >= 0 && v.cursorY < v.height && v.cursorX >= 0 && v.cursorX < v.width {
			v.altBuffer[v.cursorY][v.cursorX] = Cell{Rune: r, FG: v.currentFG, BG: v.currentBG, Attr: v.currentAttr}
			v.MarkDirty(v.cursorY)
		}
	} else {
		if v.viewOffset > 0 { // If scrolled up, jump to the bottom on new input
			v.viewOffset = 0
			v.MarkAllDirty()
			v.SetCursorPos(v.getHistoryLen()-1-v.getTopHistoryLine(), v.cursorX)
		}
		logicalY := v.cursorY + v.getTopHistoryLine()

		// Ensure all lines exist up to the cursor position
		for v.getHistoryLen() <= logicalY {
			v.appendHistoryLine(make([]Cell, 0, v.width))
		}

		line := v.getHistoryLine(logicalY)
		if line == nil {
			line = make([]Cell, 0, v.width)
		}
		for len(line) <= v.cursorX {
			line = append(line, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
		}
		if v.insertMode {
			line = append(line, Cell{}) // Make space for the new char
			copy(line[v.cursorX+1:], line[v.cursorX:])
			// Truncate at right margin if DECLRMM is active
			if v.leftRightMarginMode && v.cursorX >= v.marginLeft && v.cursorX <= v.marginRight {
				// Ensure line doesn't extend beyond right margin
				if len(line) > v.marginRight+1 {
					line = line[:v.marginRight+1]
				}
			}
		}

		line[v.cursorX] = Cell{Rune: r, FG: v.currentFG, BG: v.currentBG, Attr: v.currentAttr}
		v.setHistoryLine(logicalY, line)
		v.MarkDirty(v.cursorY)
	}

	// Determine the effective right edge for wrapping
	rightEdge := v.width - 1
	if v.leftRightMarginMode && v.cursorX >= v.marginLeft && v.cursorX <= v.marginRight {
		rightEdge = v.marginRight
	}

	if v.inAltScreen {
		if v.autoWrapMode && v.cursorX == rightEdge {
			v.wrapNext = true
		} else if v.cursorX < rightEdge {
			v.SetCursorPos(v.cursorY, v.cursorX+1)
		}
	} else {
		// Main screen wrapping logic
		if v.wrapEnabled && v.cursorX == rightEdge {
			// Mark the current cell as wrapped (continues on next line)
			logicalY := v.cursorY + v.getTopHistoryLine()
			line := v.getHistoryLine(logicalY)
			if len(line) > v.cursorX {
				line[v.cursorX].Wrapped = true
				v.setHistoryLine(logicalY, line)
			}
			// Set wrapNext instead of wrapping immediately
			// This allows CR or LF to clear the flag without creating extra lines
			v.wrapNext = true
		} else if v.cursorX < rightEdge {
			v.SetCursorPos(v.cursorY, v.cursorX+1)
		}
		// If at the edge and wrapping is disabled, cursor stays at the last column
	}
}

// --- Cursor and Scrolling ---

// SetCursorPos moves the cursor to a new position, clamping to screen bounds.
func (v *VTerm) SetCursorPos(y, x int) {
	// Clamp coordinates first
	if x < 0 {
		x = 0
	}
	if x >= v.width {
		x = v.width - 1
	}
	if y < 0 {
		y = 0
	}
	if y >= v.height {
		y = v.height - 1
	}

	// Only clear wrapNext if we're actually moving to a different position
	if y != v.cursorY || x != v.cursorX {
		v.wrapNext = false
	}

	v.prevCursorY = v.cursorY
	v.cursorX = x
	v.cursorY = y

	v.MarkDirty(v.prevCursorY)
	v.MarkDirty(v.cursorY)
}

// GetCursorX returns the current cursor X position
func (v *VTerm) GetCursorX() int {
	return v.cursorX
}

// GetCursorY returns the current cursor Y position
func (v *VTerm) GetCursorY() int {
	return v.cursorY
}

// LineFeed moves the cursor down one line, scrolling if necessary.
func (v *VTerm) LineFeed() {
	v.wrapNext = false // Clear wrapNext flag when moving to new line
	v.MarkDirty(v.cursorY)

	// Check if cursor is outside left/right margins - if so, don't scroll
	outsideMargins := v.leftRightMarginMode && (v.cursorX < v.marginLeft || v.cursorX > v.marginRight)

	if v.inAltScreen {
		if v.cursorY == v.marginBottom {
			if !outsideMargins {
				v.scrollRegion(1, v.marginTop, v.marginBottom)
			}
		} else if v.cursorY < v.height-1 {
			v.SetCursorPos(v.cursorY+1, v.cursorX)
		}
	} else {
		// Main screen: check if we're at bottom margin
		if v.cursorY == v.marginBottom {
			if !outsideMargins {
				v.scrollRegion(1, v.marginTop, v.marginBottom)
			}
		} else if v.cursorY < v.height-1 {
			// Only append history lines when cursor will actually move down
			logicalY := v.cursorY + v.getTopHistoryLine()
			if logicalY+1 >= v.getHistoryLen() {
				v.appendHistoryLine(make([]Cell, 0, v.width))
			}
			v.SetCursorPos(v.cursorY+1, v.cursorX)
		} else {
			// At bottom of screen but not at scroll region bottom: stay put
			v.viewOffset = 0 // Jump to the bottom
			v.MarkAllDirty()
		}
	}
}

// scrollRegion scrolls a portion of the screen buffer up or down.
func (v *VTerm) scrollRegion(n int, top int, bottom int) {
	v.wrapNext = false

	if v.inAltScreen {
		buffer := v.altBuffer
		if n > 0 { // Scroll Up
			for i := 0; i < n; i++ {
				copy(buffer[top:bottom], buffer[top+1:bottom+1])
				buffer[bottom] = make([]Cell, v.width)
				for x := range buffer[bottom] {
					buffer[bottom][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
		} else { // Scroll Down
			for i := 0; i < -n; i++ {
				copy(buffer[top+1:bottom+1], buffer[top:bottom])
				buffer[top] = make([]Cell, v.width)
				for x := range buffer[top] {
					buffer[top][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
		}
	} else {
		// Main screen scrolling within margins
		topHistory := v.getTopHistoryLine()
		if n > 0 { // Scroll Up
			for i := 0; i < n; i++ {
				// Remove the top line of the region
				// Move all lines in region up by one
				for y := top; y < bottom; y++ {
					srcLine := v.getHistoryLine(topHistory + y + 1)
					v.setHistoryLine(topHistory+y, srcLine)
				}
				// Clear the bottom line
				blankLine := make([]Cell, 0, v.width)
				v.setHistoryLine(topHistory+bottom, blankLine)
			}
		} else { // Scroll Down
			// Ensure history buffer has all lines we'll be writing to
			endLogicalY := topHistory + bottom
			for v.getHistoryLen() <= endLogicalY {
				v.appendHistoryLine(make([]Cell, 0, v.width))
			}
			for i := 0; i < -n; i++ {
				// Move all lines in region down by one
				for y := bottom; y > top; y-- {
					srcLine := v.getHistoryLine(topHistory + y - 1)
					v.setHistoryLine(topHistory+y, srcLine)
				}
				// Clear the top line
				blankLine := make([]Cell, 0, v.width)
				v.setHistoryLine(topHistory+top, blankLine)
			}
		}
	}
	v.MarkAllDirty()
}

// scrollUpWithinMargins scrolls content up within the left/right margins.
// Similar to deleteLinesWithinMargins but operates on the entire top/bottom region.
func (v *VTerm) scrollUpWithinMargins(n int) {
	v.wrapNext = false
	leftCol := v.marginLeft
	rightCol := v.marginRight

	if v.inAltScreen {
		// Shift content within margins upward
		for y := v.marginTop; y <= v.marginBottom-n; y++ {
			srcY := y + n
			if srcY <= v.marginBottom {
				// Copy the margin region from source line to current line
				copy(v.altBuffer[y][leftCol:rightCol+1], v.altBuffer[srcY][leftCol:rightCol+1])
			}
		}
		// Clear the bottom n lines' margin regions
		clearStart := v.marginBottom - n + 1
		if clearStart < v.marginTop {
			clearStart = v.marginTop
		}
		for y := clearStart; y <= v.marginBottom; y++ {
			if y >= 0 && y < v.height {
				for x := leftCol; x <= rightCol; x++ {
					v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
				}
			}
		}
	} else {
		// Main screen with history buffer
		topHistory := v.getTopHistoryLine()

		// Ensure history has all required lines
		endLogicalY := topHistory + v.marginBottom
		for v.getHistoryLen() <= endLogicalY {
			v.appendHistoryLine(make([]Cell, 0, v.width))
		}

		// Shift content within margins upward
		for y := v.marginTop; y <= v.marginBottom-n; y++ {
			srcY := y + n
			if srcY <= v.marginBottom {
				dstLine := v.getHistoryLine(topHistory + y)
				srcLine := v.getHistoryLine(topHistory + srcY)

				// Ensure lines are wide enough
				for len(dstLine) <= rightCol {
					dstLine = append(dstLine, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}
				for len(srcLine) <= rightCol {
					srcLine = append(srcLine, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}

				// Copy margin region
				copy(dstLine[leftCol:rightCol+1], srcLine[leftCol:rightCol+1])
				v.setHistoryLine(topHistory+y, dstLine)
			}
		}

		// Clear the bottom n lines' margin regions
		clearStart := v.marginBottom - n + 1
		if clearStart < v.marginTop {
			clearStart = v.marginTop
		}
		for y := clearStart; y <= v.marginBottom; y++ {
			if y >= 0 {
				line := v.getHistoryLine(topHistory + y)
				for len(line) <= rightCol {
					line = append(line, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}
				for x := leftCol; x <= rightCol; x++ {
					line[x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
				}
				v.setHistoryLine(topHistory+y, line)
			}
		}
	}
	v.MarkAllDirty()
}

// scrollDownWithinMargins scrolls content down within the left/right margins.
// Similar to insertLinesWithinMargins but operates on the entire top/bottom region.
func (v *VTerm) scrollDownWithinMargins(n int) {
	v.wrapNext = false
	leftCol := v.marginLeft
	rightCol := v.marginRight

	if v.inAltScreen {
		// Shift content within margins downward
		for y := v.marginBottom; y >= v.marginTop+n; y-- {
			srcY := y - n
			if srcY >= v.marginTop {
				// Copy the margin region from source line to current line
				copy(v.altBuffer[y][leftCol:rightCol+1], v.altBuffer[srcY][leftCol:rightCol+1])
			}
		}
		// Clear the top n lines' margin regions
		clearEnd := v.marginTop + n - 1
		if clearEnd > v.marginBottom {
			clearEnd = v.marginBottom
		}
		for y := v.marginTop; y <= clearEnd; y++ {
			if y >= 0 && y < v.height {
				for x := leftCol; x <= rightCol; x++ {
					v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
				}
			}
		}
	} else {
		// Main screen with history buffer
		topHistory := v.getTopHistoryLine()

		// Ensure history has all required lines
		endLogicalY := topHistory + v.marginBottom
		for v.getHistoryLen() <= endLogicalY {
			v.appendHistoryLine(make([]Cell, 0, v.width))
		}

		// Shift content within margins downward
		for y := v.marginBottom; y >= v.marginTop+n; y-- {
			srcY := y - n
			if srcY >= v.marginTop {
				dstLine := v.getHistoryLine(topHistory + y)
				srcLine := v.getHistoryLine(topHistory + srcY)

				// Ensure lines are wide enough
				for len(dstLine) <= rightCol {
					dstLine = append(dstLine, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}
				for len(srcLine) <= rightCol {
					srcLine = append(srcLine, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}

				// Copy margin region
				copy(dstLine[leftCol:rightCol+1], srcLine[leftCol:rightCol+1])
				v.setHistoryLine(topHistory+y, dstLine)
			}
		}

		// Clear the top n lines' margin regions
		clearEnd := v.marginTop + n - 1
		if clearEnd > v.marginBottom {
			clearEnd = v.marginBottom
		}
		for y := v.marginTop; y <= clearEnd; y++ {
			if y >= 0 {
				line := v.getHistoryLine(topHistory + y)
				for len(line) <= rightCol {
					line = append(line, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}
				for x := leftCol; x <= rightCol; x++ {
					line[x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
				}
				v.setHistoryLine(topHistory+y, line)
			}
		}
	}
	v.MarkAllDirty()
}

// Scroll adjusts the viewport offset for the history buffer.
func (v *VTerm) Scroll(delta int) {
	if v.inAltScreen {
		return
	}
	v.viewOffset -= delta
	if v.viewOffset < 0 {
		v.viewOffset = 0
	}
	histLen := v.getHistoryLen()
	maxOffset := histLen - v.height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if v.viewOffset > maxOffset {
		v.viewOffset = maxOffset
	}
	v.MarkAllDirty()
}

// --- CSI Handlers ---

// processANSIMode handles ANSI mode setting/resetting (SM/RM - without '?' prefix).
func (v *VTerm) processANSIMode(command rune, params []int) {
	if len(params) == 0 {
		return
	}
	mode := params[0]
	switch command {
	case 'h': // SM - Set Mode
		switch mode {
		case 4: // IRM - Insert/Replace Mode
			v.insertMode = true
		default:
			log.Printf("Parser: Unhandled ANSI set mode: %d%c", mode, command)
		}
	case 'l': // RM - Reset Mode
		switch mode {
		case 4: // IRM - Insert/Replace Mode
			v.insertMode = false
		default:
			log.Printf("Parser: Unhandled ANSI reset mode: %d%c", mode, command)
		}
	}
}

// processPrivateCSI handles terminal-specific CSI sequences (starting with '?').
func (v *VTerm) processPrivateCSI(command rune, params []int) {
	if len(params) == 0 {
		return
	}
	mode := params[0]
	switch command {
	case 'h': // SET
		switch mode {
		case 1:
			v.appCursorKeys = true
		case 6: // DECOM - Origin Mode
			v.originMode = true
			// Move cursor to home position of scroll region
			v.SetCursorPos(v.marginTop, v.marginLeft)
		case 7:
			v.autoWrapMode = true
		case 12: // SET Blinking Cursor
			// We can just log this for now, as it's a visual preference.
			log.Println("Parser: Ignoring set blinking cursor (12h)")
		case 25:
			v.SetCursorVisible(true)
		case 69: // DECLRMM - Enable left/right margin mode
			v.leftRightMarginMode = true
		case 1002, 1004, 1006:
			// Ignore mouse and focus reporting for now
		case 2004: // Enable bracketed paste mode
			if !v.bracketedPasteMode {
				v.bracketedPasteMode = true
				if v.OnBracketedPasteModeChange != nil {
					v.OnBracketedPasteModeChange(true)
				}
			}
		case 1049: // Switch to Alt Workspace
			if v.inAltScreen {
				return
			}
			v.inAltScreen = true
			v.savedMainCursorX, v.savedMainCursorY = v.cursorX, v.cursorY //+v.getTopHistoryLine()
			v.altBuffer = make([][]Cell, v.height)
			for i := range v.altBuffer {
				v.altBuffer[i] = make([]Cell, v.width)
				// Initialize all cells with proper default colors
				for j := range v.altBuffer[i] {
					v.altBuffer[i][j] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
				}
			}
			v.ClearScreen()
		case 2026: // START Synchronized Update
			v.InSynchronizedUpdate = true
		default:
			log.Printf("Parser: Unhandled private CSI set mode: ?%d%c", mode, command)
		}
	case 'l': // RESET
		switch mode {
		case 1:
			v.appCursorKeys = false
		case 6: // DECOM - Reset Origin Mode
			v.originMode = false
			// Move cursor to absolute home position
			v.SetCursorPos(0, 0)
		case 7:
			v.autoWrapMode = false
		case 12: // RESET Steady Cursor (Stop Blinking)
			// We can just log this for now.
			log.Println("Parser: Ignoring reset steady cursor (12l)")
		case 25:
			v.SetCursorVisible(false)
		case 69: // DECLRMM - Disable left/right margin mode
			v.leftRightMarginMode = false
			// Reset margins to full width
			v.marginLeft = 0
			v.marginRight = v.width - 1
		case 1002, 1004, 1006, 2031, 2048:
			// Ignore mouse and focus reporting for now
		case 2004: // Disable bracketed paste mode
			if v.bracketedPasteMode {
				v.bracketedPasteMode = false
				if v.OnBracketedPasteModeChange != nil {
					v.OnBracketedPasteModeChange(false)
				}
			}
		case 1049: // Switch to Main Workspace
			if !v.inAltScreen {
				return
			}
			v.inAltScreen = false
			v.altBuffer = nil
			physicalY := v.savedMainCursorY // - v.getTopHistoryLine()
			v.SetCursorPos(physicalY, v.savedMainCursorX)
			v.MarkAllDirty()
			if v.ScreenRestored != nil {
				v.ScreenRestored()
			}
		case 2026: // END Synchronized Update
			v.InSynchronizedUpdate = false
		default:
			log.Printf("Parser: Unhandled private CSI reset mode: ?%d%c", mode, command)
		}
	}
}

// ClearScreen fully resets the terminal buffer (used for RIS/Reset).
// This is the full reset version - it resets history and moves cursor to home.
func (v *VTerm) ClearScreen() {
	v.MarkAllDirty()
	if v.inAltScreen {
		// Use default colors, not currentFG/BG which might be from previous content
		for y := range v.altBuffer {
			for x := range v.altBuffer[y] {
				v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
			}
		}
		v.SetCursorPos(0, 0)
	} else {
		if v.historyManager != nil {
			// Using HistoryManager - just append first line
			v.historyManager.AppendLine(make([]Cell, 0, v.width))
		} else {
			// Legacy circular buffer
			v.historyBuffer = make([][]Cell, v.maxHistorySize)
			v.historyHead = 0
			v.historyLen = 1
			v.historyBuffer[0] = make([]Cell, 0, v.width)
		}
		v.viewOffset = 0
		v.SetCursorPos(0, 0)
	}
}

// ClearVisibleScreen clears just the visible display (ED 2).
// Preserves scrollback history and cursor position.
func (v *VTerm) ClearVisibleScreen() {
	v.MarkAllDirty()
	if v.inAltScreen {
		// Use default colors for cleared cells
		for y := range v.altBuffer {
			for x := range v.altBuffer[y] {
				v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
			}
		}
		// Cursor position unchanged
	} else {
		// For main screen, clear all visible lines
		logicalTop := v.getTopHistoryLine()
		blankLine := make([]Cell, v.width)
		for x := 0; x < v.width; x++ {
			blankLine[x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}
		for y := 0; y < v.height; y++ {
			logicalY := logicalTop + y
			if logicalY < v.getHistoryLen() {
				v.setHistoryLine(logicalY, append([]Cell(nil), blankLine...))
			}
		}
		// Cursor position unchanged
	}
}

// SaveCursor saves the current cursor position for either the main or alt screen.
func (v *VTerm) SaveCursor() {
	if v.inAltScreen {
		v.savedAltCursorX, v.savedAltCursorY = v.cursorX, v.cursorY
	} else {
		v.savedMainCursorX, v.savedMainCursorY = v.cursorX, v.cursorY
	}
}

// RestoreCursor restores the cursor position for either the main or alt screen.
// According to xterm behavior, DECRC also resets origin mode.
func (v *VTerm) RestoreCursor() {
	v.wrapNext = false
	// Reset origin mode (xterm behavior)
	v.originMode = false
	if v.inAltScreen {
		v.SetCursorPos(v.savedAltCursorY, v.savedAltCursorX)
	} else {
		v.SetCursorPos(v.savedMainCursorY, v.savedMainCursorX)
	}
}

// --- History and Viewport Management ---

// getHistoryLen returns the current history length (from HistoryManager or legacy buffer).
func (v *VTerm) getHistoryLen() int {
	if v.historyManager != nil {
		return v.historyManager.Length()
	}
	return v.historyLen
}

// getHistoryLine retrieves a specific line from the history buffer (or HistoryManager).
func (v *VTerm) getHistoryLine(index int) []Cell {
	if v.historyManager != nil {
		return v.historyManager.GetLine(index)
	}
	// Legacy circular buffer
	if index < 0 || index >= v.getHistoryLen() {
		return nil
	}
	physicalIndex := (v.historyHead + index) % v.maxHistorySize
	return v.historyBuffer[physicalIndex]
}

// setHistoryLine updates a specific line in the history buffer (or HistoryManager).
func (v *VTerm) setHistoryLine(index int, line []Cell) {
	if v.historyManager != nil {
		v.historyManager.SetLine(index, line)
		return
	}
	// Legacy circular buffer
	if index < 0 || index >= v.getHistoryLen() {
		return
	}
	physicalIndex := (v.historyHead + index) % v.maxHistorySize
	v.historyBuffer[physicalIndex] = line
}

// appendHistoryLine adds a new line to the end of the history buffer.
func (v *VTerm) appendHistoryLine(line []Cell) {
	if v.historyManager != nil {
		v.historyManager.AppendLine(line)
		return
	}
	// Legacy circular buffer
	if v.getHistoryLen() < v.maxHistorySize {
		physicalIndex := (v.historyHead + v.getHistoryLen()) % v.maxHistorySize
		v.historyBuffer[physicalIndex] = line
		v.historyLen++
	} else {
		// Buffer is full, wrap around (overwrite the oldest line)
		v.historyHead = (v.historyHead + 1) % v.maxHistorySize
		physicalIndex := (v.historyHead + v.getHistoryLen() - 1) % v.maxHistorySize
		v.historyBuffer[physicalIndex] = line
	}
}

// getTopHistoryLine calculates the index of the first visible line in the history buffer.
func (v *VTerm) getTopHistoryLine() int {
	if v.inAltScreen {
		return 0
	}
	histLen := v.historyLen
	if v.historyManager != nil {
		histLen = v.historyManager.Length()
	}
	top := histLen - v.height - v.viewOffset
	if top < 0 {
		top = 0
	}
	return top
}

// VisibleTop returns the history index of the first visible line.
func (v *VTerm) VisibleTop() int {
	return v.getTopHistoryLine()
}

// HistoryLength exposes the number of lines tracked in history.
func (v *VTerm) HistoryLength() int {
	if v.historyManager != nil {
		return v.historyManager.Length()
	}
	return v.historyLen
}

// HistoryLineCopy returns a copy of the specified history line, or nil if out of range.
func (v *VTerm) HistoryLineCopy(index int) []Cell {
	line := v.getHistoryLine(index)
	if line == nil {
		return nil
	}
	out := make([]Cell, len(line))
	copy(out, line)
	return out
}

// --- Dirty Line Tracking for Optimized Rendering ---

func (v *VTerm) MarkDirty(line int) {
	if line >= 0 && line < v.height {
		v.dirtyLines[line] = true
	}
}

func (v *VTerm) MarkAllDirty() { v.allDirty = true }

func (v *VTerm) GetDirtyLines() (map[int]bool, bool) {
	return v.dirtyLines, v.allDirty
}

func (v *VTerm) ClearDirty() {
	v.allDirty = false
	v.dirtyLines = make(map[int]bool)
	// Always mark cursor lines to handle blinking and movement
	v.MarkDirty(v.prevCursorY)
	v.MarkDirty(v.cursorY)
}

// --- Basic Terminal Operations ---

func (v *VTerm) CarriageReturn() {
	v.wrapNext = false // Clear wrapNext when returning to start of line

	// CR behavior with left/right margins:
	// - If inside margins: go to left margin
	// - If left of left margin: go to column 0 (unless in origin mode, then go to left margin)
	// - If right of right margin: no margins, so go to column 0
	// - If at left margin: stay there
	if v.leftRightMarginMode {
		if v.originMode {
			// In origin mode: always go to left margin
			v.SetCursorPos(v.cursorY, v.marginLeft)
		} else if v.cursorX >= v.marginLeft && v.cursorX <= v.marginRight {
			// Inside margins: go to left margin
			v.SetCursorPos(v.cursorY, v.marginLeft)
		} else {
			// Outside margins (left or right): go to column 0
			v.SetCursorPos(v.cursorY, 0)
		}
	} else {
		v.SetCursorPos(v.cursorY, 0)
	}
}

func (v *VTerm) Backspace() {
	v.wrapNext = false

	// Determine minimum column based on left margin
	minX := 0
	if v.leftRightMarginMode && v.cursorX >= v.marginLeft && v.cursorX <= v.marginRight {
		// Inside margins: stop at left margin
		minX = v.marginLeft
	}

	if v.cursorX > minX {
		v.SetCursorPos(v.cursorY, v.cursorX-1)
	}
}

func (v *VTerm) Tab() {
	v.wrapNext = false
	for x := v.cursorX + 1; x < v.width; x++ {
		if v.tabStops[x] {
			v.SetCursorPos(v.cursorY, x)
			return
		}
	}
	v.SetCursorPos(v.cursorY, v.width-1)
}

// TabForward (CHT) moves cursor forward n tab stops.
// In DEC terminals (xterm), tabs stop at the right margin when DECLRMM is active.
func (v *VTerm) TabForward(n int) {
	v.wrapNext = false

	// Determine right boundary
	rightEdge := v.width - 1
	if v.leftRightMarginMode {
		rightEdge = v.marginRight
	}

	for i := 0; i < n; i++ {
		found := false
		for x := v.cursorX + 1; x <= rightEdge; x++ {
			if v.tabStops[x] {
				v.SetCursorPos(v.cursorY, x)
				found = true
				break
			}
		}
		if !found {
			// No more tab stops, move to right edge
			v.SetCursorPos(v.cursorY, rightEdge)
			break
		}
	}
}

// TabBackward (CBT) moves cursor backward n tab stops.
// CBT ignores left/right margins and can move all the way to column 1.
func (v *VTerm) TabBackward(n int) {
	v.wrapNext = false

	for i := 0; i < n; i++ {
		found := false
		// Search backward from current position
		for x := v.cursorX - 1; x >= 0; x-- {
			if v.tabStops[x] {
				v.SetCursorPos(v.cursorY, x)
				found = true
				break
			}
		}
		if !found {
			// No more tab stops, move to left edge
			v.SetCursorPos(v.cursorY, 0)
			break
		}
	}
}

// SetTabStop sets a tab stop at the current cursor column.
func (v *VTerm) SetTabStop() {
	v.tabStops[v.cursorX] = true
}

// ClearTabStop clears tab stops.
// mode 0 or default: clear tab at cursor
// mode 3: clear all tabs
func (v *VTerm) ClearTabStop(mode int) {
	switch mode {
	case 0:
		// Clear tab at cursor
		delete(v.tabStops, v.cursorX)
	case 3:
		// Clear all tabs
		v.tabStops = make(map[int]bool)
	}
}

// Reset brings the terminal to its initial state.
// DECALN (Screen Alignment Test) fills the screen with E's, resets margins, and moves cursor home.
func (v *VTerm) DECALN() {
	v.MarkAllDirty()

	// Fill entire screen with 'E' characters
	if v.inAltScreen {
		for y := 0; y < v.height; y++ {
			for x := 0; x < v.width; x++ {
				v.altBuffer[y][x] = Cell{
					Rune: 'E',
					FG:   v.defaultFG,
					BG:   v.defaultBG,
				}
			}
		}
	} else {
		// Main screen: fill visible area
		topHistory := v.getTopHistoryLine()
		for y := 0; y < v.height; y++ {
			logicalY := topHistory + y
			// Ensure line exists
			for logicalY >= v.getHistoryLen() {
				v.appendHistoryLine(make([]Cell, 0, v.width))
			}
			line := v.getHistoryLine(logicalY)
			// Resize line if needed
			if len(line) < v.width {
				newLine := make([]Cell, v.width)
				copy(newLine, line)
				v.setHistoryLine(logicalY, newLine)
				line = newLine
			}
			for x := 0; x < v.width; x++ {
				line[x] = Cell{
					Rune: 'E',
					FG:   v.defaultFG,
					BG:   v.defaultBG,
				}
			}
			v.setHistoryLine(logicalY, line)
		}
	}

	// Reset margins to full screen
	v.marginTop = 0
	v.marginBottom = v.height - 1
	v.marginLeft = 0
	v.marginRight = v.width - 1

	// Move cursor to home
	v.SetCursorPos(0, 0)
}

func (v *VTerm) Reset() {
	v.MarkAllDirty()
	v.savedMainCursorX, v.savedMainCursorY = 0, 0
	v.savedAltCursorX, v.savedAltCursorY = 0, 0
	v.ClearScreen()
	// Reset OSC defaults BEFORE ResetAttributes() so currentFG/currentBG get correct values
	v.defaultFG = DefaultFG
	v.defaultBG = DefaultBG
	v.ResetAttributes()
	v.SetMargins(0, 0)
	v.marginLeft = 0
	v.marginRight = v.width - 1
	v.leftRightMarginMode = false
	v.originMode = false
	v.cursorVisible = true
	v.wrapNext = false
	v.autoWrapMode = true
	v.insertMode = false
	v.appCursorKeys = false
	// Reset bracketed paste mode
	if v.bracketedPasteMode {
		v.bracketedPasteMode = false
		if v.OnBracketedPasteModeChange != nil {
			v.OnBracketedPasteModeChange(false)
		}
	}
	v.tabStops = make(map[int]bool)
	for i := 0; i < v.width; i++ {
		if i%8 == 0 {
			v.tabStops[i] = true
		}
	}
}

// SoftReset (DECSTR) performs a soft terminal reset.
// Unlike RIS (Reset), DECSTR does not clear the screen or move the cursor.
// It resets modes, margins, and saved state to defaults.
func (v *VTerm) SoftReset() {
	// Save current cursor position (DECSTR must not move cursor)
	savedX, savedY := v.cursorX, v.cursorY

	// Reset saved cursor position to origin
	v.savedMainCursorX, v.savedMainCursorY = 0, 0
	v.savedAltCursorX, v.savedAltCursorY = 0, 0

	// Reset modes
	v.insertMode = false
	v.originMode = false
	v.autoWrapMode = true // Keep autowrap ON (xterm compatibility)

	// Reset bracketed paste mode
	if v.bracketedPasteMode {
		v.bracketedPasteMode = false
		if v.OnBracketedPasteModeChange != nil {
			v.OnBracketedPasteModeChange(false)
		}
	}

	// Reset margins to full screen
	// Note: SetMargins() moves cursor to origin, so we restore it afterward
	v.SetMargins(0, 0) // This sets top=0, bottom=height-1
	v.marginLeft = 0
	v.marginRight = v.width - 1
	v.leftRightMarginMode = false

	// Reset graphics rendition (SGR) to normal
	v.ResetAttributes()

	// Restore cursor position (DECSTR must not move cursor)
	v.SetCursorPos(savedY, savedX)

	// Note: Does NOT clear screen or reset tab stops
}

// ReverseIndex moves the cursor up one line, scrolling down if at the top margin.
// Index moves cursor down one line, scrolling if at bottom margin.
func (v *VTerm) Index() {
	v.wrapNext = false
	// Check if cursor is outside left/right margins - if so, don't scroll
	outsideMargins := v.leftRightMarginMode && (v.cursorX < v.marginLeft || v.cursorX > v.marginRight)

	if v.cursorY == v.marginBottom {
		if !outsideMargins {
			v.scrollRegion(1, v.marginTop, v.marginBottom)
		}
		// If outside margins, stay at marginBottom (don't move past it)
	} else if v.cursorY < v.height-1 {
		v.SetCursorPos(v.cursorY+1, v.cursorX)
	}
}

// NextLine (NEL) moves cursor down one line and to the appropriate left position.
func (v *VTerm) NextLine() {
	// Save current position to detect if Index actually moved
	oldY := v.cursorY
	oldX := v.cursorX

	// First move down like Index
	v.Index()

	// Then determine horizontal position:
	// NEL always goes to left margin or column 0, EXCEPT:
	// - When cursor was LEFT of left margin and didn't move vertically (stay at column 0/1)
	if v.leftRightMarginMode {
		// With left/right margins active
		if v.cursorY == oldY && oldX < v.marginLeft {
			// Didn't move down and was left of margin: stay at current X
			// (This happens when at bottom and outside margins)
		} else {
			// Go to left margin in all other cases
			v.SetCursorPos(v.cursorY, v.marginLeft)
		}
	} else {
		// No left/right margins: always go to column 0
		v.SetCursorPos(v.cursorY, 0)
	}
}

func (v *VTerm) ReverseIndex() {
	v.wrapNext = false
	// Check if cursor is outside left/right margins - if so, don't scroll
	outsideMargins := v.leftRightMarginMode && (v.cursorX < v.marginLeft || v.cursorX > v.marginRight)

	if v.cursorY == v.marginTop {
		if !outsideMargins {
			v.scrollRegion(-1, v.marginTop, v.marginBottom)
		}
		// If outside margins, stay at marginTop (don't move past it)
	} else if v.cursorY > 0 {
		v.SetCursorPos(v.cursorY-1, v.cursorX)
	}
}

// BackIndex (DECBI) moves cursor back one column or scrolls content right.
// ESC 6 - VT420 feature for horizontal scrolling.
func (v *VTerm) BackIndex() {
	v.wrapNext = false

	// Determine effective left/right margins
	leftMargin := 0
	rightMargin := v.width - 1
	if v.leftRightMarginMode {
		leftMargin = v.marginLeft
		rightMargin = v.marginRight
	}

	// Check if cursor is outside top/bottom margins
	outsideVerticalMargins := v.cursorY < v.marginTop || v.cursorY > v.marginBottom

	// If at left margin and inside vertical margins, scroll content right
	if v.cursorX == leftMargin && !outsideVerticalMargins {
		v.scrollHorizontal(1, leftMargin, rightMargin, v.marginTop, v.marginBottom)
		// Cursor stays at left margin
	} else if v.cursorX > 0 {
		// Not at left margin or outside margins - just move cursor left
		v.SetCursorPos(v.cursorY, v.cursorX-1)
	}
	// If at column 0, stay there (can't move further left)
}

// ForwardIndex (DECFI) moves cursor forward one column or scrolls content left.
// ESC 9 - VT420 feature for horizontal scrolling.
func (v *VTerm) ForwardIndex() {
	v.wrapNext = false

	// Determine effective left/right margins
	leftMargin := 0
	rightMargin := v.width - 1
	if v.leftRightMarginMode {
		leftMargin = v.marginLeft
		rightMargin = v.marginRight
	}

	// Check if cursor is outside top/bottom margins
	outsideVerticalMargins := v.cursorY < v.marginTop || v.cursorY > v.marginBottom

	// If at right margin and inside vertical margins, scroll content left
	if v.cursorX == rightMargin && !outsideVerticalMargins {
		v.scrollHorizontal(-1, leftMargin, rightMargin, v.marginTop, v.marginBottom)
		// Cursor stays at right margin
	} else if v.cursorX < v.width-1 {
		// Not at right margin or outside margins - just move cursor right
		v.SetCursorPos(v.cursorY, v.cursorX+1)
	}
	// If at right edge of screen (width-1), stay there (can't move further right)
}

// scrollHorizontal scrolls content horizontally within specified margins.
// n > 0: scroll right (content shifts right, blank column inserted at left)
// n < 0: scroll left (content shifts left, blank column inserted at right)
func (v *VTerm) scrollHorizontal(n int, left int, right int, top int, bottom int) {
	if v.inAltScreen {
		buffer := v.altBuffer
		if n > 0 {
			// Scroll right: shift content right, insert blank at left margin
			for i := 0; i < n; i++ {
				for y := top; y <= bottom; y++ {
					if y >= len(buffer) {
						continue
					}
					line := buffer[y]
					// Ensure line is wide enough
					for len(line) <= right {
						line = append(line, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
					}
					// Shift columns right within margin region
					for x := right; x > left; x-- {
						line[x] = line[x-1]
					}
					// Insert blank at left margin
					line[left] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
					buffer[y] = line
				}
			}
		} else if n < 0 {
			// Scroll left: shift content left, insert blank at right margin
			for i := 0; i < -n; i++ {
				for y := top; y <= bottom; y++ {
					if y >= len(buffer) {
						continue
					}
					line := buffer[y]
					// Ensure line is wide enough
					for len(line) <= right {
						line = append(line, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
					}
					// Shift columns left within margin region
					for x := left; x < right; x++ {
						line[x] = line[x+1]
					}
					// Insert blank at right margin
					line[right] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
					buffer[y] = line
				}
			}
		}
	} else {
		// Main screen scrolling
		topHistory := v.getTopHistoryLine()
		if n > 0 {
			// Scroll right: shift content right, insert blank at left margin
			for i := 0; i < n; i++ {
				for y := top; y <= bottom; y++ {
					line := v.getHistoryLine(topHistory + y)
					// Ensure line is wide enough
					for len(line) <= right {
						line = append(line, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
					}
					// Shift columns right within margin region
					for x := right; x > left; x-- {
						line[x] = line[x-1]
					}
					// Insert blank at left margin
					line[left] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
					v.setHistoryLine(topHistory+y, line)
				}
			}
		} else if n < 0 {
			// Scroll left: shift content left, insert blank at right margin
			for i := 0; i < -n; i++ {
				for y := top; y <= bottom; y++ {
					line := v.getHistoryLine(topHistory + y)
					// Ensure line is wide enough
					for len(line) <= right {
						line = append(line, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
					}
					// Shift columns left within margin region
					for x := left; x < right; x++ {
						line[x] = line[x+1]
					}
					// Insert blank at right margin
					line[right] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
					v.setHistoryLine(topHistory+y, line)
				}
			}
		}
	}
	v.MarkAllDirty()
}

// InsertColumns (DECIC) inserts blank columns at cursor position.
// CSI Pn ' } - VT420 feature for horizontal scrolling.
func (v *VTerm) InsertColumns(n int) {
	// Check if cursor is outside top/bottom margins
	if v.cursorY < v.marginTop || v.cursorY > v.marginBottom {
		return
	}

	// Determine effective left/right margins
	leftMargin := 0
	rightMargin := v.width - 1
	if v.leftRightMarginMode {
		leftMargin = v.marginLeft
		rightMargin = v.marginRight
		// If cursor is outside left/right margins, do nothing
		if v.cursorX < leftMargin || v.cursorX > rightMargin {
			return
		}
	}

	// Insert n columns at cursor position, shifting content right
	// Content beyond right margin is truncated
	if v.inAltScreen {
		buffer := v.altBuffer
		for y := v.marginTop; y <= v.marginBottom; y++ {
			if y >= len(buffer) {
				continue
			}
			line := buffer[y]
			// Ensure line is wide enough
			for len(line) <= rightMargin {
				line = append(line, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
			}
			// Shift content right from cursor to right margin
			for i := 0; i < n; i++ {
				for x := rightMargin; x > v.cursorX; x-- {
					if x > 0 && x-1 < len(line) {
						line[x] = line[x-1]
					}
				}
				// Insert blank at cursor position
				if v.cursorX < len(line) {
					line[v.cursorX] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
			buffer[y] = line
		}
	} else {
		// Main screen
		topHistory := v.getTopHistoryLine()
		for y := v.marginTop; y <= v.marginBottom; y++ {
			line := v.getHistoryLine(topHistory + y)
			// Ensure line is wide enough
			for len(line) <= rightMargin {
				line = append(line, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
			}
			// Shift content right from cursor to right margin
			for i := 0; i < n; i++ {
				for x := rightMargin; x > v.cursorX; x-- {
					if x > 0 && x-1 < len(line) {
						line[x] = line[x-1]
					}
				}
				// Insert blank at cursor position
				if v.cursorX < len(line) {
					line[v.cursorX] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
			v.setHistoryLine(topHistory+y, line)
		}
	}
	v.MarkAllDirty()
}

// DeleteColumns (DECDC) deletes columns at cursor position.
// CSI Pn ' ~ - VT420 feature for horizontal scrolling.
func (v *VTerm) DeleteColumns(n int) {
	// Check if cursor is outside top/bottom margins
	if v.cursorY < v.marginTop || v.cursorY > v.marginBottom {
		return
	}

	// Determine effective left/right margins
	leftMargin := 0
	rightMargin := v.width - 1
	if v.leftRightMarginMode {
		leftMargin = v.marginLeft
		rightMargin = v.marginRight
		// If cursor is outside left/right margins, do nothing
		if v.cursorX < leftMargin || v.cursorX > rightMargin {
			return
		}
	}

	// Delete n columns at cursor position, shifting content left
	// Blank columns inserted at right margin
	if v.inAltScreen {
		buffer := v.altBuffer
		for y := v.marginTop; y <= v.marginBottom; y++ {
			if y >= len(buffer) {
				continue
			}
			line := buffer[y]
			// Ensure line is wide enough
			for len(line) <= rightMargin {
				line = append(line, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
			}
			// Shift content left from cursor to right margin
			for i := 0; i < n; i++ {
				for x := v.cursorX; x < rightMargin; x++ {
					if x+1 < len(line) {
						line[x] = line[x+1]
					}
				}
				// Insert blank at right margin
				if rightMargin < len(line) {
					line[rightMargin] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
			buffer[y] = line
		}
	} else {
		// Main screen
		topHistory := v.getTopHistoryLine()
		for y := v.marginTop; y <= v.marginBottom; y++ {
			line := v.getHistoryLine(topHistory + y)
			// Ensure line is wide enough
			for len(line) <= rightMargin {
				line = append(line, Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG})
			}
			// Shift content left from cursor to right margin
			for i := 0; i < n; i++ {
				for x := v.cursorX; x < rightMargin; x++ {
					if x+1 < len(line) {
						line[x] = line[x+1]
					}
				}
				// Insert blank at right margin
				if rightMargin < len(line) {
					line[rightMargin] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
			v.setHistoryLine(topHistory+y, line)
		}
	}
	v.MarkAllDirty()
}

// --- Core CSI Dispatch ---

// ProcessCSI interprets a parsed CSI sequence and calls the appropriate handler.
func (v *VTerm) ProcessCSI(command rune, params []int, intermediate rune, private bool) {
	param := func(i int, defaultVal int) int {
		if i < len(params) && params[i] != 0 {
			return params[i]
		}
		return defaultVal
	}

	if intermediate == '!' && command == 'p' { // DECSTR - Soft Terminal Reset
		v.SoftReset()
		return
	}

	if intermediate == '$' && command == 'p' { // DECRQM - Request Mode
		if mode := param(0, 0); mode > 0 {
			response := fmt.Sprintf("\x1b[?%d;0$y", mode) // Default: Not Supported
			if mode == 2026 {                             // We support Synchronized Output
				response = "\x1b[?2026;1$y"
			}
			if v.WriteToPty != nil {
				v.WriteToPty([]byte(response))
			}
		}
		return
	}

	if intermediate == '\'' && command == '}' { // DECIC - Insert Column
		v.InsertColumns(param(0, 1))
		return
	}

	if intermediate == '\'' && command == '~' { // DECDC - Delete Column
		v.DeleteColumns(param(0, 1))
		return
	}

	// Handle DA2 (Secondary Device Attributes) - CSI > c
	if command == 'c' && intermediate == '>' {
		// Response: CSI > Ps ; Pv ; Pc c
		// Ps = Terminal type (1=VT220, 24=VT320, 41=VT420, 64=VT520)
		// Pv = Firmware version (e.g., 100 for version 1.0.0)
		// Pc = Keyboard type (0)
		// We claim to be VT220 (1) with version 100 and keyboard type 0
		response := "\x1b[>1;100;0c"
		if v.WriteToPty != nil {
			v.WriteToPty([]byte(response))
		}
		return
	}

	// Handle mode setting/resetting (SM/RM for ANSI modes, DECSET/DECRESET for DEC private modes)
	if command == 'h' || command == 'l' {
		if private {
			v.processPrivateCSI(command, params)
		} else {
			v.processANSIMode(command, params)
		}
		return
	}

	switch command {
	case 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'f', 'd', '`', 'a', 'e':
		v.handleCursorMovement(command, params)
	case 'I': // CHT - Cursor Horizontal Tab
		v.TabForward(param(0, 1))
	case 'Z': // CBT - Cursor Backward Tab
		v.TabBackward(param(0, 1))
	case 'J', 'K', 'P', 'X', 'b':
		v.handleErase(command, params)
	case '@':
		v.InsertCharacters(param(0, 1))
	case 'L':
		v.InsertLines(param(0, 1))
	case 'M':
		v.DeleteLines(param(0, 1))
	case 'S': // SU - Scroll Up
		if v.leftRightMarginMode {
			v.scrollUpWithinMargins(param(0, 1))
		} else {
			v.scrollRegion(param(0, 1), v.marginTop, v.marginBottom)
		}
	case 'T': // SD - Scroll Down
		if v.leftRightMarginMode {
			v.scrollDownWithinMargins(param(0, 1))
		} else {
			v.scrollRegion(-param(0, 1), v.marginTop, v.marginBottom)
		}
	case 'm':
		v.handleSGR(params)
	case 'n': // DSR - Device Status Report
		if param(0, 0) == 6 { // Report Cursor Position
			response := fmt.Sprintf("\x1b[%d;%dR", v.cursorY+1, v.cursorX+1)
			if v.WriteToPty != nil {
				v.WriteToPty([]byte(response))
			}
		}
	case 'r': // DECSTBM - Set Top and Bottom Margins
		v.SetMargins(param(0, 1), param(1, v.height))
	case 's':
		// When DECLRMM is enabled, 's' is DECSLRM (Set Left/Right Margins)
		// Otherwise, it's SCOSC (Save Cursor)
		if v.leftRightMarginMode {
			// DECSLRM - Set Left and Right Margins
			v.SetLeftRightMargins(param(0, 1), param(1, v.width))
		} else {
			v.SaveCursor()
		}
	case 'u':
		v.RestoreCursor()
	case 'c': // DA - Primary Device Attributes
		// Response: CSI ? Ps ; Ps ; ... c
		// Ps values:
		//   62 = VT220, 63 = VT320, 64 = VT420, 65 = VT520
		//   1 = 132 columns, 2 = printer, 4 = sixel graphics
		//   6 = selective erase, 9 = national replacement character-sets
		//   15 = DEC technical set, 21 = horizontal scrolling
		//   22 = color, 28 = rectangular editing, 29 = ANSI text locator
		// We claim VT220 (62) with color (22), selective erase (6),
		// horizontal scrolling (21), and rectangular editing (28)
		response := "\x1b[?62;6;21;22;28c"
		if v.WriteToPty != nil {
			v.WriteToPty([]byte(response))
		}
	case 'g': // TBC - Tab Clear
		v.ClearTabStop(param(0, 0))
	case 'q', 't':
		// Ignore DECSCA, window manipulation
	default:
		log.Printf("Parser: Unhandled CSI sequence: %q, params: %v", command, params)
	}
}

// --- CSI Sub-Handlers ---

func (v *VTerm) handleCursorMovement(command rune, params []int) {
	param := func(i int, defaultVal int) int {
		if i < len(params) && params[i] != 0 {
			return params[i]
		}
		return defaultVal
	}
	switch command {
	case 'A':
		v.MoveCursorUp(param(0, 1))
	case 'B':
		v.MoveCursorDown(param(0, 1))
	case 'C':
		v.MoveCursorForward(param(0, 1))
	case 'D':
		v.MoveCursorBackward(param(0, 1))
	case 'E':
		// CNL - Cursor Next Line: move down N lines and to column 0
		v.MoveCursorDown(param(0, 1))
		v.SetCursorPos(v.cursorY, 0)
	case 'F':
		// CPL - Cursor Previous Line: move up N lines and to column 0
		v.MoveCursorUp(param(0, 1))
		v.SetCursorPos(v.cursorY, 0)
	case 'G': // CHA - Cursor Horizontal Absolute
		col := param(0, 1) - 1
		// In origin mode, column is relative to left margin
		if v.originMode {
			col += v.marginLeft
		}
		v.SetCursorPos(v.cursorY, col)
	case 'H', 'f': // CUP - Cursor Position
		row := param(0, 1) - 1
		col := param(1, 1) - 1
		// In origin mode, coordinates are relative to scroll region
		if v.originMode {
			row += v.marginTop
			col += v.marginLeft
		}
		v.SetCursorPos(row, col)
	case 'd': // VPA - Vertical Position Absolute
		row := param(0, 1) - 1
		// In origin mode, row is relative to top margin
		if v.originMode {
			row += v.marginTop
		}
		v.SetCursorPos(row, v.cursorX)
	case '`': // HPA - Horizontal Position Absolute
		col := param(0, 1) - 1
		// In origin mode, column is relative to left margin
		if v.originMode {
			col += v.marginLeft
		}
		v.SetCursorPos(v.cursorY, col)
	case 'a': // HPR - Horizontal Position Relative
		// Move right by n columns (relative, not absolute)
		n := param(0, 1)
		newX := v.cursorX + n
		// Clamp to right edge
		if newX >= v.width {
			newX = v.width - 1
		}
		v.SetCursorPos(v.cursorY, newX)
	case 'e': // VPR - Vertical Position Relative
		// Move down by n rows (relative, not absolute)
		n := param(0, 1)
		newY := v.cursorY + n
		// Clamp to bottom edge
		if newY >= v.height {
			newY = v.height - 1
		}
		v.SetCursorPos(newY, v.cursorX)
	}
}

func (v *VTerm) handleErase(command rune, params []int) {
	param := func(i int, defaultVal int) int {
		if i < len(params) && params[i] != 0 {
			return params[i]
		}
		return defaultVal
	}
	v.wrapNext = false
	switch command {
	case 'J': // Erase in Display
		v.ClearScreenMode(param(0, 0))
	case 'K': // Erase in Line
		v.ClearLine(param(0, 0))
	case 'P': // Delete Character
		v.DeleteCharacters(param(0, 1))
	case 'X': // Erase Character
		v.EraseCharacters(param(0, 1))
	case 'b': // REP - Repeat previous graphic character
		v.RepeatCharacter(param(0, 1))
	}
}

func (v *VTerm) ClearScreenMode(mode int) {
	v.MarkAllDirty()
	switch mode {
	case 0: // Erase from cursor to end of screen
		v.ClearLine(0) // Clear from cursor to end of current line
		if v.inAltScreen {
			// Clear all lines below cursor
			for y := v.cursorY + 1; y < v.height; y++ {
				for x := 0; x < v.width; x++ {
					v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
		} else {
			// For main screen, clear all lines below cursor by clearing them individually
			logicalY := v.cursorY + v.getTopHistoryLine()
			endY := v.getTopHistoryLine() + v.height
			blankLine := make([]Cell, v.width)
			for x := 0; x < v.width; x++ {
				blankLine[x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}
			// Ensure all lines exist in history up to end of viewport
			for v.getHistoryLen() < endY {
				v.appendHistoryLine(make([]Cell, 0, v.width))
			}
			// Now clear lines below cursor
			for y := logicalY + 1; y < endY; y++ {
				v.setHistoryLine(y, append([]Cell(nil), blankLine...))
			}
		}
	case 1: // Erase from beginning of screen to cursor
		v.ClearLine(1)
		if v.inAltScreen {
			for y := 0; y < v.cursorY; y++ {
				for x := 0; x < v.width; x++ {
					v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
		} else {
			logicalY := v.cursorY + v.getTopHistoryLine()
			blankLine := make([]Cell, v.width)
			for x := 0; x < v.width; x++ {
				blankLine[x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}
			for i := v.getTopHistoryLine(); i < logicalY; i++ {
				v.setHistoryLine(i, append([]Cell(nil), blankLine...))
			}
		}
	case 2: // Erase entire visible screen (ED 2)
		v.ClearVisibleScreen()
	case 3: // Erase scrollback only, leave visible screen intact (ED 3)
		if !v.inAltScreen {
			// Clear scrollback by resetting history to only contain visible screen
			topHistory := v.getTopHistoryLine()
			newHistory := make([][]Cell, v.maxHistorySize)

			// Copy visible screen lines to new history buffer starting at position 0
			for i := 0; i < v.height; i++ {
				oldLine := v.getHistoryLine(topHistory + i)
				if oldLine != nil {
					newHistory[i] = append([]Cell(nil), oldLine...)
				} else {
					newHistory[i] = make([]Cell, 0, v.width)
				}
			}

			// Replace history buffer and reset pointers
			v.historyBuffer = newHistory
			v.historyHead = 0
			v.historyLen = v.height
			v.viewOffset = 0
		}
		// On alt screen, ED 3 does nothing (no scrollback to clear)
	}
}

func (v *VTerm) ClearLine(mode int) {
	v.MarkDirty(v.cursorY)
	var line []Cell
	var logicalY int
	if v.inAltScreen {
		line = v.altBuffer[v.cursorY]
	} else {
		logicalY = v.cursorY + v.getTopHistoryLine()
		// Ensure line exists
		for v.getHistoryLen() <= logicalY {
			v.appendHistoryLine(make([]Cell, 0, v.width))
		}
		line = v.getHistoryLine(logicalY)
		if line == nil {
			line = make([]Cell, 0, v.width)
		}
	}
	start, end := 0, v.width
	switch mode {
	case 0: // Erase from cursor to end
		start = v.cursorX
	case 1: // Erase from beginning to cursor
		end = v.cursorX + 1
	case 2: // Erase entire line
	}

	for len(line) < v.width { // Ensure line is full width before clearing
		line = append(line, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
	}

	for x := start; x < end; x++ {
		if x < len(line) {
			line[x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}
	}

	// If we're erasing to the end of line, truncate any content beyond 'end'
	// This handles cases where the history line is longer than the terminal width
	if mode == 0 || mode == 2 { // EL 0 (cursor to end) or EL 2 (entire line)
		if len(line) > end {
			line = line[:end]
		}
	}

	// Write the modified line back to the appropriate buffer
	if v.inAltScreen {
		v.altBuffer[v.cursorY] = line
	} else {
		v.setHistoryLine(logicalY, line)
	}
}

func (v *VTerm) EraseCharacters(n int) {
	v.MarkDirty(v.cursorY)
	var line []Cell
	logicalY := v.cursorY + v.getTopHistoryLine()
	if v.inAltScreen {
		line = v.altBuffer[v.cursorY]
	} else {
		// Ensure line exists
		for v.getHistoryLen() <= logicalY {
			v.appendHistoryLine(make([]Cell, 0, v.width))
		}
		line = v.getHistoryLine(logicalY)
		if line == nil {
			line = make([]Cell, 0, v.width)
		}
	}

	for i := 0; i < n; i++ {
		if v.cursorX+i < len(line) {
			line[v.cursorX+i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}
	}

	// Write the modified line back to the appropriate buffer
	if v.inAltScreen {
		v.altBuffer[v.cursorY] = line
	} else {
		v.setHistoryLine(logicalY, line)
	}
}

// In texelterm/parser/vterm.go
func (v *VTerm) InsertCharacters(n int) {
	v.MarkDirty(v.cursorY)
	if v.cursorX >= v.width {
		return
	}

	// Determine the effective right boundary
	rightBoundary := v.width
	if v.leftRightMarginMode {
		// If DECLRMM is enabled and cursor is outside margins, do nothing
		if v.cursorX < v.marginLeft || v.cursorX > v.marginRight {
			return
		}
		rightBoundary = v.marginRight + 1
	}

	if v.inAltScreen {
		line := v.altBuffer[v.cursorY]
		// Calculate how many chars to copy and where they should end
		segmentStart := v.cursorX
		segmentEnd := rightBoundary
		if segmentEnd > len(line) {
			segmentEnd = len(line)
		}

		// Create a copy of the segment that will be shifted
		segmentLen := segmentEnd - segmentStart
		if segmentLen > 0 {
			segment := make([]Cell, segmentLen)
			copy(segment, line[segmentStart:segmentEnd])

			// Insert blanks at cursor position
			blanksToInsert := n
			if v.cursorX+blanksToInsert > rightBoundary {
				blanksToInsert = rightBoundary - v.cursorX
			}
			for i := 0; i < blanksToInsert; i++ {
				line[v.cursorX+i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}

			// Copy the original segment back, shifted right
			destStart := v.cursorX + n
			if destStart < rightBoundary {
				toCopy := rightBoundary - destStart
				if toCopy > len(segment) {
					toCopy = len(segment)
				}
				copy(line[destStart:rightBoundary], segment[:toCopy])
			}
		}
	} else {
		// Main screen with history buffer
		logicalY := v.cursorY + v.getTopHistoryLine()
		line := v.getHistoryLine(logicalY)

		// Insert blanks at cursor position
		blanks := make([]Cell, n)
		for i := range blanks {
			blanks[i] = Cell{Rune: ' '}
		}

		if v.leftRightMarginMode {
			// With left/right margins: preserve everything outside margins
			// Build: [before cursor] + [blanks] + [cursor to right margin - n chars] + [after right margin]
			newLine := append([]Cell{}, line[:v.cursorX]...)
			newLine = append(newLine, blanks...)

			// Add shifted content within margins (up to right boundary - n)
			copyEnd := rightBoundary - n
			if copyEnd > len(line) {
				copyEnd = len(line)
			}
			if copyEnd > v.cursorX {
				newLine = append(newLine, line[v.cursorX:copyEnd]...)
			}

			// Preserve everything after the right margin
			if rightBoundary < len(line) {
				newLine = append(newLine, line[rightBoundary:]...)
			}

			v.setHistoryLine(logicalY, newLine)
		} else {
			// No margins: insert and shift entire line
			newLine := append(line[:v.cursorX], append(blanks, line[v.cursorX:]...)...)
			v.setHistoryLine(logicalY, newLine)
		}
	}
}

// In texelterm/parser/vterm.go
func (v *VTerm) DeleteCharacters(n int) {
	v.MarkDirty(v.cursorY)
	if v.cursorX >= v.width {
		return
	}

	// Determine the effective right boundary
	rightBoundary := v.width
	if v.leftRightMarginMode {
		// If DECLRMM is enabled and cursor is outside margins, do nothing
		if v.cursorX < v.marginLeft || v.cursorX > v.marginRight {
			return
		}
		rightBoundary = v.marginRight + 1
	}

	if v.inAltScreen {
		line := v.altBuffer[v.cursorY]
		if v.cursorX < len(line) {
			// Determine how many characters to delete
			deleteCount := n
			if v.cursorX+deleteCount > rightBoundary {
				deleteCount = rightBoundary - v.cursorX
			}

			// Shift characters from the right to the left within the boundary
			copySrcStart := v.cursorX + deleteCount
			if copySrcStart < rightBoundary {
				// Shift characters from the right to the left
				copyLen := rightBoundary - copySrcStart
				copy(line[v.cursorX:v.cursorX+copyLen], line[copySrcStart:rightBoundary])
			}

			// Clear the now-empty cells at the end of the region
			clearStart := rightBoundary - deleteCount
			if clearStart < v.cursorX {
				clearStart = v.cursorX
			}
			for i := clearStart; i < rightBoundary; i++ {
				line[i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}
		}
	} else {
		// Main screen with history buffer
		logicalY := v.cursorY + v.getTopHistoryLine()
		line := v.getHistoryLine(logicalY)
		if v.cursorX >= len(line) {
			return
		}

		// For main screen, expand line if needed to rightBoundary
		if len(line) < rightBoundary {
			expanded := make([]Cell, rightBoundary)
			copy(expanded, line)
			for i := len(line); i < rightBoundary; i++ {
				expanded[i] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
			}
			line = expanded
		}

		// Delete characters within the boundary
		deleteCount := n
		if v.cursorX+deleteCount > rightBoundary {
			deleteCount = rightBoundary - v.cursorX
		}

		// Shift characters left
		copy(line[v.cursorX:rightBoundary-deleteCount], line[v.cursorX+deleteCount:rightBoundary])

		// Clear the now-empty cells at the end
		for i := rightBoundary - deleteCount; i < rightBoundary; i++ {
			line[i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}

		v.setHistoryLine(logicalY, line)
	}
}

// RepeatCharacter (REP) repeats the last graphic character n times.
// REP respects both left/right and top/bottom margins.
func (v *VTerm) RepeatCharacter(n int) {
	if v.lastGraphicChar == 0 {
		return // No character to repeat
	}

	// Repeat the character n times
	for i := 0; i < n; i++ {
		v.placeChar(v.lastGraphicChar)
	}
}

func (v *VTerm) InsertLines(n int) {
	v.wrapNext = false

	// Check if cursor is within top/bottom margins
	if v.cursorY < v.marginTop || v.cursorY > v.marginBottom {
		return
	}

	// Check if cursor is within left/right margins when DECLRMM is active
	if v.leftRightMarginMode && (v.cursorX < v.marginLeft || v.cursorX > v.marginRight) {
		return
	}

	// When DECLRMM is active, only insert content within left/right margins
	if v.leftRightMarginMode {
		v.insertLinesWithinMargins(n)
	} else {
		v.insertFullLines(n)
	}
	v.MarkAllDirty()
}

// insertFullLines inserts entire blank lines (traditional IL behavior)
func (v *VTerm) insertFullLines(n int) {
	// IL works within the scroll region for both alt and main screens
	// Shift lines down, starting from bottom to avoid overwriting source data
	topHistory := v.getTopHistoryLine()

	for i := 0; i < n; i++ {
		if v.inAltScreen {
			// Alt screen: simple array shifting
			for y := v.marginBottom - 1; y >= v.cursorY; y-- {
				if y+1 <= v.marginBottom {
					v.altBuffer[y+1] = v.altBuffer[y]
				}
			}
			// Create blank line at cursor position
			v.altBuffer[v.cursorY] = make([]Cell, v.width)
			for j := range v.altBuffer[v.cursorY] {
				v.altBuffer[v.cursorY][j] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
			}
		} else {
			// Main screen: ensure history has enough lines first
			endLogicalY := topHistory + v.marginBottom
			for v.getHistoryLen() <= endLogicalY {
				v.appendHistoryLine(make([]Cell, 0, v.width))
			}

			// Shift lines down within the scroll region
			for y := v.marginBottom - 1; y >= v.cursorY; y-- {
				if y+1 <= v.marginBottom {
					srcLine := v.getHistoryLine(topHistory + y)
					dstLine := make([]Cell, len(srcLine))
					copy(dstLine, srcLine)
					v.setHistoryLine(topHistory+y+1, dstLine)
				}
			}
			// Create blank line at cursor position
			v.setHistoryLine(topHistory+v.cursorY, make([]Cell, 0, v.width))
		}
	}
}

// insertLinesWithinMargins inserts blank content within left/right margins only
func (v *VTerm) insertLinesWithinMargins(n int) {
	leftCol := v.marginLeft
	rightCol := v.marginRight

	if v.inAltScreen {
		// Shift content within margins downward
		for y := v.marginBottom; y >= v.cursorY+n; y-- {
			srcY := y - n
			if srcY >= v.cursorY {
				// Copy the margin region from source line to current line
				copy(v.altBuffer[y][leftCol:rightCol+1], v.altBuffer[srcY][leftCol:rightCol+1])
			}
		}
		// Clear the top n lines' margin regions (starting at cursor)
		for y := v.cursorY; y < v.cursorY+n && y <= v.marginBottom; y++ {
			if y >= 0 && y < v.height {
				for x := leftCol; x <= rightCol; x++ {
					v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
				}
			}
		}
	} else {
		// Main screen with history buffer
		topHistory := v.getTopHistoryLine()

		// Shift content within margins downward
		for y := v.marginBottom; y >= v.cursorY+n; y-- {
			srcY := y - n
			if srcY >= v.cursorY {
				dstLine := v.getHistoryLine(topHistory + y)
				srcLine := v.getHistoryLine(topHistory + srcY)

				// Ensure lines are wide enough
				for len(dstLine) <= rightCol {
					dstLine = append(dstLine, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}
				for len(srcLine) <= rightCol {
					srcLine = append(srcLine, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}

				// Copy margin region
				copy(dstLine[leftCol:rightCol+1], srcLine[leftCol:rightCol+1])
				v.setHistoryLine(topHistory+y, dstLine)
			}
		}

		// Clear the top n lines' margin regions
		for y := v.cursorY; y < v.cursorY+n && y <= v.marginBottom; y++ {
			if y >= 0 {
				line := v.getHistoryLine(topHistory + y)
				for len(line) <= rightCol {
					line = append(line, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}
				for x := leftCol; x <= rightCol; x++ {
					line[x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
				}
				v.setHistoryLine(topHistory+y, line)
			}
		}
	}
}

func (v *VTerm) insertHistoryLine(index int, line []Cell) {
	if index < 0 || index > v.getHistoryLen() {
		return
	}
	if v.getHistoryLen() < v.maxHistorySize {
		physicalInsertIndex := (v.historyHead + index) % v.maxHistorySize
		// Shift existing lines to make room
		for i := v.getHistoryLen(); i > index; i-- {
			srcPhysical := (v.historyHead + i - 1 + v.maxHistorySize) % v.maxHistorySize
			dstPhysical := (v.historyHead + i) % v.maxHistorySize
			v.historyBuffer[dstPhysical] = v.historyBuffer[srcPhysical]
		}
		v.historyBuffer[physicalInsertIndex] = line
		v.historyLen++
	} else {
		// If buffer is full, we can't properly insert in the middle,
		// so we just overwrite the line at the index.
		v.setHistoryLine(index, line)
	}
}

func (v *VTerm) DeleteLines(n int) {
	v.wrapNext = false

	// Check if cursor is within top/bottom margins
	if v.cursorY < v.marginTop || v.cursorY > v.marginBottom {
		return
	}

	// Check if cursor is within left/right margins when DECLRMM is active
	if v.leftRightMarginMode && (v.cursorX < v.marginLeft || v.cursorX > v.marginRight) {
		return
	}

	// When DECLRMM is active, only delete content within left/right margins
	if v.leftRightMarginMode {
		v.deleteLinesWithinMargins(n)
	} else {
		v.deleteFullLines(n)
	}
	v.MarkAllDirty()
}

// deleteFullLines deletes entire lines (traditional DL behavior)
func (v *VTerm) deleteFullLines(n int) {
	// DL works within the scroll region for both alt and main screens
	topHistory := v.getTopHistoryLine()

	for i := 0; i < n; i++ {
		if v.inAltScreen {
			// Alt screen: shift lines up
			for y := v.cursorY; y < v.marginBottom; y++ {
				v.altBuffer[y] = v.altBuffer[y+1]
			}
			// Create blank line at bottom of region
			v.altBuffer[v.marginBottom] = make([]Cell, v.width)
			for x := range v.altBuffer[v.marginBottom] {
				v.altBuffer[v.marginBottom][x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
			}
		} else {
			// Main screen: ensure history has enough lines first
			endLogicalY := topHistory + v.marginBottom
			for v.getHistoryLen() <= endLogicalY {
				v.appendHistoryLine(make([]Cell, 0, v.width))
			}

			// Shift lines up within the scroll region
			for y := v.cursorY; y < v.marginBottom; y++ {
				srcLine := v.getHistoryLine(topHistory + y + 1)
				dstLine := make([]Cell, len(srcLine))
				copy(dstLine, srcLine)
				v.setHistoryLine(topHistory+y, dstLine)
			}
			// Create blank line at bottom of region
			v.setHistoryLine(topHistory+v.marginBottom, make([]Cell, 0, v.width))
		}
	}
}

// deleteLinesWithinMargins deletes content within left/right margins only
func (v *VTerm) deleteLinesWithinMargins(n int) {
	leftCol := v.marginLeft
	rightCol := v.marginRight

	if v.inAltScreen {
		// Shift content within margins upward
		for y := v.cursorY; y <= v.marginBottom-n; y++ {
			srcY := y + n
			if srcY <= v.marginBottom {
				// Copy the margin region from source line to current line
				copy(v.altBuffer[y][leftCol:rightCol+1], v.altBuffer[srcY][leftCol:rightCol+1])
			}
		}
		// Clear the bottom n lines' margin regions (clamped to cursor position)
		clearStart := v.marginBottom - n + 1
		if clearStart < v.cursorY {
			clearStart = v.cursorY
		}
		for y := clearStart; y <= v.marginBottom; y++ {
			if y >= 0 && y < v.height {
				for x := leftCol; x <= rightCol; x++ {
					v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
				}
			}
		}
	} else {
		// Main screen with history buffer
		topHistory := v.getTopHistoryLine()

		// Shift content within margins upward
		for y := v.cursorY; y <= v.marginBottom-n; y++ {
			srcY := y + n
			if srcY <= v.marginBottom {
				dstLine := v.getHistoryLine(topHistory + y)
				srcLine := v.getHistoryLine(topHistory + srcY)

				// Ensure lines are wide enough
				for len(dstLine) <= rightCol {
					dstLine = append(dstLine, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}
				for len(srcLine) <= rightCol {
					srcLine = append(srcLine, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}

				// Copy margin region
				copy(dstLine[leftCol:rightCol+1], srcLine[leftCol:rightCol+1])
				v.setHistoryLine(topHistory+y, dstLine)
			}
		}

		// Clear the bottom n lines' margin regions (clamped to cursor position)
		clearStart := v.marginBottom - n + 1
		if clearStart < v.cursorY {
			clearStart = v.cursorY
		}
		for y := clearStart; y <= v.marginBottom; y++ {
			if y >= 0 {
				line := v.getHistoryLine(topHistory + y)
				for len(line) <= rightCol {
					line = append(line, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
				}
				for x := leftCol; x <= rightCol; x++ {
					line[x] = Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG}
				}
				v.setHistoryLine(topHistory+y, line)
			}
		}
	}
}

func (v *VTerm) deleteHistoryLine(index int) {
	if index < 0 || index >= v.getHistoryLen() {
		return
	}
	// Shift lines up to fill the gap
	for i := index; i < v.getHistoryLen()-1; i++ {
		srcPhysical := (v.historyHead + i + 1) % v.maxHistorySize
		dstPhysical := (v.historyHead + i) % v.maxHistorySize
		v.historyBuffer[dstPhysical] = v.historyBuffer[srcPhysical]
	}
	v.historyLen--
}

func (v *VTerm) handleSGR(params []int) {
	i := 0
	if len(params) == 0 {
		params = []int{0}
	}
	for i < len(params) {
		p := params[i]
		switch {
		case p == 0:
			v.ResetAttributes()
		case p == 1:
			v.SetAttribute(AttrBold)
		case p == 4:
			v.SetAttribute(AttrUnderline)
		case p == 7:
			v.SetAttribute(AttrReverse)
		case p == 22:
			v.ClearAttribute(AttrBold)
		case p == 24:
			v.ClearAttribute(AttrUnderline)
		case p == 27:
			v.ClearAttribute(AttrReverse)
		case p >= 30 && p <= 37:
			v.currentFG = Color{Mode: ColorModeStandard, Value: uint8(p - 30)}
		case p == 39:
			v.currentFG = v.defaultFG
		case p >= 40 && p <= 47:
			v.currentBG = Color{Mode: ColorModeStandard, Value: uint8(p - 40)}
		case p == 49:
			v.currentBG = v.defaultBG
		case p == 38: // Set extended foreground color
			if i+2 < len(params) && params[i+1] == 5 { // 256-color palette
				v.currentFG = Color{Mode: ColorMode256, Value: uint8(params[i+2])}
				i += 2
			} else if i+4 < len(params) && params[i+1] == 2 { // RGB true-color
				v.currentFG = Color{Mode: ColorModeRGB, R: uint8(params[i+2]), G: uint8(params[i+3]), B: uint8(params[i+4])}
				i += 4
			}
		case p == 48: // Set extended background color
			if i+2 < len(params) && params[i+1] == 5 { // 256-color palette
				v.currentBG = Color{Mode: ColorMode256, Value: uint8(params[i+2])}
				i += 2
			} else if i+4 < len(params) && params[i+1] == 2 { // RGB true-color
				v.currentBG = Color{Mode: ColorModeRGB, R: uint8(params[i+2]), G: uint8(params[i+3]), B: uint8(params[i+4])}
				i += 4
			}
		case p >= 90 && p <= 97: // Bright foreground
			v.currentFG = Color{Mode: ColorModeStandard, Value: uint8(p - 90 + 8)}
		case p >= 100 && p <= 107: // Bright background
			v.currentBG = Color{Mode: ColorModeStandard, Value: uint8(p - 100 + 8)}
		}
		i++
	}
}

func (v *VTerm) SetMargins(top, bottom int) {
	if top == 0 {
		top = 1
	}
	if bottom == 0 || bottom > v.height {
		bottom = v.height
	}
	if top >= bottom {
		// Invalid region, reset to full screen
		v.marginTop = 0
		v.marginBottom = v.height - 1
		return
	}
	v.marginTop = top - 1
	v.marginBottom = bottom - 1
	// Per spec, DECSTBM moves cursor to home position (1,1)
	v.SetCursorPos(0, 0)
}

func (v *VTerm) SetLeftRightMargins(left, right int) {
	if left == 0 {
		left = 1
	}
	if right == 0 || right > v.width {
		right = v.width
	}
	if left >= right {
		// Invalid region, reset to full width
		v.marginLeft = 0
		v.marginRight = v.width - 1
		return
	}
	v.marginLeft = left - 1
	v.marginRight = right - 1
}

func (v *VTerm) MoveCursorForward(n int) {
	newX := v.cursorX + n

	// Apply left/right margin constraints only if cursor is currently inside the region
	if v.leftRightMarginMode && v.cursorX >= v.marginLeft && v.cursorX <= v.marginRight {
		// Inside scroll region - constrain to right margin
		if newX > v.marginRight {
			newX = v.marginRight
		}
	} else {
		// Outside scroll region or no margins - constrain to right edge of screen
		if newX >= v.width {
			newX = v.width - 1
		}
	}
	v.SetCursorPos(v.cursorY, newX)
}

func (v *VTerm) MoveCursorBackward(n int) {
	newX := v.cursorX - n

	// Apply left/right margin constraints only if cursor is currently inside the region
	if v.leftRightMarginMode && v.cursorX >= v.marginLeft && v.cursorX <= v.marginRight {
		// Inside scroll region - constrain to left margin
		if newX < v.marginLeft {
			newX = v.marginLeft
		}
	} else {
		// Outside scroll region or no margins - constrain to left edge of screen
		if newX < 0 {
			newX = 0
		}
	}
	v.SetCursorPos(v.cursorY, newX)
}

func (v *VTerm) MoveCursorUp(n int) {
	v.wrapNext = false
	newY := v.cursorY - n

	// Apply scroll region constraints only if cursor is currently inside the region
	if v.cursorY >= v.marginTop && v.cursorY <= v.marginBottom {
		// Inside scroll region - constrain to top margin
		if newY < v.marginTop {
			newY = v.marginTop
		}
	} else {
		// Outside scroll region - constrain to top of screen
		if newY < 0 {
			newY = 0
		}
	}
	v.SetCursorPos(newY, v.cursorX)
}

func (v *VTerm) MoveCursorDown(n int) {
	v.wrapNext = false
	newY := v.cursorY + n

	// Apply scroll region constraints only if cursor is currently inside the region
	if v.cursorY >= v.marginTop && v.cursorY <= v.marginBottom {
		// Inside scroll region - constrain to bottom margin
		if newY > v.marginBottom {
			newY = v.marginBottom
		}
	} else {
		// Outside scroll region - constrain to bottom of screen
		if newY >= v.height {
			newY = v.height - 1
		}
	}
	v.SetCursorPos(newY, v.cursorX)
}

func (v *VTerm) SetAttribute(a Attribute) { v.currentAttr |= a }

func (v *VTerm) ClearAttribute(a Attribute) { v.currentAttr &^= a }

func (v *VTerm) ResetAttributes() {
	v.currentFG = v.defaultFG
	v.currentBG = v.defaultBG
	v.currentAttr = 0
}

func (v *VTerm) SetCursorVisible(visible bool) {
	if v.cursorVisible != visible {
		v.cursorVisible = visible
		v.MarkDirty(v.cursorY)
	}
}

func (c Color) String() string {
	switch c.Mode {
	case ColorModeDefault:
		return "Default"
	case ColorModeStandard:
		return fmt.Sprintf("Palette#%d", c.Value)
	case ColorMode256:
		return fmt.Sprintf("Palette#%d", c.Value)
	case ColorModeRGB:
		return fmt.Sprintf("rgb:%02x/%02x/%02x", c.R, c.G, c.B)
	}
	return "Invalid"
}

// --- Options and Configuration ---

type Option func(*VTerm)

func WithPtyWriter(writer func([]byte)) Option { return func(v *VTerm) { v.WriteToPty = writer } }

func WithTitleChangeHandler(handler func(string)) Option {
	return func(v *VTerm) { v.TitleChanged = handler }
}

func WithWrap(enabled bool) Option {
	return func(v *VTerm) { v.wrapEnabled = enabled }
}

func WithReflow(enabled bool) Option {
	return func(v *VTerm) { v.reflowEnabled = enabled }
}

func (v *VTerm) SetTitle(title string) {
	if v.TitleChanged != nil {
		v.TitleChanged(title)
	}
}

func WithDefaultFgChangeHandler(handler func(Color)) Option {
	return func(v *VTerm) { v.DefaultFgChanged = handler }
}

func WithDefaultBgChangeHandler(handler func(Color)) Option {
	return func(v *VTerm) { v.DefaultBgChanged = handler }
}

func WithQueryDefaultFgHandler(handler func()) Option {
	return func(v *VTerm) { v.QueryDefaultFg = handler }
}

func WithQueryDefaultBgHandler(handler func()) Option {
	return func(v *VTerm) { v.QueryDefaultBg = handler }
}

func WithScreenRestoredHandler(handler func()) Option {
	return func(v *VTerm) { v.ScreenRestored = handler }
}

func WithBracketedPasteModeChangeHandler(handler func(bool)) Option {
	return func(v *VTerm) { v.OnBracketedPasteModeChange = handler }
}

func WithHistoryManager(hm *HistoryManager) Option {
	return func(v *VTerm) { v.historyManager = hm }
}

// reflowHistoryBuffer rewraps all lines in the history buffer to fit the new width.
// It reconstructs logical lines by joining wrapped segments and re-wraps them.
func (v *VTerm) reflowHistoryBuffer(oldWidth, newWidth int) {
	if v.historyLen == 0 {
		return
	}

	// Extract all logical lines from history buffer
	var logicalLines [][]Cell
	currentLogical := []Cell{}

	for i := 0; i < v.getHistoryLen(); i++ {
		line := v.getHistoryLine(i)

		// Check if this line wraps to the next by looking at the LAST cell (not last non-space)
		// The Wrapped flag is set on the cell at the edge (width-1) when we wrap
		wrapped := false
		if len(line) > 0 {
			// Check the very last cell in the line
			wrapped = line[len(line)-1].Wrapped
		}

		// Find last non-space cell for trimming non-wrapped lines
		lastNonSpace := -1
		for j := len(line) - 1; j >= 0; j-- {
			if line[j].Rune != 0 && line[j].Rune != ' ' {
				lastNonSpace = j
				break
			}
		}

		// If line is wrapped, include all cells (content continues on next line)
		// If not wrapped, only include cells up to last non-space (trim padding)
		if wrapped {
			currentLogical = append(currentLogical, line...)
		} else {
			if lastNonSpace >= 0 {
				currentLogical = append(currentLogical, line[:lastNonSpace+1]...)
			}
			// End of logical line - save it and start a new one
			logicalLines = append(logicalLines, currentLogical)
			currentLogical = []Cell{}
		}
	}

	// If there's a partial logical line at the end, save it
	if len(currentLogical) > 0 {
		logicalLines = append(logicalLines, currentLogical)
	}

	// Re-wrap each logical line with the new width
	newHistory := make([][]Cell, 0, v.maxHistorySize)
	for _, logical := range logicalLines {
		// Split this logical line into physical lines of newWidth
		for len(logical) > 0 {
			lineWidth := newWidth
			if len(logical) < newWidth {
				lineWidth = len(logical)
			}

			physicalLine := make([]Cell, lineWidth)
			copy(physicalLine, logical[:lineWidth])

			// Mark as wrapped if there's more content
			if len(logical) > newWidth {
				physicalLine[lineWidth-1].Wrapped = true
			}

			newHistory = append(newHistory, physicalLine)
			logical = logical[lineWidth:]
		}
	}

	// Replace history buffer with reflowed content
	v.historyLen = len(newHistory)
	v.historyHead = 0
	for i := 0; i < v.getHistoryLen() && i < v.maxHistorySize; i++ {
		v.historyBuffer[i] = newHistory[i]
	}
	// If we have more lines than fit in the buffer, keep only the most recent
	if v.getHistoryLen() > v.maxHistorySize {
		offset := v.getHistoryLen() - v.maxHistorySize
		for i := 0; i < v.maxHistorySize; i++ {
			v.historyBuffer[i] = newHistory[offset+i]
		}
		v.historyLen = v.maxHistorySize
	}
}

// Resize handles changes to the terminal's dimensions.
func (v *VTerm) Resize(width, height int) {
	if width == v.width && height == v.height {
		return
	}

	oldHeight := v.height
	oldWidth := v.width
	v.width = width
	v.height = height

	if v.inAltScreen {
		newAltBuffer := make([][]Cell, v.height)
		for i := range newAltBuffer {
			newAltBuffer[i] = make([]Cell, v.width)
			if i < oldHeight && i < len(v.altBuffer) {
				oldLine := v.altBuffer[i]
				copy(newAltBuffer[i], oldLine)
			}
		}
		v.altBuffer = newAltBuffer
		v.SetCursorPos(v.cursorY, v.cursorX) // Re-clamp cursor
	} else {
		// Reflow main screen buffer if enabled and width changed
		if v.reflowEnabled && oldWidth != width {
			v.reflowHistoryBuffer(oldWidth, width)
		}
		// In main screen mode, the PTY application (bash/shell) will reposition
		// the cursor itself after receiving SIGWINCH. Our job is just to ensure
		// the cursor stays within valid bounds for the new dimensions.
		//
		// IMPORTANT: Don't try to "preserve" cursor position by adjusting it,
		// because bash maintains its own cursor state and will get confused if
		// we move the cursor without bash knowing.
		v.SetCursorPos(v.cursorY, v.cursorX) // Just clamp to new dimensions
	}

	// Reset margins on resize (without moving cursor)
	// Note: We can't use SetMargins() because it moves cursor to home per VT spec
	v.marginTop = 0
	v.marginBottom = v.height - 1
	v.MarkAllDirty()
}

// --- Simple Getters ---

func (v *VTerm) AppCursorKeys() bool { return v.appCursorKeys }
func (v *VTerm) Cursor() (int, int)  { return v.cursorX, v.cursorY }
func (v *VTerm) CursorVisible() bool { return v.cursorVisible }
func (v *VTerm) DefaultFG() Color    { return v.defaultFG }
func (v *VTerm) DefaultBG() Color    { return v.defaultBG }
func (v *VTerm) OriginMode() bool    { return v.originMode }
func (v *VTerm) ScrollMargins() (int, int) {
	return v.marginTop, v.marginLeft
}

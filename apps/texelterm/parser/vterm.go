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
	historyBuffer                      [][]Cell
	maxHistorySize                     int
	historyHead, historyLen            int
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
	defaultFG, defaultBG               Color
	DefaultFgChanged, DefaultBgChanged func(Color)
	QueryDefaultFg, QueryDefaultBg     func()
	ScreenRestored                     func()
	dirtyLines                         map[int]bool
	allDirty                           bool
	prevCursorY                        int
	InSynchronizedUpdate               bool
	// Shell integration (OSC 133)
	PromptActive                       bool
	InputActive                        bool
	CommandActive                      bool
	InputStartLine, InputStartCol      int
	OnPromptStart                      func()
	OnInputStart                       func()
	OnCommandStart                     func()
	OnCommandEnd                       func(exitCode int)
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
		wrapEnabled:    true,
		reflowEnabled:  true,
		marginTop:      0,
		marginBottom:   height - 1,
		defaultFG:      DefaultFG,
		defaultBG:      DefaultBG,
		dirtyLines:     make(map[int]bool),
		allDirty:       true,
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
	for i := 0; i < v.height; i++ {
		historyIdx := topHistoryLine + i
		grid[i] = make([]Cell, v.width)
		var logicalLine []Cell
		if historyIdx >= 0 && historyIdx < v.historyLen {
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

// placeChar puts a rune at the current cursor position, handling wrapping and insert mode.
func (v *VTerm) placeChar(r rune) {
	if v.wrapNext {
		if v.inAltScreen {
			v.cursorX = 0
			v.LineFeed()
		}
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
			v.SetCursorPos(v.historyLen-1-v.getTopHistoryLine(), v.cursorX)
		}
		logicalY := v.cursorY + v.getTopHistoryLine()
		line := v.getHistoryLine(logicalY)
		for len(line) <= v.cursorX {
			line = append(line, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
		}
		if v.insertMode {
			line = append(line, Cell{}) // Make space for the new char
			copy(line[v.cursorX+1:], line[v.cursorX:])
		}

		line[v.cursorX] = Cell{Rune: r, FG: v.currentFG, BG: v.currentBG, Attr: v.currentAttr}
		v.setHistoryLine(logicalY, line)
		v.MarkDirty(v.cursorY)
	}

	if v.inAltScreen {
		if v.autoWrapMode && v.cursorX == v.width-1 {
			v.wrapNext = true
		} else if v.cursorX < v.width-1 {
			v.SetCursorPos(v.cursorY, v.cursorX+1)
		}
	} else {
		// Main screen wrapping logic
		if v.wrapEnabled && v.cursorX == v.width-1 {
			// Mark the current cell as wrapped (continues on next line)
			logicalY := v.cursorY + v.getTopHistoryLine()
			line := v.getHistoryLine(logicalY)
			if len(line) > v.cursorX {
				line[v.cursorX].Wrapped = true
				v.setHistoryLine(logicalY, line)
			}
			// Move to next line
			v.LineFeed()
			v.cursorX = 0
			v.MarkDirty(v.cursorY)
		} else if v.cursorX < v.width-1 {
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
	if v.inAltScreen {
		if x >= v.width {
			x = v.width - 1
		}
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
	v.MarkDirty(v.cursorY)
	if v.inAltScreen {
		if v.cursorY == v.marginBottom {
			v.scrollRegion(1, v.marginTop, v.marginBottom)
		} else if v.cursorY < v.height-1 {
			v.SetCursorPos(v.cursorY+1, v.cursorX)
		}
	} else {
		logicalY := v.cursorY + v.getTopHistoryLine()
		if logicalY+1 >= v.historyLen {
			v.appendHistoryLine(make([]Cell, 0, v.width))
		}
		if v.cursorY < v.height-1 {
			v.SetCursorPos(v.cursorY+1, v.cursorX)
		} else {
			v.viewOffset = 0 // Jump to the bottom
			v.MarkAllDirty()
		}
	}
}

// scrollRegion scrolls a portion of the alternate screen buffer up or down.
func (v *VTerm) scrollRegion(n int, top int, bottom int) {
	v.wrapNext = false
	if !v.inAltScreen {
		return
	}
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
	maxOffset := v.historyLen - v.height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if v.viewOffset > maxOffset {
		v.viewOffset = maxOffset
	}
	v.MarkAllDirty()
}

// --- CSI Handlers ---

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
		case 4: // SET Insert Mode
			v.insertMode = true
		case 7:
			v.autoWrapMode = true
		case 12: // SET Blinking Cursor
			// We can just log this for now, as it's a visual preference.
			log.Println("Parser: Ignoring set blinking cursor (12h)")
		case 25:
			v.SetCursorVisible(true)
		case 1002, 1004, 1006, 2004:
			// Ignore mouse and focus reporting for now
		case 1049: // Switch to Alt Workspace
			if v.inAltScreen {
				return
			}
			v.inAltScreen = true
			v.savedMainCursorX, v.savedMainCursorY = v.cursorX, v.cursorY //+v.getTopHistoryLine()
			v.altBuffer = make([][]Cell, v.height)
			for i := range v.altBuffer {
				v.altBuffer[i] = make([]Cell, v.width)
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
		case 4: // RESET Insert Mode
			v.insertMode = false
		case 7:
			v.autoWrapMode = false
		case 12: // RESET Steady Cursor (Stop Blinking)
			// We can just log this for now.
			log.Println("Parser: Ignoring reset steady cursor (12l)")
		case 25:
			v.SetCursorVisible(false)
		case 1002, 1004, 1006, 2004, 2031, 2048:
			// Ignore mouse and focus reporting for now
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

// ClearScreen clears the active buffer (main or alt).
func (v *VTerm) ClearScreen() {
	v.MarkAllDirty()
	if v.inAltScreen {
		for y := range v.altBuffer {
			for x := range v.altBuffer[y] {
				v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}
		}
		v.SetCursorPos(0, 0)
	} else {
		v.historyBuffer = make([][]Cell, v.maxHistorySize)
		v.historyHead = 0
		v.historyLen = 1
		v.historyBuffer[0] = make([]Cell, 0, v.width)
		v.viewOffset = 0
		v.SetCursorPos(0, 0)
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
func (v *VTerm) RestoreCursor() {
	v.wrapNext = false
	if v.inAltScreen {
		v.SetCursorPos(v.savedAltCursorY, v.savedAltCursorX)
	} else {
		v.SetCursorPos(v.savedMainCursorY, v.savedMainCursorX)
	}
}

// --- History and Viewport Management ---

// getHistoryLine retrieves a specific line from the circular history buffer.
func (v *VTerm) getHistoryLine(index int) []Cell {
	if index < 0 || index >= v.historyLen {
		return nil
	}
	physicalIndex := (v.historyHead + index) % v.maxHistorySize
	return v.historyBuffer[physicalIndex]
}

// setHistoryLine updates a specific line in the circular history buffer.
func (v *VTerm) setHistoryLine(index int, line []Cell) {
	if index < 0 || index >= v.historyLen {
		return
	}
	physicalIndex := (v.historyHead + index) % v.maxHistorySize
	v.historyBuffer[physicalIndex] = line
}

// appendHistoryLine adds a new line to the end of the history buffer.
func (v *VTerm) appendHistoryLine(line []Cell) {
	if v.historyLen < v.maxHistorySize {
		physicalIndex := (v.historyHead + v.historyLen) % v.maxHistorySize
		v.historyBuffer[physicalIndex] = line
		v.historyLen++
	} else {
		// Buffer is full, wrap around (overwrite the oldest line)
		v.historyHead = (v.historyHead + 1) % v.maxHistorySize
		physicalIndex := (v.historyHead + v.historyLen - 1) % v.maxHistorySize
		v.historyBuffer[physicalIndex] = line
	}
}

// getTopHistoryLine calculates the index of the first visible line in the history buffer.
func (v *VTerm) getTopHistoryLine() int {
	if v.inAltScreen {
		return 0
	}
	top := v.historyLen - v.height - v.viewOffset
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
	v.SetCursorPos(v.cursorY, 0)
}

func (v *VTerm) Backspace() {
	v.wrapNext = false
	if v.cursorX > 0 {
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

// Reset brings the terminal to its initial state.
func (v *VTerm) Reset() {
	v.MarkAllDirty()
	v.savedMainCursorX, v.savedMainCursorY = 0, 0
	v.savedAltCursorX, v.savedAltCursorY = 0, 0
	v.ClearScreen()
	v.ResetAttributes()
	v.SetMargins(0, 0)
	v.cursorVisible = true
	v.wrapNext = false
	v.autoWrapMode = true
	v.insertMode = false
	v.appCursorKeys = false
	v.tabStops = make(map[int]bool)
	for i := 0; i < v.width; i++ {
		if i%8 == 0 {
			v.tabStops[i] = true
		}
	}
}

// ReverseIndex moves the cursor up one line, scrolling down if at the top margin.
func (v *VTerm) ReverseIndex() {
	v.wrapNext = false
	if v.cursorY == v.marginTop {
		v.scrollRegion(-1, v.marginTop, v.marginBottom)
	} else if v.cursorY > 0 {
		v.SetCursorPos(v.cursorY-1, v.cursorX)
	}
}

// --- Core CSI Dispatch ---

// ProcessCSI interprets a parsed CSI sequence and calls the appropriate handler.
func (v *VTerm) ProcessCSI(command rune, params []int, intermediate rune) {
	param := func(i int, defaultVal int) int {
		if i < len(params) && params[i] != 0 {
			return params[i]
		}
		return defaultVal
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

	if param(0, -1) > 0 && (command == 'h' || command == 'l') {
		v.processPrivateCSI(command, params)
		return
	}

	switch command {
	case 'A', 'B', 'C', 'D', 'G', 'H', 'f', 'd':
		v.handleCursorMovement(command, params)
	case 'J', 'K', 'P', 'X':
		v.handleErase(command, params)
	case '@':
		v.InsertCharacters(param(0, 1))
	case 'L':
		v.InsertLines(param(0, 1))
	case 'M':
		v.DeleteLines(param(0, 1))
	case 'S': // SU - Scroll Up
		v.scrollRegion(param(0, 1), v.marginTop, v.marginBottom)
	case 'T': // SD - Scroll Down
		v.scrollRegion(-param(0, 1), v.marginTop, v.marginBottom)
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
		v.SaveCursor()
	case 'u':
		v.RestoreCursor()
	case 'c': // DA - Device Attributes
		response := "\x1b[?6c" // "I am a VT102"
		if v.WriteToPty != nil {
			v.WriteToPty([]byte(response))
		}
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
	case 'G':
		col := param(0, 1) - 1
		v.SetCursorPos(v.cursorY, col)
	case 'H', 'f':
		row := param(0, 1) - 1
		col := param(1, 1) - 1
		v.SetCursorPos(row, col)
	case 'd':
		v.SetCursorPos(param(0, 1)-1, v.cursorX)
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
	}
}

func (v *VTerm) ClearScreenMode(mode int) {
	v.MarkAllDirty()
	switch mode {
	case 0: // Erase from cursor to end of screen
		v.ClearLine(0)
		if v.inAltScreen {
			for y := v.cursorY + 1; y < v.height; y++ {
				for x := 0; x < v.width; x++ {
					v.altBuffer[y][x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
				}
			}
		} else {
			logicalY := v.cursorY + v.getTopHistoryLine()
			line := v.getHistoryLine(logicalY)
			if v.cursorX < len(line) {
				v.setHistoryLine(logicalY, line[:v.cursorX])
			}
			// Effectively truncates history from the line after the cursor
			v.historyLen = logicalY + 1
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
			for i := v.getTopHistoryLine(); i < logicalY; i++ {
				v.setHistoryLine(i, make([]Cell, 0, v.width))
			}
		}
	case 2, 3: // Erase entire screen (+ scrollback for 3)
		v.ClearScreen()
	}
}

func (v *VTerm) ClearLine(mode int) {
	v.MarkDirty(v.cursorY)
	var line []Cell
	if v.inAltScreen {
		line = v.altBuffer[v.cursorY]
	} else {
		line = v.getHistoryLine(v.cursorY + v.getTopHistoryLine())
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
		line = append(line, Cell{Rune: ' '})
	}

	for x := start; x < end; x++ {
		if x < len(line) {
			line[x] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}
	}
	if !v.inAltScreen {
		v.setHistoryLine(v.cursorY+v.getTopHistoryLine(), line)
	}
}

func (v *VTerm) EraseCharacters(n int) {
	v.MarkDirty(v.cursorY)
	var line []Cell
	logicalY := v.cursorY + v.getTopHistoryLine()
	if v.inAltScreen {
		line = v.altBuffer[v.cursorY]
	} else {
		line = v.getHistoryLine(logicalY)
	}

	for i := 0; i < n; i++ {
		if v.cursorX+i < len(line) {
			line[v.cursorX+i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
		}
	}
	if !v.inAltScreen {
		v.setHistoryLine(logicalY, line)
	}
}

// In texelterm/parser/vterm.go
func (v *VTerm) InsertCharacters(n int) {
	v.MarkDirty(v.cursorY)
	if v.cursorX >= v.width {
		return
	}

	if v.inAltScreen {
		line := v.altBuffer[v.cursorY]
		// Create a copy of the segment that needs to be shifted
		if v.cursorX < len(line) {
			segment := make([]Cell, len(line[v.cursorX:]))
			copy(segment, line[v.cursorX:])

			// Insert blanks
			for i := 0; i < n && v.cursorX+i < v.width; i++ {
				line[v.cursorX+i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}

			// Copy the original segment back, shifted
			if v.cursorX+n < v.width {
				copy(line[v.cursorX+n:], segment)
			}
		}
	} else {
		// Existing logic for the history buffer (this is correct for the main screen)
		logicalY := v.cursorY + v.getTopHistoryLine()
		line := v.getHistoryLine(logicalY)
		blanks := make([]Cell, n)
		for i := range blanks {
			blanks[i] = Cell{Rune: ' '}
		}
		line = append(line[:v.cursorX], append(blanks, line[v.cursorX:]...)...)
		v.setHistoryLine(logicalY, line)
	}
}

// In texelterm/parser/vterm.go
func (v *VTerm) DeleteCharacters(n int) {
	v.MarkDirty(v.cursorY)
	if v.cursorX >= v.width {
		return
	}

	if v.inAltScreen {
		line := v.altBuffer[v.cursorY]
		if v.cursorX < len(line) {
			// Determine how many characters to copy from the right
			copySrcStart := v.cursorX + n
			if copySrcStart < v.width {
				// Shift characters from the right to the left
				copy(line[v.cursorX:], line[copySrcStart:])
			}

			// Clear the now-empty cells at the end of the line
			clearStart := v.width - n
			if v.cursorX > clearStart {
				clearStart = v.cursorX
			}
			for i := clearStart; i < v.width; i++ {
				line[i] = Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
			}
		}
	} else {
		// Existing logic for the history buffer (this is correct for the main screen)
		logicalY := v.cursorY + v.getTopHistoryLine()
		line := v.getHistoryLine(logicalY)
		if v.cursorX >= len(line) {
			return
		}
		if v.cursorX+n > len(line) {
			line = line[:v.cursorX]
		} else {
			line = append(line[:v.cursorX], line[v.cursorX+n:]...)
		}
		v.setHistoryLine(logicalY, line)
	}
}

func (v *VTerm) InsertLines(n int) {
	v.wrapNext = false
	if v.cursorY < v.marginTop || v.cursorY > v.marginBottom {
		return
	}
	if v.inAltScreen {
		// In alt screen, shift lines down within the buffer
		for i := 0; i < n; i++ {
			copy(v.altBuffer[v.cursorY+1:v.marginBottom+1], v.altBuffer[v.cursorY:v.marginBottom])
			v.altBuffer[v.cursorY] = make([]Cell, v.width)
		}
	} else {
		// In main screen, insert lines into history
		logicalY := v.cursorY + v.getTopHistoryLine()
		for i := 0; i < n; i++ {
			v.insertHistoryLine(logicalY, make([]Cell, 0, v.width))
		}
	}
	v.MarkAllDirty()
}

func (v *VTerm) insertHistoryLine(index int, line []Cell) {
	if index < 0 || index > v.historyLen {
		return
	}
	if v.historyLen < v.maxHistorySize {
		physicalInsertIndex := (v.historyHead + index) % v.maxHistorySize
		// Shift existing lines to make room
		for i := v.historyLen; i > index; i-- {
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
	if v.cursorY < v.marginTop || v.cursorY > v.marginBottom {
		return
	}
	if v.inAltScreen {
		copy(v.altBuffer[v.cursorY:v.marginBottom], v.altBuffer[v.cursorY+n:v.marginBottom+1])
		for y := v.marginBottom - n + 1; y <= v.marginBottom; y++ {
			v.altBuffer[y] = make([]Cell, v.width) // Clear new lines at bottom
		}
	} else {
		logicalY := v.cursorY + v.getTopHistoryLine()
		for i := 0; i < n; i++ {
			if logicalY < v.historyLen {
				v.deleteHistoryLine(logicalY)
			}
		}
	}
	v.MarkAllDirty()
}

func (v *VTerm) deleteHistoryLine(index int) {
	if index < 0 || index >= v.historyLen {
		return
	}
	// Shift lines up to fill the gap
	for i := index; i < v.historyLen-1; i++ {
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
	// Per spec, move cursor to home on change
	//v.SetCursorPos(v.marginTop, 0)
}

func (v *VTerm) MoveCursorForward(n int) {
	v.SetCursorPos(v.cursorY, v.cursorX+n)
}

func (v *VTerm) MoveCursorBackward(n int) {
	v.SetCursorPos(v.cursorY, v.cursorX-n)
}

func (v *VTerm) MoveCursorUp(n int) {
	v.wrapNext = false
	newY := v.cursorY - n
	if newY < v.marginTop {
		newY = v.marginTop
	}
	v.SetCursorPos(newY, v.cursorX)
}

func (v *VTerm) MoveCursorDown(n int) {
	v.wrapNext = false
	newY := v.cursorY + n
	if newY > v.marginBottom {
		newY = v.marginBottom
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

// reflowHistoryBuffer rewraps all lines in the history buffer to fit the new width.
// It reconstructs logical lines by joining wrapped segments and re-wraps them.
func (v *VTerm) reflowHistoryBuffer(oldWidth, newWidth int) {
	if v.historyLen == 0 {
		return
	}

	// Extract all logical lines from history buffer
	var logicalLines [][]Cell
	currentLogical := []Cell{}

	for i := 0; i < v.historyLen; i++ {
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
	for i := 0; i < v.historyLen && i < v.maxHistorySize; i++ {
		v.historyBuffer[i] = newHistory[i]
	}
	// If we have more lines than fit in the buffer, keep only the most recent
	if v.historyLen > v.maxHistorySize {
		offset := v.historyLen - v.maxHistorySize
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
	var linesFromEnd int
	if !v.inAltScreen {
		// Save cursor position as distance from end of history
		// This way after reflow changes line count, we can restore relative position
		logicalY := v.cursorY + v.getTopHistoryLine()
		linesFromEnd = v.historyLen - logicalY - 1
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
		// Restore cursor to same distance from end of history
		newLogicalY := v.historyLen - linesFromEnd - 1
		if newLogicalY < 0 {
			newLogicalY = 0
		}
		physicalY := newLogicalY - v.getTopHistoryLine()
		v.SetCursorPos(physicalY, v.cursorX) // Re-clamp cursor
	}

	v.SetMargins(0, 0) // Reset margins on resize
	v.MarkAllDirty()
}

// --- Simple Getters ---

func (v *VTerm) AppCursorKeys() bool { return v.appCursorKeys }
func (v *VTerm) Cursor() (int, int)  { return v.cursorX, v.cursorY }
func (v *VTerm) CursorVisible() bool { return v.cursorVisible }
func (v *VTerm) DefaultFG() Color    { return v.defaultFG }
func (v *VTerm) DefaultBG() Color    { return v.defaultBG }

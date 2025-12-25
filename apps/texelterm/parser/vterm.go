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
	"os"
)

const (
	defaultHistorySize = 2000
	// cursorMarker is a special character used to track cursor position during reflow
	// Using Unicode Private Use Area character that won't appear in normal text
	cursorMarker = rune(0xF8FF)
)

// VTerm represents the state of a virtual terminal, managing both the main screen
// with a scrollback buffer and an alternate screen for fullscreen applications.
type VTerm struct {
	width, height                      int
	cursorX, cursorY                   int
	savedMainCursorX, savedMainCursorY int
	savedAltCursorX, savedAltCursorY   int
	// Legacy circular buffer (deprecated in favor of historyManager)
	historyBuffer           [][]Cell
	maxHistorySize          int
	historyHead, historyLen int
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
	PromptActive                  bool
	InputActive                   bool
	CommandActive                 bool
	InputStartLine, InputStartCol int
	OnPromptStart                 func()
	OnInputStart                  func()
	OnCommandStart                func(cmd string)
	OnCommandEnd                  func(exitCode int)
	OnEnvironmentUpdate           func(base64Env string)
	// Bracketed paste mode (DECSET 2004)
	bracketedPasteMode         bool
	OnBracketedPasteModeChange func(bool)
	// Display buffer for scrollback reflow (new architecture)
	displayBuf *displayBufferState
}

// NewVTerm creates and initializes a new virtual terminal.
func NewVTerm(width, height int, opts ...Option) *VTerm {
	v := &VTerm{
		width:               width,
		height:              height,
		maxHistorySize:      defaultHistorySize,
		historyBuffer:       make([][]Cell, defaultHistorySize),
		viewOffset:          0,
		currentFG:           DefaultFG,
		currentBG:           DefaultBG,
		tabStops:            make(map[int]bool),
		cursorVisible:       true,
		autoWrapMode:        true,
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
	// Only clear screen if we don't have loaded history content
	// When restoring from snapshot, history is already loaded and we want to show it
	if v.historyManager == nil || v.historyManager.Length() == 0 {
		v.ClearScreen()
	}
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

	// Use new display buffer path if enabled
	if v.IsDisplayBufferEnabled() {
		if v.displayBuf != nil && v.displayBuf.display != nil && v.displayBuf.display.debugLog != nil {
			v.displayBuf.display.debugLog("Grid: using displayBufferGrid()")
		}
		return v.displayBufferGrid()
	}
	// Debug: log when using old path
	if v.displayBuf != nil && v.displayBuf.display != nil && v.displayBuf.display.debugLog != nil {
		v.displayBuf.display.debugLog("Grid: using historyManager path (display buffer NOT enabled)")
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
		// Use lineFeedForWrap to not commit the logical line (this is auto-wrap, not explicit LF)
		v.lineFeedForWrap()
		v.wrapNext = false
	}

	if v.inAltScreen {
		if v.cursorY >= 0 && v.cursorY < v.height && v.cursorX >= 0 && v.cursorX < v.width {
			v.altBuffer[v.cursorY][v.cursorX] = Cell{Rune: r, FG: v.currentFG, BG: v.currentBG, Attr: v.currentAttr}
			v.MarkDirty(v.cursorY)
		}
	} else {
		// Also write to display buffer if enabled
		if v.IsDisplayBufferEnabled() {
			v.displayBufferPlaceChar(r)
		} else {
			// Debug: track when display buffer is skipped
			if v.displayBuf != nil && v.displayBuf.display != nil && v.displayBuf.display.debugLog != nil {
				v.displayBuf.display.debugLog("placeChar SKIPPED: displayBuf=%v, enabled=%v, char='%c'",
					v.displayBuf != nil, v.displayBuf != nil && v.displayBuf.enabled, r)
			}
		}

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
// Cursor position operations: See vterm_cursor.go

// LineFeed moves the cursor down one line, scrolling if necessary.
// Scrolling operations: See vterm_scroll.go

// --- CSI Handlers ---

// processANSIMode handles ANSI mode setting/resetting (SM/RM - without '?' prefix).
// Mode handling functions: See vterm_modes.go

// ClearScreen fully resets the terminal buffer (used for RIS/Reset).
// This is the full reset version - it resets history and moves cursor to home.
// Screen clearing operations: See vterm_clear.go

// SaveCursor saves the current cursor position for either the main or alt screen.
// SaveCursor and RestoreCursor: See vterm_cursor.go

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
// See vterm_dirty.go: MarkDirty, MarkAllDirty, GetDirtyLines, ClearDirty

// --- Basic Terminal Operations ---

// Basic text navigation operations: See vterm_navigation.go

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
// Index operations (IND, NEL, RI, DECBI, DECFI): See vterm_navigation.go

// scrollHorizontal: See vterm_scroll.go

// InsertColumns (DECIC) inserts blank columns at cursor position.
// CSI Pn ' } - VT420 feature for horizontal scrolling.
// Column edit operations (DECIC, DECDC): See vterm_edit_col.go

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

// Erase operations (ED, EL, ECH): See vterm_erase.go

// Character edit operations (ICH, DCH, REP): See vterm_edit_char.go

// Line edit operations (IL, DL): See vterm_edit_line.go

// SGR (Select Graphic Rendition) functions: See vterm_sgr.go

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

	// Sync display buffer's logical X for horizontal cursor movement
	if !v.inAltScreen && v.IsDisplayBufferEnabled() {
		v.displayBufferSetCursorFromPhysical()
	}
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

	// Sync display buffer's logical X for horizontal cursor movement
	if !v.inAltScreen && v.IsDisplayBufferEnabled() {
		v.displayBufferSetCursorFromPhysical()
	}
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

// Text attribute functions: See vterm_sgr.go

// SetCursorVisible: See vterm_cursor.go

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

// WithDisplayBuffer enables the new display buffer architecture for scrollback reflow.
// When enabled, the terminal uses logical lines (width-independent) for history storage
// and reflows content correctly on resize.
func WithDisplayBuffer(enabled bool) Option {
	return func(v *VTerm) {
		if enabled {
			v.EnableDisplayBuffer()
		}
	}
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

func WithCommandStartHandler(handler func(string)) Option {
	return func(v *VTerm) { v.OnCommandStart = handler }
}

func WithCommandEndHandler(handler func(int)) Option {
	return func(v *VTerm) { v.OnCommandEnd = handler }
}

func WithEnvironmentUpdateHandler(handler func(string)) Option {
	return func(v *VTerm) { v.OnEnvironmentUpdate = handler }
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
	histLen := v.getHistoryLen()

	if histLen == 0 {
		return
	}

	// Extract all logical lines from history buffer
	var logicalLines [][]Cell
	currentLogical := []Cell{}

	debugReflow := false // Set to true to enable debug output
	if debugReflow {
		fmt.Fprintf(os.Stderr, "DEBUG REFLOW: oldWidth=%d, newWidth=%d, histLen=%d\n", oldWidth, newWidth, histLen)
	}

	logicalLineCount := 0
	physicalLineDebugCount := 0
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

		if debugReflow && physicalLineDebugCount < 30 {
			lineStr := ""
			for _, cell := range line {
				if cell.Rune == 0 {
					lineStr += "∅"
				} else {
					lineStr += string(cell.Rune)
				}
			}
			if len(lineStr) > 50 {
				lineStr = lineStr[:50] + "..."
			}
			fmt.Fprintf(os.Stderr, "DEBUG PHYS[%d] len=%d wrapped=%v lastNonSpace=%d: %q\n", i, len(line), wrapped, lastNonSpace, lineStr)
			physicalLineDebugCount++
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
			logicalLineCount++
			currentLogical = []Cell{}
		}
	}

	// If there's a partial logical line at the end, save it
	if len(currentLogical) > 0 {
		logicalLines = append(logicalLines, currentLogical)
	}

	if debugReflow {
		fmt.Fprintf(os.Stderr, "DEBUG REFLOW: Created %d logical lines from %d physical lines\n", len(logicalLines), histLen)
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
	if v.historyManager != nil {
		// Using HistoryManager - replace buffer with reflowed content
		v.historyManager.ReplaceBuffer(newHistory)
	} else {
		// Using legacy buffer
		v.historyLen = len(newHistory)
		v.historyHead = 0
		for i := 0; i < len(newHistory) && i < v.maxHistorySize; i++ {
			v.historyBuffer[i] = newHistory[i]
		}
		// If we have more lines than fit in the buffer, keep only the most recent
		if len(newHistory) > v.maxHistorySize {
			offset := len(newHistory) - v.maxHistorySize
			for i := 0; i < v.maxHistorySize; i++ {
				v.historyBuffer[i] = newHistory[offset+i]
			}
			v.historyLen = v.maxHistorySize
		}
	}
}

// placeCursorMarker places a special marker character at the current cursor position.
// Returns true if marker was placed successfully.
func (v *VTerm) placeCursorMarker() bool {
	topHistory := v.getTopHistoryLine()
	cursorLine := topHistory + v.cursorY

	if cursorLine >= v.getHistoryLen() {
		return false
	}

	line := v.getHistoryLine(cursorLine)
	if line == nil {
		return false
	}

	// Extend line if cursor is beyond current line length
	for len(line) <= v.cursorX {
		line = append(line, Cell{Rune: ' ', FG: v.defaultFG, BG: v.defaultBG})
	}

	// Place marker at cursor position
	line[v.cursorX].Rune = cursorMarker
	v.setHistoryLine(cursorLine, line)
	return true
}

// findAndRemoveCursorMarker searches for the cursor marker and returns its position.
// Removes the marker and returns (line, column, found).
func (v *VTerm) findAndRemoveCursorMarker() (int, int, bool) {
	for i := 0; i < v.getHistoryLen(); i++ {
		line := v.getHistoryLine(i)
		for j := 0; j < len(line); j++ {
			if line[j].Rune == cursorMarker {
				// Remove the marker by replacing it with a space
				line[j].Rune = ' '
				v.setHistoryLine(i, line)
				return i, j, true
			}
		}
	}
	return 0, 0, false
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
	} else if v.IsDisplayBufferEnabled() {
		// Use display buffer reflow - this is the new clean path
		v.displayBufferResize(width, height)
		v.SetCursorPos(v.cursorY, v.cursorX) // Re-clamp cursor
	} else {
		// Handle height-only changes (no width change, no reflow needed)
		if oldHeight != height && oldWidth == width {
			// Height changed but not width - adjust cursor position
			// When height increases, topHistory decreases (we show more lines above)
			// so cursor needs to move down on screen to stay at same absolute line
			oldTopHistory := v.getHistoryLen() - oldHeight + v.viewOffset
			newTopHistory := v.getHistoryLen() - height + v.viewOffset
			deltaTop := newTopHistory - oldTopHistory // Negative when height increases

			// Adjust cursor Y to compensate for topHistory shift
			v.cursorY -= deltaTop

			// Clamp to screen bounds
			if v.cursorY < 0 {
				v.cursorY = 0
			} else if v.cursorY >= v.height {
				v.cursorY = v.height - 1
			}
		} else if v.reflowEnabled && oldWidth != width {
			// Skip reflow if we have loaded history with many lines
			// Reflowing loaded history destroys it because loaded lines may not have
			// consistent wrapping information across width changes
			if v.historyManager != nil && v.historyManager.Length() > v.height {
				// Position cursor at bottom of screen where new shell output will appear
				// viewOffset=0 means we're viewing the bottom of history
				// The cursor should be at the last visible line
				v.viewOffset = 0
				v.cursorY = v.height - 1
				v.cursorX = 0
				// DON'T return early - we need to continue to the margin reset code at the end
			} else {

				// Place marker at cursor position before reflow
				markerPlaced := v.placeCursorMarker()

				// Reflow the buffer (marker will move with content)
				v.reflowHistoryBuffer(oldWidth, width)

				// Find marker and place cursor there
				if markerPlaced {
					if markerLine, markerCol, found := v.findAndRemoveCursorMarker(); found {
						// Clamp X to screen width
						if markerCol >= v.width {
							markerCol = v.width - 1
						}

						// Calculate where marker currently is on screen (with current viewOffset)
						topHistory := v.getTopHistoryLine()
						screenY := markerLine - topHistory

						// Only adjust viewOffset if marker is off-screen
						if screenY < 0 {
							// Marker is above visible area - scroll up to show it at top
							v.viewOffset += -screenY
							screenY = 0
						} else if screenY >= v.height {
							// Marker is below visible area - scroll down to show it at bottom
							adjustment := screenY - v.height + 1
							v.viewOffset -= adjustment
							screenY = v.height - 1
						}

						v.cursorY = screenY
						v.cursorX = markerCol
					} else {
						// Fallback: clamp cursor if marker not found
						v.SetCursorPos(v.cursorY, v.cursorX)
					}
				} else {
					// Fallback: clamp cursor if marker couldn't be placed
					v.SetCursorPos(v.cursorY, v.cursorX)
				}
			}
		} else {
			// No reflow needed, just clamp cursor
			v.SetCursorPos(v.cursorY, v.cursorX)
		}
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

// InAltScreen returns true if the terminal is currently showing the alternate screen buffer.
func (v *VTerm) InAltScreen() bool { return v.inAltScreen }

// GetAltBufferLine returns a copy of the specified line from the alternate screen buffer.
// Returns nil if index is out of bounds or not in alt screen mode.
func (v *VTerm) GetAltBufferLine(y int) []Cell {
	if !v.inAltScreen || y < 0 || y >= len(v.altBuffer) {
		return nil
	}
	line := v.altBuffer[y]
	out := make([]Cell, len(line))
	copy(out, line)
	return out
}

func (v *VTerm) ScrollMargins() (int, int) {
	return v.marginTop, v.marginLeft
}

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
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
)

// VTerm represents the state of a virtual terminal, managing both the main screen
// with a scrollback buffer and an alternate screen for fullscreen applications.
type VTerm struct {
	mu                                 sync.RWMutex // protects dimensional state during resize
	width, height                      int
	cursorX, cursorY                   int
	savedMainCursorX, savedMainCursorY int
	savedAltCursorX, savedAltCursorY   int
	// Alternate screen buffer (for fullscreen apps like vim, less)
	inAltScreen bool
	altBuffer   [][]Cell
	// Memory buffer for scrollback (Phase 1-3 architecture)
	memBufState *memoryBufferState
	// Terminal state
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
	prevCursorX, prevCursorY           int
	prevWrapNext                       bool // Was wrapNext true before the last SetCursorPos?
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
	// Clipboard operations (OSC 52)
	OnClipboardSet func(data []byte) // Called when app sets clipboard via OSC 52
	OnClipboardGet func() []byte     // Called when app queries clipboard via OSC 52
	// Alt screen change notification (for transformer pipeline bypass)
	OnAltScreenChange func(inAltScreen bool)
	// Bracketed paste mode (DECSET 2004)
	bracketedPasteMode         bool
	OnBracketedPasteModeChange func(bool)
	// Search highlighting configuration
	searchHighlight         string  // term to highlight
	searchHighlightLine     int64   // current result's line index (-1 = none)
	searchSelectionColor    Color   // for selected match: used with Reverse attr
	searchAccentColor       Color   // for other matches: just FG color change
	searchLineTintColor     Color   // for full-line tint on selected result
	searchLineTintIntensity float32 // blend intensity (0.0-1.0, default 0.12)
	searchDefaultBG         Color   // terminal's default background for blending
	// OnLineCommit is called when a line is committed (line feed during normal
	// shell operation). Used by output transformers to colorize lines before
	// they enter scrollback. Called after cache invalidation, before persistence.
	OnLineCommit func(lineIdx int64, line *LogicalLine, isCommand bool) bool
	// commitInsertOffset tracks lines inserted by OnLineCommit callbacks via
	// RequestLineInsert. After the callback, currentGlobal is adjusted.
	commitInsertOffset int64
	// Deprecated: Use SetOnLineIndexed instead, which is called AFTER persistence.
	// This callback was called when a line was committed, but BEFORE it was persisted,
	// which could cause search index entries for content that doesn't exist on disk.
	// Kept for backward compatibility but no longer used internally.
	OnLineIndex func(lineIdx int64, timestamp time.Time, cells []Cell, isCommand bool)
}

// NewVTerm creates and initializes a new virtual terminal.
func NewVTerm(width, height int, opts ...Option) *VTerm {
	v := &VTerm{
		width:               width,
		height:              height,
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

	// Apply options first (may configure memory buffer with disk path)
	for _, opt := range opts {
		opt(v)
	}

	// Always initialize memory buffer if not already configured by options
	if v.memBufState == nil {
		v.initMemoryBuffer()
	}
	v.EnableMemoryBuffer()

	// Set up tab stops
	for i := 0; i < width; i++ {
		if i%8 == 0 {
			v.tabStops[i] = true
		}
	}

	// Clear screen to initialize
	v.ClearScreen()

	return v
}

// logDebug is a debug logging stub.
// Set TEXELTERM_DEBUG=1 environment variable for actual debug output.
// For now, this is a no-op to avoid performance overhead.
func (v *VTerm) logDebug(format string, args ...interface{}) {
	// Debug logging disabled for performance.
	// To enable: check os.Getenv("TEXELTERM_DEBUG") and write to a log file.
	_ = format
	_ = args
}

// --- Buffer & Grid Logic ---

// Grid returns the currently visible 2D buffer of cells.
// Returns the alternate screen buffer directly if in alt screen mode,
// otherwise returns the MemoryBuffer viewport.
func (v *VTerm) Grid() [][]Cell {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.inAltScreen {
		return v.altBuffer
	}
	return v.memoryBufferGrid()
}

// IsBracketedPasteModeEnabled returns whether bracketed paste mode is enabled.
func (v *VTerm) IsBracketedPasteModeEnabled() bool {
	return v.bracketedPasteMode
}

// writeCharWithWrapping puts a rune at the current cursor position, handling wrapping and insert mode.
func (v *VTerm) writeCharWithWrapping(r rune) {
	// Track last graphic character for REP command
	v.lastGraphicChar = r

	// Get character width (1 for normal chars, 2 for wide chars like emojis)
	charWidth := runewidth.RuneWidth(r)
	if charWidth == 0 {
		// Zero-width characters (combining marks, etc.) - attach to previous cell
		// For now, just skip them to avoid issues
		return
	}

	// Determine the effective right edge for wrapping
	rightEdge := v.width - 1
	if v.leftRightMarginMode && v.cursorX >= v.marginLeft && v.cursorX <= v.marginRight {
		rightEdge = v.marginRight
	}

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

	// For wide characters, check if we need to wrap first (no room for 2-cell char)
	if charWidth == 2 && v.cursorX == rightEdge {
		// Wide char at right edge - need to wrap first
		if v.inAltScreen {
			if v.autoWrapMode {
				if v.leftRightMarginMode {
					v.cursorX = v.marginLeft
				} else {
					v.cursorX = 0
				}
				v.lineFeedForWrap()
			}
		} else {
			if v.wrapEnabled {
				if v.leftRightMarginMode {
					v.cursorX = v.marginLeft
				} else {
					v.cursorX = 0
				}
				v.lineFeedForWrap()
			}
		}
	}

	isWide := charWidth == 2

	if v.inAltScreen {
		// Use consolidated alt buffer write operation
		v.altBufferWriteCell(r, isWide)
	} else {
		// Write to MemoryBuffer
		v.memoryBufferPlaceCharWide(r, isWide)

		// Note: We no longer auto-jump to live edge here.
		// Auto-jump happens in LineFeed when NEW content is created.
		// This allows staying scrolled back during resize/redraw.
		v.MarkDirty(v.cursorY)
	}

	// Advance cursor by character width
	newX := v.cursorX + charWidth

	if v.inAltScreen {
		if v.autoWrapMode && newX > rightEdge {
			v.wrapNext = true
			// Position cursor at the edge
			v.SetCursorPos(v.cursorY, rightEdge)
		} else if newX <= rightEdge {
			v.SetCursorPos(v.cursorY, newX)
		} else {
			// At edge, no wrap mode - stay at edge
			v.SetCursorPos(v.cursorY, rightEdge)
		}
	} else {
		// Main screen wrapping logic
		if v.wrapEnabled && newX > rightEdge {
			if v.IsMemoryBufferEnabled() {
				// Memory buffer: let cursorX advance past terminal width.
				// The LogicalLine extends naturally, and WrapToWidth() handles display.
				v.cursorX = newX
			} else {
				// Set wrapNext instead of wrapping immediately
				// This allows CR or LF to clear the flag without creating extra lines
				v.wrapNext = true
				v.SetCursorPos(v.cursorY, rightEdge)
			}
		} else if newX <= rightEdge {
			v.SetCursorPos(v.cursorY, newX)
			// Sync prevCursor with new cursor position so delta-based sync doesn't see false movement.
			v.prevCursorX = v.cursorX
			v.prevCursorY = v.cursorY
		} else {
			// At edge, no wrap mode - stay at edge
			v.SetCursorPos(v.cursorY, rightEdge)
		}
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

// Height returns the current viewport height in rows.
func (v *VTerm) Height() int {
	return v.height
}

// Width returns the current viewport width in columns.
func (v *VTerm) Width() int {
	return v.width
}

// TotalPhysicalLines returns the total number of physical (wrapped) lines
// in the terminal history. This is used for scrollbar calculations.
func (v *VTerm) TotalPhysicalLines() int64 {
	if !v.IsMemoryBufferEnabled() {
		return int64(v.height)
	}
	// Use viewport's total physical line count for accurate scroll calculations
	return v.memBufState.viewport.TotalPhysicalLines()
}

// TotalLogicalLines returns the total number of logical lines in history.
// This counts each line before wrapping (used for minimap).
func (v *VTerm) TotalLogicalLines() int64 {
	if v.memBufState == nil || v.memBufState.memBuf == nil {
		return int64(v.height)
	}
	return v.memBufState.memBuf.TotalLines()
}

// GetLogicalLine returns the logical line at the given global index.
// Used by scrollbar minimap to calculate line lengths.
func (v *VTerm) GetLogicalLine(index int64) *LogicalLine {
	if v.memBufState == nil || v.memBufState.memBuf == nil {
		return nil
	}
	return v.memBufState.memBuf.GetLine(index)
}

// GetGlobalOffset returns the global offset of the oldest line in memory.
// Lines with indices less than this have been evicted.
func (v *VTerm) GetGlobalOffset() int64 {
	if v.memBufState == nil || v.memBufState.memBuf == nil {
		return 0
	}
	return v.memBufState.memBuf.GlobalOffset()
}

// GetAllLogicalLines returns all logical lines from both disk and memory.
// Used by scrollbar minimap. This reads the entire history - optimize later!
// Returns (lines, globalOffset, totalLines) where globalOffset is the global
// index of the first returned line.
func (v *VTerm) GetAllLogicalLines() ([]*LogicalLine, int64, int64) {
	if v.memBufState == nil || v.memBufState.viewport == nil {
		return nil, 0, 0
	}

	// Use viewport's reader which handles both disk and memory
	reader := v.memBufState.viewport.Reader()
	if reader == nil {
		return nil, 0, 0
	}

	globalOffset := reader.GlobalOffset()
	globalEnd := reader.GlobalEnd()
	totalLines := globalEnd - globalOffset

	if totalLines <= 0 {
		return nil, 0, 0
	}

	// Read ALL lines (disk + memory)
	lines := reader.GetLineRange(globalOffset, globalEnd)
	return lines, globalOffset, totalLines
}

// getHistoryLen returns the current history length from MemoryBuffer.
func (v *VTerm) getHistoryLen() int {
	if v.memBufState == nil || v.memBufState.memBuf == nil {
		return 0
	}
	return int(v.memBufState.memBuf.TotalLines())
}

// getLogicalLine retrieves a logical line by global index, with PageStore fallback.
// This should be used instead of accessing memBuf.GetLine() directly, as lines
// scrolled far back may have been evicted from the in-memory ring buffer to disk.
func (v *VTerm) getLogicalLine(globalIdx int64) *LogicalLine {
	if v.memBufState == nil || v.memBufState.memBuf == nil {
		return nil
	}
	line := v.memBufState.memBuf.GetLine(globalIdx)
	if line != nil {
		return line
	}
	// Fallback to PageStore for evicted lines
	if v.memBufState.pageStore != nil {
		line, _ = v.memBufState.pageStore.ReadLine(globalIdx)
		return line
	}
	return nil
}

// getHistoryLine retrieves a specific line from the MemoryBuffer.
// Returns the cells for the logical line at the given global index.
func (v *VTerm) getHistoryLine(index int) []Cell {
	line := v.getLogicalLine(int64(index))
	if line == nil {
		return nil
	}
	return line.Cells
}

// setHistoryLine updates a specific line in the MemoryBuffer.
func (v *VTerm) setHistoryLine(index int, cells []Cell) {
	if v.memBufState == nil || v.memBufState.memBuf == nil {
		return
	}

	// Ensure line exists
	line := v.memBufState.memBuf.EnsureLine(int64(index))
	if line == nil {
		return
	}

	// Update cells
	for i, cell := range cells {
		v.memBufState.memBuf.SetCell(int64(index), i, cell)
	}
}

// eraseHistoryLine clears all content from a line in the MemoryBuffer.
func (v *VTerm) eraseHistoryLine(index int) {
	if v.memBufState == nil || v.memBufState.memBuf == nil {
		return
	}
	v.memBufState.memBuf.EraseLine(int64(index), DefaultFG, DefaultBG)
}

// appendHistoryLine adds a new line to the end of the MemoryBuffer.
// Note: This is mainly used for compatibility. Normal line creation
// happens via memoryBufferLineFeed().
func (v *VTerm) appendHistoryLine(cells []Cell) {
	if v.memBufState == nil || v.memBufState.memBuf == nil {
		return
	}

	// Create a new line at the end
	totalLines := v.memBufState.memBuf.TotalLines()
	v.memBufState.memBuf.EnsureLine(totalLines)

	// Set the cells
	for i, cell := range cells {
		v.memBufState.memBuf.SetCell(totalLines, i, cell)
	}
}

// getTopHistoryLine calculates the index of the first visible line in the viewport.
// Returns the global index at the viewport top.
func (v *VTerm) getTopHistoryLine() int {
	if v.inAltScreen {
		return 0
	}
	if v.memBufState != nil {
		return int(v.memBufState.liveEdgeBase)
	}
	return 0
}

// VisibleTop returns the history index of the first visible line.
func (v *VTerm) VisibleTop() int {
	return v.getTopHistoryLine()
}

// MarkPromptStart records the position where a shell prompt starts.
// Called when OSC 133;A is received (prompt start marker).
// This is a stub for future shell integration features.
func (v *VTerm) MarkPromptStart() {
	// TODO: Record prompt start position in MemoryBuffer for shell integration
	// This would enable features like:
	// - Skipping prompts during selection
	// - Collapsing command output
	// - Seamless recovery after reconnect
}

// MarkInputStart records the position where user input starts.
// Called when OSC 133;B is received (input start / prompt end marker).
// This is a stub for future shell integration features.
func (v *VTerm) MarkInputStart() {
	// TODO: Record input start position in MemoryBuffer for shell integration
	// This would enable features like:
	// - Highlighting user input differently
	// - Command extraction for history
}

// HistoryLength exposes the number of lines tracked in history.
func (v *VTerm) HistoryLength() int {
	return v.getHistoryLen()
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

// --- Selection Coordinate Conversion ---

// ViewportToContent converts viewport coordinates to content coordinates.
// Returns (logicalLine, charOffset, isCurrentLine, ok).
// logicalLine is -1 for the current uncommitted line.
func (v *VTerm) ViewportToContent(y, x int) (logicalLine int64, charOffset int, isCurrentLine bool, ok bool) {
	if v.inAltScreen {
		// Alt screen: treat as current line equivalent
		charOffset = y*v.width + x
		return -1, charOffset, true, true
	}
	// Use MemoryBuffer's viewport to content mapping
	globalLine, offset, valid := v.memoryBufferViewportToContent(y, x)
	if !valid {
		return 0, 0, false, false
	}
	// Check if this is the current cursor line
	cursorLine, _ := v.memBufState.memBuf.GetCursor()
	isCurrentLine = globalLine == cursorLine
	return globalLine, offset, isCurrentLine, true
}

// ContentToViewport converts content coordinates to viewport coordinates.
// Returns (y, x, visible) where visible is true if content is on screen.
func (v *VTerm) ContentToViewport(logicalLine int64, charOffset int) (y, x int, visible bool) {
	if v.inAltScreen {
		// Alt screen: direct mapping
		if v.width <= 0 {
			return 0, 0, false
		}
		y = charOffset / v.width
		x = charOffset % v.width
		visible = y >= 0 && y < v.height
		return
	}
	// Use MemoryBuffer's content to viewport mapping
	return v.memoryBufferContentToViewport(logicalLine, charOffset)
}

// GetContentText extracts text from a content coordinate range.
func (v *VTerm) GetContentText(startLine int64, startOffset int, endLine int64, endOffset int) string {
	if v.inAltScreen {
		// For alt screen, extract from altBuffer
		return v.getAltScreenText(startOffset, endOffset)
	}
	if v.memBufState == nil || v.memBufState.memBuf == nil {
		return ""
	}

	// Extract text from MemoryBuffer line range (with PageStore fallback for evicted lines)
	var result []rune
	for lineIdx := startLine; lineIdx <= endLine; lineIdx++ {
		line := v.getLogicalLine(lineIdx)
		if line == nil {
			continue
		}
		start := 0
		end := len(line.Cells)
		if lineIdx == startLine {
			start = startOffset
		}
		if lineIdx == endLine {
			end = endOffset
		}

		// Extract and trim trailing spaces from each line
		var lineRunes []rune
		for i := start; i < end && i < len(line.Cells); i++ {
			r := line.Cells[i].Rune
			if r == 0 {
				r = ' '
			}
			lineRunes = append(lineRunes, r)
		}

		// Trim trailing spaces
		trimmed := strings.TrimRight(string(lineRunes), " ")
		result = append(result, []rune(trimmed)...)

		// Add newline between lines (but not at the end)
		if lineIdx < endLine {
			result = append(result, '\n')
		}
	}

	return string(result)
}

// getAltScreenText extracts text from alt screen buffer.
// Adds newlines between rows and trims trailing spaces from each row.
func (v *VTerm) getAltScreenText(startOffset, endOffset int) string {
	if v.width <= 0 {
		return ""
	}
	if startOffset > endOffset {
		startOffset, endOffset = endOffset, startOffset
	}

	var result []rune
	prevY := -1

	for offset := startOffset; offset < endOffset; offset++ {
		y := offset / v.width
		x := offset % v.width

		// Add newline when we move to a new row
		if y > prevY && prevY >= 0 {
			// Trim trailing spaces before adding newline
			resultStr := strings.TrimRight(string(result), " ")
			result = []rune(resultStr)
			result = append(result, '\n')
		}
		prevY = y

		// Append character from this position
		if y >= 0 && y < len(v.altBuffer) && x >= 0 && x < len(v.altBuffer[y]) {
			r := v.altBuffer[y][x].Rune
			if r == 0 {
				r = ' '
			}
			result = append(result, r)
		}
	}

	// Trim trailing spaces from the final line
	return strings.TrimRight(string(result), " ")
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
		v.logDebug("[SCROLL] CSI S (Scroll Up): n=%d, margins=%d-%d", param(0, 1), v.marginTop, v.marginBottom)
		if v.leftRightMarginMode {
			v.scrollUpWithinMargins(param(0, 1))
		} else {
			v.scrollRegion(param(0, 1), v.marginTop, v.marginBottom)
		}
	case 'T': // SD - Scroll Down
		v.logDebug("[SCROLL] CSI T (Scroll Down): n=%d, margins=%d-%d", param(0, 1), v.marginTop, v.marginBottom)
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
		// CSI u without intermediate = DECRC (Restore Cursor)
		// CSI > Ps u = Extended keyboard protocol (push mode) - ignore
		// CSI < u = Extended keyboard protocol (pop mode) - ignore
		// CSI = Ps u = Extended keyboard protocol (query mode) - ignore
		if intermediate == 0 {
			v.RestoreCursor()
		}
		// Extended keyboard protocol sequences are silently ignored
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
		v.logDebug("[CUP] Moving cursor to row=%d, col=%d (params=%v)", row, col, params)
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

	// Sync memory buffer cursor after any cursor movement escape sequence.
	// This is done here rather than in SetCursorPos because writeCharWithWrapping also
	// calls SetCursorPos, and it already advances the memory buffer cursor.
	if !v.inAltScreen {
		v.memoryBufferSetCursorFromPhysical()
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
		v.notifyDetectorScrollRegionClear()
		return
	}
	v.marginTop = top - 1
	v.marginBottom = bottom - 1
	v.logDebug("[SCROLL] SetMargins: top=%d, bottom=%d (0-indexed: %d-%d), height=%d", top, bottom, v.marginTop, v.marginBottom, v.height)

	// Notify FixedWidthDetector for TUI detection
	isFullScreen := (top == 1 && bottom == v.height)
	if !isFullScreen {
		v.notifyDetectorScrollRegion(v.marginTop, v.marginBottom, v.height)
	} else {
		v.notifyDetectorScrollRegionClear()
	}

	// Enhanced debug logging for scroll region state
	if v.memBufState != nil && v.memBufState.memBuf != nil {
		mb := v.memBufState.memBuf
		v.logMemBufDebug("[MARGINS] SetMargins top=%d bottom=%d isFullScreen=%v: GlobalOffset=%d GlobalEnd=%d liveEdgeBase=%d TotalLines=%d",
			top, bottom, isFullScreen, mb.GlobalOffset(), mb.GlobalEnd(), v.memBufState.liveEdgeBase, mb.TotalLines())
	}

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
	// Sync memory buffer cursor for direct calls (not through parser)
	if !v.inAltScreen {
		v.memoryBufferSetCursorFromPhysical()
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
	// Sync memory buffer cursor for direct calls (not through parser)
	if !v.inAltScreen {
		v.memoryBufferSetCursorFromPhysical()
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

func WithAltScreenChangeHandler(handler func(bool)) Option {
	return func(v *VTerm) { v.OnAltScreenChange = handler }
}

func WithBracketedPasteModeChangeHandler(handler func(bool)) Option {
	return func(v *VTerm) { v.OnBracketedPasteModeChange = handler }
}

func WithClipboardSetHandler(handler func([]byte)) Option {
	return func(v *VTerm) { v.OnClipboardSet = handler }
}

func WithClipboardGetHandler(handler func() []byte) Option {
	return func(v *VTerm) { v.OnClipboardGet = handler }
}

// WithLineCommitHandler sets a callback invoked when a line is committed during
// normal shell operation (full-screen margins). The handler receives the global
// line index, the logical line (mutable), and whether a command is currently
// active (from OSC 133 shell integration). This is the hook point for inline
// output transformers like txfmt.
func WithLineCommitHandler(handler func(int64, *LogicalLine, bool) bool) Option {
	return func(v *VTerm) { v.OnLineCommit = handler }
}

// RequestLineInsert inserts a synthetic line at beforeIdx during an
// OnLineCommit callback. The inserted line shifts all subsequent lines
// (including currentGlobal) down by 1. The caller's currentGlobal is
// adjusted automatically after the callback returns.
//
// This is safe to call only from within an OnLineCommit handler.
func (v *VTerm) RequestLineInsert(beforeIdx int64, cells []Cell) {
	if !v.IsMemoryBufferEnabled() {
		return
	}
	v.memBufState.memBuf.InsertLine(beforeIdx)
	line := v.memBufState.memBuf.GetLine(beforeIdx)
	if line == nil {
		return
	}
	// Inserted lines are synthetic (transformer-generated).
	// Content goes in Overlay, not Cells.
	line.Overlay = cells
	line.OverlayWidth = len(cells)
	line.Synthetic = true
	v.commitInsertOffset++
	cursorGlobal := v.memBufState.liveEdgeBase + int64(v.cursorY)
	if beforeIdx <= cursorGlobal {
		v.memBufState.liveEdgeBase++
	}
}

// RequestLineOverlay sets overlay content on an existing line without modifying
// the original Cells. Used by transformers to provide formatted views.
func (v *VTerm) RequestLineOverlay(lineIdx int64, cells []Cell) {
	if !v.IsMemoryBufferEnabled() {
		return
	}
	line := v.memBufState.memBuf.GetLine(lineIdx)
	if line == nil {
		return
	}
	line.Overlay = cells
	line.OverlayWidth = len(cells)
}

// NotifyLinePersist notifies the persistence layer that a line is ready for writing.
// Used by transformers after setting overlay content on previously suppressed lines.
func (v *VTerm) NotifyLinePersist(lineIdx int64) {
	if v.memBufState.persistence != nil {
		v.memBufState.persistence.NotifyWrite(lineIdx)
	}
}

// Deprecated: Use SetOnLineIndexed after EnableMemoryBufferWithDisk instead.
// This callback is called BEFORE persistence, which can cause search index entries
// for content that doesn't exist on disk after a crash.
// WithLineIndexHandler sets a callback for when lines are committed (e.g., on line feed).
func WithLineIndexHandler(handler func(lineIdx int64, timestamp time.Time, cells []Cell, isCommand bool)) Option {
	return func(v *VTerm) { v.OnLineIndex = handler }
}

// WithMemoryBuffer enables the new memory buffer system.
// This replaces the old DisplayBuffer system with the Phase 1-3 architecture.
func WithMemoryBuffer() Option {
	return func(v *VTerm) {
		v.initMemoryBuffer()
	}
}

// WithMemoryBufferDisk enables the memory buffer system with disk persistence.
// The diskPath specifies where to store the history file.
func WithMemoryBufferDisk(diskPath string, maxLines int) Option {
	return func(v *VTerm) {
		opts := MemoryBufferOptions{
			MaxLines:      maxLines,
			EvictionBatch: 1000,
			DiskPath:      diskPath,
		}
		v.initMemoryBufferWithOptions(opts)
	}
}

// WithMemoryBufferOptions enables the memory buffer system with custom options.
func WithMemoryBufferOptions(opts MemoryBufferOptions) Option {
	return func(v *VTerm) {
		v.initMemoryBufferWithOptions(opts)
	}
}

// Resize handles changes to the terminal's dimensions.
func (v *VTerm) Resize(width, height int) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if width == v.width && height == v.height {
		return
	}

	oldHeight := v.height
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
		// Use MemoryBuffer/ViewportWindow resize
		v.memoryBufferResize(width, height)
		v.SetCursorPos(v.cursorY, v.cursorX) // Re-clamp cursor
	}

	v.MarkAllDirty()
	// Reset margins on resize (without moving cursor)
	// Note: We can't use SetMargins() because it moves cursor to home per VT spec
	v.marginTop = 0
	v.marginBottom = v.height - 1
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

// LiveEdgeBase returns the current liveEdgeBase (global line index of viewport row 0).
func (v *VTerm) LiveEdgeBase() int64 {
	if v.memBufState == nil {
		return 0
	}
	return v.memBufState.liveEdgeBase
}

// MarginTop returns the current top scroll margin.
func (v *VTerm) MarginTop() int { return v.marginTop }

// MarginBottom returns the current bottom scroll margin.
func (v *VTerm) MarginBottom() int { return v.marginBottom }

// MemoryBuffer returns the underlying MemoryBuffer for diagnostic access.
func (v *VTerm) MemoryBuffer() *MemoryBuffer {
	if v.memBufState == nil {
		return nil
	}
	return v.memBufState.memBuf
}

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

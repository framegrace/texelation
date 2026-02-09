// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/term.go
// Summary: Implements term capabilities for the terminal application.
// Usage: Spawned by desktop factories to provide shell access.
// Notes: Wraps PTY management and integrates with the parser package.

package texelterm

import (
	"bufio"
	"encoding/json"
	"fmt"
	texelcore "github.com/framegrace/texelui/core"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/config"
	"github.com/framegrace/texelation/internal/theming"
	"github.com/framegrace/texelation/texel"
	"github.com/framegrace/texelui/theme"

	"github.com/creack/pty"
	"github.com/gdamore/tcell/v2"
)

func init() {
	// Redirect log output away from stderr to avoid mangling terminal display.
	// If TEXELTERM_DEBUG is set, log to file; otherwise discard.
	if os.Getenv("TEXELTERM_DEBUG") != "" {
		logFile, err := os.OpenFile("/tmp/texelterm-debug.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err == nil {
			log.SetOutput(logFile)
			log.SetFlags(log.Ltime | log.Lmicroseconds)
		} else {
			log.SetOutput(io.Discard)
		}
	} else {
		log.SetOutput(io.Discard)
	}
}

type TexelTerm struct {
	title              string
	command            string
	paneID             string // Pane ID for per-terminal history isolation
	width              int
	height             int
	cmd                *exec.Cmd
	pty                *os.File
	vterm              *parser.VTerm
	parser             *parser.Parser
	mu                 sync.Mutex
	stop               chan struct{}
	stopOnce           sync.Once
	refreshChan        chan<- bool
	wg                 sync.WaitGroup
	buf                [][]texelcore.Cell
	colorPalette       [258]tcell.Color
	controlBus         texelcore.ControlBus
	bracketedPasteMode bool // Tracks if application has enabled bracketed paste

	// Mouse and selection handling (unified for standalone and embedded modes)
	mouseCoordinator *MouseCoordinator
	clipboard        texelcore.ClipboardService

	// Scroll tracking for smooth velocity-based acceleration
	scrollEventTime time.Time // For debouncing duplicate events
	lastScrollTime  time.Time // For velocity tracking
	scrollVelocity  float64   // Accumulated velocity

	// TODO: Extract confirmation dialog to a reusable cards.DialogCard
	// that intercepts key events and renders the overlay. This would allow
	// texelterm to own a pipeline with the dialog card for cleaner separation.
	confirmClose    bool
	confirmCallback func()
	closeCh         chan struct{}
	closeOnce       sync.Once     // Protects closeCh from being closed twice
	restartCh       chan struct{} // Signal to restart shell after confirmation

	// Debug logging
	renderDebugLog func(format string, args ...interface{})

	// State persistence (via texelation storage service)
	storage texelcore.AppStorage

	// Search index (Phase 3 - Disk Layer)
	searchIndex *parser.SQLiteSearchIndex

	// History navigator (Phase 4 - Disk Layer)
	historyNavigator *HistoryNavigator

	// Scrollbar (non-overlay, resizes terminal)
	scrollbar *ScrollBar
}

var _ texelcore.CloseRequester = (*TexelTerm)(nil)
var _ texelcore.CloseCallbackRequester = (*TexelTerm)(nil)
var _ texelcore.StorageSetter = (*TexelTerm)(nil)
var _ texelcore.ClipboardAware = (*TexelTerm)(nil)
var _ texelcore.MouseHandler = (*TexelTerm)(nil)

func New(title, command string) texelcore.App {
	term := &TexelTerm{
		title:        title,
		command:      command,
		width:        80,
		height:       24,
		stop:         make(chan struct{}),
		colorPalette: newDefaultPalette(),
		closeCh:      make(chan struct{}),
		restartCh:    make(chan struct{}, 1),    // Buffered to avoid blocking
		controlBus:   texelcore.NewControlBus(), // Own control bus, no pipeline needed
	}

	return term
}

func (a *TexelTerm) RequestClose() bool {
	return a.RequestCloseWithCallback(func() {
		// External close confirmed - stop the app
		a.Stop()
	})
}

func (a *TexelTerm) RequestCloseWithCallback(onConfirm func()) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.confirmClose = true
	a.confirmCallback = onConfirm
	a.requestRefresh()
	return false
}

func (a *TexelTerm) drawConfirmation(buf [][]texelcore.Cell) {
	if len(buf) == 0 {
		return
	}
	height := len(buf)
	width := len(buf[0])

	// Box dimensions
	boxW := 40
	boxH := 5
	x := (width - boxW) / 2
	y := (height - boxH) / 2

	// Ensure fits
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if boxW > width {
		boxW = width
	}
	if boxH > height {
		boxH = height
	}

	style := tcell.StyleDefault.Background(tcell.ColorDarkRed).Foreground(tcell.ColorWhite)
	borderStyle := tcell.StyleDefault.Background(tcell.ColorDarkRed).Foreground(tcell.ColorWhite)

	// Draw box
	for r := 0; r < boxH; r++ {
		for c := 0; c < boxW; c++ {
			if y+r < height && x+c < width {
				buf[y+r][x+c] = texelcore.Cell{Ch: ' ', Style: style}
			}
		}
	}

	// Borders
	for c := 0; c < boxW; c++ {
		buf[y][x+c] = texelcore.Cell{Ch: tcell.RuneHLine, Style: borderStyle}
		buf[y+boxH-1][x+c] = texelcore.Cell{Ch: tcell.RuneHLine, Style: borderStyle}
	}
	for r := 0; r < boxH; r++ {
		buf[y+r][x] = texelcore.Cell{Ch: tcell.RuneVLine, Style: borderStyle}
		buf[y+r][x+boxW-1] = texelcore.Cell{Ch: tcell.RuneVLine, Style: borderStyle}
	}
	buf[y][x] = texelcore.Cell{Ch: tcell.RuneULCorner, Style: borderStyle}
	buf[y][x+boxW-1] = texelcore.Cell{Ch: tcell.RuneURCorner, Style: borderStyle}
	buf[y+boxH-1][x] = texelcore.Cell{Ch: tcell.RuneLLCorner, Style: borderStyle}
	buf[y+boxH-1][x+boxW-1] = texelcore.Cell{Ch: tcell.RuneLRCorner, Style: borderStyle}

	// Text
	msg := "Close Terminal? (y/n)"
	textX := x + (boxW-len(msg))/2
	textY := y + 2
	if textY < height && textY >= 0 {
		for i, r := range msg {
			col := textX + i
			if col >= 0 && col < width {
				buf[textY][col] = texelcore.Cell{Ch: r, Style: style.Bold(true)}
			}
		}
	}
}

func (a *TexelTerm) Vterm() *parser.VTerm {
	return a.vterm
}

func (a *TexelTerm) mapParserColorToTCell(c parser.Color) tcell.Color {
	switch c.Mode {
	case parser.ColorModeDefault:
		return a.colorPalette[256]
	case parser.ColorModeStandard, parser.ColorMode256:
		return a.colorPalette[c.Value]
	case parser.ColorModeRGB:
		return tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B))
	default:
		return tcell.ColorDefault
	}
}

func (a *TexelTerm) applyParserStyle(pCell parser.Cell) texelcore.Cell {
	fgColor := a.mapParserColorToTCell(pCell.FG)
	var bgColor tcell.Color
	if pCell.BG.Mode == parser.ColorModeDefault {
		bgColor = a.colorPalette[257]
	} else {
		bgColor = a.mapParserColorToTCell(pCell.BG)
	}

	// Apply DIM locally by reducing foreground brightness rather than
	// passing it to tcell as ESC[2m. Some outer terminals apply DIM
	// to background colors too, causing visual artifacts.
	if pCell.Attr&parser.AttrDim != 0 && fgColor != tcell.ColorDefault {
		r, g, b := fgColor.RGB()
		fgColor = tcell.NewRGBColor(r*6/10, g*6/10, b*6/10)
	}

	style := tcell.StyleDefault.
		Foreground(fgColor).
		Background(bgColor).
		Bold(pCell.Attr&parser.AttrBold != 0).
		Italic(pCell.Attr&parser.AttrItalic != 0).
		Underline(pCell.Attr&parser.AttrUnderline != 0).
		Reverse(pCell.Attr&parser.AttrReverse != 0)

	ch := pCell.Rune
	if ch == 0 {
		ch = ' '
	}

	return texelcore.Cell{
		Ch:    ch,
		Style: style,
	}
}

func (a *TexelTerm) SetRefreshNotifier(refreshChan chan<- bool) {
	a.refreshChan = refreshChan
	if a.historyNavigator != nil {
		a.historyNavigator.SetRefreshNotifier(refreshChan)
	}
}

// ControlBus returns the terminal's control bus for external registration.
func (a *TexelTerm) ControlBus() texelcore.ControlBus {
	return a.controlBus
}

// RegisterControl implements texelcore.ControlBusProvider.
func (a *TexelTerm) RegisterControl(id, description string, handler func(payload interface{}) error) error {
	return a.controlBus.Register(id, description, texel.ControlHandler(handler))
}

func (a *TexelTerm) SetPaneID(id [16]byte) {
	a.mu.Lock()
	a.paneID = fmt.Sprintf("%x", id)
	a.mu.Unlock()
}

// SetStorage implements texelcore.StorageSetter for per-pane state persistence.
// State is loaded immediately if vterm is already initialized.
func (a *TexelTerm) SetStorage(storage texelcore.AppStorage) {
	a.mu.Lock()
	a.storage = storage
	// If vterm is already initialized, load and apply state now
	// Note: This only applies scroll offset; prompt-aware populate is handled in runShell
	if a.vterm != nil {
		savedState := a.loadStateLocked()
		a.applyRestoredStateLocked(savedState)
	}
	a.mu.Unlock()
}

// terminalState holds the persisted terminal state for server restart recovery.
type terminalState struct {
	CursorX          int   `json:"cursorX"`
	CursorY          int   `json:"cursorY"`
	ScrollOffset     int64 `json:"scrollOffset"`
	LastPromptLine   int64 `json:"lastPromptLine"`   // Global line index of last prompt (-1 if unknown)
	LastPromptHeight int   `json:"lastPromptHeight"` // Number of lines in the prompt (default 1)
}

// saveStateLocked persists state while holding the lock.
func (a *TexelTerm) saveStateLocked() {
	if a.storage == nil || a.vterm == nil {
		return
	}
	// Don't save state when in alternate screen mode
	if a.vterm.InAltScreen() {
		return
	}

	cursorX, cursorY := a.vterm.Cursor()
	state := terminalState{
		CursorX:          cursorX,
		CursorY:          cursorY,
		ScrollOffset:     a.vterm.ScrollOffset(),
		LastPromptLine:   a.vterm.LastPromptLine(),
		LastPromptHeight: a.vterm.LastPromptHeight(),
	}

	log.Printf("[TEXELTERM] Saving state: scrollOffset=%d, cursor=(%d,%d), lastPromptLine=%d, promptHeight=%d",
		state.ScrollOffset, cursorX, cursorY, state.LastPromptLine, state.LastPromptHeight)
	if err := a.storage.Set("state", state); err != nil {
		log.Printf("[TEXELTERM] Failed to save state: %v", err)
	}
}

// loadStateLocked loads persisted state while holding the lock.
// Called when storage is set and vterm is ready.
// Returns the loaded state (with default values if no saved state exists).
func (a *TexelTerm) loadStateLocked() terminalState {
	defaultState := terminalState{LastPromptLine: -1, LastPromptHeight: 1}

	if a.storage == nil || a.vterm == nil {
		return defaultState
	}

	data, err := a.storage.Get("state")
	if err != nil || data == nil {
		return defaultState // No saved state
	}

	var state terminalState
	if err := json.Unmarshal(data, &state); err != nil {
		log.Printf("[TEXELTERM] Failed to load state: %v", err)
		return defaultState
	}

	// Ensure prompt height has a valid default (for old state files)
	if state.LastPromptHeight < 1 {
		state.LastPromptHeight = 1
	}

	log.Printf("[TEXELTERM] Loaded state: scrollOffset=%d, lastPromptLine=%d, promptHeight=%d",
		state.ScrollOffset, state.LastPromptLine, state.LastPromptHeight)
	return state
}

// applyRestoredStateLocked applies the scroll offset from a loaded state.
// This should be called after populating the viewport from history.
func (a *TexelTerm) applyRestoredStateLocked(state terminalState) {
	if a.vterm == nil {
		return
	}

	// Restore scroll offset (cursor is managed by the shell)
	if state.ScrollOffset > 0 {
		log.Printf("[TEXELTERM] Restoring scroll offset: %d (will set restoredView=true)", state.ScrollOffset)
		a.vterm.SetScrollOffset(state.ScrollOffset)
		log.Printf("[TEXELTERM] Restored scroll offset: %d, memoryBuf enabled=%v", state.ScrollOffset, a.vterm.IsMemoryBufferEnabled())
	} else {
		log.Printf("[TEXELTERM] No scroll offset to restore (offset=%d)", state.ScrollOffset)
	}
}

func (a *TexelTerm) SnapshotMetadata() (appType string, config map[string]interface{}) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Only store the command - env and cwd are in pane-ID-based files
	config = make(map[string]interface{})
	config["command"] = a.command

	return "texelterm", config
}

func colorToHex(c tcell.Color) string {
	trueColor := c.TrueColor()
	if !trueColor.Valid() {
		return "#000000"
	}
	r, g, b := trueColor.RGB()
	return fmt.Sprintf("#%02X%02X%02X", r&0xFF, g&0xFF, b&0xFF)
}

// logRenderDebug logs render state when TEXELTERM_DEBUG is set.
// Logs cursor position, dirty lines, and content of rows being rendered.
func (a *TexelTerm) logRenderDebug(grid [][]parser.Cell, cursorX, cursorY int, dirtyLines map[int]bool, allDirty bool) {
	if a.renderDebugLog == nil {
		return
	}

	rows := len(grid)
	cols := 0
	if rows > 0 {
		cols = len(grid[0])
	}

	a.renderDebugLog("Render: cursorX=%d, cursorY=%d, allDirty=%v, dirtyLines=%v",
		cursorX, cursorY, allDirty, dirtyLines)

	// Determine which rows to log
	rowsToLog := make(map[int]bool)
	if allDirty {
		// Log first 5 rows plus rows around cursor
		for y := 0; y < 5 && y < rows; y++ {
			rowsToLog[y] = true
		}
		for y := cursorY - 2; y <= cursorY+2; y++ {
			if y >= 0 && y < rows {
				rowsToLog[y] = true
			}
		}
	} else {
		for y := range dirtyLines {
			rowsToLog[y] = true
		}
	}

	// Log content of selected rows (first 50 chars)
	for y := 0; y < rows; y++ {
		if !rowsToLog[y] {
			continue
		}
		var content string
		for x := 0; x < cols && x < 50; x++ {
			r := grid[y][x].Rune
			if r == 0 {
				r = ' '
			}
			content += string(r)
		}
		a.renderDebugLog("  vtermGrid[%d]: %q", y, content)
	}
}

func (a *TexelTerm) Render() [][]texelcore.Cell {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.vterm == nil {
		return nil
	}

	vtermGrid := a.vterm.Grid()
	rows := len(vtermGrid)
	if rows == 0 {
		return nil
	}
	vtermCols := len(vtermGrid[0])

	// Calculate total output width (terminal + scrollbar if visible)
	totalCols := vtermCols
	scrollbarVisible := a.scrollbar != nil && a.scrollbar.IsVisible()
	if scrollbarVisible {
		totalCols = vtermCols + ScrollBarWidth
	}

	// Resize buffer if needed
	if len(a.buf) != rows || (rows > 0 && len(a.buf[0]) != totalCols) {
		a.buf = make([][]texelcore.Cell, rows)
		for y := range a.buf {
			a.buf[y] = make([]texelcore.Cell, totalCols)
		}
		a.vterm.MarkAllDirty()
	}

	cursorX, cursorY := a.vterm.Cursor()
	// Only show cursor if it's visible AND we're at the live edge (not scrolled into history)
	cursorVisible := a.vterm.CursorVisible() && a.vterm.AtLiveEdge()
	dirtyLines, allDirty := a.vterm.DirtyLines()

	a.logRenderDebug(vtermGrid, cursorX, cursorY, dirtyLines, allDirty)

	renderLine := func(y int) {
		for x := 0; x < vtermCols; x++ {
			parserCell := vtermGrid[y][x]
			a.buf[y][x] = a.applyParserStyle(parserCell)
			if cursorVisible && x == cursorX && y == cursorY {
				a.buf[y][x].Style = a.buf[y][x].Style.Reverse(true)
			}
		}
	}

	if allDirty {
		for y := 0; y < rows; y++ {
			renderLine(y)
		}
	} else {
		for y := range dirtyLines {
			if y >= 0 && y < rows {
				renderLine(y)
			}
		}
	}

	a.vterm.ClearDirty()
	a.applySelectionHighlightLocked(a.buf)

	// Composite scrollbar on the right side (non-overlay)
	if scrollbarVisible {
		scrollbarGrid := a.scrollbar.Render()
		if scrollbarGrid != nil {
			for y := 0; y < rows && y < len(scrollbarGrid); y++ {
				for x := 0; x < ScrollBarWidth && x < len(scrollbarGrid[y]); x++ {
					a.buf[y][vtermCols+x] = scrollbarGrid[y][x]
				}
			}
		}
	}

	if a.confirmClose {
		a.drawConfirmation(a.buf)
	}

	// Render history navigator overlay (Phase 4 - Disk Layer)
	// Note: Navigator overlays the terminal content only, not the scrollbar
	if a.historyNavigator != nil && a.historyNavigator.IsVisible() {
		a.buf = a.historyNavigator.Render(a.buf)
	}

	return a.buf
}

// applySelectionHighlightLocked applies selection highlighting to the render buffer.
// Must be called with a.mu locked.
func (a *TexelTerm) applySelectionHighlightLocked(buf [][]texelcore.Cell) {
	if a.vterm == nil || a.mouseCoordinator == nil || len(buf) == 0 {
		return
	}
	if !a.mouseCoordinator.IsSelectionRendered() {
		return
	}
	// Selection range is in content coordinates (logicalLine, charOffset)
	startLine, startOffset, endLine, endOffset, ok := a.mouseCoordinator.GetSelectionRange()
	if !ok {
		return
	}

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

	// Convert content coordinates to viewport coordinates for rendering
	startRow, startCol, startVisible := a.vterm.ContentToViewport(startLine, startOffset)
	endRow, endCol, endVisible := a.vterm.ContentToViewport(endLine, endOffset)

	// If neither endpoint is visible, check if selection spans through viewport
	if !startVisible && !endVisible {
		// Selection might still be visible if it spans the entire viewport
		// Check if startLine is above viewport and endLine is below
		if startLine < endLine {
			// Selection spans vertically - highlight entire viewport rows between them
			// For now, clamp to viewport boundaries
			startRow = 0
			startCol = 0
			endRow = len(buf) - 1
			endCol = len(buf[endRow])
		} else {
			return
		}
	} else if !startVisible {
		// Start is above viewport
		startRow = 0
		startCol = 0
	} else if !endVisible {
		// End is below viewport
		endRow = len(buf) - 1
		if endRow >= 0 {
			endCol = len(buf[endRow])
		}
	}

	// Highlight the selection range
	for y := 0; y < len(buf); y++ {
		if y < startRow || y > endRow {
			continue
		}
		row := buf[y]
		lineStart := 0
		lineEnd := len(row)
		if y == startRow {
			lineStart = clampInt(startCol, 0, lineEnd)
		}
		if y == endRow {
			lineEnd = clampInt(endCol, lineStart, len(row)) // endCol is already exclusive
		}
		if y > startRow && y < endRow {
			lineStart = 0
			lineEnd = len(row)
		}
		if y == startRow && y == endRow {
			lineEnd = clampInt(endCol, lineStart, len(row))
		}
		for x := lineStart; x < lineEnd && x < len(row); x++ {
			row[x].Style = row[x].Style.Background(highlight).Foreground(fgColor)
		}
	}
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

// expandTildePath expands a leading ~ to the user's home directory.
// Supports "~" alone or "~/path" format.
func expandTildePath(path string) string {
	if path == "" {
		return path
	}
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// handleConfirmationKey processes key events when the close confirmation dialog is shown.
// Returns true if the key was handled by the confirmation dialog.
// Caller must hold a.mu on entry; the lock may be released during callback execution.
func (a *TexelTerm) handleConfirmationKey(ev *tcell.EventKey) bool {
	if !a.confirmClose {
		return false
	}
	if ev.Key() != tcell.KeyRune {
		return true // Absorb non-rune keys while dialog is shown
	}

	r := ev.Rune()
	if r == 'y' || r == 'Y' {
		a.confirmClose = false
		if a.confirmCallback != nil {
			a.mu.Unlock()
			a.confirmCallback()
			a.mu.Lock()
			return true
		}
		// Internal close (PTY exit) - user confirmed, close the pane
		a.closeOnce.Do(func() { close(a.closeCh) })
	} else if r == 'n' || r == 'N' {
		a.confirmClose = false
		wasExternal := a.confirmCallback != nil
		a.confirmCallback = nil
		if a.vterm != nil {
			a.vterm.MarkAllDirty()
		}
		a.requestRefresh()
		// If this was an internal close (shell exit), restart the shell
		if !wasExternal {
			select {
			case a.restartCh <- struct{}{}:
			default:
			}
		}
	}
	return true
}

// handleAltScrollKey processes Alt+key combinations for scrollback navigation.
// Returns true if the key was handled as a scroll operation.
func (a *TexelTerm) handleAltScrollKey(key tcell.Key) bool {
	var scrollAmount int
	switch key {
	case tcell.KeyPgDn:
		scrollAmount = a.height
	case tcell.KeyPgUp:
		scrollAmount = -a.height
	case tcell.KeyDown:
		scrollAmount = 1
	case tcell.KeyUp:
		scrollAmount = -1
	default:
		return false
	}

	a.mu.Lock()
	a.vterm.Scroll(scrollAmount)
	a.saveStateLocked()
	a.mu.Unlock()

	a.requestRefresh()
	return true
}

// keyToEscapeSequence converts a tcell key event to the appropriate escape sequence.
// appMode indicates whether the terminal is in application cursor keys mode.
func (a *TexelTerm) keyToEscapeSequence(ev *tcell.EventKey, appMode bool) []byte {
	switch ev.Key() {
	case tcell.KeyUp:
		return []byte(If(appMode, "\x1bOA", "\x1b[A"))
	case tcell.KeyDown:
		return []byte(If(appMode, "\x1bOB", "\x1b[B"))
	case tcell.KeyRight:
		return []byte(If(appMode, "\x1bOC", "\x1b[C"))
	case tcell.KeyLeft:
		return []byte(If(appMode, "\x1bOD", "\x1b[D"))
	case tcell.KeyHome:
		return []byte("\x1b[H")
	case tcell.KeyEnd:
		return []byte("\x1b[F")
	case tcell.KeyInsert:
		return []byte("\x1b[2~")
	case tcell.KeyDelete:
		return []byte("\x1b[3~")
	case tcell.KeyPgUp:
		return []byte("\x1b[5~")
	case tcell.KeyPgDn:
		return []byte("\x1b[6~")
	case tcell.KeyF1:
		return []byte("\x1bOP")
	case tcell.KeyF2:
		return []byte("\x1bOQ")
	case tcell.KeyF3:
		return []byte("\x1bOR")
	case tcell.KeyF4:
		return []byte("\x1bOS")
	case tcell.KeyEnter:
		return []byte("\r")
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		return []byte{0x7F}
	case tcell.KeyTab:
		return []byte("\t")
	case tcell.KeyEsc:
		return []byte("\x1b")
	default:
		return []byte(string(ev.Rune()))
	}
}

func (a *TexelTerm) HandleKey(ev *tcell.EventKey) {
	// Handle confirmation dialog (if shown)
	a.mu.Lock()
	if a.handleConfirmationKey(ev) {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()

	// Handle Ctrl+G to open history navigator (Ctrl+G = "goto" in history)
	// Note: Ctrl+Shift+F doesn't work reliably (CSI u encoding issues)
	if ev.Key() == tcell.KeyCtrlG {
		if a.historyNavigator != nil {
			log.Printf("[HISTORY_NAV] Opening via Ctrl+G")
			a.historyNavigator.Show()
			// Also show scrollbar when navigator opens
			if a.scrollbar != nil {
				a.scrollbar.Show()
			}
			a.requestRefresh()
			return
		}
	}

	// Route keys to history navigator if visible
	if a.historyNavigator != nil && a.historyNavigator.IsVisible() {
		if a.historyNavigator.HandleKey(ev) {
			return
		}
	}

	// Handle Alt+B to toggle scrollbar
	if ev.Modifiers()&tcell.ModAlt != 0 && ev.Key() == tcell.KeyRune && ev.Rune() == 'b' {
		if a.scrollbar != nil {
			a.scrollbar.Toggle()
		}
		return
	}

	if a.pty == nil {
		return
	}

	// Handle Alt+key scroll operations
	if ev.Modifiers()&tcell.ModAlt != 0 {
		if a.handleAltScrollKey(ev.Key()) {
			return
		}
	}

	// Convert key to escape sequence and send to PTY
	a.mu.Lock()
	appMode := a.vterm.AppCursorKeys()
	a.vterm.EnsureLiveEdge()
	a.mu.Unlock()

	keyBytes := a.keyToEscapeSequence(ev, appMode)
	if _, err := a.pty.Write(keyBytes); err != nil {
		log.Printf("[TEXELTERM] Failed to write key to PTY: %v", err)
	}
}

func (a *TexelTerm) HandlePaste(data []byte) {
	if a.pty == nil || len(data) == 0 {
		return
	}

	// Pasting is a user action - scroll to live edge
	a.mu.Lock()
	if a.vterm != nil {
		a.vterm.EnsureLiveEdge()
	}
	a.mu.Unlock()

	// Check if bracketed paste mode is enabled (bool reads are atomic)
	if a.bracketedPasteMode {
		// In bracketed paste mode, send data as-is (preserve LF)
		// The application knows it's paste data and handles newlines itself
		prefix := []byte("\x1b[200~")
		suffix := []byte("\x1b[201~")

		// Write: prefix + data + suffix
		if _, err := a.pty.Write(prefix); err != nil {
			log.Printf("TexelTerm: paste prefix write failed: %v", err)
			return
		}
		if _, err := a.pty.Write(data); err != nil {
			log.Printf("TexelTerm: paste data write failed: %v", err)
			return
		}
		if _, err := a.pty.Write(suffix); err != nil {
			log.Printf("TexelTerm: paste suffix write failed: %v", err)
		}
	} else {
		// No bracketed paste - convert LF to CR (terminal behavior)
		converted := make([]byte, len(data))
		for i, b := range data {
			if b == '\n' {
				converted[i] = '\r'
			} else {
				converted[i] = b
			}
		}
		if _, err := a.pty.Write(converted); err != nil {
			log.Printf("TexelTerm: paste write failed: %v", err)
		}
	}
}

// HandleMouse implements texelcore.MouseHandler.
// Handles history navigator, scrollbar clicks, then delegates other events to MouseCoordinator.
// Handles history navigator, scrollbar clicks, then delegates other events to MouseCoordinator.
func (a *TexelTerm) HandleMouse(ev *tcell.EventMouse) {
	if ev == nil {
		return
	}

	// Check if history navigator is visible and wants the event
	if a.historyNavigator != nil && a.historyNavigator.IsVisible() {
		if a.historyNavigator.HandleMouse(ev) {
			a.requestRefresh()
			return
		}
	}

	x, y := ev.Position()
	buttons := ev.Buttons()

	// Check if click is on the scrollbar
	if a.scrollbar != nil && a.scrollbar.IsVisible() {
		scrollbarX := a.width - ScrollBarWidth
		if x >= scrollbarX {
			// Handle scrollbar click on button press
			if buttons&tcell.Button1 != 0 {
				localX := x - scrollbarX
				if targetOffset, ok := a.scrollbar.HandleClick(localX, y); ok {
					a.scrollToOffsetWithResultSelection(targetOffset)
					return
				}
			}
			return // Ignore other scrollbar events
		}
	}

	// Delegate to mouse coordinator for terminal content
	if a.mouseCoordinator != nil {
		a.mouseCoordinator.HandleMouse(ev)
	}
	a.requestRefresh()
}

// SetClipboard implements ClipboardSetter for internal use by MouseCoordinator.
// Currently disabled pending investigation of clipboard crash issues.
func (a *TexelTerm) SetClipboard(mime string, data []byte) {
	// Use clipboard service if available
	if a.clipboard != nil {
		a.clipboard.SetClipboard(mime, data)
	}
}

// SetClipboardService implements texelcore.ClipboardAware.
// This is called by the runtime (standalone) or desktop (embedded) to provide clipboard access.
func (a *TexelTerm) SetClipboardService(clipboard texelcore.ClipboardService) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.clipboard = clipboard
}

func (a *TexelTerm) HandleMouseWheel(x, y, deltaX, deltaY int, modifiers tcell.ModMask) {
	if deltaY == 0 {
		return
	}
	a.mu.Lock()
	if a.vterm == nil {
		a.mu.Unlock()
		return
	}

	now := time.Now()

	// Read scroll configuration from app config.
	cfg := config.App("texelterm")
	debounceMs := cfg.GetInt("texelterm.scroll", "debounce_ms", 50)
	debounceThreshold := time.Duration(debounceMs) * time.Millisecond

	// Debounce: Ignore events that are too close together
	// This handles mice that send multiple events per physical click
	if !a.scrollEventTime.IsZero() && now.Sub(a.scrollEventTime) < debounceThreshold {
		a.mu.Unlock()
		return
	}
	a.scrollEventTime = now

	lines := deltaY

	if modifiers&tcell.ModShift != 0 {
		// Shift modifier: full page scroll
		page := a.height
		if page <= 0 {
			page = 1
		}
		lines *= page
	} else {
		// Smooth velocity-based acceleration - read parameters from config.
		velocityDecay := cfg.GetFloat("texelterm.scroll", "velocity_decay", 0.6)
		velocityIncrement := cfg.GetFloat("texelterm.scroll", "velocity_increment", 0.6)
		maxVelocity := cfg.GetFloat("texelterm.scroll", "max_velocity", 15.0)
		expCurve := cfg.GetFloat("texelterm.scroll", "exponential_curve", 0.8)

		// Calculate time since last scroll
		timeDelta := now.Sub(a.lastScrollTime).Seconds()

		// Update velocity with smooth decay
		if timeDelta < velocityDecay && !a.lastScrollTime.IsZero() {
			// Continued scrolling - gradually increase velocity
			a.scrollVelocity += velocityIncrement
			if a.scrollVelocity > maxVelocity {
				a.scrollVelocity = maxVelocity
			}
		} else {
			// Long pause or first scroll - reset to base
			a.scrollVelocity = 0.0
		}

		// Apply smooth exponential curve: 1 + velocity^curve
		// This creates gentler acceleration than linear
		smoothVelocity := math.Pow(a.scrollVelocity, expCurve)
		multiplier := 1.0 + smoothVelocity

		lines = int(float64(lines) * multiplier)
		if lines == 0 && deltaY != 0 {
			lines = deltaY
		}
	}

	a.lastScrollTime = now
	a.vterm.Scroll(lines)
	a.saveStateLocked()

	a.mu.Unlock()
	a.requestRefresh()
}

func (a *TexelTerm) Run() error {
	// Main run loop - allows restarting shell after exit
	for {
		// Create a new closeCh for this iteration (in case previous one was closed)
		a.mu.Lock()
		a.closeCh = make(chan struct{})
		a.closeOnce = sync.Once{} // Reset closeOnce for new closeCh
		a.mu.Unlock()

		err := a.runShell()

		// runShell() already consumed the signal (closeCh, restartCh, or stop)
		// Check the error to decide what to do
		if err != nil {
			if err.Error() == "user confirmed close" {
				return nil // User pressed 'y' to close confirmation
			}
			if err.Error() == "external stop" {
				return nil // Stop() was called
			}
			// Unexpected error
			return err
		}
		// err == nil means user pressed 'n' to decline close, restart the shell
		log.Println("Restarting shell after user declined close")
		continue
	}
}

// getShellCommandSimpleIntegration returns a command that integrates shell monitoring
// Uses --rcfile approach with simplified integration (no background jobs)
func (a *TexelTerm) getShellCommandSimpleIntegration(env []string) *exec.Cmd {
	shellName := strings.ToLower(filepath.Base(a.command))

	// For bash, use --rcfile to load integration + user bashrc
	if strings.Contains(shellName, "bash") {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			configDir := filepath.Join(homeDir, ".config", "texelation", "shell-integration")
			if err := a.ensureShellIntegrationScripts(configDir); err == nil {
				// Use existing bash-wrapper.sh which sources both integration and user bashrc
				wrapperScript := filepath.Join(configDir, "bash-wrapper.sh")

				// bash-wrapper.sh should already exist from ensureShellIntegrationScripts
				if _, err := os.Stat(wrapperScript); err == nil {
					log.Printf("Shell integration: bash --rcfile %s -i (simplified, no background jobs)", wrapperScript)
					return exec.Command(a.command, "--rcfile", wrapperScript, "-i")
				}
			}
		}
	}

	// Default: no integration, just run shell normally
	log.Printf("Shell integration: disabled for %s", shellName)
	return exec.Command(a.command)
}

// ensureShellIntegrationScripts creates shell integration scripts if they don't exist
func (a *TexelTerm) ensureShellIntegrationScripts(configDir string) error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// We assume the scripts are already created in the config directory
	// If not, they would need to be embedded in the binary or copied on first run
	// For now, we just log and continue
	log.Printf("Shell integration directory created: %s", configDir)
	log.Printf("Please ensure integration scripts exist in this directory")

	return nil
}

// StandalonePaneID is the fixed pane ID used for standalone texelterm.
// This ensures standalone sessions have persistent history and search index
// that survives across sessions, separate from texelation pane IDs.
const StandalonePaneID = "standalone-texelterm"

func (a *TexelTerm) runShell() error {
	a.mu.Lock()
	cols, rows := a.width, a.height
	isRestart := a.vterm != nil
	paneID := a.paneID
	// Use standalone pane ID if not embedded in texelation
	if paneID == "" {
		paneID = StandalonePaneID
		a.paneID = paneID
	}
	a.mu.Unlock()

	log.Printf("[TEXELTERM] runShell starting: cols=%d, rows=%d, restart=%v, paneID=%s", cols, rows, isRestart, paneID)

	// Load environment and working directory from pane-specific file
	env, cwd := a.loadShellEnvironment(paneID)

	// Start PTY with shell command
	ptmx, cmd, err := a.startPTY(cols, rows, env, cwd)
	if err != nil {
		return err
	}
	a.pty = ptmx
	a.cmd = cmd

	// Initialize or update VTerm
	if isRestart {
		a.updatePtyWriterForRestart()
	} else {
		a.initializeVTermFirstRun(cols, rows, paneID)
	}

	// Start PTY reader and wait for exit
	return a.runPtyReaderLoop(ptmx, cmd)
}

// loadShellEnvironment loads environment variables and working directory from pane-specific file.
func (a *TexelTerm) loadShellEnvironment(paneID string) (env []string, cwd string) {
	if paneID != "" {
		if homeDir, err := os.UserHomeDir(); err == nil {
			envFile := filepath.Join(homeDir, fmt.Sprintf(".texel-env-%s", paneID))
			if data, err := os.ReadFile(envFile); err == nil {
				lines := strings.Split(string(data), "\n")
				for _, line := range lines {
					if line == "" {
						continue
					}
					if strings.HasPrefix(line, "__TEXEL_CWD=") {
						cwd = strings.TrimPrefix(line, "__TEXEL_CWD=")
						log.Printf("Restored working directory: %s", cwd)
						continue
					}
					// Skip bash functions to avoid import errors
					if strings.HasPrefix(line, "BASH_FUNC_") {
						continue
					}
					env = append(env, line)
				}
				log.Printf("Loaded environment from %s: %d variables", envFile, len(env))
			} else {
				log.Printf("Could not read env file %s: %v (normal on first run)", envFile, err)
			}
		}
	}

	// Fall back to os.Environ if file read failed
	if len(env) == 0 {
		log.Println("No pane-based env file, using os.Environ()")
		env = os.Environ()
	}

	// Always set TERM for the shell
	env = append(env, "TERM=xterm-256color")

	// Set pane ID for per-terminal history isolation
	if paneID != "" {
		env = append(env, "TEXEL_PANE_ID="+paneID)
	}

	return env, cwd
}

// startPTY creates and starts the PTY with the shell command.
func (a *TexelTerm) startPTY(cols, rows int, env []string, cwd string) (*os.File, *exec.Cmd, error) {
	cmd := a.getShellCommandSimpleIntegration(env)
	cmd.Env = env
	if cwd != "" {
		cmd.Dir = cwd
		log.Printf("Starting shell in directory: %s", cwd)
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start pty: %w", err)
	}
	return ptmx, cmd, nil
}

// updatePtyWriterForRestart updates the PTY writer callback for shell restart.
func (a *TexelTerm) updatePtyWriterForRestart() {
	a.mu.Lock()
	defer a.mu.Unlock()

	log.Println("Reusing existing vterm for seamless restart")
	if a.vterm != nil {
		a.vterm.WriteToPty = func(b []byte) {
			if a.pty != nil {
				if _, err := a.pty.Write(b); err != nil {
					log.Printf("[TEXELTERM] Failed to write to PTY: %v", err)
				}
			}
		}
	}
}

// initializeVTermFirstRun creates and configures VTerm for first-time shell run.
func (a *TexelTerm) initializeVTermFirstRun(cols, rows int, paneID string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg := config.App("texelterm")
	wrapEnabled := cfg.GetBool("texelterm", "wrap_enabled", true)
	reflowEnabled := cfg.GetBool("texelterm", "reflow_enabled", true)
	displayBufferEnabled := cfg.GetBool("texelterm", "display_buffer_enabled", true)

	a.vterm = parser.NewVTerm(cols, rows,
		parser.WithTitleChangeHandler(func(newTitle string) {
			a.title = newTitle
			a.requestRefresh()
		}),
		parser.WithCommandStartHandler(func(cmd string) {
			if cmd != "" {
				a.title = cmd
				a.requestRefresh()
			}
		}),
		parser.WithPtyWriter(func(b []byte) {
			if a.pty != nil {
				if _, err := a.pty.Write(b); err != nil {
					log.Printf("[TEXELTERM] Failed to write to PTY: %v", err)
				}
			}
		}),
		parser.WithDefaultFgChangeHandler(func(c parser.Color) {
			a.colorPalette[256] = a.mapParserColorToTCell(c)
		}),
		parser.WithDefaultBgChangeHandler(func(c parser.Color) {
			a.colorPalette[257] = a.mapParserColorToTCell(c)
		}),
		parser.WithQueryDefaultFgHandler(func() {
			a.respondToColorQuery(10)
		}),
		parser.WithQueryDefaultBgHandler(func() {
			a.respondToColorQuery(11)
		}),
		parser.WithScreenRestoredHandler(func() {
			go a.Resize(a.width, a.height)
		}),
		parser.WithBracketedPasteModeChangeHandler(func(enabled bool) {
			a.bracketedPasteMode = enabled
		}),
		parser.WithWrap(wrapEnabled),
		parser.WithReflow(reflowEnabled),
	)
	a.parser = parser.NewParser(a.vterm)

	if displayBufferEnabled {
		// MemoryBuffer is now the only system (DisplayBuffer was removed)
		a.initializeMemoryBufferLocked(paneID, cfg)
	}

	// Initialize scrollbar (non-overlay, resizes terminal)
	// Callback triggers terminal resize when visibility changes
	a.scrollbar = NewScrollBar(a.vterm, func(visible bool) {
		// Resize is called from outside the lock, so we need to call it unlocked
		go func() {
			a.Resize(a.width, a.height)
			a.requestRefresh()
		}()
	})
	a.scrollbar.SetRefreshCallback(a.requestRefresh)
	a.scrollbar.Resize(rows)

	// Initialize mouse coordinator for selection handling
	// Load config values for auto-scroll
	scrollConfig := AutoScrollConfig{
		EdgeZone:       cfg.GetInt("texelterm.selection", "edge_zone", 2),
		MaxScrollSpeed: cfg.GetInt("texelterm.selection", "max_scroll_speed", 15),
	}
	// Calculate terminal width (accounting for scrollbar)
	termWidth := cols
	if a.scrollbar.IsVisible() {
		termWidth = cols - ScrollBarWidth
	}
	a.mouseCoordinator = NewMouseCoordinator(
		NewVTermAdapter(a.vterm),     // VTermProvider for selection
		NewVTermGridAdapter(a.vterm), // GridProvider for coordinate conversion
		a,                            // MouseWheelHandler
		scrollConfig,
	)
	a.mouseCoordinator.SetSize(termWidth, rows)
	a.mouseCoordinator.SetCallbacks(
		func() { a.vterm.MarkAllDirty() },
		a.requestRefresh,
	)
	a.mouseCoordinator.SetClipboardSetter(a) // Wire up clipboard for standalone mode

	// Load and apply persisted state
	savedState := a.loadStateLocked()
	a.populateFromHistoryLocked(savedState)
	a.applyRestoredStateLocked(savedState)
}

// Note: initializeDisplayBufferLocked was removed as part of DisplayBuffer cleanup.
// MemoryBuffer is now the only scrollback system.

// initializeMemoryBufferLocked sets up the MemoryBuffer system.
// Must be called with a.mu held.
func (a *TexelTerm) initializeMemoryBufferLocked(paneID string, cfg config.Config) {
	historyMemoryLines := cfg.GetInt("texelterm.history", "memory_lines", parser.DefaultMemoryLines)
	historyPersistDir := expandTildePath(cfg.GetString("texelterm.history", "persist_dir", ""))

	if historyPersistDir == "" {
		if homeDir, err := os.UserHomeDir(); err == nil {
			historyPersistDir = filepath.Join(homeDir, ".texelation")
		}
	}

	log.Printf("[TEXELTERM] memoryBufferEnabled=true, paneID=%q, historyPersistDir=%q", paneID, historyPersistDir)

	if paneID != "" {
		scrollbackDir := filepath.Join(historyPersistDir, "scrollback")
		if err := os.MkdirAll(scrollbackDir, 0755); err != nil {
			log.Printf("Failed to create scrollback dir: %v", err)
		}
		// Use a different extension to distinguish from old format
		diskPath := filepath.Join(scrollbackDir, paneID+".hist3")

		err := a.vterm.EnableMemoryBufferWithDisk(diskPath, parser.MemoryBufferOptions{
			MaxLines:      historyMemoryLines,
			EvictionBatch: 1000,
			DiskPath:      diskPath,
			TerminalID:    paneID, // Use paneID as terminal ID for persistent history
		})
		if err != nil {
			log.Printf("[MEMORY_BUFFER] Failed to enable disk-backed buffer: %v", err)
			a.vterm.EnableMemoryBuffer()
		} else {
			log.Printf("[MEMORY_BUFFER] Enabled with disk persistence: %s", diskPath)
		}

		// Initialize search index (Phase 3 - Disk Layer)
		indexPath := filepath.Join(scrollbackDir, paneID+".index.db")
		if idx, err := parser.NewSearchIndex(indexPath); err != nil {
			log.Printf("[SEARCH_INDEX] Failed to initialize: %v", err)
		} else {
			a.searchIndex = idx
			log.Printf("[SEARCH_INDEX] Initialized at %s", indexPath)

			// Wire up the line index callback - called AFTER line is persisted to WAL
			// This ensures search index only has entries for content that exists on disk
			a.vterm.SetOnLineIndexed(func(lineIdx int64, line *parser.LogicalLine, timestamp time.Time, isCommand bool) {
				if line == nil {
					return
				}
				text := parser.ExtractText(line.Cells)
				if text != "" {
					a.searchIndex.IndexLine(lineIdx, timestamp, text, isCommand)
				} else {
					// Line was erased - remove from index to prevent stale matches
					a.searchIndex.DeleteLine(lineIdx)
				}
			})

			// Initialize history navigator (Phase 4 - Disk Layer)
			a.historyNavigator = NewHistoryNavigator(a.vterm, idx, func() {
				// Close callback: return to live edge when navigator closes
				a.vterm.ScrollToLiveEdge()
				// Hide scrollbar and clear search highlights when navigator closes
				if a.scrollbar != nil {
					a.scrollbar.Hide()
					a.scrollbar.ClearSearchResults()
				}
				a.requestRefresh()
			})
			a.historyNavigator.SetRefreshNotifier(a.refreshChan)
			a.historyNavigator.Resize(a.width, a.height) // Initialize size
			log.Printf("[HISTORY_NAV] Initialized with size %dx%d", a.width, a.height)

			// Wire up search results to scrollbar for minimap highlighting
			a.historyNavigator.SetSearchResultsCallback(func(results []parser.SearchResult) {
				if a.scrollbar != nil {
					a.scrollbar.SetSearchResults(results)
					a.requestRefresh()
				}
			})
		}
	} else {
		a.vterm.EnableMemoryBuffer()
		log.Printf("[MEMORY_BUFFER] Enabled with memory-only (no pane ID)")
	}

	// Enable debug logging if TEXELTERM_DEBUG env var is set
	if os.Getenv("TEXELTERM_DEBUG") != "" {
		log.Printf("[DEBUG] TEXELTERM_DEBUG is set - memory buffer debug logging enabled")
	}
}

// enableDebugLogging sets up debug logging for terminal operations.
func (a *TexelTerm) enableDebugLogging() {
	log.Printf("[DEBUG] TEXELTERM_DEBUG is set, opening debug log file")
	debugFile, err := os.OpenFile("/tmp/texelterm-debug.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	log.Printf("[DEBUG] Writing terminal debug to /tmp/texelterm-debug.log")
	fmt.Fprintf(debugFile, "[TEXELTERM] Debug logging initialized\n")
	a.renderDebugLog = func(format string, args ...any) {
		fmt.Fprintf(debugFile, "[RENDER] "+format+"\n", args...)
	}
}

// populateFromHistoryLocked prepares the viewport for history recovery.
// With MemoryBuffer, history is automatically available - this just logs the state.
// Must be called with a.mu held.
func (a *TexelTerm) populateFromHistoryLocked(savedState terminalState) {
	if !a.vterm.IsMemoryBufferEnabled() {
		return
	}

	if a.renderDebugLog != nil {
		a.renderDebugLog("[RECOVERY] savedState.LastPromptLine=%d, historyLen=%d",
			savedState.LastPromptLine, a.vterm.HistoryLength())
	}

	// With MemoryBuffer, history is automatically loaded from disk if available.
	// The scroll offset is restored in applyRestoredStateLocked.
	if a.renderDebugLog != nil {
		if savedState.LastPromptLine >= 0 {
			a.renderDebugLog("[RECOVERY] Saved prompt line=%d, height=%d",
				savedState.LastPromptLine, savedState.LastPromptHeight)
		} else {
			a.renderDebugLog("[RECOVERY] No saved prompt line")
		}
	}
}

// runPtyReaderLoop reads from PTY, parses output, and handles shell exit.
func (a *TexelTerm) runPtyReaderLoop(ptmx *os.File, cmd *exec.Cmd) error {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer ptmx.Close()
		reader := bufio.NewReader(ptmx)

		for {
			r, _, err := reader.ReadRune()
			if err != nil {
				if err != io.EOF {
					log.Printf("Error reading from PTY: %v", err)
				}
				return
			}

			a.mu.Lock()
			inSync := a.vterm.InSynchronizedUpdate
			a.parser.Parse(r)
			syncEnded := inSync && !a.vterm.InSynchronizedUpdate
			a.mu.Unlock()

			if syncEnded {
				a.vterm.MarkAllDirty()
				a.invalidateScrollbar()
				a.requestRefresh()
			} else if !a.vterm.InSynchronizedUpdate {
				if reader.Buffered() == 0 {
					a.invalidateScrollbar()
					a.requestRefresh()
				}
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		log.Printf("[TEXELTERM] Shell exited with: %v", err)
	}
	a.wg.Wait()

	// PTY exited - ask for confirmation before closing pane
	a.mu.Lock()
	a.confirmClose = true
	a.confirmCallback = nil
	a.requestRefresh()
	a.mu.Unlock()

	select {
	case <-a.closeCh:
		return fmt.Errorf("user confirmed close")
	case <-a.restartCh:
		return nil
	case <-a.stop:
		return fmt.Errorf("external stop")
	}
}

func (a *TexelTerm) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	if a.renderDebugLog != nil {
		a.renderDebugLog("App Resize request: %dx%d", cols, rows)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.width = cols
	a.height = rows

	// Calculate terminal width (accounting for scrollbar if visible)
	termWidth := cols
	if a.scrollbar != nil && a.scrollbar.IsVisible() {
		termWidth = cols - ScrollBarWidth
		if termWidth < 1 {
			termWidth = 1
		}
	}

	if a.vterm != nil {
		a.vterm.Resize(termWidth, rows)
	}

	if a.scrollbar != nil {
		a.scrollbar.Resize(rows)
	}

	if a.mouseCoordinator != nil {
		a.mouseCoordinator.SetSize(termWidth, rows)
	}

	if a.historyNavigator != nil {
		a.historyNavigator.Resize(termWidth, rows)
	}

	if a.pty != nil {
		pty.Setsize(a.pty, &pty.Winsize{Rows: uint16(rows), Cols: uint16(termWidth)})
	}
}

func (a *TexelTerm) Stop() {
	a.stopOnce.Do(func() {
		close(a.stop)
		var (
			cmd *exec.Cmd
			pty *os.File
		)
		a.mu.Lock()

		// Save terminal state (scroll position) before closing
		a.saveStateLocked()

		cmd = a.cmd
		pty = a.pty

		// Close memory buffer (flushes to disk if disk-backed)
		if a.vterm != nil {
			if err := a.vterm.CloseMemoryBuffer(); err != nil {
				log.Printf("Error closing memory buffer: %v", err)
			}
		}

		// Close search index (flushes pending writes)
		if a.searchIndex != nil {
			if err := a.searchIndex.Close(); err != nil {
				log.Printf("Error closing search index: %v", err)
			}
			a.searchIndex = nil
		}

		a.cmd = nil
		a.pty = nil
		a.mu.Unlock()

		if pty != nil {
			_ = pty.Close()
		}
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			proc := cmd.Process
			go func() {
				time.Sleep(500 * time.Millisecond)
				proc.Signal(syscall.SIGKILL) // Ignore error; process may already be gone.
			}()
		}
	})
	a.wg.Wait()
}

func (a *TexelTerm) GetTitle() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.title
}

// OnEvent implements texel.Listener to handle theme changes.
func (a *TexelTerm) OnEvent(event texel.Event) {
	if event.Type == texel.EventThemeChanged {
		a.mu.Lock()
		defer a.mu.Unlock()

		// Regenerate the palette with the new theme colors
		a.colorPalette = newDefaultPalette()

		// Force a full redraw
		if a.vterm != nil {
			a.vterm.MarkAllDirty()
		}
		a.requestRefresh()
	}
}

func (a *TexelTerm) respondToColorQuery(code int) {
	if a.pty == nil {
		return
	}
	// Slot 256 for default FG, 257 for default BG
	slot := 256 + (code - 10)
	color := a.colorPalette[slot]
	r, g, b := color.RGB()
	// Scale 8-bit color to 16-bit for response
	responseStr := fmt.Sprintf("\x1b]%d;rgb:%04x/%04x/%04x\a", code, r*257, g*257, b*257)
	if _, err := a.pty.Write([]byte(responseStr)); err != nil {
		log.Printf("[TEXELTERM] Failed to write color query response to PTY: %v", err)
	}
}

// scrollToOffsetAnimated scrolls to the target offset with animation for short distances.
func (a *TexelTerm) scrollToOffsetAnimated(targetOffset int64) {
	if a.vterm == nil {
		return
	}

	currentOffset := a.vterm.ScrollOffset()
	distance := targetOffset - currentOffset
	if distance < 0 {
		distance = -distance
	}

	// Use history navigator's animation settings if available
	maxAnimLines := int64(500) // Default threshold
	if a.historyNavigator != nil {
		maxAnimLines = a.historyNavigator.ScrollAnimMaxLines
	}

	// For short distances, animate line by line
	if maxAnimLines > 0 && distance <= maxAnimLines && distance > 0 {
		a.animateScrollToOffset(currentOffset, targetOffset)
	} else {
		// Jump directly for long distances
		a.vterm.SetScrollOffset(targetOffset)
	}
}

// animateScrollToOffset animates scrolling from current to target offset.
func (a *TexelTerm) animateScrollToOffset(startOffset, targetOffset int64) {
	a.animateScrollToOffsetWithDone(startOffset, targetOffset, nil)
}

// animateScrollToOffsetWithDone animates scrolling and optionally signals completion.
// If done is non-nil, it will be closed when the animation completes.
func (a *TexelTerm) animateScrollToOffsetWithDone(startOffset, targetOffset int64, done chan struct{}) {
	if a.vterm == nil {
		if done != nil {
			close(done)
		}
		return
	}

	distance := targetOffset - startOffset
	if distance < 0 {
		distance = -distance
	}
	if distance == 0 {
		if done != nil {
			close(done)
		}
		return
	}

	// Get animation settings from history navigator or use defaults
	minTime := 100 * time.Millisecond
	maxTime := 500 * time.Millisecond
	maxLines := int64(500)
	frameRate := 60

	if a.historyNavigator != nil {
		minTime = a.historyNavigator.ScrollAnimMinTime
		maxTime = a.historyNavigator.ScrollAnimMaxTime
		maxLines = a.historyNavigator.ScrollAnimMaxLines
		frameRate = a.historyNavigator.ScrollAnimFrameRate
	}

	// Calculate duration based on distance
	durationRange := maxTime - minTime
	distanceRatio := float64(distance) / float64(maxLines)
	if distanceRatio > 1 {
		distanceRatio = 1
	}
	duration := minTime + time.Duration(float64(durationRange)*distanceRatio)

	// Animate in a goroutine
	go func() {
		defer func() {
			if done != nil {
				close(done)
			}
		}()

		startTime := time.Now()
		ticker := time.NewTicker(time.Second / time.Duration(frameRate))
		defer ticker.Stop()

		for range ticker.C {
			elapsed := time.Since(startTime)
			if elapsed >= duration {
				a.vterm.SetScrollOffset(targetOffset)
				a.requestRefresh()
				return
			}

			// Use ease-out cubic easing
			t := float64(elapsed) / float64(duration)
			eased := 1 - (1-t)*(1-t)*(1-t) // ease-out cubic

			currentOffset := startOffset + int64(float64(targetOffset-startOffset)*eased)
			a.vterm.SetScrollOffset(currentOffset)
			a.requestRefresh()
		}
	}()
}

// scrollToOffsetWithResultSelection scrolls to the target offset with animation
// and selects the closest search result after animation completes.
func (a *TexelTerm) scrollToOffsetWithResultSelection(targetOffset int64) {
	if a.vterm == nil {
		return
	}

	currentOffset := a.vterm.ScrollOffset()
	distance := targetOffset - currentOffset
	if distance < 0 {
		distance = -distance
	}

	maxAnimLines := int64(500)
	if a.historyNavigator != nil {
		maxAnimLines = a.historyNavigator.ScrollAnimMaxLines
	}

	// Callback to select result after scroll completes
	selectResult := func() {
		if a.historyNavigator != nil && a.historyNavigator.IsVisible() {
			a.historyNavigator.SelectClosestResultInViewport()
			a.requestRefresh()
		}
	}

	if maxAnimLines > 0 && distance <= maxAnimLines && distance > 0 {
		// Animate and wait for completion in a goroutine
		done := make(chan struct{})
		a.animateScrollToOffsetWithDone(currentOffset, targetOffset, done)
		go func() {
			<-done
			selectResult()
		}()
	} else {
		// Jump directly for long distances
		a.vterm.SetScrollOffset(targetOffset)
		selectResult()
	}

	a.requestRefresh()
}

func (a *TexelTerm) requestRefresh() {
	if a.refreshChan != nil {
		select {
		case a.refreshChan <- true:
		default:
		}
	}
}

// invalidateScrollbar marks the scrollbar minimap cache as stale.
func (a *TexelTerm) invalidateScrollbar() {
	if a.scrollbar != nil {
		a.scrollbar.Invalidate()
	}
}

// If is a simple ternary helper
func If[T any](condition bool, trueVal, falseVal T) T {
	if condition {
		return trueVal
	}
	return falseVal
}

func newDefaultPalette() [258]tcell.Color {
	var p [258]tcell.Color
	tm := theming.ForApp("texelterm")

	// Standard ANSI colors 0-15 (Mapped to Catppuccin Palette)
	p[0] = theme.ResolveColorName("surface1")
	p[1] = theme.ResolveColorName("red")
	p[2] = theme.ResolveColorName("green")
	p[3] = theme.ResolveColorName("yellow")
	p[4] = theme.ResolveColorName("blue")
	p[5] = theme.ResolveColorName("pink")
	p[6] = theme.ResolveColorName("teal")
	p[7] = theme.ResolveColorName("subtext1")
	p[8] = theme.ResolveColorName("surface2")
	p[9] = theme.ResolveColorName("red")
	p[10] = theme.ResolveColorName("green")
	p[11] = theme.ResolveColorName("yellow")
	p[12] = theme.ResolveColorName("blue")
	p[13] = theme.ResolveColorName("pink")
	p[14] = theme.ResolveColorName("teal")
	p[15] = theme.ResolveColorName("text")

	// Fallback for any missing palette colors
	if p[0] == tcell.ColorDefault {
		p[0] = tcell.NewRGBColor(10, 10, 20)
	}
	// ... (simplified fallback, we trust the palette mostly)

	// 6x6x6 color cube (16-231)
	levels := []int32{0, 95, 135, 175, 215, 255}
	i := 16
	for r := 0; r < 6; r++ {
		for g := 0; g < 6; g++ {
			for b := 0; b < 6; b++ {
				p[i] = tcell.NewRGBColor(levels[r], levels[g], levels[b])
				i++
			}
		}
	}

	// Grayscale ramp (232-255)
	for j := 0; j < 24; j++ {
		gray := int32(8 + j*10)
		p[i] = tcell.NewRGBColor(gray, gray, gray)
		i++
	}

	// Default FG (slot 256) and BG (slot 257)
	p[256] = tm.GetSemanticColor("text.primary")
	p[257] = tm.GetSemanticColor("bg.base")
	return p
}

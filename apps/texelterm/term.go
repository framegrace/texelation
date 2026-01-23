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

const (
	// multiClickTimeout is the maximum time between clicks to be considered a multi-click
	multiClickTimeout = 500 * time.Millisecond
)

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
	selection          termSelection
	bracketedPasteMode bool // Tracks if application has enabled bracketed paste

	// Scroll tracking for smooth velocity-based acceleration
	scrollEventTime time.Time // For debouncing duplicate events
	lastScrollTime  time.Time // For velocity tracking
	scrollVelocity  float64   // Accumulated velocity

	// Auto-scroll during selection
	autoScrollActive bool
	autoScrollStop   chan struct{}
	lastMouseY       int
	lastMouseX       int

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
}

var _ texelcore.CloseRequester = (*TexelTerm)(nil)
var _ texelcore.CloseCallbackRequester = (*TexelTerm)(nil)
var _ texelcore.StorageSetter = (*TexelTerm)(nil)

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

	style := tcell.StyleDefault.
		Foreground(fgColor).
		Background(bgColor).
		Bold(pCell.Attr&parser.AttrBold != 0).
		Underline(pCell.Attr&parser.AttrUnderline != 0).
		Reverse(pCell.Attr&parser.AttrReverse != 0)

	return texelcore.Cell{
		Ch:    pCell.Rune,
		Style: style,
	}
}

func (a *TexelTerm) SetRefreshNotifier(refreshChan chan<- bool) {
	a.refreshChan = refreshChan
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
		log.Printf("[TEXELTERM] Restored scroll offset: %d, displayBuf enabled=%v", state.ScrollOffset, a.vterm.IsDisplayBufferEnabled())
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
	cols := len(vtermGrid[0])

	if len(a.buf) != rows || (rows > 0 && len(a.buf[0]) != cols) {
		a.buf = make([][]texelcore.Cell, rows)
		for y := range a.buf {
			a.buf[y] = make([]texelcore.Cell, cols)
		}
		a.vterm.MarkAllDirty()
	}

	cursorX, cursorY := a.vterm.Cursor()
	// Only show cursor if it's visible AND we're at the live edge (not scrolled into history)
	cursorVisible := a.vterm.CursorVisible() && a.vterm.AtLiveEdge()
	dirtyLines, allDirty := a.vterm.DirtyLines()

	a.logRenderDebug(vtermGrid, cursorX, cursorY, dirtyLines, allDirty)

	renderLine := func(y int) {
		for x := 0; x < cols; x++ {
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

	if a.confirmClose {
		a.drawConfirmation(a.buf)
	}

	return a.buf
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


func (a *TexelTerm) MouseWheelEnabled() bool {
	return true
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

	// If selection is active, update it based on the new scroll position
	if a.selection.active {
		line, col := a.screenToHistoryPosition(a.lastMouseX, a.lastMouseY)
		a.selection.currentLine = line
		a.selection.currentCol = col
		a.vterm.MarkAllDirty()
	}

	a.mu.Unlock()
	a.requestRefresh()
}

// manageAutoScrollState checks if the mouse is near the top/bottom edge during selection
// and starts/stops auto-scroll accordingly. Must be called with a.mu locked.
func (a *TexelTerm) manageAutoScrollState(mouseY int) {
	if !a.selection.active {
		a.stopAutoScrollLocked()
		return
	}

	// Read config for edge zone threshold.
	cfg := config.App("texelterm")
	edgeZone := cfg.GetInt("texelterm.selection", "edge_zone", 2)
	if edgeZone <= 0 {
		edgeZone = 2
	}

	// Check if mouse is in the edge zone
	nearTop := mouseY < edgeZone
	nearBottom := mouseY >= a.height-edgeZone

	if nearTop || nearBottom {
		// Start auto-scroll if not already active
		if !a.autoScrollActive {
			a.startAutoScrollLocked()
		}
	} else {
		// Stop auto-scroll if active
		a.stopAutoScrollLocked()
	}
}

// startAutoScrollLocked starts the auto-scroll goroutine. Must be called with a.mu locked.
func (a *TexelTerm) startAutoScrollLocked() {
	if a.autoScrollActive {
		return
	}

	a.autoScrollActive = true
	a.autoScrollStop = make(chan struct{})

	// Start auto-scroll goroutine
	a.wg.Add(1)
	go a.autoScrollLoop()
}

// stopAutoScrollLocked stops the auto-scroll goroutine. Must be called with a.mu locked.
func (a *TexelTerm) stopAutoScrollLocked() {
	if !a.autoScrollActive {
		return
	}

	a.autoScrollActive = false
	close(a.autoScrollStop)
	a.autoScrollStop = nil
}

// calculateAutoScrollSpeed computes scroll velocity based on mouse distance from edge.
// Returns speed in lines/second (negative=up, positive=down) and whether mouse is in edge zone.
func (a *TexelTerm) calculateAutoScrollSpeed(mouseY int, elapsed float64) (speed float64, inEdgeZone bool) {
	cfg := config.App("texelterm")
	edgeZone := cfg.GetInt("texelterm.selection", "edge_zone", 2)
	maxSpeed := cfg.GetInt("texelterm.selection", "max_scroll_speed", 15)
	if edgeZone <= 0 {
		edgeZone = 2
	}
	if maxSpeed <= 0 {
		maxSpeed = 15
	}

	// Time-based acceleration (ramps up over 3 seconds)
	timeMultiplier := 1.0 + (elapsed * 2.0)
	if timeMultiplier > 8.0 {
		timeMultiplier = 8.0
	}

	if mouseY < edgeZone {
		// Near top - scroll up (negative)
		distance := float64(edgeZone - mouseY)
		speed = -(distance * float64(maxSpeed) / float64(edgeZone)) * timeMultiplier
		return speed, true
	}
	if mouseY >= a.height-edgeZone {
		// Near bottom - scroll down (positive)
		distance := float64(mouseY - (a.height - edgeZone) + 1)
		speed = (distance * float64(maxSpeed) / float64(edgeZone)) * timeMultiplier
		return speed, true
	}
	return 0, false
}

// autoScrollLoop runs in a goroutine and performs auto-scrolling during selection drag.
func (a *TexelTerm) autoScrollLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	stopChan := a.autoScrollStop
	var accumulator float64
	startTime := time.Now()

	for {
		select {
		case <-stopChan:
			return
		case <-a.stop:
			return
		case <-ticker.C:
			a.mu.Lock()
			if !a.selection.active || a.vterm == nil {
				a.mu.Unlock()
				return
			}

			mouseY := a.lastMouseY
			mouseX := a.lastMouseX
			elapsed := time.Since(startTime).Seconds()

			speed, inEdgeZone := a.calculateAutoScrollSpeed(mouseY, elapsed)
			if !inEdgeZone {
				accumulator = 0
				a.mu.Unlock()
				continue
			}

			// Convert lines/sec to lines/tick (50ms = 20 ticks/sec)
			accumulator += speed / 20.0

			var scrollLines int
			if accumulator >= 1.0 || accumulator <= -1.0 {
				scrollLines = int(accumulator)
				accumulator -= float64(scrollLines)
			}

			if scrollLines != 0 {
				a.vterm.Scroll(scrollLines)
				a.saveStateLocked()

				// Update selection endpoint
				line, col := a.screenToHistoryPosition(mouseX, mouseY)
				if a.selection.active {
					a.selection.currentLine = line
					a.selection.currentCol = col
					a.vterm.MarkAllDirty()
				}
			}

			a.mu.Unlock()
			if scrollLines != 0 {
				a.requestRefresh()
			}
		}
	}
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

func (a *TexelTerm) runShell() error {
	a.mu.Lock()
	cols, rows := a.width, a.height
	isRestart := a.vterm != nil
	paneID := a.paneID
	a.mu.Unlock()

	log.Printf("[TEXELTERM] runShell starting: cols=%d, rows=%d, restart=%v", cols, rows, isRestart)

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
		a.initializeDisplayBufferLocked(paneID, cfg)
	}

	// Load and apply persisted state
	savedState := a.loadStateLocked()
	a.populateFromHistoryLocked(savedState)
	a.applyRestoredStateLocked(savedState)
}

// initializeDisplayBufferLocked sets up the display buffer with optional disk persistence.
// Must be called with a.mu held.
func (a *TexelTerm) initializeDisplayBufferLocked(paneID string, cfg config.Config) {
	historyMemoryLines := cfg.GetInt("texelterm.history", "memory_lines", parser.DefaultMemoryLines)
	historyPersistDir := cfg.GetString("texelterm.history", "persist_dir", "")
	if historyPersistDir == "" {
		if homeDir, err := os.UserHomeDir(); err == nil {
			historyPersistDir = filepath.Join(homeDir, ".texelation")
		}
	}

	log.Printf("[TEXELTERM] displayBufferEnabled=true, paneID=%q", paneID)

	if paneID != "" {
		scrollbackDir := filepath.Join(historyPersistDir, "scrollback")
		if err := os.MkdirAll(scrollbackDir, 0755); err != nil {
			log.Printf("Failed to create scrollback dir: %v", err)
		}
		diskPath := filepath.Join(scrollbackDir, paneID+".hist2")

		err := a.vterm.EnableDisplayBufferWithDisk(diskPath, parser.DisplayBufferOptions{
			MaxMemoryLines: historyMemoryLines,
			MarginAbove:    200,
			MarginBelow:    50,
		})
		if err != nil {
			log.Printf("[DISPLAY_BUFFER] Failed to enable disk-backed buffer: %v", err)
			a.vterm.EnableDisplayBuffer()
		} else {
			log.Printf("[DISPLAY_BUFFER] Enabled with disk persistence: %s", diskPath)
		}
	} else {
		a.vterm.EnableDisplayBuffer()
		log.Printf("[DISPLAY_BUFFER] Enabled with memory-only (no pane ID)")
	}

	// Enable debug logging if TEXELTERM_DEBUG env var is set
	if os.Getenv("TEXELTERM_DEBUG") != "" {
		a.enableDebugLogging()
	}
}

// enableDebugLogging sets up debug logging for display buffer operations.
func (a *TexelTerm) enableDebugLogging() {
	log.Printf("[DEBUG] TEXELTERM_DEBUG is set, opening debug log file")
	debugFile, err := os.OpenFile("/tmp/texelterm-debug.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	log.Printf("[DEBUG] Writing display buffer debug to /tmp/texelterm-debug.log")
	fmt.Fprintf(debugFile, "[DB] Debug logging initialized\n")
	a.vterm.SetDisplayBufferDebugLog(func(format string, args ...any) {
		fmt.Fprintf(debugFile, "[DB] "+format+"\n", args...)
	})
	a.renderDebugLog = func(format string, args ...any) {
		fmt.Fprintf(debugFile, "[RENDER] "+format+"\n", args...)
	}
}

// populateFromHistoryLocked populates the viewport from history for session recovery.
// Must be called with a.mu held.
func (a *TexelTerm) populateFromHistoryLocked(savedState terminalState) {
	if !a.vterm.IsDisplayBufferEnabled() {
		return
	}

	if a.renderDebugLog != nil {
		a.renderDebugLog("[RECOVERY] savedState.LastPromptLine=%d, historyLen=%d",
			savedState.LastPromptLine, a.vterm.HistoryLength())
	}

	if savedState.LastPromptLine >= 0 {
		if a.renderDebugLog != nil {
			a.renderDebugLog("[RECOVERY] Using seamless recovery with lastPromptLine=%d, promptHeight=%d",
				savedState.LastPromptLine, savedState.LastPromptHeight)
		}
		a.vterm.PopulateViewportFromHistoryToPrompt(savedState.LastPromptLine, savedState.LastPromptHeight)
	} else {
		if a.renderDebugLog != nil {
			a.renderDebugLog("[RECOVERY] Fallback: no valid lastPromptLine")
		}
		a.vterm.PopulateViewportFromHistory()
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
				a.requestRefresh()
			} else if !a.vterm.InSynchronizedUpdate {
				if reader.Buffered() == 0 {
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

func (a *TexelTerm) screenToHistoryPosition(x, y int) (int, int) {
	if a.vterm == nil {
		return 0, 0
	}
	top := a.vterm.VisibleTop()
	line := top + y
	historyLen := a.vterm.HistoryLength()
	if historyLen <= 0 {
		line = 0
	} else {
		if line < 0 {
			line = 0
		} else if line >= historyLen {
			line = historyLen - 1
		}
	}
	col := x
	if col < 0 {
		col = 0
	}
	if historyLen > 0 {
		if cells := a.vterm.HistoryLineCopy(line); cells != nil {
			if col > len(cells) {
				col = len(cells)
			}
		}
	}
	return line, col
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

	if a.vterm != nil {
		a.vterm.Resize(cols, rows)
	}

	if a.pty != nil {
		pty.Setsize(a.pty, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
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

		// Close display buffer (flushes to disk if disk-backed)
		if a.vterm != nil {
			if err := a.vterm.CloseDisplayBuffer(); err != nil {
				log.Printf("Error closing display buffer: %v", err)
			}
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

func (a *TexelTerm) requestRefresh() {
	if a.refreshChan != nil {
		select {
		case a.refreshChan <- true:
		default:
		}
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

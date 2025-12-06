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
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"texelation/apps/texelterm/parser"
	"texelation/texel"
	"texelation/texel/cards"
	"texelation/texel/theme"

	"github.com/creack/pty"
	"github.com/gdamore/tcell/v2"
)

const (
	// multiClickTimeout is the maximum time between clicks to be considered a multi-click
	multiClickTimeout = 500 * time.Millisecond
)

type TexelTerm struct {
	title                string
	command              string
	width                int
	height               int
	cmd                  *exec.Cmd
	pty                  *os.File
	vterm                *parser.VTerm
	parser               *parser.Parser
	historyManager       *parser.HistoryManager
	mu                   sync.Mutex
	stop                 chan struct{}
	stopOnce             sync.Once
	refreshChan          chan<- bool
	wg                   sync.WaitGroup
	buf                  [][]texel.Cell
	colorPalette         [258]tcell.Color
	controlBus           texel.ControlBus
	selection            termSelection
	bracketedPasteMode   bool // Tracks if application has enabled bracketed paste

	// Scroll tracking for smooth velocity-based acceleration
	scrollEventTime time.Time // For debouncing duplicate events
	lastScrollTime  time.Time // For velocity tracking
	scrollVelocity  float64   // Accumulated velocity

	// Auto-scroll during selection
	autoScrollActive bool
	autoScrollStop   chan struct{}
	lastMouseY       int
	lastMouseX       int

	confirmClose    bool
	confirmCallback func()
	closeCh         chan struct{}
}

var _ texel.CloseRequester = (*TexelTerm)(nil)

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

func New(title, command string) texel.App {
	term := &TexelTerm{
		title:        title,
		command:      command,
		width:        80,
		height:       24,
		stop:         make(chan struct{}),
		colorPalette: newDefaultPalette(),
		closeCh:      make(chan struct{}),
	}

	wrapped := cards.WrapApp(term)
	pipe := cards.NewPipeline(nil, wrapped)
	term.AttachControlBus(pipe.ControlBus())
	return pipe
}

func (a *TexelTerm) RequestClose() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.confirmClose = true
	a.confirmCallback = func() {
		close(a.closeCh)
	}
	a.requestRefresh()
	return false
}

func (a *TexelTerm) drawConfirmation(buf [][]texel.Cell) {
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
	if x < 0 { x = 0 }
	if y < 0 { y = 0 }
	if boxW > width { boxW = width }
	if boxH > height { boxH = height }

	style := tcell.StyleDefault.Background(tcell.ColorDarkRed).Foreground(tcell.ColorWhite)
	borderStyle := tcell.StyleDefault.Background(tcell.ColorDarkRed).Foreground(tcell.ColorWhite)

	// Draw box
	for r := 0; r < boxH; r++ {
		for c := 0; c < boxW; c++ {
			if y+r < height && x+c < width {
				buf[y+r][x+c] = texel.Cell{Ch: ' ', Style: style}
			}
		}
	}

	// Borders
	for c := 0; c < boxW; c++ {
		buf[y][x+c] = texel.Cell{Ch: tcell.RuneHLine, Style: borderStyle}
		buf[y+boxH-1][x+c] = texel.Cell{Ch: tcell.RuneHLine, Style: borderStyle}
	}
	for r := 0; r < boxH; r++ {
		buf[y+r][x] = texel.Cell{Ch: tcell.RuneVLine, Style: borderStyle}
		buf[y+r][x+boxW-1] = texel.Cell{Ch: tcell.RuneVLine, Style: borderStyle}
	}
	buf[y][x] = texel.Cell{Ch: tcell.RuneULCorner, Style: borderStyle}
	buf[y][x+boxW-1] = texel.Cell{Ch: tcell.RuneURCorner, Style: borderStyle}
	buf[y+boxH-1][x] = texel.Cell{Ch: tcell.RuneLLCorner, Style: borderStyle}
	buf[y+boxH-1][x+boxW-1] = texel.Cell{Ch: tcell.RuneLRCorner, Style: borderStyle}

	// Text
	msg := "Close Terminal? (y/n)"
	textX := x + (boxW-len(msg))/2
	textY := y + 2
	for i, r := range msg {
		if textX+i < width {
			buf[textY][textX+i] = texel.Cell{Ch: r, Style: style.Bold(true)}
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

func (a *TexelTerm) applyParserStyle(pCell parser.Cell) texel.Cell {
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

	return texel.Cell{
		Ch:    pCell.Rune,
		Style: style,
	}
}

func (a *TexelTerm) SetRefreshNotifier(refreshChan chan<- bool) {
	a.refreshChan = refreshChan
}

func (a *TexelTerm) AttachControlBus(bus texel.ControlBus) {
	a.mu.Lock()
	a.controlBus = bus
	a.mu.Unlock()
}

func colorToHex(c tcell.Color) string {
	trueColor := c.TrueColor()
	if !trueColor.Valid() {
		return "#000000"
	}
	r, g, b := trueColor.RGB()
	return fmt.Sprintf("#%02X%02X%02X", r&0xFF, g&0xFF, b&0xFF)
}

func (a *TexelTerm) Render() [][]texel.Cell {
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
		a.buf = make([][]texel.Cell, rows)
		for y := range a.buf {
			a.buf[y] = make([]texel.Cell, cols)
		}
		a.vterm.MarkAllDirty()
	}

	cursorX, cursorY := a.vterm.Cursor()
	cursorVisible := a.vterm.CursorVisible()
	dirtyLines, allDirty := a.vterm.GetDirtyLines()

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

func (a *TexelTerm) HandleKey(ev *tcell.EventKey) {
	a.mu.Lock()
	if a.confirmClose {
		if ev.Key() == tcell.KeyRune {
			r := ev.Rune()
			if r == 'y' || r == 'Y' {
				if a.confirmCallback != nil {
					a.mu.Unlock()
					a.confirmCallback()
					return // Callback handles close
				}
				// Internal close (PTY exit)
				close(a.closeCh)
			} else if r == 'n' || r == 'N' {
				a.confirmClose = false
				a.requestRefresh()
			}
		}
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()

	if a.pty == nil {
		return
	}

	a.mu.Lock()
	appMode := a.vterm.AppCursorKeys()
	a.mu.Unlock()

	key := ev.Key()

	if ev.Modifiers()&tcell.ModAlt != 0 {
		handled := true
		switch key {
		case tcell.KeyPgDn:
			a.mu.Lock()
			a.vterm.Scroll(a.height)
			a.mu.Unlock()
		case tcell.KeyPgUp:
			a.mu.Lock()
			a.vterm.Scroll(-a.height)
			a.mu.Unlock()
		case tcell.KeyDown:
			a.mu.Lock()
			a.vterm.Scroll(1)
			a.mu.Unlock()
		case tcell.KeyUp:
			a.mu.Lock()
			a.vterm.Scroll(-1)
			a.mu.Unlock()
		default:
			handled = false
		}
		if handled {
			if a.refreshChan != nil {
				select {
				case a.refreshChan <- true:
				default:
				}
			}
			return
		}
	}

	var keyBytes []byte
	switch key {
	case tcell.KeyUp:
		keyBytes = []byte(If(appMode, "\x1bOA", "\x1b[A"))
	case tcell.KeyDown:
		keyBytes = []byte(If(appMode, "\x1bOB", "\x1b[B"))
	case tcell.KeyRight:
		keyBytes = []byte(If(appMode, "\x1bOC", "\x1b[C"))
	case tcell.KeyLeft:
		keyBytes = []byte(If(appMode, "\x1bOD", "\x1b[D"))
	case tcell.KeyHome:
		keyBytes = []byte("\x1b[H")
	case tcell.KeyEnd:
		keyBytes = []byte("\x1b[F")
	case tcell.KeyInsert:
		keyBytes = []byte("\x1b[2~")
	case tcell.KeyDelete:
		keyBytes = []byte("\x1b[3~")
	case tcell.KeyPgUp:
		keyBytes = []byte("\x1b[5~")
	case tcell.KeyPgDn:
		keyBytes = []byte("\x1b[6~")
	case tcell.KeyF1:
		keyBytes = []byte("\x1bOP")
	case tcell.KeyF2:
		keyBytes = []byte("\x1bOQ")
	case tcell.KeyF3:
		keyBytes = []byte("\x1bOR")
	case tcell.KeyF4:
		keyBytes = []byte("\x1bOS")
	case tcell.KeyEnter:
		keyBytes = []byte("\r")
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		keyBytes = []byte{0x7F}
	case tcell.KeyTab:
		keyBytes = []byte("\t")
	case tcell.KeyEsc:
		keyBytes = []byte("\x1b")
	default:
		keyBytes = []byte(string(ev.Rune()))
	}

	if keyBytes != nil {
		a.pty.Write(keyBytes)
	}
}

func (a *TexelTerm) HandlePaste(data []byte) {
	if a.pty == nil || len(data) == 0 {
		return
	}

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

// isWordChar determines if a rune is part of a word (alphanumeric, underscore, or dash).
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '_' || r == '-'
}

// selectWordAtPositionLocked selects the word at the given position.
func (a *TexelTerm) selectWordAtPositionLocked(line, col int) {
	cells := a.vterm.HistoryLineCopy(line)
	if cells == nil || len(cells) == 0 {
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
	if cells == nil || len(cells) < 2 {
		return 0
	}

	// Scan from start: count consecutive non-alphanumeric characters
	for i := 0; i < len(cells); i++ {
		r := cells[i].Rune
		// Check if this is a non-alphanumeric character (potential prompt char)
		if (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			if r == ' ' && i > 0 {
				// Found space after special chars - this is the prompt end
				return i + 1
			}
			// Continue scanning special chars
			continue
		} else {
			// Hit alphanumeric - if we haven't found a space yet, no prompt
			break
		}
	}
	return 0
}

// selectLineAtPositionLocked selects the entire logical line at the given position,
// following wrapped lines to capture the complete command/output.
func (a *TexelTerm) selectLineAtPositionLocked(line int) {
	historyLen := a.vterm.HistoryLength()
	if historyLen == 0 {
		return
	}

	// Find the start of the logical line by going backwards
	startLine := line
	for startLine > 0 {
		prevLine := a.vterm.HistoryLineCopy(startLine - 1)
		if prevLine == nil || len(prevLine) == 0 {
			break
		}
		// Check if the previous line wraps (continues to our line)
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
		if currentLine == nil || len(currentLine) == 0 {
			break
		}
		// Check if the current line wraps (continues on next line)
		if currentLine[len(currentLine)-1].Wrapped {
			endLine++
		} else {
			break
		}
	}

	// Determine start column - skip prompt if selecting the current input line
	startCol := 0

	// First try OSC 133 shell integration if available
	if a.vterm.InputActive && startLine == a.vterm.InputStartLine {
		// Use OSC 133 input start position to skip the prompt
		startCol = a.vterm.InputStartCol
	} else {
		// Fallback: detect prompt pattern by scanning the line
		startLineCells := a.vterm.HistoryLineCopy(startLine)
		startCol = detectPromptEnd(startLineCells)
	}

	// Set selection range
	a.selection.anchorLine = startLine
	a.selection.anchorCol = startCol

	// Find the end column on the last line (excluding trailing spaces)
	endCells := a.vterm.HistoryLineCopy(endLine)
	endCol := 0
	if endCells != nil && len(endCells) > 0 {
		endCol = len(endCells) - 1
	}

	a.selection.currentLine = endLine
	a.selection.currentCol = endCol
}

// SelectionStart implements texel.SelectionHandler.
func (a *TexelTerm) SelectionStart(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vterm == nil {
		return false
	}
	line, col := a.resolveSelectionPositionLocked(x, y)

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

// SelectionUpdate implements texel.SelectionHandler.
func (a *TexelTerm) SelectionUpdate(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vterm == nil || !a.selection.active {
		return
	}

	// Save mouse position for auto-scroll
	a.lastMouseX = x
	a.lastMouseY = y

	line, col := a.resolveSelectionPositionLocked(x, y)
	if !a.selection.active {
		return
	}
	if line == a.selection.currentLine && col == a.selection.currentCol {
		// Position hasn't changed, but check for edge-based auto-scroll
		a.updateAutoScrollLocked(y)
		return
	}
	a.selection.currentLine = line
	a.selection.currentCol = col
	a.vterm.MarkAllDirty()
	a.requestRefresh()

	// Check if we need to start/stop auto-scroll based on mouse position
	a.updateAutoScrollLocked(y)
}

// SelectionFinish implements texel.SelectionHandler.
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
		line, col := a.resolveSelectionPositionLocked(x, y)
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

// SelectionCancel implements texel.SelectionHandler.
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

	// Read scroll configuration from theme
	cfg := theme.Get()
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
		// Smooth velocity-based acceleration - read parameters from config
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

	// If selection is active, update it based on the new scroll position
	if a.selection.active {
		line, col := a.resolveSelectionPositionLocked(a.lastMouseX, a.lastMouseY)
		a.selection.currentLine = line
		a.selection.currentCol = col
		a.vterm.MarkAllDirty()
	}

	a.mu.Unlock()
	a.requestRefresh()
}

// updateAutoScrollLocked checks if the mouse is near the top/bottom edge during selection
// and starts/stops auto-scroll accordingly. Must be called with a.mu locked.
func (a *TexelTerm) updateAutoScrollLocked(mouseY int) {
	if !a.selection.active {
		a.stopAutoScrollLocked()
		return
	}

	// Read config for edge zone threshold
	cfg := theme.Get()
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

// autoScrollLoop runs in a goroutine and performs auto-scrolling based on mouse position.
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

			// Read config
			cfg := theme.Get()
			edgeZone := cfg.GetInt("texelterm.selection", "edge_zone", 2)
			maxSpeed := cfg.GetInt("texelterm.selection", "max_scroll_speed", 15)
			if edgeZone <= 0 {
				edgeZone = 2
			}
			if maxSpeed <= 0 {
				maxSpeed = 15
			}

			mouseY := a.lastMouseY
			mouseX := a.lastMouseX

			// Calculate scroll amount based on distance from edge and time
			var scrollLines int
			var speedLinesPerSec float64

			// Ramp up speed over time (max 3 seconds for full multiplier)
			elapsed := time.Since(startTime).Seconds()
			timeMultiplier := 1.0 + (elapsed * 2.0) // 1x -> 7x over 3s
			if timeMultiplier > 8.0 {
				timeMultiplier = 8.0
			}

			if mouseY < edgeZone {
				// Near top - scroll up (negative)
				distance := float64(edgeZone - mouseY)
				// Scale speed based on distance. At distance == edgeZone, we reach maxSpeed.
				speedLinesPerSec = -(distance * float64(maxSpeed) / float64(edgeZone))
			} else if mouseY >= a.height-edgeZone {
				// Near bottom - scroll down (positive)
				distance := float64(mouseY - (a.height - edgeZone) + 1)
				speedLinesPerSec = distance * float64(maxSpeed) / float64(edgeZone)
			} else {
				// Not in edge zone
				accumulator = 0
				// Reset start time if we stop scrolling (though updateAutoScrollLocked usually kills the loop)
				a.mu.Unlock()
				continue
			}

			// Apply time multiplier
			speedLinesPerSec *= timeMultiplier

			// Convert lines/sec to lines/tick (50ms = 20 ticks/sec)
			accumulator += speedLinesPerSec / 20.0

			if accumulator >= 1.0 || accumulator <= -1.0 {
				scrollLines = int(accumulator)
				accumulator -= float64(scrollLines)
			}

			if scrollLines != 0 {
				// Perform scroll
				a.vterm.Scroll(scrollLines)

				// Update selection endpoint to current mouse position
				line, col := a.resolveSelectionPositionLocked(mouseX, mouseY)
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

	a.mu.Lock()
	cols, rows := a.width, a.height
	a.mu.Unlock()

	cmd := exec.Command(a.command)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return fmt.Errorf("failed to start pty: %w", err)
	}
	a.pty = ptmx
	a.cmd = cmd

	a.mu.Lock()
	// Read wrap/reflow and history configuration from theme
	cfg := theme.Get()
	wrapEnabled := cfg.GetBool("texelterm", "wrap_enabled", true)
	reflowEnabled := cfg.GetBool("texelterm", "reflow_enabled", true)

	// Create history configuration
	histCfg := parser.DefaultHistoryConfig()
	histCfg.MemoryLines = cfg.GetInt("texelterm.history", "memory_lines", parser.DefaultMemoryLines)
	histCfg.PersistEnabled = cfg.GetBool("texelterm.history", "persist_enabled", true)
	if persistDir := cfg.GetString("texelterm.history", "persist_dir", ""); persistDir != "" {
		histCfg.PersistDir = persistDir
	}
	histCfg.Compress = cfg.GetBool("texelterm.history", "compress", true)
	histCfg.Encrypt = cfg.GetBool("texelterm.history", "encrypt", false)

	// Get current working directory
	workingDir, _ := os.Getwd()

	// Create history manager
	hm, err := parser.NewHistoryManager(histCfg, a.command, workingDir)
	if err != nil {
		log.Printf("Failed to create history manager: %v (continuing without persistence)", err)
		hm = nil
	}
	a.historyManager = hm

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
				a.pty.Write(b)
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
			// Note: bool writes are atomic, no lock needed for simple assignment
			a.bracketedPasteMode = enabled
		}),
		parser.WithWrap(wrapEnabled),
		parser.WithReflow(reflowEnabled),
		parser.WithHistoryManager(hm),
	)
	a.parser = parser.NewParser(a.vterm)
	a.mu.Unlock()

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

			if r == '' {
			// Skip BEL character (visual bell not implemented)
			continue
			}

			a.mu.Lock()
			inSync := a.vterm.InSynchronizedUpdate
			a.parser.Parse(r)
			// Check if the sync state *ended* after this rune
			syncEnded := inSync && !a.vterm.InSynchronizedUpdate
			a.mu.Unlock()

			if syncEnded {
				a.vterm.MarkAllDirty()
				a.requestRefresh()
			} else if !a.vterm.InSynchronizedUpdate {
				a.requestRefresh()
			}
		}
	}()

	err = cmd.Wait()
	a.wg.Wait()
	
	// PTY exited. Ask for confirmation before closing pane.
	a.mu.Lock()
	a.confirmClose = true
	a.confirmCallback = nil // Internal close
	a.requestRefresh()
	a.mu.Unlock()
	
	select {
	case <-a.closeCh:
		return err
	case <-a.stop:
		return err
	}
}

func (a *TexelTerm) resolveSelectionPositionLocked(x, y int) (int, int) {
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

func (a *TexelTerm) selectionRangeLocked() (int, int, int, int, bool) {
	if !a.selection.active {
		return 0, 0, 0, 0, false
	}
	startLine := a.selection.anchorLine
	startCol := a.selection.anchorCol
	endLine := a.selection.currentLine
	endCol := a.selection.currentCol
	if startLine > endLine || (startLine == endLine && startCol > endCol) {
		startLine, endLine = endLine, startLine
		startCol, endCol = endCol, startCol
	}
	if startLine == endLine && startCol == endCol {
		return 0, 0, 0, 0, false
	}
	return startLine, startCol, endLine, endCol + 1, true
}

func (a *TexelTerm) buildSelectionTextLocked() string {
	if a.vterm == nil {
		return ""
	}
	startLine, startCol, endLine, endColExclusive, ok := a.selectionRangeLocked()
	if !ok {
		return ""
	}
	lines := make([]string, 0, endLine-startLine+1)
	for line := startLine; line <= endLine; line++ {
		cells := a.vterm.HistoryLineCopy(line)
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

func (a *TexelTerm) applySelectionHighlightLocked(buf [][]texel.Cell) {
	if a.vterm == nil || !a.selection.rendered || len(buf) == 0 {
		return
	}
	startLine, startCol, endLine, endColExclusive, ok := a.selectionRangeLocked()
	if !ok {
		return
	}
	top := a.vterm.VisibleTop()
	cfg := theme.Get()
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

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func (a *TexelTerm) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
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
			hm  *parser.HistoryManager
		)
		a.mu.Lock()
		cmd = a.cmd
		pty = a.pty
		hm = a.historyManager
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

		// Close history manager
		if hm != nil {
			if err := hm.Close(); err != nil {
				log.Printf("Error closing history manager: %v", err)
			}
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

func (a *TexelTerm) respondToColorQuery(code int) {	if a.pty == nil {
		return
	}
	// Slot 256 for default FG, 257 for default BG
	slot := 256 + (code - 10)
	color := a.colorPalette[slot]
	r, g, b := color.RGB()
	// Scale 8-bit color to 16-bit for response
	responseStr := fmt.Sprintf("\x1b]%d;rgb:%04x/%04x/%04x\a", code, r*257, g*257, b*257)
	a.pty.Write([]byte(responseStr))
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
	tm := theme.Get()

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
	if p[0] == tcell.ColorDefault { p[0] = tcell.NewRGBColor(10, 10, 20) }
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

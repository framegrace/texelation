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
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"texelation/apps/texelterm/parser"
	"texelation/texel"
	"texelation/texel/theme"

	"github.com/creack/pty"
	"github.com/gdamore/tcell/v2"
)

type TexelTerm struct {
	title        string
	command      string
	width        int
	height       int
	cmd          *exec.Cmd
	pty          *os.File
	vterm        *parser.VTerm
	parser       *parser.Parser
	mu           sync.Mutex
	stop         chan struct{}
	stopOnce     sync.Once
	refreshChan  chan<- bool
	wg           sync.WaitGroup
	buf          [][]texel.Cell
	colorPalette [258]tcell.Color
	selection    termSelection

	// OSC133 / prompt integration: tracks where the current shell prompt and
	// input line begin so visual wrapping can respect the prompt prefix.
	promptActive    bool
	promptLineIdx   int
	inputStartCol   int
	inputStartKnown bool
}

type termSelection struct {
	active      bool
	anchorLine  int
	anchorCol   int
	currentLine int
	currentCol  int
}

func New(title, command string) texel.App {
	term := &TexelTerm{
		title:        title,
		command:      command,
		width:        80,
		height:       24,
		stop:         make(chan struct{}),
		colorPalette: newDefaultPalette(),
	}

	return term
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

func colorToHex(c tcell.Color) string {
	trueColor := c.TrueColor()
	if !trueColor.Valid() {
		return "#000000"
	}
	r, g, b := trueColor.RGB()
	return fmt.Sprintf("#%02X%02X%02X", r&0xFF, g&0xFF, b&0xFF)
}

func (a *TexelTerm) HandleMessage(msg texel.Message) {}

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

	renderLine := func(y int) {
		for x := 0; x < cols; x++ {
			parserCell := vtermGrid[y][x]
			a.buf[y][x] = a.applyParserStyle(parserCell)
			if cursorVisible && x == cursorX && y == cursorY {
				a.buf[y][x].Style = a.buf[y][x].Style.Reverse(true)
			}
		}
	}

	// For simplicity and to keep visual wrapping logic deterministic, redraw
	// all rows each frame rather than relying on vterm's dirty-line tracking.
	for y := 0; y < rows; y++ {
		renderLine(y)
	}

	a.vterm.ClearDirty()
	// Visual-only reflow of long logical line at the cursor row.
	a.applyVisualWrapLocked(cursorX, cursorY, cols, rows)

	// Optional debug dump of the rendered lines for wrap investigation.
	if os.Getenv("TEXEL_WRAP_DEBUG") == "1" {
		lines := make([]string, len(a.buf))
		for i := range a.buf {
			lines[i] = wrapRowToString(a.buf[i])
		}
		log.Printf("TEXEL_WRAP_DEBUG rows=%d cols=%d cursor=(%d,%d) lines=%q", rows, cols, cursorX, cursorY, lines)
	}

	a.applySelectionHighlightLocked(a.buf)
	return a.buf
}

func (a *TexelTerm) HandleKey(ev *tcell.EventKey) {
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

// SelectionStart implements texel.SelectionHandler.
func (a *TexelTerm) SelectionStart(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vterm == nil {
		return false
	}
	line, col := a.resolveSelectionPositionLocked(x, y)
	a.selection = termSelection{
		active:      true,
		anchorLine:  line,
		anchorCol:   col,
		currentLine: line,
		currentCol:  col,
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
	line, col := a.resolveSelectionPositionLocked(x, y)
	if !a.selection.active {
		return
	}
	if line == a.selection.currentLine && col == a.selection.currentCol {
		return
	}
	a.selection.currentLine = line
	a.selection.currentCol = col
	a.vterm.MarkAllDirty()
	a.requestRefresh()
}

// SelectionFinish implements texel.SelectionHandler.
func (a *TexelTerm) SelectionFinish(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) (string, []byte, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vterm == nil || !a.selection.active {
		return "", nil, false
	}
	line, col := a.resolveSelectionPositionLocked(x, y)
	a.selection.currentLine = line
	a.selection.currentCol = col
	text := a.buildSelectionTextLocked()
	a.selection = termSelection{}
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
	if !a.selection.active {
		return
	}
	a.selection = termSelection{}
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
	lines := deltaY
	if modifiers&tcell.ModShift != 0 {
		page := a.height
		if page <= 0 {
			page = 1
		}
		lines *= page
	} else {
		const step = 3
		lines *= step
	}
	a.vterm.Scroll(lines)
	a.mu.Unlock()
	a.requestRefresh()
}

func (a *TexelTerm) Run() error {

	a.mu.Lock()
	cols, rows := a.width, a.height
	cmdStr := a.command
	a.mu.Unlock()

	cmd := exec.Command(cmdStr)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return fmt.Errorf("failed to start pty: %w", err)
	}
	a.pty = ptmx
	a.cmd = cmd

	a.mu.Lock()
	a.vterm = parser.NewVTerm(cols, rows,
		parser.WithTitleChangeHandler(func(newTitle string) {
			a.title = newTitle
			a.requestRefresh()
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
	return err
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
	if a.vterm == nil || !a.selection.active || len(buf) == 0 {
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

// wrapRowToString converts a row of cells into a trimmed string, used for
// debugging visual wrapping behaviour.
func wrapRowToString(row []texel.Cell) string {
	var b strings.Builder
	for _, cell := range row {
		ch := cell.Ch
		if ch == 0 {
			ch = ' '
		}
		b.WriteRune(ch)
	}
	return strings.TrimRight(b.String(), " ")
}

// applyVisualWrapLocked performs a visual-only reflow of the logical line at
// the cursor when it exceeds the viewport width. It does not modify vterm
// state or the PTY; it only redraws the affected rows in a.buf so the user sees
// a wrapped view of the current line while typing.
func (a *TexelTerm) applyVisualWrapLocked(cursorX, cursorY, cols, rows int) {
	if a.vterm == nil || cols <= 0 || rows <= 0 {
		return
	}
	top := a.vterm.VisibleTop()
	lineIdx := top + cursorY
	historyLen := a.vterm.HistoryLength()
	if historyLen <= 0 || lineIdx < 0 || lineIdx >= historyLen {
		return
	}
	cells := a.vterm.HistoryLineCopy(lineIdx)
	if cells == nil {
		return
	}
	totalLen := len(cells)
	if totalLen <= cols {
		// Fits in a single row; no need to reflow.
		return
	}

	// For now, treat the entire line as input; once OSC133 inputStartCol is
	// wired through and safely accessible without locking, we can preserve the
	// prompt prefix and only wrap the tail.
	prefixCols := 0
	if prefixCols < 0 {
		prefixCols = 0
	}
	if prefixCols > cols {
		prefixCols = cols
	}

	tailStart := prefixCols
	if tailStart > totalLen {
		tailStart = totalLen
	}
	tailLen := totalLen - tailStart
	if tailLen <= 0 {
		return
	}

	firstSegWidth := cols - prefixCols
	if firstSegWidth <= 0 {
		firstSegWidth = cols
		prefixCols = 0
		tailStart = 0
		tailLen = totalLen
	}

	if tailLen <= firstSegWidth {
		// Tail fits on the first row after any prefix.
		return
	}

	remain := tailLen - firstSegWidth
	extraSegs := (remain + cols - 1) / cols
	segCount := 1 + extraSegs

	// Compute visual anchor: first segment row and potential scroll so all
	// wrapped segments fit within the viewport, preferring to open rows below.
	startY := cursorY
	lastSegY := startY + segCount - 1
	scrollUp := 0
	if lastSegY >= rows {
		scrollUp = lastSegY - (rows - 1)
	}

	if scrollUp > 0 {
		shifted := make([][]texel.Cell, rows)
		tm := theme.Get()
		clrBg := tm.GetColor("ui", "surface_bg", tcell.ColorBlack)
		clrFg := tm.GetColor("ui", "surface_fg", tcell.ColorWhite)
		clearStyle := tcell.StyleDefault.Background(clrBg).Foreground(clrFg)
		for y := 0; y < rows; y++ {
			srcY := y + scrollUp
			if srcY < rows {
				shifted[y] = a.buf[srcY]
			} else {
				shifted[y] = make([]texel.Cell, cols)
				for x := 0; x < cols; x++ {
					shifted[y][x] = texel.Cell{Ch: ' ', Style: clearStyle}
				}
			}
		}
		a.buf = shifted
		startY = cursorY - scrollUp
		if startY < 0 {
			startY = 0
		}
	}

	// Redraw wrapped segments: first row keeps any prefix, subsequent rows are
	// full-width tail segments.
	// First segment row
	if startY >= 0 && startY < rows {
		// Clear tail area only (keep any prefix).
		for x := prefixCols; x < cols; x++ {
			a.buf[startY][x].Ch = ' '
		}
		segTailEnd := tailStart + firstSegWidth
		if segTailEnd > totalLen {
			segTailEnd = totalLen
		}
		for x, ci := prefixCols, tailStart; ci < segTailEnd && x < cols; ci, x = ci+1, x+1 {
			a.buf[startY][x] = a.applyParserStyle(cells[ci])
		}
	}

	// Remaining segments below.
	offset := firstSegWidth
	for seg := 1; seg < segCount; seg++ {
		targetY := startY + seg
		if targetY < 0 || targetY >= rows {
			continue
		}
		for x := 0; x < cols; x++ {
			a.buf[targetY][x].Ch = ' '
		}
		start := tailStart + offset + (seg-1)*cols
		end := start + cols
		if end > totalLen {
			end = totalLen
		}
		for x, ci := 0, start; ci < end && x < cols; ci, x = ci+1, x+1 {
			a.buf[targetY][x] = a.applyParserStyle(cells[ci])
		}
	}

	// Re-apply caret styling at the original cursor position.
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorX >= cols {
		cursorX = cols - 1
	}
	if cursorY >= 0 && cursorY < rows {
		a.buf[cursorY][cursorX].Style = a.buf[cursorY][cursorX].Style.Reverse(true)
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
		)
		a.mu.Lock()
		cmd = a.cmd
		pty = a.pty
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
	// Standard ANSI colors 0-15
	p[0] = tcell.NewRGBColor(10, 10, 20)
	p[1] = tcell.NewRGBColor(128, 0, 0)
	p[2] = tcell.NewRGBColor(0, 128, 0)
	p[3] = tcell.NewRGBColor(128, 128, 0)
	p[4] = tcell.NewRGBColor(60, 60, 128)
	p[5] = tcell.NewRGBColor(128, 0, 128)
	p[6] = tcell.NewRGBColor(0, 128, 128)
	p[7] = tcell.NewRGBColor(192, 192, 192)
	p[8] = tcell.NewRGBColor(128, 128, 128)
	p[9] = tcell.NewRGBColor(255, 0, 0)
	p[10] = tcell.NewRGBColor(0, 255, 0)
	p[11] = tcell.NewRGBColor(255, 255, 0)
	p[12] = tcell.NewRGBColor(0, 0, 255)
	p[13] = tcell.NewRGBColor(255, 0, 255)
	p[14] = tcell.NewRGBColor(0, 255, 255)
	p[15] = tcell.NewRGBColor(255, 255, 255)

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
	p[256] = p[15] // White
	p[257] = p[0]  // Black
	return p
}

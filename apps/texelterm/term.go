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
	"sync"
	"syscall"
	"texelation/apps/texelterm/parser"
	"texelation/texel"
	"texelation/texel/cards"

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
	refreshChan  chan<- bool
	wg           sync.WaitGroup
	buf          [][]texel.Cell
	colorPalette [258]tcell.Color
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

	rainbow := cards.NewRainbowCard(0.5, 0.6)
	control := func(ev *tcell.EventKey) bool {
		if ev.Key() == tcell.KeyCtrlG {
			rainbow.Toggle()
			return true
		}
		return false
	}

	return cards.NewPipeline(control, cards.WrapApp(term), rainbow)
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
			select {
			case <-a.stop:
				return
			default:
			}

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

	return cmd.Wait()
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
	close(a.stop)
	if a.pty != nil {
		a.pty.Close()
	}
	if a.cmd != nil && a.cmd.Process != nil {
		a.cmd.Process.Signal(syscall.SIGTERM)
	}
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

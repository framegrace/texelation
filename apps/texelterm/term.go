package texelterm

import (
	"bufio"
	"io"
	"log"
	"os"
	"os/exec"
	//	"strconv"
	"sync"
	"syscall"
	"texelation/apps/texelterm/parser"
	"texelation/texel"

	"github.com/creack/pty"
	"github.com/gdamore/tcell/v2" // Import tcell
)

// The texelTerm struct remains the same, but its Render method will produce tcell-compatible output.
type texelTerm struct {
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
	colorPalette [258]tcell.Color // Local palette for this terminal instance
}

func New(title, command string) texel.App {
	return &texelTerm{
		title:        title,
		command:      command,
		width:        80, // Sensible defaults
		height:       24,
		stop:         make(chan struct{}),
		colorPalette: newDefaultPalette(),
	}
}

// mapParserColorToTCell translates our internal parser.Color to a true RGB tcell.Color using the local palette.
func (a *texelTerm) mapParserColorToTCell(c parser.Color) tcell.Color {
	switch c.Mode {
	case parser.ColorModeDefault:
		// Use the default foreground color from our local palette
		return a.colorPalette[256]
	case parser.ColorModeStandard:
		return a.colorPalette[c.Value]
	case parser.ColorMode256:
		return a.colorPalette[c.Value]
	case parser.ColorModeRGB:
		return tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B))
	default:
		return tcell.ColorDefault
	}
}

func (a *texelTerm) applyParserStyle(pCell parser.Cell) texel.Cell {
	// Get the foreground color by mapping it through our local palette.
	fgColor := a.mapParserColorToTCell(pCell.FG)

	// Handle the background color explicitly.
	var bgColor tcell.Color
	if pCell.BG.Mode == parser.ColorModeDefault {
		// If the cell's BG is the default, use our palette's default BG.
		bgColor = a.colorPalette[257]
	} else {
		// Otherwise, map it normally.
		bgColor = a.mapParserColorToTCell(pCell.BG)
	}

	style := tcell.StyleDefault
	style = style.Foreground(fgColor).Background(bgColor)
	style = style.Bold(pCell.Attr&parser.AttrBold != 0)
	style = style.Underline(pCell.Attr&parser.AttrUnderline != 0)
	style = style.Reverse(pCell.Attr&parser.AttrReverse != 0)

	return texel.Cell{
		Ch:    pCell.Rune,
		Style: style,
	}
}

// SetRefreshNotifier implements the new interface method.
func (a *texelTerm) SetRefreshNotifier(refreshChan chan<- bool) {
	a.refreshChan = refreshChan
}

func (a *texelTerm) HandleMessage(msg texel.Message) {
	// This app doesn't handle messages.
}

func (a *texelTerm) Render() [][]texel.Cell {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.vterm == nil {
		return [][]texel.Cell{}
	}

	vtermGrid := a.vterm.Grid()
	rows := len(vtermGrid)
	if rows == 0 {
		return [][]texel.Cell{}
	}
	cols := len(vtermGrid[0])

	if len(a.buf) != rows || (rows > 0 && cap(a.buf[0]) != cols) {
		a.buf = make([][]texel.Cell, rows)
		for y := range a.buf {
			a.buf[y] = make([]texel.Cell, cols)
		}
	}

	cursorX, cursorY := a.vterm.Cursor()
	cursorVisible := a.vterm.CursorVisible()

	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			parserCell := vtermGrid[y][x]
			a.buf[y][x] = a.applyParserStyle(parserCell)

			if cursorVisible && x == cursorX && y == cursorY {
				a.buf[y][x].Style = a.buf[y][x].Style.Reverse(true)
			}
		}
	}
	return a.buf
}

func (a *texelTerm) HandleKey(ev *tcell.EventKey) {
	if a.pty == nil {
		return
	}

	a.mu.Lock()
	appMode := a.vterm.AppCursorKeys()
	a.mu.Unlock()

	key := ev.Key()
	var keyBytes []byte

	// Use a switch to handle special keys first.
	switch key {
	case tcell.KeyUp:
		if appMode {
			keyBytes = []byte("\x1bOA")
		} else {
			keyBytes = []byte("\x1b[A")
		}
	case tcell.KeyDown:
		if appMode {
			keyBytes = []byte("\x1bOB")
		} else {
			keyBytes = []byte("\x1b[B")
		}
	case tcell.KeyRight:
		if appMode {
			keyBytes = []byte("\x1bOC")
		} else {
			keyBytes = []byte("\x1b[C")
		}
	case tcell.KeyLeft:
		if appMode {
			keyBytes = []byte("\x1bOD")
		} else {
			keyBytes = []byte("\x1b[D")
		}
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
	// F5-F12 have more complex sequences, add as needed
	// ...

	case tcell.KeyEnter:
		keyBytes = []byte("\r")
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		// KeyBackspace is Ctrl-H, KeyBackspace2 is the real backspace
		keyBytes = []byte{'\b'}
	case tcell.KeyTab:
		keyBytes = []byte("\t")
	case tcell.KeyEsc:
		keyBytes = []byte("\x1b")

	// If it's not a special key, it's a rune or a Ctrl-key combo
	default:
		keyBytes = []byte(string(ev.Rune()))
	}

	if keyBytes != nil {
		a.pty.Write(keyBytes)
	}
}

func (a *texelTerm) Run() error {
	a.mu.Lock()
	cols := a.width
	rows := a.height
	a.mu.Unlock()

	cmd := exec.Command(a.command)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
	)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
	if err != nil {
		log.Printf("Failed to start pty with size: %v", err)
		return err
	}
	a.pty = ptmx
	a.cmd = cmd

	// Initialize our virtual terminal and parser
	a.mu.Lock()
	titleChangeHandler := func(newTitle string) {
		a.title = newTitle
		if a.refreshChan != nil {
			// Non-blocking send to avoid deadlocks if the channel is full
			select {
			case a.refreshChan <- true:
			default:
			}
		}
	}
	ptyWriter := func(b []byte) {
		if a.pty != nil {
			a.pty.Write(b)
		}
	}
	a.vterm = parser.NewVTerm(cols, rows,
		parser.WithTitleChangeHandler(titleChangeHandler),
		parser.WithPtyWriter(ptyWriter),
	)
	a.parser = parser.NewParser(a.vterm)
	a.mu.Unlock()

	a.wg.Add(1)

	go func() {
		defer a.wg.Done()
		defer ptmx.Close()

		// Create a buffered reader for robust rune-wise reading
		reader := bufio.NewReader(ptmx)

		for {
			// Check for the stop signal before blocking on ReadRune
			select {
			case <-a.stop:
				return
			default:
			}

			// Read a single, complete rune, handling UTF-8 boundaries automatically.
			r, _, err := reader.ReadRune()
			if err != nil {
				if err != io.EOF {
					log.Printf("Error reading from PTY: %v", err)
				}
				return
			}

			a.mu.Lock()
			if a.parser != nil {
				a.parser.Parse(r)
			}
			a.mu.Unlock()

			// Request a refresh after processing a rune.
			if a.refreshChan != nil {
				select {
				case a.refreshChan <- true:
				default:
				}
			}
		}
	}()

	return cmd.Wait()
}

func (a *texelTerm) Resize(cols, rows int) {
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
		pty.Setsize(a.pty, &pty.Winsize{
			Rows: uint16(rows),
			Cols: uint16(cols),
		})
		//		a.pty.Write([]byte{'\x0C'})
	}
}
func (a *texelTerm) Stop() {
	close(a.stop)

	if a.pty != nil {
		a.pty.Close()
	}
	if a.cmd != nil && a.cmd.Process != nil {
		a.cmd.Process.Signal(syscall.SIGTERM)
	}
}

func (a *texelTerm) GetTitle() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.title
}

func newDefaultPalette() [258]tcell.Color {
	// Based on the standard xterm 256 color palette.
	var p [258]tcell.Color
	// First 16 ANSI colors
	p[0] = tcell.NewRGBColor(0, 0, 0)        // Black
	p[1] = tcell.NewRGBColor(128, 0, 0)      // Maroon
	p[2] = tcell.NewRGBColor(0, 128, 0)      // Green
	p[3] = tcell.NewRGBColor(128, 128, 0)    // Olive
	p[4] = tcell.NewRGBColor(0, 0, 128)      // Navy
	p[5] = tcell.NewRGBColor(128, 0, 128)    // Purple
	p[6] = tcell.NewRGBColor(0, 128, 128)    // Teal
	p[7] = tcell.NewRGBColor(192, 192, 192)  // Silver
	p[8] = tcell.NewRGBColor(128, 128, 128)  // Grey
	p[9] = tcell.NewRGBColor(255, 0, 0)      // Red
	p[10] = tcell.NewRGBColor(0, 255, 0)     // Lime
	p[11] = tcell.NewRGBColor(255, 255, 0)   // Yellow
	p[12] = tcell.NewRGBColor(0, 0, 255)     // Blue
	p[13] = tcell.NewRGBColor(255, 0, 255)   // Fuchsia
	p[14] = tcell.NewRGBColor(0, 255, 255)   // Aqua
	p[15] = tcell.NewRGBColor(255, 255, 255) // White

	// 6x6x6 color cube
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

	// Grayscale ramp
	for j := 0; j < 24; j++ {
		gray := int32(8 + j*10)
		p[i] = tcell.NewRGBColor(gray, gray, gray)
		i++
	}

	// Default Foreground (White) and Background (Black)
	p[256] = p[15]
	p[257] = p[0]

	return p
}

package tui

import (
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"textmode-env/tui/parser"
	"time"

	"github.com/creack/pty"
	"github.com/gdamore/tcell/v2" // Import tcell
)

// The PTYApp struct remains the same, but its Render method will produce tcell-compatible output.
type PTYApp struct {
	title   string
	command string
	width   int
	height  int
	cmd     *exec.Cmd
	pty     *os.File
	vterm   *parser.VTerm
	parser  *parser.Parser
	mu      sync.Mutex
	stop    chan struct{}
}

// Render translates our VTerm's state into the main application's buffer.
func (a *PTYApp) Render() [][]Cell {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.vterm == nil {
		return [][]Cell{}
	}

	vtermGrid := a.vterm.Grid()
	rows := len(vtermGrid)
	if rows == 0 {
		return [][]Cell{}
	}
	cols := len(vtermGrid[0])

	buffer := make([][]Cell, rows)
	cursorX, cursorY := a.vterm.Cursor()
	cursorVisible := a.vterm.CursorVisible()

	for y := 0; y < rows; y++ {
		buffer[y] = make([]Cell, cols)
		for x := 0; x < cols; x++ {
			parserCell := vtermGrid[y][x]
			// The new apply function returns a tcell.Style
			buffer[y][x] = applyParserStyle(parserCell)

			if cursorVisible && x == cursorX && y == cursorY {
				// To show the cursor, we just get the style and reverse it.
				buffer[y][x].Style = buffer[y][x].Style.Reverse(true)
			}
		}
	}
	return buffer
}

func applyParserStyle(pCell parser.Cell) Cell {
	style := tcell.StyleDefault
	fg := mapParserColorToTCell(pCell.FG)
	bg := mapParserColorToTCell(pCell.BG)
	style = style.Foreground(fg).Background(bg)
	style = style.Bold(pCell.Attr&parser.AttrBold != 0)
	style = style.Underline(pCell.Attr&parser.AttrUnderline != 0)
	style = style.Reverse(pCell.Attr&parser.AttrReverse != 0)
	return Cell{
		Ch:    pCell.Rune,
		Style: style,
	}
}

// mapParserColorToTCell translates our custom Color type to a tcell.Color.
func mapParserColorToTCell(c parser.Color) tcell.Color {
	switch c.Mode {
	case parser.ColorModeDefault:
		return tcell.ColorDefault
	case parser.ColorModeStandard:
		// CORRECTED: Cast uint8 to int
		return tcell.PaletteColor(int(c.Value))
	case parser.ColorMode256:
		// CORRECTED: Cast uint8 to int
		return tcell.PaletteColor(int(c.Value))
	default:
		return tcell.ColorDefault
	}
}

// HandleKey processes a key event and writes it to the PTY.
func (a *PTYApp) HandleKey(ev *tcell.EventKey) {
	if a.pty == nil {
		return
	}
	// A key's rune is the character to write.
	// For special keys like Ctrl-L, the rune is the control character itself.
	a.pty.Write([]byte(string(ev.Rune())))

	// This could be expanded to handle arrow keys, etc.
	// switch ev.Key() {
	// case tcell.KeyUp:
	// 	a.pty.Write([]byte("\x1b[A"))
	// }
}

func NewPTYApp(title, command string) *PTYApp {
	return &PTYApp{
		title:   title,
		command: command,
		width:   80, // Sensible defaults
		height:  24,
		stop:    make(chan struct{}),
	}
}

// Run now only sets up the reading goroutine. The PTY is started by Resize.
func (a *PTYApp) Run() error {
	go func() {
		// This goroutine waits until the PTY is ready
		var ptyFile *os.File
		for {
			a.mu.Lock()
			ptyFile = a.pty
			a.mu.Unlock()
			if ptyFile != nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		buf := make([]byte, 4096)
		for {
			select {
			case <-a.stop:
				return
			default:
				n, err := ptyFile.Read(buf)
				if n > 0 {
					a.mu.Lock()
					if a.parser != nil {
						a.parser.Parse(buf[:n])
					}
					a.mu.Unlock()
				}
				if err != nil {
					return
				}
			}
		}
	}()
	return nil
}

func (a *PTYApp) Stop() {
	close(a.stop)
	if a.pty != nil {
		a.pty.Close()
	}
	if a.cmd != nil && a.cmd.Process != nil {
		a.cmd.Process.Kill()
	}
}

// Resize now handles PTY creation on its first run.
func (a *PTYApp) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	a.width = cols
	a.height = rows

	// If this is the first resize, create and launch the PTY.
	if a.pty == nil {
		log.Printf("PTYApp: First resize, creating PTY with size %dx%d", cols, rows)

		cmd := exec.Command(a.command)
		cmd.Env = append(os.Environ(),
			"TERM=xterm-256color",
			"COLUMNS="+strconv.Itoa(cols),
			"LINES="+strconv.Itoa(rows),
		)
		a.cmd = cmd

		var err error
		// Use pty.StartWithSize to create the PTY with the correct size from the beginning.
		a.pty, err = pty.StartWithSize(cmd, &pty.Winsize{
			Rows: uint16(rows),
			Cols: uint16(cols),
		})
		if err != nil {
			log.Printf("Failed to start pty with size: %v", err)
			return
		}

		// Now that the PTY exists, set up the VTerm and Parser
		titleChangeHandler := func(newTitle string) { a.title = newTitle }
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

	} else {
		// On subsequent resizes, just update the VTerm and PTY size.
		log.Printf("PTYApp: Subsequent resize, setting PTY size to %dx%d", cols, rows)
		a.vterm.Resize(cols, rows)
		pty.Setsize(a.pty, &pty.Winsize{
			Rows: uint16(rows),
			Cols: uint16(cols),
		})
	}
}

func (a *PTYApp) GetTitle() string {
	// This method now returns the dynamically updated title!
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.title
}

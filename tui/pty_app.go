package tui

import (
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"textmode-env/tui/parser"

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

func (a *PTYApp) Run() error {
	a.mu.Lock()
	cols := a.width
	rows := a.height

	// --- ONE-TIME INITIALIZATION ---
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
	// --- END INITIALIZATION ---

	a.mu.Unlock()

	cmd := exec.Command(a.command)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLUMNS="+strconv.Itoa(cols),
		"LINES="+strconv.Itoa(rows),
	)
	a.cmd = cmd

	var err error
	a.pty, err = pty.Start(cmd)
	if err != nil {
		log.Printf("Failed to start pty for command '%s': %v", a.command, err)
		return err
	}

	go func() {
		buf := make([]byte, 4096)
		for {
			select {
			case <-a.stop:
				return
			default:
				n, err := a.pty.Read(buf)
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

func (a *PTYApp) Resize(cols, rows int) {
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
	}
}

func (a *PTYApp) GetTitle() string {
	// This method now returns the dynamically updated title!
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.title
}

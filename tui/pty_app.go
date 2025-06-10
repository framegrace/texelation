package tui

import (
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"textmode-env/tui/parser"

	"github.com/creack/pty"
	"github.com/nsf/termbox-go"
)

// PTYApp uses our own custom parser.
type PTYApp struct {
	title   string
	command string
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
			buffer[y][x] = applyParserStyle(parserCell)

			if cursorVisible && x == cursorX && y == cursorY {
				fg, bg := buffer[y][x].Fg, buffer[y][x].Bg
				buffer[y][x].Fg = bg
				buffer[y][x].Bg = fg
			}
		}
	}
	return buffer
}

// applyParserStyle translates our parser.Cell into the main tui.Cell for rendering.
func applyParserStyle(pCell parser.Cell) Cell {
	fg := termbox.Attribute(pCell.FG)
	bg := termbox.Attribute(pCell.BG)

	// --- START: The Fix is here ---
	if pCell.Attr&parser.AttrReverse != 0 {
		// If colors are default, simulate standard reverse (black on white)
		if pCell.FG == parser.ColorDefault && pCell.BG == parser.ColorDefault {
			fg = termbox.ColorBlack
			bg = termbox.ColorWhite
		} else {
			// Otherwise, just swap the existing colors
			fg, bg = bg, fg
		}
	}
	// --- END: The Fix is here ---

	if pCell.Attr&parser.AttrBold != 0 {
		fg |= termbox.AttrBold
	}
	if pCell.Attr&parser.AttrUnderline != 0 {
		fg |= termbox.AttrUnderline
	}

	return Cell{
		Ch: pCell.Rune,
		Fg: fg,
		Bg: bg,
	}
}

// --- Rest of file is unchanged ---

func NewPTYApp(title, command string) *PTYApp {
	return &PTYApp{
		title:   title,
		command: command,
		stop:    make(chan struct{}),
	}
}

func (a *PTYApp) Run() error {
	cols, rows := 80, 24
	a.mu.Lock()
	if a.vterm != nil {
		grid := a.vterm.Grid()
		rows = len(grid)
		if rows > 0 {
			cols = len(grid[0])
		}
	}
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

// Resize creates our VTerm and Parser and informs the PTY of the size change.
func (a *PTYApp) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	// --- NEW: Define the callback function ---
	titleChangeHandler := func(newTitle string) {
		// This function will be called by the VTerm when a title change occurs.
		// We need to lock the mutex since this can be called from the parser's goroutine.
		//a.mu.Lock()
		//defer a.mu.Unlock()
		a.title = newTitle
	}

	// Create our virtual terminal, passing the handler as an option
	a.vterm = parser.NewVTerm(cols, rows, parser.WithTitleChangeHandler(titleChangeHandler))
	a.parser = parser.NewParser(a.vterm)

	// Inform the PTY of the size change
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

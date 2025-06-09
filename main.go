package main

import (
	"fmt"
	"github.com/creack/pty"
	"github.com/nsf/termbox-go"
	"github.com/veops/go-ansiterm"
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

var defaultFg = "white"
var defaultBg = "black"

var (
	logFile *os.File
	logger  *log.Logger
)

func init() {
	var err error
	logFile, err = os.OpenFile("ansiterm.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		panic(err)
	}
	logger = log.New(logFile, "[ansiterm] ", log.LstdFlags)
}

type Cell struct {
	ch rune
	fg termbox.Attribute
	bg termbox.Attribute
}

type Pane struct {
	x0, y0, x1, y1 int
	title          string
	tick           int
	renderFn       func(*Pane, [][]Cell)
	cmd            *exec.Cmd
	pty            *os.File
	outputBuffer   [][]rune
	screen         *ansiterm.Screen
	stream         *ansiterm.ByteStream
	mu             sync.Mutex
}

func main() {
	err := termbox.Init()
	if err != nil {
		panic(err)
	}
	defer termbox.Close()
	termbox.HideCursor()

	width, height := termbox.Size()
	curr := makeBuffer(width, height)
	prev := makeBuffer(width, height)

	eventChan := make(chan termbox.Event, 1)
	go pollEvents(eventChan)

	frame := 0
	fps := 0
	frameCount := 0
	lastFPSUpdate := time.Now()

	// Initialize panes
	panes := setupPanes(width, height)

	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()

mainloop:
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			frame++
			frameCount++

			if now.Sub(lastFPSUpdate) >= time.Second {
				fps = frameCount
				frameCount = 0
				lastFPSUpdate = now
			}

			clearBuffer(curr, width, height)

			// Draw FPS
			fpsStr := fmt.Sprintf("FPS: %d", fps)
			for i, r := range fpsStr {
				if i < width {
					curr[0][i] = Cell{ch: r, fg: termbox.ColorYellow, bg: termbox.ColorBlack}
				}
			}

			// Render each pane
			for i := range panes {
				panes[i].renderFn(&panes[i], curr)
				panes[i].tick++
			}

			// Draw borders
			drawPaneBorders(curr, panes)
			drawDiffs(curr, prev, width, height)
			termbox.Flush()

		case ev := <-eventChan:
			switch ev.Type {
			case termbox.EventKey:
				if ev.Key == termbox.KeyEsc || ev.Ch == 'q' {
					break mainloop
				}
			case termbox.EventResize:
				width, height = termbox.Size()
				curr = makeBuffer(width, height)
				prev = makeBuffer(width, height)
				panes = setupPanes(width, height)
			case termbox.EventError:
				panic(ev.Err)
			}
		}
	}
}

func launchPTY(p *Pane, command string) {
	// Initialize ANSI terminal emulator
	cols := p.x1 - p.x0 - 1
	rows := p.y1 - p.y0 - 1
	cmd := exec.Command(command)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLUMNS="+strconv.Itoa(cols),
		"LINES="+strconv.Itoa(rows),
	)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		p.title = "ERROR"
		return
	}
	p.cmd = cmd
	p.pty = ptmx

	pty.Setsize(ptmx, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})

	p.screen = ansiterm.NewScreen(cols, rows)
	p.stream = ansiterm.InitByteStream(p.screen, false)
	p.stream.Attach(p.screen)

	p.screen.WriteProcessInput = func(data string) {
		p.pty.Write([]byte(data))
	}

	p.screen.SetMargins(0, p.y1-p.y0-1)
	// Start reading PTY output
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := p.pty.Read(buf)
			if n > 0 {
				p.mu.Lock()
				p.stream.Feed(buf[:n])
				p.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
}

func setupPanes(w, h int) []Pane {
	cellW := w / 2
	cellH := h / 2
	panes := []Pane{
		{
			x0: 0, y0: 0, x1: cellW, y1: cellH, title: "htop", renderFn: appPTY,
		},
		{
			x0: cellW, y0: 0, x1: w, y1: cellH,
			title: "Pane B", renderFn: appB,
		},
		{
			x0: 0, y0: cellH, x1: cellW, y1: h,
			title: "Pane C", renderFn: appC,
		},
		{
			x0: cellW, y0: cellH, x1: w, y1: h,
			title: "Pane D", renderFn: appD,
		},
	}
	launchPTY(&panes[0], "top")
	return panes
}

func appPTY(p *Pane, buf [][]Cell) {
	if p.screen == nil {
		writeTitle(buf, p, termbox.ColorRed)
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	maxRows := p.screen.Rows()
	maxCols := p.screen.Cols()

	for y := 0; y < maxRows && p.y0+1+y < p.y1-1; y++ {
		for x := 0; x < maxCols && p.x0+1+x < p.x1-1; x++ {
			cell := p.screen.CellAt(x, y)
			runes := []rune(cell.Data)
			if len(runes) > 0 {
				fg, bg := applyStyle(cell.Fg, cell.Bg, cell.Bold, cell.Reverse)
				drawRune(buf, p, x, y, runes[0], fg, bg)
				//				buf[p.y0+y][p.x0+x] = Cell{
				//					ch: runes[0],
				//					fg: fg,
				//					bg: bg,
				//				}
			}
		}
	}
	writeTitle(buf, p, termbox.ColorWhite)
}

func mapColor(name string) termbox.Attribute {
	// if name != "default" && name != "" {
	//		logger.Printf("mapColor called with: %s", name)
	//	}
	switch name {
	case "black":
		return termbox.ColorBlack
	case "red":
		return termbox.ColorRed
	case "green":
		return termbox.ColorGreen
	case "yellow":
		return termbox.ColorYellow
	case "blue":
		return termbox.ColorBlue
	case "magenta":
		return termbox.ColorMagenta
	case "cyan":
		return termbox.ColorCyan
	case "white":
		return termbox.ColorWhite
	case "default":
		fallthrough
	default:
		return termbox.ColorDefault
	}
}

func appA(p *Pane, buf [][]Cell) {
	for y := p.y0 + 1; y < p.y1; y++ {
		for x := p.x0; x < p.x1; x++ {
			ch := rune('A' + ((x + y + p.tick) % 26))
			buf[y][x] = Cell{ch: ch, fg: termbox.ColorGreen}
		}
	}
	writeTitle(buf, p, termbox.ColorGreen)
}

func appB(p *Pane, buf [][]Cell) {
	for y := p.y0 + 1; y < p.y1; y++ {
		for x := p.x0; x < p.x1; x++ {
			if (x+y+p.tick)%5 == 0 {
				buf[y][x] = Cell{ch: '*', fg: termbox.ColorMagenta}
			}
		}
	}
	writeTitle(buf, p, termbox.ColorMagenta)
}

func appC(p *Pane, buf [][]Cell) {
	str := fmt.Sprintf("Tick: %d", p.tick)
	y := (p.y0 + p.y1) / 2
	x := (p.x0 + p.x1 - len(str)) / 2
	for i, ch := range str {
		if x+i < p.x1 {
			buf[y][x+i] = Cell{ch: ch, fg: termbox.ColorCyan}
		}
	}
	writeTitle(buf, p, termbox.ColorCyan)
}

func appD(p *Pane, buf [][]Cell) {
	for y := p.y0 + 1; y < p.y1; y++ {
		for x := p.x0; x < p.x1; x++ {
			if (x+y+p.tick)%8 == 0 {
				buf[y][x] = Cell{ch: '#', fg: termbox.ColorRed}
			}
		}
	}
	writeTitle(buf, p, termbox.ColorRed)
}

func writeTitle(buf [][]Cell, p *Pane, color termbox.Attribute) {
	title := fmt.Sprintf(" %s ", p.title)
	x := p.x0 + 1
	y := p.y0
	for i, ch := range title {
		if x+i < p.x1 {
			buf[y][x+i] = Cell{ch: ch, fg: color | termbox.AttrBold}
		}
	}
}

func drawPaneBorders(buf [][]Cell, panes []Pane) {
	for _, p := range panes {
		for x := p.x0; x < p.x1; x++ {
			buf[p.y0][x] = Cell{ch: '─', fg: termbox.ColorWhite}
		}
		for y := p.y0; y < p.y1; y++ {
			buf[y][p.x0] = Cell{ch: '│', fg: termbox.ColorWhite}
		}
		buf[p.y0][p.x0] = Cell{ch: '┌', fg: termbox.ColorWhite}
	}
}

func renderAnsiTest(p *Pane, buf [][]Cell) {
	p.mu.Lock()
	defer p.mu.Unlock()
	output := "\x1b[7mThis is reversed\x1b[0m and this is not."

	p.screen.Reset()
	p.screen.SetMargins(0, p.y1-p.y0-1)
	p.stream.Feed([]byte(output))

	for y := 0; y < p.screen.Rows(); y++ {
		for x := 0; x < p.screen.Cols(); x++ {
			cell := p.screen.CellAt(x, y)
			runes := []rune(cell.Data)
			if len(runes) > 0 {
				fg, bg := applyStyle(cell.Fg, cell.Bg, cell.Bold, cell.Reverse)
				buf[p.y0+y][p.x0+x] = Cell{
					ch: runes[0],
					fg: fg,
					bg: bg,
				}
			}
		}
	}
}

func pollEvents(ch chan termbox.Event) {
	for {
		ch <- termbox.PollEvent()
	}
}

func clearBuffer(buf [][]Cell, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			buf[y][x] = Cell{ch: ' ', fg: termbox.ColorDefault, bg: termbox.ColorDefault}
		}
	}
}

func drawDiffs(curr, prev [][]Cell, w, h int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			a := curr[y][x]
			b := prev[y][x]
			if a != b {
				termbox.SetCell(x, y, a.ch, a.fg, a.bg)
				prev[y][x] = a
			}
		}
	}
}

func makeBuffer(w, h int) [][]Cell {
	buf := make([][]Cell, h)
	for i := range buf {
		buf[i] = make([]Cell, w)
	}
	return buf
}

func applyStyle(fgName, bgName string, bold, reverse bool) (termbox.Attribute, termbox.Attribute) {
	fg := resolveColor(fgName, bold)
	bg := resolveColor(bgName, false)

	if reverse {
		fg, bg = bg, fg
	}
	return fg, bg
}

func resolveColor(name string, bold bool) termbox.Attribute {
	if name == "" || name == "default" {
		return termbox.ColorDefault
	}

	switch name {
	case "black":
		if bold {
			return termbox.ColorDarkGray
		}
		return termbox.ColorBlack
	case "red":
		if bold {
			return termbox.ColorLightRed
		}
		return termbox.ColorRed
	case "green":
		if bold {
			return termbox.ColorLightGreen
		}
		return termbox.ColorGreen
	case "yellow":
		if bold {
			return termbox.ColorLightYellow
		}
		return termbox.ColorYellow
	case "blue":
		if bold {
			return termbox.ColorLightBlue
		}
		return termbox.ColorBlue
	case "magenta":
		if bold {
			return termbox.ColorLightMagenta
		}
		return termbox.ColorMagenta
	case "cyan":
		if bold {
			return termbox.ColorLightCyan
		}
		return termbox.ColorCyan
	case "white":
		if bold {
			return termbox.ColorWhite
		}
		return termbox.ColorLightGray
	default:
		return termbox.ColorDefault
	}
}

func drawRune(buf [][]Cell, p *Pane, relX, relY int, ch rune, fg, bg termbox.Attribute) {
	x := p.x0 + 1 + relX
	y := p.y0 + 1 + relY // +1 to skip title line

	//	if y >= p.y1 || x >= p.x1 {
	//		return
	//	}
	buf[y][x] = Cell{ch: ch, fg: fg, bg: bg}
}

package tui

import (
	"fmt"
	"time"

	"github.com/nsf/termbox-go"
)

// Screen manages the entire terminal display, including all panes and the main event loop.
type Screen struct {
	width, height int
	curr, prev    [][]Cell
	panes         []*Pane
	quit          chan struct{}
}

// NewScreen initializes the terminal and creates a new Screen object.
func NewScreen() (*Screen, error) {
	if err := termbox.Init(); err != nil {
		return nil, err
	}
	termbox.HideCursor()

	w, h := termbox.Size()
	return &Screen{
		width:  w,
		height: h,
		curr:   makeBuffer(w, h),
		prev:   makeBuffer(w, h),
		panes:  make([]*Pane, 0),
		quit:   make(chan struct{}),
	}, nil
}

// AddPane adds a pane to the screen and starts its associated app.
func (s *Screen) AddPane(p *Pane) {
	s.panes = append(s.panes, p)
	go p.app.Run()
}

// Run starts the main event and rendering loop.
func (s *Screen) Run() error {
	eventChan := make(chan termbox.Event)
	go func() {
		for {
			eventChan <- termbox.PollEvent()
		}
	}()

	ticker := time.NewTicker(16 * time.Millisecond) // ~60 FPS
	defer ticker.Stop()

	for {
		select {
		case ev := <-eventChan:
			switch ev.Type {
			case termbox.EventKey:
				if ev.Key == termbox.KeyEsc || ev.Ch == 'q' {
					return nil
				}
			case termbox.EventResize:
				s.handleResize(ev.Width, ev.Height)
			case termbox.EventError:
				return ev.Err
			}
		case <-ticker.C:
			s.draw()
		case <-s.quit:
			return nil
		}
	}
}

// Close shuts down termbox and stops all hosted apps.
func (s *Screen) Close() {
	for _, p := range s.panes {
		p.app.Stop()
	}
	termbox.Close()
}

// draw clears the buffer, composites all panes, draws borders, and flushes to the terminal.
func (s *Screen) draw() {
	clearBuffer(s.curr)
	s.compositePanes()
	s.drawBorders()
	s.drawDiffs()
	termbox.Flush()
}

// compositePanes gets the rendered buffer from each pane's app and blits it to the main buffer.
func (s *Screen) compositePanes() {
	for _, p := range s.panes {
		// Get the app's own rendered buffer
		appBuffer := p.app.Render()
		// Blit (copy) the app's buffer to the main screen buffer at the pane's offset.
		// We add 1 to the coordinates to account for the top-left border.
		s.blit(p.X0+1, p.Y0+1, appBuffer)
	}
}

// blit copies a source buffer onto the screen's main buffer at a given coordinate.
func (s *Screen) blit(x, y int, source [][]Cell) {
	for r, row := range source {
		for c, cell := range row {
			absY, absX := y+r, x+c
			if absY >= 0 && absY < s.height && absX >= 0 && absX < s.width {
				s.curr[absY][absX] = cell
			}
		}
	}
}

// drawBorders draws the borders and titles for all panes.
func (s *Screen) drawBorders() {
	for _, p := range s.panes {
		// Draw horizontal lines
		for x := p.X0; x < p.X1 && x < s.width; x++ {
			if p.Y0 < s.height {
				s.curr[p.Y0][x] = Cell{Ch: '─', Fg: termbox.ColorWhite}
			}
		}
		// Draw vertical lines
		for y := p.Y0; y < p.Y1 && y < s.height; y++ {
			if p.X0 < s.width {
				s.curr[y][p.X0] = Cell{Ch: '│', Fg: termbox.ColorWhite}
			}
		}
		// Draw corner
		if p.Y0 < s.height && p.X0 < s.width {
			s.curr[p.Y0][p.X0] = Cell{Ch: '┌', Fg: termbox.ColorWhite}
		}
		// Draw title
		title := fmt.Sprintf(" %s ", p.app.GetTitle())
		tx, ty := p.X0+1, p.Y0
		for i, ch := range title {
			if tx+i < p.X1 && tx+i < s.width {
				s.curr[ty][tx+i] = Cell{Ch: ch, Fg: termbox.ColorWhite | termbox.AttrBold}
			}
		}
	}
}

// drawDiffs compares the current and previous buffers and updates only changed cells.
func (s *Screen) drawDiffs() {
	for y := 0; y < s.height; y++ {
		for x := 0; x < s.width; x++ {
			if s.curr[y][x] != s.prev[y][x] {
				cell := s.curr[y][x]
				termbox.SetCell(x, y, cell.Ch, cell.Fg, cell.Bg)
				s.prev[y][x] = cell
			}
		}
	}
}

// handleResize recalculates layout and resizes panes on terminal resize.
func (s *Screen) handleResize(w, h int) {
	s.width, s.height = w, h
	s.curr = makeBuffer(w, h)
	s.prev = makeBuffer(w, h)
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)

	// Recalculate layout (simple 2x2 grid for this example)
	cellW := w / 2
	cellH := h / 2
	dims := [][4]int{
		{0, 0, cellW, cellH},
		{cellW, 0, w, cellH},
		{0, cellH, cellW, h},
		{cellW, cellH, w, h},
	}

	for i, p := range s.panes {
		if i < len(dims) {
			d := dims[i]
			p.SetDimensions(d[0], d[1], d[2], d[3])
		}
	}
	s.draw()
}

// makeBuffer is a helper to create a 2D Cell slice.
func makeBuffer(w, h int) [][]Cell {
	buf := make([][]Cell, h)
	for i := range buf {
		buf[i] = make([]Cell, w)
	}
	return buf
}

// clearBuffer resets a buffer to default empty cells.
func clearBuffer(buf [][]Cell) {
	for y := range buf {
		for x := range buf[y] {
			buf[y][x] = Cell{Ch: ' ', Fg: termbox.ColorDefault, Bg: termbox.ColorDefault}
		}
	}
}

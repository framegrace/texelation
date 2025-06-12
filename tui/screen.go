package tui

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
)

// Screen manages the entire terminal display using tcell as the backend.
type Screen struct {
	tcellScreen tcell.Screen
	panes       []*Pane
	quit        chan struct{}
}

// NewScreen initializes the terminal with tcell.
func NewScreen() (*Screen, error) {
	tcellScreen, err := tcell.NewScreen()
	if err != nil {
		return nil, err
	}
	if err := tcellScreen.Init(); err != nil {
		return nil, err
	}

	defStyle := tcell.StyleDefault.Background(tcell.ColorReset).Foreground(tcell.ColorReset)
	tcellScreen.SetStyle(defStyle)
	tcellScreen.HideCursor()

	return &Screen{
		tcellScreen: tcellScreen,
		panes:       make([]*Pane, 0),
		quit:        make(chan struct{}),
	}, nil
}

// AddPane adds a pane to the screen and starts its associated app.
func (s *Screen) AddPane(p *Pane) {
	s.panes = append(s.panes, p)
	go p.app.Run()
}

// Run starts the main event and rendering loop.
func (s *Screen) Run() error {
	eventChan := make(chan tcell.Event)
	go func() {
		for {
			eventChan <- s.tcellScreen.PollEvent()
		}
	}()

	ticker := time.NewTicker(16 * time.Millisecond) // ~60 FPS
	defer ticker.Stop()

	for {
		s.draw() // Draw the initial state

		select {
		case ev := <-eventChan:
			switch ev := ev.(type) {
			case *tcell.EventKey:
				if ev.Key() == tcell.KeyEscape || ev.Rune() == 'q' {
					return nil
				}
			case *tcell.EventResize:
				s.handleResize()
			}
		case <-ticker.C:
			// Continuous redraw for animations
			s.draw()
		case <-s.quit:
			return nil
		}
	}
}

// Close shuts down tcell and stops all hosted apps.
func (s *Screen) Close() {
	for _, p := range s.panes {
		p.app.Stop()
	}
	s.tcellScreen.Fini()
}

// draw clears the screen, composites all panes, and shows the result.
func (s *Screen) draw() {
	s.tcellScreen.Clear()
	s.compositePanes()
	s.drawBorders()
	s.tcellScreen.Show() // Replaces termbox.Flush()
}

// compositePanes gets the rendered buffer from each pane's app and draws it.
func (s *Screen) compositePanes() {
	for _, p := range s.panes {
		appBuffer := p.app.Render()
		s.blit(p.X0+1, p.Y0+1, appBuffer)
	}
}

// blit copies a source buffer onto the tcell screen.
func (s *Screen) blit(x, y int, source [][]Cell) {
	for r, row := range source {
		for c, cell := range row {
			s.tcellScreen.SetContent(x+c, y+r, cell.Ch, nil, cell.Style)
		}
	}
}

// drawBorders draws the borders and titles for all panes.
func (s *Screen) drawBorders() {
	borderStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite)
	titleStyle := borderStyle.Bold(true)
	w, h := s.tcellScreen.Size()

	for _, p := range s.panes {
		for x := p.X0; x < p.X1 && x < w; x++ {
			if p.Y0 >= 0 && p.Y0 < h {
				s.tcellScreen.SetContent(x, p.Y0, tcell.RuneHLine, nil, borderStyle)
			}
		}
		for y := p.Y0; y < p.Y1 && y < h; y++ {
			if p.X0 >= 0 && p.X0 < w {
				s.tcellScreen.SetContent(p.X0, y, tcell.RuneVLine, nil, borderStyle)
			}
		}
		if p.X0 >= 0 && p.X0 < w && p.Y0 >= 0 && p.Y0 < h {
			s.tcellScreen.SetContent(p.X0, p.Y0, tcell.RuneULCorner, nil, borderStyle)
		}

		title := fmt.Sprintf(" %s ", p.app.GetTitle())
		for i, ch := range title {
			if p.X0+1+i < p.X1 {
				s.tcellScreen.SetContent(p.X0+1+i, p.Y0, ch, nil, titleStyle)
			}
		}
	}
}

// handleResize is called on a resize event.
func (s *Screen) handleResize() {
	w, h := s.tcellScreen.Size()
	s.tcellScreen.Sync()

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
}

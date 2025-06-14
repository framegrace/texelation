package tui

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

const (
	keyQuit       = tcell.KeyCtrlQ
	keySwitchPane = tcell.KeyCtrlA
)

// Screen manages the entire terminal display using tcell as the backend.
type Screen struct {
	tcellScreen     tcell.Screen
	panes           []*Pane
	activePaneIndex int
	fadeEffect      Effect
	quit            chan struct{}
	refreshChan     chan bool
	mu              sync.RWMutex
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
	tcellScreen.HideCursor() // We let the VTerm in the PTYApp manage the cursor

	return &Screen{
		tcellScreen:     tcellScreen,
		panes:           make([]*Pane, 0),
		activePaneIndex: 0, // Default to the first pane
		fadeEffect:      NewFadeEffect(tcell.ColorBlack, 0.25),
		quit:            make(chan struct{}),
		refreshChan:     make(chan bool, 1),
	}, nil
}

func (s *Screen) Size() (int, int) {
	return s.tcellScreen.Size()
}

// AddPane adds a pane to the screen and starts its associated app.
func (s *Screen) AddPane(p *Pane) {
	s.panes = append(s.panes, p)
	if len(s.panes) > 1 {
		p.AddEffect(s.fadeEffect)
	}

	p.app.SetRefreshNotifier(s.refreshChan)

	go func() {
		if err := p.app.Run(); err != nil {
			log.Printf("App '%s' exited with error: %v", p.app.GetTitle(), err)
		}
	}()
}

// Run starts the main event and rendering loop.
func (s *Screen) Run() error {

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGWINCH)

	eventChan := make(chan tcell.Event)
	go func() {
		for {
			eventChan <- s.tcellScreen.PollEvent()
		}
	}()

	//	ticker := time.NewTicker(16 * time.Millisecond)
	//	defer ticker.Stop()

	s.draw()

	for {
		select {
		case <-sigChan:
			s.tcellScreen.Sync()
		case ev := <-eventChan:
			switch ev := ev.(type) {
			case *tcell.EventKey:
				if ev.Key() == keyQuit {
					s.Close()
					return nil
				}
				if ev.Key() == keySwitchPane {
					if len(s.panes) > 0 {
						s.mu.Lock()
						s.panes[s.activePaneIndex].AddEffect(s.fadeEffect)
						s.activePaneIndex = (s.activePaneIndex + 1) % len(s.panes)
						s.panes[s.activePaneIndex].ClearEffects()
						s.mu.Unlock()
						s.requestRefresh()
					}
				} else {
					// For ALL other keys, forward them directly to the active pane.
					if len(s.panes) > 0 {
						s.panes[s.activePaneIndex].app.HandleKey(ev)
					}
				}
			case *tcell.EventResize:
				s.handleResize()
			}
		case <-s.refreshChan:
			s.draw()
			//		case <-ticker.C:
			//			s.draw()
		case <-s.quit:
			return nil
		}
	}
}

// compositePanes now applies all effects on a pane before drawing it.
func (s *Screen) compositePanes() {
	for _, p := range s.panes {
		appBuffer := p.app.Render()

		// Apply all effects attached to the pane in order.
		for _, effect := range p.effects {
			appBuffer = effect.Apply(appBuffer)
		}

		s.blit(p.X0+1, p.Y0+1, appBuffer)
	}
}

func (s *Screen) requestRefresh() {
	select {
	case s.refreshChan <- true:
	default:
	}
}

// drawBorders now highlights the active pane.
func (s *Screen) drawBorders() {
	w, h := s.tcellScreen.Size()
	defaultBorderStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite)
	activeBorderStyle := tcell.StyleDefault.Foreground(tcell.ColorYellow)

	for i, p := range s.panes {
		borderStyle := defaultBorderStyle
		titleStyle := defaultBorderStyle.Bold(true)

		// Highlight the active pane
		if i == s.activePaneIndex {
			borderStyle = activeBorderStyle
			titleStyle = activeBorderStyle.Bold(true)
		}

		if p.Y0 >= 0 && p.Y0 < h {
			for x := p.X0; x < p.X1 && x < w; x++ {
				s.tcellScreen.SetContent(x, p.Y0, tcell.RuneHLine, nil, borderStyle)
			}
		}
		if p.X0 >= 0 && p.X0 < w {
			for y := p.Y0 + 1; y < p.Y1 && y < h; y++ {
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

// blit copies a source buffer onto the tcell screen.
func (s *Screen) blit(x, y int, source [][]Cell) {
	for r, row := range source {
		for c, cell := range row {
			s.tcellScreen.SetContent(x+c, y+r, cell.Ch, nil, cell.Style)
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

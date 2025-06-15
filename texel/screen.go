package texel

import (
	//	"fmt"
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
	mu              sync.Mutex
	closeOnce       sync.Once
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

	eventChan := make(chan tcell.Event, 10)
	go func() {
		for {
			select {
			case <-s.quit:
				return
			default:
				eventChan <- s.tcellScreen.PollEvent()
			}
		}
	}()

	s.draw()
	for {
		select {
		case <-sigChan:
			s.tcellScreen.Sync()
		case ev := <-eventChan:
			s.handleEvent(ev)
		case <-s.refreshChan:
			s.draw()
		case <-s.quit:
			return nil
		}
	}
}

func (s *Screen) handleEvent(ev tcell.Event) {
	switch ev := ev.(type) {
	case *tcell.EventKey:
		if ev.Key() == keyQuit {
			s.Close()
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
}

func (s *Screen) compositePanes() {
	for _, p := range s.panes {
		appBuffer := p.app.Render()

		for _, effect := range p.effects {
			appBuffer = effect.Apply(appBuffer)
		}

		s.blit(p.absX0, p.absY0, appBuffer)
	}
}

func (s *Screen) requestRefresh() {
	select {
	case s.refreshChan <- true:
	default:
	}
}

func (s *Screen) drawBorders() {
	// w, h := s.tcellScreen.Size()
	// defaultBorderStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite)
	// activeBorderStyle := tcell.StyleDefault.Foreground(tcell.ColorYellow)
	//
	//	for i, p := range s.panes {
	//		borderStyle := defaultBorderStyle
	//		titleStyle := defaultBorderStyle.Bold(true)
	//
	//		if i == s.activePaneIndex {
	//			borderStyle = activeBorderStyle
	//			titleStyle = activeBorderStyle.Bold(true)
	//		}
	//
	//		// Draw top and bottom borders
	//		for x := p.absX0; x < p.absX1; x++ {
	//			if x >= w {
	//				continue
	//			}
	//			if p.absY0 >= 0 && p.absY0 < h {
	//				s.tcellScreen.SetContent(x, p.absY0, tcell.RuneHLine, nil, borderStyle)
	//			}
	//			if p.absY1-1 >= 0 && p.absY1-1 < h {
	//				s.tcellScreen.SetContent(x, p.absY1-1, tcell.RuneHLine, nil, borderStyle)
	//			}
	//		}
	//
	//		// Draw left and right borders
	//		for y := p.absY0; y < p.absY1; y++ {
	//			if y >= h {
	//				continue
	//			}
	//			if p.absX0 >= 0 && p.absX0 < w {
	//				s.tcellScreen.SetContent(p.absX0, y, tcell.RuneVLine, nil, borderStyle)
	//			}
	//			if p.absX1-1 >= 0 && p.absX1-1 < w {
	//				s.tcellScreen.SetContent(p.absX1-1, y, tcell.RuneVLine, nil, borderStyle)
	//			}
	//		}
	//
	//		// Draw corners
	//		if p.absX0 >= 0 && p.absX0 < w && p.absY0 >= 0 && p.absY0 < h {
	//			s.tcellScreen.SetContent(p.absX0, p.absY0, tcell.RuneULCorner, nil, borderStyle)
	//		}
	//		if p.absX1-1 >= 0 && p.absX1-1 < w && p.absY0 >= 0 && p.absY0 < h {
	//			s.tcellScreen.SetContent(p.absX1-1, p.absY0, tcell.RuneURCorner, nil, borderStyle)
	//		}
	//		if p.absX0 >= 0 && p.absX0 < w && p.absY1-1 >= 0 && p.absY1-1 < h {
	//			s.tcellScreen.SetContent(p.absX0, p.absY1-1, tcell.RuneLLCorner, nil, borderStyle)
	//		}
	//		if p.absX1-1 >= 0 && p.absX1-1 < w && p.absY1-1 >= 0 && p.absY1-1 < h {
	//			s.tcellScreen.SetContent(p.absX1-1, p.absY1-1, tcell.RuneLRCorner, nil, borderStyle)
	//		}
	//
	//		// Draw title
	//		title := fmt.Sprintf(" %s ", p.app.GetTitle())
	//		for i, ch := range title {
	//			if p.absX0+1+i < p.absX1-1 {
	//				s.tcellScreen.SetContent(p.absX0+1+i, p.absY0, ch, nil, titleStyle)
	//			}
	//		}
	//	}
}

// Close shuts down tcell and stops all hosted apps.
func (s *Screen) Close() {
	s.closeOnce.Do(func() {
		// Signal the main event loop and event polling goroutine to stop.
		close(s.quit)

		// Stop all the application goroutines.
		for _, p := range s.panes {
			p.app.Stop()
		}

		// Finalize the tcell screen.
		s.tcellScreen.Fini()
	})
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

func (s *Screen) ForceResize() {
	s.handleResize()
}

func (s *Screen) handleResize() {
	w, h := s.tcellScreen.Size()
	s.tcellScreen.Sync()

	for _, p := range s.panes {
		x0 := int(p.Layout.X * float64(w))
		y0 := int(p.Layout.Y * float64(h))
		x1 := int((p.Layout.X + p.Layout.W) * float64(w))
		y1 := int((p.Layout.Y + p.Layout.H) * float64(h))

		if p.Layout.X+p.Layout.W >= 1.0 {
			x1 = w
		}
		if p.Layout.Y+p.Layout.H >= 1.0 {
			y1 = h
		}
		p.SetDimensions(x0, y0, x1, y1)
	}
}

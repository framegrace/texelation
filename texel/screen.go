package texel

import (
	"github.com/gdamore/tcell/v2"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type Direction int

const (
	DirUp Direction = iota
	DirDown
	DirLeft
	DirRight
)

const (
	keyQuit       = tcell.KeyCtrlQ
	keySwitchPane = tcell.KeyCtrlA
)

type styleKey struct {
	fg, bg          tcell.Color
	bold, underline bool
	reverse         bool
}

// Screen manages the entire terminal display using tcell as the backend.
type Screen struct {
	tcellScreen     tcell.Screen
	panes           []*Pane
	activePaneIndex int
	fadeEffect      Effect
	quit            chan struct{}
	refreshChan     chan bool
	neighbors       map[int]map[Direction]int
	mu              sync.Mutex
	closeOnce       sync.Once
	styleCache      map[styleKey]tcell.Style
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

	refreshTimer := time.NewTimer(time.Hour)
	if !refreshTimer.Stop() {
		<-refreshTimer.C
	}

	scr := &Screen{
		tcellScreen:     tcellScreen,
		panes:           make([]*Pane, 0),
		neighbors:       make(map[int]map[Direction]int),
		activePaneIndex: 0,
		quit:            make(chan struct{}),
		refreshChan:     make(chan bool, 1),
		styleCache:      make(map[styleKey]tcell.Style),
	}

	scr.fadeEffect = NewFadeEffect(scr, tcell.ColorBlack, 0.25)

	return scr, nil
}

// updateNeighbors recalculates the neighbor map for all panes.
func (s *Screen) updateNeighbors() {
	w, h := s.tcellScreen.Size()
	// initialize empty neighbor entries
	s.neighbors = make(map[int]map[Direction]int, len(s.panes))
	for i := range s.panes {
		s.neighbors[i] = map[Direction]int{DirUp: -1, DirDown: -1, DirLeft: -1, DirRight: -1}
	}
	// compare each pair
	for i, p := range s.panes {
		rl := p.Layout // fractional Rect
		for j, q := range s.panes {
			if i == j {
				continue
			}
			ol := q.Layout
			// compute absolute coords
			x0 := int(rl.X * float64(w))
			y0 := int(rl.Y * float64(h))
			x1 := int((rl.X + rl.W) * float64(w))
			y1 := int((rl.Y + rl.H) * float64(h))
			x0o := int(ol.X * float64(w))
			y0o := int(ol.Y * float64(h))
			x1o := int((ol.X + ol.W) * float64(w))
			y1o := int((ol.Y + ol.H) * float64(h))
			// right neighbor: touches vertically and to right
			if x1 == x0o && y0o < y1 && y1o > y0 {
				// choose smallest Y offset (topmost)
				curr := s.neighbors[i][DirRight]
				if curr < 0 || y0o < int(s.panes[curr].Layout.Y*float64(h)) {
					s.neighbors[i][DirRight] = j
				}
			}
			// left neighbor
			if x0 == x1o && y0o < y1 && y1o > y0 {
				curr := s.neighbors[i][DirLeft]
				if curr < 0 || y0o < int(s.panes[curr].Layout.Y*float64(h)) {
					s.neighbors[i][DirLeft] = j
				}
			}
			// down neighbor
			if y1 == y0o && x0o < x1 && x1o > x0 {
				curr := s.neighbors[i][DirDown]
				if curr < 0 || x0o < int(s.panes[curr].Layout.X*float64(w)) {
					s.neighbors[i][DirDown] = j
				}
			}
			// up neighbor
			if y0 == y1o && x0o < x1 && x1o > x0 {
				curr := s.neighbors[i][DirUp]
				if curr < 0 || x0o < int(s.panes[curr].Layout.X*float64(w)) {
					s.neighbors[i][DirUp] = j
				}
			}
		}
	}
}

// splitActivePane splits the active pane in the given direction, adding a new pane.
func (s *Screen) splitActivePane(d Direction) {
	idx := s.activePaneIndex
	orig := s.panes[idx]
	layout := orig.Layout
	var a, b Rect
	// split the fractional Rect
	switch d {
	case DirLeft:
		hw := layout.W / 2
		a = Rect{X: layout.X, Y: layout.Y, W: hw, H: layout.H}
		b = Rect{X: layout.X + hw, Y: layout.Y, W: hw, H: layout.H}
	case DirRight:
		hw := layout.W / 2
		a = Rect{X: layout.X, Y: layout.Y, W: hw, H: layout.H}
		b = Rect{X: layout.X + hw, Y: layout.Y, W: hw, H: layout.H}
	case DirUp:
		hh := layout.H / 2
		a = Rect{X: layout.X, Y: layout.Y, W: layout.W, H: hh}
		b = Rect{X: layout.X, Y: layout.Y + hh, W: layout.W, H: hh}
	case DirDown:
		hh := layout.H / 2
		a = Rect{X: layout.X, Y: layout.Y, W: layout.W, H: hh}
		b = Rect{X: layout.X, Y: layout.Y + hh, W: layout.W, H: hh}
	}
	// assign new layouts
	orig.Layout = a
	orig.prevBuf = nil
	// create and insert new pane
	newPane := NewPane(b, NewShellApp())
	s.panes = append(s.panes[:idx+1], append([]*Pane{newPane}, s.panes[idx+1:]...)...)
	// update neighbor relations
	s.updateNeighbors()
	// set focus to new pane
	s.activePaneIndex = idx + 1
}

// moveActivePane moves focus to the neighbor in the given direction.
func (s *Screen) moveActivePane(d Direction) {
	if n, ok := s.neighbors[s.activePaneIndex][d]; ok && n >= 0 {
		s.activePaneIndex = n
	}
}

// swapActivePane swaps layouts of the active pane with its neighbor.
func (s *Screen) swapActivePane(d Direction) {
	if n, ok := s.neighbors[s.activePaneIndex][d]; ok && n >= 0 {
		s.panes[s.activePaneIndex].Layout, s.panes[n].Layout = s.panes[n].Layout, s.panes[s.activePaneIndex].Layout
		s.panes[s.activePaneIndex].prevBuf = nil
		s.panes[n].prevBuf = nil
		s.updateNeighbors()
	}
}

func (s *Screen) getStyle(fg, bg tcell.Color, bold, underline, reverse bool) tcell.Style {
	key := styleKey{fg: fg, bg: bg, bold: bold, underline: underline, reverse: reverse}
	if st, ok := s.styleCache[key]; ok {
		return st
	}
	st := tcell.StyleDefault.Foreground(fg).Background(bg)
	if bold {
		st = st.Bold(true)
	}
	if underline {
		st = st.Underline(true)
	}
	if reverse {
		st = st.Reverse(true)
	}
	s.styleCache[key] = st
	return st
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

	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()

	dirty := true
	s.handleResize()
	s.draw()
	for {
		select {
		case <-sigChan:
			s.tcellScreen.Sync()
			s.handleResize()
			dirty = true
		case ev := <-eventChan:
			s.handleEvent(ev)
			dirty = true
		case <-s.refreshChan:
			dirty = true
		case <-ticker.C:
			if dirty {
				s.draw()
				dirty = false
			}
		case <-s.quit:
			return nil
		}
	}
}

// handleEvent processes key and resize events, including our custom pane controls.
func (s *Screen) handleEvent(ev tcell.Event) {
	switch ev := ev.(type) {
	case *tcell.EventKey:
		key := ev.Key()
		mods := ev.Modifiers()

		// Quit
		if key == keyQuit {
			s.Close()
			return
		}

		// Arrow + modifiers for pane operations
		switch key {
		case tcell.KeyUp, tcell.KeyDown, tcell.KeyLeft, tcell.KeyRight:
			d := tcell.KeyUp
			//sz := key // reuse variable for simplicity
			var d Direction
			switch key {
			case tcell.KeyUp:
				d = DirUp
			case tcell.KeyDown:
				d = DirDown
			case tcell.KeyLeft:
				d = DirLeft
			case tcell.KeyRight:
				d = DirRight
			}

			// Alt + arrows: move
			if mods&tcell.ModAlt != 0 {
				s.moveActivePane(d)
				s.requestRefresh()
				return
			}
			// Ctrl + arrows: split
			if mods&tcell.ModCtrl != 0 {
				s.splitActivePane(d)
				s.requestRefresh()
				return
			}
			// Shift + arrows: swap
			if mods&tcell.ModShift != 0 {
				s.swapActivePane(d)
				s.requestRefresh()
				return
			}
		}

		// Switch pane (Ctrl+A)
		if key == keySwitchPane {
			if len(s.panes) > 0 {
				s.mu.Lock()
				s.panes[s.activePaneIndex].AddEffect(s.fadeEffect)
				s.activePaneIndex = (s.activePaneIndex + 1) % len(s.panes)
				s.panes[s.activePaneIndex].ClearEffects()
				s.mu.Unlock()
				s.requestRefresh()
			}
			return
		}

		// Delegate other keys to the active pane
		if len(s.panes) > 0 {
			s.panes[s.activePaneIndex].app.HandleKey(ev)
		}

	case *tcell.EventResize:
		s.handleResize()
		s.requestRefresh()
	}
}

// compositePanes draws each pane’s buffer to the screen.
func (s *Screen) compositePanes() {
	for _, p := range s.panes {
		appBuffer := p.app.Render()

		for _, effect := range p.effects {
			appBuffer = effect.Apply(appBuffer)
		}

		if p.prevBuf == nil {
			s.blit(p.absX0, p.absY0, appBuffer)
		} else {
			s.blitDiff(p.absX0, p.absY0, p.prevBuf, appBuffer)
		}
	}
}

// requestRefresh signals the main loop to redraw.
func (s *Screen) requestRefresh() {
	select {
	case s.refreshChan <- true:
	default:
	}
}

// draw executes the final screen update.
func (s *Screen) draw() {
	s.compositePanes()
	s.drawBorders()
	s.tcellScreen.Show()
}

func (s *Screen) drawBorders() {
}

// Close shuts down tcell and stops all hosted apps.
func (s *Screen) Close() {
	s.closeOnce.Do(func() {
		close(s.quit)

		for _, p := range s.panes {
			p.app.Stop()
		}

		s.tcellScreen.Fini()
	})
}

// ForceResize triggers a layout recalculation.
func (s *Screen) ForceResize() {
	s.handleResize()
}

// handleResize recalculates pane dimensions and notifies apps.
func (s *Screen) handleResize() {
	w, h := s.tcellScreen.Size()

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

		width, height := x1-x0, y1-y0
		p.app.Resize(width, height)
		p.prevBuf = nil
	}
}

// blit copies cells to the screen.
func (s *Screen) blit(x, y int, buf [][]Cell) {
	for r, row := range buf {
		for c, cell := range row {
			s.tcellScreen.SetContent(x+c, y+r, cell.Ch, nil, cell.Style)
		}
	}
}

// blitDiff only redraws changed cells.
func (s *Screen) blitDiff(x0, y0 int, oldBuf, buf [][]Cell) {
	for y, row := range buf {
		for x, cell := range row {
			if y >= len(oldBuf) || x >= len(oldBuf[y]) || cell != oldBuf[y][x] {
				s.tcellScreen.SetContent(x0+x, y0+y, cell.Ch, nil, cell.Style)
			}
		}
	}
}

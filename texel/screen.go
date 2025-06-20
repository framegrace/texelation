package texel

import (
	//	"fmt"
	"github.com/gdamore/tcell/v2"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
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
	mu              sync.Mutex
	closeOnce       sync.Once
	styleCache      map[styleKey]tcell.Style
	resizing        bool
	resizeSnap      map[*Pane]struct {
		buf          [][]Cell
		prevW, prevH int
	}
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

	refreshTimer := time.NewTimer(time.Hour) // Use a long duration initially
	if !refreshTimer.Stop() {
		<-refreshTimer.C
	}

	scr := &Screen{
		tcellScreen:     tcellScreen,
		panes:           make([]*Pane, 0),
		activePaneIndex: 0,
		quit:            make(chan struct{}),
		refreshChan:     make(chan bool, 1),
		styleCache:      make(map[styleKey]tcell.Style),
	}

	scr.fadeEffect = NewFadeEffect(scr, tcell.ColorBlack, 0.25)

	return scr, nil
}

// getStyle looks up (or builds and caches) a tcell.Style
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
			if dirty && !s.resizing {
				s.draw()
				dirty = false
			}
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
		s.requestRefresh()
	}
}

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
	//	s.tcellScreen.Clear()
	s.compositePanes()
	s.drawBorders()
	s.tcellScreen.Show() // Replaces termbox.Flush()
	if s.resizing {
		s.resizing = false
		s.resizeSnap = nil
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

func (s *Screen) blitDiff(x0, y0 int, oldBuf, buf [][]Cell) {
	for y, row := range buf {
		for x, cell := range row {
			if y >= len(oldBuf) ||
				x >= len(oldBuf[y]) ||
				cell != oldBuf[y][x] {
				s.tcellScreen.SetContent(x0+x, y0+y, cell.Ch, nil, cell.Style)
			}
		}
	}
}

func (s *Screen) ForceResize() {
	s.handleResize()
}

func (s *Screen) handleResize() {
	// 1) Get new terminal size
	termW, termH := s.tcellScreen.Size()
	//s.tcellScreen.HideCursor()

	if !s.resizing {
		s.resizing = true
		s.resizeSnap = make(map[*Pane]struct {
			buf          [][]Cell
			prevW, prevH int
		}, len(s.panes))
		for _, p := range s.panes {
			if p.prevBuf != nil {
				s.resizeSnap[p] = struct {
					buf          [][]Cell
					prevW, prevH int
				}{
					buf:   cloneBuffer(p.prevBuf),
					prevW: p.Width(),
					prevH: p.Height(),
				}
			}
		}
	}

	// layout & resize apps
	for _, p := range s.panes {
		x0 := int(p.Layout.X * float64(termW))
		y0 := int(p.Layout.Y * float64(termH))
		x1 := int((p.Layout.X + p.Layout.W) * float64(termW))
		y1 := int((p.Layout.Y + p.Layout.H) * float64(termH))
		if p.Layout.X+p.Layout.W >= 1.0 {
			x1 = termW
		}
		if p.Layout.Y+p.Layout.H >= 1.0 {
			y1 = termH
		}
		p.SetDimensions(x0, y0, x1, y1)
		p.app.Resize(x1-x0, y1-y0)
		// leave prevBuf for zoom
	}

	for _, p := range s.panes {
		if snap, ok := s.resizeSnap[p]; ok && snap.prevW > 0 && snap.prevH > 0 {
			scaled := scaleBuffer(snap.buf, snap.prevW, snap.prevH, p.Width(), p.Height())
			s.blit(p.absX0, p.absY0, scaled)
		}
	}
	s.drawBorders()
	s.tcellScreen.Show()

	for _, p := range s.panes {
		p.prevBuf = nil
	}
}

// cloneBuffer makes a deep copy of a [][]Cell:
func cloneBuffer(src [][]Cell) [][]Cell {
	dst := make([][]Cell, len(src))
	for y := range src {
		dst[y] = make([]Cell, len(src[y]))
		copy(dst[y], src[y])
	}
	return dst
}

// scaleBuffer does nearest-neighbor text scaling from old→new dims:
func scaleBuffer(src [][]Cell, oldW, oldH, newW, newH int) [][]Cell {
	dst := make([][]Cell, newH)
	for y := 0; y < newH; y++ {
		// pick source row
		sy := int(float64(y) * float64(oldH) / float64(newH))
		if sy >= oldH {
			sy = oldH - 1
		}
		dst[y] = make([]Cell, newW)
		for x := 0; x < newW; x++ {
			sx := int(float64(x) * float64(oldW) / float64(newW))
			if sx >= oldW {
				sx = oldW - 1
			}
			dst[y][x] = src[sy][sx]
		}
	}
	return dst
}

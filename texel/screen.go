package texel

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"golang.org/x/term"
	"log"
	"math"
	"os"
	"os/signal"
	"regexp"
	"strconv"
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
const eps = 1e-9

type PaneFactory func(layout Rect) *Pane

const (
	keyQuit       = tcell.KeyCtrlQ
	keyClose      = tcell.KeyCtrlX
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
	neighbors       map[int]map[Direction][]int
	mu              sync.Mutex
	closeOnce       sync.Once
	styleCache      map[styleKey]tcell.Style
	DefaultFgColor  tcell.Color
	DefaultBgColor  tcell.Color
	// Factory to create a new shell pane (injected by main)
	ShellPaneFactory PaneFactory
}

// NewScreen initializes the terminal with tcell.
func NewScreen() (*Screen, error) {
	defaultFg, defaultBg, err := initDefaultColors()
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
		neighbors:       make(map[int]map[Direction][]int),
		activePaneIndex: 0,
		quit:            make(chan struct{}),
		refreshChan:     make(chan bool, 1),
		styleCache:      make(map[styleKey]tcell.Style),
		DefaultFgColor:  defaultFg,
		DefaultBgColor:  defaultBg,
	}

	scr.fadeEffect = NewFadeEffect(scr, tcell.NewRGBColor(60, 60, 60), 0.25)

	return scr, nil
}

func initDefaultColors() (tcell.Color, tcell.Color, error) {
	fg, err := queryDefaultColor(10)
	if err != nil {
		fg = tcell.ColorRed
	}
	bg, err := queryDefaultColor(11)
	if err != nil {
		bg = tcell.ColorDarkRed
	}
	return fg, bg, err
}

func queryDefaultColor(code int) (tcell.Color, error) {
	// 1) open the controlling terminal
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return tcell.ColorDefault, fmt.Errorf("open /dev/tty: %w", err)
	}
	defer tty.Close()

	// 2) put it into raw mode (disable canonical + echo)
	oldState, err := term.MakeRaw(int(tty.Fd()))
	if err != nil {
		return tcell.ColorDefault, fmt.Errorf("MakeRaw: %w", err)
	}
	defer term.Restore(int(tty.Fd()), oldState)

	// 3) send the OSC query
	seq := fmt.Sprintf("\x1b]%d;?\a", code)
	if _, err := tty.WriteString(seq); err != nil {
		return tcell.ColorDefault, err
	}

	// 4) read until BEL (\a) or timeout
	resp := make([]byte, 0, 64)
	buf := make([]byte, 1)

	deadline := time.Now().Add(500 * time.Millisecond)
	if err := tty.SetReadDeadline(deadline); err != nil {
		return tcell.ColorDefault, err
	}

	for {
		n, err := tty.Read(buf)
		if err != nil {
			return tcell.ColorDefault, fmt.Errorf("read reply: %w", err)
		}
		resp = append(resp, buf[:n]...)
		if buf[0] == '\a' {
			break
		}
	}

	// 5) parse ESC ] code ; rgb:RR/GG/BB BEL
	pattern := fmt.Sprintf(`\x1b\]%d;rgb:([0-9A-Fa-f]{4})/([0-9A-Fa-f]{4})/([0-9A-Fa-f]{4})`, code)
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(string(resp))
	if len(m) != 4 {
		return tcell.ColorDefault, fmt.Errorf("unexpected reply: %q", resp)
	}

	hex2int := func(s string) (int32, error) {
		v, err := strconv.ParseInt(s, 16, 32)
		return int32(v), err
	}
	r, _ := hex2int(m[1])
	g, _ := hex2int(m[2])
	b, _ := hex2int(m[3])
	return tcell.NewRGBColor(r, g, b), nil
}

// updateNeighbors recalculates the neighbor map for all panes.
func (s *Screen) updateNeighbors() {
	w, h := s.tcellScreen.Size()

	// init map[int]map[Direction][]int
	s.neighbors = make(map[int]map[Direction][]int, len(s.panes))
	for i := range s.panes {
		s.neighbors[i] = map[Direction][]int{
			DirUp:    {},
			DirDown:  {},
			DirLeft:  {},
			DirRight: {},
		}
	}

	// compare every pair
	for i, p := range s.panes {
		rl := p.Layout
		x0 := int(rl.X * float64(w))
		y0 := int(rl.Y * float64(h))
		x1 := int((rl.X + rl.W) * float64(w))
		y1 := int((rl.Y + rl.H) * float64(h))

		for j, q := range s.panes {
			if i == j {
				continue
			}
			ol := q.Layout
			x0o := int(ol.X * float64(w))
			y0o := int(ol.Y * float64(h))
			x1o := int((ol.X + ol.W) * float64(w))
			y1o := int((ol.Y + ol.H) * float64(h))

			// right neighbor: touches vertically and to right
			if x1 == x0o && y0o < y1 && y1o > y0 {
				s.neighbors[i][DirRight] = append(s.neighbors[i][DirRight], j)
			}
			// left neighbor
			if x0 == x1o && y0o < y1 && y1o > y0 {
				s.neighbors[i][DirLeft] = append(s.neighbors[i][DirLeft], j)
			}
			// down neighbor
			if y1 == y0o && x0o < x1 && x1o > x0 {
				s.neighbors[i][DirDown] = append(s.neighbors[i][DirDown], j)
			}
			// up neighbor
			if y0 == y1o && x0o < x1 && x1o > x0 {
				s.neighbors[i][DirUp] = append(s.neighbors[i][DirUp], j)
			}
		}
	}
}

func (s *Screen) updateActiveEffects() {
	// Clear previous buffer and effects, then reapply
	for i, pane := range s.panes {
		pane.prevBuf = nil
		pane.ClearEffects()
		if i != s.activePaneIndex {
			pane.AddEffect(s.fadeEffect)
		}
	}
}

// splitActivePane splits the active pane in the given direction, adding a new pane.
func (s *Screen) splitActivePane(d Direction) {
	log.Printf("Split dir: %i", d)
	if s.ShellPaneFactory == nil {
		log.Panic("ShellPaneFactory not set")
	}
	idx := s.activePaneIndex
	orig := s.panes[idx]
	layout := orig.Layout
	var a, b Rect
	switch d {
	case DirLeft, DirRight:
		hw := layout.W / 2
		a, b = Rect{layout.X, layout.Y, hw, layout.H}, Rect{layout.X + hw, layout.Y, hw, layout.H}
	case DirUp, DirDown:
		hh := layout.H / 2
		a, b = Rect{layout.X, layout.Y, layout.W, hh}, Rect{layout.X, layout.Y + hh, layout.W, hh}
	}
	orig.Layout, orig.prevBuf = a, nil
	newPane := s.ShellPaneFactory(b)
	s.AddPane(newPane)
}

// moveActivePane moves focus to the neighbor in the given direction.
func (s *Screen) moveActivePane(d Direction) {
	log.Printf("Move dir: %d", d) // use %d, not %i
	if nbrs, ok := s.neighbors[s.activePaneIndex][d]; ok && len(nbrs) > 0 {
		// pick the first neighbour in the slice
		s.setActivePane(nbrs[0])
	}
}

// contains reports whether sup fully contains sub (within eps).
func contains(sup, sub Rect) bool {
	return sub.X+eps >= sup.X &&
		sub.Y+eps >= sup.Y &&
		sub.X+sub.W <= sup.X+sup.W+eps &&
		sub.Y+sub.H <= sup.Y+sup.H+eps
}

func (s *Screen) closeActivePane() {
	if len(s.panes) <= 1 {
		return
	}

	// 1) rebuild adjacency
	s.updateNeighbors()

	// 2) identify removal
	idx := s.activePaneIndex
	removed := s.panes[idx].Layout
	oldNbrs := s.neighbors[idx] // map[Direction][]int

	// 3) for each direction in priority, build per-neighbour fused rects and
	//    check that none of them would engulf any other pane.
	order := []Direction{DirUp, DirLeft, DirDown, DirRight}

	// chosen neighbours + their fused layouts
	var (
		nbrsOnSide []int
		fusedRects = make(map[int]Rect)
	)
	found := false

	for _, d := range order {
		list := oldNbrs[d]
		if len(list) == 0 {
			continue
		}

		// compute fused rect for each neighbour
		tmp := make(map[int]Rect, len(list))
		for _, oldIdx := range list {
			tmp[oldIdx] = union(s.panes[oldIdx].Layout, removed)
		}

		// check conflict: no fused rect should fully contain any other pane
		conflict := false
		for k, p := range s.panes {
			if k == idx {
				continue
			}
			// skip those neighbours themselves
			isNbr := false
			for _, oldIdx := range list {
				if k == oldIdx {
					isNbr = true
					break
				}
			}
			if isNbr {
				continue
			}
			// if any fused rect contains this third pane, direction d is unsafe
			for _, fr := range tmp {
				if contains(fr, p.Layout) {
					conflict = true
					break
				}
			}
			if conflict {
				break
			}
		}

		if !conflict {
			// we can use direction d and expand all of its neighbours
			nbrsOnSide = list
			fusedRects = tmp
			found = true
			break
		}
	}

	// 4) fallback: no fully safe direction → just take every neighbour on first non-empty side
	if !found {
		for _, d := range order {
			list := oldNbrs[d]
			if len(list) == 0 {
				continue
			}
			nbrsOnSide = list
			for _, oldIdx := range list {
				fusedRects[oldIdx] = union(s.panes[oldIdx].Layout, removed)
			}
			break
		}
	}

	// 5) pick new active pane = first neighbour of that chosen side
	if len(nbrsOnSide) > 0 {
		newIdx := nbrsOnSide[0]
		if newIdx > idx {
			newIdx-- // shift index after removal
		}
		s.activePaneIndex = newIdx
	} else {
		// should never happen—just pick 0
		s.activePaneIndex = 0
	}

	// 6) remove the pane from the slice
	s.panes[idx].Close()
	s.panes = append(s.panes[:idx], s.panes[idx+1:]...)

	// 7) apply each fused layout to its neighbour
	for oldIdx, fr := range fusedRects {
		newI := oldIdx
		if newI > idx {
			newI-- // shift past removed
		}
		s.panes[newI].Layout = fr
	}

	// 8) reflow & redraw
	s.updateNeighbors()
	s.ForceResize()
	for _, p := range s.panes {
		p.prevBuf = nil
	}
	s.requestRefresh()
}

func almostEqual(a, b float64) bool {
	const eps = 1e-9
	return math.Abs(a-b) < eps
}

func (s *Screen) setActivePane(n int) {
	s.activePaneIndex = n
	s.updateActiveEffects()
	s.ForceResize()
	for _, p := range s.panes {
		p.prevBuf = nil
	}
}

// swapActivePane swaps layouts of the active pane with its neighbor.
func (s *Screen) swapActivePane(d Direction) {
	log.Printf("Swap dir: %i", d)
	if nbrs, ok := s.neighbors[s.activePaneIndex][d]; ok && len(nbrs) >= 0 {
		p, q := s.panes[s.activePaneIndex], s.panes[nbrs[0]]
		p.Layout, q.Layout = q.Layout, p.Layout
		p.prevBuf, q.prevBuf = nil, nil
		s.updateNeighbors()
		s.ForceResize()
		for _, p := range s.panes {
			p.prevBuf = nil
		}
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
	p.app.SetRefreshNotifier(s.refreshChan)
	go func() {
		if err := p.app.Run(); err != nil {
			log.Printf("App '%s' exited with error: %v", p.app.GetTitle(), err)
		}
	}()
	s.updateNeighbors()
	s.setActivePane(len(s.panes) - 1)
	s.ForceResize()
	for _, pane := range s.panes {
		pane.prevBuf = nil
	}
	s.requestRefresh()
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
		r := ev.Rune()
		mods := ev.Modifiers()

		msg := fmt.Sprintf("Key=%v Rune=%q Mods=%b", key, r, mods)
		log.Println(msg)

		// Quit
		if key == keyQuit {
			s.Close()
			return
		}

		if key == keyClose {
			s.closeActivePane()
			return
		}

		// Arrow + modifiers for pane operations
		switch key {
		case tcell.KeyUp, tcell.KeyDown, tcell.KeyLeft, tcell.KeyRight:
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
				s.swapActivePane(d)
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
				s.moveActivePane(d)
				s.requestRefresh()
				return
			}
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

func adjacent(a, b Rect) bool {
	vOverlap := a.Y < b.Y+b.H && b.Y < a.Y+a.H
	hOverlap := a.X < b.X+b.W && b.X < a.X+a.W

	// right edge of a touches left of b
	if math.Abs(a.X+a.W-b.X) < 1e-9 && vOverlap {
		return true
	}
	// left edge of a touches right of b
	if math.Abs(b.X+b.W-a.X) < 1e-9 && vOverlap {
		return true
	}
	// bottom edge of a touches top of b
	if math.Abs(a.Y+a.H-b.Y) < 1e-9 && hOverlap {
		return true
	}
	// top edge of a touches bottom of b
	if math.Abs(b.Y+b.H-a.Y) < 1e-9 && hOverlap {
		return true
	}
	return false
}

func union(a, b Rect) Rect {
	minX := math.Min(a.X, b.X)
	minY := math.Min(a.Y, b.Y)
	maxX := math.Max(a.X+a.W, b.X+b.W)
	maxY := math.Max(a.Y+a.H, b.Y+b.H)
	return Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
}

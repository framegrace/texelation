package texel

import (
	"context"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"golang.org/x/term"
	"log"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"
)

type Direction int

const (
	DirUp Direction = iota
	DirDown
	DirLeft
	DirRight
)

// DebuggableApp is an interface that apps can implement to provide
// detailed state information for debugging purposes.
type DebuggableApp interface {
	DumpState(frameNum int)
}

// Side defines the placement of a StatusPane.
type Side int

const (
	SideTop Side = iota
	SideBottom
	SideLeft
	SideRight
)

type AppFactory func() App

const (
	keyControlMode = tcell.KeyCtrlA
	keyQuit        = tcell.KeyCtrlQ
)

const (
	ResizeStep             float64       = 0.05 // Resize by 5%
	MinRatio               float64       = 0.1  // Panes can't be smaller than 10%
	resizeDebounceDuration time.Duration = 100 * time.Millisecond
)

type styleKey struct {
	fg, bg          tcell.Color
	bold, underline bool
	reverse         bool
}

// StatusPane is a special pane with absolute sizing, placed on one side of the screen.
type StatusPane struct {
	app  App
	side Side
	size int // rows for Top/Bottom, cols for Left/Right
}

type selectedBorder struct {
	node  *Node // The parent node whose children are being resized (the split node)
	index int   // The index of the left/top pane of the border. The border is between child[index] and child[index+1].
}

// Screen manages the entire terminal display using tcell as the backend.
type Screen struct {
	tcellScreen                    tcell.Screen
	tree                           *Tree
	statusPanes                    []*StatusPane
	inactiveFadePrototype          Effect
	controlModeFadeEffectPrototype Effect
	ditherEffectPrototype          Effect
	quit                           chan struct{}
	refreshChan                    chan bool
	drawChan                       chan bool
	mu                             sync.Mutex
	closeOnce                      sync.Once
	styleCache                     map[styleKey]tcell.Style
	DefaultFgColor                 tcell.Color
	DefaultBgColor                 tcell.Color
	ShellAppFactory                AppFactory

	// Control Mode State
	inControlMode   bool
	subControlMode  rune
	effectAnimators map[*FadeEffect]context.CancelFunc

	resizeSelection   *selectedBorder
	debugFramesToDump int

	// --- RESIZE DEBOUNCING ---
	resizeTimer *time.Timer
	resizeMutex sync.Mutex
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

	scr := &Screen{
		tcellScreen:     tcellScreen,
		tree:            NewTree(),
		statusPanes:     make([]*StatusPane, 0),
		quit:            make(chan struct{}),
		refreshChan:     make(chan bool, 1),
		drawChan:        make(chan bool, 1),
		styleCache:      make(map[styleKey]tcell.Style),
		DefaultFgColor:  defaultFg,
		DefaultBgColor:  defaultBg,
		effectAnimators: make(map[*FadeEffect]context.CancelFunc),
		resizeSelection: nil,
	}
	scr.inactiveFadePrototype = NewFadeEffect(scr, tcell.NewRGBColor(20, 20, 20), 0.5)
	scr.controlModeFadeEffectPrototype = NewFadeEffect(scr, tcell.NewRGBColor(0, 50, 0), 0.2, WithIsControl(true))
	scr.ditherEffectPrototype = NewDitherEffect('░')

	return scr, nil
}

func (s *Screen) Refresh() {
	select {
	case s.refreshChan <- true:
	default:
	}
}

func (s *Screen) RequestDraw() {
	select {
	case s.drawChan <- true:
	default:
	}
}

// broadcastEvent sends an event to all panes.
func (s *Screen) broadcastEvent(event Event) {
	s.tree.Traverse(func(node *Node) {
		if node.Pane != nil {
			node.Pane.HandleEvent(event)
		}
	})
}

func (s *Screen) addStandardEffects(p *pane) {
	p.AddEffect(s.inactiveFadePrototype.Clone())
	p.AddEffect(s.controlModeFadeEffectPrototype.Clone())
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
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return tcell.ColorDefault, fmt.Errorf("open /dev/tty: %w", err)
	}
	defer tty.Close()

	oldState, err := term.MakeRaw(int(tty.Fd()))
	if err != nil {
		return tcell.ColorDefault, fmt.Errorf("MakeRaw: %w", err)
	}
	defer term.Restore(int(tty.Fd()), oldState)

	seq := fmt.Sprintf("\x1b]%d;?\a", code)
	if _, err := tty.WriteString(seq); err != nil {
		return tcell.ColorDefault, err
	}

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

func (s *Screen) broadcastStateUpdate() {
	title := s.tree.GetActiveTitle()

	msg := Message{
		Type: MsgStateUpdate,
		Payload: StatePayload{
			InControlMode: s.inControlMode,
			SubMode:       s.subControlMode,
			ActiveTitle:   title,
		},
	}
	s.tree.Traverse(func(node *Node) {
		if node.Pane != nil {
			node.Pane.app.HandleMessage(msg)
		}
	})

	for _, sp := range s.statusPanes {
		sp.app.HandleMessage(msg)
	}
}

// AddStatusPane adds a new status pane to the screen.
func (s *Screen) AddStatusPane(app App, side Side, size int) {
	sp := &StatusPane{
		app:  app,
		side: side,
		size: size,
	}
	s.statusPanes = append(s.statusPanes, sp)

	app.SetRefreshNotifier(s.refreshChan)
	go func() {
		if err := app.Run(); err != nil {
			log.Printf("Status pane app '%s' exited with error: %v", app.GetTitle(), err)
		}
	}()

	s.recalculateLayout()
}

// moveActivePane moves focus to the neighbor in the given direction.
func (s *Screen) moveActivePane(d Direction) {
	s.tree.MoveActive(d)
	s.recalculateLayout()
	s.broadcastEvent(Event{Type: EventActivePaneChanged})
	s.broadcastStateUpdate()
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
func (s *Screen) AddApp(app App) {
	p := newPane(app)
	s.addStandardEffects(p)
	p.app.SetRefreshNotifier(s.refreshChan)

	s.tree.AddApp(p)

	s.recalculateLayout()
	s.broadcastEvent(Event{Type: EventActivePaneChanged})
	s.broadcastStateUpdate()
	go p.app.Run()
}

// Run starts the main event and rendering loop.
func (s *Screen) Run() error {
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
	s.recalculateLayout()
	s.broadcastStateUpdate()
	s.draw()
	dirty = false

	for {
		select {
		case ev := <-eventChan:
			s.handleEvent(ev)
		case <-s.refreshChan:
			s.broadcastStateUpdate()
			dirty = true
		case <-s.drawChan: // New case for visual-only updates
			dirty = true
		case <-ticker.C:
			var needsContinuousUpdate bool
			s.tree.Traverse(func(node *Node) {
				if node != nil && node.Pane != nil {
					for _, effect := range node.Pane.effects {
						if effect.IsContinuous() {
							needsContinuousUpdate = true
							break
						}
					}
				}
			})
			if needsContinuousUpdate {
				dirty = true
			}
		case <-s.quit:
			return nil
		}

		if dirty {
			s.draw()
			dirty = false
		}
	}
}

// handleEvent processes key and resize events, including our custom pane controls.
func (s *Screen) handleEvent(ev tcell.Event) {
	switch ev := ev.(type) {
	case *tcell.EventKey:
		if ev.Key() == keyControlMode {
			s.inControlMode = !s.inControlMode
			s.subControlMode = 0
			if s.inControlMode {
				s.broadcastEvent(Event{Type: EventControlOn})
			} else {
				s.broadcastEvent(Event{Type: EventControlOff})
			}
			s.broadcastStateUpdate()
			s.requestRefresh()
			return
		}

		if s.inControlMode {
			s.handleControlMode(ev)
			return
		}

		if ev.Key() == keyQuit {
			s.Close()
			return
		}

		if ev.Modifiers()&tcell.ModShift != 0 {
			switch ev.Key() {
			case tcell.KeyUp:
				s.moveActivePane(DirUp)
			case tcell.KeyDown:
				s.moveActivePane(DirDown)
			case tcell.KeyLeft:
				s.moveActivePane(DirLeft)
			case tcell.KeyRight:
				s.moveActivePane(DirRight)
			}
			s.requestRefresh()
			return
		}

		if s.tree.ActiveLeaf != nil && s.tree.ActiveLeaf.Pane != nil {
			s.tree.ActiveLeaf.Pane.app.HandleKey(ev)
			s.requestRefresh()
		}

	case *tcell.EventResize:
		s.resizeMutex.Lock()
		if s.resizeTimer != nil {
			s.resizeTimer.Stop()
		}
		s.resizeTimer = time.AfterFunc(resizeDebounceDuration, s.performResize)
		s.resizeMutex.Unlock()
	}
}

// performResize contains the actual resize logic, called by the debouncing timer.
func (s *Screen) performResize() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tcellScreen.Sync()
	s.recalculateLayout()
	s.requestRefresh()
}

func (s *Screen) handleControlMode(ev *tcell.EventKey) {
	if ev.Key() == tcell.KeyEsc {
		if s.resizeSelection != nil {
			s.resizeSelection = nil
			s.requestRefresh()
			return
		}

		s.inControlMode = false
		s.subControlMode = 0
		s.broadcastEvent(Event{Type: EventControlOff})
		s.broadcastEvent(Event{Type: EventActivePaneChanged})
		s.broadcastStateUpdate()
		s.requestRefresh()
		return
	}

	if ev.Modifiers()&tcell.ModCtrl != 0 {
		if keyToDirection(ev) != -1 {
			s.handleInteractiveResize(ev)
			return
		}
	}

	if s.resizeSelection != nil {
		return
	}
	if s.subControlMode != 0 {
		switch s.subControlMode {
		case 'w':
			var d Direction
			validDir := true
			switch ev.Key() {
			case tcell.KeyUp:
				d = DirUp
			case tcell.KeyDown:
				d = DirDown
			case tcell.KeyLeft:
				d = DirLeft
			case tcell.KeyRight:
				d = DirRight
			default:
				validDir = false
			}
			if validDir {
				s.tree.SwapActivePane(d)
			}
		}
		s.subControlMode = 0
		s.inControlMode = false
		s.broadcastEvent(Event{Type: EventControlOff})
		s.broadcastStateUpdate()
		s.requestRefresh()
		return
	}

	switch ev.Rune() {
	case 'x':
		s.tree.CloseActiveLeaf()
	case 'w':
		s.subControlMode = 'w'
		s.broadcastStateUpdate()
		s.requestRefresh()
		return
	case '|':
		s.performSplit(Vertical)
	case '-':
		s.performSplit(Horizontal)
	}

	s.inControlMode = false
	s.broadcastEvent(Event{Type: EventControlOff})
	s.broadcastEvent(Event{Type: EventActivePaneChanged})
	s.broadcastStateUpdate()
	s.requestRefresh()
}

// compositePanes draws each pane’s buffer to the screen.
func (s *Screen) compositePanes() {
	s.tree.Traverse(func(node *Node) {
		if node.Pane != nil {
			p := node.Pane
			appBuffer := p.app.Render()

			for _, effect := range p.effects {
				appBuffer = effect.Apply(appBuffer)
			}

			if p.prevBuf == nil {
				s.blit(p.absX0, p.absY0, appBuffer)
			} else {
				s.blitDiff(p.absX0, p.absY0, p.prevBuf, appBuffer)
			}

			newPrevBuf := make([][]Cell, len(appBuffer))
			for i := range appBuffer {
				newPrevBuf[i] = make([]Cell, len(appBuffer[i]))
				copy(newPrevBuf[i], appBuffer[i])
			}
			p.prevBuf = newPrevBuf
		}
	})
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
	if s.debugFramesToDump > 0 {
		s.dumpGridState(s.tree.Root, 6-s.debugFramesToDump)
		s.debugFramesToDump--
	}

	s.compositePanes()
	s.drawStatusPanes()
	s.tcellScreen.Show()
}

func (s *Screen) drawStatusPanes() {
	w, h := s.tcellScreen.Size()
	topOffset, bottomOffset, leftOffset, rightOffset := 0, 0, 0, 0

	for _, sp := range s.statusPanes {
		switch sp.side {
		case SideTop:
			buf := sp.app.Render()
			s.blit(leftOffset, topOffset, buf)
			topOffset += sp.size
		case SideBottom:
			buf := sp.app.Render()
			s.blit(leftOffset, h-bottomOffset-sp.size, buf)
			bottomOffset += sp.size
		case SideLeft:
			buf := sp.app.Render()
			s.blit(leftOffset, topOffset, buf)
			leftOffset += sp.size
		case SideRight:
			buf := sp.app.Render()
			s.blit(w-rightOffset-sp.size, topOffset, buf)
			rightOffset += sp.size
		}
	}
}

// Close shuts down tcell and stops all hosted apps.
func (s *Screen) Close() {
	s.closeOnce.Do(func() {
		close(s.quit)

		for _, cancel := range s.effectAnimators {
			cancel()
		}

		s.tree.Traverse(func(node *Node) {
			if node.Pane != nil {
				node.Pane.app.Stop()
			}
		})

		for _, sp := range s.statusPanes {
			sp.app.Stop()
		}

		s.tcellScreen.Fini()
	})
}

func (s *Screen) recalculateLayout() {
	w, h := s.tcellScreen.Size()
	mainX, mainY := 0, 0
	mainW, mainH := w, h

	topOffset, bottomOffset, leftOffset, rightOffset := 0, 0, 0, 0

	for _, sp := range s.statusPanes {
		switch sp.side {
		case SideTop:
			sp.app.Resize(w, sp.size)
			topOffset += sp.size
		case SideBottom:
			sp.app.Resize(w, sp.size)
			bottomOffset += sp.size
		case SideLeft:
			sp.app.Resize(sp.size, h-topOffset-bottomOffset)
			leftOffset += sp.size
		case SideRight:
			sp.app.Resize(sp.size, h-topOffset-bottomOffset)
			rightOffset += sp.size
		}
	}

	mainX = leftOffset
	mainY = topOffset
	mainW = w - leftOffset - rightOffset
	mainH = h - topOffset - bottomOffset

	s.tree.Resize(mainX, mainY, mainW, mainH)
}

func (s *Screen) handleInteractiveResize(ev *tcell.EventKey) {
	d := keyToDirection(ev)

	if s.resizeSelection == nil {
		var border *selectedBorder
		curr := s.tree.ActiveLeaf
		for curr.Parent != nil {
			parent := curr.Parent

			if (d == DirLeft || d == DirRight) && parent.Split == Vertical {
				for i, child := range parent.Children {
					if child == curr {
						if d == DirRight && i < len(parent.Children)-1 {
							border = &selectedBorder{node: parent, index: i}
						} else if d == DirLeft && i > 0 {
							border = &selectedBorder{node: parent, index: i - 1}
						}
						break
					}
				}
			} else if (d == DirUp || d == DirDown) && parent.Split == Horizontal {
				for i, child := range parent.Children {
					if child == curr {
						if d == DirDown && i < len(parent.Children)-1 {
							border = &selectedBorder{node: parent, index: i}
						} else if d == DirUp && i > 0 {
							border = &selectedBorder{node: parent, index: i - 1}
						}
						break
					}
				}
			}

			if border != nil {
				break
			}
			curr = parent
		}

		s.resizeSelection = border
		s.ForceResize()
		s.requestRefresh()
		return
	}

	border := s.resizeSelection

	if !(((d == DirLeft || d == DirRight) && border.node.Split == Vertical) ||
		((d == DirUp || d == DirDown) && border.node.Split == Horizontal)) {
		return
	}

	leftPaneIndex := border.index
	rightPaneIndex := border.index + 1

	var growerIndex, shrinkerIndex int
	if d == DirRight || d == DirDown {
		growerIndex = leftPaneIndex
		shrinkerIndex = rightPaneIndex
	} else {
		growerIndex = rightPaneIndex
		shrinkerIndex = leftPaneIndex
	}

	if border.node.SplitRatios[shrinkerIndex] <= MinRatio {
		return
	}

	transferAmount := ResizeStep
	if border.node.SplitRatios[shrinkerIndex]-transferAmount < MinRatio {
		transferAmount = border.node.SplitRatios[shrinkerIndex] - MinRatio
	}
	if transferAmount <= 0 {
		return
	}

	border.node.SplitRatios[growerIndex] += transferAmount
	border.node.SplitRatios[shrinkerIndex] -= transferAmount

	s.recalculateLayout()
	s.requestRefresh()
}

func keyToDirection(ev *tcell.EventKey) Direction {
	switch ev.Key() {
	case tcell.KeyUp:
		return DirUp
	case tcell.KeyDown:
		return DirDown
	case tcell.KeyLeft:
		return DirLeft
	case tcell.KeyRight:
		return DirRight
	}
	return -1
}

func (s *Screen) performSplit(splitDir SplitType) {
	if s.tree.ActiveLeaf == nil || s.ShellAppFactory == nil {
		return
	}

	newApp := s.ShellAppFactory()
	p := newPane(newApp)
	s.addStandardEffects(p)
	p.app.SetRefreshNotifier(s.refreshChan)
	go newApp.Run()

	s.tree.SplitActive(splitDir, p)
	s.recalculateLayout()
}

func (s *Screen) ForceResize() {
	s.recalculateLayout()
}

func (s *Screen) blit(x, y int, buf [][]Cell) {
	for r, row := range buf {
		for c, cell := range row {
			s.tcellScreen.SetContent(x+c, y+r, cell.Ch, nil, cell.Style)
		}
	}
}

func (s *Screen) blitDiff(x0, y0 int, oldBuf, buf [][]Cell) {
	for y, row := range buf {
		for x, cell := range row {
			if y >= len(oldBuf) || x >= len(oldBuf[y]) || cell != oldBuf[y][x] {
				s.tcellScreen.SetContent(x0+x, y0+y, cell.Ch, nil, cell.Style)
			}
		}
	}
}

func (s *Screen) dumpGridState(node *Node, frameNum int) {
	if node == nil {
		return
	}
	if node.Pane != nil && node.Pane.app != nil {
		if debuggable, ok := node.Pane.app.(DebuggableApp); ok {
			log.Printf("--- FRAME DUMP #%d for Pane at [%d,%d] (Size: %dx%d) ---", frameNum, node.Pane.absX0, node.Pane.absY0, node.Pane.Width(), node.Pane.Height())
			debuggable.DumpState(frameNum)
		}
	}

	for _, child := range node.Children {
		s.dumpGridState(child, frameNum)
	}
}

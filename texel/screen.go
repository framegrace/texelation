package texel

import (
	"context"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"golang.org/x/term"
	"log"
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

// Screen manages the entire terminal display using tcell as the backend.
type Screen struct {
	tcellScreen                    tcell.Screen
	root                           *Node
	activeLeaf                     *Node
	statusPanes                    []*StatusPane
	inactiveFadePrototype          Effect
	controlModeFadeEffectPrototype Effect
	ditherEffectPrototype          Effect
	quit                           chan struct{}
	refreshChan                    chan bool
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
		statusPanes:     make([]*StatusPane, 0),
		quit:            make(chan struct{}),
		refreshChan:     make(chan bool, 1),
		styleCache:      make(map[styleKey]tcell.Style),
		DefaultFgColor:  defaultFg,
		DefaultBgColor:  defaultBg,
		effectAnimators: make(map[*FadeEffect]context.CancelFunc),
	}
	//scr.inactiveFadePrototype = NewFadeEffect(scr, tcell.NewRGBColor(20, 20, 20), 0.8)
	scr.inactiveFadePrototype = NewVignetteEffect(scr, tcell.NewRGBColor(60, 60, 60), 0.5, WithFalloff(4.0))
	// The control mode effect applies to all panes
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

// broadcastEvent sends an event to all panes.
func (s *Screen) broadcastEvent(event Event) {
	s.traverse(s.root, func(node *Node) {
		log.Printf("Broadcasting Event: %s to %s ", event, node.Pane)
		if node.Pane != nil {
			node.Pane.HandleEvent(event)
		}
	})
}

func (s *Screen) addStandardEffects(p *pane) {
	p.AddEffect(s.inactiveFadePrototype.Clone())
	p.AddEffect(s.controlModeFadeEffectPrototype.Clone())
	//p.AddEffect(s.ditherEffectPrototype.Clone())
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

func (s *Screen) broadcastStateUpdate() {
	title := ""
	if s.activeLeaf != nil && s.activeLeaf.Pane != nil {
		title = s.activeLeaf.Pane.app.GetTitle()
	}

	msg := Message{
		Type: MsgStateUpdate,
		Payload: StatePayload{
			InControlMode: s.inControlMode,
			SubMode:       s.subControlMode,
			ActiveTitle:   title,
		},
	}
	s.traverse(s.root, func(node *Node) {
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

	s.ForceResize()
}

// splitActivePane splits the active pane in the given direction, adding a new pane.
func (s *Screen) splitActivePane(d Direction) {
	if s.ShellAppFactory == nil {
		log.Panic("ShellAppFractory not set")
	}

	leaf := s.activeLeaf
	if leaf == nil {
		return
	}

	parentX, parentY, parentW, parentH := leaf.Pane.absX0, leaf.Pane.absY0, leaf.Pane.Width(), leaf.Pane.Height()

	// Preserve the original pane
	originalPane := leaf.Pane
	leaf.Pane = nil // No longer a leaf
	leaf.Left = &Node{Parent: leaf}
	leaf.Right = &Node{Parent: leaf}

	newApp := s.ShellAppFactory()
	newPane := s.createAndInitPane(newApp)

	var newActiveLeaf *Node
	switch d {
	case DirLeft:
		leaf.Split = Vertical
		leaf.Left.Pane = newPane
		leaf.Right.Pane = originalPane
		newActiveLeaf = leaf.Left
	case DirRight:
		leaf.Split = Vertical
		leaf.Left.Pane = originalPane
		leaf.Right.Pane = newPane
		newActiveLeaf = leaf.Right
	case DirUp:
		leaf.Split = Horizontal
		leaf.Left.Pane = newPane
		leaf.Right.Pane = originalPane
		newActiveLeaf = leaf.Left
	case DirDown:
		leaf.Split = Horizontal
		leaf.Left.Pane = originalPane
		leaf.Right.Pane = newPane
		newActiveLeaf = leaf.Right
	}
	s.resizeNode(leaf, leaf.Layout, parentX, parentY, parentW, parentH)
	go newPane.app.Run()
	s.setActivePane(newActiveLeaf)
}

// moveActivePane moves focus to the neighbor in the given direction.
func (s *Screen) moveActivePane(d Direction) {
	target := findNeighbor(s.activeLeaf, d)
	if target != nil {
		s.setActivePane(target)
	}
}

func findNeighbor(leaf *Node, d Direction) *Node {
	// Implementation to find a neighbor in the tree
	// This can be complex, for now, we'll just traverse up and then down.
	last := leaf
	curr := leaf.Parent
	for curr != nil {
		var next *Node
		if d == DirRight && last == curr.Left && curr.Split == Vertical {
			next = curr.Right
		} else if d == DirLeft && last == curr.Right && curr.Split == Vertical {
			next = curr.Left
		} else if d == DirDown && last == curr.Left && curr.Split == Horizontal {
			next = curr.Right
		} else if d == DirUp && last == curr.Right && curr.Split == Horizontal {
			next = curr.Left
		}

		if next != nil {
			// Found a sibling, now find the first leaf in that subtree
			for next.Pane == nil {
				// Descend to the appropriate child
				switch d {
				case DirLeft, DirUp:
					next = next.Right
				case DirRight, DirDown:
					next = next.Left
				}
			}
			return next
		}

		last = curr
		curr = curr.Parent
	}
	return nil
}

func (s *Screen) closeActivePane() {
	leaf := s.activeLeaf
	if leaf == nil || leaf.Parent == nil {
		// Don't close the last pane
		return
	}

	parent := leaf.Parent
	var sibling *Node
	if leaf == parent.Left {
		sibling = parent.Right
	} else {
		sibling = parent.Left
	}

	// The parent's layout is given to the sibling
	grandparent := parent.Parent
	sibling.Layout = parent.Layout
	sibling.Parent = grandparent

	if grandparent == nil {
		s.root = sibling
	} else {
		if parent == grandparent.Left {
			grandparent.Left = sibling
		} else {
			grandparent.Right = sibling
		}
	}

	s.setActivePane(findFirstLeaf(sibling))
	s.requestRefresh()
	s.broadcastStateUpdate()
}

func findFirstLeaf(node *Node) *Node {
	if node == nil {
		return nil
	}
	curr := node
	for curr.Pane == nil {
		curr = curr.Left
	}
	return curr
}

func (s *Screen) setActivePane(n *Node) {
	if s.activeLeaf == n {
		return
	}
	s.activeLeaf = n
	s.ForceResize()
	// Broadcast a generic event. Effects will listen and decide if they need to change state.
	s.broadcastEvent(Event{Type: EventActivePaneChanged})
	s.broadcastStateUpdate()
}

// swapActivePane swaps the pane of the active leaf with its neighbor and updates the active leaf.
func (s *Screen) swapActivePane(d Direction) {
	neighbor := findNeighbor(s.activeLeaf, d)
	if neighbor == nil {
		return
	}

	// Swap the panes within the leaves
	s.activeLeaf.Pane, neighbor.Pane = neighbor.Pane, s.activeLeaf.Pane

	// Set the new active pane and refresh the screen
	s.setActivePane(neighbor)
	s.requestRefresh()
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
	// Screen creates the internal pane wrapper.
	p := newPane(app)
	s.addStandardEffects(p)

	leaf := &Node{
		Pane:   p,
		Layout: Rect{0, 0, 1, 1},
	}

	if s.root == nil {
		s.root = leaf
	} else {
		// A more sophisticated split would happen here in a real app
		s.root = leaf
	}

	p.app.SetRefreshNotifier(s.refreshChan) // ?

	// Enforce the "Resize -> Set Active -> Run" lifecycle.
	s.ForceResize()
	s.setActivePane(leaf)
	go p.app.Run()
}

func (s *Screen) createAndInitPane(app App) *pane {
	p := newPane(app)
	s.addStandardEffects(p)
	p.app.SetRefreshNotifier(s.refreshChan)
	return p
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
	s.broadcastStateUpdate()
	s.draw()
	for {
		select {
		case <-sigChan:
			s.tcellScreen.Sync()
			s.handleResize()
			dirty = true
		case ev := <-eventChan:
			s.handleEvent(ev)
			// handleEvent will set dirty=true if it causes a state change
		case <-s.refreshChan:
			s.broadcastStateUpdate()
			dirty = true
		case <-ticker.C:
			// Check if any continuous effect is active, which forces a redraw.
			var needsContinuousUpdate bool
			s.traverse(s.root, func(node *Node) {
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
		// Ctrl-A toggles control mode
		if ev.Key() == keyControlMode {
			s.inControlMode = !s.inControlMode
			s.subControlMode = 0 // Reset any sub-command
			if s.inControlMode {
				s.broadcastEvent(Event{Type: EventControlOn})
			} else {
				s.broadcastEvent(Event{Type: EventControlOff})
			}
			s.broadcastStateUpdate()
			s.requestRefresh()
			return
		}

		// Handle events in control mode
		if s.inControlMode {
			s.handleControlMode(ev)
			return
		}

		// Quit is always available
		if ev.Key() == keyQuit {
			s.Close()
			return
		}

		// Fast navigation with Shift+arrow is always available
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

		// Delegate other keys to the active pane
		if s.activeLeaf != nil && s.activeLeaf.Pane != nil {
			s.activeLeaf.Pane.app.HandleKey(ev)
		}

	case *tcell.EventResize:
		s.handleResize()
		s.requestRefresh()
	}
}

func (s *Screen) handleControlMode(ev *tcell.EventKey) {
	// If we are in a sub-command mode (e.g., waiting for a direction)
	if s.subControlMode != 0 {
		switch s.subControlMode {
		case 'w': // Waiting for direction to swap
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
				validDir = false // Invalid key, cancel sub-mode
			}
			if validDir {
				s.swapActivePane(d)
			}
		}
		s.subControlMode = 0
		s.inControlMode = false
		s.broadcastEvent(Event{Type: EventControlOff})
		s.broadcastStateUpdate()
		s.requestRefresh()
		return
	}

	// Handle main control mode commands
	switch ev.Rune() {
	case 'x':
		s.closeActivePane()
	case 'w':
		s.subControlMode = 'w' // Enter 'w' sub-mode and wait for next key
		s.broadcastStateUpdate()
		s.requestRefresh()
		return // Stay in control mode
	case '|':
		s.splitActivePane(DirRight) // Split vertically
	case '-':
		s.splitActivePane(DirDown) // Split horizontally
	default:
		// Any other key exits control mode
	}

	// Exit control mode after executing a command
	s.inControlMode = false
	s.broadcastEvent(Event{Type: EventControlOff})
	s.broadcastStateUpdate()
	s.requestRefresh()
}

// compositePanes draws each pane’s buffer to the screen.
func (s *Screen) compositePanes() {
	s.traverse(s.root, func(node *Node) {
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
	//s.tcellScreen.Clear()
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

		// Cancel any running animations
		for _, cancel := range s.effectAnimators {
			cancel()
		}

		s.traverse(s.root, func(node *Node) {
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

// ForceResize triggers a layout recalculation.
func (s *Screen) ForceResize() {
	s.handleResize()
}

// handleResize recalculates pane dimensions and notifies apps.
func (s *Screen) handleResize() {
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

	// Calculate proportional layout for the main content area
	if s.root != nil {
		s.resizeNode(s.root, Rect{0, 0, 1, 1}, mainX, mainY, mainW, mainH)
	}
}

func (s *Screen) resizeNode(n *Node, r Rect, x, y, w, h int) {
	if n == nil {
		return
	}

	// The proportional layout is always relative to the parent's container
	n.Layout = r

	// Calculate the absolute pixel dimensions for THIS node
	absX := x + int(r.X*float64(w))
	absY := y + int(r.Y*float64(h))
	absW := int(r.W * float64(w))
	absH := int(r.H * float64(h))

	if n.Pane != nil {
		// If it's a leaf, set the final dimensions on the pane
		n.Pane.SetDimensions(absX, absY, absX+absW, absY+absH)
		n.Pane.prevBuf = nil
	} else {
		// If it's a split, recurse into the children.
		// CRUCIAL FIX: The children are laid out within the absolute
		// dimensions we just calculated for THIS node (absX, absY, absW, absH).
		if n.Split == Vertical {
			s.resizeNode(n.Left, Rect{0, 0, 0.5, 1}, absX, absY, absW, absH)
			s.resizeNode(n.Right, Rect{0.5, 0, 0.5, 1}, absX, absY, absW, absH)
		} else { // Horizontal
			s.resizeNode(n.Left, Rect{0, 0, 1, 0.5}, absX, absY, absW, absH)
			s.resizeNode(n.Right, Rect{0, 0.5, 1, 0.5}, absX, absY, absW, absH)
		}
	}
}

func (s *Screen) traverse(n *Node, f func(*Node)) {
	if n == nil {
		return
	}
	f(n)
	s.traverse(n.Left, f)
	s.traverse(n.Right, f)
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

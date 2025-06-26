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
		//log.Printf("Broadcasting Event: %s to %s ", event, node.Pane)
		if node.Pane != nil {
			node.Pane.HandleEvent(event)
		}
	})
}

func (s *Screen) addStandardEffects(p *pane) {
	p.AddEffect(s.inactiveFadePrototype.Clone())
	p.AddEffect(s.controlModeFadeEffectPrototype.Clone())
	p.AddEffect(s.ditherEffectPrototype.Clone())
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

// moveActivePane moves focus to the neighbor in the given direction.
func (s *Screen) moveActivePane(d Direction) {
	target := findNeighbor(s.activeLeaf, d)
	if target != nil {
		s.setActivePane(target)
	}
}

func findNeighbor(leaf *Node, d Direction) *Node {
	curr := leaf
	for curr.Parent != nil {
		parent := curr.Parent

		// Find our index in the parent's children list
		myIndex := -1
		for i, child := range parent.Children {
			if child == curr {
				myIndex = i
				break
			}
		}
		if myIndex == -1 {
			return nil
		} // Should not happen

		// Check for neighbors based on direction and parent's split type
		switch d {
		case DirRight:
			if parent.Split == Vertical && myIndex+1 < len(parent.Children) {
				return findFirstLeaf(parent.Children[myIndex+1])
			}
		case DirLeft:
			if parent.Split == Vertical && myIndex-1 >= 0 {
				return findFirstLeaf(parent.Children[myIndex-1])
			}
		case DirDown:
			if parent.Split == Horizontal && myIndex+1 < len(parent.Children) {
				return findFirstLeaf(parent.Children[myIndex+1])
			}
		case DirUp:
			if parent.Split == Horizontal && myIndex-1 >= 0 {
				return findFirstLeaf(parent.Children[myIndex-1])
			}
		}

		// If we couldn't find a direct neighbor, move up the tree
		curr = parent
	}
	return nil
}

func (s *Screen) closeActivePane() {
	leaf := s.activeLeaf
	if leaf == nil || leaf.Parent == nil {
		// Don't close the root pane
		return
	}

	parent := leaf.Parent
	// Find the index of the leaf being closed
	childIndex := -1
	for i, child := range parent.Children {
		if child == leaf {
			childIndex = i
			break
		}
	}
	if childIndex == -1 {
		return
	} // Should not happen

	// Remove the child from the parent's slice
	parent.Children = append(parent.Children[:childIndex], parent.Children[childIndex+1:]...)

	// If the parent has only one child left, the split is no longer needed.
	// Promote the remaining child to replace its parent.
	var nextActiveNode *Node
	if len(parent.Children) == 1 {
		remainingChild := parent.Children[0]
		grandparent := parent.Parent
		remainingChild.Parent = grandparent

		if grandparent == nil {
			s.root = remainingChild
		} else {
			// Find parent's index in grandparent's children and replace it
			for i, child := range grandparent.Children {
				if child == parent {
					grandparent.Children[i] = remainingChild
					break
				}
			}
		}
		nextActiveNode = findFirstLeaf(remainingChild)
	} else {
		// Otherwise, set focus to the previous sibling, or the new last one if we closed the first.
		newIndex := childIndex
		if newIndex >= len(parent.Children) {
			newIndex = len(parent.Children) - 1
		}
		nextActiveNode = findFirstLeaf(parent.Children[newIndex])
	}

	leaf.Pane.app.Stop() // Ensure the closed app is stopped
	s.setActivePane(nextActiveNode)
}

func findFirstLeaf(node *Node) *Node {
	if node == nil {
		return nil
	}
	curr := node
	// While the current node is not a leaf, descend to the first child.
	for len(curr.Children) > 0 {
		curr = curr.Children[0]
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
		Pane: p,
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
		s.performSplit(Vertical) // Split vertically
	case '-':
		s.performSplit(Horizontal) // Split horizontally
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
		s.resizeNode(s.root, mainX, mainY, mainW, mainH)
	}
}

func (s *Screen) resizeNode(n *Node, x, y, w, h int) {
	if n == nil {
		return
	}

	// Check if this is a leaf node
	if len(n.Children) == 0 && n.Pane != nil {
		n.Pane.setDimensions(x, y, x+w, y+h)
		n.Pane.prevBuf = nil
		return
	}

	// This is an internal node, so we lay out its children
	numChildren := len(n.Children)
	if numChildren == 0 {
		return // Nothing to do
	}

	if n.Split == Vertical {
		childW := w / numChildren
		currentX := x
		for i, child := range n.Children {
			// Give the last child all the remaining space to avoid off-by-one errors
			if i == numChildren-1 {
				childW = w - (currentX - x)
			}
			s.resizeNode(child, currentX, y, childW, h)
			currentX += childW
		}
	} else { // Horizontal
		childH := h / numChildren
		currentY := y
		for i, child := range n.Children {
			// Give the last child all the remaining space
			if i == numChildren-1 {
				childH = h - (currentY - y)
			}
			s.resizeNode(child, x, currentY, w, childH)
			currentY += childH
		}
	}
}

func findNodeByPane(current *Node, target *pane) *Node {
	if current == nil {
		return nil
	}
	if current.Pane == target {
		return current
	}
	for _, child := range current.Children {
		if found := findNodeByPane(child, target); found != nil {
			return found
		}
	}
	return nil
}

func (s *Screen) performSplit(splitDir SplitType) {
	if s.activeLeaf == nil || s.ShellAppFactory == nil {
		return
	}

	// The node we are splitting is the one containing the active pane.
	nodeToModify := s.activeLeaf

	parent := findParentOf(s.root, nil, nodeToModify)

	// CASE 1: The parent's split direction matches our desired split.
	// This means we are adding another pane to an existing group.
	if parent != nil && parent.Split == splitDir {
		// Add a new leaf node to the parent's children
		newPane := s.createAndInitPane(s.ShellAppFactory())
		newNode := &Node{
			Parent: parent,
			Pane:   newPane,
		}
		parent.Children = append(parent.Children, newNode)

		go newPane.app.Run()
		s.setActivePane(newNode)

	} else {
		// CASE 2: The pane is a single leaf or part of a different-direction split.
		// We transform the current activeNode into a new internal node with two children.

		// 1. Keep a reference to the original pane and create a new one.
		originalPane := nodeToModify.Pane
		newPane := s.createAndInitPane(s.ShellAppFactory())

		// 2. Convert the active node into an internal split node.
		nodeToModify.Pane = nil       // No longer a leaf
		nodeToModify.Split = splitDir // Set the split direction
		nodeToModify.Children = nil   // Clear any previous children (should be nil anyway)

		// 3. Create two new children for it.
		child1 := &Node{Parent: nodeToModify, Pane: originalPane}
		child2 := &Node{Parent: nodeToModify, Pane: newPane}
		nodeToModify.Children = []*Node{child1, child2}

		// 4. Start the new app and set the new second pane as the active one.
		go newPane.app.Run()
		s.setActivePane(child2)
	}

	s.ForceResize()
	s.requestRefresh()
}

// You will need this helper function:
func findParentOf(current, parent, target *Node) *Node {
	if current == nil {
		return nil
	}
	if current == target {
		return parent
	}
	for _, child := range current.Children {
		if found := findParentOf(child, current, target); found != nil {
			return found
		}
	}
	return nil
}

func (s *Screen) traverse(n *Node, f func(*Node)) {
	if n == nil {
		return
	}
	f(n)
	// Loop over the children slice instead of Left/Right
	for _, child := range n.Children {
		s.traverse(child, f)
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

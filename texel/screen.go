package texel

import (
	"context"
	"github.com/gdamore/tcell/v2"
	"log"
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
	ResizeStep float64 = 0.05 // Resize by 5%
	MinRatio   float64 = 0.1  // Panes can't be smaller than 10%
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

// Screen now represents a single workspace.
type Screen struct {
	id                             int
	width, height                  int
	desktop                        *Desktop // Reference to the parent desktop
	tree                           *Tree
	statusPanes                    []*StatusPane
	inactiveFadePrototype          Effect
	controlModeFadeEffectPrototype Effect
	ditherEffectPrototype          Effect
	refreshChan                    chan bool
	drawChan                       chan bool
	dispatcher                     *EventDispatcher
	ShellAppFactory                AppFactory
	needsDraw                      bool

	inControlMode     bool
	subControlMode    rune
	effectAnimators   map[*FadeEffect]context.CancelFunc
	resizeSelection   *selectedBorder
	debugFramesToDump int
}

// newScreen creates a new workspace screen.
func newScreen(id int, shellFactory AppFactory, desktop *Desktop) (*Screen, error) {
	s := &Screen{
		id:              id,
		desktop:         desktop,
		tree:            NewTree(),
		statusPanes:     make([]*StatusPane, 0),
		refreshChan:     make(chan bool, 1),
		drawChan:        make(chan bool, 1),
		dispatcher:      NewEventDispatcher(),
		effectAnimators: make(map[*FadeEffect]context.CancelFunc),
		ShellAppFactory: shellFactory,
	}
	s.inactiveFadePrototype = NewFadeEffect(s, tcell.NewRGBColor(20, 20, 0), 0.4)
	s.controlModeFadeEffectPrototype = NewFadeEffect(s, tcell.NewRGBColor(0, 50, 0), 0.2, WithIsControl(true))
	s.ditherEffectPrototype = NewDitherEffect('░')

	return s, nil
}

func (s *Screen) Refresh() {
	select {
	case s.refreshChan <- true:
	default:
	}
}

func (s *Screen) Broadcast(event Event) {
	s.dispatcher.Broadcast(event)
}

func (s *Screen) Subscribe(listener Listener) {
	s.dispatcher.Subscribe(listener)
}

func (s *Screen) Unsubscribe(listener Listener) {
	s.dispatcher.Unsubscribe(listener)
}

func (s *Screen) addStandardEffects(p *pane) {
	p.AddEffect(s.inactiveFadePrototype.Clone())
	p.AddEffect(s.controlModeFadeEffectPrototype.Clone())
}

func (s *Screen) broadcastStateUpdate() {
	title := s.tree.GetActiveTitle()

	s.Broadcast(Event{
		Type: EventStateUpdate,
		Payload: StatePayload{
			WorkspaceID:   s.id, // Fixed: Added field
			InControlMode: s.inControlMode,
			SubMode:       s.subControlMode,
			ActiveTitle:   title,
		},
	})
}

func (s *Screen) AddStatusPane(app App, side Side, size int) {
	sp := &StatusPane{
		app:  app,
		side: side,
		size: size,
	}
	s.statusPanes = append(s.statusPanes, sp)

	if listener, ok := app.(Listener); ok {
		s.Subscribe(listener)
	}

	app.SetRefreshNotifier(s.refreshChan)
	go func() {
		if err := app.Run(); err != nil {
			log.Printf("Status pane app '%s' exited with error: %v", app.GetTitle(), err)
		}
	}()
}

func (s *Screen) AddApp(app App) {
	p := newPane(s)
	s.addStandardEffects(p)
	s.tree.SetRoot(p)
	p.AttachApp(app, s.refreshChan)

	s.Broadcast(Event{Type: EventPaneActiveChanged, Payload: s.tree.ActiveLeaf})
	s.broadcastStateUpdate()
}
func (s *Screen) handleEvent(ev tcell.Event) {
	switch ev := ev.(type) {
	case *tcell.EventKey:
		// Enter/Exit Control Mode
		if ev.Key() == keyControlMode {
			s.inControlMode = !s.inControlMode
			s.subControlMode = 0
			if s.inControlMode {
				s.Broadcast(Event{Type: EventControlOn})
			} else {
				s.Broadcast(Event{Type: EventControlOff})
			}
			s.broadcastStateUpdate()
			s.Refresh()
			return
		}

		// Handle control mode commands
		if s.inControlMode {
			s.handleControlMode(ev)
			return
		}

		// Handle pane navigation (This was missing)
		if ev.Modifiers()&tcell.ModShift != 0 {
			isPaneNavKey := true
			switch ev.Key() {
			case tcell.KeyUp:
				s.moveActivePane(DirUp)
			case tcell.KeyDown:
				s.moveActivePane(DirDown)
			case tcell.KeyLeft:
				s.moveActivePane(DirLeft)
			case tcell.KeyRight:
				s.moveActivePane(DirRight)
			default:
				isPaneNavKey = false
			}
			if isPaneNavKey {
				s.Refresh()
				return
			}
		}

		// Pass all other keys to the active application
		if s.tree.ActiveLeaf != nil && s.tree.ActiveLeaf.Pane != nil {
			s.tree.ActiveLeaf.Pane.app.HandleKey(ev)
			s.Refresh()
		}
	}
}

func (s *Screen) moveActivePane(d Direction) {
	s.tree.MoveActive(d)
	s.recalculateLayout(s.width, s.height)
	s.Broadcast(Event{Type: EventPaneActiveChanged, Payload: s.tree.ActiveLeaf})
	s.broadcastStateUpdate()
}

func (s *Screen) handleControlMode(ev *tcell.EventKey) {
	if ev.Key() == tcell.KeyEsc {
		if s.resizeSelection != nil {
			for _, child := range s.resizeSelection.node.Children {
				if child.Pane != nil {
					child.Pane.IsResizing = false
				}
			}
			s.resizeSelection = nil
			s.Refresh()
			return
		}

		s.inControlMode = false
		s.subControlMode = 0
		s.Broadcast(Event{Type: EventControlOff})
		s.broadcastStateUpdate()
		s.Refresh()
		return
	}

	if ev.Modifiers()&tcell.ModCtrl != 0 {
		d := keyToDirection(ev)
		if d != -1 {
			s.handleInteractiveResize(ev)
			return
		}
	}

	if s.resizeSelection != nil {
		return
	}

	if s.subControlMode != 0 {
		// Handle sub-mode commands
		// For now, just exit control mode
		s.subControlMode = 0
		s.inControlMode = false
		s.Broadcast(Event{Type: EventControlOff})
		s.broadcastStateUpdate()
		s.Refresh()
		return
	}

	switch ev.Rune() {
	case 'x':
		closedPaneNode := s.tree.ActiveLeaf
		s.tree.CloseActiveLeaf()
		s.recalculateLayout(s.width, s.height) // Fixed: Use stored dimensions
		s.Broadcast(Event{Type: EventPaneClosed, Payload: closedPaneNode})
	case 'w':
		s.subControlMode = 'w'
		s.broadcastStateUpdate()
		s.Refresh()
		return // Stay in control mode
	case '|':
		s.performSplit(Vertical)
	case '-':
		s.performSplit(Horizontal)
	}

	s.inControlMode = false
	s.Broadcast(Event{Type: EventControlOff})
	s.broadcastStateUpdate()
	s.Refresh()
}

func (s *Screen) draw(tcs tcell.Screen) {
	s.compositePanes(tcs)
	s.drawStatusPanes(tcs)
	tcs.Show()
}

func (s *Screen) compositePanes(tcs tcell.Screen) {
	s.tree.Traverse(func(node *Node) {
		if node.Pane != nil && node.Pane.app != nil {
			p := node.Pane
			finalBuffer := p.Render()

			if p.prevBuf == nil {
				blit(tcs, p.absX0, p.absY0, finalBuffer)
			} else {
				blitDiff(tcs, p.absX0, p.absY0, p.prevBuf, finalBuffer)
			}
			p.prevBuf = finalBuffer
		}
	})
}

func (s *Screen) drawStatusPanes(tcs tcell.Screen) {
	w, h := tcs.Size()
	topOffset, bottomOffset, leftOffset, rightOffset := 0, 0, 0, 0

	for _, sp := range s.statusPanes {
		switch sp.side {
		case SideTop:
			buf := sp.app.Render()
			blit(tcs, leftOffset, topOffset, buf)
			topOffset += sp.size
		case SideBottom:
			buf := sp.app.Render()
			blit(tcs, leftOffset, h-bottomOffset-sp.size, buf)
			bottomOffset += sp.size
		case SideLeft:
			buf := sp.app.Render()
			blit(tcs, leftOffset, topOffset, buf)
			leftOffset += sp.size
		case SideRight:
			buf := sp.app.Render()
			blit(tcs, w-rightOffset-sp.size, topOffset, buf)
			rightOffset += sp.size
		}
	}
}

func (s *Screen) Close() {
	for _, cancel := range s.effectAnimators {
		cancel()
	}
	s.tree.Traverse(func(node *Node) {
		if node.Pane != nil {
			node.Pane.Close()
		}
	})
	for _, sp := range s.statusPanes {
		sp.app.Stop()
	}
}

func (s *Screen) recalculateLayout(w, h int) {
	s.width, s.height = w, h // Store dimensions

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

func (s *Screen) performSplit(splitDir SplitType) {
	if s.tree.ActiveLeaf == nil || s.ShellAppFactory == nil {
		return
	}
	newPane := newPane(s)
	s.addStandardEffects(newPane)
	s.tree.SplitActive(splitDir, newPane)

	s.recalculateLayout(s.width, s.height) // Fixed: Use stored dimensions

	newApp := s.ShellAppFactory()
	newPane.AttachApp(newApp, s.refreshChan)
}

func (s *Screen) needsContinuousUpdate() bool {
	var needsUpdate bool
	s.tree.Traverse(func(node *Node) {
		if node != nil && node.Pane != nil {
			for _, effect := range node.Pane.effects {
				if effect.IsContinuous() {
					needsUpdate = true
					break
				}
			}
		}
	})
	return needsUpdate
}

func blit(tcs tcell.Screen, x, y int, buf [][]Cell) {
	for r, row := range buf {
		for c, cell := range row {
			tcs.SetContent(x+c, y+r, cell.Ch, nil, cell.Style)
		}
	}
}

func blitDiff(tcs tcell.Screen, x0, y0 int, oldBuf, buf [][]Cell) {
	for y, row := range buf {
		for x, cell := range row {
			if y >= len(oldBuf) || x >= len(oldBuf[y]) || cell != oldBuf[y][x] {
				tcs.SetContent(x0+x, y0+y, cell.Ch, nil, cell.Style)
			}
		}
	}
}

func (s *Screen) handleInteractiveResize(ev *tcell.EventKey) {
	// ... logic for finding the border ...
	// After adjusting ratios:
	s.recalculateLayout(s.width, s.height) // Fixed: Use stored dimensions
	s.Refresh()
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

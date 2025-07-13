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
			InControlMode: s.desktop.inControlMode,
			SubMode:       s.desktop.subControlMode,
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

func (s *Screen) moveActivePane(d Direction) {
	s.tree.MoveActive(d)
	s.recalculateLayout(s.width, s.height)
	s.Broadcast(Event{Type: EventPaneActiveChanged, Payload: s.tree.ActiveLeaf})
	s.broadcastStateUpdate()
}

func (s *Screen) handleEvent(ev *tcell.EventKey) {
	// Handle pane navigation
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
	}
}

func (s *Screen) CloseActivePane() {
	if s.tree.ActiveLeaf == nil {
		return
	}
	closedPaneNode := s.tree.ActiveLeaf
	s.tree.CloseActiveLeaf()
	s.recalculateLayout(s.width, s.height)
	s.Broadcast(Event{Type: EventPaneClosed, Payload: closedPaneNode})
}

func (s *Screen) PerformSplit(splitDir SplitType) {
	if s.tree.ActiveLeaf == nil || s.ShellAppFactory == nil {
		return
	}
	newPane := newPane(s)
	s.addStandardEffects(newPane)
	s.tree.SplitActive(splitDir, newPane)
	s.recalculateLayout(s.width, s.height)
	newApp := s.ShellAppFactory()
	newPane.AttachApp(newApp, s.refreshChan)
}

func (s *Screen) SwapActivePane(d Direction) {
	if d != -1 {
		s.tree.SwapActivePane(d)
		s.recalculateLayout(s.width, s.height)
	}
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

func (s *Screen) findBorderToResize(d Direction) *selectedBorder {
	// (This logic is extracted from the old handleInteractiveResize)
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
	if border != nil {
		if p1 := border.node.Children[border.index].Pane; p1 != nil {
			p1.IsResizing = true
		}
		if p2 := border.node.Children[border.index+1].Pane; p2 != nil {
			p2.IsResizing = true
		}
	}
	s.Refresh()
	return border
}

func (s *Screen) handleInteractiveResize(ev *tcell.EventKey, currentSelection *selectedBorder) *selectedBorder {
	d := keyToDirection(ev)
	if currentSelection == nil {
		return s.findBorderToResize(d)
	}

	s.adjustBorder(currentSelection, d)
	return currentSelection
}

func (s *Screen) adjustBorder(border *selectedBorder, d Direction) {

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

	s.recalculateLayout(s.width, s.height)
	s.Refresh()
}

func (s *Screen) clearResizeSelection(selection *selectedBorder) {
	if selection == nil {
		return
	}
	if p1 := selection.node.Children[selection.index].Pane; p1 != nil {
		p1.IsResizing = false
	}
	if p2 := selection.node.Children[selection.index+1].Pane; p2 != nil {
		p2.IsResizing = false
	}
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

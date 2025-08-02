// texel/screen_v2.go
package texel

import (
	"github.com/gdamore/tcell/v2"
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

type selectedBorder struct {
	node  *Node // The parent node whose children are being resized (the split node)
	index int   // The index of the left/top pane of the border. The border is between child[index] and child[index+1].
}

// Screen now represents a single workspace with its own effects pipeline
type Screen struct {
	id                  int
	x, y, width, height int
	desktop             *Desktop
	tree                *Tree
	refreshChan         chan bool
	drawChan            chan bool
	dispatcher          *EventDispatcher
	ShellAppFactory     AppFactory

	// New effects system for screen-level effects
	effects  *EffectPipeline
	animator *EffectAnimator

	// Pre-created effects for control mode
	controlModeDither *DitherEffect
	controlModeFade   *FadeEffect

	resizeSelection   *selectedBorder
	debugFramesToDump int
}

// newScreen creates a new workspace screen.
func newScreen(id int, shellFactory AppFactory, desktop *Desktop) (*Screen, error) {
	s := &Screen{
		id:              id,
		desktop:         desktop,
		tree:            NewTree(),
		refreshChan:     make(chan bool, 1),
		drawChan:        make(chan bool, 1),
		dispatcher:      NewEventDispatcher(),
		ShellAppFactory: shellFactory,
		effects:         NewEffectPipeline(),
		animator:        NewEffectAnimator(),
	}

	// Create control mode effects
	s.controlModeDither = NewDitherEffect('░')
	s.controlModeFade = NewFadeEffect(desktop, tcell.NewRGBColor(0, 50, 0))

	// Add them to the screen's effect pipeline (start with 0 intensity)
	s.effects.AddEffect(s.controlModeFade)
	s.effects.AddEffect(s.controlModeDither)

	return s, nil
}

// SetControlMode activates or deactivates control mode effects
func (s *Screen) SetControlMode(active bool) {
	if active {
		// Fade in the control mode effects
		s.animator.FadeIn(s.controlModeFade, 150*time.Millisecond, func() {
			s.Refresh()
		})
		s.animator.FadeIn(s.controlModeDither, 150*time.Millisecond, func() {
			s.Refresh()
		})
	} else {
		// Fade out the control mode effects
		s.animator.FadeOut(s.controlModeFade, 150*time.Millisecond, func() {
			s.Refresh()
		})
		s.animator.FadeOut(s.controlModeDither, 150*time.Millisecond, func() {
			s.Refresh()
		})
	}
}

// AddEffect adds a custom effect to the screen's pipeline
func (s *Screen) AddEffect(effect Effect) {
	s.effects.AddEffect(effect)
}

// RemoveEffect removes an effect from the screen's pipeline
func (s *Screen) RemoveEffect(effect Effect) {
	s.effects.RemoveEffect(effect)
}

func (s *Screen) getDefaultBackground() tcell.Color {
	return s.desktop.DefaultBgColor
}

func (s *Screen) setArea(x, y, w, h int) {
	s.x, s.y, s.width, s.height = x, y, w, h
	s.recalculateLayout()
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

func (s *Screen) AddApp(app App) {
	p := newPane(s)
	s.tree.SetRoot(p)
	p.AttachApp(app, s.refreshChan)

	// Set initial active state
	p.SetActive(true)

	s.Broadcast(Event{Type: EventPaneActiveChanged, Payload: s.tree.ActiveLeaf})
	s.desktop.broadcastStateUpdate()
}

func (s *Screen) moveActivePane(d Direction) {
	// Deactivate current pane
	if s.tree.ActiveLeaf != nil && s.tree.ActiveLeaf.Pane != nil {
		s.tree.ActiveLeaf.Pane.SetActive(false)
	}

	s.tree.MoveActive(d)
	s.recalculateLayout()

	// Activate new pane
	if s.tree.ActiveLeaf != nil && s.tree.ActiveLeaf.Pane != nil {
		s.tree.ActiveLeaf.Pane.SetActive(true)
	}

	s.Broadcast(Event{Type: EventPaneActiveChanged, Payload: s.tree.ActiveLeaf})
	s.desktop.broadcastStateUpdate()
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
	s.recalculateLayout()

	// Ensure the new active pane is properly activated
	if s.tree.ActiveLeaf != nil && s.tree.ActiveLeaf.Pane != nil {
		s.tree.ActiveLeaf.Pane.SetActive(true)
	}

	s.Broadcast(Event{Type: EventPaneClosed, Payload: closedPaneNode})
}

func (s *Screen) PerformSplit(splitDir SplitType) {
	if s.tree.ActiveLeaf == nil || s.ShellAppFactory == nil {
		return
	}

	// Deactivate current pane
	if s.tree.ActiveLeaf.Pane != nil {
		s.tree.ActiveLeaf.Pane.SetActive(false)
	}

	newPane := newPane(s)
	s.tree.SplitActive(splitDir, newPane)
	s.recalculateLayout()
	newApp := s.ShellAppFactory()
	newPane.AttachApp(newApp, s.refreshChan)

	// Activate new pane
	newPane.SetActive(true)
}

func (s *Screen) SwapActivePane(d Direction) {
	if d != -1 {
		s.tree.SwapActivePane(d)
		s.recalculateLayout()
	}
}

func (s *Screen) draw(tcs tcell.Screen) {
	// First, render all panes normally
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

	// Then apply screen-level effects if any are active
	if s.hasActiveEffects() {
		s.applyScreenEffects(tcs)
	}
}

// hasActiveEffects checks if any screen-level effects are currently active
func (s *Screen) hasActiveEffects() bool {
	return s.controlModeFade.IsAnimating() || s.controlModeDither.IsAnimating()
}

// applyScreenEffects applies screen-level effects to the entire screen area
func (s *Screen) applyScreenEffects(tcs tcell.Screen) {
	// Create a buffer for the entire screen area
	buffer := make([][]Cell, s.height)
	for y := range buffer {
		buffer[y] = make([]Cell, s.width)
		for x := range buffer[y] {
			// Read the current content from tcell screen
			mainc, _, style, _ := tcs.GetContent(s.x+x, s.y+y)
			buffer[y][x] = Cell{Ch: mainc, Style: style}
		}
	}

	// Apply screen-level effects
	s.effects.Apply(&buffer)

	// Write the modified buffer back to the screen
	for y, row := range buffer {
		for x, cell := range row {
			tcs.SetContent(s.x+x, s.y+y, cell.Ch, nil, cell.Style)
		}
	}
}

func (s *Screen) Close() {
	// Stop all screen-level animations
	s.animator.StopAll()

	// Close all panes
	s.tree.Traverse(func(node *Node) {
		if node.Pane != nil {
			node.Pane.Close()
		}
	})
}

func (s *Screen) recalculateLayout() {
	s.tree.Resize(s.x, s.y, s.width, s.height)
}

func (s *Screen) findBorderToResize(d Direction) *selectedBorder {
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
		// Use the new SetResizing method
		if p1 := border.node.Children[border.index].Pane; p1 != nil {
			p1.SetResizing(true)
		}
		if p2 := border.node.Children[border.index+1].Pane; p2 != nil {
			p2.SetResizing(true)
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

	s.recalculateLayout()
	s.Refresh()
}

func (s *Screen) clearResizeSelection(selection *selectedBorder) {
	if selection == nil {
		return
	}
	// Use the new SetResizing method
	if p1 := selection.node.Children[selection.index].Pane; p1 != nil {
		p1.SetResizing(false)
	}
	if p2 := selection.node.Children[selection.index+1].Pane; p2 != nil {
		p2.SetResizing(false)
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

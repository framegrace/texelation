// texel/screen_v2.go
package texel

import (
	"github.com/gdamore/tcell/v2"
	"log"
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
	controlModeFade *FadeEffect

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

	// Create control mode effects with more subtle colors
	// Use a subtle green tint for control mode
	s.controlModeFade = NewFadeEffect(desktop, tcell.NewRGBColor(0, 100, 0)) // Dark green
	log.Printf("newScreen: Created controlModeFade with initial intensity=%.3f", s.controlModeFade.GetIntensity())

	// Add the fade effect to the screen's effect pipeline
	s.effects.AddEffect(s.controlModeFade)
	log.Printf("newScreen: Added controlModeFade to effects pipeline")

	return s, nil
}

func (s *Screen) SetControlMode(active bool) {
	log.Printf("SetControlMode called: active=%v, current intensity=%.3f", active, s.controlModeFade.GetIntensity())

	if active {
		log.Printf("SetControlMode: Activating control mode, animating to 0.15")
		// Fade in the control mode effects with subtle intensity
		s.animator.AnimateTo(s.controlModeFade, 0.15, 150*time.Millisecond, func() {
			log.Printf("SetControlMode: Control mode fade-in animation completed")
			s.Refresh()
		})
	} else {
		log.Printf("SetControlMode: Deactivating control mode, animating to 0.0")
		// Fade out the control mode effects
		s.animator.FadeOut(s.controlModeFade, 150*time.Millisecond, func() {
			log.Printf("SetControlMode: Control mode fade-out animation completed")
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
	log.Printf("AddApp: Adding app '%s'", app.GetTitle())

	p := newPane(s)
	s.tree.SetRoot(p)
	p.AttachApp(app, s.refreshChan)

	// Set initial active state AFTER attaching the app
	log.Printf("AddApp: Setting pane '%s' as active", p.getTitle())
	p.SetActive(true)

	s.Broadcast(Event{Type: EventPaneActiveChanged, Payload: s.tree.ActiveLeaf})
	s.desktop.broadcastStateUpdate()
}

func (s *Screen) moveActivePane(d Direction) {
	log.Printf("moveActivePane: Moving in direction %v", d)

	// Get current and target panes
	var currentPane, targetPane *pane
	var currentTitle, targetTitle string

	if s.tree.ActiveLeaf != nil && s.tree.ActiveLeaf.Pane != nil {
		currentPane = s.tree.ActiveLeaf.Pane
		currentTitle = currentPane.getTitle()
	}

	// We need to find the neighbor manually since findNeighbor is a method on Tree
	// Let's just proceed with the move and get the result
	oldActiveLeaf := s.tree.ActiveLeaf

	// Move in tree first
	s.tree.MoveActive(d)

	// Check if we actually moved
	if s.tree.ActiveLeaf == oldActiveLeaf {
		log.Printf("moveActivePane: No movement occurred")
		return
	}

	// Get the target pane after the move
	if s.tree.ActiveLeaf != nil && s.tree.ActiveLeaf.Pane != nil {
		targetPane = s.tree.ActiveLeaf.Pane
		targetTitle = targetPane.getTitle()
	}

	s.recalculateLayout()

	// Set states and handle animations properly
	if currentPane != nil {
		// Stop any existing animations
		currentPane.animator.Stop(currentPane.inactiveFade)
		// Set inactive state and animate
		currentPane.IsActive = false
		currentPane.animator.AnimateTo(currentPane.inactiveFade, 0.3, 200*time.Millisecond, func() {
			log.Printf("moveActivePane: Deactivation of '%s' completed", currentTitle)
			s.Refresh()
		})
	}

	if targetPane != nil {
		// Stop any existing animations
		targetPane.animator.Stop(targetPane.inactiveFade)
		// Set active state and animate
		targetPane.IsActive = true
		targetPane.animator.FadeOut(targetPane.inactiveFade, 200*time.Millisecond, func() {
			log.Printf("moveActivePane: Activation of '%s' completed", targetTitle)
			s.Refresh()
		})
	}

	log.Printf("moveActivePane: Moved from '%s' to '%s'", currentTitle, targetTitle)

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
		log.Printf("PerformSplit: Cannot split - no active leaf or shell factory")
		return
	}

	log.Printf("PerformSplit: Splitting in direction %v", splitDir)

	// Get current pane for logging
	var currentTitle string
	if s.tree.ActiveLeaf.Pane != nil {
		currentTitle = s.tree.ActiveLeaf.Pane.getTitle()
		log.Printf("PerformSplit: Current active pane: '%s'", currentTitle)
	}

	// Create new pane FIRST
	newPane := newPane(s)
	log.Printf("PerformSplit: Created new pane")

	// Perform the split in the tree
	newNode := s.tree.SplitActive(splitDir, newPane)
	if newNode == nil {
		log.Printf("PerformSplit: Failed to split tree")
		return
	}
	log.Printf("PerformSplit: Tree split completed")

	// Recalculate layout BEFORE attaching app
	s.recalculateLayout()
	log.Printf("PerformSplit: Layout recalculated")

	// Create and attach new app
	newApp := s.ShellAppFactory()
	newPane.AttachApp(newApp, s.refreshChan)
	log.Printf("PerformSplit: Attached app '%s' to new pane", newApp.GetTitle())

	// Set pane states
	// The old pane should become inactive
	if s.tree.ActiveLeaf != newNode {
		// Find the old pane and deactivate it
		s.tree.Traverse(func(node *Node) {
			if node.Pane != nil && node != newNode && node != s.tree.ActiveLeaf {
				log.Printf("PerformSplit: Deactivating old pane '%s'", node.Pane.getTitle())
				node.Pane.SetActive(false)
			}
		})
	}

	// The new pane should be active
	log.Printf("PerformSplit: Activating new pane '%s'", newPane.getTitle())
	newPane.SetActive(true)

	log.Printf("PerformSplit: Split completed successfully")
}

func (s *Screen) SwapActivePane(d Direction) {
	if d != -1 {
		s.tree.SwapActivePane(d)
		s.recalculateLayout()
	}
}

func (s *Screen) draw(tcs tcell.Screen) {
	log.Printf("Screen.draw: Drawing screen %d", s.id)

	// Create a full screen buffer to collect all pane content
	screenBuffer := make([][]Cell, s.height)
	for y := range screenBuffer {
		screenBuffer[y] = make([]Cell, s.width)
		// Initialize with default background
		defaultStyle := tcell.StyleDefault.Background(s.getDefaultBackground())
		for x := range screenBuffer[y] {
			screenBuffer[y][x] = Cell{Ch: ' ', Style: defaultStyle}
		}
	}

	// Render all panes into the screen buffer
	paneCount := 0
	s.tree.Traverse(func(node *Node) {
		if node.Pane != nil && node.Pane.app != nil {
			paneCount++
			p := node.Pane
			log.Printf("Screen.draw: Rendering pane %d: '%s'", paneCount, p.getTitle())
			paneBuffer := p.Render()

			// Copy pane buffer into screen buffer at the correct position
			for y, row := range paneBuffer {
				screenY := y + (p.absY0 - s.y)
				if screenY < 0 || screenY >= s.height {
					continue
				}
				for x, cell := range row {
					screenX := x + (p.absX0 - s.x)
					if screenX < 0 || screenX >= s.width {
						continue
					}
					screenBuffer[screenY][screenX] = cell
				}
			}
		}
	})

	log.Printf("Screen.draw: Rendered %d panes", paneCount)

	// Apply screen-level effects to the collected buffer
	if s.hasActiveEffects() {
		log.Printf("Screen.draw: Applying screen effects")
		s.effects.Apply(&screenBuffer)
	}

	// Now blit the final buffer to the screen
	for y, row := range screenBuffer {
		for x, cell := range row {
			tcs.SetContent(s.x+x, s.y+y, cell.Ch, nil, cell.Style)
		}
	}

	log.Printf("Screen.draw: Screen draw completed")
}

// hasActiveEffects checks if any screen-level effects are currently active
func (s *Screen) hasActiveEffects() bool {
	isAnimating := s.controlModeFade.IsAnimating()
	intensity := s.controlModeFade.GetIntensity()
	log.Printf("hasActiveEffects: isAnimating=%v, intensity=%.3f", isAnimating, intensity)
	return isAnimating
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

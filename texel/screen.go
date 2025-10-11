// texel/screen_v2.go
package texel

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"log"
	"sort"
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
	appLifecycle        AppLifecycleManager

	// New effects system for screen-level effects
	effects  *EffectPipeline
	animator *EffectAnimator

	// Pre-created effects for control mode
	//controlModeFade *FadeEffect
	controlModeFade *RainbowEffect

	resizeSelection   *selectedBorder
	debugFramesToDump int
}

// newScreen creates a new workspace screen.
func newScreen(id int, shellFactory AppFactory, lifecycle AppLifecycleManager, desktop *Desktop) (*Screen, error) {
	s := &Screen{
		id:              id,
		desktop:         desktop,
		tree:            NewTree(),
		refreshChan:     make(chan bool, 1),
		drawChan:        make(chan bool, 1),
		dispatcher:      NewEventDispatcher(),
		ShellAppFactory: shellFactory,
		appLifecycle:    lifecycle,
		effects:         NewEffectPipeline(),
		animator:        NewEffectAnimator(),
	}

	// Create control mode effects with more subtle colors
	// Use a subtle green tint for control mode
	//s.controlModeFade = NewFadeEffect(desktop, tcell.NewRGBColor(0, 100, 0)) // Dark green
	s.controlModeFade = NewRainbowEffect(desktop) // placeholder; remote client owns visuals now

	return s, nil
}

func (s *Screen) SetControlMode(active bool) {
	log.Printf("SetControlMode called: active=%v", active)
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

func (s *Screen) notifyFocus() {
	if s.desktop == nil || s.tree == nil {
		return
	}
	s.desktop.notifyFocusNode(s.tree.ActiveLeaf)
}

func (s *Screen) AddApp(app App) {
	log.Printf("AddApp: Adding app '%s'", app.GetTitle())

	p := newPane(s)
	s.tree.SetRoot(p)
	p.AttachApp(app, s.refreshChan)

	// Set initial active state AFTER attaching the app
	log.Printf("AddApp: Setting pane '%s' as active", p.getTitle())
	p.SetActive(true)
	s.notifyFocus()
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
		currentPane.IsActive = false
		currentPane.notifyStateChange()
	}

	if targetPane != nil {
		targetPane.IsActive = true
		targetPane.notifyStateChange()
	}

	log.Printf("moveActivePane: Moved from '%s' to '%s'", currentTitle, targetTitle)

	s.Broadcast(Event{Type: EventPaneActiveChanged, Payload: s.tree.ActiveLeaf})
	s.desktop.broadcastStateUpdate()
	s.notifyFocus()
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

func (s *Screen) AnimateGroupExpansion(groupNode *Node, newPaneIndex int, duration time.Duration) {
	if groupNode == nil || len(groupNode.SplitRatios) <= newPaneIndex {
		s.recalculateLayout()
		return
	}

	log.Printf("AnimateGroupExpansion: Animating expansion of group with %d children", len(groupNode.Children))

	// The SplitActive method already set the ratios to equal distribution
	// We need to reconstruct what the ratios were BEFORE the new pane was added
	numChildren := len(groupNode.Children)
	numOldChildren := numChildren - 1

	// Start ratios: simulate the state before the new pane was added
	startRatios := make([]float64, numChildren)
	oldEqualRatio := 1.0 / float64(numOldChildren) // What each pane had before

	for i := 0; i < numChildren; i++ {
		if i == newPaneIndex {
			startRatios[i] = 0.0 // New pane starts at 0
		} else {
			startRatios[i] = oldEqualRatio // Existing panes start at their old equal size
		}
	}

	// Target ratios: equal distribution among ALL panes (what SplitActive set)
	targetRatios := make([]float64, numChildren)
	newEqualRatio := 1.0 / float64(numChildren)
	for i := range targetRatios {
		targetRatios[i] = newEqualRatio
	}

	log.Printf("AnimateGroupExpansion: %d->%d children, old ratio=%.3f, new ratio=%.3f",
		numOldChildren, numChildren, oldEqualRatio, newEqualRatio)
	log.Printf("AnimateGroupExpansion: start=%v, target=%v", startRatios, targetRatios)

	s.animateLayoutTransition(groupNode, startRatios, targetRatios, duration)
}

// Animation for splitting current pane (only 2 panes involved)
func (s *Screen) AnimatePaneSplit(splitNode *Node, duration time.Duration) {
	if splitNode == nil || len(splitNode.Children) != 2 {
		s.recalculateLayout()
		return
	}

	log.Printf("AnimatePaneSplit: Animating split of single pane into two")

	// Start ratios: first pane gets 100%, second gets 0%
	startRatios := []float64{1.0, 0.0}

	// Target ratios: 50/50 split
	targetRatios := []float64{0.5, 0.5}

	log.Printf("AnimatePaneSplit: start=%v, target=%v", startRatios, targetRatios)

	s.animateLayoutTransition(splitNode, startRatios, targetRatios, duration)
}

// Common animation helper
func (s *Screen) animateLayoutTransition(node *Node, startRatios, targetRatios []float64, duration time.Duration) {
	// Create and start the layout animation
	layoutEffect := NewLayoutEffect(node, s, startRatios, targetRatios)
	s.effects.AddEffect(layoutEffect)

	// Set initial state
	node.SplitRatios = make([]float64, len(startRatios))
	copy(node.SplitRatios, startRatios)
	s.recalculateLayout()

	// Animate to target
	s.animator.AnimateTo(layoutEffect, 1.0, duration, func() {
		log.Printf("animateLayoutTransition: Animation completed")
		// Clean up the effect
		s.effects.RemoveEffect(layoutEffect)
		// Ensure final ratios are exactly the target
		node.SplitRatios = make([]float64, len(targetRatios))
		copy(node.SplitRatios, targetRatios)
		s.recalculateLayout()
		s.Refresh()
	})
}

func (s *Screen) CloseActivePane() {
	if s.tree.ActiveLeaf == nil {
		return
	}

	closedPaneNode := s.tree.ActiveLeaf
	parent := closedPaneNode.Parent

	// If this is the root pane, don't close it
	if parent == nil {
		return
	}

	// Find the index of the pane being closed
	closingIndex := -1
	for i, child := range parent.Children {
		if child == closedPaneNode {
			closingIndex = i
			break
		}
	}

	if closingIndex == -1 {
		log.Printf("CloseActivePane: Could not find pane index")
		return
	}

	log.Printf("CloseActivePane: Closing pane '%s' at index %d",
		closedPaneNode.Pane.getTitle(), closingIndex)

	// Start removal animation
	s.AnimatePaneRemoval(parent, closingIndex, 100*time.Millisecond, func() {
		log.Printf("CloseActivePane: Animation completed, actually removing pane")

		// Now perform the actual tree cleanup
		s.actuallyClosePane(closedPaneNode, parent, closingIndex)
	})
}

// Helper method to do the actual pane removal after animation
func (s *Screen) actuallyClosePane(closedPaneNode *Node, parent *Node, closingIndex int) {
	if closedPaneNode.Pane != nil {
		closedPaneNode.Pane.IsActive = false
	}

	// Remove the child from the parent's slice
	parent.Children = append(parent.Children[:closingIndex], parent.Children[closingIndex+1:]...)
	parent.SplitRatios = append(parent.SplitRatios[:closingIndex], parent.SplitRatios[closingIndex+1:]...)

	// If the parent has only one child left, the split is no longer needed.
	// Promote the remaining child to replace its parent.
	var nextActiveNode *Node
	if len(parent.Children) == 1 {
		remainingChild := parent.Children[0]
		grandparent := parent.Parent
		remainingChild.Parent = grandparent

		if grandparent == nil {
			s.tree.Root = remainingChild
		} else {
			// Find parent's index in grandparent's children and replace it
			for i, child := range grandparent.Children {
				if child == parent {
					grandparent.Children[i] = remainingChild
					break
				}
			}
		}
		nextActiveNode = s.tree.findFirstLeaf(remainingChild)
	} else {
		// Normalize ratios after removal
		totalRatio := 0.0
		for _, ratio := range parent.SplitRatios {
			totalRatio += ratio
		}
		if totalRatio > 0 {
			for i := range parent.SplitRatios {
				parent.SplitRatios[i] = parent.SplitRatios[i] / totalRatio
			}
		}

		// Set focus to the previous sibling, or the new last one if we closed the first
		newIndex := closingIndex
		if newIndex >= len(parent.Children) {
			newIndex = len(parent.Children) - 1
		}
		nextActiveNode = s.tree.findFirstLeaf(parent.Children[newIndex])
	}

	closedPaneNode.Pane.Close() // Ensure the closed app is stopped
	s.tree.ActiveLeaf = nextActiveNode
	if s.tree.ActiveLeaf.Pane != nil {
		s.tree.ActiveLeaf.Pane.SetActive(true)
	}

	s.recalculateLayout()
	s.Broadcast(Event{Type: EventPaneClosed, Payload: closedPaneNode})
	s.notifyFocus()
	if s.desktop != nil {
		s.desktop.broadcastTreeChanged()
	}
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

	// Check if we'll be adding to existing group or creating new split
	// This replicates the logic from SplitActive to determine animation type
	nodeToModify := s.tree.ActiveLeaf
	parent := s.tree.findParentOf(s.tree.Root, nil, nodeToModify)
	addToExistingGroup := parent != nil && parent.Split == splitDir && ratiosAreEqual(parent.SplitRatios)

	log.Printf("PerformSplit: addToExistingGroup=%v", addToExistingGroup)
	if parent != nil {
		log.Printf("PerformSplit: Parent has %d children with ratios %v (equal=%v)",
			len(parent.Children), parent.SplitRatios, ratiosAreEqual(parent.SplitRatios))
	}

	// Perform the split in the tree
	newNode := s.tree.SplitActive(splitDir, newPane)
	if newNode == nil {
		log.Printf("PerformSplit: Failed to split tree")
		return
	}
	log.Printf("PerformSplit: Tree split completed")

	// Create and attach new app
	newApp := s.ShellAppFactory()
	newPane.AttachApp(newApp, s.refreshChan)
	log.Printf("PerformSplit: Attached app '%s' to new pane", newApp.GetTitle())

	// Set pane states
	s.tree.Traverse(func(node *Node) {
		if node.Pane != nil && node != newNode && node != s.tree.ActiveLeaf {
			log.Printf("PerformSplit: Deactivating old pane '%s'", node.Pane.getTitle())
			node.Pane.SetActive(false)
		}
	})

	// The new pane should be active
	log.Printf("PerformSplit: Activating new pane '%s'", newPane.getTitle())
	newPane.SetActive(true)
	s.notifyFocus()

	// Start appropriate animation based on split type
	if addToExistingGroup {
		// CASE 1: Adding to existing equally-sized group
		// All panes were resized equally, animate the entire group
		log.Printf("PerformSplit: Animating addition to existing group")
		parent := newNode.Parent // parent is the group that got a new child
		if parent != nil {
			newPaneIndex := len(parent.Children) - 1 // New pane is always added at the end
			s.AnimateGroupExpansion(parent, newPaneIndex, 300*time.Millisecond)
		} else {
			s.recalculateLayout()
		}
	} else {
		// CASE 2: Created new split (split current pane in two)
		// Only the current pane was split, animate just that split
		log.Printf("PerformSplit: Animating new split creation")
		parent := newNode.Parent // parent is the newly created split node
		if parent != nil && len(parent.Children) == 2 {
			s.AnimatePaneSplit(parent, 300*time.Millisecond)
		} else {
			s.recalculateLayout()
		}
	}

	log.Printf("PerformSplit: Split completed successfully")
	if s.desktop != nil {
		s.desktop.broadcastTreeChanged()
	}
}

func (s *Screen) SwapActivePane(d Direction) {
	if d != -1 {
		s.tree.SwapActivePane(d)
		s.recalculateLayout()
	}
}

// Update the draw method to also log when pane animations are detected
func (s *Screen) draw(tcs ScreenDriver) {
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

	// Collect all panes and sort them by z-order for proper layering
	type paneWithOrder struct {
		pane   *pane
		zOrder int
	}

	var allPanes []paneWithOrder
	s.tree.Traverse(func(node *Node) {
		if node.Pane != nil && node.Pane.app != nil {
			allPanes = append(allPanes, paneWithOrder{
				pane:   node.Pane,
				zOrder: node.Pane.GetZOrder(),
			})
		}
	})

	// Check if z-order sorting is needed (optimization for common case)
	needsSorting := false
	for _, paneInfo := range allPanes {
		if paneInfo.zOrder != 0 {
			needsSorting = true
			break
		}
	}

	// Only sort if z-order is actually being used
	if needsSorting {
		sort.Slice(allPanes, func(i, j int) bool {
			return allPanes[i].zOrder < allPanes[j].zOrder
		})

		// Log z-orders for debugging when sorting is performed
		log.Printf("Screen.draw: Sorted panes by z-order:")
		for i, paneInfo := range allPanes {
			log.Printf("  [%d] '%s' z-order=%d", i, paneInfo.pane.getTitle(), paneInfo.zOrder)
		}
	}

	// Render all panes in z-order (lowest to highest)
	paneCount := 0
	for _, paneInfo := range allPanes {
		paneCount++
		p := paneInfo.pane
		zOrderStr := ""
		if p.GetZOrder() != 0 {
			zOrderStr = fmt.Sprintf(" [Z:%d]", p.GetZOrder())
		}
		log.Printf("Screen.draw: Rendering pane %d: '%s' at abs(%d,%d)-(%d,%d)%s",
			paneCount, p.getTitle(), p.absX0, p.absY0, p.absX1, p.absY1, zOrderStr)
		paneBuffer := p.Render()

		// Copy pane buffer into screen buffer at the correct position
		for y, row := range paneBuffer {
			screenY := y + (p.absY0 - s.y) // Account for Y offset
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

	log.Printf("Screen.draw: Rendered %d panes", paneCount)

	// Apply screen-level effects to the collected buffer
	if s.hasActiveEffects() {
		log.Printf("Screen.draw: Applying screen effects (%d active)", s.effects.GetActiveAnimationCount())
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
	return s.effects.IsAnimating()
}

// Add this method to check if any panes have active animations
func (s *Screen) hasActivePaneAnimations() bool {
	return false
}

// applyScreenEffects applies screen-level effects to the entire screen area
func (s *Screen) applyScreenEffects(tcs ScreenDriver) {
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

func (s *Screen) AnimatePaneCreation(newNodeParent *Node, newPaneIndex int, duration time.Duration) {
	if newNodeParent == nil || len(newNodeParent.SplitRatios) <= newPaneIndex {
		return
	}

	log.Printf("AnimatePaneCreation: Starting animation for new pane at index %d", newPaneIndex)

	// Calculate start ratios (new pane has 0 size, others expanded)
	startRatios := make([]float64, len(newNodeParent.SplitRatios))
	targetRatios := make([]float64, len(newNodeParent.SplitRatios))

	// Target ratios are the current equal split
	copy(targetRatios, newNodeParent.SplitRatios)

	// Start ratios: new pane starts at 0, others share the space
	totalNonNewPanes := float64(len(newNodeParent.Children) - 1)
	for i := range startRatios {
		if i == newPaneIndex {
			startRatios[i] = 0.0 // New pane starts with 0 size
		} else {
			startRatios[i] = 1.0 / totalNonNewPanes // Others share the full space
		}
	}

	log.Printf("AnimatePaneCreation: start=%v, target=%v", startRatios, targetRatios)

	// Create and start the layout animation
	layoutEffect := NewLayoutEffect(newNodeParent, s, startRatios, targetRatios)
	s.effects.AddEffect(layoutEffect)

	// Set initial state
	newNodeParent.SplitRatios = startRatios
	s.recalculateLayout()

	// Animate to target
	s.animator.AnimateTo(layoutEffect, 1.0, duration, func() {
		log.Printf("AnimatePaneCreation: Animation completed")
		// Clean up the effect
		s.effects.RemoveEffect(layoutEffect)
		// Ensure final ratios are exactly the target
		newNodeParent.SplitRatios = targetRatios
		s.recalculateLayout()
		s.Refresh()
	})
}

func (s *Screen) AnimatePaneRemoval(nodeParent *Node, removingIndex int, duration time.Duration, onComplete func()) {
	if nodeParent == nil || removingIndex >= len(nodeParent.SplitRatios) {
		if onComplete != nil {
			onComplete()
		}
		return
	}

	log.Printf("AnimatePaneRemoval: Starting animation for pane at index %d", removingIndex)

	// Calculate start and target ratios
	startRatios := make([]float64, len(nodeParent.SplitRatios))
	copy(startRatios, nodeParent.SplitRatios)

	// Target: removing pane goes to 0, others expand proportionally
	targetRatios := make([]float64, len(nodeParent.SplitRatios))
	totalOtherRatio := 0.0
	for i, ratio := range startRatios {
		if i != removingIndex {
			totalOtherRatio += ratio
		}
	}

	for i := range targetRatios {
		if i == removingIndex {
			targetRatios[i] = 0.0 // Removing pane shrinks to 0
		} else if totalOtherRatio > 0 {
			// Expand proportionally
			targetRatios[i] = startRatios[i] / totalOtherRatio
		}
	}

	log.Printf("AnimatePaneRemoval: start=%v, target=%v", startRatios, targetRatios)

	// Create and start the layout animation
	layoutEffect := NewLayoutEffect(nodeParent, s, startRatios, targetRatios)
	s.effects.AddEffect(layoutEffect)

	// Animate to target
	s.animator.AnimateTo(layoutEffect, 1.0, duration, func() {
		log.Printf("AnimatePaneRemoval: Animation completed")
		// Clean up the effect
		s.effects.RemoveEffect(layoutEffect)
		// Now actually remove the pane from the tree
		if onComplete != nil {
			onComplete()
		}
	})
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

func blit(tcs ScreenDriver, x, y int, buf [][]Cell) {
	for r, row := range buf {
		for c, cell := range row {
			tcs.SetContent(x+c, y+r, cell.Ch, nil, cell.Style)
		}
	}
}

func blitDiff(tcs ScreenDriver, x0, y0 int, oldBuf, buf [][]Cell) {
	for y, row := range buf {
		for x, cell := range row {
			if y >= len(oldBuf) || x >= len(oldBuf[y]) || cell != oldBuf[y][x] {
				tcs.SetContent(x0+x, y0+y, cell.Ch, nil, cell.Style)
			}
		}
	}
}

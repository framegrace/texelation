package texel

import (
	"context"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"golang.org/x/term"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"texelation/texel/theme"
	"time"
)

// Side defines the placement of a StatusPane.
type Side int

const (
	SideTop Side = iota
	SideBottom
	SideLeft
	SideRight
)

// StatusPane is a special pane with absolute sizing, placed on one side of the screen.
type StatusPane struct {
	app  App
	side Side
	size int // rows for Top/Bottom, cols for Left/Right
}

type PaneRect struct {
	x, y, w, h int
}

// Desktop manages a collection of workspaces (Screens).
type Desktop struct {
	display           ScreenDriver
	workspaces        map[int]*Screen
	activeWorkspace   *Screen
	statusPanes       []*StatusPane
	quit              chan struct{}
	closeOnce         sync.Once
	ShellAppFactory   AppFactory
	WelcomeAppFactory AppFactory
	styleCache        map[styleKey]tcell.Style
	DefaultFgColor    tcell.Color
	DefaultBgColor    tcell.Color
	dispatcher        *EventDispatcher
	statusBuffer      BufferStore
	appLifecycle      AppLifecycleManager

	// Global state now lives on the Desktop
	inControlMode   bool
	subControlMode  rune
	resizeSelection *selectedBorder
	zoomedPane      *Node

	// Animation system
	animationTicker *time.Ticker
	animationStop   chan struct{}

	lastMouseX         int
	lastMouseY         int
	lastMouseButtons   tcell.ButtonMask
	lastMouseModifier  tcell.ModMask
	clipboard          map[string][]byte
	lastClipboardMime  string
	focusMu            sync.RWMutex
	focusListeners     []DesktopFocusListener
	paneStateMu        sync.RWMutex
	paneStateListeners []PaneStateListener
	snapshotFactories  map[string]SnapshotFactory
}

// PaneStateSnapshot captures dynamic pane flags for external consumers.
type PaneStateSnapshot struct {
	ID       [16]byte
	Active   bool
	Resizing bool
}

// NewDesktop creates and initializes a new desktop environment.
func NewDesktop(shellFactory, welcomeFactory AppFactory) (*Desktop, error) {
	tcellScreen, err := tcell.NewScreen()
	if err != nil {
		return nil, err
	}

	driver := NewTcellScreenDriver(tcellScreen)
	lifecycle := &LocalAppLifecycle{}
	return NewDesktopWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
}

// NewDesktopWithDriver wires a Desktop using the provided screen driver and
// lifecycle manager. It exists primarily to support tests and future remote
// runtimes.
func NewDesktopWithDriver(driver ScreenDriver, shellFactory, welcomeFactory AppFactory, lifecycle AppLifecycleManager) (*Desktop, error) {
	if driver == nil {
		return nil, fmt.Errorf("screen driver is required")
	}
	if lifecycle == nil {
		lifecycle = &LocalAppLifecycle{}
	}

	if err := driver.Init(); err != nil {
		return nil, err
	}

	tm := theme.Get()
	defbg := tm.GetColor("desktop", "default_bg", tcell.ColorReset).TrueColor()
	deffg := tm.GetColor("desktop", "default_fg", tcell.ColorReset).TrueColor()
	defStyle := tcell.StyleDefault.Background(defbg).Foreground(deffg)
	driver.SetStyle(defStyle)
	driver.HideCursor()

	d := &Desktop{
		display:            driver,
		workspaces:         make(map[int]*Screen),
		statusPanes:        make([]*StatusPane, 0),
		quit:               make(chan struct{}),
		ShellAppFactory:    shellFactory,
		WelcomeAppFactory:  welcomeFactory,
		styleCache:         make(map[styleKey]tcell.Style),
		DefaultFgColor:     deffg,
		DefaultBgColor:     defbg,
		dispatcher:         NewEventDispatcher(),
		statusBuffer:       NewInMemoryBufferStore(),
		appLifecycle:       lifecycle,
		inControlMode:      false,
		subControlMode:     0,
		clipboard:          make(map[string][]byte),
		focusListeners:     make([]DesktopFocusListener, 0),
		paneStateListeners: make([]PaneStateListener, 0),
		snapshotFactories:  make(map[string]SnapshotFactory),
	}

	log.Printf("NewDesktop: Created with inControlMode=%v", d.inControlMode)
	d.SwitchToWorkspace(1)
	return d, nil
}

func (d *Desktop) Subscribe(listener Listener) {
	d.dispatcher.Subscribe(listener)
}

func (d *Desktop) Unsubscribe(listener Listener) {
	d.dispatcher.Unsubscribe(listener)
}

func (d *Desktop) RegisterFocusListener(listener DesktopFocusListener) {
	if listener == nil {
		return
	}
	d.focusMu.Lock()
	d.focusListeners = append(d.focusListeners, listener)
	d.focusMu.Unlock()
	d.notifyFocusActive()
}

// RegisterPaneStateListener registers a listener for pane active/resizing changes.
func (d *Desktop) RegisterPaneStateListener(listener PaneStateListener) {
	if listener == nil {
		return
	}
	d.paneStateMu.Lock()
	d.paneStateListeners = append(d.paneStateListeners, listener)
	d.paneStateMu.Unlock()
}

// RegisterSnapshotFactory registers a factory used to restore apps from snapshot metadata.
func (d *Desktop) RegisterSnapshotFactory(appType string, factory SnapshotFactory) {
	if appType == "" || factory == nil {
		return
	}
	d.snapshotFactories[appType] = factory
}

// UnregisterFocusListener removes a previously registered focus listener.
func (d *Desktop) UnregisterFocusListener(listener DesktopFocusListener) {
	if listener == nil {
		return
	}
	d.focusMu.Lock()
	defer d.focusMu.Unlock()
	for i, registered := range d.focusListeners {
		if registered == listener {
			d.focusListeners = append(d.focusListeners[:i], d.focusListeners[i+1:]...)
			break
		}
	}
}

// UnregisterPaneStateListener removes a previously registered pane state listener.
func (d *Desktop) UnregisterPaneStateListener(listener PaneStateListener) {
	if listener == nil {
		return
	}
	d.paneStateMu.Lock()
	defer d.paneStateMu.Unlock()
	for i, registered := range d.paneStateListeners {
		if registered == listener {
			d.paneStateListeners = append(d.paneStateListeners[:i], d.paneStateListeners[i+1:]...)
			break
		}
	}
}

// AddStatusPane adds a new status pane to the desktop.
func (d *Desktop) AddStatusPane(app App, side Side, size int) {
	sp := &StatusPane{
		app:  app,
		side: side,
		size: size,
	}
	d.statusPanes = append(d.statusPanes, sp)

	if listener, ok := app.(Listener); ok {
		d.Subscribe(listener)
	}

	if d.activeWorkspace != nil {
		app.SetRefreshNotifier(d.activeWorkspace.refreshChan)
	}

	d.appLifecycle.StartApp(app)
	d.recalculateLayout()
}

func (d *Desktop) getMainArea() (int, int, int, int) {
	w, h := d.display.Size()
	mainX, mainY := 0, 0
	mainW, mainH := w, h

	topOffset, bottomOffset, leftOffset, rightOffset := 0, 0, 0, 0

	for _, sp := range d.statusPanes {
		switch sp.side {
		case SideTop:
			topOffset += sp.size
		case SideBottom:
			bottomOffset += sp.size
		case SideLeft:
			leftOffset += sp.size
		case SideRight:
			rightOffset += sp.size
		}
	}

	mainX = leftOffset
	mainY = topOffset
	mainW = w - leftOffset - rightOffset
	mainH = h - topOffset - bottomOffset
	return mainX, mainY, mainW, mainH
}

func (d *Desktop) recalculateLayout() {
	w, h := d.display.Size()
	mainX, mainY, mainW, mainH := d.getMainArea()

	for _, sp := range d.statusPanes {
		switch sp.side {
		case SideTop:
			sp.app.Resize(w, sp.size)
		case SideBottom:
			sp.app.Resize(w, sp.size)
		case SideLeft:
			sp.app.Resize(sp.size, h-mainY-(h-mainY-mainH))
		case SideRight:
			sp.app.Resize(sp.size, h-mainY-(h-mainY-mainH))
		}
	}

	if d.zoomedPane != nil {
		if d.zoomedPane.Pane != nil {
			d.zoomedPane.Pane.setDimensions(mainX, mainY, mainX+mainW, mainY+mainH)
		}
	} else if d.activeWorkspace != nil {
		d.activeWorkspace.setArea(mainX, mainY, mainW, mainH)
	}
}

func (d *Desktop) Run() error {
	eventChan := make(chan tcell.Event, 10)
	go func() {
		for {
			select {
			case <-d.quit:
				return
			default:
				eventChan <- d.display.PollEvent()
			}
		}
	}()

	// Start animation timer for smooth effects
	d.animationTicker = time.NewTicker(16 * time.Millisecond) // 60fps
	d.animationStop = make(chan struct{})

	go func() {
		for {
			select {
			case <-d.animationTicker.C:
				if d.hasActiveAnimations() {
					log.Printf("Animation timer: Triggering redraw for active animations")
					if d.activeWorkspace != nil {
						select {
						case d.activeWorkspace.drawChan <- true:
						default:
						}
					}
				}
			case <-d.animationStop:
				return
			case <-d.quit:
				return
			}
		}
	}()

	d.recalculateLayout()

	if d.activeWorkspace != nil {
		go func() {
			d.activeWorkspace.drawChan <- true
		}()
	}

	for {
		if d.activeWorkspace == nil {
			<-time.After(100 * time.Millisecond)
			continue
		}

		refreshChan := d.activeWorkspace.refreshChan
		drawChan := d.activeWorkspace.drawChan

		select {
		case ev := <-eventChan:
			d.handleEvent(ev)

		case <-refreshChan:
			d.broadcastStateUpdate()
			d.draw()

		case <-drawChan:
			d.draw()

		case <-d.quit:
			return nil
		}
	}
}

func (d *Desktop) hasActiveAnimations() bool {
	if d.activeWorkspace == nil {
		return false
	}

	// Check screen-level effects
	if d.activeWorkspace.effects.IsAnimating() {
		return true
	}

	// Check all pane-level effects
	hasActivePaneAnimations := false
	d.activeWorkspace.tree.Traverse(func(node *Node) {
		if node.Pane != nil && node.Pane.effects.IsAnimating() {
			hasActivePaneAnimations = true
		}
	})

	return hasActivePaneAnimations
}

func (d *Desktop) handleEvent(ev tcell.Event) {
	if _, ok := ev.(*tcell.EventResize); ok {
		d.recalculateLayout()
		d.draw()
		return
	}

	key, ok := ev.(*tcell.EventKey)
	if !ok {
		return
	}

	if key.Key() == keyQuit {
		d.Close()
		return
	}

	if key.Key() == keyControlMode {
		d.toggleControlMode()
		return
	}

	if d.inControlMode {
		d.handleControlMode(key)
		return
	}

	if d.zoomedPane != nil {
		if d.zoomedPane.Pane != nil {
			d.zoomedPane.Pane.app.HandleKey(key)
		}
	} else if d.activeWorkspace != nil {
		d.activeWorkspace.handleEvent(key)
	}
}

// InjectMouseEvent records the latest mouse event metadata from remote clients.
func (d *Desktop) InjectMouseEvent(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) {
	d.lastMouseX = x
	d.lastMouseY = y
	d.lastMouseButtons = buttons
	d.lastMouseModifier = modifiers
}

// HandleClipboardSet stores clipboard contents using the provided MIME type.
func (d *Desktop) HandleClipboardSet(mime string, data []byte) {
	if d.clipboard == nil {
		d.clipboard = make(map[string][]byte)
	}
	d.clipboard[mime] = append([]byte(nil), data...)
}

// HandleClipboardGet records the last clipboard lookup.
func (d *Desktop) HandleClipboardGet(mime string) []byte {
	d.lastClipboardMime = mime
	if d.clipboard == nil {
		return nil
	}
	return append([]byte(nil), d.clipboard[mime]...)
}

// HandleThemeUpdate applies runtime theme overrides.
func (d *Desktop) HandleThemeUpdate(section, key, value string) {
	config := theme.Get()
	if _, ok := config[section]; !ok {
		config[section] = theme.Section{}
	}
	config[section][key] = value
}

// LastMousePosition returns the most recently recorded mouse coordinates.
func (d *Desktop) LastMousePosition() (int, int) {
	return d.lastMouseX, d.lastMouseY
}

// LastMouseButtons exposes the last recorded button mask.
func (d *Desktop) LastMouseButtons() tcell.ButtonMask {
	return d.lastMouseButtons
}

// LastMouseModifiers exposes the last recorded modifier mask.
func (d *Desktop) LastMouseModifiers() tcell.ModMask {
	return d.lastMouseModifier
}

// InjectKeyEvent allows external callers (e.g., remote clients) to deliver key
// input directly into the desktop event pipeline.
func (d *Desktop) InjectKeyEvent(key tcell.Key, ch rune, modifiers tcell.ModMask) {
	event := tcell.NewEventKey(key, ch, modifiers)
	d.handleEvent(event)
}

func (d *Desktop) drawStatusPanes(display ScreenDriver) {
	w, h := display.Size()
	topOffset, bottomOffset, leftOffset, rightOffset := 0, 0, 0, 0

	for _, sp := range d.statusPanes {
		switch sp.side {
		case SideTop:
			buf := sp.app.Render()
			d.statusPaneBlit(display, leftOffset, topOffset, buf)
			topOffset += sp.size
		case SideBottom:
			buf := sp.app.Render()
			d.statusPaneBlit(display, leftOffset, h-bottomOffset-sp.size, buf)
			bottomOffset += sp.size
		case SideLeft:
			buf := sp.app.Render()
			d.statusPaneBlit(display, leftOffset, topOffset, buf)
			leftOffset += sp.size
		case SideRight:
			buf := sp.app.Render()
			d.statusPaneBlit(display, w-rightOffset-sp.size, topOffset, buf)
			rightOffset += sp.size
		}
	}
}
func (d *Desktop) statusPaneBlit(display ScreenDriver, x, y int, buf [][]Cell) {
	prev := d.statusBuffer.Snapshot()
	if prev != nil {
		blitDiff(display, x, y, prev, buf)
	} else {
		blit(display, x, y, buf)
	}
	d.statusBuffer.Save(buf)
}

func (d *Desktop) toggleControlMode() {
	wasInControlMode := d.inControlMode
	d.inControlMode = !d.inControlMode
	d.subControlMode = 0

	log.Printf("toggleControlMode: was=%v, now=%v", wasInControlMode, d.inControlMode)

	if !d.inControlMode && d.resizeSelection != nil {
		d.activeWorkspace.clearResizeSelection(d.resizeSelection)
		d.resizeSelection = nil
	}

	// IMPORTANT: Only call SetControlMode if the state actually changed
	if d.activeWorkspace != nil && wasInControlMode != d.inControlMode {
		log.Printf("toggleControlMode: State changed, calling SetControlMode(%v)", d.inControlMode)
		d.activeWorkspace.SetControlMode(d.inControlMode)
	} else {
		log.Printf("toggleControlMode: State didn't change or no active workspace")
	}

	var eventType EventType
	if d.inControlMode {
		eventType = EventControlOn
	} else {
		eventType = EventControlOff
	}

	if d.activeWorkspace != nil {
		d.activeWorkspace.Broadcast(Event{Type: eventType})
	}
	d.broadcastStateUpdate()
}

func (d *Desktop) toggleZoom() {
	if d.activeWorkspace == nil {
		return
	}

	mainX, mainY, mainW, mainH := d.getMainArea()

	var effect *ZoomEffect
	if d.zoomedPane == nil { // ZOOM IN
		nodeToZoom := d.activeWorkspace.tree.ActiveLeaf
		if nodeToZoom == nil || nodeToZoom.Pane == nil {
			return
		}

		p := nodeToZoom.Pane
		start := PaneRect{x: p.absX0, y: p.absY0, w: p.Width(), h: p.Height()}
		end := PaneRect{x: mainX, y: mainY, w: mainW, h: mainH}

		effect = NewZoomEffect(d.activeWorkspace, nodeToZoom, start, end, 250*time.Millisecond, func() {
			d.zoomedPane = nodeToZoom
			d.recalculateLayout()
			d.broadcastStateUpdate()
		})
	} else { // ZOOM OUT
		nodeToUnZoom := d.zoomedPane
		d.zoomedPane = nil // Immediately set to nil to restore original layout for calculation

		// Recalculate layout to find the target un-zoomed position
		d.recalculateLayout()

		p := nodeToUnZoom.Pane
		end := PaneRect{x: p.absX0, y: p.absY0, w: p.Width(), h: p.Height()}
		start := PaneRect{x: mainX, y: mainY, w: mainW, h: mainH}

		p.setDimensions(start.x, start.y, start.x+start.w, start.y+start.h)

		effect = NewZoomEffect(d.activeWorkspace, nodeToUnZoom, start, end, 250*time.Millisecond, func() {
			d.recalculateLayout()
			d.broadcastStateUpdate()
		})
	}

	if effect != nil {
		d.activeWorkspace.AddEffect(effect)
		d.activeWorkspace.animator.AnimateTo(effect, 1.0, 250*time.Millisecond, func() {
			// Call cleanup before removing effect to reset z-order
			effect.Cleanup()
			d.activeWorkspace.RemoveEffect(effect)
			if effect.onComplete != nil {
				effect.onComplete()
			}
		})
	}
}

// handleControlMode processes all commands when the Desktop is in control mode.
func (d *Desktop) handleControlMode(ev *tcell.EventKey) {
	if ev.Key() == tcell.KeyEsc {
		d.toggleControlMode()
		return
	}

	if d.subControlMode != 0 {
		switch d.subControlMode {
		case 'w':
			d.activeWorkspace.SwapActivePane(keyToDirection(ev))
		}
		d.toggleControlMode()
		return
	}

	if ev.Modifiers()&tcell.ModCtrl != 0 {
		d.resizeSelection = d.activeWorkspace.handleInteractiveResize(ev, d.resizeSelection)
		return
	}

	r := ev.Rune()
	if r >= '1' && r <= '9' {
		wsID, _ := strconv.Atoi(string(r))
		d.SwitchToWorkspace(wsID)
		d.toggleControlMode()
		return
	}
	// Handle the command, then exit control mode (unless specified otherwise)
	exitControlMode := true
	switch r {
	case 'x':
		if d.zoomedPane != nil {
			d.activeWorkspace.CloseActivePane()
			d.zoomedPane = nil
		} else {
			d.activeWorkspace.CloseActivePane()
		}
	case '|':
		d.activeWorkspace.PerformSplit(Vertical)
	case '-':
		d.activeWorkspace.PerformSplit(Horizontal)
	case 'w':
		d.subControlMode = 'w'
		d.broadcastStateUpdate()
		exitControlMode = false // Stay in control mode for sub-mode
	case 'z':
		d.toggleZoom()
	}
	if exitControlMode {
		d.toggleControlMode()
	}
}

// broadcastStateUpdate now broadcasts on the Desktop's dispatcher
func (d *Desktop) broadcastStateUpdate() {
	if d.activeWorkspace == nil {
		return
	}
	var title string
	if d.zoomedPane != nil {
		if d.zoomedPane.Pane != nil {
			title = d.zoomedPane.Pane.getTitle()
		}
	} else {
		title = d.activeWorkspace.tree.GetActiveTitle()
	}

	allWsIDs := make([]int, 0, len(d.workspaces))
	for id := range d.workspaces {
		allWsIDs = append(allWsIDs, id)
	}
	sort.Ints(allWsIDs)

	d.dispatcher.Broadcast(Event{
		Type:    EventStateUpdate,
		Payload: d.currentStatePayload(allWsIDs, title),
	})
	//	if d.activeWorkspace != nil {
	//		d.activeWorkspace.Refresh()
	//	}
}

func (d *Desktop) currentStatePayload(allWsIDs []int, title string) StatePayload {
	if allWsIDs == nil {
		allWsIDs = make([]int, 0, len(d.workspaces))
		for id := range d.workspaces {
			allWsIDs = append(allWsIDs, id)
		}
		sort.Ints(allWsIDs)
	}
	if title == "" {
		if d.inControlMode && d.zoomedPane != nil {
			if d.zoomedPane.Pane != nil {
				title = d.zoomedPane.Pane.getTitle()
			}
		} else if d.activeWorkspace != nil {
			title = d.activeWorkspace.tree.GetActiveTitle()
		}
	}
	workspaceID := 0
	if d.activeWorkspace != nil {
		workspaceID = d.activeWorkspace.id
	}
	var zoomID [16]byte
	zoomed := false
	if d.zoomedPane != nil && d.zoomedPane.Pane != nil {
		zoomed = true
		zoomID = d.zoomedPane.Pane.ID()
	}
	return StatePayload{
		AllWorkspaces:  allWsIDs,
		WorkspaceID:    workspaceID,
		InControlMode:  d.inControlMode,
		SubMode:        d.subControlMode,
		ActiveTitle:    title,
		DesktopBgColor: d.DefaultBgColor,
		Zoomed:         zoomed,
		ZoomedPaneID:   zoomID,
	}
}

// CurrentStatePayload exposes the latest desktop state snapshot.
func (d *Desktop) CurrentStatePayload() StatePayload {
	return d.currentStatePayload(nil, "")
}

func (d *Desktop) SwitchToWorkspace(id int) {
	if d.activeWorkspace != nil && d.activeWorkspace.id == id {
		return
	}

	if d.zoomedPane != nil {
		d.zoomedPane = nil
	}

	if ws, exists := d.workspaces[id]; exists {
		d.activeWorkspace = ws
	} else {
		ws, err := newScreen(id, d.ShellAppFactory, d.appLifecycle, d)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating workspace: %v\n", err)
			return
		}
		d.workspaces[id] = ws
		d.activeWorkspace = ws

		if d.WelcomeAppFactory != nil {
			welcomeApp := d.WelcomeAppFactory()
			ws.AddApp(welcomeApp)
		}
	}

	// Apply current control mode state to the new workspace
	if d.activeWorkspace != nil {
		d.activeWorkspace.SetControlMode(d.inControlMode)
	}

	for _, sp := range d.statusPanes {
		sp.app.SetRefreshNotifier(d.activeWorkspace.refreshChan)
	}
	d.recalculateLayout()
	d.broadcastStateUpdate()
	d.notifyFocusActive()
}

func (d *Desktop) notifyFocusActive() {
	if d.activeWorkspace == nil || d.activeWorkspace.tree == nil {
		return
	}
	d.notifyFocusNode(d.activeWorkspace.tree.ActiveLeaf)
}

func (d *Desktop) notifyFocusNode(node *Node) {
	if node == nil || node.Pane == nil {
		return
	}
	d.notifyFocus(node.Pane.ID())
}

func (d *Desktop) notifyPaneState(id [16]byte, active, resizing bool) {
	d.paneStateMu.RLock()
	listeners := append([]PaneStateListener(nil), d.paneStateListeners...)
	d.paneStateMu.RUnlock()
	for _, l := range listeners {
		l.PaneStateChanged(id, active, resizing)
	}
}

func (d *Desktop) forEachPane(fn func(*pane)) {
	if fn == nil {
		return
	}
	for _, ws := range d.workspaces {
		if ws == nil || ws.tree == nil {
			continue
		}
		var traverse func(*Node)
		traverse = func(node *Node) {
			if node == nil {
				return
			}
			if node.Pane != nil {
				fn(node.Pane)
			}
			for _, child := range node.Children {
				traverse(child)
			}
		}
		traverse(ws.tree.Root)
	}
}

// PaneStates returns the current pane flags across all workspaces.
func (d *Desktop) PaneStates() []PaneStateSnapshot {
	states := make([]PaneStateSnapshot, 0)
	d.forEachPane(func(p *pane) {
		states = append(states, PaneStateSnapshot{ID: p.ID(), Active: p.IsActive, Resizing: p.IsResizing})
	})
	return states
}

func (d *Desktop) notifyFocus(paneID [16]byte) {
	d.focusMu.RLock()
	listeners := append([]DesktopFocusListener(nil), d.focusListeners...)
	d.focusMu.RUnlock()
	for _, listener := range listeners {
		listener.PaneFocused(paneID)
	}
}

func (d *Desktop) appFromSnapshot(snap PaneSnapshot) App {
	if snap.AppType != "" {
		if factory, ok := d.snapshotFactories[snap.AppType]; ok {
			cfg := cloneAppConfig(snap.AppConfig)
			if app := factory(snap.Title, cfg); app != nil {
				return app
			}
		}
	}
	return NewSnapshotApp(snap.Title, snap.Buffer)
}

func (d *Desktop) draw() {

	if d.zoomedPane != nil {
		mainX, mainY, _, _ := d.getMainArea()
		if d.zoomedPane.Pane != nil {
			paneBuffer := d.zoomedPane.Pane.Render()
			blit(d.display, mainX, mainY, paneBuffer)
		}
	} else if d.activeWorkspace != nil {
		d.activeWorkspace.draw(d.display)
	}
	d.drawStatusPanes(d.display)
	d.display.Show()
}

func (d *Desktop) Close() {
	d.closeOnce.Do(func() {
		// Stop animation timer
		if d.animationTicker != nil {
			d.animationTicker.Stop()
		}
		if d.animationStop != nil {
			close(d.animationStop)
		}

		close(d.quit)
		for _, ws := range d.workspaces {
			ws.Close()
		}
		for _, sp := range d.statusPanes {
			d.appLifecycle.StopApp(sp.app)
		}
		if d.display != nil {
			d.display.Fini()
		}
	})
}

func (d *Desktop) AddCustomEffect(effect Effect) {
	if d.activeWorkspace != nil {
		d.activeWorkspace.AddEffect(effect)
	}
}

// AddCustomPaneEffect adds a custom effect to the active pane
func (d *Desktop) AddCustomPaneEffect(effect Effect) {
	if d.activeWorkspace != nil &&
		d.activeWorkspace.tree.ActiveLeaf != nil &&
		d.activeWorkspace.tree.ActiveLeaf.Pane != nil {
		d.activeWorkspace.tree.ActiveLeaf.Pane.AddEffect(effect)
	}
}

func (d *Desktop) getStyle(fg, bg tcell.Color, bold, underline, reverse bool) tcell.Style {
	key := styleKey{fg: fg, bg: bg, bold: bold, underline: underline, reverse: reverse}
	if st, ok := d.styleCache[key]; ok {
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
	d.styleCache[key] = st
	return st
}

// queryTerminalColors attempts to query the terminal for its default colors with a timeout.
func queryTerminalColors(ctx context.Context) (fg tcell.Color, bg tcell.Color, err error) {
	// Default to standard colors in case of any error
	fg = tcell.ColorWhite
	bg = tcell.ColorBlack

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fg, bg, fmt.Errorf("could not open /dev/tty: %w", err)
	}
	defer tty.Close()

	// Use a context to make sure we don't block forever on raw mode or reads
	done := make(chan struct{})
	go func() {
		defer close(done)

		var state *term.State
		state, err = term.MakeRaw(int(tty.Fd()))
		if err != nil {
			err = fmt.Errorf("failed to make raw terminal: %w", err)
			return
		}
		defer term.Restore(int(tty.Fd()), state)

		query := func(code int) (tcell.Color, error) {
			seq := fmt.Sprintf("\x1b]%d;?\a", code)
			if _, writeErr := tty.WriteString(seq); writeErr != nil {
				return tcell.ColorDefault, writeErr
			}

			resp := make([]byte, 0, 64)
			buf := make([]byte, 1)

			// Loop with context cancellation check
			for {
				select {
				case <-ctx.Done():
					return tcell.ColorDefault, ctx.Err()
				default:
				}

				// Set a very short deadline on each read to make the loop responsive to cancellation
				readDeadline := time.Now().Add(10 * time.Millisecond)
				if deadline, ok := ctx.Deadline(); ok && deadline.Before(readDeadline) {
					readDeadline = deadline
				}
				tty.SetReadDeadline(readDeadline)

				n, readErr := tty.Read(buf)
				if readErr != nil {
					if os.IsTimeout(readErr) {
						continue // Expected timeout, check context and loop again
					}
					return tcell.ColorDefault, fmt.Errorf("failed to read from tty: %w", readErr)
				}
				resp = append(resp, buf[:n]...)
				// BEL or ST terminates the response
				if buf[0] == '\a' || (len(resp) > 1 && resp[len(resp)-2] == '\x1b' && resp[len(resp)-1] == '\\') {
					break
				}
			}

			pattern := fmt.Sprintf(`\x1b\]%d;rgb:([0-9A-Fa-f]{1,4})/([0-9A-Fa-f]{1,4})/([0-9A-Fa-f]{1,4})`, code)
			re := regexp.MustCompile(pattern)
			m := re.FindStringSubmatch(string(resp))
			if len(m) != 4 {
				return tcell.ColorDefault, fmt.Errorf("unexpected response format: %q", resp)
			}

			hex2int := func(s string) (int32, error) {
				// Pad to 4 characters for consistent parsing (e.g., "ff" -> "00ff")
				if len(s) < 4 {
					s = "00" + s
					s = s[len(s)-4:]
				}
				v, err := strconv.ParseInt(s, 16, 32)
				// Scale 16-bit color down to 8-bit for tcell
				return int32(v / 257), err
			}
			r, _ := hex2int(m[1])
			g, _ := hex2int(m[2])
			b, _ := hex2int(m[3])

			return tcell.NewRGBColor(r, g, b), nil
		}

		var queryErr error
		fg, queryErr = query(10)
		if queryErr != nil {
			err = fmt.Errorf("failed to query foreground color: %w", queryErr)
			// Don't return yet, try to get the background color
		}

		bg, queryErr = query(11)
		if queryErr != nil {
			err = fmt.Errorf("failed to query background color: %w", queryErr)
		}
	}()

	select {
	case <-ctx.Done():
		return tcell.ColorWhite, tcell.ColorBlack, ctx.Err()
	case <-done:
		// The goroutine finished, return its results.
		// If 'err' is not nil, the default fg/bg will be used.
		return fg, bg, err
	}
}

func initDefaultColors() (tcell.Color, tcell.Color, error) {
	// Set a timeout for the entire color query operation.
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	fg, bg, err := queryTerminalColors(ctx)
	if err != nil {
		// If there's any error (including timeout), log it and return safe defaults.
		// log.Printf("Could not query terminal colors, using defaults: %v", err)
		return tcell.ColorWhite, tcell.ColorBlack, nil
	}
	return fg, bg, nil
}

func (d *Desktop) TestEffectSystem() {
	log.Printf("=== EFFECT SYSTEM TEST ===")

	// Create a test effect
	testEffect := NewFadeEffect(d, tcell.NewRGBColor(255, 0, 0))
	log.Printf("Test effect created with intensity: %.3f", testEffect.GetIntensity())

	// Test setting intensity directly
	testEffect.SetIntensity(0.5)
	log.Printf("After SetIntensity(0.5): %.3f", testEffect.GetIntensity())

	testEffect.SetIntensity(0.0)
	log.Printf("After SetIntensity(0.0): %.3f", testEffect.GetIntensity())

	// Test animator
	animator := NewEffectAnimator()
	log.Printf("Starting animation test...")

	animator.AnimateTo(testEffect, 0.75, 1*time.Second, func() {
		log.Printf("Animation completed, final intensity: %.3f", testEffect.GetIntensity())
	})

	// Check intensity during animation
	time.Sleep(100 * time.Millisecond)
	log.Printf("After 100ms: %.3f", testEffect.GetIntensity())

	time.Sleep(400 * time.Millisecond)
	log.Printf("After 500ms: %.3f", testEffect.GetIntensity())

	time.Sleep(600 * time.Millisecond)
	log.Printf("After 1100ms: %.3f", testEffect.GetIntensity())

	log.Printf("=== END EFFECT SYSTEM TEST ===")
}

func (d *Desktop) logActiveAnimations() {
	if d.activeWorkspace == nil {
		return
	}

	screenCount := d.activeWorkspace.effects.GetActiveAnimationCount()
	if screenCount > 0 {
		log.Printf("Active screen animations: %d", screenCount)
	}

	d.activeWorkspace.tree.Traverse(func(node *Node) {
		if node.Pane != nil {
			paneCount := node.Pane.effects.GetActiveAnimationCount()
			if paneCount > 0 {
				log.Printf("Active animations in pane '%s': %d", node.Pane.getTitle(), paneCount)
			}
		}
	})
}

//func blitDiff(tcs tcell.Screen, x0, y0 int, oldBuf, buf [][]Cell) {
//	for y, row := range buf {
//		for x, cell := range row {
//			if y >= len(oldBuf) || x >= len(oldBuf[y]) || cell != oldBuf[y][x] {
//				tcs.SetContent(x0+x, y0+y, cell.Ch, nil, cell.Style)
//			}
//		}
//	}
//}
//
//func blit(tcs tcell.Screen, x, y int, buf [][]Cell) {
//	for r, row := range buf {
//		for c, cell := range row {
//			tcs.SetContent(x+c, y+r, cell.Ch, nil, cell.Style)
//		}
//	}
//}

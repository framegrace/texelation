// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/desktop.go
// Summary: Implements desktop capabilities for the core desktop engine.
// Usage: Used throughout the project to implement desktop inside the desktop and panes.

package texel

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"golang.org/x/term"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"texelation/registry"
	"texelation/texel/theme"
	"time"
)

// AppRegistry is an alias for the registry.Registry type.
type AppRegistry = registry.Registry

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
	id   [16]byte
}

type PaneRect struct {
	x, y, w, h int
}

func newStatusPaneID(app App) [16]byte {
	var id [16]byte
	if _, err := rand.Read(id[:]); err == nil {
		return id
	}
	fingerprint := fmt.Sprintf("status:%p:%d", app, time.Now().UnixNano())
	sum := sha1.Sum([]byte(fingerprint))
	copy(id[:], sum[:])
	return id
}

// Desktop manages a collection of workspaces (Screens).
type DesktopEngine struct {
	display           ScreenDriver
	workspaces        map[int]*Workspace
	activeWorkspace   *Workspace
	statusPanes       []*StatusPane
	floatingPanels    []*FloatingPanel
	quit              chan struct{}
	closeOnce         sync.Once
	ShellAppFactory   AppFactory
	InitAppName       string
	styleCache        map[styleKey]tcell.Style
	DefaultFgColor    tcell.Color
	DefaultBgColor    tcell.Color
	dispatcher        *EventDispatcher
	statusBuffer      BufferStore
	appLifecycle      AppLifecycleManager
	registry          *AppRegistry

	// Global state now lives on the Desktop
	inControlMode   bool
	subControlMode  rune
	resizeSelection *selectedBorder
	zoomedPane      *Node

	lastMouseX           int
	lastMouseY           int
	lastMouseButtons     tcell.ButtonMask
	lastMouseModifier    tcell.ModMask
	clipboard            map[string][]byte
	lastClipboardMime    string
	selectionMu          sync.Mutex
	selectionActive      bool
	selectionPane        *pane
	selectionHandler     SelectionHandler
	pendingClipboardMime string
	pendingClipboardData []byte
	hasPendingClipboard  bool
	focusMu              sync.RWMutex
	focusListeners       []DesktopFocusListener
	paneStateMu          sync.RWMutex
	paneStateListeners   []PaneStateListener
	snapshotFactories    map[string]SnapshotFactory
	viewportMu           sync.RWMutex
	viewportWidth        int
	viewportHeight       int
	hasViewport          bool

	stateMu      sync.Mutex
	hasLastState bool
	lastState    StatePayload

	refreshMu      sync.RWMutex
	refreshHandler func()
}

// FloatingPanel represents an app floating above the workspace.
type FloatingPanel struct {
	app    App
	x, y   int
	width  int
	height int
	modal  bool
	id     [16]byte
}

func newFloatingPanelID(app App) [16]byte {
	var id [16]byte
	if _, err := rand.Read(id[:]); err == nil {
		return id
	}
	fingerprint := fmt.Sprintf("float:%p:%d", app, time.Now().UnixNano())
	sum := sha1.Sum([]byte(fingerprint))
	copy(id[:], sum[:])
	return id
}

// PaneStateSnapshot captures dynamic pane flags for external consumers.
type PaneStateSnapshot struct {
	ID               [16]byte
	Active           bool
	Resizing         bool
	ZOrder           int
	HandlesSelection bool
}

// NewDesktopEngine creates and initializes a new desktop engine.
func NewDesktopEngine(shellFactory AppFactory, initAppName string) (*DesktopEngine, error) {
	tcellScreen, err := tcell.NewScreen()
	if err != nil {
		return nil, err
	}

	driver := NewTcellScreenDriver(tcellScreen)
	lifecycle := &LocalAppLifecycle{}
	return NewDesktopEngineWithDriver(driver, shellFactory, initAppName, lifecycle)
}

// NewDesktopEngineWithDriver wires a DesktopEngine using the provided screen driver and
// lifecycle manager. It exists primarily to support tests and future remote
// runtimes.
func NewDesktopEngineWithDriver(driver ScreenDriver, shellFactory AppFactory, initAppName string, lifecycle AppLifecycleManager) (*DesktopEngine, error) {
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
	defbg := tm.GetSemanticColor("bg.base").TrueColor()
	deffg := tm.GetSemanticColor("text.primary").TrueColor()
	defStyle := tcell.StyleDefault.Background(defbg).Foreground(deffg)
	driver.SetStyle(defStyle)
	driver.HideCursor()

	// Initialize app registry
	reg := registry.New()

	// Register built-in shell app (wrap texel.AppFactory to registry.AppFactory)
	reg.RegisterBuiltIn("texelterm", func() interface{} { return shellFactory() })

	// Note: Other apps (launcher, welcome, etc.) are registered in main.go
	// after Desktop is created, since launcher needs access to the registry

	d := &DesktopEngine{
		display:            driver,
		workspaces:         make(map[int]*Workspace),
		statusPanes:        make([]*StatusPane, 0),
		floatingPanels:     make([]*FloatingPanel, 0),
		quit:               make(chan struct{}),
		ShellAppFactory:    shellFactory,
		InitAppName:        initAppName,
		styleCache:         make(map[styleKey]tcell.Style),
		DefaultFgColor:     deffg,
		DefaultBgColor:     defbg,
		dispatcher:         NewEventDispatcher(),
		statusBuffer:       NewInMemoryBufferStore(),
		appLifecycle:       lifecycle,
		registry:           reg,
		inControlMode:      false,
		subControlMode:     0,
		clipboard:          make(map[string][]byte),
		focusListeners:     make([]DesktopFocusListener, 0),
		paneStateListeners: make([]PaneStateListener, 0),
		snapshotFactories:  make(map[string]SnapshotFactory),
	}

	// Scan for external apps
	d.loadApps()

	log.Printf("NewDesktop: Created with inControlMode=%v, %d apps registered, InitAppName=%s", d.inControlMode, d.registry.Count(), d.InitAppName)

	return d, nil
}

// loadApps scans the user's app directory and loads available apps.
func (d *DesktopEngine) loadApps() {
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Printf("Desktop: Failed to get user config dir: %v", err)
		return
	}

	appsDir := filepath.Join(configDir, "texelation", "apps")
	if err := d.registry.Scan(appsDir); err != nil {
		log.Printf("Desktop: Failed to scan apps: %v", err)
	}
}

// ForceRefresh clears caches and triggers a full repaint.
// When triggered by SIGHUP, this also reloads theme and apps.
func (d *DesktopEngine) ForceRefresh() {
	log.Println("Desktop: ForceRefresh triggered")

	// Reload apps
	d.loadApps()

	// Clear style cache to force re-evaluation of colors
	d.styleCache = make(map[styleKey]tcell.Style)

	// Trigger a full layout and state broadcast
	d.recalculateLayout()
	d.broadcastStateUpdate()
	d.broadcastTreeChanged()

	log.Println("Desktop: Broadcasting EventThemeChanged")
	d.dispatcher.Broadcast(Event{Type: EventThemeChanged})

	// Notify refresh handler if one is set (to wake up the loop)
	if d.refreshHandler != nil {
		d.refreshHandler()
	}
}

func (d *DesktopEngine) Subscribe(listener Listener) {
	d.dispatcher.Subscribe(listener)
}

func (d *DesktopEngine) Unsubscribe(listener Listener) {
	d.dispatcher.Unsubscribe(listener)
}

// Registry returns the app registry for this desktop.
func (d *DesktopEngine) Registry() *AppRegistry {
	return d.registry
}

// ActiveWorkspace returns the currently active workspace.
func (d *DesktopEngine) ActiveWorkspace() *Workspace {
	return d.activeWorkspace
}

func (d *DesktopEngine) RegisterFocusListener(listener DesktopFocusListener) {
	if listener == nil {
		return
	}
	d.focusMu.Lock()
	d.focusListeners = append(d.focusListeners, listener)
	d.focusMu.Unlock()
	d.notifyFocusActive()
}

// RegisterPaneStateListener registers a listener for pane active/resizing changes.
func (d *DesktopEngine) RegisterPaneStateListener(listener PaneStateListener) {
	if listener == nil {
		return
	}
	d.paneStateMu.Lock()
	d.paneStateListeners = append(d.paneStateListeners, listener)
	d.paneStateMu.Unlock()
}

// RegisterSnapshotFactory registers a factory used to restore apps from snapshot metadata.
func (d *DesktopEngine) RegisterSnapshotFactory(appType string, factory SnapshotFactory) {
	if appType == "" || factory == nil {
		return
	}
	d.snapshotFactories[appType] = factory
}

// UnregisterFocusListener removes a previously registered focus listener.
func (d *DesktopEngine) UnregisterFocusListener(listener DesktopFocusListener) {
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
func (d *DesktopEngine) UnregisterPaneStateListener(listener PaneStateListener) {
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

// ShowFloatingPanel displays an app in a floating overlay.
func (d *DesktopEngine) ShowFloatingPanel(app App, x, y, w, h int) {
	if app == nil {
		return
	}
	
	// Check if app is already floating? Maybe not needed for now.
	
	panel := &FloatingPanel{
		app:    app,
		x:      x,
		y:      y,
		width:  w,
		height: h,
		modal:  true,
		id:     newFloatingPanelID(app),
	}
	
	d.floatingPanels = append(d.floatingPanels, panel)
	
	if listener, ok := app.(Listener); ok {
		d.Subscribe(listener)
	}

	if d.activeWorkspace != nil {
		app.SetRefreshNotifier(d.activeWorkspace.refreshChan)
	}

	d.appLifecycle.StartApp(app, nil)
	app.Resize(w, h)
	
	d.notifyPaneState(panel.id, true, false, ZOrderFloating, false)

	d.recalculateLayout()
	d.broadcastTreeChanged()
	// d.broadcastStateUpdate() // TODO: Notify focus change if we focus the panel?
}

// CloseFloatingPanel removes a floating panel.
func (d *DesktopEngine) CloseFloatingPanel(panel *FloatingPanel) {
	if panel == nil {
		return
	}
	
	found := false
	for i, p := range d.floatingPanels {
		if p == panel {
			d.floatingPanels = append(d.floatingPanels[:i], d.floatingPanels[i+1:]...)
			found = true
			break
		}
	}
	
	if found {
		d.appLifecycle.StopApp(panel.app)
		d.recalculateLayout()
		d.broadcastTreeChanged()
	}
}

// closeFloatingPanelByApp finds and closes the floating panel hosting the given app.
func (d *DesktopEngine) closeFloatingPanelByApp(app App) {
	var panel *FloatingPanel
	for _, fp := range d.floatingPanels {
		if fp.app == app {
			panel = fp
			break
		}
	}
	if panel != nil {
		d.CloseFloatingPanel(panel)
	}
}

// AddStatusPane adds a new status pane to the desktop.
func (d *DesktopEngine) AddStatusPane(app App, side Side, size int) {
	sp := &StatusPane{
		app:  app,
		side: side,
		size: size,
		id:   newStatusPaneID(app),
	}
	d.statusPanes = append(d.statusPanes, sp)

	if listener, ok := app.(Listener); ok {
		d.Subscribe(listener)
	}

	if d.activeWorkspace != nil {
		app.SetRefreshNotifier(d.activeWorkspace.refreshChan)
	}

	d.appLifecycle.StartApp(app, nil)
	d.recalculateLayout()
	d.broadcastTreeChanged()
}

func (d *DesktopEngine) getMainArea() (int, int, int, int) {
	w, h := d.viewportSize()
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

func (d *DesktopEngine) recalculateLayout() {
	w, h := d.viewportSize()
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
	
	// Resize floating panels if needed (e.g. ensure they fit?)
	// For now, leave them as requested.

	if d.zoomedPane != nil {
		if d.zoomedPane.Pane != nil {
			d.zoomedPane.Pane.setDimensions(mainX, mainY, mainX+mainW, mainY+mainH)
		}
	} else if d.activeWorkspace != nil {
		d.activeWorkspace.setArea(mainX, mainY, mainW, mainH)
	}
}

func (d *DesktopEngine) viewportSize() (int, int) {
	d.viewportMu.RLock()
	defer d.viewportMu.RUnlock()
	if d.hasViewport && d.viewportWidth > 0 && d.viewportHeight > 0 {
		return d.viewportWidth, d.viewportHeight
	}
	return d.display.Size()
}

func (d *DesktopEngine) handleEvent(ev tcell.Event) {
	switch tev := ev.(type) {
	case *tcell.EventResize:
		d.recalculateLayout()
		return
	case *tcell.EventMouse:
		d.handleMouseEvent(tev)
		return
	}

	key, ok := ev.(*tcell.EventKey)
	if !ok {
		return
	}
	
	// Global Shortcuts
	if key.Key() == tcell.KeyF1 {
		d.launchHelpOverlay()
		return
	}
	
	// Check floating panels (topmost first)
	// Iterate in reverse to find topmost modal
	for i := len(d.floatingPanels) - 1; i >= 0; i-- {
		fp := d.floatingPanels[i]
		if fp.modal {
			fp.app.HandleKey(key)
			return
		}
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

func (d *DesktopEngine) launchLauncherOverlay() {
	// Check if already open
	for _, fp := range d.floatingPanels {
		if fp.app.GetTitle() == "Launcher" {
			d.CloseFloatingPanel(fp)
			return
		}
	}

	appInstance := d.registry.CreateApp("launcher", nil)
	app, ok := appInstance.(App)
	if !ok {
		return
	}

	// Register control bus handlers if the app provides a control bus
	if provider, ok := app.(ControlBusProvider); ok {
		// Register handler for app selection
		provider.RegisterControl("launcher.select-app", "Launch selected app in active pane", func(payload interface{}) error {
			appName, ok := payload.(string)
			if !ok {
				return nil
			}

			// Close the launcher floating panel
			d.closeFloatingPanelByApp(app)

			// Launch the selected app in the active pane
			if ws := d.ActiveWorkspace(); ws != nil {
				if pane := ws.ActivePane(); pane != nil {
					pane.ReplaceWithApp(appName, nil)
				}
			}
			return nil
		})

		// Register handler for launcher close
		provider.RegisterControl("launcher.close", "Close launcher overlay", func(payload interface{}) error {
			d.closeFloatingPanelByApp(app)
			return nil
		})
	}

	vw, vh := d.viewportSize()
	w := 60
	h := 20
	if w > vw {
		w = vw - 2
	}
	if h > vh {
		h = vh - 2
	}
	x := (vw - w) / 2
	y := (vh - h) / 2

	d.ShowFloatingPanel(app, x, y, w, h)
}

func (d *DesktopEngine) launchHelpOverlay() {
	// Check if already open
	for _, fp := range d.floatingPanels {
		if fp.app.GetTitle() == "Help" {
			d.CloseFloatingPanel(fp)
			return
		}
	}

	appInstance := d.registry.CreateApp("help", nil)
	app, ok := appInstance.(App)
	if !ok {
		return
	}

	// Register control bus handlers if the app provides a control bus
	if provider, ok := app.(ControlBusProvider); ok {
		// Register handler for help close
		provider.RegisterControl("help.close", "Close help overlay", func(payload interface{}) error {
			d.closeFloatingPanelByApp(app)
			return nil
		})
	}

	vw, vh := d.viewportSize()
	w := 60
	h := 30 // Help needs more height
	if w > vw {
		w = vw - 2
	}
	if h > vh {
		h = vh - 2
	}
	x := (vw - w) / 2
	y := (vh - h) / 2

	d.ShowFloatingPanel(app, x, y, w, h)
}

// InjectMouseEvent records the latest mouse event metadata from remote clients.
func (d *DesktopEngine) InjectMouseEvent(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) {
	d.processMouseEvent(x, y, buttons, modifiers)
}

// HandleClipboardSet stores clipboard contents using the provided MIME type.
func (d *DesktopEngine) HandleClipboardSet(mime string, data []byte) {
	if d.clipboard == nil {
		d.clipboard = make(map[string][]byte)
	}
	d.clipboard[mime] = append([]byte(nil), data...)
}

// HandleClipboardGet records the last clipboard lookup.
func (d *DesktopEngine) HandleClipboardGet(mime string) []byte {
	d.lastClipboardMime = mime
	if d.clipboard == nil {
		return nil
	}
	return append([]byte(nil), d.clipboard[mime]...)
}

// HandlePaste routes paste data to the active pane.
func (d *DesktopEngine) HandlePaste(data []byte) {
	if len(data) == 0 || d.inControlMode {
		return
	}
	if d.zoomedPane != nil && d.zoomedPane.Pane != nil {
		d.zoomedPane.Pane.handlePaste(data)
		return
	}
	if d.activeWorkspace != nil {
		d.activeWorkspace.handlePaste(data)
	}
}

// HandleThemeUpdate applies runtime theme overrides.
func (d *DesktopEngine) HandleThemeUpdate(section, key, value string) {
	config := theme.Get()
	if _, ok := config[section]; !ok {
		config[section] = theme.Section{}
	}
	config[section][key] = value
}

// LastMousePosition returns the most recently recorded mouse coordinates.
func (d *DesktopEngine) LastMousePosition() (int, int) {
	return d.lastMouseX, d.lastMouseY
}

// LastMouseButtons exposes the last recorded button mask.
func (d *DesktopEngine) LastMouseButtons() tcell.ButtonMask {
	return d.lastMouseButtons
}

// LastMouseModifiers exposes the last recorded modifier mask.
func (d *DesktopEngine) LastMouseModifiers() tcell.ModMask {
	return d.lastMouseModifier
}

// InjectKeyEvent allows external callers (e.g., remote clients) to deliver key
// input directly into the desktop event pipeline.
func (d *DesktopEngine) InjectKeyEvent(key tcell.Key, ch rune, modifiers tcell.ModMask) {
	event := tcell.NewEventKey(key, ch, modifiers)
	d.handleEvent(event)
}

func (d *DesktopEngine) handleMouseEvent(ev *tcell.EventMouse) {
	if ev == nil {
		return
	}
	x, y := ev.Position()
	d.processMouseEvent(x, y, ev.Buttons(), ev.Modifiers())
}

func (d *DesktopEngine) processMouseEvent(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) {
	prevButtons := d.lastMouseButtons

	wheelDX, wheelDY := wheelDeltaFromMask(buttons)
	if wheelDX != 0 || wheelDY != 0 {
		// Update position and modifiers, but keep lastMouseButtons (to preserve drag state)
		// Wheel events often don't report held buttons correctly.
		d.lastMouseX = x
		d.lastMouseY = y
		d.lastMouseModifier = modifiers

		d.dispatchMouseWheel(x, y, wheelDX, wheelDY, modifiers)
		return
	}

	d.lastMouseX = x
	d.lastMouseY = y
	d.lastMouseButtons = buttons
	d.lastMouseModifier = modifiers

	if d.activeWorkspace != nil {
		if d.activeWorkspace.handleMouseResize(x, y, buttons, prevButtons) {
			return
		}
	}

	d.handleAppSelection(x, y, buttons, modifiers, prevButtons)

	buttonPressed := buttons&tcell.Button1 != 0 && prevButtons&tcell.Button1 == 0
	if !buttonPressed {
		return
	}
	d.activatePaneAt(x, y)
}

func (d *DesktopEngine) paneAtCoordinates(x, y int) *pane {
	if d.zoomedPane != nil && d.zoomedPane.Pane != nil && d.zoomedPane.Pane.contains(x, y) {
		return d.zoomedPane.Pane
	}
	if d.activeWorkspace == nil {
		return nil
	}
	if node := d.activeWorkspace.nodeAt(x, y); node != nil {
		return node.Pane
	}
	return nil
}

func (d *DesktopEngine) dispatchMouseWheel(x, y, dx, dy int, modifiers tcell.ModMask) {
	pane := d.paneAtCoordinates(x, y)
	if pane == nil || !pane.handlesWheelEvents() {
		return
	}
	pane.handleMouseWheel(x, y, dx, dy, modifiers)
}

func wheelDeltaFromMask(mask tcell.ButtonMask) (int, int) {
	dx, dy := 0, 0
	if mask&tcell.WheelUp != 0 {
		dy--
	}
	if mask&tcell.WheelDown != 0 {
		dy++
	}
	if mask&tcell.WheelLeft != 0 {
		dx--
	}
	if mask&tcell.WheelRight != 0 {
		dx++
	}
	return dx, dy
}

func (d *DesktopEngine) activatePaneAt(x, y int) {
	if d.inControlMode {
		return
	}

	ws := d.activeWorkspace
	if d.zoomedPane != nil {
		if ws == nil {
			return
		}
		if d.zoomedPane.Pane != nil && d.zoomedPane.Pane.contains(x, y) {
			ws.activateLeaf(d.zoomedPane)
		}
		return
	}

	if ws == nil {
		return
	}

	if node := ws.nodeAt(x, y); node != nil {
		ws.activateLeaf(node)
	}
}

func (d *DesktopEngine) handleAppSelection(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask, prevButtons tcell.ButtonMask) {
	d.selectionMu.Lock()
	defer d.selectionMu.Unlock()

	start := buttons&tcell.Button1 != 0 && prevButtons&tcell.Button1 == 0
	release := buttons&tcell.Button1 == 0 && prevButtons&tcell.Button1 != 0
	dragging := buttons&tcell.Button1 != 0 && prevButtons&tcell.Button1 != 0

	if start {
		if d.selectionActive && d.selectionHandler != nil {
			d.selectionHandler.SelectionCancel()
		}
		d.selectionActive = false
		d.selectionHandler = nil
		d.selectionPane = nil

		pane := d.paneAtCoordinates(x, y)
		if pane != nil && pane.handlesSelectionEvents() {
			localX, localY := pane.contentLocalCoords(x, y)
			if pane.selectionHandler.SelectionStart(localX, localY, buttons, modifiers) {
				d.selectionActive = true
				d.selectionHandler = pane.selectionHandler
				d.selectionPane = pane
			}
		}
		return
	}

	if !d.selectionActive || d.selectionHandler == nil || d.selectionPane == nil {
		return
	}

	pane := d.selectionPane
	if dragging {
		localX, localY := pane.contentLocalCoords(x, y)
		d.selectionHandler.SelectionUpdate(localX, localY, buttons, modifiers)
		return
	}

	if release {
		localX, localY := pane.contentLocalCoords(x, y)
		mime, data, ok := d.selectionHandler.SelectionFinish(localX, localY, buttons, modifiers)
		d.selectionActive = false
		d.selectionHandler = nil
		d.selectionPane = nil
		if ok && len(data) > 0 {
			d.HandleClipboardSet(mime, data)
			d.pendingClipboardMime = mime
			d.pendingClipboardData = append([]byte(nil), data...)
			d.hasPendingClipboard = true
		}
		return
	}

	if buttons&tcell.Button1 == 0 {
		d.selectionHandler.SelectionCancel()
		d.selectionActive = false
		d.selectionHandler = nil
		d.selectionPane = nil
	}
}

func (d *DesktopEngine) PopPendingClipboard() (string, []byte, bool) {
	d.selectionMu.Lock()
	defer d.selectionMu.Unlock()
	if !d.hasPendingClipboard {
		return "", nil, false
	}
	mime := d.pendingClipboardMime
	data := append([]byte(nil), d.pendingClipboardData...)
	d.pendingClipboardMime = ""
	d.pendingClipboardData = nil
	d.hasPendingClipboard = false
	return mime, data, true
}

// broadcastStateUpdate now broadcasts on the Desktop's dispatcher
func (d *DesktopEngine) broadcastStateUpdate() {
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

	payload := d.currentStatePayload(allWsIDs, title)

	if !d.shouldBroadcastState(payload) {
		return
	}

	d.dispatcher.Broadcast(Event{
		Type:    EventStateUpdate,
		Payload: payload,
	})
	//	if d.activeWorkspace != nil {
	//		d.activeWorkspace.Refresh()
	//	}
}

func (d *DesktopEngine) SetRefreshHandler(handler func()) {
	d.refreshMu.Lock()
	d.refreshHandler = handler
	for _, ws := range d.workspaces {
		if ws != nil {
			ws.startRefreshMonitor()
		}
	}
	d.refreshMu.Unlock()
}

func (d *DesktopEngine) refreshHandlerFunc() func() {
	d.refreshMu.RLock()
	defer d.refreshMu.RUnlock()
	return d.refreshHandler
}

func (d *DesktopEngine) broadcastTreeChanged() {
	d.dispatcher.Broadcast(Event{Type: EventTreeChanged})
}

func (d *DesktopEngine) shouldBroadcastState(payload StatePayload) bool {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	if !d.hasLastState {
		d.storeLastState(payload)
		return true
	}
	if d.lastState.equal(payload) {
		return false
	}
	d.storeLastState(payload)
	return true
}

func (d *DesktopEngine) storeLastState(payload StatePayload) {
	d.lastState = payload
	if payload.AllWorkspaces != nil {
		d.lastState.AllWorkspaces = append([]int(nil), payload.AllWorkspaces...)
	}
	d.hasLastState = true
}

func (d *DesktopEngine) currentStatePayload(allWsIDs []int, title string) StatePayload {
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
func (d *DesktopEngine) CurrentStatePayload() StatePayload {
	return d.currentStatePayload(nil, "")
}

func (d *DesktopEngine) SwitchToWorkspace(id int) {
	if d.activeWorkspace != nil && d.activeWorkspace.id == id {
		return
	}

	if d.zoomedPane != nil {
		d.zoomedPane = nil
	}

	if ws, exists := d.workspaces[id]; exists {
		d.activeWorkspace = ws
	} else {
		ws, err := newWorkspace(id, d.ShellAppFactory, d.appLifecycle, d)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating workspace: %v\n", err)
			return
		}
		d.workspaces[id] = ws
		d.activeWorkspace = ws

		if d.InitAppName != "" {
			if appInstance := d.registry.CreateApp(d.InitAppName, nil); appInstance != nil {
				if app, ok := appInstance.(App); ok {
					ws.AddApp(app)
				} else {
					log.Printf("SwitchToWorkspace: Created app '%s' but it does not implement App interface", d.InitAppName)
				}
			} else {
				log.Printf("SwitchToWorkspace: Failed to create init app '%s'", d.InitAppName)
			}
		}
	}

	if handler := d.refreshHandlerFunc(); handler != nil && d.activeWorkspace != nil {
		d.activeWorkspace.startRefreshMonitor()
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
	d.broadcastTreeChanged()
}

func (d *DesktopEngine) notifyFocusActive() {
	if d.activeWorkspace == nil || d.activeWorkspace.tree == nil {
		return
	}
	d.notifyFocusNode(d.activeWorkspace.tree.ActiveLeaf)
}

func (d *DesktopEngine) notifyFocusNode(node *Node) {
	if node == nil || node.Pane == nil {
		return
	}
	d.notifyFocus(node.Pane.ID())
}

func (d *DesktopEngine) notifyPaneState(id [16]byte, active, resizing bool, z int, handlesSelection bool) {
	d.paneStateMu.RLock()
	listeners := append([]PaneStateListener(nil), d.paneStateListeners...)
	d.paneStateMu.RUnlock()
	for _, l := range listeners {
		l.PaneStateChanged(id, active, resizing, z, handlesSelection)
	}
}

func (d *DesktopEngine) forEachPane(fn func(*pane)) {
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
func (d *DesktopEngine) PaneStates() []PaneStateSnapshot {
	states := make([]PaneStateSnapshot, 0)
	d.forEachPane(func(p *pane) {
		states = append(states, PaneStateSnapshot{
			ID:               p.ID(),
			Active:           p.IsActive,
			Resizing:         p.IsResizing,
			ZOrder:           p.ZOrder,
			HandlesSelection: p.handlesSelectionEvents(),
		})
	})
	for _, fp := range d.floatingPanels {
		states = append(states, PaneStateSnapshot{
			ID:               fp.id,
			Active:           true,
			Resizing:         false,
			ZOrder:           ZOrderFloating,
			HandlesSelection: false,
		})
	}
	return states
}

// SetViewportSize overrides the desktop viewport dimensions, typically used by
// remote clients to dictate layout size.
func (d *DesktopEngine) SetViewportSize(cols, rows int) {
	d.viewportMu.Lock()
	d.viewportWidth = cols
	d.viewportHeight = rows
	d.hasViewport = cols > 0 && rows > 0
	d.viewportMu.Unlock()
	d.recalculateLayout()
	if d.activeWorkspace != nil {
		d.activeWorkspace.Refresh()
	}
}

func (d *DesktopEngine) notifyFocus(paneID [16]byte) {
	d.focusMu.RLock()
	listeners := append([]DesktopFocusListener(nil), d.focusListeners...)
	d.focusMu.RUnlock()
	for _, listener := range listeners {
		listener.PaneFocused(paneID)
	}
}

func (d *DesktopEngine) appFromSnapshot(snap PaneSnapshot) App {
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

func (d *DesktopEngine) Close() {
	d.closeOnce.Do(func() {
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

func (d *DesktopEngine) getStyle(fg, bg tcell.Color, bold, underline, reverse bool) tcell.Style {
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

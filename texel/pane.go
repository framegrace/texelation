// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/pane.go
// Summary: Implements pane capabilities for the core desktop engine.
// Usage: Used throughout the project to implement pane inside the desktop and panes.

// texel/pane_v2.go
package texel

import (
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"log"
	"texelation/texel/theme"
	"unicode/utf8"
)

// Z-order constants for common layering scenarios
const (
	ZOrderDefault   = 0    // Normal panes
	ZOrderFloating  = 100  // Floating windows
	ZOrderDialog    = 500  // Modal dialogs
	ZOrderAnimation = 1000 // Reserved for future use (visual effects, etc.)
	ZOrderTooltip   = 2000 // Tooltips and temporary overlays
)

// Pane represents a rectangular area on the screen that hosts an App.
type pane struct {
	absX0, absY0, absX1, absY1 int
	app                        App
	name                       string
	prevBuf                    [][]Cell
	screen                     *Workspace
	id                         [16]byte
	selectionHandler           SelectionHandler
	handlesSelection           bool
	wheelHandler               MouseWheelHandler
	handlesWheel               bool

	// Public state fields
	IsActive   bool
	IsResizing bool
	ZOrder     int // Higher values render on top, default is 0
}

// newPane creates a new, empty Pane. The App is attached later.
func newPane(s *Workspace) *pane {
	p := &pane{
		screen:     s,
		IsActive:   false,
		IsResizing: false,
	}
	if _, err := rand.Read(p.id[:]); err != nil {
		sum := sha1.Sum([]byte(fmt.Sprintf("%p", p)))
		copy(p.id[:], sum[:])
	}
	return p
}

// AttachApp connects an application to the pane, gives it its initial size,
// and starts its main run loop.
func (p *pane) AttachApp(app App, refreshChan chan<- bool) {
	log.Printf("AttachApp: Starting attachment of app '%s'", app.GetTitle())
	if p.app != nil {
		log.Printf("AttachApp: Stopping existing app '%s'", p.app.GetTitle())
		p.screen.appLifecycle.StopApp(p.app)
	}
	p.app = app
	p.name = app.GetTitle()
	p.app.SetRefreshNotifier(refreshChan)
	log.Printf("AttachApp: Refresh notifier set")

	// Pass pane ID to apps that need it (e.g., for per-pane history)
	if idSetter, ok := app.(PaneIDSetter); ok {
		idSetter.SetPaneID(p.id)
	}

	// Inject storage for apps that need it
	if p.screen != nil && p.screen.desktop != nil && p.screen.desktop.Storage() != nil {
		appType := "unknown"
		if provider, ok := app.(SnapshotProvider); ok {
			appType, _ = provider.SnapshotMetadata()
		}
		// Per-pane storage
		if setter, ok := app.(StorageSetter); ok {
			setter.SetStorage(p.screen.desktop.Storage().PaneStorage(appType, p.id))
		}
		// App-level storage (shared across instances)
		if setter, ok := app.(AppStorageSetter); ok {
			setter.SetAppStorage(p.screen.desktop.Storage().AppStorage(appType))
		}
	}

	if listener, ok := app.(Listener); ok {
		p.screen.Subscribe(listener)
	}

	// ... (rest of interface checks) ...
	if handler, ok := app.(SelectionHandler); ok {
		enabled := true
		if declarer, ok := app.(SelectionDeclarer); ok {
			enabled = declarer.SelectionEnabled()
		}
		if enabled {
			p.selectionHandler = handler
			p.handlesSelection = true
		} else {
			p.selectionHandler = nil
			p.handlesSelection = false
		}
	} else {
		p.selectionHandler = nil
		p.handlesSelection = false
	}
	if handler, ok := app.(MouseWheelHandler); ok {
		enabled := true
		if declarer, ok := app.(MouseWheelDeclarer); ok {
			enabled = declarer.MouseWheelEnabled()
		}
		if enabled {
			p.wheelHandler = handler
			p.handlesWheel = true
		} else {
			p.wheelHandler = nil
			p.handlesWheel = false
		}
	} else {
		p.wheelHandler = nil
		p.handlesWheel = false
	}
	
	log.Printf("AttachApp: Resizing app '%s' to %dx%d", p.getTitle(), p.drawableWidth(), p.drawableHeight())
	// The app is resized considering the space for borders.
	p.app.Resize(p.drawableWidth(), p.drawableHeight())
	
	log.Printf("AttachApp: Starting app lifecycle for '%s'", p.getTitle())
	currentApp := p.app
	p.screen.appLifecycle.StartApp(p.app, func(err error) {
		p.screen.handleAppExit(p, currentApp, err)
	})

	// Register control bus handlers if this is a launcher app in a pane
	if app.GetTitle() == "Launcher" {
		if provider, ok := app.(ControlBusProvider); ok {
			provider.RegisterControl("launcher.select-app", "Launch selected app in this pane", func(payload interface{}) error {
				appName, ok := payload.(string)
				if !ok {
					return nil
				}
				p.ReplaceWithApp(appName, nil)
				return nil
			})

			provider.RegisterControl("launcher.close", "Close launcher", func(payload interface{}) error {
				if p.screen != nil {
					p.screen.CloseActivePane()
				}
				return nil
			})
		}
	}

	log.Printf("AttachApp: Notifying pane state for '%s'", p.getTitle())
	if p.screen != nil && p.screen.desktop != nil {
		p.screen.desktop.notifyPaneState(p.ID(), p.IsActive, p.IsResizing, p.ZOrder, p.handlesSelection)
	}
	log.Printf("AttachApp: Completed attachment of app '%s'", p.getTitle())
}

// PrepareAppForRestore connects an application to the pane without starting it.
// This is used during snapshot restore where we need to set up pane dimensions
// before starting apps. Call StartPreparedApp after layout is calculated.
func (p *pane) PrepareAppForRestore(app App, refreshChan chan<- bool) {
	log.Printf("PrepareAppForRestore: Preparing app '%s'", app.GetTitle())
	if p.app != nil {
		log.Printf("PrepareAppForRestore: Stopping existing app '%s'", p.app.GetTitle())
		p.screen.appLifecycle.StopApp(p.app)
	}
	p.app = app
	p.name = app.GetTitle()
	p.app.SetRefreshNotifier(refreshChan)

	// Pass pane ID to apps that need it (e.g., for per-pane history)
	if idSetter, ok := app.(PaneIDSetter); ok {
		idSetter.SetPaneID(p.id)
	}

	// Inject storage for apps that need it
	if p.screen != nil && p.screen.desktop != nil && p.screen.desktop.Storage() != nil {
		appType := "unknown"
		if provider, ok := app.(SnapshotProvider); ok {
			appType, _ = provider.SnapshotMetadata()
		}
		// Per-pane storage
		if setter, ok := app.(StorageSetter); ok {
			setter.SetStorage(p.screen.desktop.Storage().PaneStorage(appType, p.id))
		}
		// App-level storage (shared across instances)
		if setter, ok := app.(AppStorageSetter); ok {
			setter.SetAppStorage(p.screen.desktop.Storage().AppStorage(appType))
		}
	}

	if listener, ok := app.(Listener); ok {
		p.screen.Subscribe(listener)
	}

	// Set up selection and mouse handlers
	if handler, ok := app.(SelectionHandler); ok {
		enabled := true
		if declarer, ok := app.(SelectionDeclarer); ok {
			enabled = declarer.SelectionEnabled()
		}
		if enabled {
			p.selectionHandler = handler
			p.handlesSelection = true
		} else {
			p.selectionHandler = nil
			p.handlesSelection = false
		}
	} else {
		p.selectionHandler = nil
		p.handlesSelection = false
	}
	if handler, ok := app.(MouseWheelHandler); ok {
		enabled := true
		if declarer, ok := app.(MouseWheelDeclarer); ok {
			enabled = declarer.MouseWheelEnabled()
		}
		if enabled {
			p.wheelHandler = handler
			p.handlesWheel = true
		} else {
			p.wheelHandler = nil
			p.handlesWheel = false
		}
	} else {
		p.wheelHandler = nil
		p.handlesWheel = false
	}

	// NOTE: We intentionally skip Resize and StartApp here.
	// These will be called by StartPreparedApp after layout is calculated.
	log.Printf("PrepareAppForRestore: App '%s' prepared (will start after layout)", p.getTitle())
}

// StartPreparedApp resizes and starts an app that was prepared via PrepareAppForRestore.
// Should be called after layout is calculated so pane has proper dimensions.
func (p *pane) StartPreparedApp() {
	if p.app == nil {
		return
	}
	log.Printf("StartPreparedApp: Starting app '%s' with size %dx%d", p.getTitle(), p.drawableWidth(), p.drawableHeight())

	// Now resize with proper dimensions
	p.app.Resize(p.drawableWidth(), p.drawableHeight())

	// Start the app lifecycle
	currentApp := p.app
	p.screen.appLifecycle.StartApp(p.app, func(err error) {
		p.screen.handleAppExit(p, currentApp, err)
	})

	// Register control bus handlers if this is a launcher app in a pane
	if p.app.GetTitle() == "Launcher" {
		if provider, ok := p.app.(ControlBusProvider); ok {
			provider.RegisterControl("launcher.select-app", "Launch selected app in this pane", func(payload interface{}) error {
				appName, ok := payload.(string)
				if !ok {
					return nil
				}
				p.ReplaceWithApp(appName, nil)
				return nil
			})

			provider.RegisterControl("launcher.close", "Close launcher", func(payload interface{}) error {
				if p.screen != nil {
					p.screen.CloseActivePane()
				}
				return nil
			})
		}
	}

	// Notify pane state
	if p.screen != nil && p.screen.desktop != nil {
		p.screen.desktop.notifyPaneState(p.ID(), p.IsActive, p.IsResizing, p.ZOrder, p.handlesSelection)
	}
	log.Printf("StartPreparedApp: Completed starting app '%s'", p.getTitle())
}

// ReplaceWithApp replaces the current app in this pane with a new app from the registry.
// This is called by control bus handlers when apps signal they want to launch a different app.
func (p *pane) ReplaceWithApp(name string, config map[string]interface{}) {
	if p.screen == nil || p.screen.desktop == nil {
		log.Printf("Pane: Cannot replace app - no desktop reference")
		return
	}

	// Create the new app from the registry
	registry := p.screen.desktop.Registry()
	if registry == nil {
		log.Printf("Pane: Cannot replace app - no registry")
		return
	}

	appInterface := registry.CreateApp(name, config)
	if appInterface == nil {
		log.Printf("Pane: Failed to create app '%s' from registry", name)
		return
	}

	// Type assert to App
	newApp, ok := appInterface.(App)
	if !ok {
		log.Printf("Pane: Registry returned non-App type for '%s'", name)
		return
	}

	log.Printf("Pane: Replacing app '%s' with '%s'", p.getTitle(), name)

	// Stop the current app explicitly before attaching the new one
	// Although AttachApp does this, doing it here ensures clean teardown before new setup
	// if p.app != nil {
	// 	 p.screen.appLifecycle.StopApp(p.app)
	// 	 p.app = nil 
	// } 
    // AttachApp handles the stop.

	// Attach the new app (this will stop the old app and start the new one)
	p.AttachApp(newApp, p.screen.refreshChan)

	// Broadcast state update so the desktop knows about the change
	p.screen.desktop.broadcastStateUpdate()
	
	// Force a refresh of the workspace to ensure the new app is rendered
	if p.screen != nil {
		p.screen.Refresh()
	}
}

// SetActive changes the active state of the pane
func (p *pane) SetActive(active bool) {
	if p.IsActive == active {
		return
	}
	p.IsActive = active
	p.notifyStateChange()
	if p.screen != nil {
		p.screen.Refresh()
	}
}

// SetResizing changes the resizing state of the pane
func (p *pane) SetResizing(resizing bool) {
	if p.IsResizing == resizing {
		return
	}
	p.IsResizing = resizing
	p.notifyStateChange()
	if p.screen != nil {
		p.screen.Refresh()
	}
}

func (p *pane) notifyStateChange() {
	if p.screen == nil || p.screen.desktop == nil {
		return
	}
	p.screen.desktop.notifyPaneState(p.ID(), p.IsActive, p.IsResizing, p.ZOrder, p.handlesSelection)
}

// SetZOrder sets the z-order (layering) of the pane
// Higher values render on top. Default is 0.
func (p *pane) SetZOrder(zOrder int) {
	if p.ZOrder == zOrder {
		return
	}
	p.ZOrder = zOrder
	log.Printf("SetZOrder: Pane '%s' z-order set to %d", p.getTitle(), zOrder)
	if p.screen != nil && p.screen.desktop != nil {
		p.screen.desktop.notifyPaneState(p.ID(), p.IsActive, p.IsResizing, p.ZOrder, p.handlesSelection)
	}
	if p.screen != nil {
		p.screen.Refresh() // Trigger redraw
	}
}

// GetZOrder returns the current z-order of the pane
func (p *pane) GetZOrder() int {
	return p.ZOrder
}

// BringToFront sets the pane to render on top of other panes
func (p *pane) BringToFront() {
	p.SetZOrder(ZOrderFloating)
}

// SendToBack resets the pane to normal z-order
func (p *pane) SendToBack() {
	p.SetZOrder(ZOrderDefault)
}

// SetAsDialog configures the pane as a modal dialog
func (p *pane) SetAsDialog() {
	p.SetZOrder(ZOrderDialog)
}

// Render draws the pane's borders, title, and the hosted application's content including effects.
func (p *pane) Render() [][]Cell {
	return p.renderBuffer(true)
}

func (p *pane) handlePaste(data []byte) {
	if p == nil || p.app == nil || len(data) == 0 {
		return
	}
	if handler, ok := p.app.(PasteHandler); ok {
		handler.HandlePaste(data)
		return
	}
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			// treat invalid byte as literal
			r = rune(data[0])
		}
		data = data[size:]
		var ev *tcell.EventKey
		switch r {
		case '\r', '\n':
			ev = tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
		case '\t':
			ev = tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
		case '':
			ev = tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone)
		case 0x1b:
			ev = tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone)
		default:
			ev = tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone)
		}
		p.app.HandleKey(ev)
	}
}

func (p *pane) renderBuffer(applyEffects bool) [][]Cell {
	w := p.Width()
	h := p.Height()

	log.Printf("Render: Pane '%s' rendering %dx%d (abs: %d,%d-%d,%d)",
		p.getTitle(), w, h, p.absX0, p.absY0, p.absX1, p.absY1)

	tm := theme.Get()
	desktopBg := tm.GetSemanticColor("bg.base").TrueColor()
	desktopFg := tm.GetSemanticColor("text.primary").TrueColor()
	defstyle := tcell.StyleDefault.Background(desktopBg).Foreground(desktopFg)

	// Create the pane's buffer.
	buffer := make([][]Cell, h)
	for i := range buffer {
		buffer[i] = make([]Cell, w)
		for j := range buffer[i] {
			buffer[i][j] = Cell{Ch: ' ', Style: defstyle}
		}
	}

	// Don't draw decorations if the pane is too small.
	if w < 2 || h < 2 {
		log.Printf("Render: Pane '%s' too small to draw decorations (%dx%d)", p.getTitle(), w, h)
		return buffer
	}

	// Determine border style based on active state.
	inactiveBorderFG := tm.GetSemanticColor("border.inactive").TrueColor()
	inactiveBorderBG := tm.GetSemanticColor("bg.base").TrueColor()
	activeBorderFG := tm.GetSemanticColor("border.active").TrueColor()
	activeBorderBG := tm.GetSemanticColor("bg.base").TrueColor()
	resizingBorderFG := tm.GetSemanticColor("border.resizing").TrueColor()

	borderStyle := defstyle.Foreground(inactiveBorderFG).Background(inactiveBorderBG)
	if p.IsActive {
		borderStyle = defstyle.Foreground(activeBorderFG).Background(activeBorderBG)
	}
	if p.IsResizing {
		borderStyle = defstyle.Foreground(resizingBorderFG).Background(activeBorderBG)
	}

	// Draw borders
	for x := 0; x < w; x++ {
		buffer[0][x] = Cell{Ch: tcell.RuneHLine, Style: borderStyle}
		buffer[h-1][x] = Cell{Ch: tcell.RuneHLine, Style: borderStyle}
	}
	for y := 0; y < h; y++ {
		buffer[y][0] = Cell{Ch: tcell.RuneVLine, Style: borderStyle}
		buffer[y][w-1] = Cell{Ch: tcell.RuneVLine, Style: borderStyle}
	}
	buffer[0][0] = Cell{Ch: tcell.RuneULCorner, Style: borderStyle}
	buffer[0][w-1] = Cell{Ch: tcell.RuneURCorner, Style: borderStyle}
	buffer[h-1][0] = Cell{Ch: tcell.RuneLLCorner, Style: borderStyle}
	buffer[h-1][w-1] = Cell{Ch: '╯', Style: borderStyle}

	// Draw Title - with proper bounds checking
	title := p.getTitle()
	if title != "" && w > 4 { // Only draw title if we have enough space
		titleRuneCount := utf8.RuneCountInString(title)
		maxTitleLength := w - 4 // Space for " " + title + " " + borders

		// Truncate title if it's too long for the pane width.
		if titleRuneCount > maxTitleLength && maxTitleLength > 0 {
			titleRunes := []rune(title)
			if maxTitleLength <= len(titleRunes) {
				title = string(titleRunes[:maxTitleLength])
			}
		}

		titleStr := " " + title + " "
		for i, ch := range titleStr {
			if 1+i < w-1 { // Ensure we don't go beyond borders
				buffer[0][1+i] = Cell{Ch: ch, Style: borderStyle}
			}
		}
	}

	// Render the app's content inside the borders.
	if p.app != nil {
		appBuffer := p.app.Render()
		if len(appBuffer) > 0 && len(appBuffer[0]) > 0 {
			log.Printf("ANIM: Render: Pane '%s' app buffer size: %dx%d (pane size: %dx%d, drawable: %dx%d)",
				p.getTitle(), len(appBuffer[0]), len(appBuffer), w, h, p.drawableWidth(), p.drawableHeight())

			for y, row := range appBuffer {
				for x, cell := range row {
					if 1+x < w-1 && 1+y < h-1 {
						buffer[1+y][1+x] = cell
					}
				}
			}
		} else {
			log.Printf("ANIM: Render: Pane '%s' app returned EMPTY buffer! (pane size: %dx%d, drawable: %dx%d)",
				p.getTitle(), w, h, p.drawableWidth(), p.drawableHeight())
		}
	} else {
		log.Printf("ANIM: Render: Pane '%s' has no app!", p.getTitle())
	}

	log.Printf("Render: Pane '%s' final buffer size: %dx%d", p.getTitle(), len(buffer), len(buffer[0]))
	return buffer
}

func (p *pane) ID() [16]byte {
	return p.id
}

func (p *pane) setID(id [16]byte) {
	p.id = id
}

// Rest of the methods remain the same...
func (p *pane) drawableWidth() int {
	w := p.Width() - 2
	if w < 0 {
		return 0
	}
	return w
}

func (p *pane) drawableHeight() int {
	h := p.Height() - 2
	if h < 0 {
		return 0
	}
	return h
}

func (p *pane) String() string {
	return p.name
}

func (p *pane) setTitle(t string) {
	p.name = t
}

func (p *pane) getTitle() string {
	if p.app != nil {
		return p.app.GetTitle()
	}
	return p.name
}

// Close stops the current app and cleans up the pane.
func (p *pane) Close() {
	// Clean up app
	if listener, ok := p.app.(Listener); ok {
		p.screen.Unsubscribe(listener)
	}
	if p.app != nil {
		p.screen.appLifecycle.StopApp(p.app)
	}
}

func (p *pane) Width() int {
	w := p.absX1 - p.absX0
	if w < 0 {
		return 0
	}
	return w
}

func (p *pane) Height() int {
	h := p.absY1 - p.absY0
	if h < 0 {
		return 0
	}
	return h
}

func (p *pane) setDimensions(x0, y0, x1, y1 int) {
	log.Printf("ANIM: setDimensions: Pane '%s' set to (%d,%d)-(%d,%d), size %dx%d",
		p.getTitle(), x0, y0, x1, y1, x1-x0, y1-y0)

	p.absX0, p.absY0, p.absX1, p.absY1 = x0, y0, x1, y1

	if p.app != nil {
		drawableW := p.drawableWidth()
		drawableH := p.drawableHeight()
		log.Printf("ANIM: setDimensions: Pane '%s' calling app.Resize(%d, %d)",
			p.getTitle(), drawableW, drawableH)
		p.app.Resize(drawableW, drawableH)
	} else {
		log.Printf("ANIM: setDimensions: Pane '%s' has no app yet!", p.getTitle())
	}
}

// contains reports whether the absolute pane bounds include the provided coordinates.
func (p *pane) contains(x, y int) bool {
	if p == nil {
		return false
	}
	if x < p.absX0 || x >= p.absX1 {
		return false
	}
	if y < p.absY0 || y >= p.absY1 {
		return false
	}
	return true
}

func (p *pane) handlesSelectionEvents() bool {
	return p != nil && p.handlesSelection && p.selectionHandler != nil
}

func (p *pane) contentLocalCoords(x, y int) (int, int) {
	innerX := x - (p.absX0 + 1)
	innerY := y - (p.absY0 + 1)
	innerWidth := p.drawableWidth()
	innerHeight := p.drawableHeight()
	if innerWidth <= 0 {
		innerX = 0
	} else {
		if innerX < 0 {
			innerX = 0
		} else if innerX >= innerWidth {
			innerX = innerWidth - 1
		}
	}
	if innerHeight <= 0 {
		innerY = 0
	} else {
		if innerY < 0 {
			innerY = 0
		} else if innerY >= innerHeight {
			innerY = innerHeight - 1
		}
	}
	return innerX, innerY
}

func (p *pane) handlesWheelEvents() bool {
	return p != nil && p.handlesWheel && p.wheelHandler != nil
}

func (p *pane) handleMouseWheel(x, y, dx, dy int, modifiers tcell.ModMask) {
	if !p.handlesWheelEvents() {
		return
	}
	localX, localY := p.contentLocalCoords(x, y)
	p.wheelHandler.HandleMouseWheel(localX, localY, dx, dy, modifiers)
}

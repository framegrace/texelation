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
	"log"
	"sync/atomic"
	"unicode/utf8"

	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/theme"
	"github.com/framegrace/texelui/widgets"
	"github.com/gdamore/tcell/v2"
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
	app                        App            // The real app - for interfaces only
	pipeline                   RenderPipeline // For events and rendering (from PipelineProvider)
	name                       string
	prevBuf                    [][]Cell
	screen                     *Workspace
	id                         [16]byte
	mouseHandler               MouseHandler
	handlesMouse               bool

	// Persistent border and buffer widgets (created once, updated each frame).
	border       *widgets.Border
	bufferWidget *widgets.BufferWidget
	wasActive    bool // tracks focus state to avoid redundant Focus/Blur calls

	// Per-pane dirty tracking for render skipping (Level 2 optimization).
	needsRender int32          // atomic: 1 = pane needs re-render
	refreshStop chan struct{}   // stop signal for refresh forwarder goroutine
	prevTitle   string         // title when prevBuf was last rendered

	// Public state fields
	IsActive       bool
	IsResizing     bool
	RoundedCorners bool
	ZOrder         int // Higher values render on top, default is 0
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
	p.initBorder()
	return p
}

// initBorder creates the persistent Border and BufferWidget instances.
// Called once from newPane; styles are refreshed lazily via refreshBorderStyles.
func (p *pane) initBorder() {
	p.bufferWidget = widgets.NewBufferWidget(nil)
	p.border = widgets.NewBorder()
	p.border.SetSquareCorners()
	p.border.SetChild(p.bufferWidget)
	p.refreshBorderStyles()
}

// refreshBorderStyles re-reads semantic colors from the theme and updates
// the border's Style, FocusedStyle, and ResizingStyle. Called at init and
// whenever prevBuf is nil (layout/theme change signal).
func (p *pane) refreshBorderStyles() {
	tm := theme.Get()
	bg := tm.GetSemanticColor("bg.base").TrueColor()
	inactiveFG := tm.GetSemanticColor("border.inactive").TrueColor()
	activeFG := tm.GetSemanticColor("border.active").TrueColor()
	resizingFG := tm.GetSemanticColor("border.resizing").TrueColor()

	p.border.Style = tcell.StyleDefault.Foreground(inactiveFG).Background(bg)
	p.border.FocusedStyle = tcell.StyleDefault.Foreground(activeFG).Background(bg)
	p.border.ResizingStyle = tcell.StyleDefault.Foreground(resizingFG).Background(bg)
}

// markDirty flags this pane for re-render on the next snapshot cycle.
func (p *pane) markDirty() {
	atomic.StoreInt32(&p.needsRender, 1)
}

// setupRefreshForwarder creates a per-pane channel that marks the pane dirty
// and forwards refresh signals to the workspace-level channel. This enables
// per-pane render skipping: only panes whose app signalled a refresh (or whose
// state changed) will be re-rendered.
func (p *pane) setupRefreshForwarder(target chan<- bool) chan<- bool {
	if p.refreshStop != nil {
		close(p.refreshStop)
	}

	ch := make(chan bool, 1)
	stop := make(chan struct{})
	p.refreshStop = stop

	go func() {
		for {
			select {
			case <-stop:
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
				atomic.StoreInt32(&p.needsRender, 1)
				select {
				case target <- true:
				default:
				}
			}
		}
	}()

	return ch
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

	// Get pipeline directly from app if it provides one
	p.pipeline = nil
	if provider, ok := app.(PipelineProvider); ok {
		p.pipeline = provider.Pipeline()
		log.Printf("AttachApp: Got pipeline from app '%s'", app.GetTitle())
	}

	// Create per-pane refresh forwarder that marks this pane dirty
	// before forwarding to the workspace-level channel.
	paneRefresh := p.setupRefreshForwarder(refreshChan)
	p.markDirty()
	p.prevBuf = nil

	// Set refresh notifier on pipeline (or app as fallback)
	if p.pipeline != nil {
		p.pipeline.SetRefreshNotifier(paneRefresh)
	} else {
		p.app.SetRefreshNotifier(paneRefresh)
	}
	log.Printf("AttachApp: Refresh notifier set")

	// Pass pane ID to apps that need it (e.g., for per-pane history)
	if idSetter, ok := app.(PaneIDSetter); ok {
		idSetter.SetPaneID(p.id)
	}

	// Inject storage for apps that need it (interfaces go to app, not pipeline)
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

	// Inject clipboard service for apps that need it
	// Desktop implements ClipboardService, so we pass it directly
	if p.screen != nil && p.screen.desktop != nil {
		if aware, ok := app.(ClipboardAware); ok {
			aware.SetClipboardService(p.screen.desktop)
		}
	}

	// Check pipeline for mouse handler (fallback to app for backwards compat)
	p.mouseHandler = nil
	p.handlesMouse = false
	eventSource := interface{}(p.pipeline)
	if eventSource == nil {
		eventSource = app
	}
	if handler, ok := eventSource.(MouseHandler); ok {
		p.mouseHandler = handler
		p.handlesMouse = true
	}

	log.Printf("AttachApp: Resizing app '%s' to %dx%d", p.getTitle(), p.drawableWidth(), p.drawableHeight())
	// Resize pipeline (or app as fallback)
	if p.pipeline != nil {
		p.pipeline.Resize(p.drawableWidth(), p.drawableHeight())
	} else {
		p.app.Resize(p.drawableWidth(), p.drawableHeight())
	}

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
		p.screen.desktop.notifyPaneState(p.ID(), p.IsActive, p.IsResizing, p.ZOrder, p.handlesMouse)
		// Notify that an app was attached (triggers snapshot persistence)
		p.screen.desktop.dispatcher.Broadcast(Event{Type: EventAppAttached})
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

	// Get pipeline directly from app if it provides one
	p.pipeline = nil
	if provider, ok := app.(PipelineProvider); ok {
		p.pipeline = provider.Pipeline()
		log.Printf("PrepareAppForRestore: Got pipeline from app '%s'", app.GetTitle())
	}

	// Create per-pane refresh forwarder that marks this pane dirty
	// before forwarding to the workspace-level channel.
	paneRefresh := p.setupRefreshForwarder(refreshChan)
	p.markDirty()
	p.prevBuf = nil

	// Set refresh notifier on pipeline (or app as fallback)
	if p.pipeline != nil {
		p.pipeline.SetRefreshNotifier(paneRefresh)
	} else {
		p.app.SetRefreshNotifier(paneRefresh)
	}

	// Pass pane ID to apps that need it (e.g., for per-pane history)
	if idSetter, ok := app.(PaneIDSetter); ok {
		idSetter.SetPaneID(p.id)
	}

	// Inject storage for apps that need it (interfaces go to app, not pipeline)
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

	// Inject clipboard service for apps that need it
	// Desktop implements ClipboardService, so we pass it directly
	if p.screen != nil && p.screen.desktop != nil {
		if aware, ok := app.(ClipboardAware); ok {
			aware.SetClipboardService(p.screen.desktop)
		}
	}

	// Check pipeline for mouse handler (fallback to app for backwards compat)
	p.mouseHandler = nil
	p.handlesMouse = false
	eventSource := interface{}(p.pipeline)
	if eventSource == nil {
		eventSource = app
	}
	if handler, ok := eventSource.(MouseHandler); ok {
		p.mouseHandler = handler
		p.handlesMouse = true
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

	// Resize pipeline (or app as fallback)
	if p.pipeline != nil {
		p.pipeline.Resize(p.drawableWidth(), p.drawableHeight())
	} else {
		p.app.Resize(p.drawableWidth(), p.drawableHeight())
	}

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
		p.screen.desktop.notifyPaneState(p.ID(), p.IsActive, p.IsResizing, p.ZOrder, p.handlesMouse)
	}
	log.Printf("StartPreparedApp: Completed starting app '%s'", p.getTitle())
}

// ReplaceWithApp replaces the current app in this pane with a new app from the registry.
// This is called by control bus handlers when apps signal they want to launch a different app.
func (p *pane) ReplaceWithApp(name string, config map[string]interface{}) {
	if p == nil || p.screen == nil || p.screen.desktop == nil {
		log.Printf("Pane: Cannot replace app - no pane or desktop reference")
		return
	}

	// Check if current app wants to show a confirmation dialog before being replaced
	if p.app != nil {
		if requester, ok := p.app.(CloseCallbackRequester); ok {
			// Pass a callback that performs the actual replacement
			if !requester.RequestCloseWithCallback(func() {
				p.doReplaceWithApp(name, config)
			}) {
				// App is showing confirmation dialog, don't proceed yet
				log.Printf("Pane: App '%s' is showing close confirmation", p.getTitle())
				return
			}
		}
	}

	// No confirmation needed or app doesn't implement CloseCallbackRequester
	p.doReplaceWithApp(name, config)
}

// doReplaceWithApp performs the actual app replacement after any confirmation.
func (p *pane) doReplaceWithApp(name string, config map[string]interface{}) {
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

	// Attach the new app (this will stop the old app and start the new one)
	p.AttachApp(newApp, p.screen.refreshChan)

	// Broadcast state update so the desktop knows about the change
	p.screen.desktop.broadcastStateUpdate()
	// App identity changed; broadcast tree change so snapshots persist the new app type.
	p.screen.desktop.broadcastTreeChanged()

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
	p.markDirty()
	if p.screen == nil || p.screen.desktop == nil {
		return
	}
	p.screen.desktop.notifyPaneState(p.ID(), p.IsActive, p.IsResizing, p.ZOrder, p.handlesMouse)
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
		p.screen.desktop.notifyPaneState(p.ID(), p.IsActive, p.IsResizing, p.ZOrder, p.handlesMouse)
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
	if p == nil || len(data) == 0 {
		return
	}
	// Try pipeline first for paste handling
	if p.pipeline != nil {
		if handler, ok := p.pipeline.(PasteHandler); ok {
			handler.HandlePaste(data)
			p.markDirty()
			return
		}
	}
	// Fallback to app
	if p.app != nil {
		if handler, ok := p.app.(PasteHandler); ok {
			handler.HandlePaste(data)
			p.markDirty()
			return
		}
	}
	// No paste handler - convert to key events
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
		if p.pipeline != nil {
			p.pipeline.HandleKey(ev)
		} else if p.app != nil {
			p.app.HandleKey(ev)
		}
	}
	p.markDirty()
}

func (p *pane) renderBuffer(applyEffects bool) [][]Cell {
	w := p.Width()
	h := p.Height()

	// Return cached buffer when pane hasn't changed (Level 2 optimization).
	currentTitle := p.getTitle()
	if atomic.LoadInt32(&p.needsRender) == 0 && p.prevBuf != nil &&
		len(p.prevBuf) == h && (h == 0 || len(p.prevBuf[0]) == w) &&
		p.prevTitle == currentTitle {
		return p.prevBuf
	}

	log.Printf("Render: Pane '%s' rendering %dx%d (abs: %d,%d-%d,%d)",
		p.getTitle(), w, h, p.absX0, p.absY0, p.absX1, p.absY1)

	// Refresh theme styles each time we actually render.
	p.refreshBorderStyles()

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

	// Render content from pipeline (or app as fallback).
	var appBuffer [][]Cell
	if p.pipeline != nil {
		appBuffer = p.pipeline.Render()
	} else if p.app != nil {
		appBuffer = p.app.Render()
	}

	if len(appBuffer) > 0 && len(appBuffer[0]) > 0 {
		log.Printf("ANIM: Render: Pane '%s' buffer size: %dx%d (pane size: %dx%d, drawable: %dx%d)",
			p.getTitle(), len(appBuffer[0]), len(appBuffer), w, h, p.drawableWidth(), p.drawableHeight())
	} else {
		log.Printf("ANIM: Render: Pane '%s' returned EMPTY buffer! (pane size: %dx%d, drawable: %dx%d)",
			p.getTitle(), w, h, p.drawableWidth(), p.drawableHeight())
	}

	// Update persistent border widget state.
	p.border.Resize(w, h)
	p.border.Title = p.getTitle()
	p.border.IsResizing = p.IsResizing
	if p.RoundedCorners {
		p.border.SetRoundedCorners()
	} else {
		p.border.SetSquareCorners()
	}

	// Update focus state only when it changes.
	if p.IsActive != p.wasActive {
		if p.IsActive {
			p.border.SetFocusable(true)
			p.border.BaseWidget.Focus()
		} else {
			p.border.BaseWidget.Blur()
		}
		p.wasActive = p.IsActive
	}

	// Swap buffer reference into the child widget.
	p.bufferWidget.SetBuffer(appBuffer)

	// Draw through the persistent widget tree.
	painter := texelcore.NewPainter(buffer, texelcore.Rect{X: 0, Y: 0, W: w, H: h})
	p.border.Draw(painter)

	// Cache the rendered buffer for reuse when pane hasn't changed.
	p.prevBuf = buffer
	p.prevTitle = currentTitle
	atomic.StoreInt32(&p.needsRender, 0)

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
	// Stop refresh forwarder goroutine.
	if p.refreshStop != nil {
		close(p.refreshStop)
		p.refreshStop = nil
	}
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
	if p.absX0 == x0 && p.absY0 == y0 && p.absX1 == x1 && p.absY1 == y1 {
		return
	}

	log.Printf("ANIM: setDimensions: Pane '%s' set to (%d,%d)-(%d,%d), size %dx%d",
		p.getTitle(), x0, y0, x1, y1, x1-x0, y1-y0)

	p.absX0, p.absY0, p.absX1, p.absY1 = x0, y0, x1, y1
	p.prevBuf = nil
	p.markDirty()

	drawableW := p.drawableWidth()
	drawableH := p.drawableHeight()

	// Resize pipeline (or app as fallback)
	if p.pipeline != nil {
		log.Printf("ANIM: setDimensions: Pane '%s' calling pipeline.Resize(%d, %d)",
			p.getTitle(), drawableW, drawableH)
		p.pipeline.Resize(drawableW, drawableH)
	} else if p.app != nil {
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

func (p *pane) handlesMouseEvents() bool {
	return p != nil && p.handlesMouse && p.mouseHandler != nil
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

// handleMouse forwards a mouse event to the pane's app with local coordinates.
func (p *pane) handleMouse(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) {
	if !p.handlesMouseEvents() {
		return
	}
	localX, localY := p.contentLocalCoords(x, y)
	ev := tcell.NewEventMouse(localX, localY, buttons, modifiers)
	p.mouseHandler.HandleMouse(ev)
	// Mark dirty synchronously so the immediate Publish() in
	// DesktopSink.HandleMouseEvent sees updated state.
	p.markDirty()
}

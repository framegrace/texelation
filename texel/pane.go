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

	"github.com/framegrace/texelation/internal/debuglog"
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
	// Uses a generation counter instead of a boolean flag to avoid TOCTOU
	// races: markDirty increments the counter, renderBuffer records the
	// value before rendering and only caches when the counter hasn't moved.
	renderGen     int32          // atomic: monotonically increasing dirty generation
	lastRendered  int32          // generation when prevBuf was last produced
	refreshStop   chan struct{}  // stop signal for refresh forwarder goroutine
	prevTitle     string         // title when prevBuf was last rendered

	// Public state fields
	IsActive       bool
	IsResizing     bool
	RoundedCorners bool
	ZOrder         int // Higher values render on top, default is 0
}

// newPane creates a new, empty Pane. The App is attached later.
func newPane(s *Workspace) *pane {
	p := &pane{
		screen:         s,
		IsActive:       false,
		IsResizing:     false,
		RoundedCorners: true,
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

// AttachApp connects an application to the pane, gives it its initial size,
// and starts its main run loop.
func (p *pane) AttachApp(app App, refreshChan chan<- bool) {
	debuglog.Printf("AttachApp: Starting attachment of app '%s'", app.GetTitle())
	if p.app != nil {
		debuglog.Printf("AttachApp: Stopping existing app '%s'", p.app.GetTitle())
		p.screen.appLifecycle.StopApp(p.app)
	}
	p.app = app
	p.name = app.GetTitle()

	// Get pipeline directly from app if it provides one
	p.pipeline = nil
	if provider, ok := app.(PipelineProvider); ok {
		p.pipeline = provider.Pipeline()
		debuglog.Printf("AttachApp: Got pipeline from app '%s'", app.GetTitle())
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
	debuglog.Printf("AttachApp: Refresh notifier set")

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

	debuglog.Printf("AttachApp: Resizing app '%s' to %dx%d", p.getTitle(), p.drawableWidth(), p.drawableHeight())
	// Resize pipeline (or app as fallback)
	if p.pipeline != nil {
		p.pipeline.Resize(p.drawableWidth(), p.drawableHeight())
	} else {
		p.app.Resize(p.drawableWidth(), p.drawableHeight())
	}

	debuglog.Printf("AttachApp: Starting app lifecycle for '%s'", p.getTitle())
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

	debuglog.Printf("AttachApp: Notifying pane state for '%s'", p.getTitle())
	if p.screen != nil && p.screen.desktop != nil {
		p.screen.desktop.notifyPaneState(p.ID(), p.IsActive, p.IsResizing, p.ZOrder, p.handlesMouse)
		// Notify that an app was attached (triggers snapshot persistence)
		p.screen.desktop.dispatcher.Broadcast(Event{Type: EventAppAttached})
	}
	debuglog.Printf("AttachApp: Completed attachment of app '%s'", p.getTitle())
}

// PrepareAppForRestore connects an application to the pane without starting it.
// This is used during snapshot restore where we need to set up pane dimensions
// before starting apps. Call StartPreparedApp after layout is calculated.
func (p *pane) PrepareAppForRestore(app App, refreshChan chan<- bool) {
	debuglog.Printf("PrepareAppForRestore: Preparing app '%s'", app.GetTitle())
	if p.app != nil {
		debuglog.Printf("PrepareAppForRestore: Stopping existing app '%s'", p.app.GetTitle())
		p.screen.appLifecycle.StopApp(p.app)
	}
	p.app = app
	p.name = app.GetTitle()

	// Get pipeline directly from app if it provides one
	p.pipeline = nil
	if provider, ok := app.(PipelineProvider); ok {
		p.pipeline = provider.Pipeline()
		debuglog.Printf("PrepareAppForRestore: Got pipeline from app '%s'", app.GetTitle())
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
	debuglog.Printf("PrepareAppForRestore: App '%s' prepared (will start after layout)", p.getTitle())
}

// StartPreparedApp resizes and starts an app that was prepared via PrepareAppForRestore.
// Should be called after layout is calculated so pane has proper dimensions.
func (p *pane) StartPreparedApp() {
	if p.app == nil {
		return
	}
	debuglog.Printf("StartPreparedApp: Starting app '%s' with size %dx%d", p.getTitle(), p.drawableWidth(), p.drawableHeight())

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
	debuglog.Printf("StartPreparedApp: Completed starting app '%s'", p.getTitle())
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
				debuglog.Printf("Pane: App '%s' is showing close confirmation", p.getTitle())
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

	debuglog.Printf("Pane: Replacing app '%s' with '%s'", p.getTitle(), name)

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

func (p *pane) ID() [16]byte {
	return p.id
}

func (p *pane) setID(id [16]byte) {
	p.id = id
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

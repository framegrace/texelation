// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/workspace.go
// Summary: Implements workspace (tab) capabilities for the core desktop engine.
// Usage: Manages a single workspace with tiling pane tree, navigation, and event routing.
// Notes: Renamed from Screen to better reflect its role as a workspace/tab manager.

package texel

import (
	"fmt"
	"log"

	"github.com/framegrace/texelation/internal/debuglog"
	"github.com/framegrace/texelation/internal/keybind"
	"github.com/gdamore/tcell/v2"
	"github.com/framegrace/texelui/theme"
)

type Direction int

const (
	DirUp Direction = iota
	DirDown
	DirLeft
	DirRight
)

// Minimum usable pane dimensions (including borders)
const (
	MinPaneWidth  = 20 // ~18 chars drawable area
	MinPaneHeight = 8  // ~6 lines drawable area
)

// DebuggableApp is an interface that apps can implement to provide
// detailed state information for debugging purposes.
type DebuggableApp interface {
	DumpState(frameNum int)
}

type AppFactory func() App


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

var workspaceAccentKeys = [8]string{
	"workspace.accent.1", "workspace.accent.2", "workspace.accent.3", "workspace.accent.4",
	"workspace.accent.5", "workspace.accent.6", "workspace.accent.7", "workspace.accent.8",
}

// WorkspaceAccentColor returns the accent color for a workspace index, cycling through
// the 8 workspace accent palette entries defined in the theme.
func WorkspaceAccentColor(index int) tcell.Color {
	key := workspaceAccentKeys[index%len(workspaceAccentKeys)]
	return theme.Get().GetSemanticColor(key)
}

// Workspace represents a single workspace/tab with its own tiling pane tree.
// Each workspace manages independent pane layout, navigation, and event routing.
type Workspace struct {
	id                  int
	Name                string
	Color               tcell.Color
	x, y, width, height int
	desktop             *DesktopEngine
	tree                *Tree
	refreshChan         chan bool
	dispatcher          *EventDispatcher
	ShellAppFactory     AppFactory
	appLifecycle        AppLifecycleManager

	resizeSelection   *selectedBorder
	mouseResizeBorder *selectedBorder
	debugFramesToDump int
}

// newWorkspace creates a new workspace with its own tiling pane tree.
func newWorkspace(id int, shellFactory AppFactory, lifecycle AppLifecycleManager, desktop *DesktopEngine) (*Workspace, error) {
	name := fmt.Sprintf("%d", id)
	if id == 1 {
		name = "default"
	}
	w := &Workspace{
		id:              id,
		Name:            name,
		Color:           WorkspaceAccentColor(id - 1),
		desktop:         desktop,
		tree:            NewTree(),
		refreshChan:     make(chan bool, 16),
		dispatcher:      NewEventDispatcher(),
		ShellAppFactory: shellFactory,
		appLifecycle:    lifecycle,
	}

	// Subscribe workspace to Desktop events so it can relay them to apps
	if desktop != nil {
		desktop.Subscribe(w)
	}

	return w, nil
}

func (w *Workspace) isDesktopClosing() bool {
	if w == nil || w.desktop == nil || w.desktop.quit == nil {
		return false
	}
	select {
	case <-w.desktop.quit:
		return true
	default:
		return false
	}
}

func forEachLeafPane(node *Node, fn func(*pane)) {
	if node == nil || fn == nil {
		return
	}
	if node.Pane != nil {
		fn(node.Pane)
		return
	}
	for _, child := range node.Children {
		forEachLeafPane(child, fn)
	}
}

func (w *Workspace) SetControlMode(active bool) {
	debuglog.Printf("SetControlMode called: active=%v", active)
}

func (w *Workspace) getDefaultBackground() tcell.Color {
	return w.desktop.DefaultBgColor
}

func (w *Workspace) Refresh() {
	select {
	case w.refreshChan <- true:
	default:
	}
}

func (w *Workspace) Broadcast(event Event) {
	w.dispatcher.Broadcast(event)
}

func (w *Workspace) Subscribe(listener Listener) {
	w.dispatcher.Subscribe(listener)
}

func (w *Workspace) Unsubscribe(listener Listener) {
	w.dispatcher.Unsubscribe(listener)
}

// OnEvent implements Listener to receive Desktop events and relay them to workspace apps.
func (w *Workspace) OnEvent(event Event) {
	// Relay Desktop-level events to all apps in this workspace
	switch event.Type {
	case EventThemeChanged:
		debuglog.Printf("Workspace %d: Received EventThemeChanged, relaying to apps", w.id)
		w.dispatcher.Broadcast(event)
		// Mark all panes dirty so borders re-render with new theme colors.
		forEachLeafPane(w.tree.Root, func(p *pane) {
			p.markDirty()
		})
	default:
		// Other Desktop events can be handled here if needed
	}
}

func (w *Workspace) notifyFocus() {
	if w.desktop == nil || w.tree == nil {
		return
	}
	w.desktop.notifyFocusNode(w.tree.ActiveLeaf)
}

func (w *Workspace) AddApp(app App) {
	debuglog.Printf("AddApp: Adding app '%s'", app.GetTitle())

	p := newPane(w)
	w.tree.SetRoot(p)
	p.AttachApp(app, w.refreshChan)

	// Set initial active state AFTER attaching the app
	debuglog.Printf("AddApp: Setting pane '%s' as active", p.getTitle())
	p.SetActive(true)
	w.recalculateLayout()
	w.notifyFocus()
	w.desktop.broadcastTreeChanged() // Notify that the tree structure changed
	w.desktop.broadcastActivePaneChanged()
	debuglog.Printf("AddApp: Completed adding app '%s'", app.GetTitle())
}

func (w *Workspace) handleEvent(ev *tcell.EventKey) {
	// Handle pane navigation via keybinding registry
	if w.desktop != nil && w.desktop.keybindings != nil {
		action := w.desktop.keybindings.Match(ev)
		switch action {
		case keybind.PaneNavUp:
			if !w.moveActivePane(DirUp) {
				// At top edge — enter tab navigation mode
				w.desktop.enterTabNavMode()
			}
			w.Refresh()
			return
		case keybind.PaneNavDown:
			w.moveActivePane(DirDown)
			w.Refresh()
			return
		case keybind.PaneNavLeft:
			w.moveActivePane(DirLeft)
			w.Refresh()
			return
		case keybind.PaneNavRight:
			w.moveActivePane(DirRight)
			w.Refresh()
			return
		}
	}

	// Pass all other keys to the active pane's pipeline (or app as fallback)
	if w.tree.ActiveLeaf != nil && w.tree.ActiveLeaf.Pane != nil {
		pane := w.tree.ActiveLeaf.Pane
		if pane.pipeline != nil {
			pane.pipeline.HandleKey(ev)
		} else if pane.app != nil {
			pane.app.HandleKey(ev)
		}
		// Mark pane dirty synchronously so the immediate Publish() call
		// in DesktopSink.HandleKeyEvent sees updated state.
		pane.markDirty()
	}
}

func (w *Workspace) handlePaste(data []byte) {
	if w == nil || len(data) == 0 {
		return
	}
	if w.tree.ActiveLeaf != nil && w.tree.ActiveLeaf.Pane != nil {
		w.tree.ActiveLeaf.Pane.handlePaste(data)
	}
}

func (w *Workspace) handleAppExit(p *pane, exitedApp App, runErr error) {
	if w == nil || p == nil || w.tree == nil {
		return
	}
	if w.isDesktopClosing() {
		return
	}

	// Ignore stale callbacks if the pane has already attached a new app.
	if exitedApp != nil && p.app != nil && p.app != exitedApp {
		debuglog.Printf("handleAppExit: ignoring exit for stale app '%s'", exitedApp.GetTitle())
		return
	}

	title := "unknown"
	if exitedApp != nil {
		title = exitedApp.GetTitle()
	} else if p.app != nil {
		title = p.app.GetTitle()
	}

	if runErr != nil {
		log.Printf("handleAppExit: app '%s' exited with error: %v - preserving pane", title, runErr)
		// Do not remove the pane. This allows the user to see the error or restart the session
		// without losing the window layout.
		return
	} else {
		debuglog.Printf("handleAppExit: app '%s' exited cleanly", title)
	}

	node := w.tree.FindNodeWithPane(p)
	if node == nil {
		debuglog.Printf("handleAppExit: pane for app '%s' already removed", title)
		return
	}

	w.removeNode(node, true)
}

// ActivePane returns the currently active pane in this workspace.
// Returns nil if there is no active pane.
func (w *Workspace) ActivePane() *pane {
	if w == nil || w.tree == nil || w.tree.ActiveLeaf == nil {
		return nil
	}
	return w.tree.ActiveLeaf.Pane
}

func (w *Workspace) ensureWelcomePane() {
	if w == nil || w.tree == nil {
		return
	}
	if w.tree.Root != nil {
		return
	}
	if w.isDesktopClosing() {
		return
	}

	var app App
	if w.desktop != nil && w.desktop.InitAppName != "" {
		if appInstance := w.desktop.Registry().CreateApp(w.desktop.InitAppName, nil); appInstance != nil {
			if a, ok := appInstance.(App); ok {
				app = a
			}
		}
	}

	if app == nil && w.ShellAppFactory != nil {
		app = w.ShellAppFactory()
	}

	if app == nil {
		return
	}

	w.AddApp(app)
	w.recalculateLayout()
	if w.desktop != nil {
		w.desktop.broadcastTreeChanged()
		w.desktop.broadcastActivePaneChanged()
	}
}

func (w *Workspace) Close() {
	w.finishMouseResize()

	// Close all panes
	w.tree.Traverse(func(node *Node) {
		if node.Pane != nil {
			node.Pane.Close()
		}
	})
}


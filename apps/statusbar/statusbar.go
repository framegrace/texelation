// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/statusbar/statusbar.go
// Summary: Widget-based status bar application using UIApp adapter.
// Usage: Added to desktops to render workspace tabs and mode/title metadata.

package statusbar

import (
	"fmt"
	"sync"
	"time"

	"github.com/framegrace/texelation/texel"
	"github.com/framegrace/texelui/adapter"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/primitives"
	"github.com/gdamore/tcell/v2"
)

const fpsEMAAlpha = 0.1 // smoothing factor: lower = smoother, ~10-frame window

// StatusBarApp displays workspace tabs and status information.
type StatusBarApp struct {
	app       *adapter.UIApp
	ui        *core.UIManager
	tabBar    *primitives.TabBar
	blendLine *BlendInfoLine

	mu         sync.RWMutex
	workspaces []texel.WorkspaceInfo
	activeID   int
	actions    texel.StatusBarActions
	stopClock  chan struct{}

	// FPS smoothing
	lastRenderTime time.Time
	smoothFPS      float64
	smoothTheoFPS  float64
}

// New creates a new StatusBarApp.
func New() *StatusBarApp {
	ui := core.NewUIManager()
	ui.SetStatusBar(nil) // no nested status bar

	tabBar := primitives.NewTabBar(0, 0, 80, []primitives.TabItem{{Label: "default"}})
	tabBar.Style.NoBlendRow = true

	blendLine := NewBlendInfoLine()

	ui.AddWidget(tabBar)
	ui.AddWidget(blendLine)

	app := adapter.NewUIApp("Status Bar", ui)
	app.DisableStatusBar()

	sb := &StatusBarApp{
		app:        app,
		ui:         ui,
		tabBar:     tabBar,
		blendLine:  blendLine,
		workspaces: []texel.WorkspaceInfo{{ID: 1, Name: "default"}},
		activeID:   1,
		stopClock:  make(chan struct{}),
	}

	// Wire tab change -> workspace switch.
	tabBar.OnChange = func(idx int) {
		sb.mu.RLock()
		actions := sb.actions
		var wsID int
		if idx >= 0 && idx < len(sb.workspaces) {
			wsID = sb.workspaces[idx].ID
		}
		sb.mu.RUnlock()
		if actions != nil && wsID > 0 {
			actions.SwitchToWorkspace(wsID)
		}
	}

	// Wire tab rename -> workspace rename.
	tabBar.OnRename = func(index int, newName string) {
		sb.mu.RLock()
		actions := sb.actions
		var wsID int
		if index >= 0 && index < len(sb.workspaces) {
			wsID = sb.workspaces[index].ID
		}
		sb.mu.RUnlock()
		if actions != nil && wsID > 0 {
			if newName == "" {
				newName = fmt.Sprintf("%d", wsID)
			}
			actions.RenameWorkspace(wsID, newName)
		}
	}

	// Wire resize -> reposition widgets.
	app.SetOnResize(func(w, h int) {
		tabBar.SetPosition(0, 0)
		tabBar.Resize(w, 1)
		blendLine.SetPosition(0, 1)
		blendLine.Resize(w, 1)
	})

	return sb
}

// SetActions stores the callback interface for workspace operations.
func (sb *StatusBarApp) SetActions(actions texel.StatusBarActions) {
	sb.mu.Lock()
	sb.actions = actions
	sb.mu.Unlock()
}

// --- core.App delegation ---

func (sb *StatusBarApp) Run() error {
	go sb.clockLoop()
	return sb.app.Run()
}

func (sb *StatusBarApp) Stop() {
	close(sb.stopClock)
	sb.app.Stop()
}

func (sb *StatusBarApp) Resize(cols, rows int) { sb.app.Resize(cols, rows) }

func (sb *StatusBarApp) GetTitle() string { return sb.app.GetTitle() }

func (sb *StatusBarApp) HandleKey(ev *tcell.EventKey) { sb.app.HandleKey(ev) }

func (sb *StatusBarApp) SetRefreshNotifier(ch chan<- bool) { sb.app.SetRefreshNotifier(ch) }

func (sb *StatusBarApp) Render() [][]core.Cell { return sb.app.Render() }

// --- Listener interface ---

// OnEvent handles state updates from the desktop's dispatcher.
func (sb *StatusBarApp) OnEvent(event texel.Event) {
	switch event.Type {
	case texel.EventWorkspacesChanged:
		if p, ok := event.Payload.(texel.WorkspacesChangedPayload); ok {
			sb.handleWorkspacesChanged(p)
		}
	case texel.EventWorkspaceSwitched:
		if p, ok := event.Payload.(texel.WorkspaceSwitchedPayload); ok {
			sb.handleWorkspaceSwitched(p)
		}
	case texel.EventModeChanged:
		if p, ok := event.Payload.(texel.ModeChangedPayload); ok {
			sb.blendLine.SetMode(p.InControlMode, p.SubMode)
		}
	case texel.EventActivePaneChanged:
		if p, ok := event.Payload.(texel.ActivePaneChangedPayload); ok {
			sb.blendLine.SetTitle(p.ActiveTitle)
		}
	case texel.EventPerformanceUpdate:
		if p, ok := event.Payload.(texel.PerformanceUpdatePayload); ok {
			sb.updateFPS(p.LastPublishDuration)
		}
	case texel.EventToast:
		if p, ok := event.Payload.(texel.ToastPayload); ok {
			sb.blendLine.ShowToast(p.Message, p.Severity, p.Duration)
		}
	}
}

// handleWorkspacesChanged rebuilds the tab bar from the workspace list.
func (sb *StatusBarApp) handleWorkspacesChanged(p texel.WorkspacesChangedPayload) {
	sb.mu.Lock()
	prevCount := len(sb.workspaces)
	sb.workspaces = p.Workspaces
	sb.activeID = p.ActiveID

	// Rebuild tab items.
	tabs := make([]primitives.TabItem, len(p.Workspaces))
	activeIdx := 0
	for i, ws := range p.Workspaces {
		label := ws.Name
		if label == "" {
			label = fmt.Sprintf("%d", ws.ID)
		}
		tabs[i] = primitives.TabItem{Label: label}
		if ws.ID == p.ActiveID {
			activeIdx = i
		}
	}
	sb.tabBar.Tabs = tabs
	sb.tabBar.ActiveIdx = activeIdx

	// Update accent color from active workspace.
	for _, ws := range p.Workspaces {
		if ws.ID == p.ActiveID && ws.Color != 0 {
			sb.blendLine.SetAccentColor(ws.Color)
			break
		}
	}

	newCount := len(p.Workspaces)
	newIdx := newCount - 1
	sb.mu.Unlock()

	// If a new workspace was added (count increased) and it's not the first, enter edit mode.
	if newCount > prevCount && newCount > 1 {
		sb.tabBar.EditTab(newIdx)
	}

	sb.refresh()
}

// handleWorkspaceSwitched updates the active tab.
func (sb *StatusBarApp) handleWorkspaceSwitched(p texel.WorkspaceSwitchedPayload) {
	sb.mu.Lock()
	sb.activeID = p.ActiveID
	activeIdx := 0
	for i, ws := range sb.workspaces {
		if ws.ID == p.ActiveID {
			activeIdx = i
			if ws.Color != 0 {
				sb.blendLine.SetAccentColor(ws.Color)
			}
			break
		}
	}
	sb.mu.Unlock()
	sb.tabBar.SetActive(activeIdx)
	sb.refresh()
}

// updateFPS uses EMA smoothing for actual and theoretical FPS.
func (sb *StatusBarApp) updateFPS(publishDuration time.Duration) {
	sb.mu.Lock()
	now := time.Now()
	if !sb.lastRenderTime.IsZero() {
		dt := now.Sub(sb.lastRenderTime)
		if dt > 0 {
			instantFPS := float64(time.Second) / float64(dt)
			sb.smoothFPS = sb.smoothFPS + fpsEMAAlpha*(instantFPS-sb.smoothFPS)
		}
	}
	sb.lastRenderTime = now

	if publishDuration > 0 {
		instantTheo := float64(time.Second) / float64(publishDuration)
		sb.smoothTheoFPS = sb.smoothTheoFPS + fpsEMAAlpha*(instantTheo-sb.smoothTheoFPS)
	}

	actual := sb.smoothFPS
	theo := sb.smoothTheoFPS
	sb.mu.Unlock()

	sb.blendLine.SetFPS(actual, theo)
	sb.refresh()
}

// clockLoop ticks every second to update the clock display.
func (sb *StatusBarApp) clockLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-sb.stopClock:
			return
		case t := <-ticker.C:
			sb.blendLine.SetClock(t.Format("15:04:05"))
			sb.refresh()
		}
	}
}

// refresh sends a non-blocking signal on the app's refresh channel.
func (sb *StatusBarApp) refresh() {
	ch := sb.app.RefreshChan()
	if ch == nil {
		return
	}
	select {
	case ch <- true:
	default:
	}
}

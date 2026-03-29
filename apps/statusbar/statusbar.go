// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/statusbar/statusbar.go
// Summary: Widget-based status bar application using UIApp adapter.
// Usage: Added to desktops to render workspace tabs and mode/title metadata.

package statusbar

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/framegrace/texelation/texel"
	"github.com/framegrace/texelui/adapter"
	dyncolor "github.com/framegrace/texelui/color"
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

	// Tab mode
	tabMode    bool
	accentBase tcell.Color // stored accent for pulsation

	// FPS smoothing
	lastRenderTime time.Time
	smoothFPS      float64
	smoothTheoFPS  float64
}

// New creates a new StatusBarApp.
func New() *StatusBarApp {
	ui := core.NewUIManager()
	ui.SetStatusBar(nil) // no nested status bar

	initialColor := texel.WorkspaceAccentColor(0)
	tabBar := primitives.NewTabBar(0, 0, 80, []primitives.TabItem{
		{Label: "default", Color: dyncolor.Solid(darkenColor(initialColor, 0.5))},
	})
	tabBar.Style.NoBlendRow = true

	blendLine := NewBlendInfoLine()

	// Match TabBar colors to blend line so they look seamless.
	tabBar.Style.ActiveBG = dyncolor.Solid(initialColor)
	tabBar.Style.ContentBG = dyncolor.Solid(blendLine.contentBG)
	tabBar.Style.BarBG = dyncolor.Solid(blendLine.contentBG)

	ui.AddWidget(tabBar)
	ui.AddWidget(blendLine)

	app := adapter.NewUIApp("Status Bar", ui)
	app.DisableStatusBar()

	sb := &StatusBarApp{
		app:        app,
		ui:         ui,
		tabBar:     tabBar,
		blendLine:  blendLine,
		workspaces: []texel.WorkspaceInfo{{ID: 1, Name: "default", Color: initialColor}},
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

	// Wire resize -> reposition widgets and re-apply accent colors.
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

// HandleClick processes a click at local coordinates within the status bar.
// Row 0 = tabs. Clicking a tab switches to that workspace.
func (sb *StatusBarApp) HandleClick(localX, localY int) {
	if localY != 0 {
		return // only row 0 has clickable tabs
	}
	// Use TabBar's tabAtX to find which tab was clicked.
	idx := sb.tabBar.TabAtX(localX)
	if idx < 0 {
		return
	}
	sb.mu.RLock()
	actions := sb.actions
	var wsID int
	if idx < len(sb.workspaces) {
		wsID = sb.workspaces[idx].ID
	}
	sb.mu.RUnlock()
	if actions != nil && wsID > 0 {
		actions.SwitchToWorkspace(wsID)
	}
}

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
		tabs[i] = primitives.TabItem{
			Label: label,
			Color: dyncolor.Solid(darkenColor(ws.Color, 0.5)),
		}
		if ws.ID == p.ActiveID {
			activeIdx = i
		}
	}
	sb.tabBar.Tabs = tabs
	sb.tabBar.ActiveIdx = activeIdx

	// Update accent color from active workspace.
	for _, ws := range p.Workspaces {
		if ws.ID == p.ActiveID && ws.Color != 0 {
			sb.setAccentColor(ws.Color)
			break
		}
	}

	sb.mu.Unlock()
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
				sb.setAccentColor(ws.Color)
			}
			break
		}
	}
	sb.mu.Unlock()
	sb.tabBar.SetActive(activeIdx)
	sb.refresh()
}

// setAccentColor updates both the blend line and the TabBar active/inactive tab colors.
// If tab mode is active, the animated colors are rebuilt from the new base.
func (sb *StatusBarApp) setAccentColor(c tcell.Color) {
	if sb.tabMode {
		sb.accentBase = c
		pulse := makePulse(c)
		sb.tabBar.Style.ActiveBG = pulse
		sb.blendLine.SetAccentDynamic(pulse)
	} else {
		sb.blendLine.SetAccentColor(c)
		sb.tabBar.Style.ActiveBG = dyncolor.Solid(c)
	}
	sb.tabBar.Style.InactiveBG = dyncolor.Solid(darkenColor(c, 0.5))
}

// makePulse creates a DynamicColor that oscillates a base color between 70% and 100% brightness.
func makePulse(base tcell.Color) dyncolor.DynamicColor {
	r, g, b := base.RGB()
	startTime := time.Now()
	return dyncolor.Func(func(_ dyncolor.ColorContext) tcell.Color {
		elapsed := time.Since(startTime).Seconds()
		factor := 0.7 + 0.3*math.Sin(elapsed*6)
		return tcell.NewRGBColor(
			int32(float64(r)*factor),
			int32(float64(g)*factor),
			int32(float64(b)*factor),
		)
	})
}

// darkenColor scales an RGB color's channels by the given factor (0.0–1.0).
func darkenColor(c tcell.Color, factor float64) tcell.Color {
	r, g, b := c.RGB()
	return tcell.NewRGBColor(
		int32(float64(r)*factor),
		int32(float64(g)*factor),
		int32(float64(b)*factor),
	)
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

// --- TabModeHandler interface ---

// HandleTabModeKey processes a key event while tab mode is active.
// Left/Right navigate tabs, Enter toggles edit, Escape is handled by the desktop.
func (sb *StatusBarApp) HandleTabModeKey(ev *tcell.EventKey) {
	// If editing, route to TabBar (which routes to the inline editor)
	if sb.tabBar.IsEditing() {
		sb.tabBar.HandleKey(ev)
		sb.refresh()
		return
	}

	switch ev.Key() {
	case tcell.KeyLeft:
		idx := sb.tabBar.ActiveIdx - 1
		if idx >= 0 {
			sb.tabBar.SetActive(idx)
			sb.switchToTabIdx(idx)
		}
	case tcell.KeyRight:
		idx := sb.tabBar.ActiveIdx + 1
		if idx < len(sb.tabBar.Tabs) {
			sb.tabBar.SetActive(idx)
			sb.switchToTabIdx(idx)
		}
	case tcell.KeyEnter:
		sb.tabBar.EditTab(sb.tabBar.ActiveIdx)
	}
	sb.refresh()
}

// ExitTabMode cancels any active edit, stops pulsation, restores static colors.
func (sb *StatusBarApp) ExitTabMode() {
	if sb.tabBar.IsEditing() {
		sb.tabBar.CancelEdit()
	}
	if sb.tabMode {
		sb.tabMode = false
		// Restore the active workspace's own accent color (static, stops animation).
		sb.mu.RLock()
		for _, ws := range sb.workspaces {
			if ws.ID == sb.activeID && ws.Color != 0 {
				sb.setAccentColor(ws.Color)
				break
			}
		}
		sb.mu.RUnlock()
	}
	sb.refresh()
}

// EnterTabMode activates pulsating animation on the active tab.
func (sb *StatusBarApp) EnterTabMode() {
	sb.tabMode = true
	sb.mu.RLock()
	// Find current accent color from active workspace.
	for _, ws := range sb.workspaces {
		if ws.ID == sb.activeID && ws.Color != 0 {
			sb.accentBase = ws.Color
			break
		}
	}
	sb.mu.RUnlock()

	// Build a pulsating color function from the base accent.
	pulse := makePulse(sb.accentBase)

	// Apply to both TabBar and blend line.
	sb.tabBar.Style.ActiveBG = pulse
	sb.blendLine.SetAccentDynamic(pulse)

	sb.refresh()
}

func (sb *StatusBarApp) switchToTabIdx(idx int) {
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

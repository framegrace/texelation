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
	stopOnce   sync.Once

	// Tab creation / deletion / navigation
	creatingNewWs    bool  // true while editing a newly created tab
	newTabInsertIdx  int   // where the new tab was inserted
	pendingDelete    int   // workspace ID awaiting delete confirmation (0 = none)
	wantsExitTabMode bool  // set when the desktop should exit tab mode
	navMode          bool  // lightweight navigation with pulse
	tabOrder         []int // workspace IDs in display order

	// FPS smoothing
	lastRenderTime time.Time
	smoothFPS      float64
	smoothTheoFPS  float64
	lastFPSInt     int
	lastTheoFPSInt int
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
	blendLine.SetAccentColor(initialColor)

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

	// Wire tab rename -> workspace rename or creation.
	tabBar.OnRename = func(index int, newName string) {
		if sb.creatingNewWs {
			// Creating a new workspace — confirm with Enter
			sb.creatingNewWs = false
			sb.mu.RLock()
			actions := sb.actions
			sb.mu.RUnlock()
			if actions != nil {
				if newName == "" {
					newName = fmt.Sprintf("%d", len(sb.tabBar.Tabs))
				}
				actions.CreateWorkspace(newName)
			}
			// Signal desktop to exit tab mode so user lands on the new workspace
			sb.wantsExitTabMode = true
			return
		}
		// Renaming an existing workspace
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
	sb.stopOnce.Do(func() { close(sb.stopClock) })
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
		// Intentionally ignored — FPS display caused 60fps buffer deltas
		// to the client, starving keyboard input. Will be reimplemented
		// client-side if needed.
	case texel.EventToast:
		if p, ok := event.Payload.(texel.ToastPayload); ok {
			sb.blendLine.ShowToast(p.Message, p.Severity, p.Duration)
		}
	}
}

// handleWorkspacesChanged rebuilds the tab bar preserving user-defined tab order.
func (sb *StatusBarApp) handleWorkspacesChanged(p texel.WorkspacesChangedPayload) {
	sb.mu.Lock()
	sb.workspaces = p.Workspaces
	sb.activeID = p.ActiveID

	// Build a lookup of current workspaces.
	wsMap := make(map[int]texel.WorkspaceInfo, len(p.Workspaces))
	for _, ws := range p.Workspaces {
		wsMap[ws.ID] = ws
	}

	// Update tabOrder: keep existing order, remove deleted, insert new.
	// Remove IDs that no longer exist.
	kept := make([]int, 0, len(sb.tabOrder))
	for _, id := range sb.tabOrder {
		if _, ok := wsMap[id]; ok {
			kept = append(kept, id)
		}
	}
	// Find new IDs not in tabOrder.
	existing := make(map[int]bool, len(kept))
	for _, id := range kept {
		existing[id] = true
	}
	var newIDs []int
	for _, ws := range p.Workspaces {
		if !existing[ws.ID] {
			newIDs = append(newIDs, ws.ID)
		}
	}
	// Insert new IDs at the recorded insert position, or append.
	if len(newIDs) > 0 && sb.newTabInsertIdx > 0 && sb.newTabInsertIdx <= len(kept) {
		order := make([]int, 0, len(kept)+len(newIDs))
		order = append(order, kept[:sb.newTabInsertIdx]...)
		order = append(order, newIDs...)
		order = append(order, kept[sb.newTabInsertIdx:]...)
		kept = order
		sb.newTabInsertIdx = 0
	} else {
		kept = append(kept, newIDs...)
	}
	sb.tabOrder = kept

	// Build tabs in tabOrder sequence.
	tabs := make([]primitives.TabItem, 0, len(kept))
	activeIdx := 0
	for i, id := range kept {
		ws, ok := wsMap[id]
		if !ok {
			continue
		}
		label := ws.Name
		if label == "" {
			label = fmt.Sprintf("%d", ws.ID)
		}
		tabs = append(tabs, primitives.TabItem{
			Label: label,
			Color: dyncolor.Solid(darkenColor(ws.Color, 0.5)),
		})
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

// UI returns the underlying UIManager for configuration.
func (sb *StatusBarApp) UI() *core.UIManager {
	return sb.ui
}

// setAccentColor updates both the blend line and the TabBar active/inactive tab colors.
func (sb *StatusBarApp) setAccentColor(c tcell.Color) {
	if sb.navMode {
		// Rebuild pulse from new color
		pulse := dyncolor.Pulse(c, 0.7, 1.0, 6)
		sb.tabBar.Style.ActiveBG = pulse
		sb.blendLine.SetAccentDynamic(pulse)
	} else {
		sb.blendLine.SetAccentColor(c)
		sb.tabBar.Style.ActiveBG = dyncolor.Solid(c)
	}
	sb.tabBar.Style.InactiveBG = dyncolor.Solid(darkenColor(c, 0.5))
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

	// Only update display when the rounded values change — avoids
	// producing a new buffer delta every single frame.
	actualInt := int(actual + 0.5)
	theoInt := int(theo + 0.5)
	changed := actualInt != sb.lastFPSInt || theoInt != sb.lastTheoFPSInt
	sb.lastFPSInt = actualInt
	sb.lastTheoFPSInt = theoInt
	sb.mu.Unlock()

	if changed {
		sb.refresh()
	}
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
			sb.blendLine.SetClock(t.Format("Mon 02 Jan"), t.Format("15:04:05"))
			sb.refresh()
		}
	}
}

// --- TabModeHandler interface ---

// StartNewTab inserts a new tab after the current one and opens the editor.
func (sb *StatusBarApp) StartNewTab() {
	sb.creatingNewWs = true
	insertIdx := sb.tabBar.ActiveIdx + 1
	sb.newTabInsertIdx = insertIdx // remember for handleWorkspacesChanged
	newColor := texel.WorkspaceAccentColor(len(sb.tabBar.Tabs))

	// Insert tab after the current active one.
	newTab := primitives.TabItem{
		Label: "",
		Color: dyncolor.Solid(darkenColor(newColor, 0.5)),
	}
	tabs := make([]primitives.TabItem, 0, len(sb.tabBar.Tabs)+1)
	tabs = append(tabs, sb.tabBar.Tabs[:insertIdx]...)
	tabs = append(tabs, newTab)
	tabs = append(tabs, sb.tabBar.Tabs[insertIdx:]...)
	sb.tabBar.Tabs = tabs
	sb.tabBar.SetActive(insertIdx)

	// Set accent color to the new tab's color immediately so it looks right while editing.
	sb.setAccentColor(newColor)

	sb.tabBar.EditTab(insertIdx)
	sb.refresh()
}

// StartRenameTab opens the editor on the current active tab for renaming.
func (sb *StatusBarApp) StartRenameTab() {
	sb.tabBar.EditTab(sb.tabBar.ActiveIdx)
	sb.refresh()
}

// StartCloseWorkspace shows a confirmation toast for closing the active workspace.
func (sb *StatusBarApp) StartCloseWorkspace() {
	sb.mu.RLock()
	var wsID int
	idx := sb.tabBar.ActiveIdx
	if idx >= 0 && idx < len(sb.workspaces) {
		wsID = sb.workspaces[idx].ID
	}
	wsCount := len(sb.workspaces)
	sb.mu.RUnlock()

	if wsCount <= 1 {
		sb.blendLine.ShowToast("Cannot close the last workspace", texel.ToastWarning, 2*time.Second)
		sb.wantsExitTabMode = true
		sb.refresh()
		return
	}
	if wsID > 0 {
		sb.pendingDelete = wsID
		name := sb.tabBar.Tabs[idx].Label
		sb.blendLine.ShowToast(fmt.Sprintf("Close workspace '%s'? (y/n)", name), texel.ToastError, 30*time.Second)
	}
	sb.refresh()
}

// HandleTabModeKey routes keys to the tab editor or delete confirmation.
func (sb *StatusBarApp) HandleTabModeKey(ev *tcell.EventKey) {
	// Handle delete confirmation
	if sb.pendingDelete > 0 {
		switch ev.Key() {
		case tcell.KeyRune:
			if ev.Rune() == 'y' || ev.Rune() == 'Y' {
				sb.mu.RLock()
				actions := sb.actions
				id := sb.pendingDelete
				sb.mu.RUnlock()
				if actions != nil {
					actions.CloseWorkspace(id)
				}
				sb.blendLine.DismissToast()
			} else {
				sb.blendLine.DismissToast()
			}
		case tcell.KeyEsc:
			sb.blendLine.DismissToast()
		}
		sb.pendingDelete = 0
		sb.wantsExitTabMode = true
		sb.refresh()
		return
	}

	// Handle tab name editing
	if sb.tabBar.IsEditing() {
		switch ev.Key() {
		case tcell.KeyEsc:
			sb.tabBar.CancelEdit()
			if sb.creatingNewWs {
				sb.creatingNewWs = false
				idx := sb.tabBar.ActiveIdx
				if idx >= 0 && idx < len(sb.tabBar.Tabs) {
					sb.tabBar.Tabs = append(sb.tabBar.Tabs[:idx], sb.tabBar.Tabs[idx+1:]...)
					if sb.tabBar.ActiveIdx >= len(sb.tabBar.Tabs) {
						sb.tabBar.ActiveIdx = len(sb.tabBar.Tabs) - 1
					}
				}
			}
			sb.wantsExitTabMode = true
		default:
			sb.tabBar.HandleKey(ev)
		}
		sb.refresh()
		return
	}

	// Nothing active — exit
	sb.wantsExitTabMode = true
	sb.refresh()
}

// EnterNavMode starts pulsating the active tab for visual feedback during navigation.
func (sb *StatusBarApp) EnterNavMode() {
	sb.navMode = true
	sb.mu.RLock()
	var accentColor tcell.Color
	for _, ws := range sb.workspaces {
		if ws.ID == sb.activeID && ws.Color != 0 {
			accentColor = ws.Color
			break
		}
	}
	sb.mu.RUnlock()
	if accentColor != 0 {
		pulse := dyncolor.Pulse(accentColor, 0.7, 1.0, 6)
		sb.tabBar.Style.ActiveBG = pulse
		sb.blendLine.SetAccentDynamic(pulse)
	}
	sb.refresh()
}

// ExitNavMode stops the pulse and restores static colors.
func (sb *StatusBarApp) exitNavMode() {
	if !sb.navMode {
		return
	}
	sb.navMode = false
	sb.mu.RLock()
	for _, ws := range sb.workspaces {
		if ws.ID == sb.activeID && ws.Color != 0 {
			sb.setAccentColor(ws.Color)
			break
		}
	}
	sb.mu.RUnlock()
	sb.refresh()
}

// ExitTabMode cancels any active edit and cleans up.
func (sb *StatusBarApp) ExitTabMode() {
	if sb.tabBar.IsEditing() {
		sb.tabBar.CancelEdit()
	}
	sb.creatingNewWs = false
	sb.exitNavMode()
	sb.refresh()
}

// IsActive returns true if the status bar is editing a tab name or awaiting delete confirmation.
func (sb *StatusBarApp) IsActive() bool {
	return sb.tabBar.IsEditing() || sb.pendingDelete > 0
}

// WantsExitTabMode returns true if the status bar wants the desktop to exit tab mode.
func (sb *StatusBarApp) WantsExitTabMode() bool {
	if sb.wantsExitTabMode {
		sb.wantsExitTabMode = false
		return true
	}
	return false
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

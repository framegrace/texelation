package texel

import (
	"github.com/framegrace/texelation/internal/debuglog"
	"github.com/framegrace/texelation/internal/keybind"
	"github.com/gdamore/tcell/v2"
)

// toggleControlMode flips control mode state and broadcasts the change.
func (d *DesktopEngine) toggleControlMode() {
	wasInControlMode := d.inControlMode
	d.inControlMode = !d.inControlMode
	d.subControlMode = 0

	debuglog.Printf("toggleControlMode: was=%v, now=%v", wasInControlMode, d.inControlMode)

	if !d.inControlMode && d.resizeSelection != nil {
		d.activeWorkspace.clearResizeSelection(d.resizeSelection)
		d.resizeSelection = nil
	}

	if d.activeWorkspace != nil && wasInControlMode != d.inControlMode {
		debuglog.Printf("toggleControlMode: State changed, calling SetControlMode(%v)", d.inControlMode)
		d.activeWorkspace.SetControlMode(d.inControlMode)
	} else {
		debuglog.Printf("toggleControlMode: State didn't change or no active workspace")
	}

	// Show/hide control mode help overlay
	if d.inControlMode {
		d.launchControlHelpOverlay()
	} else {
		d.closeControlHelpOverlay()
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
	d.broadcastModeChanged()
}

// toggleZoom collapses or expands the active pane.
func (d *DesktopEngine) toggleZoom() {
	if d.activeWorkspace == nil {
		return
	}

	if d.zoomedPane == nil {
		nodeToZoom := d.activeWorkspace.tree.ActiveLeaf
		if nodeToZoom == nil || nodeToZoom.Pane == nil {
			return
		}
		d.zoomedPane = nodeToZoom
		nodeToZoom.Pane.SetZOrder(ZOrderAnimation)
	} else {
		if d.zoomedPane.Pane != nil {
			d.zoomedPane.Pane.SetZOrder(ZOrderDefault)
		}
		d.zoomedPane = nil
	}

	d.recalculateLayout()
	d.broadcastActivePaneChanged()
	d.broadcastTreeChanged()
}

// startNewTab tells the status bar to create a new tab editor after the current tab.
// Keys are routed to the editor until Enter (create) or Escape (cancel).
func (d *DesktopEngine) startNewTab() {
	// Exit nav mode if active (clean up pulse).
	if d.inTabMode {
		d.exitTabMode()
	}
	d.inTabMode = true
	for _, sp := range d.statusPanes {
		if handler, ok := sp.app.(TabModeHandler); ok {
			handler.StartNewTab()
		}
	}
}

// startCloseWorkspace shows a confirmation toast for closing the active workspace.
// Keys are routed to the status bar for Y/N confirmation.
func (d *DesktopEngine) startCloseWorkspace() {
	if len(d.workspaces) <= 1 {
		return // can't close the last one
	}
	d.inTabMode = true
	for _, sp := range d.statusPanes {
		if handler, ok := sp.app.(TabModeHandler); ok {
			handler.StartCloseWorkspace()
		}
	}
}

// enterTabNavMode activates lightweight tab navigation (Shift+Up past top pane).
// Left/Right switch workspaces, Shift+Down or Escape exits.
// Deactivates the current workspace pane so no pane appears focused.
func (d *DesktopEngine) enterTabNavMode() {
	d.inTabMode = true

	// Deactivate the current pane so the status bar appears as the focused element.
	if ws := d.activeWorkspace; ws != nil && ws.tree != nil && ws.tree.ActiveLeaf != nil {
		if p := ws.tree.ActiveLeaf.Pane; p != nil {
			p.SetActive(false)
		}
	}

	for _, sp := range d.statusPanes {
		if handler, ok := sp.app.(TabModeHandler); ok {
			handler.EnterNavMode()
		}
	}
}

// exitTabMode leaves tab editing mode and reactivates the workspace pane.
func (d *DesktopEngine) exitTabMode() {
	d.inTabMode = false
	for _, sp := range d.statusPanes {
		if handler, ok := sp.app.(TabModeHandler); ok {
			handler.ExitTabMode()
		}
	}

	// Reactivate the workspace's active pane.
	if ws := d.activeWorkspace; ws != nil && ws.tree != nil && ws.tree.ActiveLeaf != nil {
		if p := ws.tree.ActiveLeaf.Pane; p != nil {
			p.SetActive(true)
		}
	}
}

// handleTabMode routes keys to the status bar's tab editor, or handles
// lightweight tab navigation (Shift+Up past top pane).
func (d *DesktopEngine) handleTabMode(ev *tcell.EventKey) {
	// Check if a status bar handler is actively editing/confirming.
	for _, sp := range d.statusPanes {
		if handler, ok := sp.app.(TabModeHandler); ok {
			if handler.IsActive() {
				handler.HandleTabModeKey(ev)
				if handler.WantsExitTabMode() {
					d.exitTabMode()
				}
				return
			}
		}
	}

	// Lightweight navigation mode (from Shift+Up past top pane).
	if ev.Key() == tcell.KeyEsc {
		d.exitTabMode()
		return
	}

	// Keybinding-based navigation gestures.
	if d.keybindings != nil {
		action := d.keybindings.Match(ev)
		switch action {
		case keybind.PaneNavUp:
			// Already at the top — do nothing.
			return
		case keybind.PaneNavDown:
			d.exitTabMode()
			return
		case keybind.PaneNavLeft:
			d.switchWorkspaceRelative(-1)
			return
		case keybind.PaneNavRight:
			d.switchWorkspaceRelative(1)
			return
		}
	}

	// Any other key exits nav mode and is swallowed.
	d.exitTabMode()
}

// handleControlMode processes desktop-level commands when control mode is active.
func (d *DesktopEngine) handleControlMode(ev *tcell.EventKey) {
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

	action := d.keybindings.Match(ev)
	exitControlMode := true
	switch action {
	case keybind.ControlClose:
		if d.zoomedPane != nil {
			d.activeWorkspace.CloseActivePane()
			d.zoomedPane = nil
		} else {
			d.activeWorkspace.CloseActivePane()
		}
	case keybind.ControlVSplit:
		d.activeWorkspace.PerformSplit(Vertical)
	case keybind.ControlHSplit:
		d.activeWorkspace.PerformSplit(Horizontal)
	case keybind.ControlSwap:
		d.subControlMode = 'w'
		d.broadcastModeChanged()
		exitControlMode = false
	case keybind.ControlZoom:
		d.toggleZoom()
	case keybind.ControlLauncher:
		d.launchLauncherOverlay()
	case keybind.ControlHelp:
		d.launchHelpOverlay()
	case keybind.ControlConfig:
		d.launchConfigEditorOverlay("system")
	case keybind.ControlNewTab:
		d.startNewTab()
	case keybind.ControlCloseTab:
		d.startCloseWorkspace()
	default:
		// Unknown key — still exit control mode
	}

	if exitControlMode {
		d.toggleControlMode()
	}
}

package texel

import (
	"github.com/framegrace/texelation/internal/debuglog"
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
func (d *DesktopEngine) enterTabNavMode() {
	d.inTabMode = true
	for _, sp := range d.statusPanes {
		if handler, ok := sp.app.(TabModeHandler); ok {
			handler.EnterNavMode()
		}
	}
}

// exitTabMode leaves tab editing mode.
func (d *DesktopEngine) exitTabMode() {
	d.inTabMode = false
	for _, sp := range d.statusPanes {
		if handler, ok := sp.app.(TabModeHandler); ok {
			handler.ExitTabMode()
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
	switch ev.Key() {
	case tcell.KeyEsc:
		d.exitTabMode()
	case tcell.KeyDown:
		if ev.Modifiers()&tcell.ModShift != 0 {
			d.exitTabMode()
		}
	case tcell.KeyLeft:
		d.switchWorkspaceRelative(-1)
	case tcell.KeyRight:
		d.switchWorkspaceRelative(1)
	default:
		// Any other key exits nav mode and is swallowed
		d.exitTabMode()
	}
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

	r := ev.Rune()
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
		d.broadcastModeChanged()
		exitControlMode = false
	case 'z':
		d.toggleZoom()
	case 'l':
		d.launchLauncherOverlay()
	case 'h':
		d.launchHelpOverlay()
	case 'f':
		d.launchConfigEditorOverlay("system")
	case 't':
		d.startNewTab()
	case 'X':
		d.startCloseWorkspace()
	}

	if exitControlMode {
		d.toggleControlMode()
	}
}

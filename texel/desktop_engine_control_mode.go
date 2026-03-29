package texel

import (
	"strconv"

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

// enterTabMode activates tab navigation mode. Control mode is exited first.
func (d *DesktopEngine) enterTabMode() {
	d.inTabMode = true
	for _, sp := range d.statusPanes {
		if handler, ok := sp.app.(TabModeHandler); ok {
			handler.EnterTabMode()
		}
	}
	d.broadcastModeChanged()
}

// exitTabMode leaves tab navigation mode.
func (d *DesktopEngine) exitTabMode() {
	d.inTabMode = false
	// Cancel any active tab edit.
	for _, sp := range d.statusPanes {
		if handler, ok := sp.app.(TabModeHandler); ok {
			handler.ExitTabMode()
		}
	}
	d.broadcastModeChanged()
}

// handleTabMode routes keys to the status bar's TabBar during tab mode.
func (d *DesktopEngine) handleTabMode(ev *tcell.EventKey) {
	if ev.Key() == tcell.KeyEsc {
		d.exitTabMode()
		return
	}
	// Shift+Down exits tab mode and returns focus to panes.
	if ev.Key() == tcell.KeyDown && ev.Modifiers()&tcell.ModShift != 0 {
		d.exitTabMode()
		return
	}
	// Shift+Left/Right also navigates workspaces in tab mode.
	if ev.Modifiers()&tcell.ModShift != 0 {
		switch ev.Key() {
		case tcell.KeyLeft, tcell.KeyRight:
			// Fall through to status bar handler (same as unshifted arrows)
		}
	}
	for _, sp := range d.statusPanes {
		if handler, ok := sp.app.(TabModeHandler); ok {
			handler.HandleTabModeKey(ev)
			return
		}
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
	if r >= '1' && r <= '9' {
		wsID, _ := strconv.Atoi(string(r))
		d.SwitchToWorkspace(wsID)
		d.toggleControlMode()
		return
	}

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
		d.enterTabMode()
	}

	if exitControlMode {
		d.toggleControlMode()
	}
}

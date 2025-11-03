package texel

import (
	"log"
	"strconv"

	"github.com/gdamore/tcell/v2"
)

// toggleControlMode flips control mode state and broadcasts the change.
func (d *DesktopEngine) toggleControlMode() {
	wasInControlMode := d.inControlMode
	d.inControlMode = !d.inControlMode
	d.subControlMode = 0

	log.Printf("toggleControlMode: was=%v, now=%v", wasInControlMode, d.inControlMode)

	if !d.inControlMode && d.resizeSelection != nil {
		d.activeWorkspace.clearResizeSelection(d.resizeSelection)
		d.resizeSelection = nil
	}

	if d.activeWorkspace != nil && wasInControlMode != d.inControlMode {
		log.Printf("toggleControlMode: State changed, calling SetControlMode(%v)", d.inControlMode)
		d.activeWorkspace.SetControlMode(d.inControlMode)
	} else {
		log.Printf("toggleControlMode: State didn't change or no active workspace")
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
	d.broadcastStateUpdate()
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
	d.broadcastStateUpdate()
	d.broadcastTreeChanged()
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
		d.broadcastStateUpdate()
		exitControlMode = false
	case 'z':
		d.toggleZoom()
	}

	if exitControlMode {
		d.toggleControlMode()
	}
}

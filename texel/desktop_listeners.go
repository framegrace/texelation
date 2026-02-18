// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/desktop_listeners.go
// Summary: Event listener registration and dispatch for the desktop engine.

package texel

func (d *DesktopEngine) RegisterFocusListener(listener DesktopFocusListener) {
	if listener == nil {
		return
	}
	d.focusMu.Lock()
	d.focusListeners = append(d.focusListeners, listener)
	d.focusMu.Unlock()
	d.notifyFocusActive()
}

// RegisterPaneStateListener registers a listener for pane active/resizing changes.
func (d *DesktopEngine) RegisterPaneStateListener(listener PaneStateListener) {
	if listener == nil {
		return
	}
	d.paneStateMu.Lock()
	d.paneStateListeners = append(d.paneStateListeners, listener)
	d.paneStateMu.Unlock()
}

// UnregisterFocusListener removes a previously registered focus listener.
func (d *DesktopEngine) UnregisterFocusListener(listener DesktopFocusListener) {
	if listener == nil {
		return
	}
	d.focusMu.Lock()
	defer d.focusMu.Unlock()
	for i, registered := range d.focusListeners {
		if registered == listener {
			d.focusListeners = append(d.focusListeners[:i], d.focusListeners[i+1:]...)
			break
		}
	}
}

// UnregisterPaneStateListener removes a previously registered pane state listener.
func (d *DesktopEngine) UnregisterPaneStateListener(listener PaneStateListener) {
	if listener == nil {
		return
	}
	d.paneStateMu.Lock()
	defer d.paneStateMu.Unlock()
	for i, registered := range d.paneStateListeners {
		if registered == listener {
			d.paneStateListeners = append(d.paneStateListeners[:i], d.paneStateListeners[i+1:]...)
			break
		}
	}
}

func (d *DesktopEngine) notifyFocus(paneID [16]byte) {
	d.focusMu.RLock()
	listeners := append([]DesktopFocusListener(nil), d.focusListeners...)
	d.focusMu.RUnlock()
	for _, listener := range listeners {
		listener.PaneFocused(paneID)
	}
}

func (d *DesktopEngine) notifyPaneState(id [16]byte, active, resizing bool, z int, handlesMouse bool) {
	d.paneStateMu.RLock()
	listeners := append([]PaneStateListener(nil), d.paneStateListeners...)
	d.paneStateMu.RUnlock()
	for _, l := range listeners {
		l.PaneStateChanged(id, active, resizing, z, handlesMouse)
	}
}

func (d *DesktopEngine) notifyFocusActive() {
	if d.activeWorkspace == nil || d.activeWorkspace.tree == nil {
		return
	}
	d.notifyFocusNode(d.activeWorkspace.tree.ActiveLeaf)
}

func (d *DesktopEngine) notifyFocusNode(node *Node) {
	if node == nil || node.Pane == nil {
		return
	}
	d.notifyFocus(node.Pane.ID())
}

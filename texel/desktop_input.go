// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/desktop_input.go
// Summary: Key and mouse input routing for the desktop engine.

package texel

import "github.com/gdamore/tcell/v2"

// InjectKeyEvent allows external callers (e.g., remote clients) to deliver key
// input directly into the desktop event pipeline.
func (d *DesktopEngine) InjectKeyEvent(key tcell.Key, ch rune, modifiers tcell.ModMask) {
	if key == tcell.KeyRune {
		switch ch {
		case '\n', '\r':
			key = tcell.KeyEnter
		case '\t':
			key = tcell.KeyTab
		}
	}
	d.SendEvent(desktopEvent{kind: keyEventKind, key: key, ch: ch, mod: modifiers})
}

// InjectMouseEvent records the latest mouse event metadata from remote clients.
func (d *DesktopEngine) InjectMouseEvent(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) {
	d.SendEvent(desktopEvent{kind: mouseEventKind, mx: x, my: y, buttons: buttons, mod: modifiers})
}

func (d *DesktopEngine) handleEvent(ev tcell.Event) {
	switch tev := ev.(type) {
	case *tcell.EventResize:
		d.recalculateLayout()
		return
	case *tcell.EventMouse:
		d.handleMouseEvent(tev)
		return
	}

	key, ok := ev.(*tcell.EventKey)
	if !ok {
		return
	}

	// Global Shortcuts
	if key.Key() == tcell.KeyF1 {
		d.launchHelpOverlay()
		return
	}

	// Check floating panels (topmost first)
	// Iterate in reverse to find topmost modal
	for i := len(d.floatingPanels) - 1; i >= 0; i-- {
		fp := d.floatingPanels[i]
		if fp.modal {
			// ESC closes modal floating panels
			if key.Key() == tcell.KeyEsc {
				d.CloseFloatingPanel(fp)
				return
			}
			// Route to pipeline (or app as fallback)
			if fp.pipeline != nil {
				fp.pipeline.HandleKey(key)
			} else {
				fp.app.HandleKey(key)
			}
			return
		}
	}

	if key.Key() == tcell.KeyCtrlF {
		d.launchConfigEditorOverlay(d.activeAppTarget())
		return
	}

	if key.Key() == keyControlMode {
		d.toggleControlMode()
		return
	}

	if d.inControlMode {
		d.handleControlMode(key)
		return
	}

	if d.zoomedPane != nil {
		if d.zoomedPane.Pane != nil {
			// Route to pipeline (or app as fallback)
			if d.zoomedPane.Pane.pipeline != nil {
				d.zoomedPane.Pane.pipeline.HandleKey(key)
			} else if d.zoomedPane.Pane.app != nil {
				d.zoomedPane.Pane.app.HandleKey(key)
			}
			d.zoomedPane.Pane.markDirty()
		}
	} else if d.activeWorkspace != nil {
		d.activeWorkspace.handleEvent(key)
	}
}

func (d *DesktopEngine) handleMouseEvent(ev *tcell.EventMouse) {
	if ev == nil {
		return
	}
	x, y := ev.Position()
	d.processMouseEvent(x, y, ev.Buttons(), ev.Modifiers())
}

func (d *DesktopEngine) processMouseEvent(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) {
	d.mouseMu.Lock()
	prevButtons := d.lastMouseButtons
	d.lastMouseX = x
	d.lastMouseY = y
	d.lastMouseButtons = buttons
	d.lastMouseModifier = modifiers
	d.mouseMu.Unlock()

	// Handle workspace border resize first
	if d.activeWorkspace != nil {
		if d.activeWorkspace.handleMouseResize(x, y, buttons, prevButtons) {
			return
		}
	}

	// Forward mouse events to the pane under cursor
	pane := d.paneAtCoordinates(x, y)
	if pane != nil && pane.handlesMouseEvents() {
		pane.handleMouse(x, y, buttons, modifiers)
	}

	// Activate pane on button press
	buttonPressed := buttons&tcell.Button1 != 0 && prevButtons&tcell.Button1 == 0
	if buttonPressed {
		d.activatePaneAt(x, y)
	}
}

func (d *DesktopEngine) paneAtCoordinates(x, y int) *pane {
	if d.zoomedPane != nil && d.zoomedPane.Pane != nil && d.zoomedPane.Pane.contains(x, y) {
		return d.zoomedPane.Pane
	}
	if d.activeWorkspace == nil {
		return nil
	}
	if node := d.activeWorkspace.nodeAt(x, y); node != nil {
		return node.Pane
	}
	return nil
}

func (d *DesktopEngine) activatePaneAt(x, y int) {
	if d.inControlMode {
		return
	}

	ws := d.activeWorkspace
	if d.zoomedPane != nil {
		if ws == nil {
			return
		}
		if d.zoomedPane.Pane != nil && d.zoomedPane.Pane.contains(x, y) {
			ws.activateLeaf(d.zoomedPane)
		}
		return
	}

	if ws == nil {
		return
	}

	if node := ws.nodeAt(x, y); node != nil {
		ws.activateLeaf(node)
	}
}

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

	// Alt+Left/Right: switch to previous/next workspace
	if key.Modifiers()&tcell.ModAlt != 0 {
		switch key.Key() {
		case tcell.KeyLeft:
			d.switchWorkspaceRelative(-1)
			return
		case tcell.KeyRight:
			d.switchWorkspaceRelative(1)
			return
		}
	}

	// Ctrl+Arrows: resize the nearest pane border by 1 character.
	// Ctrl+Right/Down: grow active pane (move right/bottom border outward)
	// Ctrl+Left/Up: shrink active pane (move right/bottom border inward)
	if key.Modifiers()&tcell.ModCtrl != 0 {
		switch key.Key() {
		case tcell.KeyLeft, tcell.KeyRight, tcell.KeyUp, tcell.KeyDown:
			if d.activeWorkspace != nil {
				dir := keyToDirection(key)
				// Always look for the border on the positive side first.
				// For grow (Right/Down): find right border, move it right.
				// For shrink (Left/Up): find right border, move it left.
				searchDir := dir
				if dir == DirLeft {
					searchDir = DirRight
				} else if dir == DirUp {
					searchDir = DirDown
				}
				border := d.activeWorkspace.findBorderToResize(searchDir)
				if border == nil {
					// No border on the positive side; try the other side.
					border = d.activeWorkspace.findBorderToResize(dir)
				}
				if border != nil {
					d.activeWorkspace.adjustBorder(border, dir)
					d.activeWorkspace.clearResizeSelection(border)
				}
			}
			return
		}
	}

	// If in control mode, route keys to control handler (which also manages
	// the help overlay). This must come before the floating panel check so
	// ESC exits control mode + closes the help overlay together.
	if d.inControlMode {
		d.handleControlMode(key)
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

	// Tab mode must yield to modal floating panels (launcher, help, config editor).
	if d.inTabMode && len(d.floatingPanels) == 0 {
		d.handleTabMode(key)
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

	// Check if click is in a status pane area
	if d.routeClickToStatusPane(x, y, buttons, prevButtons) {
		return
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

// StatusPaneClickHandler is implemented by status bar apps that handle clicks.
// The desktop calls this with the local coordinates within the status pane.
type StatusPaneClickHandler interface {
	HandleClick(localX, localY int)
}

// routeMouseToStatusPane checks if a mouse button-down falls within a status
// pane and forwards a click. Returns true if consumed.
func (d *DesktopEngine) routeClickToStatusPane(x, y int, buttons, prevButtons tcell.ButtonMask) bool {
	// Only handle initial button press
	buttonDown := buttons&tcell.Button1 != 0 && prevButtons&tcell.Button1 == 0
	if !buttonDown {
		return false
	}

	w, _ := d.viewportSize()
	offsetY := 0
	for _, sp := range d.statusPanes {
		switch sp.side {
		case SideTop:
			if x >= 0 && x < w && y >= offsetY && y < offsetY+sp.size {
				if handler, ok := sp.app.(StatusPaneClickHandler); ok {
					handler.HandleClick(x, y-offsetY)
					return true
				}
			}
			offsetY += sp.size
		}
	}
	return false
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

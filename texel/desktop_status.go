// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/desktop_status.go
// Summary: Status pane management and layout calculations for the desktop engine.

package texel

// Side defines the placement of a StatusPane.
type Side int

const (
	SideTop Side = iota
	SideBottom
	SideLeft
	SideRight
)

// StatusPane is a special pane with absolute sizing, placed on one side of the screen.
type StatusPane struct {
	app  App
	side Side
	size int // rows for Top/Bottom, cols for Left/Right
	id   [16]byte
}

// AddStatusPane adds a new status pane to the desktop.
func (d *DesktopEngine) AddStatusPane(app App, side Side, size int) {
	sp := &StatusPane{
		app:  app,
		side: side,
		size: size,
		id:   newStatusPaneID(app),
	}
	d.statusPanes = append(d.statusPanes, sp)

	if listener, ok := app.(Listener); ok {
		d.Subscribe(listener)
	}

	app.SetRefreshNotifier(d.makeRefreshNotifier())

	d.appLifecycle.StartApp(app, nil)
	d.recalculateLayout()
	d.broadcastTreeChanged()
}

func (d *DesktopEngine) getMainArea() (int, int, int, int) {
	w, h := d.viewportSize()
	mainX, mainY := 0, 0
	mainW, mainH := w, h

	topOffset, bottomOffset, leftOffset, rightOffset := 0, 0, 0, 0

	for _, sp := range d.statusPanes {
		switch sp.side {
		case SideTop:
			topOffset += sp.size
		case SideBottom:
			bottomOffset += sp.size
		case SideLeft:
			leftOffset += sp.size
		case SideRight:
			rightOffset += sp.size
		}
	}

	mainX = leftOffset
	mainY = topOffset
	mainW = w - leftOffset - rightOffset
	mainH = h - topOffset - bottomOffset
	return mainX, mainY, mainW, mainH
}

func (d *DesktopEngine) recalculateLayout() {
	w, h := d.viewportSize()
	mainX, mainY, mainW, mainH := d.getMainArea()

	for _, sp := range d.statusPanes {
		switch sp.side {
		case SideTop:
			sp.app.Resize(w, sp.size)
		case SideBottom:
			sp.app.Resize(w, sp.size)
		case SideLeft:
			sp.app.Resize(sp.size, h-mainY-(h-mainY-mainH))
		case SideRight:
			sp.app.Resize(sp.size, h-mainY-(h-mainY-mainH))
		}
	}

	// Resize floating panels if needed (e.g. ensure they fit?)
	// For now, leave them as requested.

	if d.zoomedPane != nil {
		if d.zoomedPane.Pane != nil {
			d.zoomedPane.Pane.setDimensions(mainX, mainY, mainX+mainW, mainY+mainH)
		}
	} else if d.activeWorkspace != nil {
		d.activeWorkspace.setArea(mainX, mainY, mainW, mainH)
	}
}

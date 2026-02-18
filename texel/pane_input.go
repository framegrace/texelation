// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/pane_input.go
// Summary: Input delegation and pane state management.

package texel

import (
	"log"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
)

func (p *pane) handlePaste(data []byte) {
	if p == nil || len(data) == 0 {
		return
	}
	// Try pipeline first for paste handling
	if p.pipeline != nil {
		if handler, ok := p.pipeline.(PasteHandler); ok {
			handler.HandlePaste(data)
			p.markDirty()
			return
		}
	}
	// Fallback to app
	if p.app != nil {
		if handler, ok := p.app.(PasteHandler); ok {
			handler.HandlePaste(data)
			p.markDirty()
			return
		}
	}
	// No paste handler - convert to key events
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			// treat invalid byte as literal
			r = rune(data[0])
		}
		data = data[size:]
		var ev *tcell.EventKey
		switch r {
		case '\r', '\n':
			ev = tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
		case '\t':
			ev = tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
		case '\x7f':
			ev = tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone)
		case 0x1b:
			ev = tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone)
		default:
			ev = tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone)
		}
		if p.pipeline != nil {
			p.pipeline.HandleKey(ev)
		} else if p.app != nil {
			p.app.HandleKey(ev)
		}
	}
	p.markDirty()
}

// handleMouse forwards a mouse event to the pane's app with local coordinates.
func (p *pane) handleMouse(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) {
	if !p.handlesMouseEvents() {
		return
	}
	localX, localY := p.contentLocalCoords(x, y)
	ev := tcell.NewEventMouse(localX, localY, buttons, modifiers)
	p.mouseHandler.HandleMouse(ev)
	// Mark dirty synchronously so the immediate Publish() in
	// DesktopSink.HandleMouseEvent sees updated state.
	p.markDirty()
}

func (p *pane) handlesMouseEvents() bool {
	return p != nil && p.handlesMouse && p.mouseHandler != nil
}

func (p *pane) contentLocalCoords(x, y int) (int, int) {
	innerX := x - (p.absX0 + 1)
	innerY := y - (p.absY0 + 1)
	innerWidth := p.drawableWidth()
	innerHeight := p.drawableHeight()
	if innerWidth <= 0 {
		innerX = 0
	} else {
		if innerX < 0 {
			innerX = 0
		} else if innerX >= innerWidth {
			innerX = innerWidth - 1
		}
	}
	if innerHeight <= 0 {
		innerY = 0
	} else {
		if innerY < 0 {
			innerY = 0
		} else if innerY >= innerHeight {
			innerY = innerHeight - 1
		}
	}
	return innerX, innerY
}

// SetActive changes the active state of the pane
func (p *pane) SetActive(active bool) {
	if p.IsActive == active {
		return
	}
	p.IsActive = active
	p.notifyStateChange()
	if p.screen != nil {
		p.screen.Refresh()
	}
}

// SetResizing changes the resizing state of the pane
func (p *pane) SetResizing(resizing bool) {
	if p.IsResizing == resizing {
		return
	}
	p.IsResizing = resizing
	p.notifyStateChange()
	if p.screen != nil {
		p.screen.Refresh()
	}
}

func (p *pane) notifyStateChange() {
	p.markDirty()
	if p.screen == nil || p.screen.desktop == nil {
		return
	}
	p.screen.desktop.notifyPaneState(p.ID(), p.IsActive, p.IsResizing, p.ZOrder, p.handlesMouse)
}

// SetZOrder sets the z-order (layering) of the pane
// Higher values render on top. Default is 0.
func (p *pane) SetZOrder(zOrder int) {
	if p.ZOrder == zOrder {
		return
	}
	p.ZOrder = zOrder
	log.Printf("SetZOrder: Pane '%s' z-order set to %d", p.getTitle(), zOrder)
	if p.screen != nil && p.screen.desktop != nil {
		p.screen.desktop.notifyPaneState(p.ID(), p.IsActive, p.IsResizing, p.ZOrder, p.handlesMouse)
	}
	if p.screen != nil {
		p.screen.Refresh() // Trigger redraw
	}
}

// GetZOrder returns the current z-order of the pane
func (p *pane) GetZOrder() int {
	return p.ZOrder
}

// BringToFront sets the pane to render on top of other panes
func (p *pane) BringToFront() {
	p.SetZOrder(ZOrderFloating)
}

// SendToBack resets the pane to normal z-order
func (p *pane) SendToBack() {
	p.SetZOrder(ZOrderDefault)
}

// SetAsDialog configures the pane as a modal dialog
func (p *pane) SetAsDialog() {
	p.SetZOrder(ZOrderDialog)
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/pane.go
// Summary: Implements pane capabilities for the core desktop engine.
// Usage: Used throughout the project to implement pane inside the desktop and panes.
// Notes: Legacy desktop logic migrated from the monolithic application.

// texel/pane_v2.go
package texel

import (
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"log"
	"texelation/texel/theme"
	"unicode/utf8"
)

// Z-order constants for common layering scenarios
const (
	ZOrderDefault   = 0    // Normal panes
	ZOrderFloating  = 100  // Floating windows
	ZOrderDialog    = 500  // Modal dialogs
	ZOrderAnimation = 1000 // Reserved for future use (visual effects, etc.)
	ZOrderTooltip   = 2000 // Tooltips and temporary overlays
)

// Pane represents a rectangular area on the screen that hosts an App.
type pane struct {
	absX0, absY0, absX1, absY1 int
	app     App
	name    string
	prevBuf [][]Cell
	screen  *Screen
	id      [16]byte

	// Public state fields
	IsActive   bool
	IsResizing bool
	ZOrder     int // Higher values render on top, default is 0
}

// newPane creates a new, empty Pane. The App is attached later.
func newPane(s *Screen) *pane {
	p := &pane{
		screen:     s,
		IsActive:   false,
		IsResizing: false,
	}
	if _, err := rand.Read(p.id[:]); err != nil {
		sum := sha1.Sum([]byte(fmt.Sprintf("%p", p)))
		copy(p.id[:], sum[:])
	}
	return p
}

// AttachApp connects an application to the pane, gives it its initial size,
// and starts its main run loop.
func (p *pane) AttachApp(app App, refreshChan chan<- bool) {
	if p.app != nil {
		p.screen.appLifecycle.StopApp(p.app)
	}
	p.app = app
	p.name = app.GetTitle()
	p.app.SetRefreshNotifier(refreshChan)
	if listener, ok := app.(Listener); ok {
		p.screen.Subscribe(listener)
	}
	// The app is resized considering the space for borders.
	p.app.Resize(p.drawableWidth(), p.drawableHeight())
	p.screen.appLifecycle.StartApp(p.app)
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
	if p.screen == nil || p.screen.desktop == nil {
		return
	}
	p.screen.desktop.notifyPaneState(p.ID(), p.IsActive, p.IsResizing, p.ZOrder)
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
		p.screen.desktop.notifyPaneState(p.ID(), p.IsActive, p.IsResizing, p.ZOrder)
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

// Render draws the pane's borders, title, and the hosted application's content including effects.
func (p *pane) Render() [][]Cell {
	return p.renderBuffer(true)
}

func (p *pane) handlePaste(data []byte) {
	if p == nil || p.app == nil || len(data) == 0 {
		return
	}
	if handler, ok := p.app.(PasteHandler); ok {
		handler.HandlePaste(data)
		return
	}
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
		case '':
			ev = tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone)
		case 0x1b:
			ev = tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone)
		default:
			ev = tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone)
		}
		p.app.HandleKey(ev)
	}
}

func (p *pane) renderBuffer(applyEffects bool) [][]Cell {
	w := p.Width()
	h := p.Height()

	log.Printf("Render: Pane '%s' rendering %dx%d (abs: %d,%d-%d,%d)",
		p.getTitle(), w, h, p.absX0, p.absY0, p.absX1, p.absY1)

	tm := theme.Get()
	desktopBg := tm.GetColor("desktop", "default_bg", tcell.ColorReset).TrueColor()
	desktopFg := tm.GetColor("desktop", "default_fg", tcell.ColorReset).TrueColor()
	defstyle := tcell.StyleDefault.Background(desktopBg).Foreground(desktopFg)

	// Create the pane's buffer.
	buffer := make([][]Cell, h)
	for i := range buffer {
		buffer[i] = make([]Cell, w)
		for j := range buffer[i] {
			buffer[i][j] = Cell{Ch: ' ', Style: defstyle}
		}
	}

	// Don't draw decorations if the pane is too small.
	if w < 2 || h < 2 {
		log.Printf("Render: Pane '%s' too small to draw decorations (%dx%d)", p.getTitle(), w, h)
		return buffer
	}

	// Determine border style based on active state.
	inactiveBorderFG := tm.GetColor("pane", "inactive_border_fg", tcell.ColorPink).TrueColor()
	inactiveBorderBG := tm.GetColor("pane", "inactive_border_bg", desktopBg).TrueColor()
	activeBorderFG := tm.GetColor("pane", "active_border_fg", tcell.ColorPink).TrueColor()
	activeBorderBG := tm.GetColor("pane", "active_border_bg", desktopBg).TrueColor()
	resizingBorderFG := tm.GetColor("pane", "resizing_border_fg", tcell.ColorPink).TrueColor()

	borderStyle := defstyle.Foreground(inactiveBorderFG).Background(inactiveBorderBG)
	if p.IsActive {
		borderStyle = defstyle.Foreground(activeBorderFG).Background(activeBorderBG)
	}
	if p.IsResizing {
		borderStyle = defstyle.Foreground(resizingBorderFG).Background(activeBorderBG)
	}

	// Draw borders
	for x := 0; x < w; x++ {
		buffer[0][x] = Cell{Ch: tcell.RuneHLine, Style: borderStyle}
		buffer[h-1][x] = Cell{Ch: tcell.RuneHLine, Style: borderStyle}
	}
	for y := 0; y < h; y++ {
		buffer[y][0] = Cell{Ch: tcell.RuneVLine, Style: borderStyle}
		buffer[y][w-1] = Cell{Ch: tcell.RuneVLine, Style: borderStyle}
	}
	buffer[0][0] = Cell{Ch: tcell.RuneULCorner, Style: borderStyle}
	buffer[0][w-1] = Cell{Ch: tcell.RuneURCorner, Style: borderStyle}
	buffer[h-1][0] = Cell{Ch: tcell.RuneLLCorner, Style: borderStyle}
	buffer[h-1][w-1] = Cell{Ch: '╯', Style: borderStyle}

	// Draw Title - with proper bounds checking
	title := p.getTitle()
	if title != "" && w > 4 { // Only draw title if we have enough space
		titleRuneCount := utf8.RuneCountInString(title)
		maxTitleLength := w - 4 // Space for " " + title + " " + borders

		// Truncate title if it's too long for the pane width.
		if titleRuneCount > maxTitleLength && maxTitleLength > 0 {
			titleRunes := []rune(title)
			if maxTitleLength <= len(titleRunes) {
				title = string(titleRunes[:maxTitleLength])
			}
		}

		titleStr := " " + title + " "
		for i, ch := range titleStr {
			if 1+i < w-1 { // Ensure we don't go beyond borders
				buffer[0][1+i] = Cell{Ch: ch, Style: borderStyle}
			}
		}
	}

	// Render the app's content inside the borders.
	if p.app != nil {
		appBuffer := p.app.Render()
		if len(appBuffer) > 0 && len(appBuffer[0]) > 0 {
			log.Printf("Render: Pane '%s' app buffer size: %dx%d",
				p.getTitle(), len(appBuffer[0]), len(appBuffer))

			for y, row := range appBuffer {
				for x, cell := range row {
					if 1+x < w-1 && 1+y < h-1 {
						buffer[1+y][1+x] = cell
					}
				}
			}
		}
	} else {
		log.Printf("Render: Pane '%s' has no app!", p.getTitle())
	}

	log.Printf("Render: Pane '%s' final buffer size: %dx%d", p.getTitle(), len(buffer), len(buffer[0]))
	return buffer
}

func (p *pane) ID() [16]byte {
	return p.id
}

func (p *pane) setID(id [16]byte) {
	p.id = id
}

// Rest of the methods remain the same...
func (p *pane) drawableWidth() int {
	w := p.Width() - 2
	if w < 0 {
		return 0
	}
	return w
}

func (p *pane) drawableHeight() int {
	h := p.Height() - 2
	if h < 0 {
		return 0
	}
	return h
}

func (p *pane) String() string {
	return p.name
}

func (p *pane) setTitle(t string) {
	p.name = t
}

func (p *pane) getTitle() string {
	if p.app != nil {
		return p.app.GetTitle()
	}
	return p.name
}

func (p *pane) Close() {
	// Clean up app
	if listener, ok := p.app.(Listener); ok {
		p.screen.Unsubscribe(listener)
	}
	if p.app != nil {
		p.screen.appLifecycle.StopApp(p.app)
	}
}

func (p *pane) Width() int {
	w := p.absX1 - p.absX0
	if w < 0 {
		return 0
	}
	return w
}

func (p *pane) Height() int {
	h := p.absY1 - p.absY0
	if h < 0 {
		return 0
	}
	return h
}

func (p *pane) setDimensions(x0, y0, x1, y1 int) {
	log.Printf("setDimensions: Pane '%s' set to (%d,%d)-(%d,%d), size %dx%d",
		p.getTitle(), x0, y0, x1, y1, x1-x0, y1-y0)

	p.absX0, p.absY0, p.absX1, p.absY1 = x0, y0, x1, y1

	if p.app != nil {
		drawableW := p.drawableWidth()
		drawableH := p.drawableHeight()
		log.Printf("setDimensions: Pane '%s' drawable area: %dx%d",
			p.getTitle(), drawableW, drawableH)
		p.app.Resize(drawableW, drawableH)
	}
}

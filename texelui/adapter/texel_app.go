// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/adapter/texel_app.go
// Summary: Adapts TexelUI UIManager to the texel.App interface.
// Usage: Wrap a UIManager in UIApp to use it as a texel.App.

package adapter

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel"
	"texelation/texelui/core"
	"texelation/texelui/widgets"
)

// UIApp adapts a TexelUI UIManager to the texel.App interface.
type UIApp struct {
	title    string
	ui       *core.UIManager
	stopCh   chan struct{}
	refresh  chan<- bool
	onResize func(w, h int)
}

// NewUIApp creates a new UIApp wrapping the given UIManager.
// If ui is nil, a new UIManager is created.
func NewUIApp(title string, ui *core.UIManager) *UIApp {
	if ui == nil {
		ui = core.NewUIManager()
	}
	app := &UIApp{title: title, ui: ui, stopCh: make(chan struct{})}
	app.EnableStatusBar() // Enable status bar by default
	return app
}

func (a *UIApp) Run() error { <-a.stopCh; return nil }

func (a *UIApp) Stop() {
	select {
	case <-a.stopCh:
	default:
		close(a.stopCh)
	}
}

func (a *UIApp) Resize(cols, rows int) {
	a.ui.Resize(cols, rows)
	if a.onResize != nil {
		a.onResize(cols, rows)
	}
}

func (a *UIApp) Render() [][]texel.Cell { return a.ui.Render() }

func (a *UIApp) GetTitle() string {
	if a.title == "" {
		return "TexelUI"
	}
	return a.title
}

func (a *UIApp) HandleKey(ev *tcell.EventKey) { a.ui.HandleKey(ev) }

func (a *UIApp) HandleMouse(ev *tcell.EventMouse) { a.ui.HandleMouse(ev) }

func (a *UIApp) SetRefreshNotifier(ch chan<- bool) { a.refresh = ch; a.ui.SetRefreshNotifier(ch) }

// UI returns the underlying UIManager for composition.
func (a *UIApp) UI() *core.UIManager { return a.ui }

// SetOnResize sets a callback to be invoked after each Resize call.
func (a *UIApp) SetOnResize(fn func(w, h int)) { a.onResize = fn }

// EnableStatusBar creates and enables a status bar.
// Returns the status bar widget for message display.
// The status bar displays key hints (left) and timed messages (right).
func (a *UIApp) EnableStatusBar() *widgets.StatusBar {
	sb := widgets.NewStatusBar(0, 0, 80)
	a.ui.SetStatusBar(sb)
	return sb
}

// StatusBar returns the current status bar widget, or nil if none.
func (a *UIApp) StatusBar() *widgets.StatusBar {
	if sbw := a.ui.StatusBar(); sbw != nil {
		if sb, ok := sbw.(*widgets.StatusBar); ok {
			return sb
		}
	}
	return nil
}

// DisableStatusBar removes and disables the status bar.
func (a *UIApp) DisableStatusBar() {
	a.ui.SetStatusBar(nil)
}

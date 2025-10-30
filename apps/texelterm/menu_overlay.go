// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/menu_overlay.go
// Summary: Experimental transparent tview menu overlay for texelterm (Ctrl-G toggle).

package texelterm

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"texelation/internal/tviewbridge"
	"texelation/texel"
)

type menuEntry struct {
	Title    string
	Subtitle string
	Shortcut rune
}

var overlayMenuEntries = []menuEntry{
	{Title: "Copy", Subtitle: "Emit Ctrl+Shift+C (not implemented)", Shortcut: 'c'},
	{Title: "Paste", Subtitle: "Emit Ctrl+Shift+V (not implemented)", Shortcut: 'v'},
	{Title: "Settings", Subtitle: "Show terminal settings (stub)", Shortcut: 's'},
	{Title: "Close Menu", Subtitle: "Hide overlay", Shortcut: 'q'},
}

type menuOverlayTerm struct {
	base        *TexelTerm
	menu        *tviewbridge.TViewApp
	menuList    *tview.List
	showMenu    bool
	width       int
	height      int
	refreshChan chan<- bool
}

func newMenuOverlay(base *TexelTerm) texel.App {
	return &menuOverlayTerm{
		base:   base,
		width:  base.width,
		height: base.height,
	}
}

func (m *menuOverlayTerm) ensureMenu() error {
	if m.menu != nil {
		return nil
	}
	root, list := createMenuPrimitive(overlayMenuEntries)
	app := tviewbridge.NewTViewApp(m.base.GetTitle()+" menu", root)
	app.Resize(m.width, m.height)
	if m.refreshChan != nil {
		app.SetRefreshNotifier(m.refreshChan)
	}
	if err := app.Run(); err != nil {
		return err
	}
	m.menu = app
	m.menuList = list
	list.SetSelectedFunc(func(index int, main, secondary string, shortcut rune) {
		m.handleMenuSelection(index)
	})
	list.SetDoneFunc(func() {
		m.hideMenu()
	})
	return nil
}

func createMenuPrimitive(entries []menuEntry) (tview.Primitive, *tview.List) {
	list := tview.NewList().
		ShowSecondaryText(true).
		SetWrapAround(true)
	list.SetBorder(false)
	list.SetBackgroundColor(tcell.ColorDefault)
	list.SetMainTextColor(tcell.ColorWhite)
	list.SetSecondaryTextColor(tcell.ColorLightGrey)
	list.SetSelectedTextColor(tcell.ColorBlack)
	list.SetSelectedBackgroundColor(tcell.ColorDarkBlue)

	for _, entry := range entries {
		list.AddItem(entry.Title, entry.Subtitle, entry.Shortcut, nil)
	}

	container := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(list, len(entries)+1, 0, false).
		AddItem(nil, 0, 1, false)
	container.SetBorder(false)
	container.SetBackgroundColor(tcell.ColorDefault)
	return container, list
}

func (m *menuOverlayTerm) Run() error {
	if err := m.ensureMenu(); err != nil {
		return err
	}
	return m.base.Run()
}

func (m *menuOverlayTerm) Stop() {
	m.base.Stop()
	if m.menu != nil {
		m.menu.Stop()
		m.menu = nil
	}
}

func (m *menuOverlayTerm) Resize(cols, rows int) {
	m.width, m.height = cols, rows
	m.base.Resize(cols, rows)
	if m.menu != nil {
		m.menu.Resize(cols, rows)
	}
}

func (m *menuOverlayTerm) Render() [][]texel.Cell {
	base := m.base.Render()
	if !m.showMenu || m.menu == nil {
		return base
	}
	return overlayBuffers(base, m.menu.Render())
}

func (m *menuOverlayTerm) GetTitle() string {
	return m.base.GetTitle()
}

func (m *menuOverlayTerm) HandleKey(ev *tcell.EventKey) {
	if ev.Key() == tcell.KeyCtrlG {
		m.toggleMenu()
		return
	}
	if m.showMenu && m.menu != nil {
		m.menu.HandleKey(ev)
		return
	}
	m.base.HandleKey(ev)
}

func (m *menuOverlayTerm) SetRefreshNotifier(ch chan<- bool) {
	m.refreshChan = ch
	m.base.SetRefreshNotifier(ch)
	if m.menu != nil {
		m.menu.SetRefreshNotifier(ch)
	}
}

func (m *menuOverlayTerm) HandlePaste(data []byte) {
	m.base.HandlePaste(data)
}

func (m *menuOverlayTerm) HandleMessage(msg texel.Message) {
	m.base.HandleMessage(msg)
}

func (m *menuOverlayTerm) requestRefresh() {
	if m.refreshChan == nil {
		return
	}
	select {
	case m.refreshChan <- true:
	default:
	}
}

func overlayBuffers(base, overlay [][]texel.Cell) [][]texel.Cell {
	if len(base) == 0 {
		return base
	}
	defaultBg, _, _ := tcell.StyleDefault.Decompose()
	for y := 0; y < len(overlay) && y < len(base); y++ {
		for x := 0; x < len(overlay[y]) && x < len(base[y]); x++ {
			cell := overlay[y][x]
			_, bg, _ := cell.Style.Decompose()
			if cell.Ch == ' ' && bg == defaultBg {
				continue
			}
			base[y][x] = cell
		}
	}
	return base
}

func (m *menuOverlayTerm) toggleMenu() {
	m.showMenu = !m.showMenu
	if m.showMenu {
		m.ensureMenu()
		if m.menuList != nil {
			m.menuList.SetCurrentItem(0)
		}
		if app := m.menu.GetApplication(); app != nil && m.menuList != nil {
			app.SetFocus(m.menuList)
		}
	} else {
		if app := m.menu.GetApplication(); app != nil {
			app.SetFocus(nil)
		}
	}
	m.requestRefresh()
}

func (m *menuOverlayTerm) hideMenu() {
	if !m.showMenu {
		return
	}
	m.showMenu = false
	if app := m.menu.GetApplication(); app != nil {
		app.SetFocus(nil)
	}
	m.requestRefresh()
}

func (m *menuOverlayTerm) handleMenuSelection(index int) {
	switch index {
	case 0:
		// TODO: hook into copy action.
	case 1:
		// TODO: hook into paste action.
	case 2:
		// TODO: open settings pane.
	case 3:
		// Close menu.
	}
	m.hideMenu()
}

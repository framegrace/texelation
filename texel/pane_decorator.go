// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/pane_decorator.go
// Summary: Implements the pane decorator pill — a compact action bar overlaid
// on the pane border that hosts app-specific and WM actions.

package texel

import (
	"sync"

	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/theme"
	"github.com/gdamore/tcell/v2"
)

// Decorator pill rune constants.
const (
	decorPillLeftCap  = '\uE0B6' // Powerline rounded left cap
	decorPillRightCap = '\uE0B4' // Powerline rounded right cap
	decorHamburger    = '\u2261' // ≡ triple bar
	decorSep          = '│'      // vertical separator between zones
)

// DecoratorAction describes a single clickable icon in the decorator pill.
type DecoratorAction struct {
	ID       string  // unique within its zone (app or WM)
	Icon     rune    // the glyph displayed
	Help     string  // tooltip / status line text
	Active   bool    // highlighted with border.active color
	Disabled bool    // shown with text.muted, OnClick not called
	OnClick  func()  // called on left-click; may be nil
}

// PaneDecorator manages a pair of action zones — app actions (left) and WM
// actions (right) — and renders them as a floating pill on the pane border.
type PaneDecorator struct {
	mu             sync.RWMutex
	appActions     []DecoratorAction
	wmActions      []DecoratorAction
	alwaysExpanded bool
	expanded       bool
}

// NewPaneDecorator allocates and returns a new PaneDecorator.
// If alwaysExpanded is true the pill never collapses.
func NewPaneDecorator(alwaysExpanded bool) *PaneDecorator {
	return &PaneDecorator{alwaysExpanded: alwaysExpanded}
}

// AddAppAction appends or replaces an app action by ID.
func (d *PaneDecorator) AddAppAction(a DecoratorAction) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, existing := range d.appActions {
		if existing.ID == a.ID {
			d.appActions[i] = a
			return
		}
	}
	d.appActions = append(d.appActions, a)
}

// RemoveAppAction removes an app action by ID; no-op if not found.
func (d *PaneDecorator) RemoveAppAction(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, a := range d.appActions {
		if a.ID == id {
			d.appActions = append(d.appActions[:i], d.appActions[i+1:]...)
			return
		}
	}
}

// UpdateAppAction updates the Active and Disabled fields of an existing app
// action identified by ID.  Other fields are left unchanged.
func (d *PaneDecorator) UpdateAppAction(a DecoratorAction) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, existing := range d.appActions {
		if existing.ID == a.ID {
			d.appActions[i].Active = a.Active
			d.appActions[i].Disabled = a.Disabled
			return
		}
	}
}

// AddWMAction appends or replaces a WM action by ID.
func (d *PaneDecorator) AddWMAction(a DecoratorAction) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, existing := range d.wmActions {
		if existing.ID == a.ID {
			d.wmActions[i] = a
			return
		}
	}
	d.wmActions = append(d.wmActions, a)
}

// UpdateWMAction updates the Active and Disabled fields of an existing WM
// action identified by ID.
func (d *PaneDecorator) UpdateWMAction(a DecoratorAction) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, existing := range d.wmActions {
		if existing.ID == a.ID {
			d.wmActions[i].Active = a.Active
			d.wmActions[i].Disabled = a.Disabled
			if a.Icon != 0 {
				d.wmActions[i].Icon = a.Icon
			}
			return
		}
	}
}

// HasActions reports whether there are any app or WM actions registered.
func (d *PaneDecorator) HasActions() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.appActions) > 0 || len(d.wmActions) > 0
}

// IsExpanded reports whether the pill is currently in expanded state.
func (d *PaneDecorator) IsExpanded() bool {
	if d.alwaysExpanded {
		return true
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.expanded
}

// SetExpanded manually sets the expanded state.  Ignored when alwaysExpanded.
func (d *PaneDecorator) SetExpanded(v bool) {
	if d.alwaysExpanded {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.expanded = v
}

// allActions returns copies of both slices under RLock.
func (d *PaneDecorator) allActions() (app []DecoratorAction, wm []DecoratorAction) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	app = make([]DecoratorAction, len(d.appActions))
	copy(app, d.appActions)
	wm = make([]DecoratorAction, len(d.wmActions))
	copy(wm, d.wmActions)
	return
}

// PillWidth returns the pill's rendered width in terminal columns.
// Collapsed: 3 (leftCap + hamburger + rightCap).
// Expanded: 2 caps + 3 cols per app action + separator (if both zones non-empty) + 3 cols per WM action.
func (d *PaneDecorator) PillWidth() int {
	if !d.IsExpanded() {
		return 3
	}
	app, wm := d.allActions()
	w := 2 // caps
	w += len(app) * 3
	if len(app) > 0 && len(wm) > 0 {
		w++ // separator
	}
	w += len(wm) * 3
	return w
}

// PillRect returns the rectangle (in absolute screen coordinates) that the
// pill occupies.  borderX0 and borderX1 are the left/right edges of the pane
// border; borderY is the row of the top border.
func (d *PaneDecorator) PillRect(borderX0, borderX1, borderY int) texelcore.Rect {
	pw := d.PillWidth()
	// Right-align the pill inside the top border, leaving 1 cell gap from the right corner.
	x := borderX1 - pw - 1
	if x < borderX0+1 {
		x = borderX0 + 1
	}
	return texelcore.Rect{X: x, Y: borderY, W: pw, H: 1}
}

// Draw renders the decorator pill into buf at the position determined by the
// border geometry.  borderStyle is the tcell.Style used for the border so that
// cap colors can be derived from it (Powerline trick: cap FG = border FG, cap
// BG = border BG).
func (d *PaneDecorator) Draw(buf [][]Cell, borderX0, borderX1, borderY int, borderStyle tcell.Style) {
	if borderY < 0 || borderY >= len(buf) {
		return
	}
	row := buf[borderY]

	tm := theme.Get()
	mutedFG := tm.GetSemanticColor("text.muted").TrueColor()
	activeBG := tm.GetSemanticColor("border.active").TrueColor()

	// Derive cap colors from the border style.
	borderFG, borderBG, _ := borderStyle.Decompose()

	// Cap style: border FG as foreground (glyph color), border BG as background.
	// The pill interior uses the reverse: border FG as background, border BG as foreground.
	pillBG := borderFG
	pillTextFG := borderBG

	capStyle := tcell.StyleDefault.Foreground(borderFG).Background(borderBG)
	innerBase := tcell.StyleDefault.Foreground(pillTextFG).Background(pillBG)
	mutedStyle := tcell.StyleDefault.Foreground(mutedFG).Background(pillBG)
	// Active: bright background with contrasting foreground for visibility.
	activeStyle := tcell.StyleDefault.Foreground(borderBG).Background(activeBG)

	rect := d.PillRect(borderX0, borderX1, borderY)
	x := rect.X

	putCell := func(col int, ch rune, style tcell.Style) {
		if col >= 0 && col < len(row) {
			row[col] = Cell{Ch: ch, Style: style}
		}
	}

	if !d.IsExpanded() {
		// Collapsed: leftCap + hamburger + rightCap
		putCell(x, decorPillLeftCap, capStyle)
		putCell(x+1, decorHamburger, mutedStyle)
		putCell(x+2, decorPillRightCap, capStyle)
		return
	}

	app, wm := d.allActions()

	// Left cap
	putCell(x, decorPillLeftCap, capStyle)
	x++

	// App actions
	for _, a := range app {
		var iconStyle tcell.Style
		switch {
		case a.Disabled:
			iconStyle = mutedStyle
		case a.Active:
			iconStyle = activeStyle
		default:
			iconStyle = innerBase
		}
		putCell(x, ' ', innerBase)
		putCell(x+1, a.Icon, iconStyle)
		putCell(x+2, ' ', innerBase)
		x += 3
	}

	// Separator between zones (only when both are non-empty)
	if len(app) > 0 && len(wm) > 0 {
		putCell(x, decorSep, mutedStyle)
		x++
	}

	// WM actions
	for _, a := range wm {
		var iconStyle tcell.Style
		switch {
		case a.Disabled:
			iconStyle = mutedStyle
		case a.Active:
			iconStyle = activeStyle
		default:
			iconStyle = innerBase
		}
		putCell(x, ' ', innerBase)
		putCell(x+1, a.Icon, iconStyle)
		putCell(x+2, ' ', innerBase)
		x += 3
	}

	// Right cap
	putCell(x, decorPillRightCap, capStyle)
}

// HandleMouse processes a mouse event and returns the help text of the hovered
// action (if any) and whether the event was consumed.
//
// The method uses the expanded pill rect for hover detection so that hovering
// the zone where the pill WOULD expand is enough to trigger expansion.
func (d *PaneDecorator) HandleMouse(absX, absY int, buttons tcell.ButtonMask, borderX0, borderX1, borderY int) (help string, consumed bool) {
	// Compute the expanded-state rect to determine the hover zone.
	expandedWidth := d.expandedWidth()
	borderFG := tcell.ColorDefault // placeholder — width only depends on actions
	_ = borderFG

	// Build expanded rect (right-aligned, same formula as PillRect but forced expanded).
	pillX := borderX1 - expandedWidth - 1
	if pillX < borderX0+1 {
		pillX = borderX0 + 1
	}
	hoverRect := texelcore.Rect{X: pillX, Y: borderY, W: expandedWidth, H: 1}

	inZone := hoverRect.Contains(absX, absY)

	if !inZone {
		// Outside pill zone: collapse if currently expanded.
		if d.IsExpanded() && !d.alwaysExpanded {
			d.mu.Lock()
			d.expanded = false
			d.mu.Unlock()
		}
		return "", false
	}

	// Inside zone but pill is collapsed: expand and consume.
	if !d.IsExpanded() {
		if !d.alwaysExpanded {
			d.mu.Lock()
			d.expanded = true
			d.mu.Unlock()
		}
		return "", true
	}

	// Pill is expanded — find which action the cursor is on.
	app, wm := d.allActions()

	// Walk through the expanded layout to map absX → action.
	col := pillX + 1 // skip left cap

	// App actions: each is 3 wide (space icon space).
	for i, a := range app {
		actionX := col + i*3
		if absX >= actionX && absX < actionX+3 {
			help = a.Help
			if buttons&tcell.Button1 != 0 && !a.Disabled && a.OnClick != nil {
				a.OnClick()
			}
			return help, true
		}
	}
	col += len(app) * 3

	// Separator (1 wide if both zones non-empty).
	if len(app) > 0 && len(wm) > 0 {
		if absX == col {
			return "", true // consumed but no action
		}
		col++
	}

	// WM actions.
	for i, a := range wm {
		actionX := col + i*3
		if absX >= actionX && absX < actionX+3 {
			help = a.Help
			if buttons&tcell.Button1 != 0 && !a.Disabled && a.OnClick != nil {
				a.OnClick()
			}
			return help, true
		}
	}

	// On the right cap or any remaining cell — still consumed.
	return "", true
}

// expandedWidth returns the pill width as if it were expanded.
// Used by HandleMouse to determine the hover zone regardless of current state.
func (d *PaneDecorator) expandedWidth() int {
	app, wm := d.allActions()
	w := 2 // caps
	w += len(app) * 3
	if len(app) > 0 && len(wm) > 0 {
		w++ // separator
	}
	w += len(wm) * 3
	// Minimum expanded width equals collapsed width.
	if w < 3 {
		w = 3
	}
	return w
}

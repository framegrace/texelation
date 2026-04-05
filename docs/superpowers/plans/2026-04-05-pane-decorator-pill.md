# Pane Decorator Pill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the toggle pill from inside texelterm to the pane border as a generic texelation feature. Apps register actions via ControlBus. Texelation adds its own window-manager actions (zoom toggle). The pill uses zero terminal space.

**Architecture:** A `PaneDecorator` on each pane manages two zones of actions (app-left, WM-right) drawn on the top border row. Apps send `decorator.add`/`decorator.remove`/`decorator.update` ControlBus messages. The pane renders the pill after the border, handles hover expand/collapse and click forwarding. TexelTerm's internal toggle overlay is removed and replaced with ControlBus registrations.

**Tech Stack:** Go, tcell, texelui widgets (Border, ToggleButton), ControlBus

---

## File Structure

| File | Responsibility |
|------|---------------|
| Create: `texel/pane_decorator.go` | DecoratorAction type, PaneDecorator struct, draw/mouse logic |
| Create: `texel/pane_decorator_test.go` | Unit tests for decorator draw, collapse, ControlBus |
| Modify: `texel/pane.go` | Add decorator field to pane, wire ControlBus handlers |
| Modify: `texel/pane_render.go` | Call decorator draw after border draw |
| Modify: `texel/pane_input.go` | Route border mouse events to decorator |
| Modify: `texel/desktop_engine_core.go` | Add zoom toggle as WM decorator action |
| Delete: `apps/texelterm/toggle_overlay.go` | Remove internal pill rendering |
| Modify: `apps/texelterm/term.go` | Remove overlay fields, register actions via ControlBus |
| Modify: `apps/texelterm/term_statusbar_test.go` | Update tests for removed overlay |

---

### Task 1: DecoratorAction type and PaneDecorator struct

**Files:**
- Create: `texel/pane_decorator.go`

- [ ] **Step 1: Create the types and core logic**

```go
// texel/pane_decorator.go
package texel

import (
	"sync"

	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/theme"
	"github.com/gdamore/tcell/v2"
)

// Powerline rounded cap characters for pill shape.
const (
	decorPillLeftCap  = '\uE0B6'
	decorPillRightCap = '\uE0B4'
	decorHamburger    = '\u2261' // ≡
	decorSep          = '│'
)

// DecoratorAction describes an action button on the pane border pill.
type DecoratorAction struct {
	ID       string
	Icon     rune
	Help     string
	Active   bool
	Disabled bool
	OnClick  func()
}

// PaneDecorator manages the pill-shaped action bar on a pane's top border.
type PaneDecorator struct {
	mu          sync.RWMutex
	appActions  []DecoratorAction // left zone: app-registered actions
	wmActions   []DecoratorAction // right zone: texelation window-manager actions
	expanded    bool              // true when mouse is hovering
	alwaysExpanded bool           // config: never collapse to hamburger
}

// NewPaneDecorator creates a decorator with the given config.
func NewPaneDecorator(alwaysExpanded bool) *PaneDecorator {
	return &PaneDecorator{
		alwaysExpanded: alwaysExpanded,
	}
}

// AddAppAction adds or updates an app action by ID. Order preserved (first add = leftmost).
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

// RemoveAppAction removes an app action by ID.
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

// UpdateAppAction updates Active/Disabled state of an app action by ID.
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

// AddWMAction adds or updates a window-manager action by ID.
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

// UpdateWMAction updates Active/Disabled state of a WM action by ID.
func (d *PaneDecorator) UpdateWMAction(a DecoratorAction) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, existing := range d.wmActions {
		if existing.ID == a.ID {
			d.wmActions[i].Active = a.Active
			d.wmActions[i].Disabled = a.Disabled
			return
		}
	}
}

// allActions returns app actions + separator + WM actions for rendering.
func (d *PaneDecorator) allActions() (app []DecoratorAction, wm []DecoratorAction) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.appActions, d.wmActions
}

// HasActions returns true if any actions are registered.
func (d *PaneDecorator) HasActions() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.appActions) > 0 || len(d.wmActions) > 0
}

// IsExpanded returns whether the pill is currently expanded.
func (d *PaneDecorator) IsExpanded() bool {
	return d.alwaysExpanded || d.expanded
}

// SetExpanded sets the hover-expand state.
func (d *PaneDecorator) SetExpanded(v bool) {
	if !d.alwaysExpanded {
		d.expanded = v
	}
}

// collapsedWidth returns the width of the collapsed pill.
func collapsedWidth() int {
	return 3 // left cap + hamburger + right cap
}

// expandedWidth returns the width of the expanded pill.
func (d *PaneDecorator) expandedWidth() int {
	app, wm := d.allActions()
	w := 2 // caps
	w += len(app) * 3
	if len(app) > 0 && len(wm) > 0 {
		w += 1 // separator
	}
	w += len(wm) * 3
	return w
}

// PillWidth returns the current pill width based on state.
func (d *PaneDecorator) PillWidth() int {
	if !d.HasActions() {
		return 0
	}
	if d.IsExpanded() {
		return d.expandedWidth()
	}
	return collapsedWidth()
}

// PillRect returns the bounding rect for the pill on the border.
// borderX1 is the right edge of the border (exclusive).
func (d *PaneDecorator) PillRect(borderX0, borderX1, borderY int) texelcore.Rect {
	w := d.PillWidth()
	if w == 0 {
		return texelcore.Rect{}
	}
	x := borderX1 - w - 1 // -1 to leave room for border corner
	return texelcore.Rect{X: x, Y: borderY, W: w, H: 1}
}

// Draw renders the pill on the given buffer at the border's top row.
func (d *PaneDecorator) Draw(buf [][]Cell, borderX0, borderX1, borderY int, borderStyle tcell.Style) {
	if !d.HasActions() || len(buf) == 0 {
		return
	}

	rect := d.PillRect(borderX0, borderX1, borderY)
	if rect.W == 0 || rect.X < borderX0+1 {
		return // too narrow to fit
	}

	tm := theme.Get()
	borderFG, borderBG, _ := borderStyle.Decompose()
	activeFG := tm.GetSemanticColor("border.active")
	mutedFG := tm.GetSemanticColor("text.muted")
	if activeFG == tcell.ColorDefault {
		activeFG = borderFG
	}
	if mutedFG == tcell.ColorDefault {
		mutedFG = borderFG
	}

	// Cap style: pill BG (= border FG) as foreground, border BG as background
	capStyle := tcell.StyleDefault.Foreground(borderFG).Background(borderBG)
	// Normal action style: same colors as border
	normalStyle := tcell.StyleDefault.Foreground(borderFG).Background(borderBG)
	// Active action: highlighted
	activeStyle := tcell.StyleDefault.Foreground(activeFG).Background(borderBG)
	// Disabled action: muted
	disabledStyle := tcell.StyleDefault.Foreground(mutedFG).Background(borderBG)

	y := rect.Y
	if y < 0 || y >= len(buf) {
		return
	}
	row := buf[y]

	setCell := func(x int, ch rune, style tcell.Style) {
		if x >= 0 && x < len(row) {
			row[x] = Cell{Ch: ch, Style: style}
		}
	}

	if !d.IsExpanded() {
		// Collapsed: left cap + hamburger + right cap
		hamburgerStyle := tcell.StyleDefault.Foreground(mutedFG).Background(borderBG)
		setCell(rect.X, decorPillLeftCap, capStyle)
		setCell(rect.X+1, decorHamburger, hamburgerStyle)
		setCell(rect.X+2, decorPillRightCap, capStyle)
		return
	}

	// Expanded
	app, wm := d.allActions()
	x := rect.X
	setCell(x, decorPillLeftCap, capStyle)
	x++

	drawAction := func(a DecoratorAction) {
		style := normalStyle
		if a.Active {
			style = activeStyle
		}
		if a.Disabled {
			style = disabledStyle
		}
		setCell(x, ' ', style)
		x++
		setCell(x, a.Icon, style)
		x++
		setCell(x, ' ', style)
		x++
	}

	for _, a := range app {
		drawAction(a)
	}

	if len(app) > 0 && len(wm) > 0 {
		sepStyle := tcell.StyleDefault.Foreground(mutedFG).Background(borderBG)
		setCell(x, decorSep, sepStyle)
		x++
	}

	for _, a := range wm {
		drawAction(a)
	}

	setCell(x, decorPillRightCap, capStyle)
}

// HandleMouse processes a mouse event in absolute coordinates.
// Returns the action's Help text and whether the event was consumed.
// If an action was clicked, its OnClick is called.
func (d *PaneDecorator) HandleMouse(absX, absY int, buttons tcell.ButtonMask, borderX0, borderX1, borderY int) (help string, consumed bool) {
	if !d.HasActions() {
		return "", false
	}

	// Check against expanded rect for hover detection
	expandedW := d.expandedWidth()
	expandedX := borderX1 - expandedW - 1
	expandedRect := texelcore.Rect{X: expandedX, Y: borderY, W: expandedW, H: 1}

	currentRect := d.PillRect(borderX0, borderX1, borderY)

	inExpanded := expandedRect.Contains(absX, absY)
	inCurrent := currentRect.Contains(absX, absY)

	if !inExpanded && !inCurrent {
		if d.expanded {
			d.expanded = false
		}
		return "", false
	}

	// Mouse is in the pill zone — expand if collapsed
	if !d.IsExpanded() {
		d.expanded = true
		return "", true
	}

	// Find which action was hit (expanded state)
	app, wm := d.allActions()
	x := currentRect.X + 1 // skip left cap

	clicked := buttons&tcell.Button1 != 0

	for i := range app {
		if absX >= x && absX < x+3 {
			if clicked && !app[i].Disabled && app[i].OnClick != nil {
				app[i].OnClick()
			}
			return app[i].Help, true
		}
		x += 3
	}

	if len(app) > 0 && len(wm) > 0 {
		x++ // separator
	}

	for i := range wm {
		if absX >= x && absX < x+3 {
			if clicked && !wm[i].Disabled && wm[i].OnClick != nil {
				wm[i].OnClick()
			}
			return wm[i].Help, true
		}
		x += 3
	}

	return "", true
}
```

- [ ] **Step 2: Build**

```bash
go build ./texel/
```

Expected: builds.

- [ ] **Step 3: Commit**

```bash
git add texel/pane_decorator.go
git commit -m "Add PaneDecorator type with draw, mouse, and action management"
```

---

### Task 2: Unit tests for PaneDecorator

**Files:**
- Create: `texel/pane_decorator_test.go`

- [ ] **Step 1: Write tests**

```go
// texel/pane_decorator_test.go
package texel

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestPaneDecorator_AddRemoveActions(t *testing.T) {
	d := NewPaneDecorator(false)
	d.AddAppAction(DecoratorAction{ID: "search", Icon: '󰍉'})
	d.AddAppAction(DecoratorAction{ID: "config", Icon: '⚙'})
	if !d.HasActions() {
		t.Fatal("expected actions")
	}
	d.RemoveAppAction("search")
	app, _ := d.allActions()
	if len(app) != 1 || app[0].ID != "config" {
		t.Fatalf("expected 1 app action 'config', got %v", app)
	}
}

func TestPaneDecorator_CollapsedWidth(t *testing.T) {
	d := NewPaneDecorator(false)
	d.AddAppAction(DecoratorAction{ID: "a", Icon: 'X'})
	if d.PillWidth() != 3 {
		t.Fatalf("collapsed width: got %d, want 3", d.PillWidth())
	}
}

func TestPaneDecorator_ExpandedWidth(t *testing.T) {
	d := NewPaneDecorator(false)
	d.SetExpanded(true)
	d.AddAppAction(DecoratorAction{ID: "a", Icon: 'X'})
	d.AddAppAction(DecoratorAction{ID: "b", Icon: 'Y'})
	d.AddWMAction(DecoratorAction{ID: "z", Icon: 'Z'})
	// 2 caps + 2*3 app + 1 sep + 1*3 wm = 2+6+1+3 = 12
	if d.PillWidth() != 12 {
		t.Fatalf("expanded width: got %d, want 12", d.PillWidth())
	}
}

func TestPaneDecorator_DrawCollapsed(t *testing.T) {
	d := NewPaneDecorator(false)
	d.AddAppAction(DecoratorAction{ID: "a", Icon: 'X'})

	buf := makeTestBuffer(1, 40)
	style := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)
	d.Draw(buf, 0, 40, 0, style)

	// Should have hamburger near right edge
	found := false
	for _, cell := range buf[0] {
		if cell.Ch == decorHamburger {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("hamburger not found in collapsed pill")
	}
}

func TestPaneDecorator_DrawExpanded(t *testing.T) {
	d := NewPaneDecorator(false)
	d.SetExpanded(true)
	d.AddAppAction(DecoratorAction{ID: "a", Icon: 'X', Active: true})

	buf := makeTestBuffer(1, 40)
	style := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)
	d.Draw(buf, 0, 40, 0, style)

	// Should have the icon
	found := false
	for _, cell := range buf[0] {
		if cell.Ch == 'X' {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("action icon 'X' not found in expanded pill")
	}
}

func TestPaneDecorator_AlwaysExpanded(t *testing.T) {
	d := NewPaneDecorator(true)
	d.AddAppAction(DecoratorAction{ID: "a", Icon: 'X'})
	if !d.IsExpanded() {
		t.Fatal("expected always expanded")
	}
	d.SetExpanded(false) // should be ignored
	if !d.IsExpanded() {
		t.Fatal("alwaysExpanded should ignore SetExpanded(false)")
	}
}

func TestPaneDecorator_UpdateAction(t *testing.T) {
	d := NewPaneDecorator(false)
	d.AddAppAction(DecoratorAction{ID: "a", Icon: 'X', Active: false})
	d.UpdateAppAction(DecoratorAction{ID: "a", Active: true})
	app, _ := d.allActions()
	if !app[0].Active {
		t.Fatal("expected Active=true after update")
	}
}

func makeTestBuffer(rows, cols int) [][]Cell {
	buf := make([][]Cell, rows)
	for y := range buf {
		buf[y] = make([]Cell, cols)
		for x := range buf[y] {
			buf[y][x] = Cell{Ch: '─', Style: tcell.StyleDefault}
		}
	}
	return buf
}
```

- [ ] **Step 2: Run tests**

```bash
go test ./texel/ -v -run TestPaneDecorator
```

Expected: all PASS.

- [ ] **Step 3: Commit**

```bash
git add texel/pane_decorator_test.go
git commit -m "Add unit tests for PaneDecorator"
```

---

### Task 3: Wire decorator into pane

**Files:**
- Modify: `texel/pane.go`
- Modify: `texel/pane_render.go`
- Modify: `texel/pane_input.go`

- [ ] **Step 1: Add decorator field to pane struct**

In `texel/pane.go`, add to the `pane` struct (after `wasActive`):

```go
	// Pane decorator (pill action bar on the border)
	decorator *PaneDecorator
```

In `newPane`, after `p.initBorder()`, add:

```go
	p.decorator = NewPaneDecorator(false)
```

- [ ] **Step 2: Register ControlBus handlers for decorator messages**

In `texel/pane.go`, in the `AttachApp` method, after the existing ControlBus registrations (around line 210-230), add decorator handlers for ALL apps that provide a ControlBus:

```go
	// Register decorator action handlers for all apps with a ControlBus.
	if provider, ok := app.(ControlBusProvider); ok {
		provider.RegisterControl("decorator.add", "Add decorator action", func(payload interface{}) error {
			if a, ok := payload.(DecoratorAction); ok {
				p.decorator.AddAppAction(a)
				p.markDirty()
			}
			return nil
		})
		provider.RegisterControl("decorator.remove", "Remove decorator action", func(payload interface{}) error {
			if id, ok := payload.(string); ok {
				p.decorator.RemoveAppAction(id)
				p.markDirty()
			}
			return nil
		})
		provider.RegisterControl("decorator.update", "Update decorator action state", func(payload interface{}) error {
			if a, ok := payload.(DecoratorAction); ok {
				p.decorator.UpdateAppAction(a)
				p.markDirty()
			}
			return nil
		})
	}
```

- [ ] **Step 3: Call decorator Draw in pane_render.go**

In `texel/pane_render.go`, after `p.border.Draw(painter)` (line 152), add:

```go
	// Draw decorator pill on the border's top row
	if p.decorator != nil && p.decorator.HasActions() {
		borderStyle := p.currentBorderStyle()
		p.decorator.Draw(buffer, 0, w, 0, borderStyle)
	}
```

Add a helper method in `pane_render.go`:

```go
// currentBorderStyle returns the tcell.Style the border is currently using.
func (p *pane) currentBorderStyle() tcell.Style {
	ds := p.border.DetermineStyle()
	ctx := color.ColorContext{}
	return tcell.StyleDefault.
		Foreground(ds.FG.Resolve(ctx)).
		Background(ds.BG.Resolve(ctx)).
		Attributes(ds.Attrs)
}
```

Note: `DetermineStyle()` is currently `determineStyle()` (unexported) on the Border widget. You may need to either export it or access the border's fields directly. Check the Border widget — if `determineStyle` is unexported, use `p.border.Style` for normal, `p.border.FocusedStyle` when active, `p.border.ResizingStyle` when resizing:

```go
func (p *pane) currentBorderStyle() tcell.Style {
	var ds color.DynamicStyle
	if p.IsResizing {
		ds = p.border.ResizingStyle
	} else if p.IsActive {
		ds = p.border.FocusedStyle
	} else {
		ds = p.border.Style
	}
	ctx := color.ColorContext{}
	return tcell.StyleDefault.
		Foreground(ds.FG.Resolve(ctx)).
		Background(ds.BG.Resolve(ctx)).
		Attributes(ds.Attrs)
}
```

- [ ] **Step 4: Route mouse events to decorator in pane_input.go**

In `texel/pane_input.go`, modify `handleMouse` to check the decorator first. The decorator operates in absolute coordinates (border coordinates), while the current `handleMouse` converts to content-local coords. Add a check BEFORE the existing mouse forwarding:

```go
func (p *pane) handleMouse(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) {
	// Check if mouse hits the decorator pill on the top border
	if p.decorator != nil && p.decorator.HasActions() && y == p.absY0 {
		help, consumed := p.decorator.HandleMouse(x, y, buttons, p.absX0, p.absX1, p.absY0)
		if consumed {
			if help != "" {
				// Show help in status bar (via ControlBus or direct)
				_ = help // TODO: wire to status bar in future
			}
			p.markDirty()
			return
		}
	}

	if !p.handlesMouseEvents() {
		return
	}
	localX, localY := p.contentLocalCoords(x, y)
	ev := tcell.NewEventMouse(localX, localY, buttons, modifiers)
	p.mouseHandler.HandleMouse(ev)
	p.markDirty()
}
```

- [ ] **Step 5: Build and test**

```bash
go build ./...
go test ./texel/ -v -run TestPaneDecorator
```

- [ ] **Step 6: Commit**

```bash
git add texel/pane.go texel/pane_render.go texel/pane_input.go
git commit -m "Wire PaneDecorator into pane: ControlBus, render, mouse"
```

---

### Task 4: Add zoom toggle as WM action

**Files:**
- Modify: `texel/desktop_engine_core.go`
- Modify: `texel/pane.go`

- [ ] **Step 1: Add WM zoom action when pane is created**

In `texel/pane.go`, in `newPane`, after creating the decorator, add the zoom WM action:

```go
	// Add window-manager zoom action to the decorator (right zone).
	p.decorator.AddWMAction(DecoratorAction{
		ID:   "zoom",
		Icon: '󰊓', // nf-md-fullscreen
		Help: "Toggle zoom",
		OnClick: func() {
			if p.screen != nil && p.screen.desktop != nil {
				p.screen.desktop.toggleZoom()
			}
		},
	})
```

- [ ] **Step 2: Update zoom icon based on state**

In `texel/pane_render.go`, in the render method, before drawing the decorator, update the zoom action's Active state and icon:

```go
	// Update zoom state on the decorator
	if p.decorator != nil {
		zoomed := p.screen != nil && p.screen.desktop != nil && p.screen.desktop.zoomedPane != nil
		p.decorator.UpdateWMAction(DecoratorAction{
			ID:     "zoom",
			Active: zoomed,
		})
	}
```

- [ ] **Step 3: Build and test**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add texel/pane.go texel/pane_render.go
git commit -m "Add zoom toggle as WM decorator action"
```

---

### Task 5: Remove texelterm internal toggle overlay

**Files:**
- Delete: `apps/texelterm/toggle_overlay.go`
- Modify: `apps/texelterm/term.go`

- [ ] **Step 1: Delete toggle_overlay.go**

```bash
rm apps/texelterm/toggle_overlay.go
```

- [ ] **Step 2: Remove overlay fields from TexelTerm struct**

In `term.go`, remove from the struct:

```go
	overlayExpanded bool
```

Remove toggle button fields that are no longer rendered internally (keep them as state holders used by ControlBus updates):

The `tfmToggle`, `wrpToggle`, `searchToggle`, `cfgToggle` fields STAY — they're used for state tracking. The `overlayExpanded` field is removed.

- [ ] **Step 3: Remove drawToggleOverlay and handleToggleOverlayMouse calls**

In `term.go`, in the `Render` method (around line 653-655), remove:

```go
	a.updateModeIndicatorsLocked()
	a.drawToggleOverlay(a.buf, totalCols)
```

Replace with:

```go
	a.updateModeIndicatorsLocked()
```

In the `HandleMouse` method (around line 1303), remove:

```go
	if a.handleToggleOverlayMouse(ev) {
		return
	}
```

- [ ] **Step 4: Register decorator actions via ControlBus in Run()**

In `term.go`, add a method to register decorator actions. Call it from `Run()` after the ControlBus is wired (after `PrepareAppForRestore` or in `runShell` after initialization):

```go
// registerDecoratorActions sends decorator.add ControlBus messages to register
// the texelterm toggle actions on the pane's border pill.
func (a *TexelTerm) registerDecoratorActions() {
	cb := a.controlBus
	if cb == nil {
		return
	}

	cb.Send("decorator.add", texel.DecoratorAction{
		ID: "cfg", Icon: '⚙', Help: "Configuration (F4)",
		OnClick: func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.configPanel != nil {
				if a.configPanel.IsVisible() {
					a.configPanel.Close()
				} else {
					a.configPanel.Show()
				}
				a.cfgToggle.Active = a.configPanel.IsVisible()
				if a.vterm != nil {
					a.vterm.MarkAllDirty()
				}
				a.requestRefresh()
			}
		},
	})

	cb.Send("decorator.add", texel.DecoratorAction{
		ID: "tfm", Icon: '󰁨', Help: "Transformers (F8)",
		OnClick: func() {
			a.toggleTransformers()
		},
	})

	cb.Send("decorator.add", texel.DecoratorAction{
		ID: "wrp", Icon: '', Help: "Line wrapping",
		Active: true,
		OnClick: func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			a.wrpUserPref = !a.wrpUserPref
			if a.vterm != nil {
				a.vterm.SetWrapEnabled(a.wrpUserPref)
				a.vterm.MarkAllDirty()
			}
			a.wrpToggle.Active = a.wrpUserPref
			a.requestRefresh()
		},
	})

	cb.Send("decorator.add", texel.DecoratorAction{
		ID: "search", Icon: '󰍉', Help: "Search (F3)",
		OnClick: func() {
			if a.historyNavigator != nil {
				if a.historyNavigator.IsVisible() {
					a.closeSearch()
				} else {
					a.openSearch()
				}
			}
		},
	})
}
```

Note: The `texel.DecoratorAction` type needs to be importable from the texelterm package. Since both are in the same module, this is straightforward — just import `"github.com/framegrace/texelation/texel"`.

However, there may be a circular dependency issue (texel imports apps/texelterm for snapshot factories, texelterm imports texel for DecoratorAction). If so, move `DecoratorAction` to a shared package or use `interface{}` payload with the ControlBus (which already uses `interface{}`). The ControlBus handler in `pane.go` already does a type assertion: `if a, ok := payload.(DecoratorAction); ok`. So texelterm just needs to send the same struct type.

To avoid circular imports, cast to the ControlBus `Send` with the struct directly — the handler in pane.go will type-assert it. If there's a circular import, define a duplicate struct in texelterm and have the pane handler accept both (or use a shared interface).

**Simplest approach**: texelterm already imports `texel` indirectly (via other packages). Check if direct import works. If not, define the action as a map:

```go
cb.Send("decorator.add", map[string]interface{}{
	"id": "cfg", "icon": '⚙', "help": "Configuration (F4)",
	"onClick": func() { ... },
})
```

And have the pane handler accept `map[string]interface{}` as well as `DecoratorAction`.

- [ ] **Step 5: Update updateModeIndicatorsLocked to send ControlBus updates**

Replace the toggle button state updates that were used for rendering with ControlBus `decorator.update` messages:

```go
func (a *TexelTerm) updateModeIndicatorsLocked() {
	if a.vterm == nil || a.controlBus == nil {
		return
	}

	tfmOverride := a.vterm.IsInTUIMode() || a.vterm.InAltScreen()

	// Update transformer state
	tfmActive := a.tfmUserPref && !tfmOverride
	if tfmOverride && a.pipeline != nil && a.pipeline.Enabled() {
		a.pipeline.SetEnabled(false)
		a.vterm.SetShowOverlay(false)
		a.vterm.MarkAllDirty()
	} else if !tfmOverride && a.pipeline != nil && a.pipeline.Enabled() != a.tfmUserPref {
		a.pipeline.SetEnabled(a.tfmUserPref)
		a.vterm.SetShowOverlay(a.tfmUserPref)
		a.vterm.MarkAllDirty()
	}

	// Update wrap state
	if tfmOverride {
		if a.vterm.WrapEnabled() {
			a.vterm.SetWrapEnabled(false)
		}
	} else if a.vterm.WrapEnabled() != a.wrpUserPref {
		a.vterm.SetWrapEnabled(a.wrpUserPref)
	}

	// Send state updates to decorator via ControlBus
	a.controlBus.Send("decorator.update", DecoratorAction{ID: "tfm", Active: tfmActive, Disabled: tfmOverride})
	a.controlBus.Send("decorator.update", DecoratorAction{ID: "wrp", Active: a.wrpUserPref && !tfmOverride, Disabled: tfmOverride})
	a.controlBus.Send("decorator.update", DecoratorAction{ID: "search", Active: a.historyNavigator != nil && a.historyNavigator.IsVisible()})
	a.controlBus.Send("decorator.update", DecoratorAction{ID: "cfg", Active: a.configPanel != nil && a.configPanel.IsVisible()})
}
```

Note: `DecoratorAction` here refers to the texel package type. If circular imports are an issue, use the map approach.

- [ ] **Step 6: Build and fix any compilation issues**

```bash
go build ./...
```

Fix any circular import or compilation issues.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "Remove texelterm internal toggle overlay, register actions via ControlBus"
```

---

### Task 6: Update tests

**Files:**
- Modify: `apps/texelterm/term_statusbar_test.go`

- [ ] **Step 1: Update the overlay test**

The `TestToggleOverlayRenderedInOutput` test checked for the hamburger icon in texelterm's own render buffer. Since the pill is now on the pane border (drawn by the pane, not texelterm), this test should verify that texelterm's row 0 is pure terminal content — no overlay:

```go
func TestToggleOverlayRenderedInOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	script := writeScript(t, "#!/bin/sh\nprintf 'hello'\n")
	app := texelterm.New("texelterm", script)
	app.Resize(40, 11)
	app.SetRefreshNotifier(make(chan bool, 4))
	errCh := make(chan error, 1)
	go func() { errCh <- app.Run() }()
	time.Sleep(300 * time.Millisecond)

	buf := app.Render()
	if buf == nil {
		t.Fatal("expected non-nil render buffer")
	}
	// Row 0 should be pure terminal content — no overlay pill
	contentRow := rowToString(buf[0])
	if strings.Contains(contentRow, "\u2261") {
		t.Error("hamburger icon should not be in texelterm render buffer (moved to pane border)")
	}

	app.Stop()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("texelterm did not exit after stop")
	}
}
```

- [ ] **Step 2: Run all tests**

```bash
go test ./...
```

- [ ] **Step 3: Commit**

```bash
git add apps/texelterm/term_statusbar_test.go
git commit -m "Update tests for pane decorator migration"
```

---

### Task 7: Configuration for always-expanded mode

**Files:**
- Modify: `texel/pane.go`

- [ ] **Step 1: Read config for decorator_expanded**

In `newPane`, read the system config to determine if the decorator should always be expanded:

```go
	alwaysExpanded := false
	cfg := config.System()
	if v, ok := cfg["pane_decorator_expanded"].(bool); ok {
		alwaysExpanded = v
	}
	p.decorator = NewPaneDecorator(alwaysExpanded)
```

Add import: `"github.com/framegrace/texelation/config"`

- [ ] **Step 2: Build and test**

```bash
go build ./...
go test ./texel/ -v -run TestPaneDecorator
```

- [ ] **Step 3: Commit**

```bash
git add texel/pane.go
git commit -m "Add pane_decorator_expanded config option"
```

---

### Task 8: Manual testing

- [ ] **Step 1: Build**

```bash
make build
```

- [ ] **Step 2: Test pill on border**

Start texelation. Verify:
- Collapsed hamburger pill appears on the top-right border of each terminal pane
- Hovering expands to show config, transformers, wrap, search icons (left zone) and zoom icon (right zone, after separator)
- Clicking toggles work (wrap, transformers, search, config panel)
- Zoom toggle enters/exits zoom mode
- Mouse leaving the pill collapses it
- Powerline rounded caps (`` ``) on both ends in both states
- Pill uses border colors (changes with focus/unfocus)

- [ ] **Step 3: Test config**

Set `"pane_decorator_expanded": true` in texelation.json. Restart. Verify pill is always expanded.

- [ ] **Step 4: Commit any fixes**

```bash
git add -A
git commit -m "Fix issues found during manual testing"
```

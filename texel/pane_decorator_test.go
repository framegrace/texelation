// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/pane_decorator_test.go
// Summary: Unit tests for PaneDecorator pill rendering and action management.

package texel

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

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

func TestPaneDecorator_AddRemoveActions(t *testing.T) {
	d := NewPaneDecorator(false)

	if d.HasActions() {
		t.Fatal("expected no actions on a fresh decorator")
	}

	d.AddAppAction(DecoratorAction{ID: "action1", Icon: 'A', Help: "First"})
	d.AddAppAction(DecoratorAction{ID: "action2", Icon: 'B', Help: "Second"})

	if !d.HasActions() {
		t.Fatal("expected HasActions() true after adding two app actions")
	}

	d.RemoveAppAction("action1")

	// Verify action1 is gone but action2 remains by checking the pill is still active.
	if !d.HasActions() {
		t.Fatal("expected HasActions() true after removing only one of two actions")
	}

	d.RemoveAppAction("action2")

	if d.HasActions() {
		t.Fatal("expected HasActions() false after removing all actions")
	}
}

func TestPaneDecorator_CollapsedWidth(t *testing.T) {
	d := NewPaneDecorator(false)
	d.AddAppAction(DecoratorAction{ID: "a1", Icon: 'X', Help: "Test"})

	// Collapsed: just the hamburger icon plus two cap characters → width 3.
	got := d.PillWidth()
	if got != 3 {
		t.Fatalf("collapsed PillWidth: want 3, got %d", got)
	}
}

func TestPaneDecorator_ExpandedWidth(t *testing.T) {
	d := NewPaneDecorator(false)
	d.SetExpanded(true)

	d.AddAppAction(DecoratorAction{ID: "a1", Icon: 'X'})
	d.AddAppAction(DecoratorAction{ID: "a2", Icon: 'Y'})
	d.AddWMAction(DecoratorAction{ID: "w1", Icon: 'Z'})

	// Layout: 2 caps + 2*3 app slots + 1 separator + 1*3 wm slots = 2+6+1+3 = 12.
	want := 12
	got := d.PillWidth()
	if got != want {
		t.Fatalf("expanded PillWidth: want %d, got %d", want, got)
	}
}

func TestPaneDecorator_DrawCollapsed(t *testing.T) {
	d := NewPaneDecorator(false)
	d.AddAppAction(DecoratorAction{ID: "a1", Icon: 'X'})

	buf := makeTestBuffer(1, 40)
	borderStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)
	d.Draw(buf, 0, 40, 0, borderStyle)

	// Collapsed pill must contain the hamburger icon somewhere in the row.
	found := false
	for _, cell := range buf[0] {
		if cell.Ch == decorHamburger {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("DrawCollapsed: hamburger rune %q not found in buf[0]", decorHamburger)
	}
}

func TestPaneDecorator_DrawExpanded(t *testing.T) {
	d := NewPaneDecorator(false)
	d.SetExpanded(true)
	d.AddAppAction(DecoratorAction{ID: "a1", Icon: 'X'})

	buf := makeTestBuffer(1, 40)
	borderStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)
	d.Draw(buf, 0, 40, 0, borderStyle)

	found := false
	for _, cell := range buf[0] {
		if cell.Ch == 'X' {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("DrawExpanded: icon 'X' not found in buf[0]")
	}
}

func TestPaneDecorator_AlwaysExpanded(t *testing.T) {
	d := NewPaneDecorator(true)

	if !d.IsExpanded() {
		t.Fatal("alwaysExpanded decorator should report IsExpanded() true")
	}

	// SetExpanded(false) must be ignored when alwaysExpanded is set.
	d.SetExpanded(false)
	if !d.IsExpanded() {
		t.Fatal("SetExpanded(false) should be ignored on an alwaysExpanded decorator")
	}
}

func TestPaneDecorator_UpdateAction(t *testing.T) {
	d := NewPaneDecorator(false)
	d.AddAppAction(DecoratorAction{ID: "a1", Icon: 'A', Active: false})

	// Expand so icons are rendered, then update the action to Active=true.
	d.SetExpanded(true)
	d.UpdateAppAction(DecoratorAction{ID: "a1", Icon: 'A', Active: true})

	buf := makeTestBuffer(1, 40)
	borderStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)
	d.Draw(buf, 0, 40, 0, borderStyle)

	// After the update the action icon must still appear in the buffer.
	found := false
	for _, cell := range buf[0] {
		if cell.Ch == 'A' {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("UpdateAction: icon 'A' not found after update to Active=true")
	}
}

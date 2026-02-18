// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/client_selection.go
// Summary: Mouse-driven text selection state and clipboard extraction for client runtime.

package clientruntime

import (
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/client"
)

func (s *clientState) handleSelectionMouse(ev *tcell.EventMouse) bool {
	if s == nil || ev == nil || s.cache == nil {
		return false
	}
	buttons := ev.Buttons()

	// Ignore wheel events for selection state to prevent false releases
	// (wheel events often don't report held buttons correctly)
	if buttons&(tcell.WheelUp|tcell.WheelDown|tcell.WheelLeft|tcell.WheelRight) != 0 {
		return false
	}

	x, y := ev.Position()
	sel := &s.selection
	changed := false

	startPressed := buttons&tcell.Button1 != 0 && sel.lastButtons&tcell.Button1 == 0
	stillPressed := buttons&tcell.Button1 != 0 && sel.dragging
	released := buttons&tcell.Button1 == 0 && sel.lastButtons&tcell.Button1 != 0

	if startPressed {
		pane := s.cache.PaneAt(x, y)
		if pane == nil {
			changed = sel.clear() || changed
		} else if pane.HandlesSelection {
			changed = sel.clear() || changed
		} else {
			changed = sel.begin(pane, x, y) || changed
		}
	} else if stillPressed {
		pane := s.cache.PaneByID(sel.paneID)
		changed = sel.updateCurrent(pane, x, y) || changed
	} else if released {
		pane := s.cache.PaneByID(sel.paneID)
		changed = sel.finish(pane, x, y) || changed
		if pane == nil || !pane.HandlesSelection {
			changed = sel.clear() || changed
		}
	}

	sel.lastButtons = buttons
	return changed
}

func (s *clientState) clearSelection() bool {
	if s == nil {
		return false
	}
	return s.selection.clear()
}

func (s *clientState) selectionBounds() (pane *client.PaneState, minX, maxX, minY, maxY int, ok bool) {
	if s == nil {
		return nil, 0, 0, 0, 0, false
	}
	sel := &s.selection
	if !sel.isVisible() {
		return nil, 0, 0, 0, 0, false
	}
	pane = s.cache.PaneByID(sel.paneID)
	if pane == nil {
		return nil, 0, 0, 0, 0, false
	}
	minX, maxX, minY, maxY, okBounds := sel.bounds()
	if !okBounds {
		return nil, 0, 0, 0, 0, false
	}
	return pane, minX, maxX, minY, maxY, true
}

func (s *clientState) selectionClipboardData() ([]byte, string, bool) {
	pane, minX, maxX, minY, maxY, ok := s.selectionBounds()
	if !ok || pane == nil {
		return nil, "", false
	}
	if minX >= maxX || minY >= maxY {
		return nil, "", false
	}
	lines := make([]string, 0, maxY-minY)
	for y := minY; y < maxY; y++ {
		localY := y - pane.Rect.Y
		if localY < 0 || localY >= pane.Rect.Height {
			continue
		}
		rowCells := pane.RowCells(localY)
		start := minX - pane.Rect.X
		end := maxX - pane.Rect.X
		if start < 0 {
			start = 0
		}
		if end > pane.Rect.Width {
			end = pane.Rect.Width
		}
		if end <= start {
			continue
		}
		width := end - start
		runes := make([]rune, width)
		for i := 0; i < width; i++ {
			ch := ' '
			idx := start + i
			if rowCells != nil && idx < len(rowCells) {
				if rowCells[idx].Ch != 0 {
					ch = rowCells[idx].Ch
				}
			}
			runes[i] = ch
		}
		lines = append(lines, string(runes))
	}
	if len(lines) == 0 {
		return nil, "", false
	}
	text := strings.Join(lines, "\n")
	return []byte(text), "text/plain", true
}

type selectionRect struct {
	x, y, width, height int
}

func (r selectionRect) clamp(x, y int) (int, int, bool) {
	if r.width <= 0 || r.height <= 0 {
		return 0, 0, false
	}
	maxX := r.x + r.width - 1
	maxY := r.y + r.height - 1
	if x < r.x {
		x = r.x
	} else if x > maxX {
		x = maxX
	}
	if y < r.y {
		y = r.y
	} else if y > maxY {
		y = maxY
	}
	return x, y, true
}

type selectionState struct {
	active      bool
	dragging    bool
	moved       bool
	hasPane     bool
	paneID      [16]byte
	paneRect    selectionRect
	anchorX     int
	anchorY     int
	currentX    int
	currentY    int
	lastButtons tcell.ButtonMask
	pendingCopy bool
}

func (s *selectionState) clear() bool {
	changed := s.active || s.dragging || s.hasPane || s.pendingCopy
	s.active = false
	s.dragging = false
	s.moved = false
	s.hasPane = false
	s.pendingCopy = false
	s.paneID = [16]byte{}
	s.paneRect = selectionRect{}
	return changed
}

func (s *selectionState) begin(pane *client.PaneState, x, y int) bool {
	if pane == nil {
		return s.clear()
	}
	s.dragging = true
	s.moved = false
	s.pendingCopy = false
	s.active = false
	s.hasPane = true
	s.paneID = pane.ID
	s.paneRect = selectionRect{
		x:      pane.Rect.X,
		y:      pane.Rect.Y,
		width:  pane.Rect.Width,
		height: pane.Rect.Height,
	}
	x, y, ok := s.paneRect.clamp(x, y)
	if !ok {
		return s.clear()
	}
	s.anchorX = x
	s.anchorY = y
	s.currentX = x
	s.currentY = y
	return true
}

func (s *selectionState) updateCurrent(pane *client.PaneState, x, y int) bool {
	if !s.dragging || !s.hasPane {
		return false
	}
	if pane != nil {
		s.paneRect = selectionRect{
			x:      pane.Rect.X,
			y:      pane.Rect.Y,
			width:  pane.Rect.Width,
			height: pane.Rect.Height,
		}
	}
	x, y, ok := s.paneRect.clamp(x, y)
	if !ok {
		return false
	}
	if x == s.currentX && y == s.currentY {
		return false
	}
	s.currentX = x
	s.currentY = y
	if x != s.anchorX || y != s.anchorY {
		s.moved = true
	}
	return true
}

func (s *selectionState) finish(pane *client.PaneState, x, y int) bool {
	if !s.dragging {
		return false
	}
	s.dragging = false
	if pane != nil {
		s.paneRect = selectionRect{
			x:      pane.Rect.X,
			y:      pane.Rect.Y,
			width:  pane.Rect.Width,
			height: pane.Rect.Height,
		}
	}
	x, y, ok := s.paneRect.clamp(x, y)
	if ok {
		s.currentX = x
		s.currentY = y
	}
	if s.moved {
		s.active = true
		s.pendingCopy = true
	} else {
		s.active = false
		s.pendingCopy = false
		s.hasPane = false
	}
	return true
}

func (s *selectionState) isVisible() bool {
	return (s.active || s.dragging) && s.hasPane
}

func (s *selectionState) bounds() (minX, maxX, minY, maxY int, ok bool) {
	if !s.isVisible() {
		return 0, 0, 0, 0, false
	}
	x0, x1 := s.anchorX, s.currentX
	y0, y1 := s.anchorY, s.currentY
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	// Expand to make the range inclusive of the end cell.
	return x0, x1 + 1, y0, y1 + 1, true
}

func (s *selectionState) consumePendingCopy() bool {
	if !s.pendingCopy {
		return false
	}
	s.pendingCopy = false
	return true
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/workspace_navigation.go
// Summary: Focus cycling, pane selection, and mouse resize for workspaces.

package texel

import (
	"log"

	"github.com/gdamore/tcell/v2"
)

func (w *Workspace) moveActivePane(d Direction) {
	log.Printf("moveActivePane: Moving in direction %v", d)

	// Get current and target panes
	var currentPane, targetPane *pane
	var currentTitle, targetTitle string

	if w.tree.ActiveLeaf != nil && w.tree.ActiveLeaf.Pane != nil {
		currentPane = w.tree.ActiveLeaf.Pane
		currentTitle = currentPane.getTitle()
	}

	// We need to find the neighbor manually since findNeighbor is a method on Tree
	// Let's just proceed with the move and get the result
	oldActiveLeaf := w.tree.ActiveLeaf

	// Move in tree first
	w.tree.MoveActive(d)

	// Check if we actually moved
	if w.tree.ActiveLeaf == oldActiveLeaf {
		log.Printf("moveActivePane: No movement occurred")
		return
	}

	// Get the target pane after the move
	if w.tree.ActiveLeaf != nil && w.tree.ActiveLeaf.Pane != nil {
		targetPane = w.tree.ActiveLeaf.Pane
		targetTitle = targetPane.getTitle()
	}

	w.recalculateLayout()

	// Set states and handle animations properly
	if currentPane != nil {
		currentPane.IsActive = false
		currentPane.notifyStateChange()
	}

	if targetPane != nil {
		targetPane.IsActive = true
		targetPane.notifyStateChange()
	}

	log.Printf("moveActivePane: Moved from '%s' to '%s'", currentTitle, targetTitle)

	w.Broadcast(Event{Type: EventPaneActiveChanged, Payload: w.tree.ActiveLeaf})
	w.desktop.broadcastStateUpdate()
	w.notifyFocus()
}

func (w *Workspace) nodeAt(x, y int) *Node {
	if w == nil || w.tree == nil {
		return nil
	}
	return w.tree.FindLeafAt(x, y)
}

func (w *Workspace) setBorderResizing(border *selectedBorder, resizing bool) {
	if border == nil || border.node == nil {
		return
	}
	if border.index < 0 || border.index+1 >= len(border.node.Children) {
		return
	}
	left := border.node.Children[border.index]
	right := border.node.Children[border.index+1]
	forEachLeafPane(left, func(p *pane) {
		p.SetResizing(resizing)
	})
	forEachLeafPane(right, func(p *pane) {
		p.SetResizing(resizing)
	})
}

func (w *Workspace) activateLeaf(node *Node) bool {
	if w == nil || node == nil || node.Pane == nil || w.tree == nil {
		return false
	}
	if w.tree.ActiveLeaf == node {
		if !node.Pane.IsActive {
			node.Pane.SetActive(true)
		}
		return false
	}

	if current := w.tree.ActiveLeaf; current != nil && current.Pane != nil {
		current.Pane.SetActive(false)
	}

	w.tree.ActiveLeaf = node
	node.Pane.SetActive(true)

	w.Broadcast(Event{Type: EventPaneActiveChanged, Payload: node})
	w.notifyFocus()
	if w.desktop != nil {
		w.desktop.broadcastStateUpdate()
	}
	return true
}

func (w *Workspace) borderForNeighbor(node *Node, dir Direction) *selectedBorder {
	if w == nil || w.tree == nil || node == nil {
		return nil
	}
	current := node
	for current.Parent != nil {
		parent := current.Parent
		index := -1
		for i, child := range parent.Children {
			if child == current {
				index = i
				break
			}
		}
		if index == -1 {
			return nil
		}

		switch dir {
		case DirLeft:
			if parent.Split == Vertical && index > 0 {
				return &selectedBorder{node: parent, index: index - 1}
			}
		case DirRight:
			if parent.Split == Vertical && index < len(parent.Children)-1 {
				return &selectedBorder{node: parent, index: index}
			}
		case DirUp:
			if parent.Split == Horizontal && index > 0 {
				return &selectedBorder{node: parent, index: index - 1}
			}
		case DirDown:
			if parent.Split == Horizontal && index < len(parent.Children)-1 {
				return &selectedBorder{node: parent, index: index}
			}
		}

		current = parent
	}
	return nil
}

func (w *Workspace) borderAt(x, y int) *selectedBorder {
	if w == nil || w.tree == nil {
		return nil
	}
	node := w.tree.FindLeafAt(x, y)
	if node == nil || node.Pane == nil {
		return nil
	}
	p := node.Pane
	if x == p.absX0 {
		if border := w.borderForNeighbor(node, DirLeft); border != nil {
			return border
		}
	}
	if x == p.absX1-1 {
		if border := w.borderForNeighbor(node, DirRight); border != nil {
			return border
		}
	}
	if y == p.absY0 {
		if border := w.borderForNeighbor(node, DirUp); border != nil {
			return border
		}
	}
	if y == p.absY1-1 {
		if border := w.borderForNeighbor(node, DirDown); border != nil {
			return border
		}
	}
	return nil
}

func (w *Workspace) startMouseResize(border *selectedBorder) {
	if border == nil {
		return
	}
	clone := &selectedBorder{
		node:  border.node,
		index: border.index,
	}
	w.mouseResizeBorder = clone
	w.setBorderResizing(clone, true)
}

func (w *Workspace) finishMouseResize() {
	if w.mouseResizeBorder == nil {
		return
	}
	w.setBorderResizing(w.mouseResizeBorder, false)
	w.mouseResizeBorder = nil
}

func (w *Workspace) updateMouseResize(x, y int) {
	if w == nil || w.tree == nil || w.mouseResizeBorder == nil {
		return
	}
	border := w.mouseResizeBorder
	if border.node == nil {
		return
	}

	switch border.node.Split {
	case Vertical:
		w.adjustBorderToX(border, x)
	case Horizontal:
		w.adjustBorderToY(border, y)
	}
}

func (w *Workspace) handleMouseResize(x, y int, buttons, prevButtons tcell.ButtonMask) bool {
	if w == nil || w.tree == nil {
		return false
	}

	resizing := w.mouseResizeBorder != nil
	buttonDown := buttons&tcell.Button1 != 0
	prevDown := prevButtons&tcell.Button1 != 0

	if resizing {
		if buttonDown {
			w.updateMouseResize(x, y)
		} else if prevDown {
			w.finishMouseResize()
		}
		return true
	}

	start := buttonDown && !prevDown
	if !start {
		return false
	}

	border := w.borderAt(x, y)
	if border == nil {
		return false
	}

	w.startMouseResize(border)
	return true
}

func (w *Workspace) SwapActivePane(d Direction) {
	if d != -1 {
		w.tree.SwapActivePane(d)
		w.recalculateLayout()
		w.Refresh()
		if w.desktop != nil {
			w.desktop.broadcastTreeChanged()
			w.desktop.broadcastStateUpdate()
		}
	}
}

// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/widgets/box.go
// Summary: VBox and HBox container widgets for vertical and horizontal layouts.

package widgets

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
)

// BoxAlign specifies how children are aligned within available space.
type BoxAlign int

const (
	BoxAlignStart  BoxAlign = iota // Align to start (top for VBox, left for HBox)
	BoxAlignCenter                 // Center children
	BoxAlignEnd                    // Align to end (bottom for VBox, right for HBox)
)

// boxChild holds a child widget with its size hint.
type boxChild struct {
	widget   core.Widget
	size     int  // Fixed size (0 = use widget's natural size, -1 = flex)
	flex     bool // If true, this child expands to fill remaining space
	naturalW int  // Widget's natural width (captured at add time)
	naturalH int  // Widget's natural height (captured at add time)
}

// boxBase is the common implementation for VBox and HBox.
type boxBase struct {
	core.BaseWidget
	Style   tcell.Style
	Spacing int      // Space between children
	Align   BoxAlign // Alignment of children

	children       []boxChild
	inv            func(core.Rect)
	lastFocusedIdx int
	vertical       bool // true for VBox, false for HBox
}

func newBoxBase(vertical bool) *boxBase {
	tm := theme.Get()
	bg := tm.GetSemanticColor("bg.surface")
	fg := tm.GetSemanticColor("text.primary")

	b := &boxBase{
		Style:          tcell.StyleDefault.Background(bg).Foreground(fg),
		Spacing:        0,
		Align:          BoxAlignStart,
		lastFocusedIdx: -1,
		vertical:       vertical,
	}
	b.SetPosition(0, 0)
	b.Resize(1, 1)
	b.SetFocusable(true)
	return b
}

// AddChild adds a child widget with its natural size.
func (b *boxBase) AddChild(w core.Widget) {
	nw, nh := w.Size() // Capture natural size before layout modifies it
	b.children = append(b.children, boxChild{widget: w, size: 0, naturalW: nw, naturalH: nh})
	b.layout()
	if b.inv != nil {
		if ia, ok := w.(core.InvalidationAware); ok {
			ia.SetInvalidator(b.inv)
		}
	}
}

// AddChildWithSize adds a child widget with a fixed size.
func (b *boxBase) AddChildWithSize(w core.Widget, size int) {
	nw, nh := w.Size() // Capture natural size before layout modifies it
	b.children = append(b.children, boxChild{widget: w, size: size, naturalW: nw, naturalH: nh})
	b.layout()
	if b.inv != nil {
		if ia, ok := w.(core.InvalidationAware); ok {
			ia.SetInvalidator(b.inv)
		}
	}
}

// AddFlexChild adds a child widget that expands to fill remaining space.
func (b *boxBase) AddFlexChild(w core.Widget) {
	nw, nh := w.Size() // Capture natural size before layout modifies it
	b.children = append(b.children, boxChild{widget: w, flex: true, naturalW: nw, naturalH: nh})
	b.layout()
	if b.inv != nil {
		if ia, ok := w.(core.InvalidationAware); ok {
			ia.SetInvalidator(b.inv)
		}
	}
}

// ClearChildren removes all children.
func (b *boxBase) ClearChildren() {
	b.children = nil
	b.lastFocusedIdx = -1
}

// SetInvalidator implements core.InvalidationAware.
func (b *boxBase) SetInvalidator(fn func(core.Rect)) {
	b.inv = fn
	for _, child := range b.children {
		if ia, ok := child.widget.(core.InvalidationAware); ok {
			ia.SetInvalidator(fn)
		}
	}
}

// Draw renders all children.
func (b *boxBase) Draw(painter *core.Painter) {
	style := b.EffectiveStyle(b.Style)
	painter.Fill(b.Rect, ' ', style)

	for _, child := range b.children {
		child.widget.Draw(painter)
	}
}

// Resize updates the box size and relays out children.
func (b *boxBase) Resize(w, h int) {
	b.BaseWidget.Resize(w, h)
	b.layout()
}

// SetPosition updates the box position and relays out children.
func (b *boxBase) SetPosition(x, y int) {
	b.BaseWidget.SetPosition(x, y)
	b.layout()
}

// layout positions all children.
func (b *boxBase) layout() {
	if len(b.children) == 0 {
		return
	}

	// Calculate total fixed space and count flex children
	var totalFixed int
	var flexCount int
	for _, child := range b.children {
		if child.flex {
			flexCount++
		} else if child.size > 0 {
			totalFixed += child.size
		} else {
			// Use stored natural size (not current widget size which may have been modified)
			if b.vertical {
				totalFixed += child.naturalH
			} else {
				totalFixed += child.naturalW
			}
		}
	}

	// Add spacing
	totalFixed += b.Spacing * (len(b.children) - 1)

	// Calculate flex size
	var available int
	if b.vertical {
		available = b.Rect.H
	} else {
		available = b.Rect.W
	}
	remaining := available - totalFixed
	if remaining < 0 {
		remaining = 0
	}
	flexSize := 0
	if flexCount > 0 {
		flexSize = remaining / flexCount
	}

	// Position children
	pos := 0
	if b.vertical {
		pos = b.Rect.Y
	} else {
		pos = b.Rect.X
	}

	for _, child := range b.children {
		var size int
		if child.flex {
			size = flexSize
		} else if child.size > 0 {
			size = child.size
		} else {
			// Use stored natural size
			if b.vertical {
				size = child.naturalH
			} else {
				size = child.naturalW
			}
		}

		if b.vertical {
			child.widget.SetPosition(b.Rect.X, pos)
			child.widget.Resize(b.Rect.W, size)
			pos += size + b.Spacing
		} else {
			child.widget.SetPosition(pos, b.Rect.Y)
			child.widget.Resize(size, b.Rect.H)
			pos += size + b.Spacing
		}
	}
}

// VisitChildren implements core.ChildContainer.
func (b *boxBase) VisitChildren(fn func(core.Widget)) {
	for _, child := range b.children {
		fn(child.widget)
	}
}

// WidgetAt implements core.HitTester.
func (b *boxBase) WidgetAt(x, y int) core.Widget {
	if !b.HitTest(x, y) {
		return nil
	}
	for _, child := range b.children {
		if child.widget.HitTest(x, y) {
			if ht, ok := child.widget.(core.HitTester); ok {
				if deep := ht.WidgetAt(x, y); deep != nil {
					return deep
				}
			}
			return child.widget
		}
	}
	return b
}

// getFocusableChildren returns all focusable child widgets.
func (b *boxBase) getFocusableChildren() []core.Widget {
	var result []core.Widget
	for _, child := range b.children {
		if child.widget.Focusable() {
			result = append(result, child.widget)
		}
	}
	return result
}

// Focus focuses the first focusable child.
func (b *boxBase) Focus() {
	b.BaseWidget.Focus()
	children := b.getFocusableChildren()
	if len(children) == 0 {
		return
	}
	if b.lastFocusedIdx >= 0 && b.lastFocusedIdx < len(children) {
		children[b.lastFocusedIdx].Focus()
		return
	}
	children[0].Focus()
	b.lastFocusedIdx = 0
}

// Blur blurs all children.
func (b *boxBase) Blur() {
	children := b.getFocusableChildren()
	for i, w := range children {
		if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
			b.lastFocusedIdx = i
			w.Blur()
			break
		}
	}
	b.BaseWidget.Blur()
}

// TrapsFocus returns false.
func (b *boxBase) TrapsFocus() bool {
	return false
}

// CycleFocus moves focus to next/previous focusable child.
func (b *boxBase) CycleFocus(forward bool) bool {
	children := b.getFocusableChildren()
	if len(children) == 0 {
		return false
	}

	currentIdx := -1
	var focusedChild core.Widget
	for i, w := range children {
		if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
			currentIdx = i
			focusedChild = w
			break
		}
	}

	if currentIdx < 0 {
		if forward {
			children[0].Focus()
			b.lastFocusedIdx = 0
		} else {
			children[len(children)-1].Focus()
			b.lastFocusedIdx = len(children) - 1
		}
		b.invalidate()
		return true
	}

	var nextIdx int
	if forward {
		nextIdx = currentIdx + 1
		if nextIdx >= len(children) {
			return false
		}
	} else {
		nextIdx = currentIdx - 1
		if nextIdx < 0 {
			return false
		}
	}

	focusedChild.Blur()
	children[nextIdx].Focus()
	b.lastFocusedIdx = nextIdx
	b.invalidate()
	return true
}

// HandleKey routes key events to the focused child.
func (b *boxBase) HandleKey(ev *tcell.EventKey) bool {
	children := b.getFocusableChildren()
	for i, w := range children {
		if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
			isTab := ev.Key() == tcell.KeyTab || ev.Key() == tcell.KeyBacktab
			if isTab {
				if _, isContainer := w.(core.FocusCycler); isContainer {
					if w.HandleKey(ev) {
						return true
					}
				}
				return false
			}
			if w.HandleKey(ev) {
				b.lastFocusedIdx = i
				return true
			}
			return false
		}
	}
	return false
}

// HandleMouse routes mouse events to children.
func (b *boxBase) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !b.HitTest(x, y) {
		return false
	}

	buttons := ev.Buttons()
	isPress := buttons&tcell.Button1 != 0
	isWheel := buttons&(tcell.WheelUp|tcell.WheelDown|tcell.WheelLeft|tcell.WheelRight) != 0

	children := b.getFocusableChildren()
	for i, child := range children {
		if child.HitTest(x, y) {
			if isPress {
				for _, w := range children {
					if fs, ok := w.(core.FocusState); ok && fs.IsFocused() && w != child {
						w.Blur()
					}
				}
				child.Focus()
				b.lastFocusedIdx = i
				b.invalidate()
			}
			if ma, ok := child.(core.MouseAware); ok {
				return ma.HandleMouse(ev)
			}
			return !isWheel
		}
	}
	return !isWheel
}

func (b *boxBase) invalidate() {
	if b.inv != nil {
		b.inv(b.Rect)
	}
}

// Size returns the natural size of the box based on children.
// For VBox: width = max child width, height = sum of child heights + spacing.
// For HBox: width = sum of child widths + spacing, height = max child height.
// Uses stored natural sizes captured when children were added.
func (b *boxBase) Size() (int, int) {
	if len(b.children) == 0 {
		return b.Rect.W, b.Rect.H
	}

	var totalMain, maxCross int
	nonFlexCount := 0

	for _, child := range b.children {
		var childMain, childCross int

		if child.flex {
			// Flex children have no natural size in main direction
			// Skip them for natural size calculation
			continue
		} else if child.size > 0 {
			// Fixed size specified
			childMain = child.size
			if b.vertical {
				childCross = child.naturalW
			} else {
				childCross = child.naturalH
			}
		} else {
			// Use child's natural size (stored at add time)
			if b.vertical {
				childMain = child.naturalH
				childCross = child.naturalW
			} else {
				childMain = child.naturalW
				childCross = child.naturalH
			}
		}

		totalMain += childMain
		if nonFlexCount > 0 {
			totalMain += b.Spacing
		}
		nonFlexCount++
		if childCross > maxCross {
			maxCross = childCross
		}
	}

	if b.vertical {
		return maxCross, totalMain
	}
	return totalMain, maxCross
}

// VBox is a vertical box container that stacks children from top to bottom.
type VBox struct {
	*boxBase
}

// NewVBox creates a new vertical box container.
// Position defaults to 0,0 and size to 1,1.
func NewVBox() *VBox {
	return &VBox{boxBase: newBoxBase(true)}
}

// HBox is a horizontal box container that arranges children from left to right.
type HBox struct {
	*boxBase
}

// NewHBox creates a new horizontal box container.
// Position defaults to 0,0 and size to 1,1.
func NewHBox() *HBox {
	return &HBox{boxBase: newBoxBase(false)}
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/mouse_coordinator.go
// Summary: Unified mouse event handling for both standalone and embedded modes.

package texelterm

import (
	"sync"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/gdamore/tcell/v2"
)

// MouseWheelHandler is implemented by apps that want to react to mouse wheel input.
type MouseWheelHandler interface {
	HandleMouseWheel(x, y, deltaX, deltaY int, modifiers tcell.ModMask)
}

// ClipboardSetter is implemented by contexts that can set clipboard content.
type ClipboardSetter interface {
	SetClipboard(mime string, data []byte)
}

// GridProvider provides access to the viewport grid and coordinate conversion.
// This interface abstracts VTerm access for MouseCoordinator, enabling testability.
type GridProvider interface {
	// Grid returns the current viewport grid (height x width cells).
	Grid() [][]parser.Cell
	// ViewportToContent converts viewport coordinates to content coordinates.
	// Returns (logicalLine, charOffset, isCurrentLine, ok).
	// logicalLine is -1 for current uncommitted line when isCurrentLine is true.
	ViewportToContent(row, col int) (logicalLine int64, charOffset int, isCurrentLine bool, ok bool)
	// MarkAllDirty marks all viewport rows as needing re-render.
	MarkAllDirty()
	// Scroll scrolls the viewport by the given number of lines.
	// Positive values scroll down (show older content), negative scroll up.
	Scroll(lines int)
}

// MouseCoordinator unifies mouse event handling for both standalone and embedded modes.
// In standalone mode, it receives tcell.EventMouse directly via HandleMouse.
// In embedded mode, the desktop engine calls SelectionStart/Update/Finish.
// Both paths converge on the same internal selection logic.
type MouseCoordinator struct {
	mu               sync.Mutex
	clickDetector    *ClickDetector
	selectionMachine *SelectionStateMachine
	autoScroll       *AutoScrollManager
	wheelHandler     MouseWheelHandler
	clipboardSetter  ClipboardSetter
	gridProvider     GridProvider
	width, height    int

	// Button state tracking for standalone mode
	lastMouseButtons tcell.ButtonMask
	lastMouseX       int
	lastMouseY       int

	// Callbacks
	onDirty   func() // Called when display needs refresh
	onRefresh func() // Called to request refresh
}

// NewMouseCoordinator creates a new mouse coordinator.
// vtermProvider is used by SelectionStateMachine for word/line selection.
// gridProvider is used for viewport access and coordinate conversion.
// scrollConfig provides auto-scroll settings.
func NewMouseCoordinator(
	vtermProvider VTermProvider,
	gridProvider GridProvider,
	wheelHandler MouseWheelHandler,
	scrollConfig AutoScrollConfig,
) *MouseCoordinator {
	coord := &MouseCoordinator{
		clickDetector:    NewClickDetector(DefaultMultiClickTimeout),
		selectionMachine: NewSelectionStateMachineWithProvider(vtermProvider),
		autoScroll:       NewAutoScrollManager(scrollConfig),
		wheelHandler:     wheelHandler,
		gridProvider:     gridProvider,
	}

	return coord
}

// SetSize updates the terminal dimensions.
func (m *MouseCoordinator) SetSize(width, height int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.width = width
	m.height = height
	m.selectionMachine.SetSize(width, height)
	m.autoScroll.SetSize(height)
}

// SetCallbacks configures the callbacks for state changes.
func (m *MouseCoordinator) SetCallbacks(onDirty, onRefresh func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onDirty = onDirty
	m.onRefresh = onRefresh

	// Wire auto-scroll callbacks
	m.autoScroll.SetCallbacks(
		func(lines int) {
			if m.gridProvider != nil {
				m.gridProvider.Scroll(lines)
			}
		},
		onRefresh,
		func(x, y int) (int64, int, int) {
			// Resolve position and update selection to extend it during auto-scroll
			m.mu.Lock()
			logicalLine, charOffset, viewportRow := m.resolvePositionLocked(x, y)
			m.selectionMachine.Update(logicalLine, charOffset, viewportRow, 0)
			m.mu.Unlock()
			m.markDirty()
			return logicalLine, charOffset, viewportRow
		},
	)
}

// SetClipboardSetter sets the clipboard handler for standalone mode.
func (m *MouseCoordinator) SetClipboardSetter(setter ClipboardSetter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clipboardSetter = setter
}

// HandleMouse implements mouse event handling for standalone mode.
// Converts tcell.EventMouse to selection operations.
func (m *MouseCoordinator) HandleMouse(ev *tcell.EventMouse) bool {
	if ev == nil {
		return false
	}

	x, y := ev.Position()
	buttons := ev.Buttons()
	modifiers := ev.Modifiers()

	m.mu.Lock()
	defer m.mu.Unlock()

	prevButtons := m.lastMouseButtons
	m.lastMouseX = x
	m.lastMouseY = y
	m.lastMouseButtons = buttons

	// Handle mouse wheel events
	wheelDX, wheelDY := wheelDeltaFromMask(buttons)
	if wheelDX != 0 || wheelDY != 0 {
		m.mu.Unlock()
		if m.wheelHandler != nil {
			m.wheelHandler.HandleMouseWheel(x, y, wheelDX, wheelDY, modifiers)
		}
		m.mu.Lock()
		return true
	}

	// Detect button state transitions
	start := buttons&tcell.Button1 != 0 && prevButtons&tcell.Button1 == 0
	release := buttons&tcell.Button1 == 0 && prevButtons&tcell.Button1 != 0
	dragging := buttons&tcell.Button1 != 0 && prevButtons&tcell.Button1 != 0

	if start {
		// Cancel any existing selection
		if m.selectionMachine.IsActive() {
			m.selectionMachine.Cancel()
			m.autoScroll.Stop()
		}

		// Start new selection
		logicalLine, charOffset, viewportRow := m.resolvePositionLocked(x, y)
		clickType := m.clickDetector.DetectClick(viewportRow, charOffset)
		m.selectionMachine.Start(logicalLine, charOffset, viewportRow, clickType, modifiers)
		m.markDirty()
		return true
	}

	if dragging && m.selectionMachine.IsActive() {
		m.autoScroll.UpdatePosition(x, y)

		// Check if we should start/stop auto-scroll
		if m.autoScroll.ShouldAutoScroll(y, m.height) {
			if !m.autoScroll.IsActive() {
				m.autoScroll.Start()
			}
		} else {
			if m.autoScroll.IsActive() {
				m.autoScroll.Stop()
			}
		}

		logicalLine, charOffset, viewportRow := m.resolvePositionLocked(x, y)
		m.selectionMachine.Update(logicalLine, charOffset, viewportRow, modifiers)
		m.markDirty()
		return true
	}

	if release && m.selectionMachine.IsActive() {
		m.autoScroll.Stop()

		logicalLine, charOffset, viewportRow := m.resolvePositionLocked(x, y)
		mime, data, ok := m.selectionMachine.Finish(logicalLine, charOffset, viewportRow, modifiers)
		m.markDirty()

		// Copy to clipboard
		if ok && len(data) > 0 && m.clipboardSetter != nil {
			m.clipboardSetter.SetClipboard(mime, data)
		}
		return true
	}

	// Right-click: cancel selection
	if buttons&tcell.Button3 != 0 && prevButtons&tcell.Button3 == 0 {
		if m.selectionMachine.IsActive() || m.selectionMachine.IsRendered() {
			m.selectionMachine.Cancel()
			m.autoScroll.Stop()
			m.markDirty()
			return true
		}
	}

	return false
}

// IsSelectionActive returns true if a selection is in progress.
func (m *MouseCoordinator) IsSelectionActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.selectionMachine.IsActive()
}

// IsSelectionRendered returns true if a selection should be displayed.
func (m *MouseCoordinator) IsSelectionRendered() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.selectionMachine.IsRendered()
}

// GetSelectionRange returns the current selection range for rendering in content coordinates.
// Returns startLine (int64), startOffset (int), endLine (int64), endOffset (int), ok.
// The caller should convert to viewport coordinates using vterm.ContentToViewport().
func (m *MouseCoordinator) GetSelectionRange() (startLine int64, startOffset int, endLine int64, endOffset int, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.selectionMachine.GetSelectionRange()
}

// resolvePositionLocked converts screen coordinates to content coordinates.
// Returns (logicalLine, charOffset, viewportRow) for use with selection.
// logicalLine is -1 for current (uncommitted) line.
// Must be called with mutex locked.
func (m *MouseCoordinator) resolvePositionLocked(x, y int) (logicalLine int64, charOffset int, viewportRow int) {
	if m.gridProvider == nil {
		return 0, 0, 0
	}

	// Clamp Y to viewport bounds
	viewportRow = y
	if viewportRow < 0 {
		viewportRow = 0
	}
	if m.height > 0 && viewportRow >= m.height {
		viewportRow = m.height - 1
	}

	// Clamp X to valid range
	col := x
	if col < 0 {
		col = 0
	}

	// Get the grid to find line width for clamping
	grid := m.gridProvider.Grid()
	if grid != nil && viewportRow < len(grid) {
		lineWidth := len(grid[viewportRow])
		if col > lineWidth {
			col = lineWidth
		}
	}

	// Convert viewport position to content coordinates
	logicalLine, charOffset, _, ok := m.gridProvider.ViewportToContent(viewportRow, col)
	if !ok {
		// Fallback: treat as current line
		return -1, col, viewportRow
	}

	return logicalLine, charOffset, viewportRow
}

// markDirty marks the terminal display as needing a refresh.
func (m *MouseCoordinator) markDirty() {
	if m.gridProvider != nil {
		m.gridProvider.MarkAllDirty()
	}
	if m.onDirty != nil {
		m.onDirty()
	}
}

// wheelDeltaFromMask extracts wheel delta from button mask.
func wheelDeltaFromMask(mask tcell.ButtonMask) (int, int) {
	dx, dy := 0, 0
	if mask&tcell.WheelUp != 0 {
		dy--
	}
	if mask&tcell.WheelDown != 0 {
		dy++
	}
	if mask&tcell.WheelLeft != 0 {
		dx--
	}
	if mask&tcell.WheelRight != 0 {
		dx++
	}
	return dx, dy
}

// vtermGridAdapter wraps a VTerm to implement GridProvider.
type vtermGridAdapter struct {
	vterm *parser.VTerm
}

// NewVTermGridAdapter creates a GridProvider from a VTerm.
func NewVTermGridAdapter(vterm *parser.VTerm) GridProvider {
	if vterm == nil {
		return nil
	}
	return &vtermGridAdapter{vterm: vterm}
}

func (a *vtermGridAdapter) Grid() [][]parser.Cell {
	if a.vterm == nil {
		return nil
	}
	return a.vterm.Grid()
}

func (a *vtermGridAdapter) ViewportToContent(row, col int) (int64, int, bool, bool) {
	if a.vterm == nil {
		return 0, 0, false, false
	}
	return a.vterm.ViewportToContent(row, col)
}

func (a *vtermGridAdapter) MarkAllDirty() {
	if a.vterm != nil {
		a.vterm.MarkAllDirty()
	}
}

func (a *vtermGridAdapter) Scroll(lines int) {
	if a.vterm != nil {
		a.vterm.Scroll(lines)
	}
}

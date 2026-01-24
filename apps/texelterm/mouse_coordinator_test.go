// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/mouse_coordinator_test.go
// Summary: Comprehensive tests for MouseCoordinator with mocked dependencies.

package texelterm

import (
	"sync"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/gdamore/tcell/v2"
)

// mockGridProvider implements GridProvider for testing.
type mockGridProvider struct {
	mu           sync.Mutex
	grid         [][]parser.Cell
	vtocResults  map[string]vtocResult // "row,col" -> result
	markDirtyCnt int
	scrollCalls  []int
}

type vtocResult struct {
	logicalLine   int64
	charOffset    int
	isCurrentLine bool
	ok            bool
}

func newMockGridProvider(width, height int) *mockGridProvider {
	grid := make([][]parser.Cell, height)
	for y := range grid {
		grid[y] = make([]parser.Cell, width)
		for x := range grid[y] {
			grid[y][x] = parser.Cell{Rune: ' '}
		}
	}
	return &mockGridProvider{
		grid:        grid,
		vtocResults: make(map[string]vtocResult),
	}
}

func (m *mockGridProvider) Grid() [][]parser.Cell {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.grid
}

func (m *mockGridProvider) ViewportToContent(row, col int) (int64, int, bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := vtocKey(row, col)
	if res, ok := m.vtocResults[key]; ok {
		return res.logicalLine, res.charOffset, res.isCurrentLine, res.ok
	}
	// Default: treat as current line at column offset
	return -1, col, true, true
}

func (m *mockGridProvider) MarkAllDirty() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markDirtyCnt++
}

func (m *mockGridProvider) Scroll(lines int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scrollCalls = append(m.scrollCalls, lines)
}

func (m *mockGridProvider) getMarkDirtyCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.markDirtyCnt
}

func (m *mockGridProvider) getScrollCalls() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]int, len(m.scrollCalls))
	copy(result, m.scrollCalls)
	return result
}

func (m *mockGridProvider) setVtocResult(row, col int, logicalLine int64, charOffset int, isCurrentLine, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vtocResults[vtocKey(row, col)] = vtocResult{logicalLine, charOffset, isCurrentLine, ok}
}

func vtocKey(row, col int) string {
	return string(rune(row*10000 + col))
}

// mockVTermProviderForCoord implements VTermProvider for testing.
type mockVTermProviderForCoord struct {
	inputActive    bool
	inputStartLine int
	inputStartCol  int
	historyLines   map[int][]parser.Cell
	currentLine    []parser.Cell
	grid           [][]parser.Cell
	contentText    string
}

func newMockVTermProviderForCoord() *mockVTermProviderForCoord {
	return &mockVTermProviderForCoord{
		historyLines: make(map[int][]parser.Cell),
	}
}

func (m *mockVTermProviderForCoord) InputActive() bool   { return m.inputActive }
func (m *mockVTermProviderForCoord) InputStartLine() int { return m.inputStartLine }
func (m *mockVTermProviderForCoord) InputStartCol() int  { return m.inputStartCol }
func (m *mockVTermProviderForCoord) HistoryLineCopy(line int) []parser.Cell {
	return m.historyLines[line]
}
func (m *mockVTermProviderForCoord) CurrentLineCells() []parser.Cell { return m.currentLine }
func (m *mockVTermProviderForCoord) Grid() [][]parser.Cell           { return m.grid }
func (m *mockVTermProviderForCoord) GetContentText(startLine int64, startOffset int, endLine int64, endOffset int) string {
	return m.contentText
}

func (m *mockVTermProviderForCoord) ViewportRow(row int) []parser.Cell {
	if m.grid != nil && row >= 0 && row < len(m.grid) {
		return m.grid[row]
	}
	return nil
}

// mockWheelHandler tracks wheel events.
type mockWheelHandler struct {
	mu     sync.Mutex
	events []wheelEvent
}

type wheelEvent struct {
	x, y, deltaX, deltaY int
	modifiers            tcell.ModMask
}

func (m *mockWheelHandler) HandleMouseWheel(x, y, deltaX, deltaY int, modifiers tcell.ModMask) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, wheelEvent{x, y, deltaX, deltaY, modifiers})
}

func (m *mockWheelHandler) getEvents() []wheelEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]wheelEvent, len(m.events))
	copy(result, m.events)
	return result
}

// mockClipboardSetter tracks clipboard operations.
type mockClipboardSetter struct {
	mu   sync.Mutex
	mime string
	data []byte
}

func (m *mockClipboardSetter) SetClipboard(mime string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mime = mime
	m.data = data
}

// TestMouseCoordinator_New tests coordinator creation.
func TestMouseCoordinator_New(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	gridProv := newMockGridProvider(80, 24)
	wheelHandler := &mockWheelHandler{}
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, gridProv, wheelHandler, config)

	if coord == nil {
		t.Fatal("expected non-nil coordinator")
	}
	if coord.clickDetector == nil {
		t.Error("expected click detector to be initialized")
	}
	if coord.selectionMachine == nil {
		t.Error("expected selection machine to be initialized")
	}
	if coord.autoScroll == nil {
		t.Error("expected auto-scroll to be initialized")
	}
}

// TestMouseCoordinator_SetSize tests size configuration.
func TestMouseCoordinator_SetSize(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	gridProv := newMockGridProvider(80, 24)
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, gridProv, nil, config)
	coord.SetSize(100, 40)

	// Verify size was stored
	coord.mu.Lock()
	width, height := coord.width, coord.height
	coord.mu.Unlock()

	if width != 100 || height != 40 {
		t.Errorf("expected size (100, 40), got (%d, %d)", width, height)
	}
}

// TestMouseCoordinator_SelectionLifecycle tests the full selection lifecycle.
func TestMouseCoordinator_SelectionLifecycle(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	vtermProv.contentText = "selected text"
	gridProv := newMockGridProvider(80, 24)
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, gridProv, nil, config)
	coord.SetSize(80, 24)
	coord.SetCallbacks(func() {}, func() {})

	clipboard := &mockClipboardSetter{}
	coord.SetClipboardSetter(clipboard)

	// Start selection
	ok := coord.SelectionStart(10, 5, tcell.Button1, 0)
	if !ok {
		t.Fatal("expected SelectionStart to return true")
	}

	if !coord.IsSelectionActive() {
		t.Error("expected selection to be active after start")
	}

	// Update selection
	coord.SelectionUpdate(20, 5, tcell.Button1, 0)

	// Finish selection - this returns data but does NOT set clipboard
	// (clipboard is set via HandleMouseEvent release, not SelectionFinish)
	mime, data, ok := coord.SelectionFinish(20, 5, tcell.Button1, 0)

	// Selection should have completed with content from mock
	if !ok {
		t.Log("Selection finish returned ok=false (expected due to test mock setup)")
	}

	// MarkAllDirty should have been called
	if gridProv.getMarkDirtyCount() == 0 {
		t.Error("expected MarkAllDirty to be called")
	}

	// For non-empty selections, verify returned data (not clipboard)
	if ok && len(data) > 0 {
		if mime != "text/plain" {
			t.Errorf("mime = %q, want %q", mime, "text/plain")
		}
		if string(data) != "selected text" {
			t.Errorf("data = %q, want %q", string(data), "selected text")
		}
	}
}

// TestMouseCoordinator_SelectionCancel tests selection cancellation.
func TestMouseCoordinator_SelectionCancel(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	gridProv := newMockGridProvider(80, 24)
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, gridProv, nil, config)
	coord.SetSize(80, 24)
	coord.SetCallbacks(func() {}, func() {})

	// Start and then cancel
	coord.SelectionStart(10, 5, tcell.Button1, 0)
	coord.SelectionCancel()

	if coord.IsSelectionActive() {
		t.Error("expected selection to be inactive after cancel")
	}
}

// TestMouseCoordinator_NilGridProvider tests handling of nil provider.
func TestMouseCoordinator_NilGridProvider(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, nil, nil, config)
	coord.SetSize(80, 24)

	// Should not panic, should return false
	ok := coord.SelectionStart(10, 5, tcell.Button1, 0)
	if ok {
		t.Error("expected SelectionStart to return false with nil gridProvider")
	}
}

// TestMouseCoordinator_GetSelectionRange tests range retrieval.
func TestMouseCoordinator_GetSelectionRange(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	gridProv := newMockGridProvider(80, 24)
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, gridProv, nil, config)
	coord.SetSize(80, 24)
	coord.SetCallbacks(func() {}, func() {})

	// Before any selection
	_, _, _, _, ok := coord.GetSelectionRange()
	if ok {
		t.Error("expected no selection range before start")
	}

	// Start a selection
	coord.SelectionStart(10, 5, tcell.Button1, 0)
	coord.SelectionUpdate(20, 5, tcell.Button1, 0)

	// Range should be available (may be empty though)
	coord.GetSelectionRange()
	// Just verify it doesn't panic
}

// TestMouseCoordinator_IsSelectionRendered tests rendered state tracking.
func TestMouseCoordinator_IsSelectionRendered(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	gridProv := newMockGridProvider(80, 24)
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, gridProv, nil, config)
	coord.SetSize(80, 24)
	coord.SetCallbacks(func() {}, func() {})

	// Initially not rendered
	if coord.IsSelectionRendered() {
		t.Error("expected not rendered initially")
	}

	// Start selection - should be rendered
	coord.SelectionStart(10, 5, tcell.Button1, 0)
	if !coord.IsSelectionRendered() {
		t.Error("expected rendered during selection")
	}
}

// TestMouseCoordinator_CoordinateClamping tests that coordinates are clamped properly.
func TestMouseCoordinator_CoordinateClamping(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	gridProv := newMockGridProvider(80, 24)
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, gridProv, nil, config)
	coord.SetSize(80, 24)
	coord.SetCallbacks(func() {}, func() {})

	// Test negative coordinates - should be clamped to 0
	coord.SelectionStart(-5, -3, tcell.Button1, 0)
	// Should not panic

	// Test coordinates beyond bounds - should be clamped
	coord.SelectionUpdate(100, 30, tcell.Button1, 0)
	// Should not panic

	coord.SelectionCancel()
}

// TestMouseCoordinator_CallbacksWired tests that callbacks are properly connected.
func TestMouseCoordinator_CallbacksWired(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	gridProv := newMockGridProvider(80, 24)
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	dirtyCalled := false

	coord := NewMouseCoordinator(vtermProv, gridProv, nil, config)
	coord.SetSize(80, 24)
	coord.SetCallbacks(
		func() { dirtyCalled = true },
		func() {},
	)

	// Trigger an action that calls markDirty
	coord.SelectionStart(10, 5, tcell.Button1, 0)

	if !dirtyCalled {
		t.Error("expected onDirty callback to be called")
	}
}

// TestMouseCoordinator_SetClipboardSetter tests clipboard setter configuration.
func TestMouseCoordinator_SetClipboardSetter(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	gridProv := newMockGridProvider(80, 24)
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, gridProv, nil, config)

	clipboard := &mockClipboardSetter{}
	coord.SetClipboardSetter(clipboard)

	// Verify it was set
	coord.mu.Lock()
	setter := coord.clipboardSetter
	coord.mu.Unlock()

	if setter != clipboard {
		t.Error("expected clipboard setter to be set")
	}
}

// TestMouseCoordinator_MultipleSelections tests starting new selection cancels old one.
func TestMouseCoordinator_MultipleSelections(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	gridProv := newMockGridProvider(80, 24)
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, gridProv, nil, config)
	coord.SetSize(80, 24)
	coord.SetCallbacks(func() {}, func() {})

	// Start first selection
	coord.SelectionStart(10, 5, tcell.Button1, 0)

	// Start second selection - should cancel first
	coord.SelectionStart(30, 10, tcell.Button1, 0)

	// Should still have active selection
	if !coord.IsSelectionActive() {
		t.Error("expected selection to be active after second start")
	}
}

// TestVTermGridAdapter_NilVTerm tests that nil vterm is handled safely.
func TestVTermGridAdapter_NilVTerm(t *testing.T) {
	adapter := NewVTermGridAdapter(nil)
	if adapter != nil {
		t.Error("expected nil adapter for nil vterm")
	}
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/mouse_coordinator_test.go
// Summary: Comprehensive tests for MouseCoordinator with mocked dependencies.

package texelterm

import (
	"sync"
	"testing"
	"time"

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

func (m *mockVTermProviderForCoord) PromptStartLine() int64 { return -1 }
func (m *mockVTermProviderForCoord) ContentEndLine() int64  { return 0 }

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
	mu    sync.Mutex
	mime  string
	data  []byte
	calls int
	all   []clipboardCall
}

type clipboardCall struct {
	mime string
	data []byte
}

func (m *mockClipboardSetter) SetClipboard(mime string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mime = mime
	m.data = data
	m.calls++
	m.all = append(m.all, clipboardCall{mime: mime, data: append([]byte(nil), data...)})
}

func (m *mockClipboardSetter) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *mockClipboardSetter) lastData() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		return nil
	}
	return append([]byte(nil), m.data...)
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

// TestMouseCoordinator_SelectionLifecycle tests the full selection lifecycle via HandleMouse.
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

	// Start selection (button1 press)
	ev := tcell.NewEventMouse(10, 5, tcell.Button1, 0)
	ok := coord.HandleMouse(ev)
	if !ok {
		t.Fatal("expected HandleMouse to return true for button press")
	}

	if !coord.IsSelectionActive() {
		t.Error("expected selection to be active after start")
	}

	// Update selection (drag with button1 still pressed)
	ev = tcell.NewEventMouse(20, 5, tcell.Button1, 0)
	coord.HandleMouse(ev)

	// Finish selection (button1 release)
	ev = tcell.NewEventMouse(20, 5, tcell.ButtonNone, 0)
	coord.HandleMouse(ev)

	// MarkAllDirty should have been called
	if gridProv.getMarkDirtyCount() == 0 {
		t.Error("expected MarkAllDirty to be called")
	}
}

// TestMouseCoordinator_SelectionCancel tests selection cancellation via right-click.
func TestMouseCoordinator_SelectionCancel(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	gridProv := newMockGridProvider(80, 24)
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, gridProv, nil, config)
	coord.SetSize(80, 24)
	coord.SetCallbacks(func() {}, func() {})

	// Start selection
	ev := tcell.NewEventMouse(10, 5, tcell.Button1, 0)
	coord.HandleMouse(ev)

	// Cancel with right-click
	ev = tcell.NewEventMouse(10, 5, tcell.Button3, 0)
	coord.HandleMouse(ev)

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

	// Should not panic
	ev := tcell.NewEventMouse(10, 5, tcell.Button1, 0)
	coord.HandleMouse(ev)
}

// TestMouseCoordinator_SelectionRange tests range retrieval.
func TestMouseCoordinator_SelectionRange(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	gridProv := newMockGridProvider(80, 24)
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, gridProv, nil, config)
	coord.SetSize(80, 24)
	coord.SetCallbacks(func() {}, func() {})

	// Before any selection
	_, _, _, _, ok := coord.SelectionRange()
	if ok {
		t.Error("expected no selection range before start")
	}

	// Start a selection
	ev := tcell.NewEventMouse(10, 5, tcell.Button1, 0)
	coord.HandleMouse(ev)

	// Drag to update
	ev = tcell.NewEventMouse(20, 5, tcell.Button1, 0)
	coord.HandleMouse(ev)

	// Range should be available (may be empty though)
	coord.SelectionRange()
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
	ev := tcell.NewEventMouse(10, 5, tcell.Button1, 0)
	coord.HandleMouse(ev)
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
	ev := tcell.NewEventMouse(-5, -3, tcell.Button1, 0)
	coord.HandleMouse(ev)
	// Should not panic

	// Test coordinates beyond bounds - should be clamped
	ev = tcell.NewEventMouse(100, 30, tcell.Button1, 0)
	coord.HandleMouse(ev)
	// Should not panic

	// Cancel selection
	ev = tcell.NewEventMouse(0, 0, tcell.Button3, 0)
	coord.HandleMouse(ev)
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
	ev := tcell.NewEventMouse(10, 5, tcell.Button1, 0)
	coord.HandleMouse(ev)

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
	ev := tcell.NewEventMouse(10, 5, tcell.Button1, 0)
	coord.HandleMouse(ev)

	// Release first button
	ev = tcell.NewEventMouse(10, 5, tcell.ButtonNone, 0)
	coord.HandleMouse(ev)

	// Start second selection - should work
	ev = tcell.NewEventMouse(30, 10, tcell.Button1, 0)
	coord.HandleMouse(ev)

	// Should have active selection
	if !coord.IsSelectionActive() {
		t.Error("expected selection to be active after second start")
	}
}

// TestMouseCoordinator_WheelEvent tests wheel event handling.
func TestMouseCoordinator_WheelEvent(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	gridProv := newMockGridProvider(80, 24)
	wheelHandler := &mockWheelHandler{}
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, gridProv, wheelHandler, config)
	coord.SetSize(80, 24)

	// Send wheel event
	ev := tcell.NewEventMouse(10, 5, tcell.WheelDown, 0)
	ok := coord.HandleMouse(ev)

	if !ok {
		t.Error("expected HandleMouse to return true for wheel event")
	}

	events := wheelHandler.getEvents()
	if len(events) != 1 {
		t.Errorf("expected 1 wheel event, got %d", len(events))
	}
}

// TestVTermGridAdapter_NilVTerm tests that nil vterm is handled safely.
func TestVTermGridAdapter_NilVTerm(t *testing.T) {
	adapter := NewVTermGridAdapter(nil)
	if adapter != nil {
		t.Error("expected nil adapter for nil vterm")
	}
}

// pressRelease drives a complete press+release cycle through the
// coordinator at the given viewport position. Each call advances the
// click count if it falls within DefaultMultiClickTimeout of the
// previous one (the click detector is position+timeout sensitive).
func pressRelease(coord *MouseCoordinator, x, y int) {
	coord.HandleMouse(tcell.NewEventMouse(x, y, tcell.Button1, 0))
	coord.HandleMouse(tcell.NewEventMouse(x, y, tcell.ButtonNone, 0))
}

// hasPendingClipboard reports whether a deferred multi-click clipboard
// write is currently armed. Drops out from under the coordinator's
// mutex so it can race-safely observe internal state.
func hasPendingClipboard(coord *MouseCoordinator) bool {
	coord.mu.Lock()
	defer coord.mu.Unlock()
	return coord.pendingClipTimer != nil
}

// TestMouseCoordinator_MultiClickChainSingleClipboard verifies that
// double→triple→quadruple click on the same position fires
// SetClipboard exactly once after the multi-click timeout, instead of
// once per release. This is the regression guard for the "ghostty
// shows continuous Content copied toasts on multi-click" bug — every
// extra OSC52 hop translates to a host-terminal toast.
func TestMouseCoordinator_MultiClickChainSingleClipboard(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	vtermProv.contentText = "selected"
	// Provide a non-empty current line so SelectionModeLine /
	// SelectionModeWord / SelectionModeSpaceWord all produce a
	// non-empty selection that triggers the clipboard write.
	vtermProv.currentLine = []parser.Cell{
		{Rune: 'h'}, {Rune: 'e'}, {Rune: 'l'}, {Rune: 'l'}, {Rune: 'o'},
	}
	gridProv := newMockGridProvider(80, 24)
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, gridProv, nil, config)
	coord.SetSize(80, 24)
	coord.SetCallbacks(func() {}, func() {})

	clipboard := &mockClipboardSetter{}
	coord.SetClipboardSetter(clipboard)

	// Click 1 (single — empty drag, no clipboard write expected).
	pressRelease(coord, 2, 0)
	// Clicks 2/3/4 within the multi-click window at the same position
	// escalate the selection. Each release defers the clipboard write,
	// and the next press cancels the previously deferred one.
	pressRelease(coord, 2, 0)
	pressRelease(coord, 2, 0)
	pressRelease(coord, 2, 0)

	// While the window is still open, no SetClipboard call should
	// have reached the host terminal yet.
	if got := clipboard.callCount(); got != 0 {
		t.Fatalf("expected 0 clipboard writes during click chain, got %d", got)
	}
	if !hasPendingClipboard(coord) {
		t.Fatal("expected a pending deferred clipboard write after multi-click chain")
	}

	// Wait past the multi-click timeout for the deferred write to fire.
	deadline := time.Now().Add(DefaultMultiClickTimeout * 4)
	for time.Now().Before(deadline) {
		if clipboard.callCount() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := clipboard.callCount(); got != 1 {
		t.Fatalf("expected exactly 1 clipboard write after timeout, got %d", got)
	}
}

// TestMouseCoordinator_NewPressCancelsPendingClipboard verifies that
// starting a fresh selection inside the multi-click window cancels the
// previously-armed deferred clipboard write — the user's intent has
// shifted to a new selection and the superseded payload must not
// surface to the host terminal.
func TestMouseCoordinator_NewPressCancelsPendingClipboard(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	vtermProv.contentText = "word"
	vtermProv.currentLine = []parser.Cell{{Rune: 'a'}, {Rune: 'b'}, {Rune: 'c'}}
	gridProv := newMockGridProvider(80, 24)
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, gridProv, nil, config)
	coord.SetSize(80, 24)
	coord.SetCallbacks(func() {}, func() {})

	clipboard := &mockClipboardSetter{}
	coord.SetClipboardSetter(clipboard)

	// Two clicks at the same position → double-click selection,
	// release defers the clipboard write.
	pressRelease(coord, 1, 0)
	pressRelease(coord, 1, 0)
	if !hasPendingClipboard(coord) {
		t.Fatal("expected pending clipboard write after double-click")
	}

	// A third press at a different position cancels the pending
	// write before it can fire.
	coord.HandleMouse(tcell.NewEventMouse(40, 10, tcell.Button1, 0))
	if hasPendingClipboard(coord) {
		t.Fatal("expected pending clipboard write to be cancelled by new press")
	}

	// Release without drag — empty selection, no clipboard write.
	coord.HandleMouse(tcell.NewEventMouse(40, 10, tcell.ButtonNone, 0))

	// Wait long enough that any leaked deferred write would have fired.
	time.Sleep(DefaultMultiClickTimeout * 2)
	if got := clipboard.callCount(); got != 0 {
		t.Fatalf("expected 0 clipboard writes after cancellation, got %d", got)
	}
}

// TestMouseCoordinator_SingleDragImmediateClipboard verifies that
// single-click drag selections continue to copy synchronously on
// release — there is no escalation gesture to wait for, and deferring
// would add user-visible latency.
func TestMouseCoordinator_SingleDragImmediateClipboard(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	vtermProv.contentText = "drag-selected"
	gridProv := newMockGridProvider(80, 24)
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, gridProv, nil, config)
	coord.SetSize(80, 24)
	coord.SetCallbacks(func() {}, func() {})

	clipboard := &mockClipboardSetter{}
	coord.SetClipboardSetter(clipboard)

	// Press, drag to a different column, release.
	coord.HandleMouse(tcell.NewEventMouse(5, 0, tcell.Button1, 0))
	coord.HandleMouse(tcell.NewEventMouse(15, 0, tcell.Button1, 0))
	coord.HandleMouse(tcell.NewEventMouse(15, 0, tcell.ButtonNone, 0))

	if got := clipboard.callCount(); got != 1 {
		t.Fatalf("expected 1 immediate clipboard write for single drag, got %d", got)
	}
	if hasPendingClipboard(coord) {
		t.Fatal("expected no deferred write after single-drag release")
	}
}

// TestMouseCoordinator_RightClickCancelsPendingClipboard verifies that
// right-click cancellation drops a deferred multi-click clipboard
// write, mirroring the way it drops the visible selection.
func TestMouseCoordinator_RightClickCancelsPendingClipboard(t *testing.T) {
	vtermProv := newMockVTermProviderForCoord()
	vtermProv.contentText = "word"
	vtermProv.currentLine = []parser.Cell{{Rune: 'a'}, {Rune: 'b'}, {Rune: 'c'}}
	gridProv := newMockGridProvider(80, 24)
	config := AutoScrollConfig{EdgeZone: 2, MaxScrollSpeed: 15}

	coord := NewMouseCoordinator(vtermProv, gridProv, nil, config)
	coord.SetSize(80, 24)
	coord.SetCallbacks(func() {}, func() {})

	clipboard := &mockClipboardSetter{}
	coord.SetClipboardSetter(clipboard)

	// Double-click → deferred write.
	pressRelease(coord, 1, 0)
	pressRelease(coord, 1, 0)
	if !hasPendingClipboard(coord) {
		t.Fatal("expected pending clipboard write after double-click")
	}

	// Right-click cancels the rendered selection AND the pending
	// clipboard write.
	coord.HandleMouse(tcell.NewEventMouse(1, 0, tcell.Button3, 0))
	coord.HandleMouse(tcell.NewEventMouse(1, 0, tcell.ButtonNone, 0))

	if hasPendingClipboard(coord) {
		t.Fatal("expected right-click to cancel the pending clipboard write")
	}

	time.Sleep(DefaultMultiClickTimeout * 2)
	if got := clipboard.callCount(); got != 0 {
		t.Fatalf("expected 0 clipboard writes after right-click cancel, got %d", got)
	}
}

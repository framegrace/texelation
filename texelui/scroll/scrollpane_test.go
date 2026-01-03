// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package scroll

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"texelation/texel"
	"texelation/texelui/core"
)

// mockWidget is a test widget for ScrollPane testing.
type mockWidget struct {
	core.BaseWidget
	focused bool
}

func newMockWidget(x, y, w, h int, focusable bool) *mockWidget {
	m := &mockWidget{}
	m.SetPosition(x, y)
	m.Resize(w, h)
	m.SetFocusable(focusable)
	return m
}

func (m *mockWidget) Draw(p *core.Painter) {
	// Fill with 'X' for visibility in tests
	for y := 0; y < m.Rect.H; y++ {
		for x := 0; x < m.Rect.W; x++ {
			p.SetCell(m.Rect.X+x, m.Rect.Y+y, 'X', tcell.StyleDefault)
		}
	}
}

func (m *mockWidget) Focus() {
	m.BaseWidget.Focus()
	m.focused = true
}

func (m *mockWidget) Blur() {
	m.BaseWidget.Blur()
	m.focused = false
}

func (m *mockWidget) IsFocused() bool {
	return m.focused
}

// mockContainer is a container widget for testing focus traversal.
type mockContainer struct {
	core.BaseWidget
	children []core.Widget
}

func newMockContainer(x, y, w, h int) *mockContainer {
	c := &mockContainer{}
	c.SetPosition(x, y)
	c.Resize(w, h)
	return c
}

func (c *mockContainer) AddChild(w core.Widget) {
	c.children = append(c.children, w)
}

func (c *mockContainer) Draw(p *core.Painter) {
	for _, child := range c.children {
		child.Draw(p)
	}
}

func (c *mockContainer) VisitChildren(f func(core.Widget)) {
	for _, child := range c.children {
		f(child)
	}
}

func TestNewScrollPane(t *testing.T) {
	sp := NewScrollPane(10, 5, 40, 20, tcell.StyleDefault)

	x, y := sp.Position()
	if x != 10 || y != 5 {
		t.Errorf("Position = (%d, %d), want (10, 5)", x, y)
	}

	w, h := sp.Size()
	if w != 40 || h != 20 {
		t.Errorf("Size = (%d, %d), want (40, 20)", w, h)
	}

	if sp.ScrollOffset() != 0 {
		t.Errorf("Initial ScrollOffset = %d, want 0", sp.ScrollOffset())
	}

	if sp.CanScroll() {
		t.Error("Should not be scrollable without content")
	}
}

func TestScrollPane_SetChild(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)

	// Set child that's taller than viewport
	child := newMockWidget(0, 0, 40, 30, false)
	sp.SetChild(child)

	if sp.ContentHeight() != 30 {
		t.Errorf("ContentHeight = %d, want 30", sp.ContentHeight())
	}

	if !sp.CanScroll() {
		t.Error("Should be scrollable with tall content")
	}

	if sp.GetChild() != child {
		t.Error("GetChild() should return the set child")
	}
}

func TestScrollPane_SetContentHeight(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)
	sp.SetContentHeight(50)

	if sp.ContentHeight() != 50 {
		t.Errorf("ContentHeight = %d, want 50", sp.ContentHeight())
	}

	state := sp.State()
	if state.ContentHeight != 50 {
		t.Errorf("State.ContentHeight = %d, want 50", state.ContentHeight)
	}
}

func TestScrollPane_ScrollBy(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)
	sp.SetContentHeight(100)

	// Scroll down
	sp.ScrollBy(20)
	if sp.ScrollOffset() != 20 {
		t.Errorf("ScrollOffset = %d, want 20", sp.ScrollOffset())
	}

	// Scroll up
	sp.ScrollBy(-5)
	if sp.ScrollOffset() != 15 {
		t.Errorf("ScrollOffset = %d, want 15", sp.ScrollOffset())
	}

	// Scroll past bottom (should clamp to max)
	sp.ScrollBy(100)
	if sp.ScrollOffset() != 90 { // 100 - 10 = 90
		t.Errorf("ScrollOffset = %d, want 90", sp.ScrollOffset())
	}

	// Scroll past top (should clamp to 0)
	sp.ScrollBy(-200)
	if sp.ScrollOffset() != 0 {
		t.Errorf("ScrollOffset = %d, want 0", sp.ScrollOffset())
	}
}

func TestScrollPane_ScrollTo(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)
	sp.SetContentHeight(100)

	// Scroll to row 50
	sp.ScrollTo(50)
	// ScrollTo uses minimal movement, so row 50 should be at bottom
	// offset = 50 - 10 + 1 = 41
	if sp.ScrollOffset() != 41 {
		t.Errorf("ScrollOffset = %d, want 41", sp.ScrollOffset())
	}

	// Row already visible shouldn't change offset
	sp.ScrollTo(45) // 45 is in [41, 51)
	if sp.ScrollOffset() != 41 {
		t.Errorf("ScrollOffset = %d, want 41 (no change)", sp.ScrollOffset())
	}
}

func TestScrollPane_ScrollToTopBottom(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)
	sp.SetContentHeight(100)

	sp.ScrollBy(50) // Scroll to middle

	sp.ScrollToTop()
	if sp.ScrollOffset() != 0 {
		t.Errorf("ScrollOffset = %d, want 0", sp.ScrollOffset())
	}

	sp.ScrollToBottom()
	if sp.ScrollOffset() != 90 { // 100 - 10
		t.Errorf("ScrollOffset = %d, want 90", sp.ScrollOffset())
	}
}

func TestScrollPane_ScrollToCentered(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)
	sp.SetContentHeight(100)

	sp.ScrollToCentered(50)
	// Center row 50: offset = 50 - 10/2 = 45
	if sp.ScrollOffset() != 45 {
		t.Errorf("ScrollOffset = %d, want 45", sp.ScrollOffset())
	}
}

func TestScrollPane_CanScroll(t *testing.T) {
	tests := []struct {
		name        string
		contentH    int
		viewportH   int
		scrollTo    int
		wantUp      bool
		wantDown    bool
		wantScroll  bool
	}{
		{
			name:        "content fits",
			contentH:    5,
			viewportH:   10,
			scrollTo:    0,
			wantUp:      false,
			wantDown:    false,
			wantScroll:  false,
		},
		{
			name:        "at top",
			contentH:    100,
			viewportH:   10,
			scrollTo:    0,
			wantUp:      false,
			wantDown:    true,
			wantScroll:  true,
		},
		{
			name:        "at bottom",
			contentH:    100,
			viewportH:   10,
			scrollTo:    90,
			wantUp:      true,
			wantDown:    false,
			wantScroll:  true,
		},
		{
			name:        "in middle",
			contentH:    100,
			viewportH:   10,
			scrollTo:    50,
			wantUp:      true,
			wantDown:    true,
			wantScroll:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sp := NewScrollPane(0, 0, 40, tt.viewportH, tcell.StyleDefault)
			sp.SetContentHeight(tt.contentH)
			if tt.scrollTo > 0 {
				sp.ScrollBy(tt.scrollTo)
			}

			if got := sp.CanScrollUp(); got != tt.wantUp {
				t.Errorf("CanScrollUp = %v, want %v", got, tt.wantUp)
			}
			if got := sp.CanScrollDown(); got != tt.wantDown {
				t.Errorf("CanScrollDown = %v, want %v", got, tt.wantDown)
			}
			if got := sp.CanScroll(); got != tt.wantScroll {
				t.Errorf("CanScroll = %v, want %v", got, tt.wantScroll)
			}
		})
	}
}

func TestScrollPane_HandleKey_PageUpDown(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)
	sp.SetContentHeight(100)
	sp.ScrollBy(50) // Start in middle

	// Page up
	ev := tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModNone)
	if !sp.HandleKey(ev) {
		t.Error("HandleKey should return true for PgUp")
	}
	if sp.ScrollOffset() != 40 { // 50 - 10
		t.Errorf("After PgUp: ScrollOffset = %d, want 40", sp.ScrollOffset())
	}

	// Page down
	ev = tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone)
	if !sp.HandleKey(ev) {
		t.Error("HandleKey should return true for PgDn")
	}
	if sp.ScrollOffset() != 50 { // 40 + 10
		t.Errorf("After PgDn: ScrollOffset = %d, want 50", sp.ScrollOffset())
	}
}

func TestScrollPane_HandleKey_CtrlHomeEnd(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)
	sp.SetContentHeight(100)
	sp.ScrollBy(50)

	// Ctrl+Home -> top
	ev := tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModCtrl)
	if !sp.HandleKey(ev) {
		t.Error("HandleKey should return true for Ctrl+Home")
	}
	if sp.ScrollOffset() != 0 {
		t.Errorf("After Ctrl+Home: ScrollOffset = %d, want 0", sp.ScrollOffset())
	}

	// Ctrl+End -> bottom
	ev = tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModCtrl)
	if !sp.HandleKey(ev) {
		t.Error("HandleKey should return true for Ctrl+End")
	}
	if sp.ScrollOffset() != 90 {
		t.Errorf("After Ctrl+End: ScrollOffset = %d, want 90", sp.ScrollOffset())
	}
}

func TestScrollPane_HandleMouse_Wheel(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)
	sp.SetContentHeight(100)
	sp.ScrollBy(50)

	// Wheel up
	ev := tcell.NewEventMouse(5, 5, tcell.WheelUp, tcell.ModNone)
	if !sp.HandleMouse(ev) {
		t.Error("HandleMouse should return true for WheelUp")
	}
	if sp.ScrollOffset() != 47 { // 50 - 3
		t.Errorf("After WheelUp: ScrollOffset = %d, want 47", sp.ScrollOffset())
	}

	// Wheel down
	ev = tcell.NewEventMouse(5, 5, tcell.WheelDown, tcell.ModNone)
	if !sp.HandleMouse(ev) {
		t.Error("HandleMouse should return true for WheelDown")
	}
	if sp.ScrollOffset() != 50 { // 47 + 3
		t.Errorf("After WheelDown: ScrollOffset = %d, want 50", sp.ScrollOffset())
	}
}

func TestScrollPane_HandleMouse_OutsideBounds(t *testing.T) {
	sp := NewScrollPane(10, 10, 20, 10, tcell.StyleDefault)
	sp.SetContentHeight(100)

	// Mouse event outside bounds
	ev := tcell.NewEventMouse(5, 5, tcell.WheelUp, tcell.ModNone)
	if sp.HandleMouse(ev) {
		t.Error("HandleMouse should return false for event outside bounds")
	}
}

func TestScrollPane_EnsureFocusedVisible(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)

	// Create a container with focusable child at row 50
	container := newMockContainer(0, 0, 40, 100)
	widget := newMockWidget(0, 50, 40, 5, true)
	widget.Focus()
	container.AddChild(widget)

	sp.SetChild(container)
	sp.SetContentHeight(100)

	// Initially at top, focused widget is not visible
	sp.EnsureFocusedVisible()

	// Should have scrolled to make row 50 visible
	state := sp.State()
	if !state.IsRowVisible(50) {
		t.Errorf("Row 50 should be visible after EnsureFocusedVisible, offset=%d", sp.ScrollOffset())
	}
}

func TestScrollPane_VisitChildren(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)
	child := newMockWidget(0, 0, 40, 30, false)
	sp.SetChild(child)

	count := 0
	sp.VisitChildren(func(w core.Widget) {
		count++
		if w != child {
			t.Error("VisitChildren should visit the child")
		}
	})

	if count != 1 {
		t.Errorf("VisitChildren visited %d children, want 1", count)
	}
}

func TestScrollPane_VisitChildren_NoChild(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)

	count := 0
	sp.VisitChildren(func(w core.Widget) {
		count++
	})

	if count != 0 {
		t.Errorf("VisitChildren visited %d children, want 0", count)
	}
}

func TestScrollPane_WidgetAt(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)
	child := newMockWidget(5, 0, 30, 20, false)
	sp.SetChild(child)

	// Point on child - ScrollPane returns itself to handle mouse events
	w := sp.WidgetAt(10, 5)
	if w != sp {
		t.Error("WidgetAt should return scroll pane (handles mouse routing internally)")
	}

	// Point on scroll pane but not on child
	w = sp.WidgetAt(2, 5)
	if w != sp {
		t.Error("WidgetAt should return scroll pane for point in bounds")
	}

	// Point outside scroll pane
	w = sp.WidgetAt(50, 50)
	if w != nil {
		t.Error("WidgetAt should return nil for point outside")
	}
}

func TestScrollPane_Draw(t *testing.T) {
	buf := createTestBuffer(50, 20)
	painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 50, H: 20})

	sp := NewScrollPane(5, 2, 40, 10, tcell.StyleDefault)
	sp.SetContentHeight(30)
	sp.ScrollBy(10) // Scroll down 10 rows

	child := newMockWidget(5, 2, 40, 30, false)
	sp.SetChild(child)

	sp.Draw(painter)

	// Should have scrollbar with arrows (default behavior)
	// Layout: [▲][track area (8 rows)][▼]
	// With content=30, viewport=10, offset=10, trackHeight=8:
	// - thumbSize = (10 * 8) / 30 = 2 rows
	// - scrollableContent = 30 - 10 = 20
	// - scrollableTrack = 8 - 2 = 6
	// - thumbStart = (10 * 6) / 20 = 3 (within track area)
	// - thumbEnd = 3 + 2 = 5
	scrollbarX := 5 + 40 - 1 // Right edge

	// Row 0: Up arrow
	if ch := getCell(buf, scrollbarX, 2); ch != DefaultUpGlyph {
		t.Errorf("Up arrow at row 0: got %c, want %c", ch, DefaultUpGlyph)
	}

	// Track area rows 0-2 (screen rows 1-3): Track before thumb
	for row := 1; row <= 3; row++ {
		y := 2 + row
		if ch := getCell(buf, scrollbarX, y); ch != DefaultTrackChar {
			t.Errorf("Track at row %d: got %c, want %c", row, ch, DefaultTrackChar)
		}
	}

	// Track area rows 3-4 (screen rows 4-5): Thumb
	for row := 4; row <= 5; row++ {
		y := 2 + row
		if ch := getCell(buf, scrollbarX, y); ch != DefaultThumbChar {
			t.Errorf("Thumb at row %d: got %c, want %c", row, ch, DefaultThumbChar)
		}
	}

	// Track area rows 5-7 (screen rows 6-8): Track after thumb
	for row := 6; row <= 8; row++ {
		y := 2 + row
		if ch := getCell(buf, scrollbarX, y); ch != DefaultTrackChar {
			t.Errorf("Track at row %d: got %c, want %c", row, ch, DefaultTrackChar)
		}
	}

	// Row 9: Down arrow
	if ch := getCell(buf, scrollbarX, 2+9); ch != DefaultDownGlyph {
		t.Errorf("Down arrow at row 9: got %c, want %c", ch, DefaultDownGlyph)
	}
}

func TestScrollPane_Draw_NoIndicators(t *testing.T) {
	buf := createTestBuffer(50, 20)
	painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 50, H: 20})

	sp := NewScrollPane(5, 2, 40, 10, tcell.StyleDefault)
	sp.SetContentHeight(30)
	sp.ScrollBy(10)
	sp.ShowIndicators(false) // Disable indicators

	child := newMockWidget(5, 2, 40, 30, false)
	sp.SetChild(child)

	sp.Draw(painter)

	// Should NOT have scroll indicators
	upX, upY := 5+40-1, 2
	if ch := getCell(buf, upX, upY); ch == DefaultUpGlyph {
		t.Error("Up indicator should not be drawn when ShowIndicators is false")
	}
}

func TestScrollPane_Draw_ArrowsNotOverwrittenByThumb(t *testing.T) {
	// Test that arrows are never overwritten by thumb at any scroll position
	testCases := []struct {
		name          string
		contentHeight int
		viewportH     int
		scrollOffset  int
	}{
		{"at_top", 100, 10, 0},              // Thumb at top, only down arrow
		{"near_top", 100, 10, 5},            // Thumb near top
		{"middle", 100, 10, 45},             // Thumb in middle
		{"near_bottom", 100, 10, 85},        // Thumb near bottom
		{"at_bottom", 100, 10, 90},          // Thumb at bottom, only up arrow
		{"small_viewport", 100, 5, 50},      // Small viewport
		{"large_content", 1000, 20, 500},    // Large content
		{"minimal_scroll", 15, 10, 3},       // Minimal scrollable content
		{"tiny_viewport", 50, 3, 25},        // Very small viewport (3 rows)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			buf := createTestBuffer(50, tc.viewportH+5)
			painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 50, H: tc.viewportH + 5})

			sp := NewScrollPane(5, 2, 40, tc.viewportH, tcell.StyleDefault)
			sp.SetContentHeight(tc.contentHeight)
			sp.ScrollBy(tc.scrollOffset)

			child := newMockWidget(5, 2, 40, tc.contentHeight, false)
			sp.SetChild(child)

			sp.Draw(painter)

			scrollbarX := 5 + 40 - 1 // Right edge
			topY := 2
			bottomY := 2 + tc.viewportH - 1

			// Arrows are always drawn on top as visual indicators
			// Check that arrows are present at top and bottom
			chTop := getCell(buf, scrollbarX, topY)
			if chTop != DefaultUpGlyph {
				t.Errorf("Up arrow at top missing: got %c, want %c", chTop, DefaultUpGlyph)
			}

			chBottom := getCell(buf, scrollbarX, bottomY)
			if chBottom != DefaultDownGlyph {
				t.Errorf("Down arrow at bottom missing: got %c, want %c", chBottom, DefaultDownGlyph)
			}
		})
	}
}

func TestScrollPane_Draw_ScrollbarLayout(t *testing.T) {
	// Visual test - prints the scrollbar layout for debugging
	buf := createTestBuffer(50, 15)
	painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 50, H: 15})

	sp := NewScrollPane(5, 2, 40, 10, tcell.StyleDefault)
	sp.SetContentHeight(100)
	sp.ScrollBy(45) // Middle position

	child := newMockWidget(5, 2, 40, 100, false)
	sp.SetChild(child)

	sp.Draw(painter)

	scrollbarX := 5 + 40 - 1 // Right edge

	// Print the scrollbar column for debugging
	t.Log("Scrollbar layout (viewport rows 0-9):")
	for row := 0; row < 10; row++ {
		y := 2 + row
		ch := getCell(buf, scrollbarX, y)
		var desc string
		switch ch {
		case DefaultUpGlyph:
			desc = "UP_ARROW"
		case DefaultDownGlyph:
			desc = "DOWN_ARROW"
		case DefaultThumbChar:
			desc = "THUMB"
		case DefaultTrackChar:
			desc = "TRACK"
		default:
			desc = "OTHER"
		}
		t.Logf("  Row %d (y=%d): %c (%s)", row, y, ch, desc)
	}

	// Verify structure: should be [▲][track/thumb area][▼]
	topCh := getCell(buf, scrollbarX, 2)
	bottomCh := getCell(buf, scrollbarX, 11)

	if topCh != DefaultUpGlyph {
		t.Errorf("Top should be up arrow, got %c", topCh)
	}
	if bottomCh != DefaultDownGlyph {
		t.Errorf("Bottom should be down arrow, got %c", bottomCh)
	}
}

func TestScrollPane_Draw_ThumbAtExtremes(t *testing.T) {
	// Test that arrows are always shown and thumb never overlaps them
	testCases := []struct {
		name   string
		offset int
	}{
		{"at_top", 0},
		{"at_bottom", 90},
		{"one_from_top", 1},
		{"one_from_bottom", 89},
		{"middle", 45},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			buf := createTestBuffer(50, 15)
			painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 50, H: 15})

			sp := NewScrollPane(5, 2, 40, 10, tcell.StyleDefault)
			sp.SetContentHeight(100) // viewport=10, content=100, maxOffset=90
			sp.ScrollBy(tc.offset)

			child := newMockWidget(5, 2, 40, 100, false)
			sp.SetChild(child)

			sp.Draw(painter)

			scrollbarX := 5 + 40 - 1 // Right edge
			topY := 2
			bottomY := 11

			topCh := getCell(buf, scrollbarX, topY)
			bottomCh := getCell(buf, scrollbarX, bottomY)

			t.Logf("At offset %d: top=%c bottom=%c", tc.offset, topCh, bottomCh)

			// Arrows should ALWAYS be shown (as click targets)
			if topCh != DefaultUpGlyph {
				t.Errorf("Up arrow should always be shown at top, got %c", topCh)
			}
			if bottomCh != DefaultDownGlyph {
				t.Errorf("Down arrow should always be shown at bottom, got %c", bottomCh)
			}

			// Thumb should never be at arrow positions
			if topCh == DefaultThumbChar {
				t.Error("Thumb should never appear at up arrow position")
			}
			if bottomCh == DefaultThumbChar {
				t.Error("Thumb should never appear at down arrow position")
			}
		})
	}
}

func TestScrollPane_Invalidation(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)
	sp.SetContentHeight(100)

	invalidated := false
	sp.SetInvalidator(func(r core.Rect) {
		invalidated = true
	})

	sp.ScrollBy(10)
	if !invalidated {
		t.Error("ScrollBy should trigger invalidation")
	}

	invalidated = false
	sp.ScrollBy(0) // No actual change
	// Implementation may still invalidate; behavior is implementation-specific
}

func TestScrollPane_ChildInvalidator(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)

	invalidatorCalled := false
	child := newMockWidget(0, 0, 40, 30, false)

	// Set invalidator before child
	sp.SetInvalidator(func(r core.Rect) {
		invalidatorCalled = true
	})
	sp.SetChild(child)

	// Trigger invalidation via scroll
	sp.ScrollBy(10)

	if !invalidatorCalled {
		t.Error("Invalidator should have been called")
	}
}

func TestScrollPane_Resize(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)
	sp.SetContentHeight(100)
	sp.ScrollBy(50)

	// Resize viewport
	sp.Resize(40, 20)

	w, h := sp.Size()
	if w != 40 || h != 20 {
		t.Errorf("Size = (%d, %d), want (40, 20)", w, h)
	}

	// State should update viewport height
	state := sp.State()
	if state.ViewportHeight != 20 {
		t.Errorf("State.ViewportHeight = %d, want 20", state.ViewportHeight)
	}

	// Offset should remain valid (50 is still valid for 100-20=80 max)
	if sp.ScrollOffset() != 50 {
		t.Errorf("ScrollOffset = %d, want 50", sp.ScrollOffset())
	}
}

func TestScrollPane_Resize_ClampOffset(t *testing.T) {
	sp := NewScrollPane(0, 0, 40, 10, tcell.StyleDefault)
	sp.SetContentHeight(100)
	sp.ScrollToBottom() // offset = 90

	// Resize to larger viewport
	sp.Resize(40, 50)

	// Max offset now is 100 - 50 = 50, so offset should be clamped
	state := sp.State()
	if state.Offset > 50 {
		t.Errorf("Offset = %d, should be clamped to max 50", state.Offset)
	}
}

// Integration test: full rendering with scrolled content
func TestScrollPane_IntegrationDraw(t *testing.T) {
	buf := make([][]texel.Cell, 30)
	for y := range buf {
		buf[y] = make([]texel.Cell, 60)
		for x := range buf[y] {
			buf[y][x] = texel.Cell{Ch: ' ', Style: tcell.StyleDefault}
		}
	}
	painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 60, H: 30})

	// ScrollPane at (10, 5) with size 40x10, content height 50
	sp := NewScrollPane(10, 5, 40, 10, tcell.StyleDefault)

	// Create child widget that fills 40x50
	child := &labelWidget{x: 10, y: 5, w: 40, h: 50}
	sp.SetChild(child)
	sp.SetContentHeight(50)

	// Draw at offset 0 - should see rows 0-9
	sp.Draw(painter)

	// Verify row 0 is visible (label writes row number)
	if buf[5][15].Ch != '0' {
		t.Errorf("At offset 0, expected '0' at row 5, got '%c'", buf[5][15].Ch)
	}

	// Scroll down 20 rows
	sp.ScrollBy(20)
	sp.Draw(painter)

	// Now should see rows 20-29
	if buf[5][15].Ch != '2' && buf[5][16].Ch != '0' {
		t.Errorf("At offset 20, expected '20' at row 5, got '%c%c'", buf[5][15].Ch, buf[5][16].Ch)
	}
}

// labelWidget writes row number at start of each line
type labelWidget struct {
	x, y, w, h int
}

func (l *labelWidget) SetPosition(x, y int)  { l.x, l.y = x, y }
func (l *labelWidget) Position() (int, int)  { return l.x, l.y }
func (l *labelWidget) Resize(w, h int)       { l.w, l.h = w, h }
func (l *labelWidget) Size() (int, int)      { return l.w, l.h }
func (l *labelWidget) Focusable() bool       { return false }
func (l *labelWidget) Focus()                {}
func (l *labelWidget) Blur()                 {}
func (l *labelWidget) HandleKey(*tcell.EventKey) bool { return false }
func (l *labelWidget) HitTest(x, y int) bool {
	return x >= l.x && x < l.x+l.w && y >= l.y && y < l.y+l.h
}

func (l *labelWidget) Draw(p *core.Painter) {
	for row := 0; row < l.h; row++ {
		// Write row number at start of each line
		label := []rune(itoa(row))
		for i, ch := range label {
			p.SetCell(l.x+5+i, l.y+row, ch, tcell.StyleDefault)
		}
	}
}

// itoa converts int to string (simple implementation for tests)
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []rune{}
	for n > 0 {
		digits = append([]rune{rune('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// TestScrollPane_MouseScrollbarArrows verifies clicking arrows scrolls by 1 row.
func TestScrollPane_MouseScrollbarArrows(t *testing.T) {
	sp := NewScrollPane(0, 0, 20, 10, tcell.StyleDefault)
	child := newMockWidget(0, 0, 19, 100, false) // Content height 100
	sp.SetChild(child)
	sp.SetContentHeight(100)

	// Scrollbar is at x=19 (rightmost column)
	scrollbarX := 19

	// Click up arrow at (19, 0) - should scroll up by 1 (but already at 0)
	sp.HandleMouse(tcell.NewEventMouse(scrollbarX, 0, tcell.Button1, 0))
	if sp.ScrollOffset() != 0 {
		t.Errorf("Expected offset 0 after up arrow at top, got %d", sp.ScrollOffset())
	}

	// Scroll down first
	sp.ScrollBy(10)
	if sp.ScrollOffset() != 10 {
		t.Fatalf("Expected offset 10, got %d", sp.ScrollOffset())
	}

	// Click up arrow - should scroll to 9
	sp.HandleMouse(tcell.NewEventMouse(scrollbarX, 0, tcell.Button1, 0))
	if sp.ScrollOffset() != 9 {
		t.Errorf("Expected offset 9 after up arrow click, got %d", sp.ScrollOffset())
	}

	// Click down arrow at (19, 9) - should scroll to 10
	sp.HandleMouse(tcell.NewEventMouse(scrollbarX, 9, tcell.Button1, 0))
	if sp.ScrollOffset() != 10 {
		t.Errorf("Expected offset 10 after down arrow click, got %d", sp.ScrollOffset())
	}
}

// TestScrollPane_MouseScrollbarTrack verifies clicking track scrolls by page.
func TestScrollPane_MouseScrollbarTrack(t *testing.T) {
	sp := NewScrollPane(0, 0, 20, 10, tcell.StyleDefault)
	child := newMockWidget(0, 0, 19, 100, false)
	sp.SetChild(child)
	sp.SetContentHeight(100)

	scrollbarX := 19

	// Start at middle
	sp.ScrollBy(50)
	if sp.ScrollOffset() != 50 {
		t.Fatalf("Expected offset 50, got %d", sp.ScrollOffset())
	}

	// Click track above thumb - should page up
	// Thumb is somewhere in the middle, track row 1 should be above it
	sp.HandleMouse(tcell.NewEventMouse(scrollbarX, 1, tcell.Button1, 0))
	if sp.ScrollOffset() >= 50 {
		t.Errorf("Expected offset < 50 after page up, got %d", sp.ScrollOffset())
	}

	// Go to top, then click track below thumb - should page down
	sp.ScrollBy(-100) // Go to top
	if sp.ScrollOffset() != 0 {
		t.Fatalf("Expected offset 0, got %d", sp.ScrollOffset())
	}

	// Click track near bottom (but above down arrow)
	sp.HandleMouse(tcell.NewEventMouse(scrollbarX, 8, tcell.Button1, 0))
	if sp.ScrollOffset() == 0 {
		t.Errorf("Expected offset > 0 after page down, got %d", sp.ScrollOffset())
	}
}

// TestScrollPane_MouseScrollbarThumbDrag verifies thumb drag scrolls proportionally.
func TestScrollPane_MouseScrollbarThumbDrag(t *testing.T) {
	sp := NewScrollPane(0, 0, 20, 10, tcell.StyleDefault)
	child := newMockWidget(0, 0, 19, 100, false)
	sp.SetChild(child)
	sp.SetContentHeight(100)

	scrollbarX := 19

	// Scrollbar geometry: viewport=10, content=100, track=8 (H-2)
	// Thumb size = (10 * 8) / 100 = 0.8 -> 1 (min)
	// At offset 0, thumb is at track row 0

	// Start drag on thumb (row 1, which is track row 0)
	sp.HandleMouse(tcell.NewEventMouse(scrollbarX, 1, tcell.Button1, 0))
	if !sp.draggingThumb {
		t.Fatal("Expected draggingThumb to be true after click on thumb")
	}

	// Drag down to row 5 (track row 4)
	sp.HandleMouse(tcell.NewEventMouse(scrollbarX, 5, tcell.Button1, 0))
	if sp.ScrollOffset() == 0 {
		t.Errorf("Expected offset > 0 after dragging thumb down, got %d", sp.ScrollOffset())
	}
	draggedOffset := sp.ScrollOffset()

	// Drag further down
	sp.HandleMouse(tcell.NewEventMouse(scrollbarX, 8, tcell.Button1, 0))
	if sp.ScrollOffset() <= draggedOffset {
		t.Errorf("Expected offset > %d after dragging more, got %d", draggedOffset, sp.ScrollOffset())
	}

	// Release button
	sp.HandleMouse(tcell.NewEventMouse(scrollbarX, 8, 0, 0))
	if sp.draggingThumb {
		t.Error("Expected draggingThumb to be false after release")
	}
}

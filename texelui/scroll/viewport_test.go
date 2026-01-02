// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package scroll

import (
	"testing"

	"texelation/texelui/core"
)

func TestNewViewport(t *testing.T) {
	rect := core.Rect{X: 10, Y: 5, W: 40, H: 20}
	v := NewViewport(rect, 30)

	if v.ScreenRect != rect {
		t.Errorf("ScreenRect = %+v, want %+v", v.ScreenRect, rect)
	}
	if v.ScrollOffset != 30 {
		t.Errorf("ScrollOffset = %d, want 30", v.ScrollOffset)
	}
}

func TestViewport_ContentToScreen(t *testing.T) {
	// Viewport at screen position (10, 5), size 40x20, scrolled 30 rows
	v := NewViewport(core.Rect{X: 10, Y: 5, W: 40, H: 20}, 30)
	// Visible content rows are [30, 50)
	// Content row 30 maps to screen Y 5
	// Content row 49 maps to screen Y 24

	tests := []struct {
		contentY    int
		wantScreenY int
		wantVisible bool
	}{
		{30, 5, true},   // First visible row
		{40, 15, true},  // Middle visible
		{49, 24, true},  // Last visible row
		{29, 4, false},  // Just above viewport
		{50, 25, false}, // Just below viewport
		{0, -25, false}, // Way above
	}

	for _, tt := range tests {
		screenY, visible := v.ContentToScreen(tt.contentY)
		if screenY != tt.wantScreenY || visible != tt.wantVisible {
			t.Errorf("ContentToScreen(%d) = (%d, %v), want (%d, %v)",
				tt.contentY, screenY, visible, tt.wantScreenY, tt.wantVisible)
		}
	}
}

func TestViewport_ScreenToContent(t *testing.T) {
	v := NewViewport(core.Rect{X: 10, Y: 5, W: 40, H: 20}, 30)

	tests := []struct {
		screenY     int
		wantContent int
	}{
		{5, 30},  // Top of viewport
		{15, 40}, // Middle
		{24, 49}, // Bottom of viewport
		{4, 29},  // Above viewport
		{25, 50}, // Below viewport
	}

	for _, tt := range tests {
		contentY := v.ScreenToContent(tt.screenY)
		if contentY != tt.wantContent {
			t.Errorf("ScreenToContent(%d) = %d, want %d",
				tt.screenY, contentY, tt.wantContent)
		}
	}
}

func TestViewport_ContentRectToScreen(t *testing.T) {
	v := NewViewport(core.Rect{X: 10, Y: 5, W: 40, H: 20}, 30)
	// Visible content rows: [30, 50)

	tests := []struct {
		name        string
		contentRect core.Rect
		wantScreen  core.Rect
		wantVisible bool
	}{
		{
			name:        "fully visible",
			contentRect: core.Rect{X: 10, Y: 35, W: 20, H: 5},
			wantScreen:  core.Rect{X: 10, Y: 10, W: 20, H: 5}, // Y: 35-30+5=10
			wantVisible: true,
		},
		{
			name:        "partially visible top",
			contentRect: core.Rect{X: 10, Y: 28, W: 20, H: 5},
			wantScreen:  core.Rect{X: 10, Y: 3, W: 20, H: 5}, // Y: 28-30+5=3
			wantVisible: true,                                 // overlaps [3,8) with [5,25)
		},
		{
			name:        "partially visible bottom",
			contentRect: core.Rect{X: 10, Y: 47, W: 20, H: 5},
			wantScreen:  core.Rect{X: 10, Y: 22, W: 20, H: 5}, // Y: 47-30+5=22
			wantVisible: true,                                  // overlaps [22,27) with [5,25)
		},
		{
			name:        "above viewport",
			contentRect: core.Rect{X: 10, Y: 20, W: 20, H: 5},
			wantScreen:  core.Rect{X: 10, Y: -5, W: 20, H: 5}, // Y: 20-30+5=-5
			wantVisible: false,                                 // [−5,0) doesn't overlap [5,25)
		},
		{
			name:        "below viewport",
			contentRect: core.Rect{X: 10, Y: 55, W: 20, H: 5},
			wantScreen:  core.Rect{X: 10, Y: 30, W: 20, H: 5}, // Y: 55-30+5=30
			wantVisible: false,                                 // [30,35) doesn't overlap [5,25)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			screenRect, visible := v.ContentRectToScreen(tt.contentRect)
			if screenRect != tt.wantScreen || visible != tt.wantVisible {
				t.Errorf("ContentRectToScreen(%+v) = (%+v, %v), want (%+v, %v)",
					tt.contentRect, screenRect, visible, tt.wantScreen, tt.wantVisible)
			}
		})
	}
}

func TestViewport_ClipToViewport(t *testing.T) {
	v := NewViewport(core.Rect{X: 10, Y: 5, W: 40, H: 20}, 0)
	// Viewport screen bounds: X=[10,50), Y=[5,25)

	tests := []struct {
		name        string
		screenRect  core.Rect
		wantClipped core.Rect
		wantVisible bool
	}{
		{
			name:        "fully inside",
			screenRect:  core.Rect{X: 15, Y: 10, W: 20, H: 10},
			wantClipped: core.Rect{X: 15, Y: 10, W: 20, H: 10},
			wantVisible: true,
		},
		{
			name:        "clip left edge",
			screenRect:  core.Rect{X: 5, Y: 10, W: 20, H: 10},
			wantClipped: core.Rect{X: 10, Y: 10, W: 15, H: 10},
			wantVisible: true,
		},
		{
			name:        "clip top edge",
			screenRect:  core.Rect{X: 15, Y: 0, W: 20, H: 10},
			wantClipped: core.Rect{X: 15, Y: 5, W: 20, H: 5},
			wantVisible: true,
		},
		{
			name:        "clip right edge",
			screenRect:  core.Rect{X: 40, Y: 10, W: 20, H: 10},
			wantClipped: core.Rect{X: 40, Y: 10, W: 10, H: 10},
			wantVisible: true,
		},
		{
			name:        "clip bottom edge",
			screenRect:  core.Rect{X: 15, Y: 20, W: 20, H: 10},
			wantClipped: core.Rect{X: 15, Y: 20, W: 20, H: 5},
			wantVisible: true,
		},
		{
			name:        "completely outside left",
			screenRect:  core.Rect{X: 0, Y: 10, W: 5, H: 10},
			wantClipped: core.Rect{},
			wantVisible: false,
		},
		{
			name:        "completely outside top",
			screenRect:  core.Rect{X: 15, Y: 0, W: 20, H: 3},
			wantClipped: core.Rect{},
			wantVisible: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clipped, visible := v.ClipToViewport(tt.screenRect)
			if visible != tt.wantVisible {
				t.Errorf("ClipToViewport(%+v) visible = %v, want %v",
					tt.screenRect, visible, tt.wantVisible)
			}
			if visible && clipped != tt.wantClipped {
				t.Errorf("ClipToViewport(%+v) = %+v, want %+v",
					tt.screenRect, clipped, tt.wantClipped)
			}
		})
	}
}

func TestViewport_IsContentRectVisible(t *testing.T) {
	v := NewViewport(core.Rect{X: 0, Y: 0, W: 80, H: 24}, 10)
	// Visible content rows: [10, 34)

	tests := []struct {
		contentRect core.Rect
		want        bool
	}{
		{core.Rect{X: 0, Y: 15, W: 10, H: 3}, true},  // Fully visible
		{core.Rect{X: 0, Y: 8, W: 10, H: 5}, true},   // Partially visible (top)
		{core.Rect{X: 0, Y: 32, W: 10, H: 5}, true},  // Partially visible (bottom)
		{core.Rect{X: 0, Y: 0, W: 10, H: 5}, false},  // Above viewport
		{core.Rect{X: 0, Y: 40, W: 10, H: 5}, false}, // Below viewport
	}

	for _, tt := range tests {
		if got := v.IsContentRectVisible(tt.contentRect); got != tt.want {
			t.Errorf("IsContentRectVisible(%+v) = %v, want %v", tt.contentRect, got, tt.want)
		}
	}
}

func TestViewport_VisibleContentRange(t *testing.T) {
	tests := []struct {
		name      string
		viewport  Viewport
		wantStart int
		wantEnd   int
	}{
		{
			name:      "no scroll",
			viewport:  NewViewport(core.Rect{X: 0, Y: 0, W: 80, H: 24}, 0),
			wantStart: 0,
			wantEnd:   24,
		},
		{
			name:      "scrolled",
			viewport:  NewViewport(core.Rect{X: 0, Y: 0, W: 80, H: 24}, 50),
			wantStart: 50,
			wantEnd:   74,
		},
		{
			name:      "offset viewport",
			viewport:  NewViewport(core.Rect{X: 10, Y: 5, W: 60, H: 15}, 20),
			wantStart: 20,
			wantEnd:   35,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := tt.viewport.VisibleContentRange()
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("VisibleContentRange() = (%d, %d), want (%d, %d)",
					start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

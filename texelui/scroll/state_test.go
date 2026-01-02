// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package scroll

import "testing"

func TestNewState(t *testing.T) {
	tests := []struct {
		name           string
		contentHeight  int
		viewportHeight int
		wantOffset     int
	}{
		{"normal", 100, 20, 0},
		{"content fits", 10, 20, 0},
		{"zero viewport", 100, 0, 0},
		{"zero content", 0, 20, 0},
		{"negative content", -5, 20, 0},
		{"negative viewport", 100, -5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewState(tt.contentHeight, tt.viewportHeight)
			if s.Offset != tt.wantOffset {
				t.Errorf("NewState().Offset = %d, want %d", s.Offset, tt.wantOffset)
			}
		})
	}
}

func TestState_Clamp(t *testing.T) {
	tests := []struct {
		name       string
		state      State
		wantOffset int
	}{
		{
			name:       "already valid",
			state:      State{ContentHeight: 100, ViewportHeight: 20, Offset: 10},
			wantOffset: 10,
		},
		{
			name:       "negative offset",
			state:      State{ContentHeight: 100, ViewportHeight: 20, Offset: -5},
			wantOffset: 0,
		},
		{
			name:       "offset too high",
			state:      State{ContentHeight: 100, ViewportHeight: 20, Offset: 90},
			wantOffset: 80, // max = 100 - 20 = 80
		},
		{
			name:       "content fits viewport",
			state:      State{ContentHeight: 10, ViewportHeight: 20, Offset: 5},
			wantOffset: 0, // no scrolling needed
		},
		{
			name:       "exactly fits",
			state:      State{ContentHeight: 20, ViewportHeight: 20, Offset: 5},
			wantOffset: 0, // no scrolling needed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.state.Clamp()
			if got.Offset != tt.wantOffset {
				t.Errorf("Clamp().Offset = %d, want %d", got.Offset, tt.wantOffset)
			}
		})
	}
}

func TestState_MaxOffset(t *testing.T) {
	tests := []struct {
		name  string
		state State
		want  int
	}{
		{"normal", State{ContentHeight: 100, ViewportHeight: 20}, 80},
		{"exactly fits", State{ContentHeight: 20, ViewportHeight: 20}, 0},
		{"content smaller", State{ContentHeight: 10, ViewportHeight: 20}, 0},
		{"zero viewport", State{ContentHeight: 100, ViewportHeight: 0}, 100},
		{"zero content", State{ContentHeight: 0, ViewportHeight: 20}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.MaxOffset(); got != tt.want {
				t.Errorf("MaxOffset() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestState_CanScroll(t *testing.T) {
	tests := []struct {
		name     string
		state    State
		wantUp   bool
		wantDown bool
		wantAny  bool
	}{
		{
			name:     "at top, can scroll down",
			state:    State{ContentHeight: 100, ViewportHeight: 20, Offset: 0},
			wantUp:   false,
			wantDown: true,
			wantAny:  true,
		},
		{
			name:     "at bottom, can scroll up",
			state:    State{ContentHeight: 100, ViewportHeight: 20, Offset: 80},
			wantUp:   true,
			wantDown: false,
			wantAny:  true,
		},
		{
			name:     "middle, can scroll both",
			state:    State{ContentHeight: 100, ViewportHeight: 20, Offset: 40},
			wantUp:   true,
			wantDown: true,
			wantAny:  true,
		},
		{
			name:     "content fits, no scroll",
			state:    State{ContentHeight: 10, ViewportHeight: 20, Offset: 0},
			wantUp:   false,
			wantDown: false,
			wantAny:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.CanScrollUp(); got != tt.wantUp {
				t.Errorf("CanScrollUp() = %v, want %v", got, tt.wantUp)
			}
			if got := tt.state.CanScrollDown(); got != tt.wantDown {
				t.Errorf("CanScrollDown() = %v, want %v", got, tt.wantDown)
			}
			if got := tt.state.CanScroll(); got != tt.wantAny {
				t.Errorf("CanScroll() = %v, want %v", got, tt.wantAny)
			}
		})
	}
}

func TestState_VisibleRange(t *testing.T) {
	tests := []struct {
		name      string
		state     State
		wantStart int
		wantEnd   int
	}{
		{
			name:      "at top",
			state:     State{ContentHeight: 100, ViewportHeight: 20, Offset: 0},
			wantStart: 0,
			wantEnd:   20,
		},
		{
			name:      "scrolled",
			state:     State{ContentHeight: 100, ViewportHeight: 20, Offset: 50},
			wantStart: 50,
			wantEnd:   70,
		},
		{
			name:      "at bottom",
			state:     State{ContentHeight: 100, ViewportHeight: 20, Offset: 80},
			wantStart: 80,
			wantEnd:   100,
		},
		{
			name:      "content smaller than viewport",
			state:     State{ContentHeight: 10, ViewportHeight: 20, Offset: 0},
			wantStart: 0,
			wantEnd:   10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := tt.state.VisibleRange()
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("VisibleRange() = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestState_IsRowVisible(t *testing.T) {
	state := State{ContentHeight: 100, ViewportHeight: 20, Offset: 30}
	// Visible range is [30, 50)

	tests := []struct {
		row  int
		want bool
	}{
		{29, false},  // just above
		{30, true},   // first visible
		{40, true},   // middle
		{49, true},   // last visible
		{50, false},  // just below
		{0, false},   // way above
		{99, false},  // way below
	}

	for _, tt := range tests {
		if got := state.IsRowVisible(tt.row); got != tt.want {
			t.Errorf("IsRowVisible(%d) = %v, want %v", tt.row, got, tt.want)
		}
	}
}

func TestState_ScrollBy(t *testing.T) {
	tests := []struct {
		name       string
		state      State
		delta      int
		wantOffset int
	}{
		{
			name:       "scroll down",
			state:      State{ContentHeight: 100, ViewportHeight: 20, Offset: 0},
			delta:      10,
			wantOffset: 10,
		},
		{
			name:       "scroll up",
			state:      State{ContentHeight: 100, ViewportHeight: 20, Offset: 50},
			delta:      -10,
			wantOffset: 40,
		},
		{
			name:       "scroll past top",
			state:      State{ContentHeight: 100, ViewportHeight: 20, Offset: 5},
			delta:      -20,
			wantOffset: 0,
		},
		{
			name:       "scroll past bottom",
			state:      State{ContentHeight: 100, ViewportHeight: 20, Offset: 70},
			delta:      20,
			wantOffset: 80,
		},
		{
			name:       "page down",
			state:      State{ContentHeight: 100, ViewportHeight: 20, Offset: 0},
			delta:      20,
			wantOffset: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.state.ScrollBy(tt.delta)
			if got.Offset != tt.wantOffset {
				t.Errorf("ScrollBy(%d).Offset = %d, want %d", tt.delta, got.Offset, tt.wantOffset)
			}
		})
	}
}

func TestState_ScrollTo(t *testing.T) {
	tests := []struct {
		name       string
		state      State
		row        int
		wantOffset int
	}{
		{
			name:       "row already visible, no change",
			state:      State{ContentHeight: 100, ViewportHeight: 20, Offset: 30},
			row:        40, // in range [30, 50)
			wantOffset: 30,
		},
		{
			name:       "row above viewport, scroll up",
			state:      State{ContentHeight: 100, ViewportHeight: 20, Offset: 30},
			row:        10,
			wantOffset: 10,
		},
		{
			name:       "row below viewport, scroll down",
			state:      State{ContentHeight: 100, ViewportHeight: 20, Offset: 30},
			row:        60, // needs to show row 60 at bottom
			wantOffset: 41, // 60 - 20 + 1 = 41
		},
		{
			name:       "scroll to first row",
			state:      State{ContentHeight: 100, ViewportHeight: 20, Offset: 50},
			row:        0,
			wantOffset: 0,
		},
		{
			name:       "scroll to last row",
			state:      State{ContentHeight: 100, ViewportHeight: 20, Offset: 0},
			row:        99,
			wantOffset: 80, // max offset
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.state.ScrollTo(tt.row)
			if got.Offset != tt.wantOffset {
				t.Errorf("ScrollTo(%d).Offset = %d, want %d", tt.row, got.Offset, tt.wantOffset)
			}
		})
	}
}

func TestState_ScrollToCentered(t *testing.T) {
	tests := []struct {
		name       string
		state      State
		row        int
		wantOffset int
	}{
		{
			name:       "center in middle",
			state:      State{ContentHeight: 100, ViewportHeight: 20, Offset: 0},
			row:        50,
			wantOffset: 40, // 50 - 20/2 = 40
		},
		{
			name:       "center near top, clamps to 0",
			state:      State{ContentHeight: 100, ViewportHeight: 20, Offset: 50},
			row:        5,
			wantOffset: 0, // 5 - 10 = -5, clamped to 0
		},
		{
			name:       "center near bottom, clamps to max",
			state:      State{ContentHeight: 100, ViewportHeight: 20, Offset: 0},
			row:        95,
			wantOffset: 80, // 95 - 10 = 85, clamped to 80
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.state.ScrollToCentered(tt.row)
			if got.Offset != tt.wantOffset {
				t.Errorf("ScrollToCentered(%d).Offset = %d, want %d", tt.row, got.Offset, tt.wantOffset)
			}
		})
	}
}

func TestState_ScrollToTopBottom(t *testing.T) {
	state := State{ContentHeight: 100, ViewportHeight: 20, Offset: 50}

	top := state.ScrollToTop()
	if top.Offset != 0 {
		t.Errorf("ScrollToTop().Offset = %d, want 0", top.Offset)
	}

	bottom := state.ScrollToBottom()
	if bottom.Offset != 80 {
		t.Errorf("ScrollToBottom().Offset = %d, want 80", bottom.Offset)
	}
}

func TestState_WithHeight(t *testing.T) {
	state := State{ContentHeight: 100, ViewportHeight: 20, Offset: 50}

	// Change content height
	newContent := state.WithContentHeight(60)
	if newContent.ContentHeight != 60 {
		t.Errorf("WithContentHeight().ContentHeight = %d, want 60", newContent.ContentHeight)
	}
	if newContent.Offset != 40 { // 60 - 20 = 40 max
		t.Errorf("WithContentHeight().Offset = %d, want 40 (clamped)", newContent.Offset)
	}

	// Change viewport height
	newViewport := state.WithViewportHeight(30)
	if newViewport.ViewportHeight != 30 {
		t.Errorf("WithViewportHeight().ViewportHeight = %d, want 30", newViewport.ViewportHeight)
	}
	if newViewport.Offset != 50 { // still valid (100 - 30 = 70 max)
		t.Errorf("WithViewportHeight().Offset = %d, want 50", newViewport.Offset)
	}
}

func TestState_Immutability(t *testing.T) {
	original := State{ContentHeight: 100, ViewportHeight: 20, Offset: 50}

	// All methods should return new state, not modify original
	_ = original.ScrollBy(10)
	_ = original.ScrollTo(0)
	_ = original.ScrollToCentered(80)
	_ = original.Clamp()

	if original.Offset != 50 {
		t.Errorf("Original state was mutated: Offset = %d, want 50", original.Offset)
	}
}

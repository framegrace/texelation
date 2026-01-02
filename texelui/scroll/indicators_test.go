// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package scroll

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"texelation/texel"
	"texelation/texelui/core"
)

// createTestBuffer creates a buffer for testing rendering.
func createTestBuffer(w, h int) [][]texel.Cell {
	buf := make([][]texel.Cell, h)
	for y := range buf {
		buf[y] = make([]texel.Cell, w)
		for x := range buf[y] {
			buf[y][x] = texel.Cell{Ch: ' ', Style: tcell.StyleDefault}
		}
	}
	return buf
}

// getCell returns the character at a position in the buffer.
func getCell(buf [][]texel.Cell, x, y int) rune {
	if y >= 0 && y < len(buf) && x >= 0 && x < len(buf[y]) {
		return buf[y][x].Ch
	}
	return 0
}

func TestDefaultIndicatorConfig(t *testing.T) {
	style := tcell.StyleDefault.Foreground(tcell.ColorRed)
	config := DefaultIndicatorConfig(style)

	if config.Position != IndicatorRight {
		t.Errorf("Position = %v, want IndicatorRight", config.Position)
	}
	if config.UpGlyph != DefaultUpGlyph {
		t.Errorf("UpGlyph = %c, want %c", config.UpGlyph, DefaultUpGlyph)
	}
	if config.DownGlyph != DefaultDownGlyph {
		t.Errorf("DownGlyph = %c, want %c", config.DownGlyph, DefaultDownGlyph)
	}
	if config.Style != style {
		t.Errorf("Style not preserved")
	}
}

func TestDrawIndicators_CanScrollBoth(t *testing.T) {
	buf := createTestBuffer(40, 20)
	painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 40, H: 20})
	rect := core.Rect{X: 10, Y: 5, W: 20, H: 10}

	// Scroll state that can scroll both ways
	state := State{ContentHeight: 100, ViewportHeight: 10, Offset: 50}

	DrawIndicatorsSimple(painter, rect, state, tcell.StyleDefault)

	// Check up indicator at top-right
	upX, upY := rect.X+rect.W-1, rect.Y
	if got := getCell(buf, upX, upY); got != DefaultUpGlyph {
		t.Errorf("Up indicator at (%d,%d) = %c, want %c", upX, upY, got, DefaultUpGlyph)
	}

	// Check down indicator at bottom-right
	downX, downY := rect.X+rect.W-1, rect.Y+rect.H-1
	if got := getCell(buf, downX, downY); got != DefaultDownGlyph {
		t.Errorf("Down indicator at (%d,%d) = %c, want %c", downX, downY, got, DefaultDownGlyph)
	}
}

func TestDrawIndicators_AtTop(t *testing.T) {
	buf := createTestBuffer(40, 20)
	painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 40, H: 20})
	rect := core.Rect{X: 10, Y: 5, W: 20, H: 10}

	// At top - can only scroll down
	state := State{ContentHeight: 100, ViewportHeight: 10, Offset: 0}

	DrawIndicatorsSimple(painter, rect, state, tcell.StyleDefault)

	// No up indicator
	upX, upY := rect.X+rect.W-1, rect.Y
	if got := getCell(buf, upX, upY); got != ' ' {
		t.Errorf("Should have no up indicator at (%d,%d), got %c", upX, upY, got)
	}

	// Has down indicator
	downX, downY := rect.X+rect.W-1, rect.Y+rect.H-1
	if got := getCell(buf, downX, downY); got != DefaultDownGlyph {
		t.Errorf("Down indicator at (%d,%d) = %c, want %c", downX, downY, got, DefaultDownGlyph)
	}
}

func TestDrawIndicators_AtBottom(t *testing.T) {
	buf := createTestBuffer(40, 20)
	painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 40, H: 20})
	rect := core.Rect{X: 10, Y: 5, W: 20, H: 10}

	// At bottom - can only scroll up
	state := State{ContentHeight: 100, ViewportHeight: 10, Offset: 90}

	DrawIndicatorsSimple(painter, rect, state, tcell.StyleDefault)

	// Has up indicator
	upX, upY := rect.X+rect.W-1, rect.Y
	if got := getCell(buf, upX, upY); got != DefaultUpGlyph {
		t.Errorf("Up indicator at (%d,%d) = %c, want %c", upX, upY, got, DefaultUpGlyph)
	}

	// No down indicator
	downX, downY := rect.X+rect.W-1, rect.Y+rect.H-1
	if got := getCell(buf, downX, downY); got != ' ' {
		t.Errorf("Should have no down indicator at (%d,%d), got %c", downX, downY, got)
	}
}

func TestDrawIndicators_NoScroll(t *testing.T) {
	buf := createTestBuffer(40, 20)
	painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 40, H: 20})
	rect := core.Rect{X: 10, Y: 5, W: 20, H: 10}

	// Content fits in viewport - no scrolling needed
	state := State{ContentHeight: 5, ViewportHeight: 10, Offset: 0}

	DrawIndicatorsSimple(painter, rect, state, tcell.StyleDefault)

	// No up indicator
	upX, upY := rect.X+rect.W-1, rect.Y
	if got := getCell(buf, upX, upY); got != ' ' {
		t.Errorf("Should have no up indicator, got %c", got)
	}

	// No down indicator
	downX, downY := rect.X+rect.W-1, rect.Y+rect.H-1
	if got := getCell(buf, downX, downY); got != ' ' {
		t.Errorf("Should have no down indicator, got %c", got)
	}
}

func TestDrawIndicators_LeftPosition(t *testing.T) {
	buf := createTestBuffer(40, 20)
	painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 40, H: 20})
	rect := core.Rect{X: 10, Y: 5, W: 20, H: 10}

	state := State{ContentHeight: 100, ViewportHeight: 10, Offset: 50}

	config := IndicatorConfig{
		Position:  IndicatorLeft,
		Style:     tcell.StyleDefault,
		UpGlyph:   DefaultUpGlyph,
		DownGlyph: DefaultDownGlyph,
	}
	DrawIndicators(painter, rect, state, config)

	// Up indicator at left
	if got := getCell(buf, rect.X, rect.Y); got != DefaultUpGlyph {
		t.Errorf("Up indicator at left = %c, want %c", got, DefaultUpGlyph)
	}

	// Down indicator at left
	if got := getCell(buf, rect.X, rect.Y+rect.H-1); got != DefaultDownGlyph {
		t.Errorf("Down indicator at left = %c, want %c", got, DefaultDownGlyph)
	}

	// Right edge should be empty
	if got := getCell(buf, rect.X+rect.W-1, rect.Y); got != ' ' {
		t.Errorf("Right edge should be empty, got %c", got)
	}
}

func TestDrawIndicators_CustomGlyphs(t *testing.T) {
	buf := createTestBuffer(40, 20)
	painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 40, H: 20})
	rect := core.Rect{X: 10, Y: 5, W: 20, H: 10}

	state := State{ContentHeight: 100, ViewportHeight: 10, Offset: 50}

	config := IndicatorConfig{
		Position:  IndicatorRight,
		Style:     tcell.StyleDefault,
		UpGlyph:   '↑',
		DownGlyph: '↓',
	}
	DrawIndicators(painter, rect, state, config)

	// Check custom glyphs
	if got := getCell(buf, rect.X+rect.W-1, rect.Y); got != '↑' {
		t.Errorf("Up indicator = %c, want ↑", got)
	}
	if got := getCell(buf, rect.X+rect.W-1, rect.Y+rect.H-1); got != '↓' {
		t.Errorf("Down indicator = %c, want ↓", got)
	}
}

func TestDrawIndicators_ZeroSizeRect(t *testing.T) {
	buf := createTestBuffer(40, 20)
	painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 40, H: 20})

	state := State{ContentHeight: 100, ViewportHeight: 10, Offset: 50}

	// Should not panic with zero-size rect
	DrawIndicatorsSimple(painter, core.Rect{X: 10, Y: 5, W: 0, H: 10}, state, tcell.StyleDefault)
	DrawIndicatorsSimple(painter, core.Rect{X: 10, Y: 5, W: 20, H: 0}, state, tcell.StyleDefault)
	DrawIndicatorsSimple(painter, core.Rect{X: 10, Y: 5, W: 0, H: 0}, state, tcell.StyleDefault)
}

func TestDrawIndicators_SingleRowViewport(t *testing.T) {
	buf := createTestBuffer(40, 20)
	painter := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 40, H: 20})
	rect := core.Rect{X: 10, Y: 5, W: 20, H: 1}

	// Single row viewport that can scroll both ways
	state := State{ContentHeight: 100, ViewportHeight: 1, Offset: 50}

	DrawIndicatorsSimple(painter, rect, state, tcell.StyleDefault)

	// Both indicators should be at the same position (H=1 means top=bottom)
	// Only up indicator should be visible since down overlaps
	x := rect.X + rect.W - 1
	y := rect.Y // Same as rect.Y + rect.H - 1 when H=1

	ch := getCell(buf, x, y)
	// Either up or down glyph is acceptable in this edge case
	if ch != DefaultUpGlyph && ch != DefaultDownGlyph {
		t.Errorf("Indicator at single row = %c, want %c or %c", ch, DefaultUpGlyph, DefaultDownGlyph)
	}
}

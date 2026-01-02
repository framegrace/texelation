// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/scroll/indicators.go
// Summary: Scroll indicator rendering for scrollable widgets.
// Provides reusable scroll indicator glyphs (▲/▼) that show when content overflows.

package scroll

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texelui/core"
)

// IndicatorPosition specifies where scroll indicators are rendered.
type IndicatorPosition int

const (
	// IndicatorRight places indicators at the right edge of the viewport (default).
	IndicatorRight IndicatorPosition = iota
	// IndicatorLeft places indicators at the left edge of the viewport.
	IndicatorLeft
)

// Default indicator glyphs.
const (
	DefaultUpGlyph   = '▲'
	DefaultDownGlyph = '▼'
)

// IndicatorConfig configures the appearance of scroll indicators.
type IndicatorConfig struct {
	// Position specifies where indicators are drawn (left or right edge).
	Position IndicatorPosition

	// Style is the tcell style for indicator glyphs.
	Style tcell.Style

	// UpGlyph is the character shown when content is above the viewport.
	UpGlyph rune

	// DownGlyph is the character shown when content is below the viewport.
	DownGlyph rune
}

// DefaultIndicatorConfig returns a default configuration with standard glyphs.
func DefaultIndicatorConfig(style tcell.Style) IndicatorConfig {
	return IndicatorConfig{
		Position:  IndicatorRight,
		Style:     style,
		UpGlyph:   DefaultUpGlyph,
		DownGlyph: DefaultDownGlyph,
	}
}

// DrawIndicators renders scroll indicators on a viewport.
// Shows an up indicator if state.CanScrollUp() and a down indicator if state.CanScrollDown().
func DrawIndicators(painter *core.Painter, rect core.Rect, state State, config IndicatorConfig) {
	if rect.W <= 0 || rect.H <= 0 {
		return
	}

	// Determine X position based on config
	var x int
	switch config.Position {
	case IndicatorLeft:
		x = rect.X
	case IndicatorRight:
		fallthrough
	default:
		x = rect.X + rect.W - 1
	}

	// Draw up indicator at top
	if state.CanScrollUp() {
		glyph := config.UpGlyph
		if glyph == 0 {
			glyph = DefaultUpGlyph
		}
		painter.SetCell(x, rect.Y, glyph, config.Style)
	}

	// Draw down indicator at bottom
	if state.CanScrollDown() {
		glyph := config.DownGlyph
		if glyph == 0 {
			glyph = DefaultDownGlyph
		}
		painter.SetCell(x, rect.Y+rect.H-1, glyph, config.Style)
	}
}

// DrawIndicatorsSimple is a convenience function that draws indicators with default config.
func DrawIndicatorsSimple(painter *core.Painter, rect core.Rect, state State, style tcell.Style) {
	DrawIndicators(painter, rect, state, DefaultIndicatorConfig(style))
}

// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/scroll/indicators.go
// Summary: Scroll indicator and scrollbar rendering for scrollable widgets.
// Provides reusable scroll indicator glyphs (▲/▼) and a proportional scrollbar.

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

// Default scrollbar characters.
const (
	DefaultThumbChar = '█' // Solid block for thumb
	DefaultTrackChar = '░' // Light shade for track
)

// ScrollbarConfig configures the appearance of the scrollbar.
type ScrollbarConfig struct {
	// Position specifies where the scrollbar is drawn (left or right edge).
	Position IndicatorPosition

	// ThumbStyle is the style for the scrollbar thumb (visible portion indicator).
	ThumbStyle tcell.Style

	// TrackStyle is the style for the scrollbar track (background).
	TrackStyle tcell.Style

	// ArrowStyle is the style for the up/down arrow indicators.
	ArrowStyle tcell.Style

	// ThumbChar is the character used for the thumb.
	ThumbChar rune

	// TrackChar is the character used for the track.
	TrackChar rune

	// UpArrow is the character for the up arrow (default ▲).
	UpArrow rune

	// DownArrow is the character for the down arrow (default ▼).
	DownArrow rune

	// MinThumbSize is the minimum size of the thumb in rows (default 1).
	MinThumbSize int
}

// DefaultScrollbarConfig returns a default scrollbar configuration.
// thumbStyle is used for the thumb, trackStyle for the track background.
func DefaultScrollbarConfig(thumbStyle, trackStyle tcell.Style) ScrollbarConfig {
	return ScrollbarConfig{
		Position:     IndicatorRight,
		ThumbStyle:   thumbStyle,
		TrackStyle:   trackStyle,
		ArrowStyle:   thumbStyle, // Arrows use same style as thumb by default
		ThumbChar:    DefaultThumbChar,
		TrackChar:    DefaultTrackChar,
		UpArrow:      DefaultUpGlyph,
		DownArrow:    DefaultDownGlyph,
		MinThumbSize: 1,
	}
}

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

	// ShowScrollbar enables a proportional scrollbar instead of just arrows.
	ShowScrollbar bool

	// Scrollbar is the scrollbar configuration (used when ShowScrollbar is true).
	Scrollbar ScrollbarConfig
}

// DefaultIndicatorConfig returns a default configuration with standard glyphs.
func DefaultIndicatorConfig(style tcell.Style) IndicatorConfig {
	return IndicatorConfig{
		Position:      IndicatorRight,
		Style:         style,
		UpGlyph:       DefaultUpGlyph,
		DownGlyph:     DefaultDownGlyph,
		ShowScrollbar: false,
	}
}

// DefaultIndicatorConfigWithScrollbar returns a configuration with scrollbar enabled.
func DefaultIndicatorConfigWithScrollbar(thumbStyle, trackStyle tcell.Style) IndicatorConfig {
	return IndicatorConfig{
		Position:      IndicatorRight,
		Style:         thumbStyle, // Used for arrows if shown
		UpGlyph:       DefaultUpGlyph,
		DownGlyph:     DefaultDownGlyph,
		ShowScrollbar: true,
		Scrollbar:     DefaultScrollbarConfig(thumbStyle, trackStyle),
	}
}

// DrawIndicators renders scroll indicators on a viewport.
// If ShowScrollbar is enabled, draws a proportional scrollbar.
// Otherwise shows an up indicator if state.CanScrollUp() and down if state.CanScrollDown().
func DrawIndicators(painter *core.Painter, rect core.Rect, state State, config IndicatorConfig) {
	if rect.W <= 0 || rect.H <= 0 {
		return
	}

	// If scrollbar is enabled, draw it instead of simple arrows
	if config.ShowScrollbar {
		DrawScrollbar(painter, rect, state, config.Scrollbar)
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

// DrawScrollbar renders a proportional scrollbar showing position and visible portion.
// The thumb size represents the proportion of visible content to total content.
// The thumb position represents where in the content the viewport is located.
// Arrow indicators (▲/▼) are shown at top/bottom when scrolling is possible.
func DrawScrollbar(painter *core.Painter, rect core.Rect, state State, config ScrollbarConfig) {
	if rect.W <= 0 || rect.H <= 0 {
		return
	}

	// If content fits in viewport, no scrollbar needed
	if !state.CanScroll() {
		return
	}

	// Determine X position
	var x int
	switch config.Position {
	case IndicatorLeft:
		x = rect.X
	case IndicatorRight:
		fallthrough
	default:
		x = rect.X + rect.W - 1
	}

	// Get characters
	thumbChar := config.ThumbChar
	if thumbChar == 0 {
		thumbChar = DefaultThumbChar
	}
	trackChar := config.TrackChar
	if trackChar == 0 {
		trackChar = DefaultTrackChar
	}
	upArrow := config.UpArrow
	if upArrow == 0 {
		upArrow = DefaultUpGlyph
	}
	downArrow := config.DownArrow
	if downArrow == 0 {
		downArrow = DefaultDownGlyph
	}

	// Draw up arrow at top
	painter.SetCell(x, rect.Y, upArrow, config.ArrowStyle)

	// Draw down arrow at bottom
	painter.SetCell(x, rect.Y+rect.H-1, downArrow, config.ArrowStyle)

	// Track area is between arrows (rows 1 to H-2)
	trackHeight := rect.H - 2
	if trackHeight <= 0 {
		return
	}

	// Calculate thumb size: proportion of viewport to content
	// thumbSize = (viewportHeight / contentHeight) * trackHeight
	thumbSize := (state.ViewportHeight * trackHeight) / state.ContentHeight
	minThumb := config.MinThumbSize
	if minThumb <= 0 {
		minThumb = 1
	}
	if thumbSize < minThumb {
		thumbSize = minThumb
	}
	if thumbSize > trackHeight {
		thumbSize = trackHeight
	}

	// Calculate thumb position within the track area
	// The scrollable range is (contentHeight - viewportHeight)
	// The thumb moves within (trackHeight - thumbSize) pixels
	scrollableContent := state.ContentHeight - state.ViewportHeight
	scrollableTrack := trackHeight - thumbSize

	var thumbStart int
	if scrollableContent > 0 && scrollableTrack > 0 {
		thumbStart = (state.Offset * scrollableTrack) / scrollableContent
	}
	if thumbStart < 0 {
		thumbStart = 0
	}
	if thumbStart > scrollableTrack {
		thumbStart = scrollableTrack
	}

	thumbEndRow := thumbStart + thumbSize

	// Draw the scrollbar track and thumb (between arrows)
	for row := 0; row < trackHeight; row++ {
		y := rect.Y + 1 + row // +1 to skip up arrow
		if row >= thumbStart && row < thumbEndRow {
			// Draw thumb
			painter.SetCell(x, y, thumbChar, config.ThumbStyle)
		} else {
			// Draw track
			painter.SetCell(x, y, trackChar, config.TrackStyle)
		}
	}
}

// DrawIndicatorsSimple is a convenience function that draws indicators with default config.
func DrawIndicatorsSimple(painter *core.Painter, rect core.Rect, state State, style tcell.Style) {
	DrawIndicators(painter, rect, state, DefaultIndicatorConfig(style))
}

// DrawScrollbarSimple is a convenience function that draws a scrollbar with default config.
func DrawScrollbarSimple(painter *core.Painter, rect core.Rect, state State, thumbStyle, trackStyle tcell.Style) {
	DrawScrollbar(painter, rect, state, DefaultScrollbarConfig(thumbStyle, trackStyle))
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/scrollbar.go
// Summary: Vertical scrollbar with minimap showing line length distribution.
// Usage: Shows scroll position and provides visual minimap of history content.

package texelterm

import (
	"log"
	"math"
	"sync"

	texelcolor "github.com/framegrace/texelui/color"
	texelcore "github.com/framegrace/texelui/core"
	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/internal/theming"
)

// Braille dot values for building characters
// Braille is a 2x4 grid:
//   dot1(+1)   dot4(+8)
//   dot2(+2)   dot5(+16)
//   dot3(+4)   dot6(+32)
//   dot7(+64)  dot8(+128)
// Unicode braille starts at U+2800
const brailleBase = 0x2800

// Braille dot values indexed by row (0-3) and column (0=left, 1=right)
var brailleDots = [4][2]rune{
	{1, 8},    // Row 0: dots 1, 4
	{2, 16},   // Row 1: dots 2, 5
	{4, 32},   // Row 2: dots 3, 6
	{64, 128}, // Row 3: dots 7, 8
}

// ScrollBarWidth is the width of the scrollbar in characters.
// 1 border + 5 braille minimap chars = 6 total
const ScrollBarWidth = 6

// MinimapWidth is the number of braille characters for the minimap.
const MinimapWidth = 5

// ScrollBar is a vertical scrollbar that shows scroll position in terminal history.
// It's designed to be rendered alongside (not overlaying) the terminal content.
type ScrollBar struct {
	// Terminal integration
	vterm *parser.VTerm

	// Dimensions
	height int

	// Visibility
	visible bool

	// Callback when visibility changes (triggers terminal resize)
	onVisibilityChanged func(visible bool)

	// Styling
	trackStyle  tcell.Style
	thumbStyle  tcell.Style
	borderStyle tcell.Style

	mu sync.Mutex
}

// NewScrollBar creates a new scrollbar for the given terminal.
func NewScrollBar(vterm *parser.VTerm, onVisibilityChanged func(visible bool)) *ScrollBar {
	tm := theming.ForApp("texelterm")
	bgColor := tm.GetSemanticColor("bg.surface")
	fgColor := tm.GetSemanticColor("text.muted")
	accentColor := tm.GetSemanticColor("accent.primary")

	return &ScrollBar{
		vterm:               vterm,
		visible:             false,
		onVisibilityChanged: onVisibilityChanged,
		trackStyle:          tcell.StyleDefault.Foreground(fgColor).Background(bgColor),
		thumbStyle:          tcell.StyleDefault.Foreground(accentColor).Background(accentColor),
		borderStyle:         tcell.StyleDefault.Foreground(fgColor).Background(bgColor),
	}
}

// Show makes the scrollbar visible and triggers a terminal resize.
func (s *ScrollBar) Show() {
	s.mu.Lock()
	if s.visible {
		s.mu.Unlock()
		return
	}
	s.visible = true
	callback := s.onVisibilityChanged
	s.mu.Unlock()

	if callback != nil {
		callback(true)
	}
}

// Hide makes the scrollbar invisible and triggers a terminal resize.
func (s *ScrollBar) Hide() {
	s.mu.Lock()
	if !s.visible {
		s.mu.Unlock()
		return
	}
	s.visible = false
	callback := s.onVisibilityChanged
	s.mu.Unlock()

	if callback != nil {
		callback(false)
	}
}

// Toggle toggles the scrollbar visibility.
func (s *ScrollBar) Toggle() {
	s.mu.Lock()
	visible := s.visible
	s.mu.Unlock()

	if visible {
		s.Hide()
	} else {
		s.Show()
	}
}

// IsVisible returns whether the scrollbar is currently visible.
func (s *ScrollBar) IsVisible() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.visible
}

// Resize updates the scrollbar's height.
func (s *ScrollBar) Resize(height int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.height = height
}

// Render returns the scrollbar grid (ScrollBarWidth x height).
// Returns nil if not visible.
func (s *ScrollBar) Render() [][]texelcore.Cell {
	s.mu.Lock()
	visible := s.visible
	height := s.height
	s.mu.Unlock()

	if !visible || height <= 0 {
		return nil
	}

	// Create the grid
	grid := make([][]texelcore.Cell, height)
	for y := range grid {
		grid[y] = make([]texelcore.Cell, ScrollBarWidth)
	}

	// Get scroll metrics from vterm
	var thumbStartSub, thumbEndSub int
	if s.vterm != nil {
		thumbStartSub, thumbEndSub = s.calculateThumbPosition(height)
	} else {
		// No vterm, show full thumb
		thumbStartSub = 0
		thumbEndSub = height * 3
	}

	// Calculate braille minimap data (line lengths + colors per row)
	minimap := s.calculateBrailleMinimap(height)

	// Render each row using braille characters with colors
	for y := 0; y < height; y++ {
		s.renderBrailleMinimapRow(grid[y], y, thumbStartSub, thumbEndSub, &minimap[y])
	}

	return grid
}

// minimapSubpixelData holds data for one subpixel row.
type minimapSubpixelData struct {
	lineLength float64
	fg         tcell.Color
	bg         tcell.Color
}

// minimapRowData holds the computed data for one scrollbar row (4 subpixels).
type minimapRowData struct {
	subpixels [4]minimapSubpixelData
}

// calculateBrailleMinimap computes line lengths and colors for braille rendering.
// Each scrollbar row shows 4 lines (one per braille subpixel row).
// The entire history (including disk) is mapped to fit the scrollbar height.
// Returns data for each row including line lengths and dominant colors.
func (s *ScrollBar) calculateBrailleMinimap(height int) []minimapRowData {
	minimap := make([]minimapRowData, height)

	if s.vterm == nil || height <= 0 {
		return minimap
	}

	// Get ALL lines from disk + memory
	allLines, totalLines := s.vterm.GetAllLogicalLines()

	if totalLines <= 0 || len(allLines) == 0 {
		log.Printf("[SCROLLBAR] No lines: totalLines=%d, len=%d", totalLines, len(allLines))
		return minimap
	}

	termWidth := s.vterm.Width()
	if termWidth <= 0 {
		termWidth = 80
	}

	// Total subpixel rows = height * 4
	totalSubpixels := int64(height * 4)

	// Lines per subpixel row (map entire history to scrollbar)
	linesPerSubpixel := float64(totalLines) / float64(totalSubpixels)
	if linesPerSubpixel < 1 {
		linesPerSubpixel = 1
	}

	log.Printf("[SCROLLBAR] Minimap: totalLines=%d (from array len=%d), linesPerSubpixel=%.2f, height=%d",
		totalLines, len(allLines), linesPerSubpixel, height)

	for y := 0; y < height; y++ {
		// Calculate everything for this row from the preloaded lines
		s.analyzeRowFromLines(y, allLines, totalLines, linesPerSubpixel, termWidth, &minimap[y])
	}

	return minimap
}

// analyzeRowFromLines calculates all data for one scrollbar row from preloaded lines.
// Each subpixel gets its own dominant color from the same lines used for its content.
func (s *ScrollBar) analyzeRowFromLines(y int, allLines []*parser.LogicalLine, totalLines int64, linesPerSubpixel float64, termWidth int, row *minimapRowData) {
	// Process each subpixel independently
	for subRow := 0; subRow < 4; subRow++ {
		subpixelIdx := y*4 + subRow
		startIdx := int(float64(subpixelIdx) * linesPerSubpixel)
		endIdx := int(float64(subpixelIdx+1) * linesPerSubpixel)
		if endIdx > len(allLines) {
			endIdx = len(allLines)
		}

		if startIdx >= len(allLines) || startIdx >= endIdx {
			continue
		}

		var totalLength int64
		var lineCount int64

		// Track colors for THIS subpixel only
		fgCounts := make(map[tcell.Color]int)
		bgCounts := make(map[tcell.Color]int)

		// Process lines for this subpixel
		for i := startIdx; i < endIdx; i++ {
			line := allLines[i]
			if line == nil {
				continue
			}

			// Calculate line length
			length := len(line.Cells)
			for length > 0 && (line.Cells[length-1].Rune == ' ' || line.Cells[length-1].Rune == 0) {
				length--
			}
			totalLength += int64(length)
			lineCount++

			// Count colors for THIS subpixel
			for j := 0; j < length; j++ {
				cell := line.Cells[j]
				if cell.Rune == 0 || cell.Rune == ' ' {
					continue
				}
				fgCounts[s.parserColorToTcell(cell.FG)]++
				bgCounts[s.parserColorToTcell(cell.BG)]++
			}
		}

		// Set line length for this subpixel
		if lineCount > 0 {
			avgLength := float64(totalLength) / float64(lineCount)
			row.subpixels[subRow].lineLength = avgLength / float64(termWidth)
			if row.subpixels[subRow].lineLength > 1.0 {
				row.subpixels[subRow].lineLength = 1.0
			}
		}

		// Find the brightest color from both FG and BG
		// Use it as FG only (no background painting)
		row.subpixels[subRow].fg = s.pickBrightestColor(fgCounts, bgCounts)
		row.subpixels[subRow].bg = tcell.ColorDefault
	}
}

// analyzeLines calculates line length and dominant colors in a single pass.
// This ensures content and colors are derived from exactly the same data.
func (s *ScrollBar) analyzeLines(startLine, endLine int64, termWidth int) minimapSubpixelData {
	result := minimapSubpixelData{
		fg: tcell.ColorDefault,
		bg: tcell.ColorDefault,
	}

	if s.vterm == nil || startLine >= endLine {
		return result
	}

	var totalLength int64
	var lineCount int64
	var cellCount int64
	fgCounts := make(map[tcell.Color]int)
	bgCounts := make(map[tcell.Color]int)

	for lineIdx := startLine; lineIdx < endLine; lineIdx++ {
		line := s.vterm.GetLogicalLine(lineIdx)
		if line == nil {
			continue
		}

		// Calculate line length (trim trailing spaces)
		length := len(line.Cells)
		for length > 0 && (line.Cells[length-1].Rune == ' ' || line.Cells[length-1].Rune == 0) {
			length--
		}
		totalLength += int64(length)
		lineCount++

		// Count ALL non-empty cells for colors (no sampling)
		for i := 0; i < length; i++ {
			cell := line.Cells[i]
			if cell.Rune == 0 || cell.Rune == ' ' {
				continue
			}
			cellCount++

			fg := s.parserColorToTcell(cell.FG)
			bg := s.parserColorToTcell(cell.BG)

			fgCounts[fg]++
			bgCounts[bg]++
		}
	}

	// Calculate average line length
	if lineCount > 0 {
		avgLength := float64(totalLength) / float64(lineCount)
		result.lineLength = avgLength / float64(termWidth)
		if result.lineLength > 1.0 {
			result.lineLength = 1.0
		}
	}

	// Find dominant colors (including default)
	maxFgCount := 0
	for color, count := range fgCounts {
		if count > maxFgCount {
			maxFgCount = count
			result.fg = color
		}
	}

	maxBgCount := 0
	for color, count := range bgCounts {
		if count > maxBgCount {
			maxBgCount = count
			result.bg = color
		}
	}

	// Debug: log details for first few line ranges
	if startLine < 20 {
		log.Printf("[SCROLLBAR DEBUG] lines [%d-%d): length=%.2f, fg=%v (count=%d), bg=%v (count=%d), cells=%d, lines=%d",
			startLine, endLine, result.lineLength, result.fg, maxFgCount, result.bg, maxBgCount, cellCount, lineCount)
	}

	return result
}


// pickMostVividColor finds the most vivid (highest chroma) color from combined FG and BG counts.
// Uses the color with highest saturation that has significant presence.
func (s *ScrollBar) pickBrightestColor(fgCounts, bgCounts map[tcell.Color]int) tcell.Color {
	// Combine counts from both FG and BG
	combined := make(map[tcell.Color]int)
	for color, count := range fgCounts {
		combined[color] += count
	}
	for color, count := range bgCounts {
		combined[color] += count
	}

	if len(combined) == 0 {
		return tcell.ColorDefault
	}

	// Calculate total and threshold
	var total int
	for _, count := range combined {
		total += count
	}
	threshold := total / 10 // 10% minimum presence

	// Find most vivid color with significant presence
	var mostVivid tcell.Color
	var maxChroma float64 = -1

	for color, count := range combined {
		if color == tcell.ColorDefault || count < threshold {
			continue
		}

		chroma := s.colorChroma(color)
		if chroma > maxChroma {
			maxChroma = chroma
			mostVivid = color
		}
	}

	if mostVivid != tcell.ColorDefault {
		return mostVivid
	}

	// Fallback: find any non-default with max count
	var fallback tcell.Color
	var maxCount int
	for color, count := range combined {
		if color != tcell.ColorDefault && count > maxCount {
			maxCount = count
			fallback = color
		}
	}

	return fallback
}

// colorChroma returns the chroma (saturation/vividness) of a color using OKLCH.
// Higher values mean more vivid/colorful.
func (s *ScrollBar) colorChroma(c tcell.Color) float64 {
	r, g, b := c.RGB()
	_, chroma, _ := texelcolor.RGBToOKLCH(r, g, b)
	return chroma
}

// parserColorToTcell converts a parser.Color to tcell.Color.
func (s *ScrollBar) parserColorToTcell(c parser.Color) tcell.Color {
	switch c.Mode {
	case parser.ColorModeDefault:
		return tcell.ColorDefault
	case parser.ColorModeStandard:
		// Standard ANSI colors 0-7 (or 0-15 with bright)
		return tcell.PaletteColor(int(c.Value))
	case parser.ColorMode256:
		return tcell.PaletteColor(int(c.Value))
	case parser.ColorModeRGB:
		return tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B))
	default:
		return tcell.ColorDefault
	}
}


// Sextant block characters for thumb with 3x vertical resolution
const (
	blockFull   = '█'  // U+2588 - full block
	blockTop    = '🬂' // U+1FB02 - top third
	blockMiddle = '🬋' // U+1FB0B - middle third
	blockBottom = '🬭' // U+1FB2D - bottom third
)

// renderBrailleMinimapRow renders a scrollbar row using braille characters.
// Each row shows 4 lines as horizontal bars using braille subpixels.
// thumbStartSub and thumbEndSub are in sub-row units (3 per row).
func (s *ScrollBar) renderBrailleMinimapRow(row []texelcore.Cell, y int, thumbStartSub, thumbEndSub int, data *minimapRowData) {
	// This row's sub-row range: [y*3, y*3+3)
	rowSubStart := y * 3
	rowSubEnd := y*3 + 3

	// Determine if/how thumb covers this row
	hasThumb := thumbEndSub > rowSubStart && thumbStartSub < rowSubEnd

	// Column 0: border with thumb indicator using block chars
	borderChar := '│'
	borderStyle := s.borderStyle
	if hasThumb {
		borderChar, borderStyle = s.getThumbBlockChar(thumbStartSub, thumbEndSub)
	}
	row[0] = texelcore.Cell{
		Ch:    borderChar,
		Style: borderStyle,
	}

	// Minimap content style
	fg, _ := s.pickDominantSubpixelColor(data)
	defaultFg, defaultBg, _ := s.trackStyle.Decompose()
	if fg == tcell.ColorDefault {
		fg = defaultFg
	}
	style := tcell.StyleDefault.Foreground(fg).Background(defaultBg)

	// Extract line lengths for braille building
	var lineLengths [4]float64
	for i := 0; i < 4; i++ {
		lineLengths[i] = data.subpixels[i].lineLength
	}

	// Build MinimapWidth braille characters (columns 1 to ScrollBarWidth-1)
	brailleChars := s.buildBrailleRow(lineLengths[:])

	for x := 1; x < ScrollBarWidth; x++ {
		charIdx := x - 1
		if charIdx < MinimapWidth {
			row[x] = texelcore.Cell{
				Ch:    brailleChars[charIdx],
				Style: style,
			}
		}
	}
}

// getThumbBlockChar returns the block character and style for thumb position.
// thumbStartSub, thumbEndSub: the sub-row range of the thumb
func (s *ScrollBar) getThumbBlockChar(thumbStartSub, thumbEndSub int) (rune, tcell.Style) {
	// Use accent color as foreground, border foreground as background for continuity
	accentFg, _, _ := s.thumbStyle.Decompose()
	borderFg, _, _ := s.borderStyle.Decompose()
	style := tcell.StyleDefault.Foreground(accentFg).Background(borderFg)

	// Check if thumb fits in a single row (start and end are in the same row)
	startRow := thumbStartSub / 3
	endRow := (thumbEndSub - 1) / 3 // -1 because endSub is exclusive

	// If thumb spans multiple rows, use full block
	if startRow != endRow {
		return blockFull, style
	}

	// Single row thumb - cycle through top → middle → bottom based on position mod 3
	switch thumbStartSub % 3 {
	case 0:
		return blockTop, style
	case 1:
		return blockMiddle, style
	case 2:
		return blockBottom, style
	default:
		return blockFull, style
	}
}

// pickDominantSubpixelColor picks the most common color across all 4 subpixels.
func (s *ScrollBar) pickDominantSubpixelColor(data *minimapRowData) (tcell.Color, tcell.Color) {
	// Count colors across all subpixels, weighted by line length
	fgVotes := make(map[tcell.Color]float64)
	bgVotes := make(map[tcell.Color]float64)

	for i := 0; i < 4; i++ {
		sp := &data.subpixels[i]
		if sp.lineLength > 0 {
			fgVotes[sp.fg] += sp.lineLength
			bgVotes[sp.bg] += sp.lineLength
		}
	}

	// Find color with most votes
	var bestFg, bestBg tcell.Color
	var maxFgVotes, maxBgVotes float64

	for color, votes := range fgVotes {
		if votes > maxFgVotes {
			maxFgVotes = votes
			bestFg = color
		}
	}

	for color, votes := range bgVotes {
		if votes > maxBgVotes {
			maxBgVotes = votes
			bestBg = color
		}
	}

	return bestFg, bestBg
}

// buildBrailleRow builds MinimapWidth braille characters representing 4 horizontal lines.
// lineLengths[0-3] are the lengths (0.0-1.0) for each of the 4 subpixel rows.
// Returns MinimapWidth braille runes.
func (s *ScrollBar) buildBrailleRow(lineLengths []float64) [MinimapWidth]rune {
	var chars [MinimapWidth]rune

	// MinimapWidth braille chars × 2 dots per char = horizontal pixels
	const totalPixels = MinimapWidth * 2

	// Check if all lengths are 0 (no data) - show a track pattern
	allZero := true
	for _, l := range lineLengths {
		if l > 0 {
			allZero = false
			break
		}
	}
	if allZero {
		// Show a visible dotted track pattern (left column dots)
		// Pattern: ⡇ (dots 1,2,3,7 = 1+2+4+64 = 71)
		trackChar := rune(brailleBase + 71)
		for i := range chars {
			if i == 0 {
				chars[i] = trackChar // First char has dots
			} else {
				chars[i] = brailleBase // Rest empty
			}
		}
		return chars
	}

	for charIdx := 0; charIdx < MinimapWidth; charIdx++ {
		var pattern rune = brailleBase

		// For each of the 4 subpixel rows
		for subRow := 0; subRow < 4; subRow++ {
			if subRow >= len(lineLengths) {
				continue
			}

			// Convert line length to pixel count (0-totalPixels)
			pixels := int(lineLengths[subRow] * totalPixels)
			if pixels > totalPixels {
				pixels = totalPixels
			}

			// Calculate which dots to set for this character
			// charIdx 0: pixels 0-1, charIdx 1: pixels 2-3, etc.
			leftPixel := charIdx * 2
			rightPixel := charIdx*2 + 1

			// Set left dot if line reaches this pixel
			if pixels > leftPixel {
				pattern += brailleDots[subRow][0]
			}
			// Set right dot if line reaches this pixel
			if pixels > rightPixel {
				pattern += brailleDots[subRow][1]
			}
		}

		chars[charIdx] = pattern
	}

	return chars
}

// calculateThumbPosition calculates the thumb position based on scroll state.
// Returns (thumbStart, thumbEnd) in sub-row coordinates (3x resolution).
// Each row has 3 sub-positions: top, middle, bottom.
// Uses logical lines to match the minimap coordinate system.
func (s *ScrollBar) calculateThumbPosition(height int) (int, int) {
	if s.vterm == nil || height <= 2 {
		return 0, height * 3
	}

	// Get scroll metrics - use logical lines to match minimap
	scrollOffset := s.vterm.ScrollOffset()
	_, totalLines := s.vterm.GetAllLogicalLines()
	viewportHeight := int64(s.vterm.Height())

	// Total sub-rows (3x resolution)
	totalSubRows := height * 3

	// If no scrollable content, thumb fills entire track
	if totalLines <= viewportHeight {
		return 0, totalSubRows
	}

	// Calculate thumb size in sub-rows (proportional to viewport/total)
	thumbSize := int(float64(totalSubRows) * float64(viewportHeight) / float64(totalLines))
	if thumbSize < 1 {
		thumbSize = 1
	}
	if thumbSize > totalSubRows {
		thumbSize = totalSubRows
	}

	// Calculate thumb position
	// scrollOffset 0 = at bottom (live edge), higher = scrolled up into history
	maxScroll := totalLines - viewportHeight
	if maxScroll <= 0 {
		return 0, totalSubRows
	}

	// Position: scrollOffset 0 -> thumb at bottom, maxScroll -> thumb at top
	scrollRatio := float64(scrollOffset) / float64(maxScroll)
	thumbTop := int(math.Ceil(float64(totalSubRows-thumbSize) * (1.0 - scrollRatio)))

	thumbStart := thumbTop
	thumbEnd := thumbTop + thumbSize

	// Clamp
	if thumbStart < 0 {
		thumbStart = 0
	}
	if thumbEnd > totalSubRows {
		thumbEnd = totalSubRows
	}

	return thumbStart, thumbEnd
}


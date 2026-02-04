// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/scrollbar.go
// Summary: Vertical scrollbar with minimap showing line length distribution.
// Usage: Shows scroll position and provides visual minimap of history content.

package texelterm

import (
	"math"
	"sync"
	"time"

	texelcolor "github.com/framegrace/texelui/color"
	texelcore "github.com/framegrace/texelui/core"
	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/internal/theming"
	"github.com/framegrace/texelui/theme"
)

// Debounce delay for minimap invalidation
const minimapDebounceDelay = 100 * time.Millisecond

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
	trackStyle           tcell.Style
	thumbStyle           tcell.Style
	borderStyle          tcell.Style
	accentColor          tcell.Color // For thumb
	searchHighlightColor tcell.Color // For search result highlighting on minimap

	// Cached minimap data (only recalculated when invalidated)
	cachedMinimap      []minimapRowData
	cachedMinimapValid bool
	cachedTotalLines   int64

	// Search results for highlighting
	searchResultLines map[int64]bool // Set of global line indices with search results

	// Debounce timer for invalidation
	invalidateTimer   *time.Timer
	pendingInvalidate bool

	mu sync.Mutex
}

// NewScrollBar creates a new scrollbar for the given terminal.
func NewScrollBar(vterm *parser.VTerm, onVisibilityChanged func(visible bool)) *ScrollBar {
	tm := theming.ForApp("texelterm")
	bgColor := tm.GetSemanticColor("bg.surface")
	fgColor := tm.GetSemanticColor("text.muted")
	accentColor := tm.GetSemanticColor("accent.primary")

	// Green from palette for search result highlighting
	greenColor := theme.ResolveColorName("green")

	return &ScrollBar{
		vterm:                vterm,
		visible:              false,
		onVisibilityChanged:  onVisibilityChanged,
		trackStyle:           tcell.StyleDefault.Foreground(fgColor).Background(bgColor),
		thumbStyle:           tcell.StyleDefault.Foreground(accentColor).Background(accentColor),
		borderStyle:          tcell.StyleDefault.Foreground(fgColor).Background(bgColor),
		accentColor:          accentColor,
		searchHighlightColor: greenColor,
		searchResultLines:    make(map[int64]bool),
	}
}

// SetSearchHighlightColor sets the color used for search result markers on the minimap.
func (s *ScrollBar) SetSearchHighlightColor(color tcell.Color) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.searchHighlightColor = color
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
	if s.height != height {
		s.height = height
		s.cachedMinimapValid = false // Invalidate on resize
	}
}

// Invalidate marks the minimap cache as stale with debouncing.
// Call this when terminal content changes (new lines, reflow, etc.)
// The actual invalidation is debounced to avoid rapid recalculations during fast output.
func (s *ScrollBar) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Mark as pending invalidation
	s.pendingInvalidate = true

	// Reset or start debounce timer
	if s.invalidateTimer != nil {
		s.invalidateTimer.Stop()
	}
	s.invalidateTimer = time.AfterFunc(minimapDebounceDelay, func() {
		s.mu.Lock()
		if s.pendingInvalidate {
			s.cachedMinimapValid = false
			s.pendingInvalidate = false
		}
		s.mu.Unlock()
	})
}

// SetSearchResults updates the search result line indices for minimap highlighting.
// Pass nil or empty slice to clear results.
func (s *ScrollBar) SetSearchResults(results []parser.SearchResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clear and rebuild the set
	s.searchResultLines = make(map[int64]bool)
	for _, r := range results {
		s.searchResultLines[r.GlobalLineIdx] = true
	}

	// Invalidate cache since search results affect rendering
	s.cachedMinimapValid = false
}

// ClearSearchResults removes all search result highlights from the minimap.
func (s *ScrollBar) ClearSearchResults() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.searchResultLines = make(map[int64]bool)
	s.cachedMinimapValid = false
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
	var useSmoothBlocks bool
	if s.vterm != nil {
		thumbStartSub, thumbEndSub, useSmoothBlocks = s.calculateThumbPosition(height)
	} else {
		// No vterm, show full thumb
		thumbStartSub = 0
		thumbEndSub = height * 3
		useSmoothBlocks = false
	}

	// Calculate braille minimap data (line lengths + colors per row)
	minimap := s.calculateBrailleMinimap(height)

	// Render each row using braille characters with colors
	for y := 0; y < height; y++ {
		s.renderBrailleMinimapRow(grid[y], y, thumbStartSub, thumbEndSub, useSmoothBlocks, &minimap[y])
	}

	return grid
}

// minimapSubpixelData holds data for one subpixel row.
type minimapSubpixelData struct {
	lineLength      float64
	fg              tcell.Color
	bg              tcell.Color
	hasSearchResult bool // True if this subpixel range contains search results
}

// minimapRowData holds the computed data for one scrollbar row (4 subpixels).
type minimapRowData struct {
	subpixels [4]minimapSubpixelData
}

// calculateBrailleMinimap computes line lengths and colors for braille rendering.
// Uses cached data if available, otherwise recalculates from all lines.
// Each scrollbar row shows 4 lines (one per braille subpixel row).
// The entire history (including disk) is mapped to fit the scrollbar height.
// Returns data for each row including line lengths and dominant colors.
func (s *ScrollBar) calculateBrailleMinimap(height int) []minimapRowData {
	s.mu.Lock()
	// Return cached data if valid and same height
	if s.cachedMinimapValid && len(s.cachedMinimap) == height {
		result := s.cachedMinimap
		s.mu.Unlock()
		return result
	}
	s.mu.Unlock()

	// Need to recalculate
	minimap := make([]minimapRowData, height)

	if s.vterm == nil || height <= 0 {
		return minimap
	}

	// Get ALL lines from disk + memory
	allLines, globalOffset, totalLines := s.vterm.GetAllLogicalLines()

	if totalLines <= 0 || len(allLines) == 0 {
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

	// Get search result lines (thread-safe copy)
	s.mu.Lock()
	searchLines := s.searchResultLines
	s.mu.Unlock()

	for y := 0; y < height; y++ {
		// Calculate everything for this row from the preloaded lines
		s.analyzeRowFromLines(y, allLines, globalOffset, linesPerSubpixel, termWidth, searchLines, &minimap[y])
	}

	// Cache the result
	s.mu.Lock()
	s.cachedMinimap = minimap
	s.cachedTotalLines = totalLines
	s.cachedMinimapValid = true
	s.mu.Unlock()

	return minimap
}

// analyzeRowFromLines calculates all data for one scrollbar row from preloaded lines.
// Each subpixel gets its own dominant color from the same lines used for its content.
// searchLines is a set of global line indices that contain search results.
func (s *ScrollBar) analyzeRowFromLines(y int, allLines []*parser.LogicalLine, globalOffset int64, linesPerSubpixel float64, termWidth int, searchLines map[int64]bool, row *minimapRowData) {
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
		hasSearchResult := false

		// Track colors for THIS subpixel only
		fgCounts := make(map[tcell.Color]int)
		bgCounts := make(map[tcell.Color]int)

		// Process lines for this subpixel
		for i := startIdx; i < endIdx; i++ {
			line := allLines[i]
			if line == nil {
				continue
			}

			// Check if this line has a search result
			globalIdx := globalOffset + int64(i)
			if searchLines[globalIdx] {
				hasSearchResult = true
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
		row.subpixels[subRow].hasSearchResult = hasSearchResult
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
// useSmoothBlocks enables the smooth block trick when raw thumb size < 1 character.
func (s *ScrollBar) renderBrailleMinimapRow(row []texelcore.Cell, y int, thumbStartSub, thumbEndSub int, useSmoothBlocks bool, data *minimapRowData) {
	// This row's sub-row range: [y*3, y*3+3)
	rowSubStart := y * 3
	rowSubEnd := y*3 + 3

	// Determine if/how thumb covers this row
	hasThumb := thumbEndSub > rowSubStart && thumbStartSub < rowSubEnd

	// Column 0: border with thumb indicator using block chars
	borderChar := '│'
	borderStyle := s.borderStyle
	if hasThumb {
		borderChar, borderStyle = s.getThumbBlockChar(thumbStartSub, useSmoothBlocks)
	}
	row[0] = texelcore.Cell{
		Ch:    borderChar,
		Style: borderStyle,
	}

	// Check if any subpixel in this row has a search result
	hasSearchResult := false
	for i := 0; i < 4; i++ {
		if data.subpixels[i].hasSearchResult {
			hasSearchResult = true
			break
		}
	}

	// Minimap content style - use accent background for search results
	fg, _ := s.pickDominantSubpixelColor(data)
	defaultFg, defaultBg, _ := s.trackStyle.Decompose()
	if fg == tcell.ColorDefault {
		fg = defaultFg
	}
	bg := defaultBg
	if hasSearchResult {
		// Use mauve background to highlight search results
		bg = s.searchHighlightColor
	}
	style := tcell.StyleDefault.Foreground(fg).Background(bg)

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
// thumbStartSub: the starting sub-row position of the thumb
// useSmoothBlocks: true when raw thumb size < 1 character (enables 3x resolution)
func (s *ScrollBar) getThumbBlockChar(thumbStartSub int, useSmoothBlocks bool) (rune, tcell.Style) {
	// Use accent color as foreground, border foreground as background for continuity
	accentFg, _, _ := s.thumbStyle.Decompose()
	borderFg, _, _ := s.borderStyle.Decompose()
	style := tcell.StyleDefault.Foreground(accentFg).Background(borderFg)

	// Only use smooth blocks when raw thumb size < 1 character
	// This gives 3x resolution for very large histories
	if !useSmoothBlocks {
		return blockFull, style
	}

	// Smooth block mode - cycle through top → middle → bottom based on position mod 3
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
// Returns (thumbStart, thumbEnd, useSmoothBlocks) in sub-row coordinates (3x resolution).
// Each row has 3 sub-positions: top, middle, bottom.
// useSmoothBlocks is true when the raw thumb size < 3 (less than one character),
// enabling the smooth block trick for 3x resolution.
// Uses logical lines to match the minimap coordinate system.
func (s *ScrollBar) calculateThumbPosition(height int) (int, int, bool) {
	if s.vterm == nil || height <= 2 {
		return 0, height * 3, false
	}

	// Get scroll metrics
	// scrollOffset is in PHYSICAL lines (accounts for line wrapping)
	scrollOffset := s.vterm.ScrollOffset()
	viewportHeight := int64(s.vterm.Height())

	// Get both logical and physical line counts
	// - Logical lines: for minimap proportions (search results use logical indices)
	// - Physical lines: for scroll position (scrollOffset is physical)
	s.mu.Lock()
	totalLogicalLines := s.cachedTotalLines
	s.mu.Unlock()

	if totalLogicalLines <= 0 {
		totalLogicalLines = s.vterm.TotalLogicalLines()
	}

	totalPhysicalLines := s.vterm.TotalPhysicalLines()

	// Total sub-rows (3x resolution)
	totalSubRows := height * 3

	// If no scrollable content, thumb fills entire track
	if totalLogicalLines <= viewportHeight {
		return 0, totalSubRows, false
	}

	// Calculate raw thumb size in sub-rows (proportional to viewport/total)
	// Use logical lines for size to match minimap proportions
	rawThumbSize := float64(totalSubRows) * float64(viewportHeight) / float64(totalLogicalLines)

	// Use smooth blocks when raw size < 3 (less than one full character)
	// This gives us 3x resolution for very large histories
	useSmoothBlocks := rawThumbSize < 3.0

	thumbSize := int(rawThumbSize)
	if thumbSize < 1 {
		thumbSize = 1
	}
	if thumbSize > totalSubRows {
		thumbSize = totalSubRows
	}

	// Calculate thumb position
	// scrollOffset is in PHYSICAL lines, so use physical lines for maxScroll
	// This ensures thumb position is accurate relative to actual scroll state
	maxScroll := totalPhysicalLines - viewportHeight
	if maxScroll <= 0 {
		return 0, totalSubRows, false
	}

	// Position: scrollOffset 0 -> thumb at bottom, maxScroll -> thumb at top
	scrollRatio := float64(scrollOffset) / float64(maxScroll)
	thumbTop := int(math.Ceil(float64(totalSubRows-thumbSize) * (1.0 - scrollRatio)))

	thumbStart := thumbTop
	thumbEnd := thumbTop + thumbSize

	// When not using smooth blocks, snap to row boundaries to avoid
	// the thumb appearing on different number of rows depending on position
	if !useSmoothBlocks {
		// Round thumbStart down to row boundary
		thumbStart = (thumbStart / 3) * 3
		// Ensure thumbSize is at least one full row
		if thumbSize < 3 {
			thumbSize = 3
		}
		// Round thumbSize up to full rows
		thumbSize = ((thumbSize + 2) / 3) * 3
		thumbEnd = thumbStart + thumbSize
	}

	// Clamp
	if thumbStart < 0 {
		thumbStart = 0
	}
	if thumbEnd > totalSubRows {
		thumbEnd = totalSubRows
	}

	return thumbStart, thumbEnd, useSmoothBlocks
}

// HandleClick handles a mouse click on the scrollbar.
// x is relative to the scrollbar (0 = border column).
// y is the row clicked.
// Returns (targetScrollOffset, ok). If ok is false, the click should be ignored.
func (s *ScrollBar) HandleClick(x, y int) (int64, bool) {
	s.mu.Lock()
	visible := s.visible
	height := s.height
	s.mu.Unlock()

	if !visible || height <= 0 || s.vterm == nil {
		return 0, false
	}

	// Ignore clicks on the border column (x == 0)
	if x < 1 {
		return 0, false
	}

	// Get total lines
	_, _, totalLines := s.vterm.GetAllLogicalLines()
	if totalLines <= 0 {
		return 0, false
	}

	viewportHeight := int64(s.vterm.Height())
	if totalLines <= viewportHeight {
		return 0, false // No scrolling needed
	}

	// Convert click position to scroll offset
	// y=0 is top (oldest content, max scroll), y=height-1 is bottom (newest, scroll=0)
	clickRatio := float64(y) / float64(height-1)
	maxScroll := totalLines - viewportHeight

	// Invert: top of scrollbar = max scroll, bottom = 0
	targetOffset := int64(float64(maxScroll) * (1.0 - clickRatio))

	// Clamp
	if targetOffset < 0 {
		targetOffset = 0
	}
	if targetOffset > maxScroll {
		targetOffset = maxScroll
	}

	return targetOffset, true
}

// Width returns the scrollbar width.
func (s *ScrollBar) Width() int {
	return ScrollBarWidth
}


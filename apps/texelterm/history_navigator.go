// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/history_navigator.go
// Summary: 2-line overlay card for searching terminal history.
// Usage: Opened with Ctrl+Shift+F, provides full-text search with navigation and keymap hints.

package texelterm

import (
	"fmt"
	"log"
	"sync"
	"time"

	texelcore "github.com/framegrace/texelui/core"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/internal/effects"
	"github.com/framegrace/texelation/internal/theming"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/theme"
	"github.com/framegrace/texelui/widgets"
	"github.com/gdamore/tcell/v2"
)

// tcellToParserColor converts a tcell.Color to a parser.Color.
func tcellToParserColor(c tcell.Color) parser.Color {
	if c == tcell.ColorDefault {
		return parser.Color{Mode: parser.ColorModeDefault}
	}
	// Check if it's a true color (RGB)
	if c.IsRGB() {
		r, g, b := c.RGB()
		return parser.Color{Mode: parser.ColorModeRGB, R: uint8(r), G: uint8(g), B: uint8(b)}
	}
	// Check if it's a 256-color palette index
	if c <= 255 {
		return parser.Color{Mode: parser.ColorMode256, Value: uint8(c)}
	}
	// Fallback to standard color
	return parser.Color{Mode: parser.ColorModeStandard, Value: uint8(c % 8)}
}

// HistoryNavigator is a 2-line overlay card for searching terminal history.
// It provides:
//   - Full-text search with prev/next navigation
//   - Context-sensitive keymap hints
//   - Keyboard navigation (Tab cycles results, arrows cycle widgets, Escape closes)
type HistoryNavigator struct {
	// UI components
	ui *core.UIManager

	// Terminal integration
	vterm       *parser.VTerm
	searchIndex *parser.SQLiteSearchIndex
	onClose     func()

	// Widgets - Row 1: Search
	searchIcon  *widgets.Label
	searchInput *widgets.Input
	prevBtn     *widgets.Button
	nextBtn     *widgets.Button
	counterLbl  *widgets.Label

	// Widgets - Row 2: Keymap hints
	keymapLbl *widgets.Label

	// Focus tracking for keymap hints
	focusedWidget core.Widget

	// Search state
	searchResults []parser.SearchResult
	resultIndex   int

	// Highlight colors (for styled search highlighting)
	searchHighlightColor parser.Color // Unified color: selected match, line tint, scrollbar
	highlightAccentColor parser.Color // For other matches: just FG change
	lineTintIntensity    float32      // Blend intensity for line tint (default: 0.12)
	defaultBGColor       parser.Color // Terminal's default background for proper blending

	// Visibility and dimensions
	visible bool
	width   int
	height  int

	// Refresh and lifecycle
	refreshCh chan<- bool
	stopCh    chan struct{}

	// Debouncing for search
	searchTimer *time.Timer

	// Callback when search results change (for scrollbar minimap highlighting)
	onSearchResultsChanged func(results []parser.SearchResult)
	timerMu     sync.Mutex

	// Scroll animation state
	animating     bool
	animStopCh    chan struct{}
	animMu        sync.Mutex

	// Scroll animation config
	ScrollAnimMaxLines  int64         // Max lines for animated scroll (0 = disabled)
	ScrollAnimMinTime   time.Duration // Min animation duration
	ScrollAnimMaxTime   time.Duration // Max animation duration
	ScrollAnimFrameRate int           // Frames per second
	ScrollAnimEasing    effects.EasingFunc

	// Long jump edge animation config
	ScrollAnimEdgeLines      int           // Lines to show at start/end edges (default: 5)
	ScrollAnimEdgeStartDelay time.Duration // Initial delay between edge lines (default: 80ms)
	ScrollAnimEdgeEndDelay   time.Duration // Final delay for edge lines (default: 25ms)

	mu sync.Mutex
}

// Scroll animation defaults
const (
	defaultScrollAnimMaxLines       = 500
	defaultScrollAnimMinTime        = 400 * time.Millisecond
	defaultScrollAnimMaxTime        = 1500 * time.Millisecond
	defaultScrollAnimFrameRate      = 60
	defaultScrollAnimEdgeLines      = 5
	defaultScrollAnimEdgeStartDelay = 80 * time.Millisecond
	defaultScrollAnimEdgeEndDelay   = 25 * time.Millisecond
)

// NewHistoryNavigator creates a new history navigator card.
func NewHistoryNavigator(vterm *parser.VTerm, searchIndex *parser.SQLiteSearchIndex, onClose func()) *HistoryNavigator {
	h := &HistoryNavigator{
		ui:          core.NewUIManager(),
		vterm:       vterm,
		searchIndex: searchIndex,
		onClose:     onClose,
		stopCh:      make(chan struct{}),
		// Scroll animation defaults
		ScrollAnimMaxLines:       defaultScrollAnimMaxLines,
		ScrollAnimMinTime:        defaultScrollAnimMinTime,
		ScrollAnimMaxTime:        defaultScrollAnimMaxTime,
		ScrollAnimFrameRate:      defaultScrollAnimFrameRate,
		ScrollAnimEasing:         effects.EaseInOutCubic,
		ScrollAnimEdgeLines:      defaultScrollAnimEdgeLines,
		ScrollAnimEdgeStartDelay: defaultScrollAnimEdgeStartDelay,
		ScrollAnimEdgeEndDelay:   defaultScrollAnimEdgeEndDelay,
	}

	// Disable status bar for this compact card
	h.ui.SetStatusBar(nil)

	// Create widgets
	h.createWidgets()

	// Set up event handlers
	h.setupEventHandlers()

	// Register as focus observer to update keymap hints
	h.ui.AddFocusObserver(h)

	return h
}

// OnFocusChanged implements core.FocusObserver to update keymap hints when focus changes.
// Note: This is called from UIManager with its lock held, so we must not acquire h.mu here
// to avoid deadlock. focusedWidget is only accessed from the UI goroutine so this is safe.
func (h *HistoryNavigator) OnFocusChanged(focused core.Widget) {
	h.focusedWidget = focused
	h.updateKeymapHint()
	h.requestRefresh()
}

// createWidgets creates all the UI widgets for the 2-line layout.
func (h *HistoryNavigator) createWidgets() {
	// Use theme colors
	tm := theming.ForApp("texelterm")
	bgColor := tm.GetSemanticColor("bg.surface")
	fgColor := tm.GetSemanticColor("text.primary")
	mutedColor := tm.GetSemanticColor("text.muted")
	accentColor := tm.GetSemanticColor("accent.primary")

	baseStyle := tcell.StyleDefault.Foreground(fgColor).Background(bgColor)
	mutedStyle := tcell.StyleDefault.Foreground(mutedColor).Background(bgColor)
	accentStyle := tcell.StyleDefault.Foreground(accentColor).Background(bgColor)

	// Initialize highlight colors for search results
	// Use green from palette as unified search highlight color
	// This color is used for: selected match text, line tint, scrollbar markers
	greenColor := theme.ResolveColorName("green")
	h.searchHighlightColor = tcellToParserColor(greenColor)
	h.highlightAccentColor = tcellToParserColor(mutedColor)
	h.lineTintIntensity = 0.12 // Subtle 12% background tint

	// Get actual terminal background for proper blending
	terminalBG := tm.GetSemanticColor("bg.base")
	h.defaultBGColor = tcellToParserColor(terminalBG)

	// Search widgets
	h.searchIcon = widgets.NewLabel("🔍")
	h.searchIcon.Style = accentStyle
	h.searchIcon.SetFocusable(false)

	h.searchInput = widgets.NewInput()
	h.searchInput.Placeholder = "Search history..."
	h.searchInput.Style = baseStyle
	h.searchInput.SetFocusable(true)

	h.prevBtn = widgets.NewButton("◀Prev")
	h.prevBtn.SetFocusable(true)

	h.nextBtn = widgets.NewButton("Next▶")
	h.nextBtn.SetFocusable(true)

	h.counterLbl = widgets.NewLabel("")
	h.counterLbl.Style = mutedStyle
	h.counterLbl.SetFocusable(false)

	// Row 2: Keymap hints
	h.keymapLbl = widgets.NewLabel("")
	h.keymapLbl.Style = mutedStyle
	h.keymapLbl.SetFocusable(false)

	// Add widgets to UI manager
	h.ui.AddWidget(h.searchIcon)
	h.ui.AddWidget(h.searchInput)
	h.ui.AddWidget(h.prevBtn)
	h.ui.AddWidget(h.nextBtn)
	h.ui.AddWidget(h.counterLbl)
	h.ui.AddWidget(h.keymapLbl)
}

// setupEventHandlers wires up all widget callbacks.
func (h *HistoryNavigator) setupEventHandlers() {
	// Search input - debounced search on change
	h.searchInput.OnChange = func(text string) {
		h.scheduleSearch(text)
	}
	h.searchInput.OnSubmit = func(text string) {
		// Jump to next result on Enter
		h.navigateToNextResult()
	}

	// Search navigation buttons
	h.prevBtn.OnClick = func() {
		h.navigateToPrevResult()
	}
	h.nextBtn.OnClick = func() {
		h.navigateToNextResult()
	}
}

// Show displays the navigator and focuses the search input.
func (h *HistoryNavigator) Show() {
	h.mu.Lock()
	h.visible = true
	h.ui.Focus(h.searchInput)
	h.mu.Unlock()
	h.requestRefresh()
}

// Hide closes the navigator and triggers the onClose callback.
func (h *HistoryNavigator) Hide() {
	h.mu.Lock()
	h.visible = false
	h.mu.Unlock()

	// Cancel any pending search
	h.timerMu.Lock()
	if h.searchTimer != nil {
		h.searchTimer.Stop()
		h.searchTimer = nil
	}
	h.timerMu.Unlock()

	// Clear search highlighting
	if h.vterm != nil {
		h.vterm.ClearSearchHighlight()
	}

	if h.onClose != nil {
		h.onClose()
	}
}

// IsVisible returns whether the navigator is currently shown.
func (h *HistoryNavigator) IsVisible() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.visible
}

// SetSearchResultsCallback sets a callback to be invoked when search results change.
// Used to notify the scrollbar for minimap highlighting.
func (h *HistoryNavigator) SetSearchResultsCallback(callback func(results []parser.SearchResult)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onSearchResultsChanged = callback
}

// Resize adjusts the navigator layout to fit the given dimensions.
// The navigator uses 2 lines at the bottom.
func (h *HistoryNavigator) Resize(cols, rows int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.width = cols
	h.height = rows // Store terminal height for mouse hit detection

	// UIManager gets full width but only 2 lines
	h.ui.Resize(cols, 2)
	h.layoutWidgets()
}

// layoutWidgets positions widgets in the 2-line layout.
func (h *HistoryNavigator) layoutWidgets() {
	if h.width < 40 {
		return // Too narrow to display
	}

	// Row 0: [🔍] [search input.........] [◀Prev] [Next▶] [1/42]
	x := 0

	// Search icon (3 chars: icon + space)
	h.searchIcon.SetPosition(x, 0)
	h.searchIcon.Resize(2, 1)
	x += 3

	// Calculate widths from right side
	// Button width = len(text) + 4 for "[ text ]" display format
	counterWidth := 8 // "999/999"
	btnWidth := 9     // "◀Prev" or "Next▶" (5 chars + 4 padding)
	rightWidgets := counterWidth + btnWidth*2 + 4

	// Search input gets remaining space
	inputWidth := max(h.width-x-rightWidgets, 10)
	h.searchInput.SetPosition(x, 0)
	h.searchInput.Resize(inputWidth, 1)
	x += inputWidth + 1

	// Prev button
	h.prevBtn.SetPosition(x, 0)
	h.prevBtn.Resize(btnWidth, 1)
	x += btnWidth + 1

	// Next button
	h.nextBtn.SetPosition(x, 0)
	h.nextBtn.Resize(btnWidth, 1)
	x += btnWidth + 1

	// Counter label
	h.counterLbl.SetPosition(x, 0)
	h.counterLbl.Resize(counterWidth, 1)

	// Row 1: Keymap hints (full width)
	h.keymapLbl.SetPosition(0, 1)
	h.keymapLbl.Resize(h.width, 1)
	h.updateKeymapHint()
}

// updateKeymapHint updates the keymap label based on the currently focused widget.
// Assumes h.mu is held or called from a safe context.
func (h *HistoryNavigator) updateKeymapHint() {
	var hint string
	switch h.focusedWidget {
	case h.searchInput:
		hint = "Tab/^N:Next  S-Tab/^P:Prev  Alt+↑↓:Scroll  ←→:Focus  Esc:Close"
	case h.prevBtn:
		hint = "Enter:Prev  Tab/^N:Next  S-Tab/^P:Prev  Alt+↑↓:Scroll  Esc:Close"
	case h.nextBtn:
		hint = "Enter:Next  Tab/^N:Next  S-Tab/^P:Prev  Alt+↑↓:Scroll  Esc:Close"
	default:
		hint = "Tab/^N:Next  S-Tab/^P:Prev  Alt+↑↓:Scroll  Esc:Close"
	}

	h.keymapLbl.Text = hint
}

// HandleKey processes keyboard input for the navigator.
// Returns true if the key was consumed.
func (h *HistoryNavigator) HandleKey(ev *tcell.EventKey) bool {
	h.mu.Lock()
	visible := h.visible
	h.mu.Unlock()

	if !visible {
		return false
	}

	// Pass through Alt+scroll keys to terminal for manual scrolling
	// This allows browsing around results while navigator is open
	if ev.Modifiers()&tcell.ModAlt != 0 {
		switch ev.Key() {
		case tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyUp, tcell.KeyDown:
			return false // Let terminal handle scroll
		}
	}

	// Handle Escape or Ctrl+Q to close
	// Note: Escape is often intercepted by texelui runtime, so Ctrl+Q is the reliable option
	if ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyCtrlQ {
		h.Hide()
		return true
	}

	// Tab/Shift+Tab: navigate search results
	if ev.Key() == tcell.KeyTab {
		if ev.Modifiers()&tcell.ModShift != 0 {
			h.navigateToPrevResult()
		} else {
			h.navigateToNextResult()
		}
		return true
	}
	if ev.Key() == tcell.KeyBacktab {
		h.navigateToPrevResult()
		return true
	}

	// Ctrl+N/Ctrl+P: also navigate results (vim-style)
	if ev.Key() == tcell.KeyCtrlN {
		h.navigateToNextResult()
		return true
	}
	if ev.Key() == tcell.KeyCtrlP {
		h.navigateToPrevResult()
		return true
	}

	// Enter: handle based on focused widget
	if ev.Key() == tcell.KeyEnter {
		switch h.focusedWidget {
		case h.searchInput:
			h.navigateToNextResult()
		case h.prevBtn:
			h.navigateToPrevResult()
		case h.nextBtn:
			h.navigateToNextResult()
		default:
			h.navigateToNextResult()
		}
		return true
	}

	// Arrow Left/Right: cycle focus between widgets
	if ev.Key() == tcell.KeyLeft {
		// Send synthetic BackTab to UIManager for focus cycling
		syntheticEv := tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone)
		h.ui.HandleKey(syntheticEv)
		h.requestRefresh()
		return true
	}
	if ev.Key() == tcell.KeyRight {
		// Send synthetic Tab to UIManager for focus cycling
		syntheticEv := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
		h.ui.HandleKey(syntheticEv)
		h.requestRefresh()
		return true
	}

	// Pass other keys to UIManager (don't hold lock - callbacks may need it)
	h.ui.HandleKey(ev)
	h.requestRefresh()
	return true
}

// HandleMouse processes mouse input for the navigator overlay.
// Returns true if the mouse event was consumed.
func (h *HistoryNavigator) HandleMouse(ev *tcell.EventMouse) bool {
	h.mu.Lock()
	visible := h.visible
	height := h.height
	h.mu.Unlock()

	if !visible {
		return false
	}

	// The overlay is at the bottom of the screen (last 2 rows)
	x, y := ev.Position()
	overlayStartY := height - 2
	if y < overlayStartY {
		return false // Click is above the overlay
	}

	// Adjust y to be relative to the overlay (0 or 1)
	adjustedY := y - overlayStartY

	// For clicks on row 0 (search row), handle focus explicitly before delegating
	// to UIManager. This prevents focus from being lost when clicking on the
	// search input or empty space.
	if ev.Buttons()&tcell.Button1 != 0 && adjustedY == 0 {
		// Get widget bounds
		inputX, _ := h.searchInput.Position()
		inputW, _ := h.searchInput.Size()
		prevX, _ := h.prevBtn.Position()
		prevW, _ := h.prevBtn.Size()
		nextX, _ := h.nextBtn.Position()
		nextW, _ := h.nextBtn.Size()

		if x >= inputX && x < inputX+inputW {
			// Click on search input
			h.ui.Focus(h.searchInput)
			h.requestRefresh()
			return true
		} else if x >= prevX && x < prevX+prevW {
			// Click on prev button
			h.ui.Focus(h.prevBtn)
			h.prevBtn.OnClick()
			h.requestRefresh()
			return true
		} else if x >= nextX && x < nextX+nextW {
			// Click on next button
			h.ui.Focus(h.nextBtn)
			h.nextBtn.OnClick()
			h.requestRefresh()
			return true
		}
		// Click on empty space - keep current focus
		return true
	}

	// For non-click events or row 1 (keymap hints), delegate to UIManager
	adjustedEv := tcell.NewEventMouse(
		x,
		adjustedY,
		ev.Buttons(),
		ev.Modifiers(),
	)
	return h.ui.HandleMouse(adjustedEv)
}

// Render draws the 1-line overlay at the bottom of the input buffer.
func (h *HistoryNavigator) Render(input [][]texelcore.Cell) [][]texelcore.Cell {
	h.mu.Lock()
	visible := h.visible
	h.mu.Unlock()

	if !visible || len(input) < 2 {
		return input
	}

	// Render UIManager to get the 2-line overlay (don't hold lock - ui has its own)
	overlay := h.ui.Render()

	// Copy overlay to bottom 2 lines of input buffer
	termHeight := len(input)
	for y := 0; y < 2 && y < len(overlay); y++ {
		targetY := termHeight - 2 + y
		if targetY >= 0 && targetY < termHeight {
			for x := 0; x < len(input[targetY]) && x < len(overlay[y]); x++ {
				input[targetY][x] = overlay[y][x]
			}
		}
	}

	return input
}

// SetRefreshNotifier sets the refresh channel for triggering redraws.
func (h *HistoryNavigator) SetRefreshNotifier(ch chan<- bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.refreshCh = ch
	h.ui.SetRefreshNotifier(ch)
}

// requestRefresh triggers a refresh if the channel is set.
func (h *HistoryNavigator) requestRefresh() {
	if h.refreshCh != nil {
		select {
		case h.refreshCh <- true:
		default:
		}
	}
}

// --- Search Logic ---

// scheduleSearch debounces search execution (150ms delay).
func (h *HistoryNavigator) scheduleSearch(query string) {
	h.timerMu.Lock()
	defer h.timerMu.Unlock()

	// Cancel any pending search
	if h.searchTimer != nil {
		h.searchTimer.Stop()
	}

	// Schedule new search after 150ms
	h.searchTimer = time.AfterFunc(150*time.Millisecond, func() {
		h.performSearch(query)
	})
}

// performSearch executes the search and updates results.
func (h *HistoryNavigator) performSearch(query string) {
	if h.searchIndex == nil {
		return
	}

	if query == "" {
		h.mu.Lock()
		h.searchResults = nil
		h.resultIndex = 0
		h.counterLbl.Text = ""
		callback := h.onSearchResultsChanged
		h.mu.Unlock()

		// Notify scrollbar that search results are cleared
		if callback != nil {
			callback(nil)
		}

		// Clear search highlighting when query is empty
		if h.vterm != nil {
			h.vterm.ClearSearchHighlight()
		}

		h.requestRefresh()
		return
	}

	// Search outside the lock (SQLite has its own locking)
	// Use high limit to ensure minimap shows all results
	results, err := h.searchIndex.Search(query, 10000)
	if err != nil {
		log.Printf("[HISTORY_NAV] Search error: %v", err)
		h.mu.Lock()
		h.counterLbl.Text = "Error"
		h.mu.Unlock()
		h.requestRefresh()
		return
	}

	h.mu.Lock()
	h.searchResults = results
	h.resultIndex = 0
	h.updateCounterDisplay()
	var firstResult *parser.SearchResult
	searchTerm := h.searchInput.Text // Capture for highlighting
	highlightColor := h.searchHighlightColor
	accentColor := h.highlightAccentColor
	lineTintIntensity := h.lineTintIntensity
	defaultBG := h.defaultBGColor
	callback := h.onSearchResultsChanged
	if len(results) > 0 {
		firstResult = &results[0]
	}
	h.mu.Unlock()

	// Notify scrollbar of new search results
	if callback != nil {
		callback(results)
	}

	// Auto-navigate to first result if any (outside lock)
	if h.vterm != nil {
		// Set styled search highlighting with the current line
		// Uses unified highlightColor for selected match and line tint
		currentLine := int64(-1)
		if firstResult != nil {
			currentLine = firstResult.GlobalLineIdx
		}
		h.vterm.SetSearchHighlightStyled(searchTerm, currentLine, highlightColor, accentColor, highlightColor, lineTintIntensity, defaultBG)

		if firstResult != nil {
			h.vterm.ScrollToGlobalLine(firstResult.GlobalLineIdx)
		}
	}

	h.requestRefresh()
}

// navigateToNextResult moves to the next search result.
func (h *HistoryNavigator) navigateToNextResult() {
	h.mu.Lock()
	if len(h.searchResults) == 0 {
		h.mu.Unlock()
		return
	}

	h.resultIndex = (h.resultIndex + 1) % len(h.searchResults)
	result := h.searchResults[h.resultIndex]
	h.updateCounterDisplay()
	h.mu.Unlock()

	// Call vterm outside the lock to avoid deadlock
	if h.vterm != nil {
		// Update the current highlight line before scrolling
		h.vterm.UpdateSearchHighlightLine(result.GlobalLineIdx)
		h.animateScrollToLine(result.GlobalLineIdx)
	}
}

// navigateToPrevResult moves to the previous search result.
func (h *HistoryNavigator) navigateToPrevResult() {
	h.mu.Lock()
	if len(h.searchResults) == 0 {
		h.mu.Unlock()
		return
	}

	h.resultIndex--
	if h.resultIndex < 0 {
		h.resultIndex = len(h.searchResults) - 1
	}
	result := h.searchResults[h.resultIndex]
	h.updateCounterDisplay()
	h.mu.Unlock()

	// Call vterm outside the lock to avoid deadlock
	if h.vterm != nil {
		// Update the current highlight line before scrolling
		h.vterm.UpdateSearchHighlightLine(result.GlobalLineIdx)
		h.animateScrollToLine(result.GlobalLineIdx)
	}
}

// updateCounterDisplay updates the "X/Y" counter label.
func (h *HistoryNavigator) updateCounterDisplay() {
	if len(h.searchResults) == 0 {
		h.counterLbl.Text = ""
	} else {
		h.counterLbl.Text = fmt.Sprintf("%d/%d", h.resultIndex+1, len(h.searchResults))
	}
}

// SelectClosestResultInViewport finds the search result closest to the viewport center
// and makes it the current result. This is used when jumping via scrollbar click.
// If no results are in the viewport, scrolls to the closest result nearby.
// Returns true if a result was selected.
func (h *HistoryNavigator) SelectClosestResultInViewport() bool {
	if h.vterm == nil {
		return false
	}

	h.mu.Lock()
	if len(h.searchResults) == 0 {
		h.mu.Unlock()
		return false
	}

	// Get viewport bounds using coordinate conversion
	// This properly handles line wrapping (physical vs logical lines)
	viewportHeight := h.vterm.Height()
	centerRow := viewportHeight / 2

	// Get logical line at top of viewport
	topLine, _, _, topOk := h.vterm.ViewportToContent(0, 0)
	// Get logical line at bottom of viewport
	bottomLine, _, _, bottomOk := h.vterm.ViewportToContent(viewportHeight-1, 0)
	// Get logical line at center of viewport
	centerLine, _, _, centerOk := h.vterm.ViewportToContent(centerRow, 0)

	if !topOk || !bottomOk {
		h.mu.Unlock()
		return false
	}
	if !centerOk {
		centerLine = (topLine + bottomLine) / 2
	}

	// Find the result closest to center that's in the viewport
	bestInViewportIdx := -1
	bestInViewportDistance := int64(1<<62 - 1)

	// Also track the closest result overall (even outside viewport)
	bestOverallIdx := -1
	bestOverallDistance := int64(1<<62 - 1)

	for i, result := range h.searchResults {
		lineIdx := result.GlobalLineIdx
		distance := lineIdx - centerLine
		if distance < 0 {
			distance = -distance
		}

		// Track closest overall
		if distance < bestOverallDistance {
			bestOverallDistance = distance
			bestOverallIdx = i
		}

		// Check if result is in viewport (inclusive range)
		if lineIdx >= topLine && lineIdx <= bottomLine {
			if distance < bestInViewportDistance {
				bestInViewportDistance = distance
				bestInViewportIdx = i
			}
		}
	}

	// Use in-viewport result if found, otherwise use closest overall
	bestIdx := bestInViewportIdx
	needsScroll := false
	if bestIdx < 0 && bestOverallIdx >= 0 {
		bestIdx = bestOverallIdx
		needsScroll = true
	}

	if bestIdx < 0 {
		h.mu.Unlock()
		return false
	}

	// Update the current result
	h.resultIndex = bestIdx
	result := h.searchResults[bestIdx]
	h.updateCounterDisplay()
	h.mu.Unlock()

	// Update highlighting and scroll if needed (outside lock)
	if h.vterm != nil {
		h.vterm.UpdateSearchHighlightLine(result.GlobalLineIdx)
		if needsScroll {
			h.vterm.ScrollToGlobalLine(result.GlobalLineIdx)
		}
	}

	h.requestRefresh()
	return true
}

// --- Scroll Animation ---

// lerp interpolates between two durations
func lerp(a, b time.Duration, t float32) time.Duration {
	return a + time.Duration(float32(b-a)*t)
}

// absInt64 returns absolute value of int64
func absInt64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// animateScrollToLine scrolls to the target line with animation.
// For very short distances (≤ half viewport): instant jump.
// For medium distances (≤ ScrollAnimMaxLines): smooth eased animation.
// For long distances (> ScrollAnimMaxLines): three-phase animation with visible edge lines.
func (h *HistoryNavigator) animateScrollToLine(targetLine int64) {
	if h.vterm == nil {
		return
	}

	// Get current scroll offset
	startOffset := h.vterm.ScrollOffset()

	// Jump to target to get the target scroll offset
	if !h.vterm.ScrollToGlobalLine(targetLine) {
		return // Out of range
	}
	targetOffset := h.vterm.ScrollOffset()

	// Calculate distance in scroll offset units
	distance := absInt64(targetOffset - startOffset)

	// For no change or animation disabled, keep the instant jump
	if h.ScrollAnimMaxLines <= 0 || distance == 0 {
		h.requestRefresh()
		return
	}

	// For very short jumps (within half the viewport), jump instantly.
	// This avoids sluggish animation when navigating between adjacent results.
	viewportHeight := int64(h.vterm.Height())
	if distance <= viewportHeight/2 {
		h.requestRefresh()
		return
	}

	// Restore original position to animate from there
	h.vterm.SetScrollOffset(startOffset)

	// Stop any existing animation
	h.animMu.Lock()
	if h.animating && h.animStopCh != nil {
		close(h.animStopCh)
	}
	h.animStopCh = make(chan struct{})
	h.animating = true
	stopCh := h.animStopCh
	h.animMu.Unlock()

	// Dispatch to appropriate animation handler
	if distance <= h.ScrollAnimMaxLines {
		// MEDIUM JUMP: Use smooth animation with easing
		h.animateShortJump(startOffset, targetOffset, distance, stopCh)
	} else {
		// LONG JUMP: Three-phase animation with visible edge lines
		h.animateLongJump(startOffset, targetOffset, distance, stopCh)
	}
}

// animateShortJump performs smooth eased animation for short distances.
func (h *HistoryNavigator) animateShortJump(startOffset, targetOffset, distance int64, stopCh chan struct{}) {
	// Calculate animation duration based on distance (scales linearly)
	// Short jumps are faster, longer jumps take more time
	durationRange := h.ScrollAnimMaxTime - h.ScrollAnimMinTime
	distanceRatio := float64(distance) / float64(h.ScrollAnimMaxLines)
	duration := h.ScrollAnimMinTime + time.Duration(float64(durationRange)*distanceRatio)

	// Use configured easing function
	easing := h.ScrollAnimEasing
	if easing == nil {
		easing = effects.EaseInOutCubic
	}

	// Animate in a goroutine
	go func() {
		startTime := time.Now()
		ticker := time.NewTicker(time.Second / time.Duration(h.ScrollAnimFrameRate))
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				elapsed := time.Since(startTime)
				if elapsed >= duration {
					// Animation complete - ensure we're at exact target
					h.vterm.SetScrollOffset(targetOffset)
					h.requestRefresh()

					h.animMu.Lock()
					h.animating = false
					h.animMu.Unlock()
					return
				}

				// Calculate progress and apply easing
				progress := float32(elapsed) / float32(duration)
				easedProgress := easing(progress)

				// Interpolate scroll offset
				currentOffset := startOffset + int64(float32(targetOffset-startOffset)*easedProgress)
				h.vterm.SetScrollOffset(currentOffset)
				h.requestRefresh()
			}
		}
	}()
}

// animateLongJump performs three-phase animation for long distances:
// Phase 1: Show first N edge lines one-by-one (accelerating)
// Phase 2: Fast smooth scroll through the middle
// Phase 3: Show last N edge lines one-by-one (decelerating)
func (h *HistoryNavigator) animateLongJump(startOffset, targetOffset, distance int64, stopCh chan struct{}) {
	direction := int64(1)
	if targetOffset < startOffset {
		direction = -1
	}

	edgeLines := int64(h.ScrollAnimEdgeLines)

	// If distance is too short for three phases, fall back to smooth animation
	if distance <= 2*edgeLines {
		h.animateShortJump(startOffset, targetOffset, distance, stopCh)
		return
	}

	go func() {
		// PHASE 1: Ease-in (show first N lines one-by-one, accelerating)
		for i := int64(1); i <= edgeLines; i++ {
			select {
			case <-stopCh:
				return
			default:
			}

			// Delay decreases with each line (ease-in effect)
			var progress float32
			if edgeLines > 1 {
				progress = float32(i-1) / float32(edgeLines-1)
			}
			delay := lerp(h.ScrollAnimEdgeStartDelay, h.ScrollAnimEdgeEndDelay, progress)
			time.Sleep(delay)

			h.vterm.SetScrollOffset(startOffset + i*direction)
			h.requestRefresh()
		}

		// PHASE 2: Fast middle (smooth animation through bulk)
		middleStart := startOffset + edgeLines*direction
		middleEnd := targetOffset - edgeLines*direction
		middleDistance := absInt64(middleEnd - middleStart)

		if middleDistance > 0 {
			// Use short duration for middle phase (fast scroll)
			middleDuration := h.ScrollAnimMinTime / 2

			// Use configured easing function
			easing := h.ScrollAnimEasing
			if easing == nil {
				easing = effects.EaseInOutCubic
			}

			startTime := time.Now()
			ticker := time.NewTicker(time.Second / time.Duration(h.ScrollAnimFrameRate))

			for {
				select {
				case <-stopCh:
					ticker.Stop()
					return
				case <-ticker.C:
					elapsed := time.Since(startTime)
					if elapsed >= middleDuration {
						// Phase 2 complete
						h.vterm.SetScrollOffset(middleEnd)
						h.requestRefresh()
						ticker.Stop()
						goto phase3
					}

					// Calculate progress and apply easing
					progress := float32(elapsed) / float32(middleDuration)
					easedProgress := easing(progress)

					// Interpolate scroll offset
					currentOffset := middleStart + int64(float32(middleEnd-middleStart)*easedProgress)
					h.vterm.SetScrollOffset(currentOffset)
					h.requestRefresh()
				}
			}
		}

	phase3:
		// PHASE 3: Ease-out (show last N lines one-by-one, decelerating)
		phaseStart := targetOffset - edgeLines*direction
		for i := int64(1); i <= edgeLines; i++ {
			select {
			case <-stopCh:
				return
			default:
			}

			// Delay increases with each line (ease-out effect)
			var progress float32
			if edgeLines > 1 {
				progress = float32(i-1) / float32(edgeLines-1)
			}
			delay := lerp(h.ScrollAnimEdgeEndDelay, h.ScrollAnimEdgeStartDelay, progress)
			time.Sleep(delay)

			h.vterm.SetScrollOffset(phaseStart + i*direction)
			h.requestRefresh()
		}

		// Ensure we land exactly on target
		h.vterm.SetScrollOffset(targetOffset)
		h.requestRefresh()

		h.animMu.Lock()
		h.animating = false
		h.animMu.Unlock()
	}()
}

// --- Lifecycle ---

// Run blocks until the navigator is stopped.
func (h *HistoryNavigator) Run() error {
	<-h.stopCh
	return nil
}

// Stop signals the navigator to stop.
func (h *HistoryNavigator) Stop() {
	close(h.stopCh)
}

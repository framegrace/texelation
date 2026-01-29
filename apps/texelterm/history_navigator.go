// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/history_navigator.go
// Summary: 2-line overlay card for searching and navigating terminal history.
// Usage: Opened with Ctrl+Shift+F, provides search and time-based navigation.

package texelterm

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	texelcore "github.com/framegrace/texelui/core"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/internal/theming"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
	"github.com/gdamore/tcell/v2"
)

// HistoryNavigator is a 2-line overlay card for searching and navigating terminal history.
// It provides:
//   - Full-text search with prev/next navigation
//   - Time-based navigation with relative (-1h, +30m) and absolute timestamps
//   - Keyboard navigation (Tab cycles inputs, Escape closes)
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

	// Widgets - Row 2: Time
	timeIcon     *widgets.Label
	timeInput    *widgets.Input
	minusHourBtn *widgets.Button
	plusHourBtn  *widgets.Button
	jumpBtn      *widgets.Button
	timestampLbl *widgets.Label

	// Search state
	searchResults []parser.SearchResult
	resultIndex   int

	// Visibility and dimensions
	visible bool
	width   int
	height  int

	// Refresh and lifecycle
	refreshCh chan<- bool
	stopCh    chan struct{}

	// Debouncing for search
	searchTimer *time.Timer
	timerMu     sync.Mutex

	mu sync.Mutex
}

// NewHistoryNavigator creates a new history navigator card.
func NewHistoryNavigator(vterm *parser.VTerm, searchIndex *parser.SQLiteSearchIndex, onClose func()) *HistoryNavigator {
	h := &HistoryNavigator{
		ui:          core.NewUIManager(),
		vterm:       vterm,
		searchIndex: searchIndex,
		onClose:     onClose,
		stopCh:      make(chan struct{}),
	}

	// Disable status bar for this compact card
	h.ui.SetStatusBar(nil)

	// Create widgets
	h.createWidgets()

	// Set up event handlers
	h.setupEventHandlers()

	return h
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

	// Row 1: Search widgets
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

	// Row 2: Time widgets
	h.timeIcon = widgets.NewLabel("⏰")
	h.timeIcon.Style = accentStyle
	h.timeIcon.SetFocusable(false)

	h.timeInput = widgets.NewInput()
	h.timeInput.Placeholder = "-1h, 30m, 14:30..."
	h.timeInput.Style = baseStyle
	h.timeInput.SetFocusable(true)

	h.minusHourBtn = widgets.NewButton("-1h")
	h.minusHourBtn.SetFocusable(true)

	h.plusHourBtn = widgets.NewButton("+1h")
	h.plusHourBtn.SetFocusable(true)

	h.jumpBtn = widgets.NewButton("Jump")
	h.jumpBtn.SetFocusable(true)

	h.timestampLbl = widgets.NewLabel("")
	h.timestampLbl.Style = mutedStyle
	h.timestampLbl.SetFocusable(false)

	// Add widgets to UI manager
	h.ui.AddWidget(h.searchIcon)
	h.ui.AddWidget(h.searchInput)
	h.ui.AddWidget(h.prevBtn)
	h.ui.AddWidget(h.nextBtn)
	h.ui.AddWidget(h.counterLbl)
	h.ui.AddWidget(h.timeIcon)
	h.ui.AddWidget(h.timeInput)
	h.ui.AddWidget(h.minusHourBtn)
	h.ui.AddWidget(h.plusHourBtn)
	h.ui.AddWidget(h.jumpBtn)
	h.ui.AddWidget(h.timestampLbl)
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

	// Time input - parse and jump on Enter
	h.timeInput.OnSubmit = func(text string) {
		h.jumpToTime(text)
	}

	// Time buttons
	h.minusHourBtn.OnClick = func() {
		h.adjustTime(-1 * time.Hour)
	}
	h.plusHourBtn.OnClick = func() {
		h.adjustTime(1 * time.Hour)
	}
	h.jumpBtn.OnClick = func() {
		h.jumpToTime(h.timeInput.Text)
	}
}

// Show displays the navigator and focuses the search input.
func (h *HistoryNavigator) Show() {
	log.Printf("[HISTORY_NAV] Show: getting lock...")
	h.mu.Lock()
	log.Printf("[HISTORY_NAV] Show: setting visible=true")
	h.visible = true
	log.Printf("[HISTORY_NAV] Show: focusing search input...")
	h.ui.Focus(h.searchInput)
	log.Printf("[HISTORY_NAV] Show: releasing lock...")
	h.mu.Unlock()

	// Don't update timestamp on show - it will be updated when user navigates
	// This avoids potential blocking on vterm/sqlite calls
	log.Printf("[HISTORY_NAV] Show: requesting refresh...")
	h.requestRefresh()
	log.Printf("[HISTORY_NAV] Show: done")
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

// Resize adjusts the navigator layout to fit the given dimensions.
// The navigator uses only 2 lines at the bottom.
func (h *HistoryNavigator) Resize(cols, rows int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.width = cols
	h.height = 2 // Navigator is always 2 lines

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

	// Row 1: [⏰] [time input.........] [-1h] [+1h] [Jump] [timestamp]
	x = 0

	// Time icon
	h.timeIcon.SetPosition(x, 1)
	h.timeIcon.Resize(2, 1)
	x += 3

	// Calculate widths from right side
	// Button width = len(text) + 4 for "[ text ]" display format
	timestampWidth := 20 // "2025-01-28 14:30:45"
	smallBtnWidth := 7   // "-1h" or "+1h" (3 chars + 4 padding)
	jumpBtnWidth := 8    // "Jump" (4 chars + 4 padding)
	rightWidgets = timestampWidth + smallBtnWidth*2 + jumpBtnWidth + 5

	// Time input gets remaining space
	inputWidth = max(h.width-x-rightWidgets, 10)
	h.timeInput.SetPosition(x, 1)
	h.timeInput.Resize(inputWidth, 1)
	x += inputWidth + 1

	// -1h button
	h.minusHourBtn.SetPosition(x, 1)
	h.minusHourBtn.Resize(smallBtnWidth, 1)
	x += smallBtnWidth + 1

	// +1h button
	h.plusHourBtn.SetPosition(x, 1)
	h.plusHourBtn.Resize(smallBtnWidth, 1)
	x += smallBtnWidth + 1

	// Jump button
	h.jumpBtn.SetPosition(x, 1)
	h.jumpBtn.Resize(jumpBtnWidth, 1)
	x += jumpBtnWidth + 1

	// Timestamp label
	h.timestampLbl.SetPosition(x, 1)
	h.timestampLbl.Resize(timestampWidth, 1)
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

	// Handle Escape or Ctrl+Q to close
	// Note: Escape is often intercepted by texelui runtime, so Ctrl+Q is the reliable option
	if ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyCtrlQ {
		h.Hide()
		return true
	}

	// Let UIManager handle Tab/Shift-Tab for natural focus cycling through all widgets

	// Handle Ctrl+N/Ctrl+P for result navigation (vim-style)
	if ev.Key() == tcell.KeyCtrlN {
		h.navigateToNextResult()
		return true
	}
	if ev.Key() == tcell.KeyCtrlP {
		h.navigateToPrevResult()
		return true
	}

	// Pass other keys to UIManager (don't hold lock - callbacks may need it)
	h.ui.HandleKey(ev)
	h.requestRefresh()
	return true
}

// Render draws the 2-line overlay at the bottom of the input buffer.
func (h *HistoryNavigator) Render(input [][]texelcore.Cell) [][]texelcore.Cell {
	log.Printf("[HISTORY_NAV] Render called, getting lock...")
	h.mu.Lock()
	visible := h.visible
	h.mu.Unlock()
	log.Printf("[HISTORY_NAV] Render: visible=%v", visible)

	if !visible || len(input) < 2 {
		return input
	}

	log.Printf("[HISTORY_NAV] Render: calling ui.Render()...")
	// Render UIManager to get the 2-line overlay (don't hold lock - ui has its own)
	overlay := h.ui.Render()
	log.Printf("[HISTORY_NAV] Render: ui.Render() returned %d rows", len(overlay))

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
		h.mu.Unlock()
		h.requestRefresh()
		return
	}

	// Search outside the lock (SQLite has its own locking)
	results, err := h.searchIndex.Search(query, 100)
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
	if len(results) > 0 {
		firstResult = &results[0]
	}
	h.mu.Unlock()

	// Auto-navigate to first result if any (outside lock)
	if firstResult != nil && h.vterm != nil {
		h.vterm.ScrollToGlobalLine(firstResult.GlobalLineIdx)
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
		log.Printf("[HISTORY_NAV] Scrolling to result %d/%d at line %d", h.resultIndex+1, len(h.searchResults), result.GlobalLineIdx)
		ok := h.vterm.ScrollToGlobalLine(result.GlobalLineIdx)
		log.Printf("[HISTORY_NAV] ScrollToGlobalLine returned %v", ok)
	}
	h.requestRefresh()
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
		h.vterm.ScrollToGlobalLine(result.GlobalLineIdx)
	}
	h.requestRefresh()
}

// updateCounterDisplay updates the "X/Y" counter label.
func (h *HistoryNavigator) updateCounterDisplay() {
	if len(h.searchResults) == 0 {
		h.counterLbl.Text = ""
	} else {
		h.counterLbl.Text = fmt.Sprintf("%d/%d", h.resultIndex+1, len(h.searchResults))
	}
}

// --- Time Navigation ---

// jumpToTime parses the time input and scrolls to that point in history.
func (h *HistoryNavigator) jumpToTime(timeStr string) {
	if h.searchIndex == nil || h.vterm == nil {
		return
	}

	targetTime, err := parseTimeInput(timeStr)
	if err != nil {
		log.Printf("[HISTORY_NAV] Time parse error: %v", err)
		return
	}

	lineIdx, err := h.searchIndex.FindLineAt(targetTime)
	if err != nil || lineIdx < 0 {
		log.Printf("[HISTORY_NAV] FindLineAt error: %v (lineIdx=%d)", err, lineIdx)
		return
	}

	h.mu.Lock()
	h.vterm.ScrollToGlobalLine(lineIdx)
	h.updateTimestampDisplayLocked()
	h.mu.Unlock()
	h.requestRefresh()
}

// adjustTime shifts the current view by the given duration and scrolls.
func (h *HistoryNavigator) adjustTime(delta time.Duration) {
	if h.searchIndex == nil || h.vterm == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Get current timestamp from scroll position
	currentTime := h.getCurrentTimestampLocked()
	if currentTime.IsZero() {
		currentTime = time.Now()
	}

	// Apply delta
	targetTime := currentTime.Add(delta)

	// Find and scroll to the target time
	lineIdx, err := h.searchIndex.FindLineAt(targetTime)
	if err != nil || lineIdx < 0 {
		return
	}

	h.vterm.ScrollToGlobalLine(lineIdx)
	h.updateTimestampDisplayLocked()
	h.requestRefresh()
}

// updateTimestampDisplay updates the timestamp label with the current view's time.
// updateTimestampDisplayLocked updates the timestamp label. Assumes h.mu is locked.
func (h *HistoryNavigator) updateTimestampDisplayLocked() {
	ts := h.getCurrentTimestampLocked()
	if ts.IsZero() {
		h.timestampLbl.Text = ""
	} else {
		h.timestampLbl.Text = ts.Format("2006-01-02 15:04:05")
	}
}

// getCurrentTimestampLocked returns the timestamp of the current view position.
// Assumes h.mu is locked.
func (h *HistoryNavigator) getCurrentTimestampLocked() time.Time {
	if h.searchIndex == nil || h.vterm == nil {
		return time.Time{}
	}

	// Get the global line at the top of the viewport
	// This is approximate - we use the scroll offset to estimate
	offset := h.vterm.ScrollOffset()
	globalEnd := h.vterm.GlobalEnd()

	// Simple estimation: higher scroll offset = older content
	// This is a rough approximation since scroll offset is in physical lines
	estimatedLine := max(globalEnd-offset, h.vterm.GlobalOffset())

	ts, err := h.searchIndex.GetTimestamp(estimatedLine)
	if err != nil {
		return time.Time{}
	}
	return ts
}

// --- Time Parsing ---

// parseTimeInput parses various time input formats:
//   - Relative: "5m", "1h", "2h30m", "-1h", "+30m"
//   - Absolute: "14:30", "14:30:45", "2025-01-28 14:30"
//   - Natural: "yesterday", "today 3pm" (limited support)
func parseTimeInput(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time input")
	}

	now := time.Now()

	// Try relative time first (most common use case)
	if dur, err := parseRelativeTime(s); err == nil {
		return now.Add(dur), nil
	}

	// Try absolute time formats
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"15:04:05",
		"15:04",
		"3:04pm",
		"3pm",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			// For time-only formats, use today's date
			if t.Year() == 0 {
				t = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, now.Location())
			}
			return t, nil
		}
	}

	// Try natural language
	if t, err := parseNaturalTime(s, now); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("unrecognized time format: %s", s)
}

// parseRelativeTime parses relative duration strings.
// Accepts: "5m", "1h", "2h30m", "-1h", "+30m"
// By default (no sign), values are interpreted as "ago" (negative).
func parseRelativeTime(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty relative time")
	}

	// Determine direction
	negative := true // Default: treat as "X ago"
	if strings.HasPrefix(s, "+") {
		negative = false
		s = s[1:]
	} else if strings.HasPrefix(s, "-") {
		negative = true
		s = s[1:]
	}

	// Parse duration using Go's time.ParseDuration
	// It supports: "300ms", "1.5h", "2h45m"
	dur, err := time.ParseDuration(s)
	if err != nil {
		// Try alternative format: "1h30m" without decimal
		dur, err = parseAlternativeDuration(s)
		if err != nil {
			return 0, err
		}
	}

	if negative {
		return -dur, nil
	}
	return dur, nil
}

// parseAlternativeDuration parses duration strings like "1h30m", "2d", "1w"
func parseAlternativeDuration(s string) (time.Duration, error) {
	// Pattern: optional number followed by unit, repeated
	re := regexp.MustCompile(`(\d+)([smhdw])`)
	matches := re.FindAllStringSubmatch(strings.ToLower(s), -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("invalid duration format")
	}

	var total time.Duration
	for _, match := range matches {
		n, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		switch match[2] {
		case "s":
			total += time.Duration(n) * time.Second
		case "m":
			total += time.Duration(n) * time.Minute
		case "h":
			total += time.Duration(n) * time.Hour
		case "d":
			total += time.Duration(n) * 24 * time.Hour
		case "w":
			total += time.Duration(n) * 7 * 24 * time.Hour
		}
	}

	if total == 0 {
		return 0, fmt.Errorf("no valid duration components")
	}
	return total, nil
}

// parseNaturalTime parses natural language time expressions.
func parseNaturalTime(s string, now time.Time) (time.Time, error) {
	s = strings.ToLower(strings.TrimSpace(s))

	// Simple natural time expressions
	switch {
	case s == "now":
		return now, nil
	case s == "today":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), nil
	case s == "yesterday":
		yesterday := now.AddDate(0, 0, -1)
		return time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, now.Location()), nil
	case strings.HasPrefix(s, "today "):
		// "today 3pm", "today 14:30"
		timeStr := strings.TrimPrefix(s, "today ")
		t, err := parseTimeOnly(timeStr)
		if err != nil {
			return time.Time{}, err
		}
		return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, now.Location()), nil
	case strings.HasPrefix(s, "yesterday "):
		// "yesterday 3pm"
		timeStr := strings.TrimPrefix(s, "yesterday ")
		t, err := parseTimeOnly(timeStr)
		if err != nil {
			return time.Time{}, err
		}
		yesterday := now.AddDate(0, 0, -1)
		return time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), t.Hour(), t.Minute(), t.Second(), 0, yesterday.Location()), nil
	}

	return time.Time{}, fmt.Errorf("unrecognized natural time: %s", s)
}

// parseTimeOnly parses time-only strings.
func parseTimeOnly(s string) (time.Time, error) {
	formats := []string{"15:04:05", "15:04", "3:04pm", "3pm"}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time format: %s", s)
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

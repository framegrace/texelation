// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/tui_viewport_manager.go
// Summary: Coordinates frozen line commits for TUI content preservation.
//
// TUIViewportManager implements the frozen lines + live viewport model:
// - Lines freeze (commit to history) when they scroll off row 0
// - Truncate+append pattern replaces previous frozen content on commits
// - Final commit when TUI mode exits
//
// This replaces the TUI snapshot system with a more robust approach
// where frozen content integrates directly into scrollback history.

package parser

import (
	"sync"
	"time"
)

// TUIViewportManager coordinates frozen line commits for TUI applications.
// It implements the frozen lines model where viewport content is committed
// to history as it scrolls off the top of the screen.
type TUIViewportManager struct {
	mu sync.Mutex

	// viewport provides access to the physical grid and row extraction
	viewport *ViewportState

	// history stores committed lines (normal + frozen)
	history *ScrollbackHistory

	// tracker tracks which history lines are frozen
	tracker *FreezeTracker

	// liveViewportStart is the history index where the current live viewport begins.
	// Content from history index [liveViewportStart, history.TotalLen()) is the
	// "live" portion that gets replaced on each debounced commit.
	liveViewportStart int64

	// tuiModeActive indicates whether TUI mode is currently active
	tuiModeActive bool

	// lastFreezeTime tracks when the last freeze occurred (for cooldown)
	lastFreezeTime time.Time

	// freezeCooldown prevents rapid duplicate freezes
	freezeCooldown time.Duration

	// debugLog is an optional logging function
	debugLog func(format string, args ...interface{})
}

// NewTUIViewportManager creates a new TUI viewport manager.
func NewTUIViewportManager(viewport *ViewportState, history *ScrollbackHistory) *TUIViewportManager {
	return &TUIViewportManager{
		viewport:          viewport,
		history:           history,
		tracker:           NewFreezeTracker(),
		liveViewportStart: 0,
		tuiModeActive:     false,
		freezeCooldown:    100 * time.Millisecond,
	}
}

// SetDebugLog sets the debug logging function.
func (t *TUIViewportManager) SetDebugLog(fn func(format string, args ...interface{})) {
	t.debugLog = fn
}

// EnterTUIMode prepares for TUI content preservation.
// Called when TUI signals are first detected.
func (t *TUIViewportManager) EnterTUIMode() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tuiModeActive {
		return // Already in TUI mode
	}

	t.tuiModeActive = true

	// Only set liveViewportStart if we're starting fresh (not re-entering after brief exit).
	// If liveViewportStart is already set (> 0 or has TUI content after it), keep it.
	// This prevents duplicates when TUI apps briefly reset scroll regions.
	if t.history != nil {
		currentLen := t.history.TotalLen()
		// Only update liveViewportStart if:
		// 1. It's at 0 (never set), or
		// 2. History has shrunk below liveViewportStart (content was cleared)
		if t.liveViewportStart == 0 || t.liveViewportStart > currentLen {
			t.liveViewportStart = currentLen
		}
		// Otherwise, keep the existing liveViewportStart so we continue
		// replacing the same TUI content block
	}

	if t.debugLog != nil {
		t.debugLog("[TUIViewportManager] EnterTUIMode: liveViewportStart=%d", t.liveViewportStart)
	}
}

// CommitBeforeScreenClear should be called BEFORE the screen is cleared (ED 0/2).
// This captures the current viewport state (like token usage) and replaces any
// previously committed transient content (like autocomplete menus).
// This must be called while the content is still on screen, before the erase happens.
func (t *TUIViewportManager) CommitBeforeScreenClear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.tuiModeActive {
		return // Not in TUI mode, nothing to commit
	}

	// Clear committed flags so we can re-commit all viewport content
	if t.viewport != nil {
		for y := 0; y < t.viewport.Height(); y++ {
			t.viewport.ClearRowCommitted(y)
		}
	}

	committed := t.commitLiveViewportLocked()

	if t.debugLog != nil {
		historyLen := int64(0)
		if t.history != nil {
			historyLen = t.history.TotalLen()
		}
		t.debugLog("[TUIViewportManager] CommitBeforeScreenClear: committed %d lines, total history %d",
			committed, historyLen)
	}
}

// ExitTUIMode finalizes and cleans up TUI mode state.
// Called when TUI mode ends (scroll region reset to full screen).
// This performs a FINAL COMMIT to capture the current viewport state (like token usage)
// and replace any transient content (like autocomplete menus).
// This is called when the scroll region is reset to full screen, which happens
// BEFORE the screen is cleared, so we can capture the final content.
// IMPORTANT: We do NOT reset liveViewportStart here. This allows re-entering
// TUI mode to continue replacing the same content block instead of creating duplicates.
func (t *TUIViewportManager) ExitTUIMode() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.tuiModeActive {
		return // Not in TUI mode
	}

	// Do a FINAL COMMIT before exiting TUI mode.
	// This captures the current viewport state (like token usage) and replaces
	// any previously committed transient content (like autocomplete menus).
	// Clear committed flags first so the commit captures current content.
	if t.viewport != nil {
		for y := 0; y < t.viewport.Height(); y++ {
			t.viewport.ClearRowCommitted(y)
		}
	}
	committed := t.commitLiveViewportLocked()

	historyLen := int64(0)
	if t.history != nil {
		historyLen = t.history.TotalLen()
	}

	t.tuiModeActive = false
	// NOTE: We intentionally do NOT reset liveViewportStart here.
	// Keeping it allows re-entering TUI mode to continue replacing the same
	// TUI content block, preventing duplicates when apps briefly reset scroll regions.

	if t.debugLog != nil {
		t.debugLog("[TUIViewportManager] ExitTUIMode: final commit of %d lines, total history %d, liveViewportStart=%d",
			committed, historyLen, t.liveViewportStart)
	}
}

// IsActive returns whether TUI mode is currently active.
func (t *TUIViewportManager) IsActive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tuiModeActive
}

// FreezeScrolledLines commits rows that scrolled off row 0 to history.
// Called from ScrollContentUp when rows scroll off the top.
// The lines are marked as frozen in the tracker.
func (t *TUIViewportManager) FreezeScrolledLines(lines []*LogicalLine) {
	if len(lines) == 0 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.tuiModeActive {
		if t.debugLog != nil {
			t.debugLog("[TUIViewportManager] FreezeScrolledLines: skipped %d lines - TUI mode not active", len(lines))
		}
		return
	}

	if t.history == nil {
		if t.debugLog != nil {
			t.debugLog("[TUIViewportManager] FreezeScrolledLines: skipped %d lines - history is nil", len(lines))
		}
		return
	}

	// Apply cooldown to prevent rapid duplicate freezes
	if time.Since(t.lastFreezeTime) < t.freezeCooldown {
		if t.debugLog != nil {
			remaining := t.freezeCooldown - time.Since(t.lastFreezeTime)
			t.debugLog("[TUIViewportManager] FreezeScrolledLines: skipped %d lines - cooldown active (%.1fms remaining)",
				len(lines), float64(remaining)/float64(time.Millisecond))
		}
		return
	}

	startIdx := t.history.TotalLen()

	// Commit each line as fixed-width
	for _, line := range lines {
		// Ensure line has fixed width set
		if line.FixedWidth == 0 && t.viewport != nil {
			line.FixedWidth = t.viewport.Width()
		}
		line.TrimTrailingSpaces()
		t.history.Append(line)
	}

	endIdx := t.history.TotalLen() - 1

	// Track these as frozen
	t.tracker.MarkFrozen(startIdx, endIdx)
	t.lastFreezeTime = time.Now()

	if t.debugLog != nil {
		t.debugLog("[TUIViewportManager] FreezeScrolledLines: froze %d lines [%d, %d]",
			len(lines), startIdx, endIdx)
	}
}

// CommitLiveViewport freezes all current viewport rows.
// Called on debounced commit or explicit API call.
// Returns the number of lines committed.
func (t *TUIViewportManager) CommitLiveViewport() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.commitLiveViewportLocked()
}

// commitLiveViewportLocked performs the commit. Caller must hold the lock.
func (t *TUIViewportManager) commitLiveViewportLocked() int {
	if t.viewport == nil || t.history == nil {
		return 0
	}

	// Only commit if TUI mode is active (protects against truncating when not in TUI mode)
	if !t.tuiModeActive {
		if t.debugLog != nil {
			t.debugLog("[TUIViewportManager] commitLiveViewport: skipped - TUI mode not active")
		}
		return 0
	}

	// Step 1: Truncate history back to liveViewportStart
	// This removes the previous "live" content to be replaced
	t.truncateToLocked(t.liveViewportStart)

	// Step 2: Extract non-committed rows from viewport
	height := t.viewport.Height()
	width := t.viewport.Width()

	// Find first non-committed row
	firstNonCommitted := -1
	for y := 0; y < height; y++ {
		if !t.viewport.IsRowCommitted(y) {
			firstNonCommitted = y
			break
		}
	}

	if firstNonCommitted < 0 {
		// All rows are from history, nothing to commit
		if t.debugLog != nil {
			t.debugLog("[TUIViewportManager] commitLiveViewport: no non-committed rows")
		}
		return 0
	}

	// Find last non-empty, non-committed row
	lastNonEmpty := -1
	grid := t.viewport.Grid()
	for y := height - 1; y >= firstNonCommitted; y-- {
		if t.viewport.IsRowCommitted(y) {
			continue
		}
		// Check for non-space content
		for _, cell := range grid[y] {
			if cell.Rune != ' ' && cell.Rune != 0 {
				lastNonEmpty = y
				break
			}
		}
		if lastNonEmpty >= 0 {
			break
		}
	}

	if lastNonEmpty < 0 {
		// No non-empty content to commit
		if t.debugLog != nil {
			t.debugLog("[TUIViewportManager] commitLiveViewport: no non-empty rows")
		}
		return 0
	}

	// Step 3: Commit rows as fixed-width lines
	startIdx := t.history.TotalLen()
	committed := 0

	for y := firstNonCommitted; y <= lastNonEmpty; y++ {
		if t.viewport.IsRowCommitted(y) {
			continue
		}

		// Create fixed-width logical line from row
		cells := make([]Cell, width)
		copy(cells, grid[y])

		line := &LogicalLine{
			Cells:      cells,
			FixedWidth: width,
		}
		line.TrimTrailingSpaces()

		t.history.Append(line)
		t.viewport.MarkRowAsCommitted(y)
		committed++
	}

	if committed > 0 {
		endIdx := t.history.TotalLen() - 1
		t.tracker.MarkFrozen(startIdx, endIdx)
	}

	// NOTE: liveViewportStart is NOT updated here.
	// It stays at the value set in EnterTUIMode() so that subsequent
	// commits truncate back to that point and REPLACE the live content
	// (truncate+append pattern), rather than appending duplicates.

	if t.debugLog != nil {
		t.debugLog("[TUIViewportManager] commitLiveViewport: committed %d lines, liveViewportStart=%d (unchanged)",
			committed, t.liveViewportStart)
	}

	return committed
}

// TruncateTo removes frozen lines from memory beyond the limit.
// Only affects memory - disk history is preserved for recovery.
func (t *TUIViewportManager) TruncateTo(maxLines int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.history == nil || maxLines <= 0 {
		return
	}

	currentLen := int(t.history.TotalLen())
	if currentLen <= maxLines {
		return // Within limits
	}

	excessLines := int64(currentLen - maxLines)
	t.truncateToLocked(t.history.TotalLen() - excessLines)
}

// truncateToLocked truncates history to the given length. Caller must hold lock.
func (t *TUIViewportManager) truncateToLocked(newLen int64) {
	if t.history == nil {
		return
	}

	currentLen := t.history.TotalLen()
	if newLen >= currentLen {
		return // Nothing to truncate
	}

	// Use the history's TruncateTo method which handles memory window correctly
	t.history.TruncateTo(newLen)

	// Update tracker to remove frozen ranges beyond newLen
	t.tracker.TruncateTo(newLen)

	if t.debugLog != nil {
		t.debugLog("[TUIViewportManager] truncateTo: newLen=%d, removed %d lines from memory",
			newLen, currentLen-newLen)
	}
}

// IsFrozen returns whether the given history line was frozen.
func (t *TUIViewportManager) IsFrozen(globalLineIndex int64) bool {
	return t.tracker.IsFrozen(globalLineIndex)
}

// FrozenLineCount returns the total number of frozen lines.
func (t *TUIViewportManager) FrozenLineCount() int64 {
	return t.tracker.FrozenCount()
}

// LiveViewportStart returns the history index where live viewport begins.
func (t *TUIViewportManager) LiveViewportStart() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.liveViewportStart
}

// Reset clears all TUI state. Used for testing.
func (t *TUIViewportManager) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.tuiModeActive = false
	t.liveViewportStart = 0
	t.tracker.Clear()
	t.lastFreezeTime = time.Time{}
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/vterm_tui_mode.go
// Summary: TUI mode detection and fixed-width viewport commit management.
//
// TUI apps (like codex) use scroll regions and absolute cursor positioning
// on the main screen. This content should be preserved as fixed-width blocks
// that don't reflow on resize.
//
// Detection signals:
// - Scroll region changes (DECSTBM with non-full-screen margins)
// - Large cursor jumps (CUP moving more than 1 row)
// - Cursor visibility changes (DECTCEM)
//
// When signals are detected, a debounced commit is scheduled. After the
// debounce period (100ms), the viewport is committed as fixed-width lines.

package parser

import (
	"sync"
	"time"
)

// TUIMode tracks TUI application state for fixed-width content preservation.
type TUIMode struct {
	mu sync.Mutex

	// active indicates whether TUI mode is currently detected
	active bool

	// signalCount tracks how many TUI signals have been received
	signalCount int

	// lastSignal is when the last TUI signal was received
	lastSignal time.Time

	// idleTimeout is how long without signals before TUI mode expires
	idleTimeout time.Duration

	// commitTimer is the debounce timer for viewport commits
	commitTimer *time.Timer

	// commitDebounce is the debounce period before committing
	commitDebounce time.Duration

	// commitCallback is called when debounce expires to commit viewport
	commitCallback func()

	// minSignalsToCommit is the minimum signals before a commit is allowed
	minSignalsToCommit int

	// lastCommitTime tracks when the last commit happened.
	// Used to prevent rapid duplicate commits - a new commit is only
	// allowed after commitCooldown has passed since the last one.
	lastCommitTime time.Time

	// commitCooldown is how long to wait after a commit before allowing another.
	// This prevents duplicates from TUI apps that briefly reset scroll regions.
	commitCooldown time.Duration
}

// TUIModeConfig holds configuration for TUI mode detection.
type TUIModeConfig struct {
	// IdleTimeout is how long without signals before TUI mode expires.
	// Default: 5 seconds
	IdleTimeout time.Duration

	// CommitDebounce is the debounce period before committing viewport.
	// Default: 100ms
	CommitDebounce time.Duration

	// MinSignalsToCommit is minimum signals needed before commit is allowed.
	// Default: 3 (prevents false positives from occasional cursor moves)
	MinSignalsToCommit int

	// CommitCooldown is how long to wait after a commit before allowing another.
	// This prevents duplicates from TUI apps that briefly reset scroll regions.
	// Default: 2 seconds
	CommitCooldown time.Duration
}

// DefaultTUIModeConfig returns sensible defaults for TUI mode detection.
func DefaultTUIModeConfig() TUIModeConfig {
	return TUIModeConfig{
		IdleTimeout:        5 * time.Second,
		CommitDebounce:     100 * time.Millisecond,
		MinSignalsToCommit: 3,
		CommitCooldown:     2 * time.Second,
	}
}

// NewTUIMode creates a new TUI mode tracker with the given configuration.
func NewTUIMode(config TUIModeConfig) *TUIMode {
	if config.IdleTimeout == 0 {
		config.IdleTimeout = 5 * time.Second
	}
	if config.CommitDebounce == 0 {
		config.CommitDebounce = 100 * time.Millisecond
	}
	if config.MinSignalsToCommit == 0 {
		config.MinSignalsToCommit = 3
	}
	if config.CommitCooldown == 0 {
		config.CommitCooldown = 2 * time.Second
	}

	return &TUIMode{
		idleTimeout:        config.IdleTimeout,
		commitDebounce:     config.CommitDebounce,
		minSignalsToCommit: config.MinSignalsToCommit,
		commitCooldown:     config.CommitCooldown,
	}
}

// SetCommitCallback sets the function to call when viewport should be committed.
func (t *TUIMode) SetCommitCallback(callback func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.commitCallback = callback
}

// Signal records a TUI signal and activates TUI mode.
// signalType is for debugging/logging (e.g., "scroll_region", "cursor_jump").
func (t *TUIMode) Signal(signalType string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.signalCount++
	t.lastSignal = time.Now()
	t.active = true

	// Schedule a debounced commit
	t.scheduleCommitLocked()
}

// Reset clears TUI mode state. Called when TUI app exits
// (e.g., scroll region reset to full screen).
// Note: lastCommitTime is NOT cleared here - the cooldown period
// prevents duplicate commits when TUI apps briefly reset scroll regions.
func (t *TUIMode) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.active = false
	t.signalCount = 0
	// Note: lastCommitTime intentionally NOT cleared here.
	// The cooldown period prevents duplicates from brief scroll region changes.

	if t.commitTimer != nil {
		t.commitTimer.Stop()
		t.commitTimer = nil
	}
}

// IsActive returns whether TUI mode is currently active.
// Returns false if mode has expired due to idle timeout.
func (t *TUIMode) IsActive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.active {
		return false
	}

	// Check idle timeout
	if time.Since(t.lastSignal) > t.idleTimeout {
		t.active = false
		t.signalCount = 0
		return false
	}

	return true
}

// SignalCount returns the number of TUI signals received since activation.
func (t *TUIMode) SignalCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.signalCount
}

// scheduleCommitLocked starts or restarts the debounce timer.
// Must be called with t.mu held.
func (t *TUIMode) scheduleCommitLocked() {
	// Cancel existing timer
	if t.commitTimer != nil {
		t.commitTimer.Stop()
	}

	// Start new timer
	t.commitTimer = time.AfterFunc(t.commitDebounce, func() {
		t.executeCommit()
	})
}

// executeCommit is called when the debounce timer expires.
func (t *TUIMode) executeCommit() {
	t.mu.Lock()
	callback := t.commitCallback
	signalCount := t.signalCount
	minSignals := t.minSignalsToCommit
	active := t.active
	lastCommit := t.lastCommitTime
	cooldown := t.commitCooldown

	// Reset signal count after commit attempt
	t.signalCount = 0
	t.mu.Unlock()

	// Only commit if we have enough signals and mode is active
	if !active || signalCount < minSignals {
		return
	}

	// Check cooldown - don't commit if we recently committed.
	// This prevents duplicates from TUI apps that briefly reset scroll regions.
	if !lastCommit.IsZero() && time.Since(lastCommit) < cooldown {
		return
	}

	if callback != nil {
		callback()
		// Record commit time to prevent rapid duplicates
		t.mu.Lock()
		t.lastCommitTime = time.Now()
		t.mu.Unlock()
	}
}

// Stop cleans up resources. Should be called when VTerm is destroyed.
func (t *TUIMode) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.commitTimer != nil {
		t.commitTimer.Stop()
		t.commitTimer = nil
	}
}

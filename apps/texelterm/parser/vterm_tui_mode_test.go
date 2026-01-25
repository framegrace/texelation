// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestTUIMode_InitialState(t *testing.T) {
	tm := NewTUIMode(DefaultTUIModeConfig())

	if tm.IsActive() {
		t.Error("TUIMode should not be active initially")
	}
	if tm.SignalCount() != 0 {
		t.Errorf("SignalCount should be 0 initially, got %d", tm.SignalCount())
	}
}

func TestTUIMode_SignalActivatesMode(t *testing.T) {
	tm := NewTUIMode(DefaultTUIModeConfig())

	tm.Signal("scroll_region")

	if !tm.IsActive() {
		t.Error("TUIMode should be active after signal")
	}
	if tm.SignalCount() != 1 {
		t.Errorf("SignalCount should be 1, got %d", tm.SignalCount())
	}
}

func TestTUIMode_MultipleSignals(t *testing.T) {
	tm := NewTUIMode(DefaultTUIModeConfig())

	tm.Signal("scroll_region")
	tm.Signal("cursor_jump")
	tm.Signal("cursor_visibility")

	if tm.SignalCount() != 3 {
		t.Errorf("SignalCount should be 3, got %d", tm.SignalCount())
	}
}

func TestTUIMode_Reset(t *testing.T) {
	tm := NewTUIMode(DefaultTUIModeConfig())

	tm.Signal("scroll_region")
	tm.Signal("cursor_jump")
	tm.Reset()

	if tm.IsActive() {
		t.Error("TUIMode should not be active after reset")
	}
	if tm.SignalCount() != 0 {
		t.Errorf("SignalCount should be 0 after reset, got %d", tm.SignalCount())
	}
}

func TestTUIMode_IdleTimeout(t *testing.T) {
	config := TUIModeConfig{
		IdleTimeout:        50 * time.Millisecond, // Short timeout for test
		CommitDebounce:     10 * time.Millisecond,
		MinSignalsToCommit: 1,
	}
	tm := NewTUIMode(config)

	tm.Signal("scroll_region")

	if !tm.IsActive() {
		t.Error("TUIMode should be active immediately after signal")
	}

	// Wait for idle timeout
	time.Sleep(60 * time.Millisecond)

	if tm.IsActive() {
		t.Error("TUIMode should not be active after idle timeout")
	}
}

func TestTUIMode_DebouncedCommit(t *testing.T) {
	config := TUIModeConfig{
		IdleTimeout:        1 * time.Second,
		CommitDebounce:     50 * time.Millisecond,
		MinSignalsToCommit: 1,
	}
	tm := NewTUIMode(config)

	var commitCount int32
	tm.SetCommitCallback(func() {
		atomic.AddInt32(&commitCount, 1)
	})

	// Send signal - should trigger debounced commit
	tm.Signal("scroll_region")

	// Commit should not happen immediately
	if atomic.LoadInt32(&commitCount) != 0 {
		t.Error("Commit should not happen immediately")
	}

	// Wait for debounce
	time.Sleep(70 * time.Millisecond)

	if atomic.LoadInt32(&commitCount) != 1 {
		t.Errorf("Expected 1 commit after debounce, got %d", atomic.LoadInt32(&commitCount))
	}
}

func TestTUIMode_DebounceResetsOnNewSignal(t *testing.T) {
	config := TUIModeConfig{
		IdleTimeout:        1 * time.Second,
		CommitDebounce:     50 * time.Millisecond,
		MinSignalsToCommit: 1,
	}
	tm := NewTUIMode(config)

	var commitCount int32
	tm.SetCommitCallback(func() {
		atomic.AddInt32(&commitCount, 1)
	})

	// Send first signal
	tm.Signal("scroll_region")

	// Wait 30ms (not enough for commit)
	time.Sleep(30 * time.Millisecond)

	// Send another signal - should reset debounce
	tm.Signal("cursor_jump")

	// Wait another 30ms (60ms total, but debounce was reset)
	time.Sleep(30 * time.Millisecond)

	// Should still not have committed (debounce reset)
	if atomic.LoadInt32(&commitCount) != 0 {
		t.Error("Commit should not happen - debounce was reset")
	}

	// Wait for full debounce after last signal
	time.Sleep(30 * time.Millisecond)

	if atomic.LoadInt32(&commitCount) != 1 {
		t.Errorf("Expected 1 commit after debounce, got %d", atomic.LoadInt32(&commitCount))
	}
}

func TestTUIMode_MinSignalsRequired(t *testing.T) {
	config := TUIModeConfig{
		IdleTimeout:        1 * time.Second,
		CommitDebounce:     20 * time.Millisecond,
		MinSignalsToCommit: 3,
	}
	tm := NewTUIMode(config)

	var commitCount int32
	tm.SetCommitCallback(func() {
		atomic.AddInt32(&commitCount, 1)
	})

	// Send only 2 signals (below threshold of 3)
	tm.Signal("scroll_region")
	tm.Signal("cursor_jump")

	// Wait for debounce
	time.Sleep(40 * time.Millisecond)

	// Should NOT commit - not enough signals
	if atomic.LoadInt32(&commitCount) != 0 {
		t.Error("Commit should not happen with fewer than MinSignalsToCommit signals")
	}

	// Send 3 more signals (now at 3 total since count was reset)
	tm.Signal("a")
	tm.Signal("b")
	tm.Signal("c")

	// Wait for debounce
	time.Sleep(40 * time.Millisecond)

	// Should commit now
	if atomic.LoadInt32(&commitCount) != 1 {
		t.Errorf("Expected 1 commit after reaching MinSignalsToCommit, got %d", atomic.LoadInt32(&commitCount))
	}
}

func TestTUIMode_StopCancelsTimer(t *testing.T) {
	config := TUIModeConfig{
		IdleTimeout:        1 * time.Second,
		CommitDebounce:     50 * time.Millisecond,
		MinSignalsToCommit: 1,
	}
	tm := NewTUIMode(config)

	var commitCount int32
	tm.SetCommitCallback(func() {
		atomic.AddInt32(&commitCount, 1)
	})

	tm.Signal("scroll_region")
	tm.Stop()

	// Wait for what would have been the debounce period
	time.Sleep(70 * time.Millisecond)

	// Commit should NOT happen - timer was stopped
	if atomic.LoadInt32(&commitCount) != 0 {
		t.Error("Commit should not happen after Stop()")
	}
}

func TestTUIMode_ResetCancelsTimer(t *testing.T) {
	config := TUIModeConfig{
		IdleTimeout:        1 * time.Second,
		CommitDebounce:     50 * time.Millisecond,
		MinSignalsToCommit: 1,
	}
	tm := NewTUIMode(config)

	var commitCount int32
	tm.SetCommitCallback(func() {
		atomic.AddInt32(&commitCount, 1)
	})

	tm.Signal("scroll_region")
	tm.Reset()

	// Wait for what would have been the debounce period
	time.Sleep(70 * time.Millisecond)

	// Commit should NOT happen - timer was cancelled by Reset
	if atomic.LoadInt32(&commitCount) != 0 {
		t.Error("Commit should not happen after Reset()")
	}
}

func TestTUIMode_DefaultConfig(t *testing.T) {
	config := DefaultTUIModeConfig()

	if config.IdleTimeout != 5*time.Second {
		t.Errorf("Default IdleTimeout should be 5s, got %v", config.IdleTimeout)
	}
	if config.CommitDebounce != 100*time.Millisecond {
		t.Errorf("Default CommitDebounce should be 100ms, got %v", config.CommitDebounce)
	}
	if config.MinSignalsToCommit != 3 {
		t.Errorf("Default MinSignalsToCommit should be 3, got %d", config.MinSignalsToCommit)
	}
}

func TestTUIMode_OnlyCommitsOncePerSession(t *testing.T) {
	// Verify that TUI mode only commits once within the cooldown period,
	// even with continuous signals. This prevents duplicate history entries.
	config := TUIModeConfig{
		IdleTimeout:        1 * time.Second,
		CommitDebounce:     30 * time.Millisecond,
		MinSignalsToCommit: 2,
		CommitCooldown:     500 * time.Millisecond, // Long enough cooldown for testing
	}
	tm := NewTUIMode(config)

	var commitCount int32
	tm.SetCommitCallback(func() {
		atomic.AddInt32(&commitCount, 1)
	})

	// Send initial signals to trigger first commit
	tm.Signal("scroll_region")
	tm.Signal("cursor_jump")

	// Wait for debounce
	time.Sleep(50 * time.Millisecond)

	// Should have 1 commit
	if atomic.LoadInt32(&commitCount) != 1 {
		t.Errorf("Expected 1 commit after first debounce, got %d", atomic.LoadInt32(&commitCount))
	}

	// Send MORE signals (simulating ongoing TUI activity)
	tm.Signal("cursor_jump")
	tm.Signal("cursor_jump")
	tm.Signal("cursor_visibility")

	// Wait for what would be another debounce period
	time.Sleep(50 * time.Millisecond)

	// Should STILL be 1 commit - cooldown prevents more
	if atomic.LoadInt32(&commitCount) != 1 {
		t.Errorf("Expected still 1 commit (no duplicates), got %d", atomic.LoadInt32(&commitCount))
	}

	// Reset TUI mode (simulating TUI app exit)
	tm.Reset()

	// Immediately after reset, signals still won't cause commit due to cooldown
	tm.Signal("scroll_region")
	tm.Signal("cursor_jump")
	time.Sleep(50 * time.Millisecond)

	// Should STILL be 1 - within cooldown period (we're at ~150ms, cooldown is 500ms)
	if atomic.LoadInt32(&commitCount) != 1 {
		t.Errorf("Expected still 1 commit (within cooldown), got %d", atomic.LoadInt32(&commitCount))
	}

	// Wait for cooldown to expire (need to wait ~400ms more)
	time.Sleep(400 * time.Millisecond)

	// Now new signals should allow another commit
	tm.Signal("scroll_region")
	tm.Signal("cursor_jump")

	time.Sleep(50 * time.Millisecond)

	// Now should have 2 commits (cooldown expired)
	if atomic.LoadInt32(&commitCount) != 2 {
		t.Errorf("Expected 2 commits after cooldown expired, got %d", atomic.LoadInt32(&commitCount))
	}
}

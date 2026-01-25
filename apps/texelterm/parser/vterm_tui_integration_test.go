// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestVTerm_TUISignals_ScrollRegion(t *testing.T) {
	v := NewVTerm(80, 24)
	defer v.StopTUIMode()

	// Initial state: TUI mode should not be active
	if v.tuiMode.IsActive() {
		t.Error("TUI mode should not be active initially")
	}

	// Set a non-full-screen scroll region (typical of TUI apps)
	v.SetMargins(2, 20) // Not full screen (1,24)

	if !v.tuiMode.IsActive() {
		t.Error("TUI mode should be active after setting non-full-screen scroll region")
	}
	if v.tuiMode.SignalCount() != 1 {
		t.Errorf("Expected 1 signal, got %d", v.tuiMode.SignalCount())
	}
}

func TestVTerm_TUISignals_CursorJump(t *testing.T) {
	v := NewVTerm(80, 24)
	defer v.StopTUIMode()

	// Move cursor by more than 1 row
	v.SetCursorPos(10, 5)

	if !v.tuiMode.IsActive() {
		t.Error("TUI mode should be active after large cursor jump")
	}
	if v.tuiMode.SignalCount() != 1 {
		t.Errorf("Expected 1 signal, got %d", v.tuiMode.SignalCount())
	}
}

func TestVTerm_TUISignals_CursorVisibility(t *testing.T) {
	v := NewVTerm(80, 24)
	defer v.StopTUIMode()

	// Toggle cursor visibility
	v.SetCursorVisible(false)

	if !v.tuiMode.IsActive() {
		t.Error("TUI mode should be active after cursor visibility change")
	}
	if v.tuiMode.SignalCount() != 1 {
		t.Errorf("Expected 1 signal, got %d", v.tuiMode.SignalCount())
	}
}

func TestVTerm_TUISignals_SmallMoveNotSignal(t *testing.T) {
	v := NewVTerm(80, 24)
	defer v.StopTUIMode()

	// Small cursor move (1 row or less) should NOT signal
	v.SetCursorPos(1, 5)

	if v.tuiMode.IsActive() {
		t.Error("TUI mode should NOT be active after small cursor move")
	}
}

func TestVTerm_TUISignals_ResetOnFullScreenMargins(t *testing.T) {
	v := NewVTerm(80, 24)
	defer v.StopTUIMode()

	// Activate TUI mode
	v.SetMargins(2, 20)
	if !v.tuiMode.IsActive() {
		t.Fatal("TUI mode should be active")
	}

	// Reset to full screen margins
	v.SetMargins(1, 24)

	// TUI mode should be reset
	if v.tuiMode.IsActive() {
		t.Error("TUI mode should be reset when margins return to full screen")
	}
}

func TestVTerm_TUISignals_NotInAltScreen(t *testing.T) {
	v := NewVTerm(80, 24)
	defer v.StopTUIMode()

	// Enter alt screen
	v.processPrivateCSI('h', []int{1049})

	// Try to trigger TUI signals
	v.SetMargins(2, 20)
	v.SetCursorPos(10, 5)
	v.SetCursorVisible(false)

	// None should activate TUI mode since we're in alt screen
	if v.tuiMode.IsActive() {
		t.Error("TUI mode should NOT be active in alt screen")
	}
}

func TestVTerm_TUIModeCommit_DebouncedCallback(t *testing.T) {
	v := NewVTerm(80, 24)
	defer v.StopTUIMode()

	// Configure TUIMode with short debounce for testing
	v.tuiMode.Stop() // Stop existing
	v.tuiMode = NewTUIMode(TUIModeConfig{
		IdleTimeout:        1 * time.Second,
		CommitDebounce:     50 * time.Millisecond,
		MinSignalsToCommit: 2,
	})

	var commitCount int32
	v.tuiMode.SetCommitCallback(func() {
		atomic.AddInt32(&commitCount, 1)
	})

	// Send signals (need at least MinSignalsToCommit)
	v.SetMargins(2, 20)
	v.SetCursorPos(10, 5)

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

func TestVTerm_CommitViewportAsFixedWidth_Integration(t *testing.T) {
	v := NewVTerm(10, 3)
	defer v.StopTUIMode()

	// Write some content
	for _, r := range "Row0" {
		v.writeCharWithWrapping(r)
	}
	v.SetCursorPos(1, 0)
	for _, r := range "Row1" {
		v.writeCharWithWrapping(r)
	}
	v.SetCursorPos(2, 0)
	for _, r := range "Row2" {
		v.writeCharWithWrapping(r)
	}

	// Commit viewport as fixed-width
	v.commitViewportAsFixedWidth()

	// Check history
	history := v.displayBuf.history
	if history == nil {
		t.Fatal("history should not be nil")
	}

	// The viewport commit adds 3 lines
	if history.TotalLen() != 3 {
		t.Errorf("expected 3 history lines, got %d", history.TotalLen())
	}

	// Check that lines have FixedWidth set
	for i := int64(0); i < 3; i++ {
		line := history.GetGlobal(i)
		if line == nil {
			t.Fatalf("line %d is nil", i)
		}
		if line.FixedWidth != 10 {
			t.Errorf("line %d: expected FixedWidth=10, got %d", i, line.FixedWidth)
		}
	}

	// Verify fixed-width lines don't reflow
	line := history.GetGlobal(0)
	wrapped := line.WrapToWidth(5) // Should NOT wrap, should clip
	if len(wrapped) != 1 {
		t.Errorf("fixed-width line should produce 1 physical line, got %d", len(wrapped))
	}
	if len(wrapped[0].Cells) != 5 {
		t.Errorf("expected 5 cells (clipped), got %d", len(wrapped[0].Cells))
	}
}

func TestVTerm_TUIMode_FullFlow(t *testing.T) {
	// Simulate a TUI app that:
	// 1. Sets a scroll region
	// 2. Jumps cursor around
	// 3. Content gets committed as fixed-width

	v := NewVTerm(20, 10)
	defer v.StopTUIMode()

	// Reconfigure with short debounce for test
	v.tuiMode.Stop()
	v.tuiMode = NewTUIMode(TUIModeConfig{
		IdleTimeout:        1 * time.Second,
		CommitDebounce:     30 * time.Millisecond,
		MinSignalsToCommit: 2,
	})

	var committed int32
	v.tuiMode.SetCommitCallback(func() {
		c := v.displayBuf.display.viewport.CommitViewportAsFixedWidth()
		atomic.StoreInt32(&committed, int32(c))
	})

	// Write content to viewport
	v.SetCursorPos(0, 0)
	for _, r := range "StatusBar" {
		v.writeCharWithWrapping(r)
	}
	v.SetCursorPos(5, 0)
	for _, r := range "Content" {
		v.writeCharWithWrapping(r)
	}

	// Simulate TUI signals
	v.SetMargins(2, 8)    // Non-full-screen scroll region
	v.SetCursorPos(5, 10) // Large cursor jump (but this is row 5, we're at row 5 so no jump)
	v.SetCursorPos(8, 0)  // Large cursor jump (3 rows)
	v.SetCursorVisible(false)

	// Wait for commit
	time.Sleep(60 * time.Millisecond)

	// Content should be committed
	if atomic.LoadInt32(&committed) == 0 {
		t.Error("Expected viewport to be committed as fixed-width")
	}

	// Verify history has fixed-width lines
	history := v.displayBuf.history
	if history.TotalLen() > 0 {
		line := history.GetGlobal(0)
		if line.FixedWidth != 20 {
			t.Errorf("expected FixedWidth=20, got %d", line.FixedWidth)
		}
	}
}

func TestVTerm_TUISignals_NotWhileViewingHistory(t *testing.T) {
	// TUI signals and commits should NOT happen when the user is viewing history
	// (scrolled back). This prevents duplicating content when scrolling around.
	v := NewVTerm(80, 24)
	defer v.StopTUIMode()

	// Write some content to create history
	for i := 0; i < 30; i++ {
		for _, r := range "Line content here" {
			v.writeCharWithWrapping(r)
		}
		v.CarriageReturn()
		v.LineFeed()
	}

	// Record initial history count
	initialHistoryLen := v.displayBuf.history.TotalLen()

	// Scroll up into history
	v.Scroll(-5)

	// Verify we're viewing history
	if !v.displayBuf.display.viewingHistory {
		t.Fatal("Expected to be viewing history after scroll up")
	}

	// Try to trigger TUI signals - they should be ignored
	v.SetMargins(2, 20) // Would normally signal
	v.SetCursorPos(10, 5)
	v.SetCursorVisible(false)

	// TUI mode should NOT be active because we're viewing history
	if v.tuiMode.IsActive() {
		t.Error("TUI mode should NOT be active while viewing history")
	}

	// Try to commit - should be blocked
	v.commitViewportAsFixedWidth()

	// History should NOT have grown
	if v.displayBuf.history.TotalLen() != initialHistoryLen {
		t.Errorf("History should not grow while viewing history: was %d, now %d",
			initialHistoryLen, v.displayBuf.history.TotalLen())
	}
}

func TestVTerm_TUISnapshotClearedOnExit(t *testing.T) {
	// When a TUI app exits (resets scroll region to full screen),
	// the TUI snapshot should be cleared so old content doesn't persist.
	v := NewVTerm(80, 24)
	defer v.StopTUIMode()

	// Reconfigure with short debounce for test
	v.tuiMode.Stop()
	v.tuiMode = NewTUIMode(TUIModeConfig{
		IdleTimeout:        1 * time.Second,
		CommitDebounce:     30 * time.Millisecond,
		MinSignalsToCommit: 2,
	})

	v.tuiMode.SetCommitCallback(func() {
		v.displayBuf.display.CaptureTUISnapshot()
	})

	// Simulate TUI app: set non-full-screen scroll region
	v.SetMargins(2, 20)
	v.SetCursorPos(5, 0)

	// Write some content
	for _, r := range "TUI Content Here" {
		v.writeCharWithWrapping(r)
	}

	// Add more signals to trigger commit
	v.SetCursorPos(10, 5)
	v.SetCursorVisible(false)

	// Wait for debounce to trigger snapshot capture
	time.Sleep(50 * time.Millisecond)

	// Verify snapshot was captured
	if !v.displayBuf.display.HasTUISnapshot() {
		t.Fatal("TUI snapshot should have been captured")
	}

	// Now simulate TUI app exit: reset scroll region to full screen
	v.SetMargins(1, 24)

	// TUI snapshot should be cleared
	if v.displayBuf.display.HasTUISnapshot() {
		t.Error("TUI snapshot should be cleared when scroll region resets to full screen")
	}
}

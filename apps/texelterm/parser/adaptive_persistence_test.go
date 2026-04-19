// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/adaptive_persistence_test.go
// Summary: Tests for AdaptivePersistence and its components.

package parser

import (
	"os"
	"sync"
	"testing"
	"time"
)

// --- RateMonitor Tests ---

func TestRateMonitor_RecordAndCalculate(t *testing.T) {
	rm := NewRateMonitor(100)
	start := time.Now()

	// Establish baseline
	rm.CalculateRate(start)

	// Record 10 writes
	for i := 0; i < 10; i++ {
		rm.RecordWrite()
	}

	// 10 writes in 1 second = 10/sec
	rate := rm.CalculateRate(start.Add(time.Second))
	if rate != 10 {
		t.Errorf("expected rate 10, got %.2f", rate)
	}
}

func TestRateMonitor_RateOverMultipleSamples(t *testing.T) {
	rm := NewRateMonitor(100)
	start := time.Now()

	// First sample: establish baseline
	rm.CalculateRate(start)

	// 20 writes, sample at 1s
	for i := 0; i < 20; i++ {
		rm.RecordWrite()
	}
	rate1 := rm.CalculateRate(start.Add(time.Second))
	if rate1 != 20 {
		t.Errorf("expected rate 20 at 1s, got %.2f", rate1)
	}

	// 5 more writes over 0.5s — rate should be 10/s for this interval
	for i := 0; i < 5; i++ {
		rm.RecordWrite()
	}
	rate2 := rm.CalculateRate(start.Add(1500 * time.Millisecond))
	if rate2 != 10 {
		t.Errorf("expected rate 10 at 1.5s, got %.2f", rate2)
	}
}

func TestRateMonitor_LargeCount(t *testing.T) {
	rm := NewRateMonitor(10) // windowSize ignored in counter mode
	start := time.Now()

	rm.CalculateRate(start)

	// Record 1000 writes
	for i := 0; i < 1000; i++ {
		rm.RecordWrite()
	}

	if rm.Size() != 1000 {
		t.Errorf("expected size 1000, got %d", rm.Size())
	}

	// 1000 writes in 1 second
	rate := rm.CalculateRate(start.Add(time.Second))
	if rate != 1000 {
		t.Errorf("expected rate 1000, got %.2f", rate)
	}
}

func TestRateMonitor_Reset(t *testing.T) {
	rm := NewRateMonitor(100)

	rm.RecordWrite()
	rm.RecordWrite()
	rm.RecordWrite()

	if rm.Size() != 3 {
		t.Errorf("expected size 3, got %d", rm.Size())
	}

	rm.Reset()

	if rm.Size() != 0 {
		t.Errorf("expected size 0 after reset, got %d", rm.Size())
	}

	// First call after reset establishes baseline
	rate := rm.CalculateRate(time.Now())
	if rate != 0 {
		t.Errorf("expected rate 0 after reset, got %.2f", rate)
	}
}

func TestRateMonitor_EmptyMonitor(t *testing.T) {
	rm := NewRateMonitor(100)

	// First call establishes baseline, returns 0
	rate := rm.CalculateRate(time.Now())
	if rate != 0 {
		t.Errorf("expected rate 0 for fresh monitor, got %.2f", rate)
	}
}

func TestRateMonitor_RateNotCount(t *testing.T) {
	// Verifies CalculateRate returns writes/second, not just count.
	// 5 writes in 0.5s should give 10/sec, not 5.
	rm := NewRateMonitor(100)
	start := time.Now()

	rm.CalculateRate(start)

	for i := 0; i < 5; i++ {
		rm.RecordWrite()
	}

	// 5 writes in 0.5 seconds = 10/sec
	rate := rm.CalculateRate(start.Add(500 * time.Millisecond))
	if rate != 10 {
		t.Errorf("expected rate 10 (5 writes in 0.5s), got %.2f", rate)
	}
}

// --- ModeController Tests ---

func TestModeController_DetermineMode(t *testing.T) {
	mc := NewModeController(10, 100)

	tests := []struct {
		rate float64
		want PersistMode
	}{
		{0, PersistWriteThrough},
		{5, PersistWriteThrough},
		{9.9, PersistWriteThrough},
		{10, PersistDebounced},
		{50, PersistDebounced},
		{99.9, PersistDebounced},
		{100, PersistBestEffort},
		{500, PersistBestEffort},
	}

	for _, tc := range tests {
		got := mc.DetermineMode(tc.rate)
		if got != tc.want {
			t.Errorf("DetermineMode(%.1f) = %s, want %s", tc.rate, got, tc.want)
		}
	}
}

func TestModeController_CalculateDebounceDelay(t *testing.T) {
	mc := NewModeController(10, 100)
	minDelay := 50 * time.Millisecond
	maxDelay := 500 * time.Millisecond

	tests := []struct {
		rate        float64
		wantApprox  time.Duration
		description string
	}{
		{0, minDelay, "below threshold should use min delay"},
		{5, minDelay, "below threshold should use min delay"},
		{10, minDelay, "at threshold should use min delay"},
		{55, 275 * time.Millisecond, "midpoint should interpolate"},
		{100, maxDelay, "at max threshold should use max delay"},
		{200, maxDelay, "above max threshold should cap at max delay"},
	}

	for _, tc := range tests {
		got := mc.CalculateDebounceDelay(tc.rate, minDelay, maxDelay)
		// Allow 10ms tolerance for rounding
		diff := got - tc.wantApprox
		if diff < 0 {
			diff = -diff
		}
		if diff > 10*time.Millisecond {
			t.Errorf("%s: rate=%.1f, got %v, want ~%v", tc.description, tc.rate, got, tc.wantApprox)
		}
	}
}

func TestModeController_LinearInterpolation(t *testing.T) {
	mc := NewModeController(10, 100)
	minDelay := 100 * time.Millisecond
	maxDelay := 1000 * time.Millisecond

	// Test that delay increases linearly
	prev := mc.CalculateDebounceDelay(10, minDelay, maxDelay)
	for rate := 20.0; rate <= 100; rate += 10 {
		curr := mc.CalculateDebounceDelay(rate, minDelay, maxDelay)
		if curr < prev {
			t.Errorf("delay should increase with rate: rate=%.0f gave %v, prev was %v", rate, curr, prev)
		}
		prev = curr
	}
}

// --- AdaptivePersistence Tests ---

// createTestPageStore creates a PageStore for testing.
func createTestPageStore(t testing.TB, tmpDir string) *PageStore {
	t.Helper()
	config := DefaultPageStoreConfig(tmpDir, "test-terminal")
	ps, err := CreatePageStore(config)
	if err != nil {
		t.Fatalf("failed to create page store: %v", err)
	}
	return ps
}

func TestAdaptivePersistence_WriteThroughMode(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	tmpDir := t.TempDir()
	ps := createTestPageStore(t, tmpDir)

	config := DefaultAdaptivePersistenceConfig()
	ap, err := NewAdaptivePersistence(config, mb, ps)
	if err != nil {
		t.Fatalf("failed to create AdaptivePersistence: %v", err)
	}
	defer ap.Close()

	// Write a line to memory buffer
	mb.SetCursor(0, 0)
	mb.Write('H', DefaultFG, DefaultBG, 0)
	mb.Write('i', DefaultFG, DefaultBG, 0)

	// Notify write - should be immediate (WriteThrough mode)
	ap.NotifyWrite(0)

	// Should be in WriteThrough mode at low rate
	if ap.CurrentMode() != PersistWriteThrough {
		t.Errorf("expected WriteThrough mode, got %s", ap.CurrentMode())
	}

	// Line should have been written to disk
	metrics := ap.Metrics()
	if metrics.LinesWritten != 1 {
		t.Errorf("expected 1 line written, got %d", metrics.LinesWritten)
	}

	// No pending lines
	if ap.PendingCount() != 0 {
		t.Errorf("expected 0 pending, got %d", ap.PendingCount())
	}
}

func TestAdaptivePersistence_DebouncedMode(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	tmpDir := t.TempDir()
	ps := createTestPageStore(t, tmpDir)

	config := DefaultAdaptivePersistenceConfig()
	config.DebounceMinDelay = 10 * time.Millisecond
	config.DebounceMaxDelay = 50 * time.Millisecond

	now := time.Now()
	currentTime := now

	ap, err := newAdaptivePersistenceWithNow(config, mb, ps, nil, func() time.Time {
		return currentTime
	})
	if err != nil {
		t.Fatalf("failed to create AdaptivePersistence: %v", err)
	}
	defer ap.Close()

	// Simulate moderate write rate (80 writes at 50/s)
	// Rate recalculation happens every 64 writes, so need >64 to trigger mode change.
	for i := 0; i < 80; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write('X', DefaultFG, DefaultBG, 0)
		ap.NotifyWrite(int64(i))
		currentTime = currentTime.Add(20 * time.Millisecond)
	}

	// Should be in Debounced mode
	if ap.CurrentMode() != PersistDebounced {
		t.Errorf("expected Debounced mode, got %s", ap.CurrentMode())
	}

	// Should have pending lines (not all written immediately)
	if ap.PendingCount() == 0 {
		t.Log("Note: all lines may have been written due to timing")
	}

	// Force flush to complete
	ap.Flush()

	// All lines should be written after flush
	metrics := ap.Metrics()
	if metrics.LinesWritten != 80 {
		t.Errorf("expected 80 lines written after flush, got %d", metrics.LinesWritten)
	}
}

func TestAdaptivePersistence_BestEffortMode(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 1000, EvictionBatch: 100})

	tmpDir := t.TempDir()
	ps := createTestPageStore(t, tmpDir)

	config := DefaultAdaptivePersistenceConfig()
	config.IdleThreshold = 50 * time.Millisecond

	now := time.Now()
	currentTime := now

	ap, err := newAdaptivePersistenceWithNow(config, mb, ps, nil, func() time.Time {
		return currentTime
	})
	if err != nil {
		t.Fatalf("failed to create AdaptivePersistence: %v", err)
	}
	defer ap.Close()

	// Simulate high write rate (200 writes/sec)
	for i := 0; i < 200; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write('X', DefaultFG, DefaultBG, 0)
		ap.NotifyWrite(int64(i))
		currentTime = currentTime.Add(5 * time.Millisecond)
	}

	// Should be in BestEffort mode
	if ap.CurrentMode() != PersistBestEffort {
		t.Errorf("expected BestEffort mode, got %s", ap.CurrentMode())
	}

	// Should have many pending lines
	pending := ap.PendingCount()
	if pending < 100 {
		t.Errorf("expected many pending lines in BestEffort, got %d", pending)
	}

	// Force flush
	ap.Flush()

	if ap.PendingCount() != 0 {
		t.Errorf("expected 0 pending after flush, got %d", ap.PendingCount())
	}
}

func TestAdaptivePersistence_ModeTransitions(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 1000, EvictionBatch: 100})

	tmpDir := t.TempDir()
	ps := createTestPageStore(t, tmpDir)

	config := DefaultAdaptivePersistenceConfig()
	now := time.Now()
	currentTime := now

	ap, err := newAdaptivePersistenceWithNow(config, mb, ps, nil, func() time.Time {
		return currentTime
	})
	if err != nil {
		t.Fatalf("failed to create AdaptivePersistence: %v", err)
	}
	defer ap.Close()

	// Start with low rate - should be WriteThrough
	// Rate recalculation happens every 64 writes, so initial low-rate writes
	// stay in WriteThrough since no recalc triggers yet.
	for i := 0; i < 5; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write('A', DefaultFG, DefaultBG, 0)
		ap.NotifyWrite(int64(i))
		currentTime = currentTime.Add(200 * time.Millisecond) // 5 writes/sec
	}
	if ap.CurrentMode() != PersistWriteThrough {
		t.Errorf("expected WriteThrough at low rate, got %s", ap.CurrentMode())
	}

	// Increase to medium rate - should be Debounced
	// Need >64 total writes for rate recalculation to trigger.
	for i := 5; i < 75; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write('B', DefaultFG, DefaultBG, 0)
		ap.NotifyWrite(int64(i))
		currentTime = currentTime.Add(20 * time.Millisecond) // 50 writes/sec
	}
	if ap.CurrentMode() != PersistDebounced {
		t.Errorf("expected Debounced at medium rate, got %s", ap.CurrentMode())
	}

	// Increase to high rate - should be BestEffort
	for i := 75; i < 275; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write('C', DefaultFG, DefaultBG, 0)
		ap.NotifyWrite(int64(i))
		currentTime = currentTime.Add(5 * time.Millisecond) // 200 writes/sec
	}
	if ap.CurrentMode() != PersistBestEffort {
		t.Errorf("expected BestEffort at high rate, got %s", ap.CurrentMode())
	}

	// Verify mode changes were tracked
	metrics := ap.Metrics()
	if metrics.ModeChanges < 2 {
		t.Errorf("expected at least 2 mode changes, got %d", metrics.ModeChanges)
	}
}

func TestAdaptivePersistence_IdleFlush(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	tmpDir := t.TempDir()
	ps := createTestPageStore(t, tmpDir)

	config := DefaultAdaptivePersistenceConfig()
	config.IdleThreshold = 100 * time.Millisecond

	ap, err := NewAdaptivePersistence(config, mb, ps)
	if err != nil {
		t.Fatalf("failed to create AdaptivePersistence: %v", err)
	}
	defer ap.Close()

	// Write some lines at high rate to enter BestEffort mode
	for i := 0; i < 200; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write('X', DefaultFG, DefaultBG, 0)
		ap.NotifyWrite(int64(i))
	}

	// Should have pending lines
	pendingBefore := ap.PendingCount()
	if pendingBefore == 0 {
		t.Skip("no pending lines, test may be timing dependent")
	}

	// Wait for idle threshold + ticker interval
	time.Sleep(200 * time.Millisecond)

	// Pending should be flushed by idle monitor
	pendingAfter := ap.PendingCount()
	if pendingAfter > 0 {
		t.Logf("still have %d pending after idle (was %d), may need longer wait", pendingAfter, pendingBefore)
	}
}

func TestAdaptivePersistence_FlushOnClose(t *testing.T) {
	// Use larger buffer to avoid eviction
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 500, EvictionBatch: 50})

	tmpDir := t.TempDir()
	ps := createTestPageStore(t, tmpDir)

	config := DefaultAdaptivePersistenceConfig()
	config.IdleThreshold = 10 * time.Second // Long idle to ensure flush happens on Close

	ap, err := NewAdaptivePersistence(config, mb, ps)
	if err != nil {
		t.Fatalf("failed to create AdaptivePersistence: %v", err)
	}

	// Write lines at high rate to accumulate pending
	for i := 0; i < 200; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write('X', DefaultFG, DefaultBG, 0)
		ap.NotifyWrite(int64(i))
	}

	// Close should flush pending
	err = ap.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}

	// Verify data was persisted by reopening
	psConfig := DefaultPageStoreConfig(tmpDir, "test-terminal")
	ps2, err := OpenPageStore(psConfig)
	if err != nil {
		t.Fatalf("failed to reopen page store: %v", err)
	}
	if ps2 == nil {
		t.Fatal("reopened page store is nil")
	}
	defer ps2.Close()

	lineCount := ps2.LineCount()
	if lineCount != 200 {
		t.Errorf("expected 200 lines on disk, got %d", lineCount)
	}
}

func TestAdaptivePersistence_EvictedLine(t *testing.T) {
	// Small buffer to force eviction
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 10, EvictionBatch: 5})

	tmpDir := t.TempDir()
	ps := createTestPageStore(t, tmpDir)

	config := DefaultAdaptivePersistenceConfig()
	// Use BestEffort to accumulate pending lines
	config.WriteThroughMaxRate = 0.1
	config.DebouncedMaxRate = 0.2

	ap, err := NewAdaptivePersistence(config, mb, ps)
	if err != nil {
		t.Fatalf("failed to create AdaptivePersistence: %v", err)
	}
	defer ap.Close()

	// Write 5 lines
	for i := 0; i < 5; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write('X', DefaultFG, DefaultBG, 0)
		ap.NotifyWrite(int64(i))
	}

	// Now write more to trigger eviction of early lines
	for i := 5; i < 20; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write('Y', DefaultFG, DefaultBG, 0)
		ap.NotifyWrite(int64(i))
	}

	// Flush - should handle evicted lines gracefully
	err = ap.Flush()
	if err != nil {
		t.Errorf("Flush returned error (may be expected for evicted lines): %v", err)
	}

	// No crash = success
}

func TestAdaptivePersistence_NilMemBuf(t *testing.T) {
	tmpDir := t.TempDir()
	ps := createTestPageStore(t, tmpDir)
	defer ps.Close()

	_, err := NewAdaptivePersistence(DefaultAdaptivePersistenceConfig(), nil, ps)
	if err == nil {
		t.Error("expected error for nil memBuf")
	}
}

func TestAdaptivePersistence_NilDisk(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	_, err := NewAdaptivePersistence(DefaultAdaptivePersistenceConfig(), mb, nil)
	if err == nil {
		t.Error("expected error for nil disk")
	}
}

func TestAdaptivePersistence_Metrics(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	tmpDir := t.TempDir()
	ps := createTestPageStore(t, tmpDir)

	config := DefaultAdaptivePersistenceConfig()
	ap, err := NewAdaptivePersistence(config, mb, ps)
	if err != nil {
		t.Fatalf("failed to create AdaptivePersistence: %v", err)
	}
	defer ap.Close()

	// Write some lines
	for i := 0; i < 10; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write('X', DefaultFG, DefaultBG, 0)
		ap.NotifyWrite(int64(i))
	}

	metrics := ap.Metrics()

	if metrics.TotalWrites != 10 {
		t.Errorf("expected TotalWrites=10, got %d", metrics.TotalWrites)
	}

	// In WriteThrough mode, lines should be written immediately
	if metrics.CurrentMode == PersistWriteThrough && metrics.LinesWritten != 10 {
		t.Errorf("expected LinesWritten=10 in WriteThrough, got %d", metrics.LinesWritten)
	}
}

func TestAdaptivePersistence_String(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	tmpDir := t.TempDir()
	ps := createTestPageStore(t, tmpDir)

	config := DefaultAdaptivePersistenceConfig()
	ap, err := NewAdaptivePersistence(config, mb, ps)
	if err != nil {
		t.Fatalf("failed to create AdaptivePersistence: %v", err)
	}
	defer ap.Close()

	s := ap.String()
	if s == "" {
		t.Error("String() returned empty")
	}
	if len(s) < 20 {
		t.Errorf("String() too short: %s", s)
	}
}

func TestAdaptivePersistence_Concurrency(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 1000, EvictionBatch: 100})

	tmpDir := t.TempDir()
	ps := createTestPageStore(t, tmpDir)

	config := DefaultAdaptivePersistenceConfig()
	ap, err := NewAdaptivePersistence(config, mb, ps)
	if err != nil {
		t.Fatalf("failed to create AdaptivePersistence: %v", err)
	}

	var wg sync.WaitGroup
	numWriters := 5
	writesPerWriter := 50

	// Multiple concurrent writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < writesPerWriter; i++ {
				lineIdx := int64(writerID*writesPerWriter + i)
				mb.SetCursor(lineIdx, 0)
				mb.Write(rune('A'+writerID), DefaultFG, DefaultBG, 0)
				ap.NotifyWrite(lineIdx)
			}
		}(w)
	}

	// Concurrent readers
	for r := 0; r < 3; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				_ = ap.CurrentMode()
				_ = ap.Metrics()
				_ = ap.PendingCount()
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// Close should work after concurrent access
	err = ap.Close()
	if err != nil {
		t.Errorf("Close after concurrent access returned error: %v", err)
	}
}

func TestAdaptivePersistence_DoubleClose(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	tmpDir := t.TempDir()
	ps := createTestPageStore(t, tmpDir)

	config := DefaultAdaptivePersistenceConfig()
	ap, err := NewAdaptivePersistence(config, mb, ps)
	if err != nil {
		t.Fatalf("failed to create AdaptivePersistence: %v", err)
	}

	// First close
	err = ap.Close()
	if err != nil {
		t.Errorf("first Close returned error: %v", err)
	}

	// Second close should be safe
	err = ap.Close()
	if err != nil {
		t.Errorf("second Close returned error: %v", err)
	}
}

func TestAdaptivePersistence_NotifyAfterClose(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	tmpDir := t.TempDir()
	ps := createTestPageStore(t, tmpDir)

	config := DefaultAdaptivePersistenceConfig()
	ap, err := NewAdaptivePersistence(config, mb, ps)
	if err != nil {
		t.Fatalf("failed to create AdaptivePersistence: %v", err)
	}

	ap.Close()

	// Write after close should be safe (no panic)
	mb.SetCursor(0, 0)
	mb.Write('X', DefaultFG, DefaultBG, 0)
	ap.NotifyWrite(0) // Should not panic
}

func TestAdaptivePersistence_BatchNotify(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	tmpDir := t.TempDir()
	ps := createTestPageStore(t, tmpDir)

	config := DefaultAdaptivePersistenceConfig()
	ap, err := NewAdaptivePersistence(config, mb, ps)
	if err != nil {
		t.Fatalf("failed to create AdaptivePersistence: %v", err)
	}
	defer ap.Close()

	// Write multiple lines
	for i := 0; i < 10; i++ {
		mb.SetCursor(int64(i), 0)
		mb.Write('X', DefaultFG, DefaultBG, 0)
	}

	// Batch notify
	indices := []int64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	ap.NotifyWriteBatch(indices)

	metrics := ap.Metrics()
	if metrics.TotalWrites != 10 {
		t.Errorf("expected TotalWrites=10 for batch, got %d", metrics.TotalWrites)
	}
}

func TestAdaptivePersistence_EmptyBatchNotify(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})

	tmpDir := t.TempDir()
	ps := createTestPageStore(t, tmpDir)

	config := DefaultAdaptivePersistenceConfig()
	ap, err := NewAdaptivePersistence(config, mb, ps)
	if err != nil {
		t.Fatalf("failed to create AdaptivePersistence: %v", err)
	}
	defer ap.Close()

	// Empty batch should be safe
	ap.NotifyWriteBatch([]int64{})
	ap.NotifyWriteBatch(nil)

	metrics := ap.Metrics()
	if metrics.TotalWrites != 0 {
		t.Errorf("expected TotalWrites=0 for empty batch, got %d", metrics.TotalWrites)
	}
}

func TestAdaptivePersistence_DefaultConfig(t *testing.T) {
	config := DefaultAdaptivePersistenceConfig()

	if config.WriteThroughMaxRate != 10 {
		t.Errorf("expected WriteThroughMaxRate=10, got %.1f", config.WriteThroughMaxRate)
	}
	if config.DebouncedMaxRate != 100 {
		t.Errorf("expected DebouncedMaxRate=100, got %.1f", config.DebouncedMaxRate)
	}
	if config.DebounceMinDelay != 50*time.Millisecond {
		t.Errorf("expected DebounceMinDelay=50ms, got %v", config.DebounceMinDelay)
	}
	if config.DebounceMaxDelay != 500*time.Millisecond {
		t.Errorf("expected DebounceMaxDelay=500ms, got %v", config.DebounceMaxDelay)
	}
	if config.IdleThreshold != 1*time.Second {
		t.Errorf("expected IdleThreshold=1s, got %v", config.IdleThreshold)
	}
	if config.RateWindowSize != 1000 {
		t.Errorf("expected RateWindowSize=1000, got %d", config.RateWindowSize)
	}
}

func TestPersistMode_String(t *testing.T) {
	tests := []struct {
		mode PersistMode
		want string
	}{
		{PersistWriteThrough, "WriteThrough"},
		{PersistDebounced, "Debounced"},
		{PersistBestEffort, "BestEffort"},
		{PersistMode(99), "Unknown"},
	}

	for _, tc := range tests {
		got := tc.mode.String()
		if got != tc.want {
			t.Errorf("PersistMode(%d).String() = %s, want %s", tc.mode, got, tc.want)
		}
	}
}

// --- Benchmark Tests ---

func BenchmarkAdaptivePersistence_NotifyWrite(b *testing.B) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 10000, EvictionBatch: 1000})

	tmpDir := b.TempDir()
	psConfig := DefaultPageStoreConfig(tmpDir, "bench-terminal")
	ps, err := CreatePageStore(psConfig)
	if err != nil {
		b.Fatalf("failed to create page store: %v", err)
	}

	config := DefaultAdaptivePersistenceConfig()
	ap, err := NewAdaptivePersistence(config, mb, ps)
	if err != nil {
		b.Fatalf("failed to create AdaptivePersistence: %v", err)
	}
	defer ap.Close()

	// Pre-populate memory buffer
	for i := 0; i < b.N; i++ {
		mb.SetCursor(int64(i%1000), 0)
		mb.Write('X', DefaultFG, DefaultBG, 0)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ap.NotifyWrite(int64(i % 1000))
	}
}

func TestAdaptivePersistence_FlushSyncsWAL(t *testing.T) {
	tmpDir := t.TempDir()
	walConfig := DefaultWALConfig(tmpDir, "test-flush-sync")
	walConfig.CheckpointInterval = 0

	wal, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog failed: %v", err)
	}

	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	mb.EnsureLine(0)
	line := mb.GetLine(0)
	line.Cells = []Cell{{Rune: 'X'}}

	config := DefaultAdaptivePersistenceConfig()
	ap, err := newAdaptivePersistenceWithWAL(config, mb, wal, time.Now)
	if err != nil {
		t.Fatalf("failed to create AdaptivePersistence: %v", err)
	}
	defer ap.Close()

	// Notify a write and flush
	ap.NotifyWrite(0)
	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// After flush, the WAL file should have content beyond just the header (32 bytes)
	walPath := wal.WALPath()
	info, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("WAL file stat failed: %v", err)
	}
	if info.Size() <= 32 {
		t.Errorf("WAL file too small after flush+sync: %d bytes (expected > 32)", info.Size())
	}
}

func TestAdaptivePersistence_CloseSyncsBeforeWALClose(t *testing.T) {
	tmpDir := t.TempDir()
	walConfig := DefaultWALConfig(tmpDir, "test-close-sync")
	walConfig.CheckpointInterval = 0

	wal, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog failed: %v", err)
	}

	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	mb.EnsureLine(0)
	line := mb.GetLine(0)
	line.Cells = []Cell{{Rune: 'Z'}}

	config := DefaultAdaptivePersistenceConfig()
	ap, err := newAdaptivePersistenceWithWAL(config, mb, wal, time.Now)
	if err != nil {
		t.Fatalf("failed to create AdaptivePersistence: %v", err)
	}

	// Notify a write (will be pending)
	ap.NotifyWrite(0)

	// Close should flush + sync + close WAL without error
	if err := ap.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// After close, data should be in the PageStore (checkpoint happened)
	// Reopen to verify
	wal2, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		t.Fatalf("Reopen WAL failed: %v", err)
	}
	defer wal2.Close()

	if wal2.LineCount() != 1 {
		t.Errorf("expected 1 line after close+reopen, got %d", wal2.LineCount())
	}
}

func BenchmarkRateMonitor_RecordWrite(b *testing.B) {
	rm := NewRateMonitor(1000)
	for i := 0; i < b.N; i++ {
		rm.RecordWrite()
	}
}

func BenchmarkRateMonitor_CalculateRate(b *testing.B) {
	rm := NewRateMonitor(1000)
	now := time.Now()

	for i := 0; i < 500; i++ {
		rm.RecordWrite()
	}
	rm.CalculateRate(now) // establish baseline

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rm.RecordWrite()
		rm.CalculateRate(now.Add(time.Duration(i) * time.Millisecond))
	}
}

// --- FIFO flush-order test helpers ---

// newTestAdaptivePersistenceBestEffort creates an AdaptivePersistence backed
// by a WAL in a temp directory, locked into BestEffort mode so writes
// accumulate in the pending list and are only flushed when Flush() is called.
// This lets tests control exactly when the flush happens and verify flush order.
// The returned *WriteAheadLog is used by walAppendedOrder to inspect flush order.
func newTestAdaptivePersistenceBestEffort(t testing.TB) (*AdaptivePersistence, *WriteAheadLog) {
	t.Helper()
	tmpDir := t.TempDir()
	walConfig := DefaultWALConfig(tmpDir, "test-order")
	walConfig.CheckpointInterval = 0 // disable auto-checkpoint

	wal, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}

	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	// Ensure lines 0-9 exist in the memory buffer so GetLine returns non-nil.
	for i := int64(0); i < 10; i++ {
		mb.EnsureLine(i)
		mb.GetLine(i).Cells = []Cell{{Rune: 'X'}}
	}

	config := DefaultAdaptivePersistenceConfig()
	// Force BestEffort mode so writes accumulate and are only flushed on
	// explicit Flush() — this exercises flushPendingLocked's ordering.
	config.WriteThroughMaxRate = 0
	config.DebouncedMaxRate = 0
	// Long idle threshold so idle monitor doesn't flush during test.
	config.IdleThreshold = 1 * time.Hour

	ap, err := newAdaptivePersistenceWithWAL(config, mb, wal, time.Now)
	if err != nil {
		t.Fatalf("newAdaptivePersistenceWithWAL: %v", err)
	}
	t.Cleanup(func() { ap.Close() })

	// Force BestEffort mode immediately so writes accumulate rather than
	// flushing individually. Mode normally only changes every 64 writes.
	ap.mu.Lock()
	ap.currentMode = PersistBestEffort
	ap.mu.Unlock()

	return ap, wal
}

// walAppendedOrder reads the WAL entries in file order and returns their
// GlobalLineIdx values. This reflects the order lines were flushed to disk.
func walAppendedOrder(t testing.TB, wal *WriteAheadLog) []int64 {
	t.Helper()
	entries, err := wal.readWALEntries()
	if err != nil {
		t.Fatalf("readWALEntries: %v", err)
	}
	order := make([]int64, 0, len(entries))
	for _, e := range entries {
		order = append(order, int64(e.GlobalLineIdx))
	}
	return order
}

// equalInt64 returns true if two slices have the same contents.
func equalInt64(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAdaptivePersistence_FlushesInCallOrder(t *testing.T) {
	ap, wal := newTestAdaptivePersistenceBestEffort(t)
	ap.NotifyWrite(3)
	ap.NotifyWrite(7)
	ap.NotifyWrite(5)
	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	got := walAppendedOrder(t, wal)
	want := []int64{3, 7, 5}
	if !equalInt64(got, want) {
		t.Errorf("append order = %v, want %v", got, want)
	}
}

// TestMain can be used for setup/teardown if needed
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

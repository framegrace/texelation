// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/adaptive_persistence.go
// Summary: AdaptivePersistence manages disk writes with dynamic rate adjustment.
//
// Architecture:
//
//	AdaptivePersistence is the persistence layer between MemoryBuffer and disk.
//	It monitors write rate and dynamically adjusts persistence strategy:
//
//	  - WriteThrough (< 10 writes/sec): Immediate disk writes for each line
//	  - Debounced (10-100 writes/sec): Batched writes with adaptive delay
//	  - BestEffort (> 100 writes/sec): Flush only on idle or explicit request
//
//	The debounce delay scales linearly with write rate (adaptive debouncing):
//	slower rates get shorter delays (more responsive), faster rates get longer
//	delays (reduced I/O overhead).
//
//	A background goroutine monitors for idle periods and flushes pending writes
//	when the terminal becomes idle, ensuring data is eventually persisted even
//	in BestEffort mode.
//
// Usage (Phase 4 integration):
//
//	persistence, _ := NewAdaptivePersistence(config, memBuf, diskHistory)
//	defer persistence.Close()
//	// After each write to MemoryBuffer:
//	persistence.NotifyWrite(memBuf.CursorLine())

package parser

import (
	"fmt"
	"slices"
	"sync"
	"time"
)

// AdaptivePersistenceConfig holds configuration for the adaptive persistence layer.
type AdaptivePersistenceConfig struct {
	// Rate thresholds for mode switching (writes per second)
	WriteThroughMaxRate float64 // Below this: WriteThrough (default: 10)
	DebouncedMaxRate    float64 // Below this: Debounced; above: BestEffort (default: 100)

	// Debounce timing (adaptive: delay scales with rate)
	DebounceMinDelay time.Duration // Minimum delay when rate is low (default: 50ms)
	DebounceMaxDelay time.Duration // Maximum delay when rate approaches threshold (default: 500ms)

	// Idle detection for BestEffort mode
	IdleThreshold time.Duration // Flush after this much idle time (default: 1s)

	// Ring buffer size for rate calculation
	RateWindowSize int // Number of timestamps to track (default: 1000)
}

// DefaultAdaptivePersistenceConfig returns sensible default configuration.
func DefaultAdaptivePersistenceConfig() AdaptivePersistenceConfig {
	return AdaptivePersistenceConfig{
		WriteThroughMaxRate: 10,
		DebouncedMaxRate:    100,
		DebounceMinDelay:    50 * time.Millisecond,
		DebounceMaxDelay:    500 * time.Millisecond,
		IdleThreshold:       1 * time.Second,
		RateWindowSize:      1000,
	}
}

// PersistenceMetrics tracks performance data for monitoring.
type PersistenceMetrics struct {
	TotalWrites      int64       // Total NotifyWrite calls
	TotalFlushes     int64       // Number of flush operations
	LinesWritten     int64       // Successful disk writes
	ModeChanges      int64       // Mode transition count
	CurrentMode      PersistMode // Current persistence mode
	CurrentWriteRate float64     // Current writes per second
	FailedWrites     int64       // Disk write errors (logged but continued)
}

// AdaptivePersistence manages disk writes with dynamic rate adjustment.
type AdaptivePersistence struct {
	config  AdaptivePersistenceConfig
	memBuf  *MemoryBuffer
	disk    *DiskHistory
	nowFunc func() time.Time // For testing; defaults to time.Now

	// Components
	rateMonitor *RateMonitor
	modeCtrl    *ModeController

	// State
	currentMode  PersistMode
	pendingLines map[int64]bool // Lines awaiting flush
	lastActivity time.Time

	// Debounce timer
	flushTimer *time.Timer

	// Background goroutine for idle detection
	idleTicker *time.Ticker
	stopCh     chan struct{}
	stopped    bool
	stopOnce   sync.Once // Ensures stopIdleMonitor runs exactly once

	// Metrics
	metrics PersistenceMetrics

	mu sync.Mutex
}

// NewAdaptivePersistence creates a new adaptive persistence layer.
//
// Parameters:
//   - config: Configuration for rate thresholds and timing
//   - memBuf: MemoryBuffer to read dirty lines from
//   - disk: DiskHistory to write lines to (passed in for testability)
//
// The background idle monitor is started automatically.
// Call Close() when done to flush pending writes and stop the monitor.
func NewAdaptivePersistence(
	config AdaptivePersistenceConfig,
	memBuf *MemoryBuffer,
	disk *DiskHistory,
) (*AdaptivePersistence, error) {
	return newAdaptivePersistenceWithNow(config, memBuf, disk, time.Now)
}

// newAdaptivePersistenceWithNow allows injecting a custom time function for testing.
func newAdaptivePersistenceWithNow(
	config AdaptivePersistenceConfig,
	memBuf *MemoryBuffer,
	disk *DiskHistory,
	nowFunc func() time.Time,
) (*AdaptivePersistence, error) {
	if memBuf == nil {
		return nil, fmt.Errorf("memBuf cannot be nil")
	}
	if disk == nil {
		return nil, fmt.Errorf("disk cannot be nil")
	}
	if nowFunc == nil {
		nowFunc = time.Now
	}

	// Apply defaults for zero values
	if config.WriteThroughMaxRate <= 0 {
		config.WriteThroughMaxRate = 10
	}
	if config.DebouncedMaxRate <= 0 {
		config.DebouncedMaxRate = 100
	}
	if config.DebounceMinDelay <= 0 {
		config.DebounceMinDelay = 50 * time.Millisecond
	}
	if config.DebounceMaxDelay <= 0 {
		config.DebounceMaxDelay = 500 * time.Millisecond
	}
	if config.IdleThreshold <= 0 {
		config.IdleThreshold = 1 * time.Second
	}
	if config.RateWindowSize <= 0 {
		config.RateWindowSize = 1000
	}

	ap := &AdaptivePersistence{
		config:       config,
		memBuf:       memBuf,
		disk:         disk,
		nowFunc:      nowFunc,
		rateMonitor:  NewRateMonitor(config.RateWindowSize),
		modeCtrl:     NewModeController(config.WriteThroughMaxRate, config.DebouncedMaxRate),
		currentMode:  PersistWriteThrough,
		pendingLines: make(map[int64]bool),
		lastActivity: nowFunc(),
		stopCh:       make(chan struct{}),
		stopped:      false,
		metrics: PersistenceMetrics{
			CurrentMode: PersistWriteThrough,
		},
	}

	// Start idle monitor
	ap.startIdleMonitor()

	return ap, nil
}

// NotifyWrite is called when a line changes in MemoryBuffer.
// It records the write, updates the mode, and handles persistence based on mode.
func (ap *AdaptivePersistence) NotifyWrite(lineIdx int64) {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	if ap.stopped {
		return
	}

	ap.metrics.TotalWrites++
	writeRate := ap.updateRateAndModeLocked()

	// Handle based on mode
	ap.handleWriteLocked([]int64{lineIdx}, writeRate)
}

// NotifyWriteBatch records multiple line writes efficiently.
// Use this when multiple lines change at once (e.g., scroll operations).
func (ap *AdaptivePersistence) NotifyWriteBatch(lineIndices []int64) {
	if len(lineIndices) == 0 {
		return
	}

	ap.mu.Lock()
	defer ap.mu.Unlock()

	if ap.stopped {
		return
	}

	ap.metrics.TotalWrites += int64(len(lineIndices))
	writeRate := ap.updateRateAndModeLocked()

	// Handle based on mode
	ap.handleWriteLocked(lineIndices, writeRate)
}

// updateRateAndModeLocked records a write timestamp, calculates rate, and adjusts mode.
// Must be called with ap.mu held.
func (ap *AdaptivePersistence) updateRateAndModeLocked() float64 {
	now := ap.nowFunc()
	ap.lastActivity = now

	// Record timestamp and calculate rate
	ap.rateMonitor.RecordWrite(now)
	writeRate := ap.rateMonitor.CalculateRate(now, time.Second)
	ap.metrics.CurrentWriteRate = writeRate

	// Adjust mode based on rate
	newMode := ap.modeCtrl.DetermineMode(writeRate)
	if newMode != ap.currentMode {
		ap.currentMode = newMode
		ap.metrics.CurrentMode = newMode
		ap.metrics.ModeChanges++
	}

	return writeRate
}

// handleWriteLocked processes line writes based on current mode.
// Must be called with ap.mu held.
func (ap *AdaptivePersistence) handleWriteLocked(lineIndices []int64, writeRate float64) {
	switch ap.currentMode {
	case PersistWriteThrough:
		// Immediate write for each line
		for _, idx := range lineIndices {
			ap.flushLineLocked(idx)
		}

	case PersistDebounced:
		// Add to pending and schedule debounced flush with adaptive delay
		for _, idx := range lineIndices {
			ap.pendingLines[idx] = true
		}
		delay := ap.modeCtrl.CalculateDebounceDelay(
			writeRate,
			ap.config.DebounceMinDelay,
			ap.config.DebounceMaxDelay,
		)
		ap.scheduleFlushLocked(delay)

	case PersistBestEffort:
		// Just add to pending; idle monitor will flush
		for _, idx := range lineIndices {
			ap.pendingLines[idx] = true
		}
	}
}

// Flush forces immediate flush of all pending writes.
func (ap *AdaptivePersistence) Flush() error {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	ap.cancelFlushTimerLocked()
	return ap.flushPendingLocked()
}

// Close flushes pending writes, stops the idle monitor, and closes the disk.
func (ap *AdaptivePersistence) Close() error {
	ap.mu.Lock()

	if ap.stopped {
		ap.mu.Unlock()
		return nil
	}
	ap.stopped = true

	// Cancel debounce timer
	ap.cancelFlushTimerLocked()

	// Flush pending writes
	flushErr := ap.flushPendingLocked()

	ap.mu.Unlock()

	// Stop idle monitor (outside lock to avoid deadlock)
	ap.stopIdleMonitor()

	// Close disk
	diskErr := ap.disk.Close()

	// Return first error
	if flushErr != nil {
		return flushErr
	}
	return diskErr
}

// CurrentMode returns the current persistence mode.
func (ap *AdaptivePersistence) CurrentMode() PersistMode {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	return ap.currentMode
}

// Metrics returns a copy of current metrics.
func (ap *AdaptivePersistence) Metrics() PersistenceMetrics {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	return ap.metrics
}

// PendingCount returns the number of lines awaiting flush.
func (ap *AdaptivePersistence) PendingCount() int {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	return len(ap.pendingLines)
}

// String returns debug information.
func (ap *AdaptivePersistence) String() string {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	return fmt.Sprintf("AdaptivePersistence{mode=%s, rate=%.2f/s, pending=%d, writes=%d, flushes=%d}",
		ap.currentMode, ap.metrics.CurrentWriteRate, len(ap.pendingLines),
		ap.metrics.TotalWrites, ap.metrics.TotalFlushes)
}

// --- Internal Methods ---

// scheduleFlushLocked sets or resets the debounce timer.
// Must be called with ap.mu held.
func (ap *AdaptivePersistence) scheduleFlushLocked(delay time.Duration) {
	ap.cancelFlushTimerLocked()

	ap.flushTimer = time.AfterFunc(delay, func() {
		ap.mu.Lock()
		defer ap.mu.Unlock()
		if !ap.stopped {
			ap.flushPendingLocked()
		}
	})
}

// cancelFlushTimerLocked stops any pending debounce timer.
// Must be called with ap.mu held.
func (ap *AdaptivePersistence) cancelFlushTimerLocked() {
	if ap.flushTimer != nil {
		ap.flushTimer.Stop()
		ap.flushTimer = nil
	}
}

// flushPendingLocked writes all pending lines to disk.
// Must be called with ap.mu held.
func (ap *AdaptivePersistence) flushPendingLocked() error {
	if len(ap.pendingLines) == 0 {
		return nil
	}

	ap.metrics.TotalFlushes++

	// Collect and sort line indices for deterministic order
	indices := make([]int64, 0, len(ap.pendingLines))
	for idx := range ap.pendingLines {
		indices = append(indices, idx)
	}
	slices.Sort(indices)

	var firstErr error
	for _, idx := range indices {
		if err := ap.flushLineLocked(idx); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			ap.metrics.FailedWrites++
			// Log and continue with other lines
		}
	}

	// Clear pending set
	ap.pendingLines = make(map[int64]bool)

	return firstErr
}

// flushLineLocked writes a single line to disk.
// Must be called with ap.mu held.
func (ap *AdaptivePersistence) flushLineLocked(lineIdx int64) error {
	line := ap.memBuf.GetLine(lineIdx)
	if line == nil {
		// Line was evicted from memory - clear dirty and skip
		ap.memBuf.ClearDirty(lineIdx)
		delete(ap.pendingLines, lineIdx)
		return nil
	}

	if err := ap.disk.AppendLine(line); err != nil {
		// Log error but don't clear dirty (retry on next flush)
		return fmt.Errorf("failed to write line %d: %w", lineIdx, err)
	}

	ap.memBuf.ClearDirty(lineIdx)
	ap.metrics.LinesWritten++
	return nil
}

// startIdleMonitor starts the background goroutine for idle detection.
func (ap *AdaptivePersistence) startIdleMonitor() {
	// Check for idle at half the threshold interval
	checkInterval := max(ap.config.IdleThreshold/2, 100*time.Millisecond)

	ap.idleTicker = time.NewTicker(checkInterval)

	go func() {
		for {
			select {
			case <-ap.idleTicker.C:
				ap.checkIdle()
			case <-ap.stopCh:
				return
			}
		}
	}()
}

// stopIdleMonitor stops the background goroutine.
// Safe to call multiple times due to sync.Once.
func (ap *AdaptivePersistence) stopIdleMonitor() {
	ap.stopOnce.Do(func() {
		if ap.idleTicker != nil {
			ap.idleTicker.Stop()
		}
		close(ap.stopCh)
	})
}

// checkIdle flushes pending lines if idle threshold exceeded.
func (ap *AdaptivePersistence) checkIdle() {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	if ap.stopped || len(ap.pendingLines) == 0 {
		return
	}

	idleDuration := ap.nowFunc().Sub(ap.lastActivity)
	if idleDuration >= ap.config.IdleThreshold {
		ap.flushPendingLocked()
	}
}

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
	"log"
	"slices"
	"sync"
	"time"
)

// Monitoring thresholds for adaptive persistence
const (
	// Log warning when pending lines exceed this count
	pendingLineWarningThreshold = 500

	// Log warning when flush takes longer than this
	flushSlowThreshold = 100 * time.Millisecond

	// Log info when write rate exceeds this (high activity indicator)
	highWriteRateThreshold = 50.0 // writes per second
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
// pendingLineInfo stores metadata for a line awaiting flush.
type pendingLineInfo struct {
	timestamp time.Time
	isCommand bool
}

type AdaptivePersistence struct {
	config  AdaptivePersistenceConfig
	memBuf  *MemoryBuffer
	disk    *PageStore       // Direct PageStore (legacy mode)
	wal     *WriteAheadLog   // WAL-based persistence (preferred)
	nowFunc func() time.Time // For testing; defaults to time.Now

	// Components
	rateMonitor *RateMonitor
	modeCtrl    *ModeController

	// State
	currentMode     PersistMode
	pendingLines    map[int64]*pendingLineInfo // Lines awaiting flush with metadata
	pendingMetadata *ViewportState             // Metadata awaiting flush (written with content)
	lastActivity    time.Time

	// Callback for search indexing - called AFTER line is persisted to WAL
	// This ensures search index only has entries for content that exists on disk.
	OnLineIndexed func(lineIdx int64, line *LogicalLine, timestamp time.Time, isCommand bool)

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
//   - disk: PageStore to write lines to (passed in for testability)
//
// The background idle monitor is started automatically.
// Call Close() when done to flush pending writes and stop the monitor.
//
// Deprecated: Use NewAdaptivePersistenceWithWAL for crash recovery support.
func NewAdaptivePersistence(
	config AdaptivePersistenceConfig,
	memBuf *MemoryBuffer,
	disk *PageStore,
) (*AdaptivePersistence, error) {
	return newAdaptivePersistenceWithNow(config, memBuf, disk, nil, time.Now)
}

// NewAdaptivePersistenceWithWAL creates an adaptive persistence layer with WAL support.
//
// Parameters:
//   - config: Configuration for rate thresholds and timing
//   - memBuf: MemoryBuffer to read dirty lines from
//   - walConfig: Configuration for the Write-Ahead Log
//
// The WAL provides crash recovery by journaling writes before committing to PageStore.
// On startup, uncommitted entries are recovered automatically.
func NewAdaptivePersistenceWithWAL(
	config AdaptivePersistenceConfig,
	memBuf *MemoryBuffer,
	walConfig WALConfig,
) (*AdaptivePersistence, error) {
	if memBuf == nil {
		return nil, fmt.Errorf("memBuf cannot be nil")
	}

	// Open or create WAL (which owns PageStore)
	wal, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL: %w", err)
	}

	return newAdaptivePersistenceWithWAL(config, memBuf, wal, time.Now)
}

// newAdaptivePersistenceWithWAL creates persistence with an existing WAL (for testing).
func newAdaptivePersistenceWithWAL(
	config AdaptivePersistenceConfig,
	memBuf *MemoryBuffer,
	wal *WriteAheadLog,
	nowFunc func() time.Time,
) (*AdaptivePersistence, error) {
	return newAdaptivePersistenceWithNow(config, memBuf, nil, wal, nowFunc)
}

// newAdaptivePersistenceWithNow allows injecting a custom time function for testing.
// Either disk or wal must be non-nil. If both are provided, wal takes precedence.
func newAdaptivePersistenceWithNow(
	config AdaptivePersistenceConfig,
	memBuf *MemoryBuffer,
	disk *PageStore,
	wal *WriteAheadLog,
	nowFunc func() time.Time,
) (*AdaptivePersistence, error) {
	if memBuf == nil {
		return nil, fmt.Errorf("memBuf cannot be nil")
	}
	if disk == nil && wal == nil {
		return nil, fmt.Errorf("either disk or wal must be provided")
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
		wal:          wal,
		nowFunc:      nowFunc,
		rateMonitor:  NewRateMonitor(config.RateWindowSize),
		modeCtrl:     NewModeController(config.WriteThroughMaxRate, config.DebouncedMaxRate),
		currentMode:  PersistWriteThrough,
		pendingLines: make(map[int64]*pendingLineInfo),
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
// For search indexing support, use NotifyWriteWithMeta to provide timestamp and command flag.
func (ap *AdaptivePersistence) NotifyWrite(lineIdx int64) {
	ap.NotifyWriteWithMeta(lineIdx, time.Time{}, false)
}

// NotifyWriteWithMeta is called when a line changes, with metadata for search indexing.
// The metadata (timestamp, isCommand) is stored and passed to OnLineIndexed callback
// AFTER the line is successfully persisted to disk.
func (ap *AdaptivePersistence) NotifyWriteWithMeta(lineIdx int64, timestamp time.Time, isCommand bool) {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	if ap.stopped {
		return
	}

	// Use current time if not provided
	if timestamp.IsZero() {
		timestamp = ap.nowFunc()
	}

	ap.metrics.TotalWrites++
	writeRate := ap.updateRateAndModeLocked()

	// Store metadata for this line
	info := &pendingLineInfo{
		timestamp: timestamp,
		isCommand: isCommand,
	}

	// Handle based on mode
	ap.handleWriteLockedWithMeta(lineIdx, info, writeRate)
}

// NotifyWriteBatch records multiple line writes efficiently.
// Use this when multiple lines change at once (e.g., scroll operations).
// All lines get the same timestamp and are marked as non-commands.
func (ap *AdaptivePersistence) NotifyWriteBatch(lineIndices []int64) {
	if len(lineIndices) == 0 {
		return
	}

	ap.mu.Lock()
	defer ap.mu.Unlock()

	if ap.stopped {
		return
	}

	timestamp := ap.nowFunc()
	ap.metrics.TotalWrites += int64(len(lineIndices))
	writeRate := ap.updateRateAndModeLocked()

	// Handle based on mode - batch lines share timestamp
	for _, idx := range lineIndices {
		info := &pendingLineInfo{
			timestamp: timestamp,
			isCommand: false,
		}
		ap.handleWriteLockedWithMeta(idx, info, writeRate)
	}
}

// NotifyMetadataChange records a metadata change (scroll position, cursor).
// Metadata is batched with content and written together on flush, ensuring consistency.
func (ap *AdaptivePersistence) NotifyMetadataChange(state *ViewportState) {
	if state == nil {
		return
	}

	ap.mu.Lock()
	defer ap.mu.Unlock()

	if ap.stopped {
		return
	}

	// Store pending metadata - will be written on next flush
	ap.pendingMetadata = state
	ap.lastActivity = ap.nowFunc()
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
		oldMode := ap.currentMode
		ap.currentMode = newMode
		ap.metrics.CurrentMode = newMode
		ap.metrics.ModeChanges++

		// Log mode transitions, especially to BestEffort (high activity)
		if newMode == PersistBestEffort {
			log.Printf("[AdaptivePersistence] Mode transition: %s -> %s (rate=%.1f/s, pending=%d) - high activity detected",
				oldMode, newMode, writeRate, len(ap.pendingLines))
		} else if oldMode == PersistBestEffort {
			log.Printf("[AdaptivePersistence] Mode transition: %s -> %s (rate=%.1f/s) - activity normalized",
				oldMode, newMode, writeRate)
		}
	}

	// Warn if write rate is unusually high
	if writeRate > highWriteRateThreshold && ap.metrics.TotalWrites%100 == 0 {
		// Log every 100 writes to avoid spamming
		log.Printf("[AdaptivePersistence] High write rate: %.1f/s (mode=%s, pending=%d)",
			writeRate, ap.currentMode, len(ap.pendingLines))
	}

	return writeRate
}

// handleWriteLocked processes line writes based on current mode.
// Must be called with ap.mu held.
func (ap *AdaptivePersistence) handleWriteLockedWithMeta(lineIdx int64, info *pendingLineInfo, writeRate float64) {
	switch ap.currentMode {
	case PersistWriteThrough:
		// Immediate write - store info temporarily for the callback
		ap.pendingLines[lineIdx] = info
		ap.flushLineLocked(lineIdx)

	case PersistDebounced:
		// Add to pending and schedule debounced flush with adaptive delay
		ap.pendingLines[lineIdx] = info
		delay := ap.modeCtrl.CalculateDebounceDelay(
			writeRate,
			ap.config.DebounceMinDelay,
			ap.config.DebounceMaxDelay,
		)
		ap.scheduleFlushLocked(delay)

	case PersistBestEffort:
		// Just add to pending; idle monitor will flush
		ap.pendingLines[lineIdx] = info
	}

	// Warn if pending line count is getting high
	pendingCount := len(ap.pendingLines)
	if pendingCount > pendingLineWarningThreshold && pendingCount%100 == 0 {
		log.Printf("[AdaptivePersistence] Warning: %d lines pending flush (mode=%s, rate=%.1f/s)",
			pendingCount, ap.currentMode, writeRate)
	}
}

// Flush forces immediate flush of all pending writes.
func (ap *AdaptivePersistence) Flush() error {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	ap.cancelFlushTimerLocked()
	return ap.flushPendingLocked()
}

// Close flushes pending writes, stops the idle monitor, and closes storage.
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

	// Explicitly sync WAL before releasing lock. This ensures data reaches
	// disk even if the process is killed before wal.Close() can checkpoint.
	if ap.wal != nil {
		if err := ap.wal.SyncWAL(); err != nil && flushErr == nil {
			flushErr = err
		}
	}

	ap.mu.Unlock()

	// Stop idle monitor (outside lock to avoid deadlock)
	ap.stopIdleMonitor()

	// Close storage (WAL or direct PageStore)
	var storageErr error
	if ap.wal != nil {
		storageErr = ap.wal.Close()
	} else if ap.disk != nil {
		storageErr = ap.disk.Close()
	}

	// Return first error
	if flushErr != nil {
		return flushErr
	}
	return storageErr
}

// CurrentMode returns the current persistence mode.
func (ap *AdaptivePersistence) CurrentMode() PersistMode {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	return ap.currentMode
}

// PageStore returns the underlying PageStore for history access.
// Returns the WAL's PageStore if using WAL, otherwise the direct PageStore.
func (ap *AdaptivePersistence) PageStore() *PageStore {
	if ap.wal != nil {
		return ap.wal.pageStore
	}
	return ap.disk
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

// flushPendingLocked writes all pending lines and metadata to disk.
// Content and metadata are written together to ensure consistency.
// Must be called with ap.mu held.
func (ap *AdaptivePersistence) flushPendingLocked() error {
	if len(ap.pendingLines) == 0 && ap.pendingMetadata == nil {
		return nil
	}

	lineCount := len(ap.pendingLines)
	startTime := ap.nowFunc()

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

	// Clear pending lines
	ap.pendingLines = make(map[int64]*pendingLineInfo)

	// Write metadata AFTER content so they stay consistent
	// (metadata references content that's now on disk)
	if ap.pendingMetadata != nil && ap.wal != nil {
		if err := ap.wal.WriteMetadata(ap.pendingMetadata); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to write metadata: %w", err)
			}
		}
		ap.pendingMetadata = nil
	}

	// Sync WAL to disk so data survives process crash.
	// Without this, written data sits in the OS page cache and may be lost
	// on SIGKILL or machine shutdown.
	if ap.wal != nil {
		if err := ap.wal.SyncWAL(); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to sync WAL after flush: %w", err)
			}
		}
	}

	// Monitor flush performance
	elapsed := ap.nowFunc().Sub(startTime)
	if elapsed > flushSlowThreshold {
		log.Printf("[AdaptivePersistence] Slow flush: %d lines in %v (%.1f lines/ms)",
			lineCount, elapsed, float64(lineCount)/float64(elapsed.Milliseconds()))
	}

	return firstErr
}

// flushLineLocked writes a single line to disk (via WAL or direct PageStore).
// After successful write, calls OnLineIndexed callback for search indexing.
// Must be called with ap.mu held.
func (ap *AdaptivePersistence) flushLineLocked(lineIdx int64) error {
	// Get pending info (may be nil for legacy callers)
	info := ap.pendingLines[lineIdx]

	line := ap.memBuf.GetLine(lineIdx)
	if line == nil {
		// Line was evicted from memory - clear dirty and skip
		ap.memBuf.ClearDirty(lineIdx)
		delete(ap.pendingLines, lineIdx)
		return nil
	}

	// Clone the line before encoding to prevent a data race.
	// GetLine returns a pointer to the actual LogicalLine in the ring buffer.
	// In Debounced/BestEffort modes, this flush runs on a background goroutine
	// while the main goroutine may be writing to the same line's Cells.
	lineCopy := line.Clone()

	// Use WAL if available, otherwise direct PageStore
	var err error
	if ap.wal != nil {
		err = ap.wal.Append(lineIdx, lineCopy, ap.nowFunc())
	} else {
		err = ap.disk.AppendLine(lineCopy)
	}

	if err != nil {
		// Log error but don't clear dirty (retry on next flush)
		return fmt.Errorf("failed to write line %d: %w", lineIdx, err)
	}

	ap.memBuf.ClearDirty(lineIdx)
	delete(ap.pendingLines, lineIdx)
	ap.metrics.LinesWritten++

	// Call search index callback AFTER successful write
	// This ensures search index only has entries for persisted content
	if ap.OnLineIndexed != nil && info != nil {
		ap.OnLineIndexed(lineIdx, lineCopy, info.timestamp, info.isCommand)
	}

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

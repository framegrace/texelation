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
	"sync"
	"time"

	"github.com/framegrace/texelation/internal/debuglog"
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

// LineStore is the storage backend that AdaptivePersistence reads lines from.
// It is satisfied by *MemoryBuffer and by sparseLineStoreAdapter.
type LineStore interface {
	GetLine(globalIdx int64) *LogicalLine
	ClearDirty(globalIdx int64)
	SetPreEvictCallback(func([]EvictedLine))
}

// opKind identifies the kind of pending persistence operation.
type opKind uint8

const (
	opWrite  opKind = 1
	opDelete opKind = 2
)

// pendingOp is one entry in the FIFO ops list maintained by AdaptivePersistence.
// For writes: lo == hi == lineIdx.
// For deletes (added by Task 6): lo..hi is the deleted range.
// dropped is set when a later write for the same lineIdx supersedes this op.
type pendingOp struct {
	kind    opKind
	lo, hi  int64
	ts      time.Time
	isCmd   bool // writes only
	dropped bool // superseded by a later write or swallowed by a delete
}

// AdaptivePersistence manages disk writes with dynamic rate adjustment.
type AdaptivePersistence struct {
	config  AdaptivePersistenceConfig
	memBuf  LineStore
	disk    *PageStore       // Direct PageStore fallback (when no WAL is supplied)
	wal     *WriteAheadLog   // WAL-based persistence (preferred)
	nowFunc func() time.Time // For testing; defaults to time.Now

	// Components
	rateMonitor *RateMonitor
	modeCtrl    *ModeController

	// State
	currentMode     PersistMode
	pendingOps      []pendingOp      // FIFO list of pending operations
	pendingSet      map[int64]int    // lineIdx -> index in pendingOps for most recent write op
	pendingMetadata *MainScreenState // Metadata awaiting flush (written with content)
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

	// flushIOMu serializes the I/O phase of flushPendingLocked across
	// concurrent callers (idle monitor vs explicit Close/Flush). Without
	// this, two flushes can run their I/O loops in parallel: the second
	// Close can proceed to wal.Close() while the first's idle-monitor
	// flush is still writing entries, losing the tail of the first's
	// snapshot when wal.Close truncates.
	flushIOMu sync.Mutex
}

// NewAdaptivePersistence creates a new adaptive persistence layer.
//
// Parameters:
//   - config: Configuration for rate thresholds and timing
//   - memBuf: LineStore to read dirty lines from (e.g. *MemoryBuffer or sparseLineStoreAdapter)
//   - disk: PageStore to write lines to (passed in for testability)
//
// The background idle monitor is started automatically.
// Call Close() when done to flush pending writes and stop the monitor.
//
// Deprecated: Use NewAdaptivePersistenceWithWAL for crash recovery support.
func NewAdaptivePersistence(
	config AdaptivePersistenceConfig,
	memBuf LineStore,
	disk *PageStore,
) (*AdaptivePersistence, error) {
	return newAdaptivePersistenceWithNow(config, memBuf, disk, nil, time.Now)
}

// NewAdaptivePersistenceWithWAL creates an adaptive persistence layer with WAL support.
//
// Parameters:
//   - config: Configuration for rate thresholds and timing
//   - memBuf: LineStore to read dirty lines from (e.g. *MemoryBuffer or sparseLineStoreAdapter)
//   - walConfig: Configuration for the Write-Ahead Log
//
// The WAL provides crash recovery by journaling writes before committing to PageStore.
// On startup, uncommitted entries are recovered automatically.
func NewAdaptivePersistenceWithWAL(
	config AdaptivePersistenceConfig,
	memBuf LineStore,
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
	memBuf LineStore,
	wal *WriteAheadLog,
	nowFunc func() time.Time,
) (*AdaptivePersistence, error) {
	return newAdaptivePersistenceWithNow(config, memBuf, nil, wal, nowFunc)
}

// newAdaptivePersistenceWithNow allows injecting a custom time function for testing.
// Either disk or wal must be non-nil. If both are provided, wal takes precedence.
func newAdaptivePersistenceWithNow(
	config AdaptivePersistenceConfig,
	memBuf LineStore,
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

	rm := NewRateMonitor(config.RateWindowSize)
	// Establish rate baseline so the first CalculateRate sample (at write 64)
	// returns a real rate instead of just initializing.
	rm.CalculateRate(nowFunc())

	ap := &AdaptivePersistence{
		config:      config,
		memBuf:      memBuf,
		disk:        disk,
		wal:         wal,
		nowFunc:     nowFunc,
		rateMonitor: rm,
		modeCtrl:    NewModeController(config.WriteThroughMaxRate, config.DebouncedMaxRate),
		currentMode: PersistWriteThrough,
		pendingOps:  nil,
		pendingSet:  make(map[int64]int),
		lastActivity: nowFunc(),
		stopCh:      make(chan struct{}),
		stopped:     false,
		metrics: PersistenceMetrics{
			CurrentMode: PersistWriteThrough,
		},
	}

	// Install pre-evict callback so memBuf flushes dirty lines before
	// they leave the ring buffer. Without this, dirty lines that piled
	// up in pendingOps (e.g. during a BestEffort burst) get silently
	// dropped when memBuf evicts to make room — flushPendingLocked
	// later sees mb.GetLine(idx) == nil and skips them, producing huge
	// gaps in pageStore.
	memBuf.SetPreEvictCallback(ap.flushEvictedLines)

	// Start idle monitor
	ap.startIdleMonitor()

	return ap, nil
}

// flushEvictedLines is invoked by MemoryBuffer.evictLocked just before
// dirty lines disappear from the ring buffer. The lines arrive as
// independent clones so we can persist them without re-reading from
// memBuf (which would deadlock since memBuf is locked during eviction).
//
// We must NOT acquire ap.mu here because the persistence flush path
// may already hold it indirectly via NotifyWrite → memBuf.Write paths.
// Instead we write directly to the WAL/disk and mark pendingOps entries
// dropped via a brief lock acquisition at the end.
func (ap *AdaptivePersistence) flushEvictedLines(lines []EvictedLine) {
	if len(lines) == 0 {
		return
	}
	for _, e := range lines {
		var err error
		if ap.wal != nil {
			err = ap.wal.Append(e.GlobalIdx, e.Line, ap.nowFunc())
		} else if ap.disk != nil {
			err = ap.diskWriteOrUpdate(e.GlobalIdx, e.Line, ap.nowFunc())
		}
		if err != nil {
			ap.metrics.FailedWrites++
			continue
		}
		ap.metrics.LinesWritten++
	}
	// Mark any pending write ops for evicted lines as dropped so a later
	// flush doesn't try to fetch them from memBuf (where they no longer exist).
	ap.mu.Lock()
	for _, e := range lines {
		if idx, ok := ap.pendingSet[e.GlobalIdx]; ok {
			ap.pendingOps[idx].dropped = true
			delete(ap.pendingSet, e.GlobalIdx)
		}
	}
	ap.mu.Unlock()
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

	ap.metrics.TotalWrites++
	writeRate := ap.updateRateAndModeLocked()

	// Use lastActivity as timestamp when not provided.
	// lastActivity is set every 64 writes by updateRateAndModeLocked,
	// which is precise enough for search indexing timestamps.
	if timestamp.IsZero() {
		timestamp = ap.lastActivity
	}

	// Handle based on mode
	ap.handleWriteLockedWithMeta(lineIdx, timestamp, isCommand, writeRate)
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
		ap.handleWriteLockedWithMeta(idx, timestamp, false, writeRate)
	}
}

// NotifyMetadataChange records a metadata change (write position, cursor).
// Metadata is batched with content and written together on flush, ensuring consistency.
func (ap *AdaptivePersistence) NotifyMetadataChange(state *MainScreenState) {
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

// updateRateAndModeLocked records a write and adjusts mode based on rate.
// Must be called with ap.mu held.
func (ap *AdaptivePersistence) updateRateAndModeLocked() float64 {
	// Record write (just increments a counter — O(1), no time.Now needed)
	ap.rateMonitor.RecordWrite()

	// Only sample time and calculate rate every 64 writes.
	// time.Now() is expensive (~7% CPU in profiles) and rate monitoring
	// doesn't need per-character precision.
	if ap.metrics.TotalWrites&63 != 0 {
		return ap.metrics.CurrentWriteRate
	}

	now := ap.nowFunc()
	ap.lastActivity = now

	writeRate := ap.rateMonitor.CalculateRate(now)
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
			debuglog.Printf("[AdaptivePersistence] Mode transition: %s -> %s (rate=%.1f/s, pending=%d) - high activity detected",
				oldMode, newMode, writeRate, len(ap.pendingSet))
		} else if oldMode == PersistBestEffort {
			debuglog.Printf("[AdaptivePersistence] Mode transition: %s -> %s (rate=%.1f/s) - activity normalized",
				oldMode, newMode, writeRate)
		}
	}

	// Warn if write rate is unusually high
	if writeRate > highWriteRateThreshold && ap.metrics.TotalWrites%100 == 0 {
		debuglog.Printf("[AdaptivePersistence] High write rate: %.1f/s (mode=%s, pending=%d)",
			writeRate, ap.currentMode, len(ap.pendingSet))
	}

	return writeRate
}

// handleWriteLockedWithMeta processes line writes based on current mode.
// Must be called with ap.mu held.
func (ap *AdaptivePersistence) handleWriteLockedWithMeta(lineIdx int64, ts time.Time, isCmd bool, writeRate float64) {
	// Supersede any earlier pending write for this line: mark it dropped so
	// the FIFO flush skips it. The new op takes the tail position (call order).
	if prev, ok := ap.pendingSet[lineIdx]; ok {
		ap.pendingOps[prev].dropped = true
	}
	newIdx := len(ap.pendingOps)
	ap.pendingOps = append(ap.pendingOps, pendingOp{
		kind:  opWrite,
		lo:    lineIdx,
		hi:    lineIdx,
		ts:    ts,
		isCmd: isCmd,
	})
	ap.pendingSet[lineIdx] = newIdx

	switch ap.currentMode {
	case PersistWriteThrough:
		// Immediate write: flush the single op we just enqueued.
		ap.flushLineLocked(lineIdx)

	case PersistDebounced:
		// Schedule debounced flush with adaptive delay.
		delay := ap.modeCtrl.CalculateDebounceDelay(
			writeRate,
			ap.config.DebounceMinDelay,
			ap.config.DebounceMaxDelay,
		)
		ap.scheduleFlushLocked(delay)

	case PersistBestEffort:
		// Just leave in pending; idle monitor will flush.
	}

	// Warn if pending op count is getting high.
	pendingCount := len(ap.pendingOps)
	if pendingCount > pendingLineWarningThreshold && pendingCount%100 == 0 {
		debuglog.Printf("[AdaptivePersistence] Warning: %d ops pending flush (mode=%s, rate=%.1f/s)",
			pendingCount, ap.currentMode, writeRate)
	}
}

// NotifyClearRange records a tombstone for the closed range [lo, hi]. Any
// queued writes for line indices in that range are marked dropped and removed
// from pendingSet so they are not flushed to disk. An opDelete op is then
// appended to the FIFO ops queue.
//
// Flush semantics match the current mode:
//   - WriteThrough: flush immediately (same as NotifyWrite in WriteThrough).
//   - Debounced:    arm the debounce timer; ordering preserved by FIFO queue.
//   - BestEffort:   queue only; flush on idle or explicit Flush / Close.
//
// A lo > hi or negative value is a no-op.
func (ap *AdaptivePersistence) NotifyClearRange(lo, hi int64) {
	if lo < 0 || hi < lo {
		return
	}

	ap.mu.Lock()
	defer ap.mu.Unlock()

	if ap.stopped {
		return
	}

	// Sweep pendingSet: drop any queued write op whose lineIdx falls in [lo, hi].
	for gi, opIdx := range ap.pendingSet {
		if gi >= lo && gi <= hi {
			ap.pendingOps[opIdx].dropped = true
			delete(ap.pendingSet, gi)
		}
	}

	// Append the delete op in FIFO order.
	ap.pendingOps = append(ap.pendingOps, pendingOp{
		kind: opDelete,
		lo:   lo,
		hi:   hi,
		ts:   ap.nowFunc(),
	})

	// Flush or schedule based on current mode.
	switch ap.currentMode {
	case PersistWriteThrough:
		ap.flushPendingLocked()

	case PersistDebounced:
		// Use minimum delay for a single delete — responsiveness matters more
		// than batching here since deletes are rare ops.
		ap.scheduleFlushLocked(ap.config.DebounceMinDelay)

	case PersistBestEffort:
		// Leave in pending; idle monitor or explicit Flush will drain it.
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

// PendingCount returns the number of unique lines awaiting flush.
func (ap *AdaptivePersistence) PendingCount() int {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	return len(ap.pendingSet)
}

// String returns debug information.
func (ap *AdaptivePersistence) String() string {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	return fmt.Sprintf("AdaptivePersistence{mode=%s, rate=%.2f/s, pending=%d, writes=%d, flushes=%d}",
		ap.currentMode, ap.metrics.CurrentWriteRate, len(ap.pendingSet),
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
// It releases ap.mu during I/O to avoid blocking NotifyWrite on the parse
// thread. Must be called with ap.mu held; ap.mu is re-held on return.
func (ap *AdaptivePersistence) flushPendingLocked() error {
	if len(ap.pendingOps) == 0 && ap.pendingMetadata == nil {
		return nil
	}

	lineCount := len(ap.pendingOps)
	startTime := ap.nowFunc()

	ap.metrics.TotalFlushes++

	// --- Collect phase (locked): clone lines and snapshot state ---
	// Walk the FIFO ops list in enqueue order, skipping dropped entries.
	// Both write and delete ops are collected in order to preserve FIFO
	// semantics between writes and deletes.
	type ioOp struct {
		kind    opKind
		lineIdx int64 // for writes
		line    *LogicalLine
		ts      time.Time
		isCmd   bool
		lo, hi  int64 // for deletes
	}
	ioOps := make([]ioOp, 0, len(ap.pendingOps))

	for _, op := range ap.pendingOps {
		if op.dropped {
			continue
		}
		switch op.kind {
		case opWrite:
			line := ap.memBuf.GetLine(op.lo)
			if line == nil {
				ap.memBuf.ClearDirty(op.lo)
				continue
			}
			ioOps = append(ioOps, ioOp{
				kind:    opWrite,
				lineIdx: op.lo,
				line:    line.Clone(),
				ts:      op.ts,
				isCmd:   op.isCmd,
			})
			ap.memBuf.ClearDirty(op.lo)
		case opDelete:
			ioOps = append(ioOps, ioOp{
				kind: opDelete,
				lo:   op.lo,
				hi:   op.hi,
				ts:   op.ts,
			})
		}
	}

	pendingMeta := ap.pendingMetadata
	ap.pendingMetadata = nil
	ap.pendingOps = nil
	ap.pendingSet = make(map[int64]int)

	// --- I/O phase (unlocked): write to WAL without blocking the parser ---
	// Hold flushIOMu to serialize with any other concurrent flush. Without
	// this, Close can run its own flushPendingLocked while the idle monitor
	// is still in its I/O loop, then proceed to wal.Close() which
	// truncates the WAL and drops the idle monitor's remaining entries.
	ap.mu.Unlock()
	ap.flushIOMu.Lock()
	defer func() {
		ap.flushIOMu.Unlock()
	}()

	var firstErr error
	for _, op := range ioOps {
		switch op.kind {
		case opWrite:
			var err error
			if ap.wal != nil {
				err = ap.wal.Append(op.lineIdx, op.line, ap.nowFunc())
			} else {
				err = ap.diskWriteOrUpdate(op.lineIdx, op.line, ap.nowFunc())
			}
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("failed to write line %d: %w", op.lineIdx, err)
				}
				ap.metrics.FailedWrites++
				continue
			}
			ap.metrics.LinesWritten++
			if ap.OnLineIndexed != nil {
				ap.OnLineIndexed(op.lineIdx, op.line, op.ts, op.isCmd)
			}
		case opDelete:
			if ap.wal != nil {
				if err := ap.wal.DeleteRange(op.lo, op.hi); err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("failed to delete range [%d, %d]: %w", op.lo, op.hi, err)
					}
				}
			}
			// No disk-only path for deletes: the WAL is always required for
			// range tombstones. If ap.wal == nil (legacy no-WAL mode), the
			// delete is silently ignored — the no-WAL path does not support
			// crash-safe tombstones.
		}
	}

	if pendingMeta != nil && ap.wal != nil {
		// Validate metadata against what's actually on disk.
		// CursorGlobalIdx must not exceed the WAL's known line count,
		// otherwise recovery will have metadata pointing to non-existent lines.
		walLineCount := ap.wal.NextGlobalIdx()
		if pendingMeta.WriteTop > walLineCount {
			debuglog.Printf("[AdaptivePersistence] Clamping metadata WriteTop %d → %d (WAL lineCount)",
				pendingMeta.WriteTop, walLineCount)
			pendingMeta.WriteTop = walLineCount
		}
		if pendingMeta.CursorGlobalIdx > walLineCount && walLineCount > 0 {
			pendingMeta.CursorGlobalIdx = walLineCount
			debuglog.Printf("[AdaptivePersistence] Clamped CursorGlobalIdx to %d (would exceed WAL)",
				pendingMeta.CursorGlobalIdx)
		}
		if err := ap.wal.WriteMainScreenState(pendingMeta); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to write metadata: %w", err)
			}
		}
	}

	if ap.wal != nil {
		if err := ap.wal.SyncWAL(); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to sync WAL after flush: %w", err)
			}
		}
	}

	// --- Re-acquire lock ---
	ap.mu.Lock()

	elapsed := ap.nowFunc().Sub(startTime)
	if elapsed > flushSlowThreshold {
		debuglog.Printf("[AdaptivePersistence] Slow flush: %d lines in %v (%.1f lines/ms)",
			lineCount, elapsed, float64(lineCount)/float64(elapsed.Milliseconds()))
	}

	return firstErr
}

// flushLineLocked writes a single line to disk (via WAL or direct PageStore).
// This is the WriteThrough fast path: the op was just appended to pendingOps
// at position pendingSet[lineIdx], so we read the op's metadata from there,
// write to disk, then mark the op dropped and remove it from pendingSet.
// Must be called with ap.mu held.
func (ap *AdaptivePersistence) flushLineLocked(lineIdx int64) error {
	// Get the pending op for this line (set by handleWriteLockedWithMeta just before).
	opIdx, ok := ap.pendingSet[lineIdx]
	var ts time.Time
	var isCmd bool
	if ok {
		op := &ap.pendingOps[opIdx]
		ts = op.ts
		isCmd = op.isCmd
		// Mark as dropped so flushPendingLocked won't re-write it.
		op.dropped = true
		delete(ap.pendingSet, lineIdx)
	}

	line := ap.memBuf.GetLine(lineIdx)
	if line == nil {
		// Line was evicted from memory - clear dirty and skip
		ap.memBuf.ClearDirty(lineIdx)
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
		err = ap.diskWriteOrUpdate(lineIdx, lineCopy, ap.nowFunc())
	}

	if err != nil {
		// Log error but don't clear dirty (retry on next flush)
		return fmt.Errorf("failed to write line %d: %w", lineIdx, err)
	}

	ap.memBuf.ClearDirty(lineIdx)
	ap.metrics.LinesWritten++

	// Call search index callback AFTER successful write
	// This ensures search index only has entries for persisted content
	if ap.OnLineIndexed != nil && ok {
		ap.OnLineIndexed(lineIdx, lineCopy, ts, isCmd)
	}

	return nil
}

// diskWriteOrUpdate writes a line to the disk PageStore (no-WAL path).
// If the line already exists at lineIdx, it is updated via UpdateLine.
// If lineIdx points to a gap (line count exceeds lineIdx but line is absent),
// the write is silently skipped — the line was never stored and cannot be
// back-filled without WAL support.
func (ap *AdaptivePersistence) diskWriteOrUpdate(lineIdx int64, line *LogicalLine, ts time.Time) error {
	if lineIdx < ap.disk.LineCount() {
		existing, err := ap.disk.ReadLine(lineIdx)
		if err != nil {
			return err
		}
		if existing == nil {
			// Gap — line was never stored; skip silently.
			return nil
		}
		return ap.disk.UpdateLine(lineIdx, line, ts)
	}
	return ap.disk.AppendLineWithGlobalIdx(lineIdx, line, ts)
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

	if ap.stopped || len(ap.pendingOps) == 0 {
		return
	}

	idleDuration := ap.nowFunc().Sub(ap.lastActivity)
	if idleDuration >= ap.config.IdleThreshold {
		ap.flushPendingLocked()
	}
}

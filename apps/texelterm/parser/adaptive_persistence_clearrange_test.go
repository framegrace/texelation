// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/adaptive_persistence_clearrange_test.go
// Summary: Tests for AdaptivePersistence.NotifyClearRange.

package parser

import (
	"testing"
	"time"
)

// walOp represents an operation recorded in the WAL for assertion purposes.
type walOp struct {
	kind string // "W" for write, "D" for delete
	lo   int64
	hi   int64 // meaningful only for deletes
}

// walOps reads all WAL entries in file order and returns them as walOp values.
// Writes carry lo==hi==GlobalLineIdx. Deletes carry lo==GlobalLineIdx (the
// range start) and hi==DeleteHi (the range end, inclusive).
func walOps(t testing.TB, wal *WriteAheadLog) []walOp {
	t.Helper()
	entries, err := wal.readWALEntries()
	if err != nil {
		t.Fatalf("readWALEntries: %v", err)
	}
	ops := make([]walOp, 0, len(entries))
	for _, e := range entries {
		switch e.Type {
		case EntryTypeLineWrite, EntryTypeLineModify:
			ops = append(ops, walOp{kind: "W", lo: int64(e.GlobalLineIdx), hi: int64(e.GlobalLineIdx)})
		case EntryTypeLineDelete:
			ops = append(ops, walOp{kind: "D", lo: int64(e.GlobalLineIdx), hi: e.DeleteHi})
		// Skip CHECKPOINT / METADATA / MAIN_SCREEN_STATE entries.
		}
	}
	return ops
}

// equalWalOps returns true if two slices have the same contents.
func equalWalOps(a, b []walOp) bool {
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

// newTestAdaptivePersistenceWriteThrough creates an AdaptivePersistence locked
// into WriteThrough mode. Writes flush immediately to WAL so they are
// already committed before a subsequent NotifyClearRange call.
func newTestAdaptivePersistenceWriteThrough(t testing.TB) (*AdaptivePersistence, *WriteAheadLog) {
	t.Helper()
	tmpDir := t.TempDir()
	walConfig := DefaultWALConfig(tmpDir, "test-writethrough")
	walConfig.CheckpointInterval = 0

	wal, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}

	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	for i := int64(0); i < 10; i++ {
		mb.EnsureLine(i)
		mb.GetLine(i).Cells = []Cell{{Rune: 'X'}}
	}

	config := DefaultAdaptivePersistenceConfig()
	// Force WriteThrough mode: rate thresholds set high enough that the
	// initial rate (0) stays below WriteThroughMaxRate.
	config.WriteThroughMaxRate = 1000
	config.DebouncedMaxRate = 2000
	config.IdleThreshold = 1 * time.Hour

	ap, err := newAdaptivePersistenceWithWAL(config, mb, wal, time.Now)
	if err != nil {
		t.Fatalf("newAdaptivePersistenceWithWAL: %v", err)
	}
	t.Cleanup(func() { ap.Close() })

	// Explicitly set WriteThrough mode so the test is mode-stable.
	ap.mu.Lock()
	ap.currentMode = PersistWriteThrough
	ap.mu.Unlock()

	return ap, wal
}

// newTestAdaptivePersistenceDebounced creates an AdaptivePersistence locked
// into Debounced mode with a very long debounce delay, so writes accumulate
// in the pending queue and are only flushed on an explicit Flush() call.
func newTestAdaptivePersistenceDebounced(t testing.TB) (*AdaptivePersistence, *WriteAheadLog) {
	t.Helper()
	tmpDir := t.TempDir()
	walConfig := DefaultWALConfig(tmpDir, "test-debounced")
	walConfig.CheckpointInterval = 0

	wal, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}

	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	for i := int64(0); i < 10; i++ {
		mb.EnsureLine(i)
		mb.GetLine(i).Cells = []Cell{{Rune: 'X'}}
	}

	config := DefaultAdaptivePersistenceConfig()
	// Force Debounced mode: WriteThroughMaxRate=0 so any positive rate enters
	// Debounced, but DebouncedMaxRate is very high so it never reaches BestEffort.
	config.WriteThroughMaxRate = 0
	config.DebouncedMaxRate = 1e9
	// Long delays so the debounce timer never fires during the test.
	config.DebounceMinDelay = 1 * time.Hour
	config.DebounceMaxDelay = 1 * time.Hour
	config.IdleThreshold = 1 * time.Hour

	ap, err := newAdaptivePersistenceWithWAL(config, mb, wal, time.Now)
	if err != nil {
		t.Fatalf("newAdaptivePersistenceWithWAL: %v", err)
	}
	t.Cleanup(func() { ap.Close() })

	// Explicitly set Debounced mode so writes queue without flushing.
	ap.mu.Lock()
	ap.currentMode = PersistDebounced
	ap.mu.Unlock()

	return ap, wal
}

// TestAdaptivePersistence_NotifyClearRangeWriteThrough verifies that in
// WriteThrough mode, writes before NotifyClearRange are already flushed to
// WAL (immediate flush) and appear in order before the delete tombstone.
// A write after the clear also appears in order.
//
// Call sequence: W5, W6, ClearRange(0,10), W6
// Since WriteThrough flushes immediately, W5 and W6 hit WAL before the clear
// sweeps pendingSet (which is already empty). Expected WAL: W5, W6, D0-10, W6.
func TestAdaptivePersistence_NotifyClearRangeWriteThrough(t *testing.T) {
	ap, wal := newTestAdaptivePersistenceWriteThrough(t)

	ap.NotifyWrite(5)
	ap.NotifyWrite(6)
	ap.NotifyClearRange(0, 10)
	ap.NotifyWrite(6) // new write post-clear should survive

	// WriteThrough flushes each op immediately; no explicit Flush needed.
	// But call Flush to ensure any pending ops (the last W6 and the delete)
	// are committed before we read the WAL.
	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	got := walOps(t, wal)
	want := []walOp{
		{kind: "W", lo: 5, hi: 5},
		{kind: "W", lo: 6, hi: 6},
		{kind: "D", lo: 0, hi: 10},
		{kind: "W", lo: 6, hi: 6},
	}
	if !equalWalOps(got, want) {
		t.Errorf("WAL ops = %v, want %v", got, want)
	}
}

// TestAdaptivePersistence_NotifyClearRangeSweepsQueuedWrite verifies that in
// a queued mode (Debounced), a write inside the cleared range that has not yet
// been flushed is marked dropped and does not appear in the WAL.
//
// Call sequence: W5, ClearRange(0,10), Flush()
// W5 is queued but not yet flushed; ClearRange sweeps it. Expected WAL: D0-10.
func TestAdaptivePersistence_NotifyClearRangeSweepsQueuedWrite(t *testing.T) {
	ap, wal := newTestAdaptivePersistenceDebounced(t)

	ap.NotifyWrite(5)
	ap.NotifyClearRange(0, 10)

	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	got := walOps(t, wal)
	want := []walOp{
		{kind: "D", lo: 0, hi: 10},
	}
	if !equalWalOps(got, want) {
		t.Errorf("WAL ops = %v, want %v", got, want)
	}
}

// TestAdaptivePersistence_NotifyClearRangePostWriteSurvives verifies that a
// write issued AFTER a NotifyClearRange is not swept and does appear in the WAL.
//
// Call sequence: W5, ClearRange(0,10), W5, Flush()
// The first W5 is swept; the second W5 (post-clear) must survive.
// Expected WAL: D0-10, W5.
func TestAdaptivePersistence_NotifyClearRangePostWriteSurvives(t *testing.T) {
	ap, wal := newTestAdaptivePersistenceDebounced(t)

	ap.NotifyWrite(5)
	ap.NotifyClearRange(0, 10)
	ap.NotifyWrite(5) // new write for same line post-clear; must survive

	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	got := walOps(t, wal)
	want := []walOp{
		{kind: "D", lo: 0, hi: 10},
		{kind: "W", lo: 5, hi: 5},
	}
	if !equalWalOps(got, want) {
		t.Errorf("WAL ops = %v, want %v", got, want)
	}
}

// TestAdaptivePersistence_NotifyClearRangeOutsideRangeUnaffected verifies that
// writes outside [lo, hi] are not swept by NotifyClearRange.
//
// Call sequence: W3, W15, ClearRange(5,10), Flush()
// W3 and W15 are outside [5,10] and must appear in the WAL.
// Expected WAL: W3, W15, D5-10.
func TestAdaptivePersistence_NotifyClearRangeOutsideRangeUnaffected(t *testing.T) {
	ap, wal := newTestAdaptivePersistenceDebounced(t)

	// Ensure lines 3 and 15 exist in memory buffer.
	mb := ap.memBuf.(*MemoryBuffer)
	mb.EnsureLine(15)
	mb.GetLine(15).Cells = []Cell{{Rune: 'Y'}}

	ap.NotifyWrite(3)
	ap.NotifyWrite(15)
	ap.NotifyClearRange(5, 10)

	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	got := walOps(t, wal)
	want := []walOp{
		{kind: "W", lo: 3, hi: 3},
		{kind: "W", lo: 15, hi: 15},
		{kind: "D", lo: 5, hi: 10},
	}
	if !equalWalOps(got, want) {
		t.Errorf("WAL ops = %v, want %v", got, want)
	}
}

// TestAdaptivePersistence_NotifyClearRangeBestEffortQueues verifies that in
// BestEffort mode, NotifyClearRange does not flush immediately — the delete op
// stays pending until an explicit Flush().
func TestAdaptivePersistence_NotifyClearRangeBestEffortQueues(t *testing.T) {
	ap, wal := newTestAdaptivePersistenceBestEffort(t)

	ap.NotifyWrite(5)
	ap.NotifyClearRange(0, 10)

	// Before flush: WAL must be empty (nothing written yet).
	beforeFlush := walOps(t, wal)
	if len(beforeFlush) != 0 {
		t.Errorf("before Flush: WAL ops = %v, want empty", beforeFlush)
	}

	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	got := walOps(t, wal)
	want := []walOp{
		{kind: "D", lo: 0, hi: 10},
	}
	if !equalWalOps(got, want) {
		t.Errorf("WAL ops after Flush = %v, want %v", got, want)
	}
}

// TestAdaptivePersistence_NotifyClearRangeSingleLine verifies that a single-line
// range (lo == hi) deletes exactly that line and leaves adjacent lines intact.
//
// Call sequence: W4, W5, W6, ClearRange(5,5), Flush()
// W4 and W6 are outside [5,5] and must survive in the WAL.
// W5 is swept by the clear; only the Delete tombstone appears for line 5.
// Expected WAL: W4, W6, D5-5.
func TestAdaptivePersistence_NotifyClearRangeSingleLine(t *testing.T) {
	ap, wal := newTestAdaptivePersistenceBestEffort(t)

	ap.NotifyWrite(4)
	ap.NotifyWrite(5)
	ap.NotifyWrite(6)
	ap.NotifyClearRange(5, 5)

	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	got := walOps(t, wal)
	want := []walOp{
		{kind: "W", lo: 4, hi: 4},
		{kind: "W", lo: 6, hi: 6},
		{kind: "D", lo: 5, hi: 5},
	}
	if !equalWalOps(got, want) {
		t.Errorf("WAL ops = %v, want %v", got, want)
	}
}

// TestAdaptivePersistence_NotifyClearRangeInvalidRangeIsNoop verifies that
// NotifyClearRange is a no-op when given an invalid range (negative lo or hi < lo).
// No delete op must appear in the WAL after Flush.
func TestAdaptivePersistence_NotifyClearRangeInvalidRangeIsNoop(t *testing.T) {
	cases := []struct {
		name string
		lo   int64
		hi   int64
	}{
		{"negative lo", -1, 5},
		{"hi less than lo", 10, 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ap, wal := newTestAdaptivePersistenceBestEffort(t)

			// Seed a write so there is something to not-sweep.
			ap.NotifyWrite(3)
			ap.NotifyClearRange(tc.lo, tc.hi)

			if err := ap.Flush(); err != nil {
				t.Fatalf("Flush: %v", err)
			}

			got := walOps(t, wal)
			// The write must appear; no Delete entry must be present.
			for _, op := range got {
				if op.kind == "D" {
					t.Errorf("case %q: unexpected Delete op in WAL: %v (all ops: %v)", tc.name, op, got)
				}
			}
			if len(got) != 1 || got[0].kind != "W" || got[0].lo != 3 {
				t.Errorf("case %q: WAL ops = %v, want [W(3,3)]", tc.name, got)
			}
		})
	}
}

// TestAdaptivePersistence_NotifyClearRangeMultipleOverlapping verifies that two
// overlapping clear ranges both produce Delete tombstones in FIFO order, and that
// no write ops appear (all three writes are swept by one clear or the other).
//
// Call sequence: W5, W6, W7, ClearRange(5,6), ClearRange(6,7), Flush()
// Both clears overlap on line 6. W5 is swept by first clear, W6 by first clear,
// W7 by second clear. No writes survive. Expected WAL: D5-6, D6-7.
func TestAdaptivePersistence_NotifyClearRangeMultipleOverlapping(t *testing.T) {
	ap, wal := newTestAdaptivePersistenceBestEffort(t)

	ap.NotifyWrite(5)
	ap.NotifyWrite(6)
	ap.NotifyWrite(7)
	ap.NotifyClearRange(5, 6)
	ap.NotifyClearRange(6, 7)

	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	got := walOps(t, wal)
	want := []walOp{
		{kind: "D", lo: 5, hi: 6},
		{kind: "D", lo: 6, hi: 7},
	}
	if !equalWalOps(got, want) {
		t.Errorf("WAL ops = %v, want %v", got, want)
	}
}

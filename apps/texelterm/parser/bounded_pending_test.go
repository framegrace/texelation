// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/bounded_pending_test.go
// Summary: Regression tests for AdaptivePersistenceConfig.MaxPendingLines.
// Protects the crash-loss bound: after a burst larger than the cap,
// the WAL must contain at least (burst - cap) lines BEFORE close is
// called — proving in-flight cap-triggered flushes actually ran.

package parser

import (
	"testing"
)

// TestMaxPendingLines_CapTriggersFlushDuringBurst writes N lines far
// larger than the cap, then — without calling Close or Flush —
// inspects walNextIdx. It must already cover (N - cap) lines, which
// can only happen if the cap-triggered synchronous flush path ran
// mid-burst. Without the cap, BestEffort holds everything in RAM and
// the WAL is effectively empty until Close.
func TestMaxPendingLines_CapTriggersFlushDuringBurst(t *testing.T) {
	dir := t.TempDir()
	id := "bounded-cap"
	const cols, rows = 120, 24
	const burstLines = 20000
	const cap = 5000

	v := newTestVTerm(t, cols, rows, dir, id)
	defer v.CloseMemoryBuffer()

	// Override the cap to our test value so we don't depend on default
	// drifting over time.
	ap := v.memBufState.persistence
	ap.mu.Lock()
	ap.config.MaxPendingLines = cap
	ap.mu.Unlock()

	writeNumberedLines(v, 0, burstLines)

	wal := ap.wal
	if wal == nil {
		t.Fatal("expected WAL-backed persistence")
	}
	walIdx := wal.NextGlobalIdx()

	// After burstLines writes, at most `cap` lines should still be
	// buffered in pendingLines. Everything before that must have been
	// flushed to the WAL by cap-triggered synchronous flushes.
	minWalLines := int64(burstLines - cap)
	if walIdx < minWalLines {
		t.Errorf("walNextIdx=%d, expected at least %d — cap-triggered flush did not run (burst=%d, cap=%d)",
			walIdx, minWalLines, burstLines, cap)
	}

	pending := ap.PendingCount()
	if pending > cap {
		t.Errorf("pendingLines=%d exceeds cap=%d after burst", pending, cap)
	}

	t.Logf("burst=%d cap=%d → walNextIdx=%d pending=%d",
		burstLines, cap, walIdx, pending)
}

// TestMaxPendingLines_Disabled_PreservesOldBehavior verifies that
// setting MaxPendingLines to 0 disables the cap and restores the
// pre-fix behavior: a burst stays entirely in RAM until close, idle
// flush, or eviction. This lets callers opt out if they need the
// lower-latency path.
func TestMaxPendingLines_Disabled_PreservesOldBehavior(t *testing.T) {
	dir := t.TempDir()
	id := "bounded-disabled"
	const cols, rows = 120, 24
	const burstLines = 8000

	v := newTestVTerm(t, cols, rows, dir, id)
	defer v.CloseMemoryBuffer()

	ap := v.memBufState.persistence
	ap.mu.Lock()
	ap.config.MaxPendingLines = 0
	// Force BestEffort directly so Debounced's timer flush can't
	// interfere with the assertion.
	ap.currentMode = PersistBestEffort
	ap.mu.Unlock()

	writeNumberedLines(v, 0, burstLines)

	// With the cap disabled and in BestEffort, no flush should have
	// fired yet (idle monitor hasn't had time, no eviction, no close).
	// The WAL should be empty or nearly so, and pendingLines should
	// hold the whole burst.
	pending := ap.PendingCount()
	if pending < burstLines-rows { // tolerate a handful of lines that
		// went through eviction via large memBuf
		t.Errorf("pendingLines=%d, expected ~%d (cap disabled, no flush expected)",
			pending, burstLines)
	}
	t.Logf("cap=disabled burst=%d → pending=%d", burstLines, pending)
}

// TestMaxPendingLines_BoundsCrashLoss is the end-to-end property test:
// simulate a crash (dirty close, skipping the graceful CloseMemoryBuffer
// path), reopen, and count how much of the burst survived. With the cap
// at 5000, at least (burst - 5000) lines must be recoverable.
func TestMaxPendingLines_BoundsCrashLoss(t *testing.T) {
	dir := t.TempDir()
	id := "bounded-crash"
	const cols, rows = 120, 24
	const burstLines = 20000
	const cap = 5000

	v1 := newTestVTerm(t, cols, rows, dir, id)
	ap := v1.memBufState.persistence
	ap.mu.Lock()
	ap.config.MaxPendingLines = cap
	ap.currentMode = PersistBestEffort
	ap.mu.Unlock()

	writeNumberedLines(v1, 0, burstLines)

	// Simulate a crash: dirtyClose skips CloseMemoryBuffer's flush
	// path and just nukes file handles. Anything still in pendingLines
	// is lost.
	dirtyClose(v1)

	// Reopen and see how much survived.
	v2 := newTestVTerm(t, cols, rows, dir, id)
	defer v2.CloseMemoryBuffer()

	ps := v2.memBufState.pageStore
	if ps == nil {
		t.Fatal("no pageStore after reopen")
	}
	storedCount := ps.LineCount()

	// With cap=5000 and burst=20000, at LEAST 15000 lines must have
	// been flushed to disk before the crash.
	minRecovered := int64(burstLines - cap)
	if storedCount < minRecovered {
		t.Errorf("after crash: pageStore has %d lines, expected at least %d (burst=%d cap=%d)",
			storedCount, minRecovered, burstLines, cap)
	}

	// Sanity: an early recovered line (well before the cap window)
	// should have the exact content we wrote. Picking line 0 avoids
	// any ambiguity about whether the crash-loss window included it.
	line, err := ps.ReadLine(0)
	if err != nil {
		t.Fatalf("read line 0: %v", err)
	}
	if line == nil {
		t.Fatal("line 0 is nil after recovery")
	}
	got := trimLogicalLine(logicalLineToString(line))
	want := "L00000"
	if len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("line 0 content mismatch: got %q, expected prefix %q", got, want)
	}

	t.Logf("burst=%d cap=%d → recovered=%d (max loss=%d)",
		burstLines, cap, storedCount, burstLines-int(storedCount))
}

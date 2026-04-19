// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"testing"
	"time"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// TestLoadStore_SkipsTombstonedRange verifies that lines deleted via
// WriteAheadLog.DeleteRange are absent from a sparse.Store after a
// close-and-reopen cycle.
//
// Sequence:
//  1. Open a WAL, append 20 lines (gi 0–19) via wal.Append.
//  2. Call wal.DeleteRange(5, 10) — emits a tombstone and removes [5,10]
//     from the in-memory PageStore index.
//  3. SyncWAL() — flush WAL entries to disk without checkpointing (so the
//     tombstone survives on disk for recovery).
//  4. Reopen the WAL on the same directory — recover() replays all line
//     writes then applies the delete tombstone.
//  5. LoadStore into a fresh sparse.Store.
//  6. Assert GetLine returns nil for gi ∈ [5,10] and non-nil for gi ∈ {0,4,11,19}.
func TestLoadStore_SkipsTombstonedRange(t *testing.T) {
	baseDir := t.TempDir()
	terminalID := "test-tombstone"

	cfg := parser.DefaultWALConfig(baseDir, terminalID)
	cfg.CheckpointInterval = 0 // disable auto-checkpoint

	// Open the first WAL.
	wal1, err := parser.OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}

	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	// Append 20 lines (gi 0–19) to the WAL.
	for i := range 20 {
		cells := []parser.Cell{{Rune: rune('A' + i)}}
		line := &parser.LogicalLine{Cells: cells}
		if err := wal1.Append(int64(i), line, now); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}

	// Delete gi range [5, 10] inclusive.
	if err := wal1.DeleteRange(5, 10); err != nil {
		t.Fatalf("DeleteRange(5, 10): %v", err)
	}

	// Flush WAL to disk without checkpointing so the tombstone survives.
	if err := wal1.SyncWAL(); err != nil {
		t.Fatalf("SyncWAL: %v", err)
	}
	// Intentionally do NOT call wal1.Close() — that would checkpoint and lose
	// the delete tombstone.  The reopen path's recover() must replay it.

	// Reopen the WAL on the same path; recover() replays line writes + tombstone.
	cfg2 := parser.DefaultWALConfig(baseDir, terminalID)
	cfg2.CheckpointInterval = 0
	wal2, err := parser.OpenWriteAheadLog(cfg2)
	if err != nil {
		t.Fatalf("reopen OpenWriteAheadLog: %v", err)
	}
	defer wal2.Close()

	ps2 := wal2.PageStore()

	// Load the PageStore into a fresh sparse.Store.
	store := NewStore(80)
	if err := LoadStore(store, ps2); err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	// Lines in [5, 10] must be absent (tombstoned).
	for gi := int64(5); gi <= 10; gi++ {
		if got := store.GetLine(gi); got != nil {
			t.Errorf("GetLine(%d) = %v, want nil (tombstoned)", gi, got)
		}
	}

	// Lines outside the deleted range must be present.
	for _, gi := range []int64{0, 4, 11, 19} {
		if got := store.GetLine(gi); got == nil {
			t.Errorf("GetLine(%d) = nil, want non-nil (not tombstoned)", gi)
		}
	}
}

// TestLoadStore_TombstoneSurvivesCheckpoint is the checkpoint-path companion to
// TestLoadStore_SkipsTombstonedRange.  It exercises the bug where
// checkpointLocked() skipped EntryTypeLineDelete entries, causing tombstoned
// lines to be resurrected after a normal WAL.Close() cycle.
//
// The only difference from TestLoadStore_SkipsTombstonedRange is that the WAL
// is closed with wal1.Close() instead of wal1.SyncWAL().  Close() triggers a
// final checkpoint (WAL entries flushed → PageStore → WAL truncated), so the
// tombstone must be applied to the PageStore before truncation — otherwise it
// is silently dropped and the deleted lines reappear on the next reopen.
//
// This test FAILS without the fix and PASSES with it.
func TestLoadStore_TombstoneSurvivesCheckpoint(t *testing.T) {
	baseDir := t.TempDir()
	terminalID := "test-tombstone-checkpoint"

	cfg := parser.DefaultWALConfig(baseDir, terminalID)
	cfg.CheckpointInterval = 0 // disable auto-checkpoint

	// Open the first WAL.
	wal1, err := parser.OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}

	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	// Append 20 lines (gi 0–19) to the WAL.
	for i := range 20 {
		cells := []parser.Cell{{Rune: rune('A' + i)}}
		line := &parser.LogicalLine{Cells: cells}
		if err := wal1.Append(int64(i), line, now); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}

	// Delete gi range [5, 10] inclusive.
	if err := wal1.DeleteRange(5, 10); err != nil {
		t.Fatalf("DeleteRange(5, 10): %v", err)
	}

	// Close() forces a final checkpoint: WAL entries (including the tombstone)
	// are replayed into the PageStore, PageStore is flushed, WAL is truncated.
	// The tombstone must survive this so that the reopen sees deleted lines as
	// absent.
	if err := wal1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen the WAL on the same path.
	cfg2 := parser.DefaultWALConfig(baseDir, terminalID)
	cfg2.CheckpointInterval = 0
	wal2, err := parser.OpenWriteAheadLog(cfg2)
	if err != nil {
		t.Fatalf("reopen OpenWriteAheadLog: %v", err)
	}
	defer wal2.Close()

	ps2 := wal2.PageStore()

	// Load the PageStore into a fresh sparse.Store.
	store := NewStore(80)
	if err := LoadStore(store, ps2); err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	// Lines in [5, 10] must be absent (tombstoned) — they must NOT be
	// resurrected from the page files after checkpoint.
	for gi := int64(5); gi <= 10; gi++ {
		if got := store.GetLine(gi); got != nil {
			t.Errorf("GetLine(%d) = %v, want nil (tombstoned; checkpoint should have applied delete)", gi, got)
		}
	}

	// Lines outside the deleted range must be present.
	for _, gi := range []int64{0, 4, 11, 19} {
		if got := store.GetLine(gi); got == nil {
			t.Errorf("GetLine(%d) = nil, want non-nil (not tombstoned)", gi)
		}
	}
}

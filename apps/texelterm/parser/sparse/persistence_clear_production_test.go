// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"testing"
	"time"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// TestLoadStore_TombstoneSurvivesCheckpointAfterFlush is the production-bug
// regression test. It reproduces the sequence the running texel-server hits
// in real life:
//
//  1. Phantom writes are flushed to on-disk .page files (via a periodic
//     checkpoint — default 30s).
//  2. ED 2 anchor-rewind emits a DeleteRange tombstone into the WAL.
//  3. Shutdown triggers a final Close() checkpoint. The checkpoint sees ONLY
//     the delete entry (writes were already flushed and WAL truncated).
//  4. deleteRangeNoWAL removes the entries from the in-memory pageIndex but
//     the raw page file bytes on disk are never touched, then the WAL is
//     truncated.
//  5. On reopen, rebuildIndex() rescans the page files and repopulates
//     pageIndex from their headers — resurrecting the phantom rows.
//
// Distinct from TestLoadStore_TombstoneSurvivesCheckpoint: that test appends
// all 20 writes + the delete inside a single checkpoint cycle, which the
// two-pass epoch approach handles by shadowing writes BEFORE they hit
// PageStore. This test proves the bug remains when writes are flushed to
// disk BEFORE the delete is emitted.
func TestLoadStore_TombstoneSurvivesCheckpointAfterFlush(t *testing.T) {
	baseDir := t.TempDir()
	terminalID := "test-tombstone-prod"

	cfg := parser.DefaultWALConfig(baseDir, terminalID)
	cfg.CheckpointInterval = 0 // disable auto-checkpoint; we drive it by hand

	wal1, err := parser.OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}

	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	// Append 20 lines (gi 0–19).
	for i := range 20 {
		cells := []parser.Cell{{Rune: rune('A' + i)}}
		line := &parser.LogicalLine{Cells: cells}
		if err := wal1.Append(int64(i), line, now); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}

	// FORCE a checkpoint BEFORE the delete. This flushes all writes to the
	// .page files and truncates the WAL. This is the critical step that
	// distinguishes the production scenario from the existing checkpoint
	// test: writes now live on disk as raw bytes in page files.
	if err := wal1.Checkpoint(); err != nil {
		t.Fatalf("pre-delete Checkpoint: %v", err)
	}

	// NOW emit the delete tombstone.
	if err := wal1.DeleteRange(5, 10); err != nil {
		t.Fatalf("DeleteRange(5, 10): %v", err)
	}

	// Final close triggers a second checkpoint. This checkpoint's replay
	// sees ONLY the delete entry (writes were flushed in the earlier
	// checkpoint). The fix must actually remove those bytes from the page
	// files on disk — not just from the in-memory pageIndex — or reopen
	// will resurrect them via rebuildIndex.
	if err := wal1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen. rebuildIndex() will re-scan every .page file from disk.
	cfg2 := parser.DefaultWALConfig(baseDir, terminalID)
	cfg2.CheckpointInterval = 0
	wal2, err := parser.OpenWriteAheadLog(cfg2)
	if err != nil {
		t.Fatalf("reopen OpenWriteAheadLog: %v", err)
	}
	defer wal2.Close()

	ps2 := wal2.PageStore()

	store := NewStore(80)
	if err := LoadStore(store, ps2); err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	// Tombstoned range must not resurrect from page files.
	for gi := int64(5); gi <= 10; gi++ {
		if got := store.GetLine(gi); got != nil {
			t.Errorf("GetLine(%d) = %v, want nil (page bytes must be removed, not just pageIndex)", gi, got)
		}
	}

	// Untouched rows must still be present.
	for _, gi := range []int64{0, 4, 11, 19} {
		if got := store.GetLine(gi); got == nil {
			t.Errorf("GetLine(%d) = nil, want non-nil", gi)
		}
	}
}

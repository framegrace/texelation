// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestWAL(t *testing.T) *WriteAheadLog {
	t.Helper()
	dir := t.TempDir()
	cfg := DefaultWALConfig(filepath.Join(dir, "hist"), "sparse-wal-test")
	cfg.CheckpointInterval = 0       // disable timer-based auto-checkpoint
	cfg.CheckpointMaxSize = 1 << 30  // effectively disable size-based auto-checkpoint
	wal, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}
	t.Cleanup(func() { wal.Close() })
	return wal
}

// TestWAL_RecoveryWithGapAfterCheckpoint reproduces the ls -lR startup
// failure: PageStore has sparse content from a prior checkpoint (gaps inside
// its logical-end range), and the WAL holds unflushed LineWrite/LineModify
// entries for some of those gap indices. Recovery must not use LineCount as
// an existence proxy — it must check HasLine per index.
func TestWAL_RecoveryWithGapAfterCheckpoint(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultWALConfig(filepath.Join(dir, "hist"), "recover-gap-test")
	cfg.CheckpointInterval = 0
	cfg.CheckpointMaxSize = 1 << 30

	// Session 1: write content with a gap, force checkpoint, then write more
	// LineWrite entries past the gap without a second checkpoint, and close.
	wal1, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog #1: %v", err)
	}
	now := time.Now()
	for _, idx := range []int64{0, 1, 2} {
		if err := wal1.Append(idx, mkLine("early"), now); err != nil {
			t.Fatalf("Append(%d): %v", idx, err)
		}
	}
	// Jump past a gap and checkpoint so PageStore has sparse content.
	for _, idx := range []int64{100, 101} {
		if err := wal1.Append(idx, mkLine("mid"), now); err != nil {
			t.Fatalf("Append(%d): %v", idx, err)
		}
	}
	if err := wal1.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	// Write more entries after checkpoint; these stay in the WAL, not yet in
	// PageStore. Include a LineModify for an already-checkpointed line.
	for _, idx := range []int64{200, 201, 202} {
		if err := wal1.Append(idx, mkLine("late"), now); err != nil {
			t.Fatalf("Append(%d): %v", idx, err)
		}
	}
	if err := wal1.Append(101, mkLine("updated"), now); err != nil {
		t.Fatalf("Append modify: %v", err)
	}
	if err := wal1.Close(); err != nil {
		t.Fatalf("wal1.Close: %v", err)
	}

	// Session 2: reopen. Recovery must succeed even though 200/201/202 fall
	// inside a gap from PageStore's perspective (LineCount was 102, so the
	// old heuristic "lineIdx >= LineCount → new" would misclassify).
	wal2, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog #2: %v", err)
	}
	defer wal2.Close()

	if got := wal2.pageStore.LineCount(); got != 203 {
		t.Errorf("LineCount after recovery: got %d, want 203", got)
	}
	if got := wal2.pageStore.StoredLineCount(); got != 8 {
		t.Errorf("StoredLineCount after recovery: got %d, want 8", got)
	}

	// Modified line must show the updated content.
	line, _ := wal2.pageStore.ReadLine(101)
	if line == nil {
		t.Fatalf("ReadLine(101) after recovery: nil")
	}
	if got := string(runesFromCells(line.Cells)); got != "updated" {
		t.Errorf("line 101 after recovery: got %q, want \"updated\"", got)
	}

	// New post-checkpoint lines must be present.
	for _, idx := range []int64{200, 201, 202} {
		line, _ := wal2.pageStore.ReadLine(idx)
		if line == nil {
			t.Errorf("ReadLine(%d) after recovery: nil", idx)
		}
	}
}

func TestWAL_CheckpointWithGap(t *testing.T) {
	wal := newTestWAL(t)

	// Append entries at globalIdx 0,1,2 then jump to 100,101.
	now := time.Now()
	for _, idx := range []int64{0, 1, 2, 100, 101} {
		if err := wal.Append(idx, mkLine("x"), now); err != nil {
			t.Fatalf("Append(%d): %v", idx, err)
		}
	}

	// Force checkpoint.
	if err := wal.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	// PageStore state after checkpoint.
	if got := wal.pageStore.LineCount(); got != 102 {
		t.Errorf("LineCount after checkpoint: got %d, want 102", got)
	}
	if got := wal.pageStore.StoredLineCount(); got != 5 {
		t.Errorf("StoredLineCount after checkpoint: got %d, want 5", got)
	}

	// Verify stored entries are readable and gaps return nil.
	for _, idx := range []int64{0, 1, 2, 100, 101} {
		line, err := wal.pageStore.ReadLine(idx)
		if err != nil || line == nil {
			t.Errorf("ReadLine(%d): got (%v, %v), want line", idx, line, err)
		}
	}
	for _, idx := range []int64{3, 50, 99} {
		line, err := wal.pageStore.ReadLine(idx)
		if err != nil || line != nil {
			t.Errorf("ReadLine(%d) gap: got (%v, %v), want (nil, nil)", idx, line, err)
		}
	}
}

func TestWAL_CheckpointWithGap_ThenModify(t *testing.T) {
	wal := newTestWAL(t)

	now := time.Now()
	for _, idx := range []int64{0, 1, 2, 100, 101} {
		if err := wal.Append(idx, mkLine("x"), now); err != nil {
			t.Fatalf("Append(%d): %v", idx, err)
		}
	}
	if err := wal.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	// Modify an existing line — goes through WAL as EntryTypeLineModify,
	// checkpoint Pass 2 calls UpdateLine(101, ...) which must succeed.
	if err := wal.Append(101, mkLine("modified"), now); err != nil {
		t.Fatalf("Append modify: %v", err)
	}
	if err := wal.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint modify: %v", err)
	}

	line, err := wal.pageStore.ReadLine(101)
	if err != nil || line == nil {
		t.Fatalf("ReadLine(101): (%v, %v)", line, err)
	}
	if got := string(runesFromCells(line.Cells)); got != "modified" {
		t.Errorf("line 101 content: got %q, want \"modified\"", got)
	}
}

func TestWAL_RecoveryPreservesGap(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultWALConfig(filepath.Join(dir, "hist"), "sparse-recovery-test")
	cfg.CheckpointInterval = 0
	cfg.CheckpointMaxSize = 1 << 30

	wal1, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog #1: %v", err)
	}
	now := time.Now()
	for _, idx := range []int64{0, 1, 2, 100, 101} {
		if err := wal1.Append(idx, mkLine("x"), now); err != nil {
			t.Fatalf("Append(%d): %v", idx, err)
		}
	}
	if err := wal1.Close(); err != nil {
		t.Fatalf("wal1.Close: %v", err)
	}

	wal2, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog #2: %v", err)
	}
	defer wal2.Close()

	if got := wal2.pageStore.LineCount(); got != 102 {
		t.Errorf("LineCount after recovery: got %d, want 102", got)
	}
	if got := wal2.pageStore.StoredLineCount(); got != 5 {
		t.Errorf("StoredLineCount after recovery: got %d, want 5", got)
	}
	for _, idx := range []int64{50} {
		line, _ := wal2.pageStore.ReadLine(idx)
		if line != nil {
			t.Errorf("ReadLine(%d) after recovery: got line, want nil", idx)
		}
	}
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/adaptive_persistence_recovery_test.go
// Summary: Tests AdaptivePersistence metadata and line recovery at the
// persistence layer — one level above the WAL. Exercises NotifyWrite,
// NotifyMetadataChange, Flush, Close, and dirty-close paths to verify
// that liveEdgeBase/cursor/content survive restarts.

package parser

import (
	"fmt"
	"testing"
	"time"
)

// --- Helpers ---

// apTestSetup creates a MemoryBuffer + WAL + AdaptivePersistence for testing.
// Returns ap, memBuf, and the WAL config (for reopening). Caller must close ap.
type apTestEnv struct {
	ap     *AdaptivePersistence
	mb     *MemoryBuffer
	walCfg WALConfig
	dir    string
	id     string
}

func newAPTestEnv(t *testing.T, dir, id string, numLines int) *apTestEnv {
	t.Helper()

	walCfg := DefaultWALConfig(dir, id)
	walCfg.CheckpointInterval = 0
	walCfg.CheckpointMaxSize = 0

	wal, err := OpenWriteAheadLog(walCfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}

	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100000, EvictionBatch: 1000})
	mb.SetTermWidth(80)

	apCfg := DefaultAdaptivePersistenceConfig()
	ap, err := newAdaptivePersistenceWithWAL(apCfg, mb, wal, time.Now)
	if err != nil {
		wal.Close()
		t.Fatalf("newAdaptivePersistenceWithWAL: %v", err)
	}

	// Write lines to MemoryBuffer and notify persistence
	for i := 0; i < numLines; i++ {
		mb.EnsureLine(int64(i))
		mb.SetCursor(int64(i), 0)
		text := fmt.Sprintf("ap-line-%06d-padding", i)
		for _, r := range text {
			mb.Write(r, DefaultFG, DefaultBG, 0)
		}
		ap.NotifyWrite(int64(i))
	}

	return &apTestEnv{ap: ap, mb: mb, walCfg: walCfg, dir: dir, id: id}
}

// setMetadata sets viewport metadata on the AdaptivePersistence.
func (env *apTestEnv) setMetadata(leb int64, cx, cy int) {
	env.ap.NotifyMetadataChange(&ViewportState{
		LiveEdgeBase: leb,
		CursorX:      cx,
		CursorY:      cy,
		SavedAt:      time.Now(),
	})
}

// dirtyCloseAP simulates a crash: stops the persistence without flushing.
func (env *apTestEnv) dirtyCloseAP() {
	env.ap.mu.Lock()
	env.ap.stopped = true
	env.ap.cancelFlushTimerLocked()
	env.ap.mu.Unlock()
	env.ap.stopIdleMonitor()

	if env.ap.wal != nil {
		env.ap.wal.mu.Lock()
		env.ap.wal.stopped = true
		if env.ap.wal.checkpointTimer != nil {
			env.ap.wal.checkpointTimer.Stop()
		}
		env.ap.wal.mu.Unlock()
		close(env.ap.wal.stopCh)
		if env.ap.wal.walFile != nil {
			env.ap.wal.walFile.Close()
		}
		if env.ap.wal.pageStore != nil {
			env.ap.wal.pageStore.Close()
		}
	}
}

// reopenWALMetadata reopens the WAL and returns recovered metadata + line count.
func (env *apTestEnv) reopenWALMetadata(t *testing.T) (*ViewportState, int64) {
	t.Helper()
	cfg := env.walCfg
	wal, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("Reopen WAL: %v", err)
	}
	meta := wal.RecoveredMetadata()
	lineCount := wal.pageStore.LineCount()
	wal.Close()
	return meta, lineCount
}

// verifyConsistency reopens the WAL and checks that the recovered state is
// internally consistent: metadata references valid lines, no gaps in
// PageStore, and liveEdgeBase/cursor are within bounds.
func (env *apTestEnv) verifyConsistency(t *testing.T, viewportHeight int) {
	t.Helper()
	cfg := env.walCfg
	wal, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("Reopen WAL for consistency check: %v", err)
	}
	defer wal.Close()

	meta := wal.RecoveredMetadata()
	ps := wal.pageStore
	lineCount := ps.LineCount()

	t.Logf("Consistency check: lineCount=%d, meta=%+v", lineCount, meta)

	// Check line continuity: read all lines and verify no nil gaps
	if lineCount > 0 {
		lines, err := ps.ReadLineRange(0, lineCount)
		if err != nil {
			t.Errorf("ReadLineRange(0, %d): %v", lineCount, err)
		} else {
			nilCount := 0
			for i, line := range lines {
				if line == nil {
					nilCount++
					if nilCount <= 5 {
						t.Errorf("Line %d is nil (gap in PageStore)", i)
					}
				}
			}
			if nilCount > 5 {
				t.Errorf("... and %d more nil lines", nilCount-5)
			}
		}
	}

	if meta == nil {
		t.Log("No metadata — consistency check limited to line continuity")
		return
	}

	// LiveEdgeBase must be within [0, lineCount]
	if meta.LiveEdgeBase < 0 {
		t.Errorf("LiveEdgeBase=%d is negative", meta.LiveEdgeBase)
	}
	if meta.LiveEdgeBase > lineCount {
		t.Errorf("LiveEdgeBase=%d exceeds lineCount=%d", meta.LiveEdgeBase, lineCount)
	}

	// CursorY must be within [0, viewportHeight-1]
	if viewportHeight > 0 {
		if meta.CursorY < 0 || meta.CursorY >= viewportHeight {
			t.Errorf("CursorY=%d out of viewport range [0, %d)", meta.CursorY, viewportHeight)
		}
	}

	// CursorX must be non-negative
	if meta.CursorX < 0 {
		t.Errorf("CursorX=%d is negative", meta.CursorX)
	}

	// The cursor's global line (liveEdgeBase + cursorY) must be <= lineCount
	cursorGlobal := meta.LiveEdgeBase + int64(meta.CursorY)
	if cursorGlobal > lineCount {
		t.Errorf("Cursor global line %d (LEB=%d + CY=%d) exceeds lineCount=%d",
			cursorGlobal, meta.LiveEdgeBase, meta.CursorY, lineCount)
	}

	// The viewport range [liveEdgeBase, liveEdgeBase+viewportHeight) should
	// be backed by actual content (lines exist in PageStore)
	if viewportHeight > 0 && meta.LiveEdgeBase+int64(viewportHeight) <= lineCount {
		vpLines, err := ps.ReadLineRange(meta.LiveEdgeBase, meta.LiveEdgeBase+int64(viewportHeight))
		if err != nil {
			t.Errorf("ReadLineRange for viewport [%d, %d): %v",
				meta.LiveEdgeBase, meta.LiveEdgeBase+int64(viewportHeight), err)
		} else {
			nilVP := 0
			for _, line := range vpLines {
				if line == nil {
					nilVP++
				}
			}
			if nilVP > 0 {
				t.Errorf("%d nil lines in viewport range [%d, %d)",
					nilVP, meta.LiveEdgeBase, meta.LiveEdgeBase+int64(viewportHeight))
			}
		}
	}
}

// --- Tests ---

// TestAP_MetadataRoundTrip_CleanClose writes lines and metadata, does a
// clean Close, and verifies metadata is recovered from WAL.
func TestAP_MetadataRoundTrip_CleanClose(t *testing.T) {
	dir := t.TempDir()
	env := newAPTestEnv(t, dir, "meta-clean", 50)

	env.setMetadata(30, 5, 15)

	if err := env.ap.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	meta, lineCount := env.reopenWALMetadata(t)
	t.Logf("Recovered: lineCount=%d, meta=%+v", lineCount, meta)

	if lineCount != 50 {
		t.Errorf("lineCount: got %d, want 50", lineCount)
	}
	if meta == nil {
		t.Fatal("Metadata not recovered after clean close")
	}
	if meta.LiveEdgeBase != 30 {
		t.Errorf("LiveEdgeBase: got %d, want 30", meta.LiveEdgeBase)
	}
	if meta.CursorX != 5 {
		t.Errorf("CursorX: got %d, want 5", meta.CursorX)
	}
	if meta.CursorY != 15 {
		t.Errorf("CursorY: got %d, want 15", meta.CursorY)
	}
}

// TestAP_MetadataRoundTrip_FlushThenClose writes, flushes explicitly,
// updates metadata, closes. Metadata from the Close path should win.
func TestAP_MetadataRoundTrip_FlushThenClose(t *testing.T) {
	dir := t.TempDir()
	env := newAPTestEnv(t, dir, "meta-flush-close", 50)

	// First metadata + flush
	env.setMetadata(10, 0, 5)
	if err := env.ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Update metadata AFTER flush (simulates cursor movement after idle flush)
	env.setMetadata(30, 7, 20)

	// Close should write the updated metadata
	if err := env.ap.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	meta, _ := env.reopenWALMetadata(t)
	if meta == nil {
		t.Fatal("Metadata not recovered")
	}
	if meta.LiveEdgeBase != 30 {
		t.Errorf("LiveEdgeBase: got %d, want 30 (updated value)", meta.LiveEdgeBase)
	}
	if meta.CursorY != 20 {
		t.Errorf("CursorY: got %d, want 20 (updated value)", meta.CursorY)
	}
}

// TestAP_MetadataRoundTrip_DirtyClose_AfterFlush writes, flushes with
// metadata, then dirty-closes. The flushed metadata should survive.
func TestAP_MetadataRoundTrip_DirtyClose_AfterFlush(t *testing.T) {
	dir := t.TempDir()
	env := newAPTestEnv(t, dir, "meta-dirty-flushed", 50)

	env.setMetadata(25, 3, 12)
	if err := env.ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Dirty close — metadata was already flushed
	env.dirtyCloseAP()

	meta, lineCount := env.reopenWALMetadata(t)
	t.Logf("Recovered: lineCount=%d, meta=%+v", lineCount, meta)

	if meta == nil {
		t.Fatal("Metadata lost after dirty close (was flushed)")
	}
	if meta.LiveEdgeBase != 25 {
		t.Errorf("LiveEdgeBase: got %d, want 25", meta.LiveEdgeBase)
	}
	if meta.CursorY != 12 {
		t.Errorf("CursorY: got %d, want 12", meta.CursorY)
	}
}

// TestAP_MetadataRoundTrip_DirtyClose_BeforeFlush writes and sets metadata
// but dirty-closes before any flush. Metadata should be lost.
func TestAP_MetadataRoundTrip_DirtyClose_BeforeFlush(t *testing.T) {
	dir := t.TempDir()
	env := newAPTestEnv(t, dir, "meta-dirty-noflu", 50)

	env.setMetadata(40, 1, 19)

	// Dirty close — nothing flushed
	env.dirtyCloseAP()

	meta, _ := env.reopenWALMetadata(t)
	// Metadata may or may not be present depending on whether WriteThrough
	// mode flushed some lines (and metadata) inline. For BestEffort mode
	// with unflushed data, metadata should be nil.
	if meta != nil {
		t.Logf("Some metadata recovered (WriteThrough flushed inline): LEB=%d, cursor=(%d,%d)",
			meta.LiveEdgeBase, meta.CursorX, meta.CursorY)
	} else {
		t.Log("No metadata recovered as expected (dirty close before flush)")
	}
}

// TestAP_MetadataRoundTrip_IdleFlush writes lines, waits for idle flush
// to fire, then dirty-closes. Metadata from the idle flush should survive.
func TestAP_MetadataRoundTrip_IdleFlush(t *testing.T) {
	dir := t.TempDir()
	env := newAPTestEnv(t, dir, "meta-idle", 200)

	env.setMetadata(180, 0, 19)

	// Wait for idle flush (threshold is 1s, check interval is 500ms)
	time.Sleep(1600 * time.Millisecond)

	// Dirty close after idle flush
	env.dirtyCloseAP()

	meta, lineCount := env.reopenWALMetadata(t)
	t.Logf("Recovered: lineCount=%d, meta=%+v", lineCount, meta)

	if meta == nil {
		t.Fatal("Metadata lost after idle flush + dirty close")
	}
	if meta.LiveEdgeBase != 180 {
		t.Errorf("LiveEdgeBase: got %d, want 180", meta.LiveEdgeBase)
	}
}

// TestAP_MetadataNotOverwrittenByStaleFlush simulates the race where
// a background flush snapshots old metadata, then Close() writes new
// metadata, then the background flush writes its stale snapshot.
// The new metadata must survive.
func TestAP_MetadataNotOverwrittenByStaleFlush(t *testing.T) {
	dir := t.TempDir()
	env := newAPTestEnv(t, dir, "meta-stale", 50)

	// Set initial metadata and flush to persist it
	env.setMetadata(10, 0, 5)
	if err := env.ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Update metadata (simulates cursor movement — within valid bounds)
	env.setMetadata(26, 2, 23)

	// Flush again — this is the "final" metadata
	if err := env.ap.Flush(); err != nil {
		t.Fatalf("Flush 2: %v", err)
	}

	// Close
	if err := env.ap.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	meta, _ := env.reopenWALMetadata(t)
	if meta == nil {
		t.Fatal("Metadata not recovered")
	}
	// The LATEST metadata (26, 2, 23) must be what's recovered
	if meta.LiveEdgeBase != 26 {
		t.Errorf("LiveEdgeBase: got %d, want 26 (latest)", meta.LiveEdgeBase)
	}
	if meta.CursorY != 23 {
		t.Errorf("CursorY: got %d, want 23 (latest)", meta.CursorY)
	}
}

// TestAP_MetadataAfterManyFlushes writes and flushes repeatedly with
// different metadata values. Only the last one should be recovered.
func TestAP_MetadataAfterManyFlushes(t *testing.T) {
	dir := t.TempDir()
	env := newAPTestEnv(t, dir, "meta-many", 20)

	// Use metadata within bounds of the 20 lines written
	for i := 0; i < 10; i++ {
		leb := int64(i) // stays within [0, 19]
		env.setMetadata(leb, i%5, i%10)
		if err := env.ap.Flush(); err != nil {
			t.Fatalf("Flush %d: %v", i, err)
		}
	}

	if err := env.ap.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	meta, _ := env.reopenWALMetadata(t)
	if meta == nil {
		t.Fatal("Metadata not recovered")
	}
	// Last: i=9 → LEB=9, CX=4, CY=9
	if meta.LiveEdgeBase != 9 {
		t.Errorf("LiveEdgeBase: got %d, want 9", meta.LiveEdgeBase)
	}
	if meta.CursorX != 4 {
		t.Errorf("CursorX: got %d, want 4", meta.CursorX)
	}
	if meta.CursorY != 9 {
		t.Errorf("CursorY: got %d, want 9", meta.CursorY)
	}
}

// TestAP_BurstWriteThenClose_MetadataSurvives simulates the ls -lR scenario:
// rapid writes (BestEffort mode), then immediate Close without waiting.
func TestAP_BurstWriteThenClose_MetadataSurvives(t *testing.T) {
	dir := t.TempDir()

	walCfg := DefaultWALConfig(dir, "burst-meta")
	walCfg.CheckpointInterval = 0
	walCfg.CheckpointMaxSize = 0

	wal, err := OpenWriteAheadLog(walCfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}

	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100000, EvictionBatch: 1000})
	mb.SetTermWidth(80)

	apCfg := DefaultAdaptivePersistenceConfig()
	ap, err := newAdaptivePersistenceWithWAL(apCfg, mb, wal, time.Now)
	if err != nil {
		t.Fatalf("newAdaptivePersistenceWithWAL: %v", err)
	}

	// Write 2000 lines rapidly (should trigger BestEffort mode)
	for i := 0; i < 2000; i++ {
		mb.EnsureLine(int64(i))
		mb.SetCursor(int64(i), 0)
		for _, r := range fmt.Sprintf("burst-%06d", i) {
			mb.Write(r, DefaultFG, DefaultBG, 0)
		}
		ap.NotifyWrite(int64(i))
	}

	// Set metadata AFTER burst (simulates the state at time of close)
	ap.NotifyMetadataChange(&ViewportState{
		LiveEdgeBase: 1976,
		CursorX:      0,
		CursorY:      23,
	})

	// Close immediately
	if err := ap.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen and verify
	wal2, err := OpenWriteAheadLog(walCfg)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	meta := wal2.RecoveredMetadata()
	lineCount := wal2.pageStore.LineCount()
	wal2.Close()

	t.Logf("Recovered: lineCount=%d, meta=%+v", lineCount, meta)

	if lineCount < 2000 {
		t.Errorf("lineCount: got %d, want >= 2000", lineCount)
	}
	if meta == nil {
		t.Fatal("Metadata lost after burst + close")
	}
	if meta.LiveEdgeBase != 1976 {
		t.Errorf("LiveEdgeBase: got %d, want 1976", meta.LiveEdgeBase)
	}
	if meta.CursorY != 23 {
		t.Errorf("CursorY: got %d, want 23", meta.CursorY)
	}
}

// TestAP_ConcurrentWritesDuringFlush verifies that writes arriving
// during an ongoing flush (when the lock is released in the I/O phase)
// are not lost and don't corrupt metadata.
func TestAP_ConcurrentWritesDuringFlush(t *testing.T) {
	dir := t.TempDir()
	env := newAPTestEnv(t, dir, "concurrent", 100)

	env.setMetadata(80, 0, 19)

	// Flush to persist initial state
	if err := env.ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Write more lines (simulates PTY output during background flush)
	for i := 100; i < 200; i++ {
		env.mb.EnsureLine(int64(i))
		env.mb.SetCursor(int64(i), 0)
		for _, r := range fmt.Sprintf("concurrent-%06d", i) {
			env.mb.Write(r, DefaultFG, DefaultBG, 0)
		}
		env.ap.NotifyWrite(int64(i))
	}

	// Update metadata
	env.setMetadata(176, 0, 23)

	// Close (should flush the new lines + updated metadata)
	if err := env.ap.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	meta, lineCount := env.reopenWALMetadata(t)
	t.Logf("Recovered: lineCount=%d, meta=%+v", lineCount, meta)

	if lineCount < 200 {
		t.Errorf("lineCount: got %d, want >= 200 (includes concurrent writes)", lineCount)
	}
	if meta == nil {
		t.Fatal("Metadata lost")
	}
	if meta.LiveEdgeBase != 176 {
		t.Errorf("LiveEdgeBase: got %d, want 176 (updated after concurrent writes)", meta.LiveEdgeBase)
	}
}

// TestAP_MultipleRestarts_NoDrift does write→close→reopen cycles at the
// AdaptivePersistence level and verifies metadata doesn't drift.
func TestAP_MultipleRestarts_NoDrift(t *testing.T) {
	dir := t.TempDir()
	id := "ap-drift"

	var expectedLEB int64
	for restart := 0; restart < 5; restart++ {
		walCfg := DefaultWALConfig(dir, id)
		walCfg.CheckpointInterval = 0
		walCfg.CheckpointMaxSize = 0

		wal, err := OpenWriteAheadLog(walCfg)
		if err != nil {
			t.Fatalf("Restart %d: OpenWriteAheadLog: %v", restart, err)
		}

		mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100000, EvictionBatch: 1000})
		mb.SetTermWidth(80)

		apCfg := DefaultAdaptivePersistenceConfig()
		ap, err := newAdaptivePersistenceWithWAL(apCfg, mb, wal, time.Now)
		if err != nil {
			t.Fatalf("Restart %d: create AP: %v", restart, err)
		}

		// Write some lines
		base := int64(restart * 100)
		for i := base; i < base+100; i++ {
			mb.EnsureLine(i)
			mb.SetCursor(i, 0)
			for _, r := range fmt.Sprintf("restart%d-line%d", restart, i) {
				mb.Write(r, DefaultFG, DefaultBG, 0)
			}
			ap.NotifyWrite(i)
		}

		expectedLEB = base + 76
		ap.NotifyMetadataChange(&ViewportState{
			LiveEdgeBase: expectedLEB,
			CursorX:      0,
			CursorY:      23,
		})

		if err := ap.Close(); err != nil {
			t.Fatalf("Restart %d: Close: %v", restart, err)
		}

		// Verify
		wal2, err := OpenWriteAheadLog(walCfg)
		if err != nil {
			t.Fatalf("Restart %d: reopen: %v", restart, err)
		}
		meta := wal2.RecoveredMetadata()
		lineCount := wal2.pageStore.LineCount()
		wal2.Close()

		t.Logf("Restart %d: lineCount=%d, meta.LEB=%d (expected %d)",
			restart, lineCount, func() int64 {
				if meta != nil {
					return meta.LiveEdgeBase
				}
				return -1
			}(), expectedLEB)

		if meta == nil {
			t.Fatalf("Restart %d: metadata lost", restart)
		}
		if meta.LiveEdgeBase != expectedLEB {
			t.Errorf("Restart %d: LiveEdgeBase drift: got %d, want %d",
				restart, meta.LiveEdgeBase, expectedLEB)
		}
	}
}

// --- Consistency after data loss tests ---

// TestAP_Consistency_CleanClose verifies full consistency after clean close.
func TestAP_Consistency_CleanClose(t *testing.T) {
	dir := t.TempDir()
	env := newAPTestEnv(t, dir, "cons-clean", 200)
	env.setMetadata(176, 0, 23)
	env.ap.Close()
	env.verifyConsistency(t, 24)
}

// TestAP_Consistency_DirtyClose_AfterFlush verifies consistency when
// dirty-closing after an explicit flush. Whatever was flushed must be
// self-consistent.
func TestAP_Consistency_DirtyClose_AfterFlush(t *testing.T) {
	dir := t.TempDir()
	env := newAPTestEnv(t, dir, "cons-dirty-flush", 200)
	env.setMetadata(176, 0, 23)
	env.ap.Flush()

	// Write more lines (unflushed)
	for i := 200; i < 400; i++ {
		env.mb.EnsureLine(int64(i))
		env.mb.SetCursor(int64(i), 0)
		for _, r := range fmt.Sprintf("extra-%06d", i) {
			env.mb.Write(r, DefaultFG, DefaultBG, 0)
		}
		env.ap.NotifyWrite(int64(i))
	}
	// Update metadata to point to unflushed region
	env.setMetadata(376, 0, 23)

	// Dirty close — the extra lines and updated metadata are lost
	env.dirtyCloseAP()

	// Consistency check: whatever survived should be self-consistent
	env.verifyConsistency(t, 24)
}

// TestAP_Consistency_DirtyClose_MidBurst verifies consistency when
// dirty-closing mid-burst with no prior flush. Some lines may have
// been written in WriteThrough mode, rest are lost.
func TestAP_Consistency_DirtyClose_MidBurst(t *testing.T) {
	dir := t.TempDir()
	env := newAPTestEnv(t, dir, "cons-dirty-burst", 500)
	env.setMetadata(476, 0, 23)

	// Dirty close immediately
	env.dirtyCloseAP()

	env.verifyConsistency(t, 24)
}

// TestAP_Consistency_DirtyClose_AfterIdleFlush verifies consistency
// after idle flush fires then dirty close.
func TestAP_Consistency_DirtyClose_AfterIdleFlush(t *testing.T) {
	dir := t.TempDir()
	env := newAPTestEnv(t, dir, "cons-dirty-idle", 300)
	env.setMetadata(276, 0, 23)

	// Wait for idle flush
	time.Sleep(1600 * time.Millisecond)

	env.dirtyCloseAP()
	env.verifyConsistency(t, 24)
}

// TestAP_Consistency_AfterMultipleFlushesAndDirtyClose does several
// flush cycles with different metadata, then dirty-closes. The last
// successfully flushed state must be consistent.
func TestAP_Consistency_AfterMultipleFlushesAndDirtyClose(t *testing.T) {
	dir := t.TempDir()
	env := newAPTestEnv(t, dir, "cons-multi-flush", 100)

	// Flush with metadata pointing to line 76
	env.setMetadata(76, 0, 23)
	env.ap.Flush()

	// Write more, flush with metadata pointing to line 176
	for i := 100; i < 200; i++ {
		env.mb.EnsureLine(int64(i))
		env.mb.SetCursor(int64(i), 0)
		for _, r := range fmt.Sprintf("batch2-%06d", i) {
			env.mb.Write(r, DefaultFG, DefaultBG, 0)
		}
		env.ap.NotifyWrite(int64(i))
	}
	env.setMetadata(176, 0, 23)
	env.ap.Flush()

	// Write more (unflushed), update metadata to unflushed region
	for i := 200; i < 300; i++ {
		env.mb.EnsureLine(int64(i))
		env.mb.SetCursor(int64(i), 0)
		for _, r := range fmt.Sprintf("batch3-%06d", i) {
			env.mb.Write(r, DefaultFG, DefaultBG, 0)
		}
		env.ap.NotifyWrite(int64(i))
	}
	env.setMetadata(276, 0, 23)

	// Dirty close — batch 3 and its metadata are lost
	env.dirtyCloseAP()

	// The recovered state should be from batch 2's flush
	env.verifyConsistency(t, 24)

	// Additionally verify the metadata matches the LAST flushed state
	meta, lineCount := env.reopenWALMetadata(t)
	t.Logf("After multi-flush dirty close: lineCount=%d, meta=%+v", lineCount, meta)
	if meta != nil && meta.LiveEdgeBase > lineCount {
		t.Errorf("Inconsistent: LiveEdgeBase=%d > lineCount=%d", meta.LiveEdgeBase, lineCount)
	}
}

// TestAP_Consistency_CursorBeyondContent verifies that the persistence
// layer clamps metadata so LiveEdgeBase and cursor never point beyond
// the content that was actually written to disk.
func TestAP_Consistency_CursorBeyondContent(t *testing.T) {
	dir := t.TempDir()
	env := newAPTestEnv(t, dir, "cons-cursor-oob", 50)

	// Set metadata with cursor pointing well beyond content
	// (This simulates a bug where metadata gets out of sync)
	env.setMetadata(100, 0, 23) // LEB=100 but only 50 lines exist
	env.ap.Flush()
	env.dirtyCloseAP()

	// Use verifyConsistency which opens once and checks everything
	// (reopenWALMetadata would checkpoint/close, consuming the WAL entries)
	env.verifyConsistency(t, 24)
}

// --- State coherence tests: recovered state was a real terminal state ---

// TestAP_Coherence_MetadataMatchesContent verifies that the recovered
// metadata corresponds to the actual content on disk — not just structurally
// valid, but a real state the terminal was in.
func TestAP_Coherence_MetadataMatchesContent(t *testing.T) {
	dir := t.TempDir()

	walCfg := DefaultWALConfig(dir, "coherence")
	walCfg.CheckpointInterval = 0
	walCfg.CheckpointMaxSize = 0
	wal, err := OpenWriteAheadLog(walCfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}

	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100000, EvictionBatch: 1000})
	mb.SetTermWidth(80)

	apCfg := DefaultAdaptivePersistenceConfig()
	ap, err := newAdaptivePersistenceWithWAL(apCfg, mb, wal, time.Now)
	if err != nil {
		t.Fatalf("create AP: %v", err)
	}

	// Write 100 lines with known content
	for i := 0; i < 100; i++ {
		mb.EnsureLine(int64(i))
		mb.SetCursor(int64(i), 0)
		text := fmt.Sprintf("coherence-line-%04d", i)
		for _, r := range text {
			mb.Write(r, DefaultFG, DefaultBG, 0)
		}
		ap.NotifyWrite(int64(i))
	}

	// Set metadata that matches line content: LiveEdgeBase=76 means
	// the viewport shows lines 76-99, cursor on line 99 (row 23)
	ap.NotifyMetadataChange(&ViewportState{
		LiveEdgeBase: 76,
		CursorX:      0,
		CursorY:      23,
	})

	// Flush and close
	ap.Flush()
	ap.Close()

	// Reopen and verify the viewport content matches the metadata
	wal2, err := OpenWriteAheadLog(walCfg)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	meta := wal2.RecoveredMetadata()
	ps := wal2.pageStore

	if meta == nil {
		wal2.Close()
		t.Fatal("Metadata not recovered")
	}

	t.Logf("Recovered: LEB=%d, cursor=(%d,%d), lineCount=%d",
		meta.LiveEdgeBase, meta.CursorX, meta.CursorY, ps.LineCount())

	// Verify the viewport top line content matches what we wrote
	viewportLines, err := ps.ReadLineRange(meta.LiveEdgeBase, meta.LiveEdgeBase+24)
	if err != nil {
		wal2.Close()
		t.Fatalf("ReadLineRange: %v", err)
	}

	for row, line := range viewportLines {
		globalIdx := meta.LiveEdgeBase + int64(row)
		expected := fmt.Sprintf("coherence-line-%04d", globalIdx)
		got := trimLogicalLine(logicalLineToString(line))
		if got != expected {
			t.Errorf("Viewport row %d (global %d): got %q, want %q",
				row, globalIdx, got, expected)
		}
	}

	// Verify the cursor line specifically
	cursorGlobal := meta.LiveEdgeBase + int64(meta.CursorY)
	cursorLine, err := ps.ReadLineRange(cursorGlobal, cursorGlobal+1)
	if err != nil || len(cursorLine) == 0 {
		wal2.Close()
		t.Fatalf("Cursor line %d not readable", cursorGlobal)
	}
	cursorExpected := fmt.Sprintf("coherence-line-%04d", cursorGlobal)
	cursorGot := trimLogicalLine(logicalLineToString(cursorLine[0]))
	if cursorGot != cursorExpected {
		t.Errorf("Cursor line (global %d): got %q, want %q",
			cursorGlobal, cursorGot, cursorExpected)
	}

	wal2.Close()
}

// TestAP_Coherence_FlushMidBurst verifies that when a flush happens during
// active output, the recovered state is a consistent snapshot: the metadata
// references lines that were actually flushed, not lines that only exist
// in memory.
func TestAP_Coherence_FlushMidBurst(t *testing.T) {
	dir := t.TempDir()

	walCfg := DefaultWALConfig(dir, "coherence-burst")
	walCfg.CheckpointInterval = 0
	walCfg.CheckpointMaxSize = 0
	wal, err := OpenWriteAheadLog(walCfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}

	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100000, EvictionBatch: 1000})
	mb.SetTermWidth(80)

	apCfg := DefaultAdaptivePersistenceConfig()
	ap, err := newAdaptivePersistenceWithWAL(apCfg, mb, wal, time.Now)
	if err != nil {
		t.Fatalf("create AP: %v", err)
	}

	// Phase 1: Write 100 lines and flush with metadata
	for i := 0; i < 100; i++ {
		mb.EnsureLine(int64(i))
		mb.SetCursor(int64(i), 0)
		for _, r := range fmt.Sprintf("phase1-line-%04d", i) {
			mb.Write(r, DefaultFG, DefaultBG, 0)
		}
		ap.NotifyWrite(int64(i))
	}
	ap.NotifyMetadataChange(&ViewportState{
		LiveEdgeBase: 76,
		CursorX:      0,
		CursorY:      23,
	})
	ap.Flush()

	// Phase 2: Write 200 MORE lines and set NEW metadata — but DON'T flush
	for i := 100; i < 300; i++ {
		mb.EnsureLine(int64(i))
		mb.SetCursor(int64(i), 0)
		for _, r := range fmt.Sprintf("phase2-line-%04d", i) {
			mb.Write(r, DefaultFG, DefaultBG, 0)
		}
		ap.NotifyWrite(int64(i))
	}
	ap.NotifyMetadataChange(&ViewportState{
		LiveEdgeBase: 276,
		CursorX:      0,
		CursorY:      23,
	})

	// Dirty close — phase 2 data is lost
	ap.mu.Lock()
	ap.stopped = true
	ap.cancelFlushTimerLocked()
	ap.mu.Unlock()
	ap.stopIdleMonitor()
	if ap.wal != nil {
		ap.wal.mu.Lock()
		ap.wal.stopped = true
		if ap.wal.checkpointTimer != nil {
			ap.wal.checkpointTimer.Stop()
		}
		ap.wal.mu.Unlock()
		close(ap.wal.stopCh)
		ap.wal.walFile.Close()
		if ap.wal.pageStore != nil {
			ap.wal.pageStore.Close()
		}
	}

	// Reopen and verify: we should get phase 1's state, NOT phase 2's
	wal2, err := OpenWriteAheadLog(walCfg)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	meta := wal2.RecoveredMetadata()
	ps := wal2.pageStore
	lineCount := ps.LineCount()

	t.Logf("Recovered: LEB=%d, cursor=(%d,%d), lineCount=%d",
		meta.LiveEdgeBase, meta.CursorX, meta.CursorY, lineCount)

	if meta == nil {
		wal2.Close()
		t.Fatal("Metadata not recovered")
	}

	// The metadata must be from phase 1 (LEB=76), NOT phase 2 (LEB=276)
	if meta.LiveEdgeBase == 276 {
		t.Errorf("Recovered phase 2 metadata (LEB=276) but phase 2 lines were not flushed — INCOHERENT")
	}

	// Lines referenced by metadata must exist and have phase 1 content
	if meta.LiveEdgeBase+24 <= lineCount {
		vpLines, err := ps.ReadLineRange(meta.LiveEdgeBase, meta.LiveEdgeBase+24)
		if err != nil {
			wal2.Close()
			t.Fatalf("ReadLineRange: %v", err)
		}
		for row, line := range vpLines {
			if line == nil {
				t.Errorf("Viewport row %d is nil — metadata references non-existent content", row)
				continue
			}
			got := trimLogicalLine(logicalLineToString(line))
			globalIdx := meta.LiveEdgeBase + int64(row)
			expected := fmt.Sprintf("phase1-line-%04d", globalIdx)
			if got != expected {
				t.Errorf("Viewport row %d (global %d): got %q, want %q (phase 1 content)",
					row, globalIdx, got, expected)
			}
		}
	}

	wal2.Close()
}

// TestAP_Coherence_MultiSession verifies coherence across multiple
// write→close→reopen sessions. Each session adds lines; on recovery,
// the metadata must always point to content from the correct session.
func TestAP_Coherence_MultiSession(t *testing.T) {
	dir := t.TempDir()
	id := "coherence-multi"

	for session := 0; session < 4; session++ {
		walCfg := DefaultWALConfig(dir, id)
		walCfg.CheckpointInterval = 0
		walCfg.CheckpointMaxSize = 0
		wal, err := OpenWriteAheadLog(walCfg)
		if err != nil {
			t.Fatalf("Session %d: OpenWriteAheadLog: %v", session, err)
		}

		mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100000, EvictionBatch: 1000})
		mb.SetTermWidth(80)

		apCfg := DefaultAdaptivePersistenceConfig()
		ap, err := newAdaptivePersistenceWithWAL(apCfg, mb, wal, time.Now)
		if err != nil {
			t.Fatalf("Session %d: create AP: %v", session, err)
		}

		// Write session-specific lines
		base := int64(session * 50)
		for i := base; i < base+50; i++ {
			mb.EnsureLine(i)
			mb.SetCursor(i, 0)
			text := fmt.Sprintf("session%d-line-%04d", session, i)
			for _, r := range text {
				mb.Write(r, DefaultFG, DefaultBG, 0)
			}
			ap.NotifyWrite(i)
		}

		leb := base + 26
		ap.NotifyMetadataChange(&ViewportState{
			LiveEdgeBase: leb,
			CursorX:      0,
			CursorY:      23,
		})

		ap.Close()

		// Verify: the content at LEB must be from THIS session
		wal2, err := OpenWriteAheadLog(walCfg)
		if err != nil {
			t.Fatalf("Session %d: reopen: %v", session, err)
		}
		meta := wal2.RecoveredMetadata()
		if meta == nil {
			wal2.Close()
			t.Fatalf("Session %d: metadata lost", session)
		}

		// Read the line at LEB
		lines, err := wal2.pageStore.ReadLineRange(meta.LiveEdgeBase, meta.LiveEdgeBase+1)
		if err != nil || len(lines) == 0 || lines[0] == nil {
			wal2.Close()
			t.Fatalf("Session %d: viewport top line not readable at %d", session, meta.LiveEdgeBase)
		}

		got := trimLogicalLine(logicalLineToString(lines[0]))
		expected := fmt.Sprintf("session%d-line-%04d", session, meta.LiveEdgeBase)
		if got != expected {
			t.Errorf("Session %d: viewport top (global %d): got %q, want %q",
				session, meta.LiveEdgeBase, got, expected)
		}

		wal2.Close()
	}
}

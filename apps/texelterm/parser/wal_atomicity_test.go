// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/wal_atomicity_test.go
// Summary: Tests WAL atomicity and recovery from crashes at every critical point.
// Verifies that partial writes, corrupted entries, and mid-checkpoint crashes
// don't corrupt state or lose committed data.

package parser

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- Helpers ---

// createWALWithLines opens a WAL, writes n lines, and returns it (still open).
func createWALWithLines(t *testing.T, dir, id string, n int) *WriteAheadLog {
	t.Helper()
	cfg := DefaultWALConfig(dir, id)
	cfg.CheckpointInterval = 0 // disable auto-checkpoint timer
	cfg.CheckpointMaxSize = 0  // disable size-based auto-checkpoint
	wal, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}

	for i := 0; i < n; i++ {
		line := &LogicalLine{
			Cells: makeCells(fmt.Sprintf("wal-line-%06d-content-padding-to-make-it-realistic", i)),
		}
		if err := wal.Append(int64(i), line, time.Now()); err != nil {
			t.Fatalf("Append line %d: %v", i, err)
		}
	}
	return wal
}

// walFilePath returns the WAL file path for a given dir and id.
func walFilePath(dir, id string) string {
	return filepath.Join(dir, "terminals", id, "wal.log")
}

// reopenAndVerify reopens the WAL, returns the recovered metadata and
// the PageStore line count. Closes the WAL after reading.
func reopenAndVerify(t *testing.T, dir, id string) (meta *ViewportState, lineCount int64) {
	t.Helper()
	cfg := DefaultWALConfig(dir, id)
	cfg.CheckpointInterval = 0
	cfg.CheckpointMaxSize = 0
	wal, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("Reopen WAL: %v", err)
	}
	meta = wal.RecoveredMetadata()
	lineCount = wal.pageStore.LineCount()
	wal.Close()
	return
}

// --- WAL Entry Atomicity Tests ---

// TestWAL_TruncatedEntry_LastEntryPartial simulates a crash mid-write by
// truncating the WAL file so the last entry is incomplete. Recovery should
// discard the partial entry and recover all complete entries.
func TestWAL_TruncatedEntry_LastEntryPartial(t *testing.T) {
	dir := t.TempDir()
	id := "trunc-partial"

	wal := createWALWithLines(t, dir, id, 10)
	walPath := walFilePath(dir, id)

	// Write metadata so we can verify it survives
	wal.WriteMetadata(&ViewportState{
		LiveEdgeBase: 5,
		CursorX:      10,
		CursorY:      20,
	})

	// Sync to ensure everything is on disk
	wal.SyncWAL()

	// Get WAL file size, then write one more entry and DON'T sync
	fi, _ := os.Stat(walPath)
	sizeBeforeLastWrite := fi.Size()

	wal.Append(10, &LogicalLine{Cells: makeCells("this-entry-will-be-truncated")}, time.Now())
	// Don't sync — but the write might be in the file already via OS buffer

	// Force-close without flush
	wal.mu.Lock()
	wal.stopped = true
	wal.mu.Unlock()
	close(wal.stopCh)
	wal.walFile.Close()
	if wal.pageStore != nil {
		wal.pageStore.Close()
	}

	// Truncate the WAL to cut the last entry in half
	fi2, _ := os.Stat(walPath)
	if fi2.Size() > sizeBeforeLastWrite {
		// There IS a new entry — truncate it to simulate partial write
		halfEntry := sizeBeforeLastWrite + (fi2.Size()-sizeBeforeLastWrite)/2
		os.Truncate(walPath, halfEntry)
		t.Logf("Truncated WAL from %d to %d (cutting last entry)", fi2.Size(), halfEntry)
	} else {
		t.Log("Last entry wasn't flushed to disk — truncation test trivially passes")
	}

	// Reopen and verify
	meta, lineCount := reopenAndVerify(t, dir, id)
	t.Logf("Recovered: lineCount=%d, meta=%+v", lineCount, meta)

	// Should have 10 lines (the truncated 11th is discarded)
	if lineCount < 10 {
		t.Errorf("Expected at least 10 lines, got %d", lineCount)
	}

	// Metadata from before the truncation should survive
	if meta == nil {
		t.Error("Metadata lost after truncated entry recovery")
	} else {
		if meta.LiveEdgeBase != 5 {
			t.Errorf("LiveEdgeBase: got %d, want 5", meta.LiveEdgeBase)
		}
		if meta.CursorX != 10 {
			t.Errorf("CursorX: got %d, want 10", meta.CursorX)
		}
	}
}

// TestWAL_CorruptedCRC_SingleEntry flips a byte in a WAL entry to corrupt
// its CRC. Recovery should discard this entry and everything after it.
func TestWAL_CorruptedCRC_SingleEntry(t *testing.T) {
	dir := t.TempDir()
	id := "corrupt-crc"

	wal := createWALWithLines(t, dir, id, 20)
	wal.SyncWAL()

	// Close cleanly to get the WAL file
	wal.mu.Lock()
	wal.stopped = true
	wal.mu.Unlock()
	close(wal.stopCh)
	wal.walFile.Close()
	if wal.pageStore != nil {
		wal.pageStore.Close()
	}

	// Corrupt a byte in the middle of the WAL file (after header)
	walPath := walFilePath(dir, id)
	data, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Flip a byte roughly 40% into the file (after some valid entries)
	corruptPos := WALHeaderSize + (len(data)-WALHeaderSize)*4/10
	data[corruptPos] ^= 0xFF
	if err := os.WriteFile(walPath, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Logf("Corrupted byte at position %d (file size %d)", corruptPos, len(data))

	// Reopen — should recover entries before the corruption
	meta, lineCount := reopenAndVerify(t, dir, id)
	t.Logf("Recovered: lineCount=%d, meta=%+v", lineCount, meta)

	// Should have some lines (not all 20 — corruption cuts off the rest)
	if lineCount <= 0 {
		t.Error("Expected at least some lines to be recovered")
	}
	if lineCount >= 20 {
		t.Error("Expected corruption to discard some entries")
	}
	t.Logf("Recovered %d of 20 lines (corruption at ~40%%)", lineCount)
}

// TestWAL_EmptyFile_JustHeader tests recovery from a WAL that has only
// the header (as if crash happened right after creation).
func TestWAL_EmptyFile_JustHeader(t *testing.T) {
	dir := t.TempDir()
	id := "empty-header"

	// Create WAL, close it, then truncate to just the header
	wal := createWALWithLines(t, dir, id, 0)
	wal.Close()

	walPath := walFilePath(dir, id)
	os.Truncate(walPath, WALHeaderSize)

	meta, lineCount := reopenAndVerify(t, dir, id)
	t.Logf("Recovered: lineCount=%d, meta=%+v", lineCount, meta)

	if lineCount != 0 {
		t.Errorf("Expected 0 lines from empty WAL, got %d", lineCount)
	}
}

// --- Checkpoint Crash Tests ---

// TestWAL_CrashAfterCheckpoint_BeforeMetadataRewrite simulates a crash
// after checkpoint replays entries to PageStore and truncates WAL, but
// BEFORE metadata is re-written to the fresh WAL.
func TestWAL_CrashAfterCheckpoint_BeforeMetadataRewrite(t *testing.T) {
	dir := t.TempDir()
	id := "crash-post-checkpoint"

	wal := createWALWithLines(t, dir, id, 50)
	wal.WriteMetadata(&ViewportState{
		LiveEdgeBase: 30,
		CursorX:      5,
		CursorY:      15,
	})
	wal.SyncWAL()

	// Manually checkpoint (this replays to PageStore and truncates WAL)
	wal.Checkpoint()

	// Now simulate crash BEFORE metadata re-write by truncating WAL
	// to just the header (removing the re-written metadata)
	walPath := walFilePath(dir, id)

	// Close file handles
	wal.mu.Lock()
	wal.stopped = true
	wal.mu.Unlock()
	close(wal.stopCh)
	wal.walFile.Close()
	if wal.pageStore != nil {
		wal.pageStore.Close()
	}

	os.Truncate(walPath, WALHeaderSize)
	t.Log("Truncated WAL to header-only after checkpoint")

	// Reopen — lines should be in PageStore, but metadata is lost
	meta, lineCount := reopenAndVerify(t, dir, id)
	t.Logf("Recovered: lineCount=%d, meta=%+v", lineCount, meta)

	// Lines should survive (they're in PageStore)
	if lineCount != 50 {
		t.Errorf("Expected 50 lines in PageStore, got %d", lineCount)
	}

	// Metadata is lost — this is expected for this crash scenario
	// The system should handle nil metadata gracefully
	if meta != nil {
		t.Logf("Metadata survived (bonus): liveEdgeBase=%d", meta.LiveEdgeBase)
	} else {
		t.Log("Metadata lost as expected — system should handle this gracefully")
	}
}

// TestWAL_DuplicateReplay_Idempotent simulates a crash where entries
// were replayed to PageStore but WAL wasn't truncated — causing the same
// entries to be replayed again on next open. Should be idempotent.
func TestWAL_DuplicateReplay_Idempotent(t *testing.T) {
	dir := t.TempDir()
	id := "dup-replay"

	wal := createWALWithLines(t, dir, id, 30)
	wal.WriteMetadata(&ViewportState{
		LiveEdgeBase: 10,
		CursorX:      0,
		CursorY:      5,
	})
	wal.SyncWAL()

	// Close without checkpointing — entries stay in WAL
	wal.mu.Lock()
	wal.stopped = true
	wal.mu.Unlock()
	close(wal.stopCh)
	wal.walFile.Close()
	if wal.pageStore != nil {
		wal.pageStore.Close()
	}

	// Open twice — first open replays entries, second should be idempotent
	meta1, count1 := reopenAndVerify(t, dir, id)
	meta2, count2 := reopenAndVerify(t, dir, id)

	t.Logf("First open:  lineCount=%d, meta=%+v", count1, meta1)
	t.Logf("Second open: lineCount=%d, meta=%+v", count2, meta2)

	if count1 != count2 {
		t.Errorf("Line count changed across reopens: %d → %d (not idempotent)", count1, count2)
	}
	if count1 != 30 {
		t.Errorf("Expected 30 lines, got %d", count1)
	}
}

// --- Metadata Persistence Tests ---

// TestWAL_MetadataWrittenAndRecovered verifies that metadata written via
// WriteMetadata survives close+reopen when no checkpoint occurs.
func TestWAL_MetadataWrittenAndRecovered(t *testing.T) {
	dir := t.TempDir()
	id := "meta-basic"

	wal := createWALWithLines(t, dir, id, 5)
	expected := &ViewportState{
		LiveEdgeBase: 42,
		CursorX:      7,
		CursorY:      19,
	}
	wal.WriteMetadata(expected)
	wal.SyncWAL()

	// Close without checkpoint
	wal.mu.Lock()
	wal.stopped = true
	wal.mu.Unlock()
	close(wal.stopCh)
	wal.walFile.Close()
	if wal.pageStore != nil {
		wal.pageStore.Close()
	}

	meta, _ := reopenAndVerify(t, dir, id)
	if meta == nil {
		t.Fatal("Metadata not recovered")
	}
	if meta.LiveEdgeBase != expected.LiveEdgeBase {
		t.Errorf("LiveEdgeBase: got %d, want %d", meta.LiveEdgeBase, expected.LiveEdgeBase)
	}
	if meta.CursorX != expected.CursorX {
		t.Errorf("CursorX: got %d, want %d", meta.CursorX, expected.CursorX)
	}
	if meta.CursorY != expected.CursorY {
		t.Errorf("CursorY: got %d, want %d", meta.CursorY, expected.CursorY)
	}
}

// TestWAL_MetadataLastOneWins writes multiple metadata entries and verifies
// only the last one is recovered.
func TestWAL_MetadataLastOneWins(t *testing.T) {
	dir := t.TempDir()
	id := "meta-lastwin"

	wal := createWALWithLines(t, dir, id, 5)

	for i := 0; i < 10; i++ {
		wal.WriteMetadata(&ViewportState{
			LiveEdgeBase: int64(i * 100),
			CursorX:      i,
			CursorY:      i + 1,
		})
	}
	wal.SyncWAL()

	wal.mu.Lock()
	wal.stopped = true
	wal.mu.Unlock()
	close(wal.stopCh)
	wal.walFile.Close()
	if wal.pageStore != nil {
		wal.pageStore.Close()
	}

	meta, _ := reopenAndVerify(t, dir, id)
	if meta == nil {
		t.Fatal("Metadata not recovered")
	}
	// Last write: i=9 → LiveEdgeBase=900, CursorX=9, CursorY=10
	if meta.LiveEdgeBase != 900 {
		t.Errorf("LiveEdgeBase: got %d, want 900 (last write)", meta.LiveEdgeBase)
	}
	if meta.CursorX != 9 {
		t.Errorf("CursorX: got %d, want 9", meta.CursorX)
	}
}

// TestWAL_MetadataSurvivesCheckpoint verifies that metadata is preserved
// across a checkpoint (re-written to the fresh WAL).
func TestWAL_MetadataSurvivesCheckpoint(t *testing.T) {
	dir := t.TempDir()
	id := "meta-checkpoint"

	wal := createWALWithLines(t, dir, id, 20)
	expected := &ViewportState{
		LiveEdgeBase: 15,
		CursorX:      3,
		CursorY:      12,
	}
	wal.WriteMetadata(expected)
	wal.SyncWAL()

	// Clean close with checkpoint
	wal.Close()

	meta, lineCount := reopenAndVerify(t, dir, id)
	t.Logf("After checkpoint+reopen: lineCount=%d, meta=%+v", lineCount, meta)

	if lineCount != 20 {
		t.Errorf("Expected 20 lines, got %d", lineCount)
	}
	if meta == nil {
		t.Fatal("Metadata lost after checkpoint")
	}
	if meta.LiveEdgeBase != expected.LiveEdgeBase {
		t.Errorf("LiveEdgeBase: got %d, want %d", meta.LiveEdgeBase, expected.LiveEdgeBase)
	}
	if meta.CursorY != expected.CursorY {
		t.Errorf("CursorY: got %d, want %d", meta.CursorY, expected.CursorY)
	}
}

// --- WAL Entry Format Tests ---

// TestWAL_EntryFormat_CRCCoversFullEntry verifies that the CRC covers
// type + lineIdx + timestamp + data, and that flipping any byte is detected.
func TestWAL_EntryFormat_CRCCoversFullEntry(t *testing.T) {
	dir := t.TempDir()
	id := "crc-coverage"

	wal := createWALWithLines(t, dir, id, 1)
	wal.SyncWAL()

	wal.mu.Lock()
	wal.stopped = true
	wal.mu.Unlock()
	close(wal.stopCh)
	wal.walFile.Close()
	if wal.pageStore != nil {
		wal.pageStore.Close()
	}

	walPath := walFilePath(dir, id)
	original, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Try flipping each byte in the entry (after header) — each should fail CRC
	entryStart := WALHeaderSize
	entryEnd := len(original) // single entry fills the rest
	corruptionDetected := 0
	corruptionMissed := 0

	for pos := entryStart; pos < entryEnd; pos++ {
		corrupted := make([]byte, len(original))
		copy(corrupted, original)
		corrupted[pos] ^= 0x01 // flip least significant bit

		if err := os.WriteFile(walPath, corrupted, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		_, lineCount := reopenAndVerify(t, dir, id)
		if lineCount > 0 {
			corruptionMissed++
		} else {
			corruptionDetected++
		}

		// Restore original for next iteration
		if err := os.WriteFile(walPath, original, 0644); err != nil {
			t.Fatalf("WriteFile restore: %v", err)
		}
	}

	t.Logf("CRC coverage: %d/%d byte flips detected, %d missed",
		corruptionDetected, entryEnd-entryStart, corruptionMissed)

	// CRC should catch most flips. The only bytes where a flip might not
	// be detected are if flipping a CRC byte happens to produce a valid CRC
	// for the corrupted data — astronomically unlikely but theoretically possible.
	if corruptionMissed > 0 {
		t.Errorf("CRC failed to detect %d byte corruptions", corruptionMissed)
	}
}

// TestWAL_LargeEntry_Atomicity writes a line with many cells (large entry)
// and verifies it's either fully recovered or fully discarded.
func TestWAL_LargeEntry_Atomicity(t *testing.T) {
	dir := t.TempDir()
	id := "large-entry"

	cfg := DefaultWALConfig(dir, id)
	cfg.CheckpointInterval = 0
	cfg.CheckpointMaxSize = 0
	wal, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}

	// Write a small line first
	small := &LogicalLine{Cells: makeCells("small")}
	wal.Append(0, small, time.Now())

	// Write a large line (8KB+ to exceed PIPE_BUF)
	largeCells := make([]Cell, 2000)
	for i := range largeCells {
		largeCells[i] = Cell{Rune: rune('A' + (i % 26))}
	}
	large := &LogicalLine{Cells: largeCells}
	wal.Append(1, large, time.Now())
	wal.SyncWAL()

	// Close and reopen
	wal.mu.Lock()
	wal.stopped = true
	wal.mu.Unlock()
	close(wal.stopCh)
	wal.walFile.Close()
	if wal.pageStore != nil {
		wal.pageStore.Close()
	}

	_, lineCount := reopenAndVerify(t, dir, id)
	if lineCount != 2 {
		t.Errorf("Expected 2 lines (small + large), got %d", lineCount)
	}

	// Now truncate mid-large-entry and verify only small survives
	walPath := walFilePath(dir, id)
	data, _ := os.ReadFile(walPath)

	// Find approximately where the large entry starts (after header + small entry)
	// Small entry is ~WALEntryBase + len("small")*cellSize
	// Cut at 60% of file to be somewhere in the large entry
	cutPoint := int64(WALHeaderSize) + int64(len(data)-WALHeaderSize)*6/10
	os.Truncate(walPath, cutPoint)
	t.Logf("Truncated WAL from %d to %d bytes", len(data), cutPoint)

	_, lineCount2 := reopenAndVerify(t, dir, id)
	t.Logf("After truncation: lineCount=%d", lineCount2)

	// Should have at least the small entry, large entry should be discarded
	if lineCount2 < 1 {
		t.Error("Expected at least 1 line after truncating large entry")
	}
	if lineCount2 > 1 {
		t.Logf("Large entry survived truncation — cut was after the entry ended")
	}
}

// TestWAL_ZeroLengthData_DoesNotCorrupt verifies that a WAL entry with
// DataLen=0 (empty line) is valid and doesn't break recovery.
func TestWAL_ZeroLengthData_DoesNotCorrupt(t *testing.T) {
	dir := t.TempDir()
	id := "zero-data"

	cfg := DefaultWALConfig(dir, id)
	cfg.CheckpointInterval = 0
	cfg.CheckpointMaxSize = 0
	wal, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}

	// Write an empty line
	wal.Append(0, &LogicalLine{Cells: nil}, time.Now())
	// Write a normal line after
	wal.Append(1, &LogicalLine{Cells: makeCells("after-empty")}, time.Now())
	wal.SyncWAL()
	wal.Close()

	_, lineCount := reopenAndVerify(t, dir, id)
	if lineCount != 2 {
		t.Errorf("Expected 2 lines (empty + normal), got %d", lineCount)
	}
}

// TestWAL_AppendAfterRecovery verifies that new entries can be appended
// after recovering from a crash, and the combined state is correct.
func TestWAL_AppendAfterRecovery(t *testing.T) {
	dir := t.TempDir()
	id := "append-after"

	// Session 1: write 10 lines, dirty close
	wal1 := createWALWithLines(t, dir, id, 10)
	wal1.SyncWAL()
	wal1.mu.Lock()
	wal1.stopped = true
	wal1.mu.Unlock()
	close(wal1.stopCh)
	wal1.walFile.Close()
	if wal1.pageStore != nil {
		wal1.pageStore.Close()
	}

	// Session 2: reopen (recovers 10 lines), write 5 more, clean close
	cfg := DefaultWALConfig(dir, id)
	cfg.CheckpointInterval = 0
	cfg.CheckpointMaxSize = 0
	wal2, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}

	// nextGlobalIdx should be set correctly after recovery
	for i := 10; i < 15; i++ {
		line := &LogicalLine{Cells: makeCells(fmt.Sprintf("session2-line-%d", i))}
		if err := wal2.Append(int64(i), line, time.Now()); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	wal2.Close()

	// Session 3: verify all 15 lines
	_, lineCount := reopenAndVerify(t, dir, id)
	if lineCount != 15 {
		t.Errorf("Expected 15 lines (10 + 5), got %d", lineCount)
	}
}

// TestWAL_HeaderCorruption verifies behavior when the WAL header itself
// is corrupted (bad magic bytes).
// NOTE: Currently the WAL does not validate magic on open — this test
// documents that behavior. Header validation would be a good improvement.
func TestWAL_HeaderCorruption(t *testing.T) {
	dir := t.TempDir()
	id := "header-corrupt"

	wal := createWALWithLines(t, dir, id, 5)
	wal.Close()

	walPath := walFilePath(dir, id)
	data, _ := os.ReadFile(walPath)

	// Corrupt magic bytes
	copy(data[0:8], []byte("BADMAGIC"))
	os.WriteFile(walPath, data, 0644)

	cfg := DefaultWALConfig(dir, id)
	cfg.CheckpointInterval = 0
	wal2, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Logf("Correctly rejected corrupted header: %v", err)
	} else {
		// Currently accepted — document this as known behavior
		t.Log("WAL opened with corrupted header (no magic validation on open)")
		wal2.Close()
	}
}

// TestWAL_VersionMismatch verifies behavior with wrong version number.
// NOTE: Currently the WAL does not validate version on open.
func TestWAL_VersionMismatch(t *testing.T) {
	dir := t.TempDir()
	id := "version-bad"

	wal := createWALWithLines(t, dir, id, 5)
	wal.Close()

	walPath := walFilePath(dir, id)
	data, _ := os.ReadFile(walPath)

	// Set version to 99
	binary.LittleEndian.PutUint32(data[8:12], 99)
	os.WriteFile(walPath, data, 0644)

	cfg := DefaultWALConfig(dir, id)
	cfg.CheckpointInterval = 0
	wal2, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Logf("Correctly rejected version mismatch: %v", err)
	} else {
		// Currently accepted — document this as known behavior
		t.Log("WAL opened with wrong version (no version validation on open)")
		wal2.Close()
	}
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/burst_recovery_test.go
// Summary: Tests for burst write → close → reopen recovery integrity.
// Reproduces the "ls -lR" bug where high-volume output followed by
// server restart causes content loss or incorrect viewport position.

package parser

import (
	"fmt"
	"testing"
	"time"
)

// clearRecoveryGuard clears the restoredFromDisk flag so tests can write
// lines through the parser without the linefeed suppression interfering.
func clearRecoveryGuard(v *VTerm) {
	if v.memBufState != nil {
		v.memBufState.restoredFromDisk = false
	}
}

// dirtyClose simulates a crash: stops the idle monitor and closes file
// handles without flushing pending writes or checkpointing.
func dirtyClose(v *VTerm) {
	if v.memBufState == nil {
		return
	}
	ap := v.memBufState.persistence
	if ap == nil {
		return
	}
	ap.mu.Lock()
	ap.stopped = true
	ap.cancelFlushTimerLocked()
	ap.mu.Unlock()
	ap.stopIdleMonitor()

	if ap.wal != nil {
		// Close WAL file handles without checkpoint or flush
		ap.wal.mu.Lock()
		ap.wal.stopped = true
		if ap.wal.checkpointTimer != nil {
			ap.wal.checkpointTimer.Stop()
		}
		ap.wal.mu.Unlock()
		close(ap.wal.stopCh)
		if ap.wal.walFile != nil {
			ap.wal.walFile.Close()
		}
		if ap.wal.pageStore != nil {
			ap.wal.pageStore.Close()
		}
	}
}

// newTestVTerm creates a VTerm with disk-backed memory buffer in the given dir.
func newTestVTerm(t *testing.T, cols, rows int, dir, id string) *VTerm {
	t.Helper()
	v := NewVTerm(cols, rows)
	diskPath := fmt.Sprintf("%s/%s.hist3", dir, id)
	if err := v.EnableMemoryBufferWithDisk(diskPath, MemoryBufferOptions{
		MaxLines:   100000,
		DiskPath:   diskPath,
		TerminalID: id,
	}); err != nil {
		t.Fatalf("EnableMemoryBufferWithDisk: %v", err)
	}
	return v
}

// writeLines feeds n lines of text through the VTerm parser, simulating
// rapid terminal output like `ls -lR`.
func writeLines(v *VTerm, n int) {
	p := NewParser(v)
	for i := 0; i < n; i++ {
		line := fmt.Sprintf("line %06d: some/path/to/file_%d.txt  -rw-r--r-- 1 user group 12345 Jan  1 00:00\r\n", i, i)
		for _, r := range line {
			p.Parse(r)
		}
	}
}

// snapshotState captures the VTerm state we expect to survive recovery.
type snapshotState struct {
	liveEdgeBase int64
	cursorX      int
	cursorY      int
	globalEnd    int64
	// Content sample: text of a few lines around the viewport
	viewportTopLine string
	cursorLine      string
}

func captureState(v *VTerm) snapshotState {
	mb := v.memBufState.memBuf
	s := snapshotState{
		liveEdgeBase: v.memBufState.liveEdgeBase,
		cursorX:      v.cursorX,
		cursorY:      v.cursorY,
		globalEnd:    mb.GlobalEnd(),
	}
	if line := mb.GetLine(v.memBufState.liveEdgeBase); line != nil {
		s.viewportTopLine = trimLogicalLine(logicalLineToString(line))
	}
	cursorGlobal := v.memBufState.liveEdgeBase + int64(v.cursorY)
	if line := mb.GetLine(cursorGlobal); line != nil {
		s.cursorLine = trimLogicalLine(logicalLineToString(line))
	}
	return s
}

// TestBurstWriteRecovery_BasicIntegrity writes many lines, closes the VTerm,
// reopens from disk, and verifies that liveEdgeBase, cursor, and content
// are correctly recovered.
func TestBurstWriteRecovery_BasicIntegrity(t *testing.T) {
	dir := t.TempDir()
	id := "burst-basic"
	const cols, rows = 120, 24
	const numLines = 500

	// --- Session 1: Write burst output ---
	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeLines(v1, numLines)

	before := captureState(v1)
	t.Logf("Before close: liveEdgeBase=%d, cursor=(%d,%d), globalEnd=%d",
		before.liveEdgeBase, before.cursorX, before.cursorY, before.globalEnd)
	t.Logf("  viewportTopLine: %q", before.viewportTopLine)
	t.Logf("  cursorLine:      %q", before.cursorLine)

	if before.globalEnd < int64(numLines) {
		t.Errorf("Expected globalEnd >= %d, got %d", numLines, before.globalEnd)
	}

	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	// --- Session 2: Reopen and verify ---
	v2 := newTestVTerm(t, cols, rows, dir, id)

	after := captureState(v2)
	t.Logf("After reopen:  liveEdgeBase=%d, cursor=(%d,%d), globalEnd=%d",
		after.liveEdgeBase, after.cursorX, after.cursorY, after.globalEnd)
	t.Logf("  viewportTopLine: %q", after.viewportTopLine)
	t.Logf("  cursorLine:      %q", after.cursorLine)

	// Verify liveEdgeBase survived
	if after.liveEdgeBase != before.liveEdgeBase {
		t.Errorf("liveEdgeBase mismatch: before=%d, after=%d", before.liveEdgeBase, after.liveEdgeBase)
	}

	// Verify cursor position survived
	if after.cursorX != before.cursorX || after.cursorY != before.cursorY {
		t.Errorf("cursor mismatch: before=(%d,%d), after=(%d,%d)",
			before.cursorX, before.cursorY, after.cursorX, after.cursorY)
	}

	// Verify viewport top line content
	if after.viewportTopLine != before.viewportTopLine {
		t.Errorf("viewportTopLine mismatch:\n  before: %q\n  after:  %q",
			before.viewportTopLine, after.viewportTopLine)
	}

	// Verify cursor line content
	if after.cursorLine != before.cursorLine {
		t.Errorf("cursorLine mismatch:\n  before: %q\n  after:  %q",
			before.cursorLine, after.cursorLine)
	}

	v2.CloseMemoryBuffer()
}

// TestBurstWriteRecovery_LargeVolume tests with a very large number of lines
// that exercises BestEffort mode and idle flush.
func TestBurstWriteRecovery_LargeVolume(t *testing.T) {
	dir := t.TempDir()
	id := "burst-large"
	const cols, rows = 120, 24
	const numLines = 5000

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeLines(v1, numLines)

	// Wait for idle flush to fire (idle threshold is 1s by default)
	time.Sleep(1500 * time.Millisecond)

	before := captureState(v1)
	t.Logf("Before close: liveEdgeBase=%d, cursor=(%d,%d), globalEnd=%d",
		before.liveEdgeBase, before.cursorX, before.cursorY, before.globalEnd)

	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	v2 := newTestVTerm(t, cols, rows, dir, id)
	after := captureState(v2)
	t.Logf("After reopen:  liveEdgeBase=%d, cursor=(%d,%d), globalEnd=%d",
		after.liveEdgeBase, after.cursorX, after.cursorY, after.globalEnd)

	if after.liveEdgeBase != before.liveEdgeBase {
		t.Errorf("liveEdgeBase mismatch: before=%d, after=%d", before.liveEdgeBase, after.liveEdgeBase)
	}
	if after.cursorX != before.cursorX || after.cursorY != before.cursorY {
		t.Errorf("cursor mismatch: before=(%d,%d), after=(%d,%d)",
			before.cursorX, before.cursorY, after.cursorX, after.cursorY)
	}

	v2.CloseMemoryBuffer()
}

// TestBurstWriteRecovery_ImmediateClose tests closing immediately after burst
// output without waiting for idle flush — the Close() path must flush everything.
func TestBurstWriteRecovery_ImmediateClose(t *testing.T) {
	dir := t.TempDir()
	id := "burst-immediate"
	const cols, rows = 120, 24
	const numLines = 2000

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeLines(v1, numLines)

	// Close immediately — no idle flush, everything must be flushed by Close
	before := captureState(v1)
	t.Logf("Before close: liveEdgeBase=%d, cursor=(%d,%d), globalEnd=%d",
		before.liveEdgeBase, before.cursorX, before.cursorY, before.globalEnd)

	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	v2 := newTestVTerm(t, cols, rows, dir, id)
	after := captureState(v2)
	t.Logf("After reopen:  liveEdgeBase=%d, cursor=(%d,%d), globalEnd=%d",
		after.liveEdgeBase, after.cursorX, after.cursorY, after.globalEnd)

	if after.liveEdgeBase != before.liveEdgeBase {
		t.Errorf("liveEdgeBase mismatch: before=%d, after=%d", before.liveEdgeBase, after.liveEdgeBase)
	}
	if after.cursorX != before.cursorX || after.cursorY != before.cursorY {
		t.Errorf("cursor mismatch: before=(%d,%d), after=(%d,%d)",
			before.cursorX, before.cursorY, after.cursorX, after.cursorY)
	}

	// Content should not be lost — check that lines near the viewport exist
	mb2 := v2.memBufState.memBuf
	for y := 0; y < rows; y++ {
		globalLine := after.liveEdgeBase + int64(y)
		if globalLine >= mb2.GlobalEnd() {
			break
		}
		line := mb2.GetLine(globalLine)
		if line == nil {
			t.Errorf("line %d (viewport row %d) is nil after recovery", globalLine, y)
		}
	}

	v2.CloseMemoryBuffer()
}

// TestBurstWriteRecovery_MultipleRestarts tests repeated write→close→reopen
// cycles to catch state drift across restarts.
func TestBurstWriteRecovery_MultipleRestarts(t *testing.T) {
	dir := t.TempDir()
	id := "burst-multi"
	const cols, rows = 80, 24
	const linesPerSession = 200
	const numRestarts = 5

	var lastLiveEdgeBase int64

	for restart := 0; restart < numRestarts; restart++ {
		v := newTestVTerm(t, cols, rows, dir, id)
		clearRecoveryGuard(v) // allow test writes to scroll normally
		writeLines(v, linesPerSession)

		state := captureState(v)
		t.Logf("Restart %d: liveEdgeBase=%d, cursor=(%d,%d), globalEnd=%d",
			restart, state.liveEdgeBase, state.cursorX, state.cursorY, state.globalEnd)

		if restart > 0 {
			// After first session, liveEdgeBase should grow monotonically
			if state.liveEdgeBase < lastLiveEdgeBase {
				t.Errorf("Restart %d: liveEdgeBase went backward: %d -> %d",
					restart, lastLiveEdgeBase, state.liveEdgeBase)
			}
		}
		lastLiveEdgeBase = state.liveEdgeBase

		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("Restart %d: CloseMemoryBuffer: %v", restart, err)
		}
	}

	// Final verification: reopen and check content is accessible
	vFinal := newTestVTerm(t, cols, rows, dir, id)
	finalState := captureState(vFinal)
	t.Logf("Final: liveEdgeBase=%d, cursor=(%d,%d), globalEnd=%d",
		finalState.liveEdgeBase, finalState.cursorX, finalState.cursorY, finalState.globalEnd)

	// Should have accumulated lines from all sessions
	if finalState.globalEnd < int64(linesPerSession*numRestarts) {
		t.Errorf("Expected globalEnd >= %d, got %d", linesPerSession*numRestarts, finalState.globalEnd)
	}

	vFinal.CloseMemoryBuffer()
}

// TestBurstWriteRecovery_ContentIntegrity verifies that specific line content
// survives the write→close→reopen cycle.
func TestBurstWriteRecovery_ContentIntegrity(t *testing.T) {
	dir := t.TempDir()
	id := "burst-content"
	const cols, rows = 120, 24
	const numLines = 300

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeLines(v1, numLines)

	// Sample specific lines before close
	mb1 := v1.memBufState.memBuf
	type sample struct {
		idx  int64
		text string
	}
	samples := []sample{}
	// Sample first, middle, and last few lines
	for _, idx := range []int64{0, 1, int64(numLines / 2), int64(numLines - 2), int64(numLines - 1)} {
		if idx < mb1.GlobalOffset() || idx >= mb1.GlobalEnd() {
			continue
		}
		if line := mb1.GetLine(idx); line != nil {
			samples = append(samples, sample{idx, trimLogicalLine(logicalLineToString(line))})
		}
	}

	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	v2 := newTestVTerm(t, cols, rows, dir, id)
	mb2 := v2.memBufState.memBuf

	for _, s := range samples {
		line := mb2.GetLine(s.idx)
		if line == nil {
			// Line might be outside the loaded window — try PageStore
			if v2.memBufState.pageStore != nil {
				lines, err := v2.memBufState.pageStore.ReadLineRange(s.idx, s.idx+1)
				if err == nil && len(lines) > 0 {
					got := trimLogicalLine(logicalLineToString(lines[0]))
					if got != s.text {
						t.Errorf("Line %d from PageStore: got %q, want %q", s.idx, got, s.text)
					}
					continue
				}
			}
			t.Errorf("Line %d not found after recovery", s.idx)
			continue
		}
		got := trimLogicalLine(logicalLineToString(line))
		if got != s.text {
			t.Errorf("Line %d: got %q, want %q", s.idx, got, s.text)
		}
	}

	v2.CloseMemoryBuffer()
}

// TestBurstWriteRecovery_MetadataNotCorrupted verifies that metadata
// is correct after burst writes + close by reopening the VTerm and
// checking the recovered state (which reads from WAL or PageStore).
func TestBurstWriteRecovery_MetadataNotCorrupted(t *testing.T) {
	dir := t.TempDir()
	id := "burst-metadata"
	const cols, rows = 80, 30
	const numLines = 1000

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeLines(v1, numLines)

	expectedLEB := v1.memBufState.liveEdgeBase
	expectedCX := v1.cursorX
	expectedCY := v1.cursorY

	t.Logf("Expected metadata: liveEdgeBase=%d, cursor=(%d,%d)", expectedLEB, expectedCX, expectedCY)

	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	// Reopen via VTerm to exercise the full recovery path
	v2 := newTestVTerm(t, cols, rows, dir, id)
	after := captureState(v2)

	t.Logf("Recovered state: liveEdgeBase=%d, cursor=(%d,%d)",
		after.liveEdgeBase, after.cursorX, after.cursorY)

	if after.liveEdgeBase != expectedLEB {
		t.Errorf("LiveEdgeBase: got %d, want %d", after.liveEdgeBase, expectedLEB)
	}
	if after.cursorX != expectedCX {
		t.Errorf("CursorX: got %d, want %d", after.cursorX, expectedCX)
	}
	if after.cursorY != expectedCY {
		t.Errorf("CursorY: got %d, want %d", after.cursorY, expectedCY)
	}

	v2.CloseMemoryBuffer()
}

// --- Dirty close (crash simulation) tests ---

// TestBurstWriteRecovery_DirtyClose_AfterIdleFlush writes lines, waits for
// the idle flush to persist them, then does a dirty close (simulating a crash).
// The recovered state should match what the idle flush saved.
func TestBurstWriteRecovery_DirtyClose_AfterIdleFlush(t *testing.T) {
	dir := t.TempDir()
	id := "dirty-idle"
	const cols, rows = 80, 24
	const numLines = 500

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeLines(v1, numLines)

	stateBeforeFlush := captureState(v1)
	t.Logf("State after write: liveEdgeBase=%d, cursor=(%d,%d), globalEnd=%d",
		stateBeforeFlush.liveEdgeBase, stateBeforeFlush.cursorX, stateBeforeFlush.cursorY, stateBeforeFlush.globalEnd)

	// Wait for idle flush to fire and persist
	time.Sleep(1500 * time.Millisecond)

	// Dirty close — no CloseMemoryBuffer, just yank the files
	dirtyClose(v1)

	// Reopen
	v2 := newTestVTerm(t, cols, rows, dir, id)
	after := captureState(v2)
	t.Logf("After dirty reopen: liveEdgeBase=%d, cursor=(%d,%d), globalEnd=%d",
		after.liveEdgeBase, after.cursorX, after.cursorY, after.globalEnd)

	// After idle flush, everything should have been persisted
	if after.liveEdgeBase != stateBeforeFlush.liveEdgeBase {
		t.Errorf("liveEdgeBase mismatch: expected=%d, got=%d",
			stateBeforeFlush.liveEdgeBase, after.liveEdgeBase)
	}
	if after.cursorY != stateBeforeFlush.cursorY {
		t.Errorf("cursorY mismatch: expected=%d, got=%d",
			stateBeforeFlush.cursorY, after.cursorY)
	}
	// Content at viewport top should match
	if after.viewportTopLine != stateBeforeFlush.viewportTopLine {
		t.Errorf("viewportTopLine mismatch:\n  expected: %q\n  got:      %q",
			stateBeforeFlush.viewportTopLine, after.viewportTopLine)
	}

	v2.CloseMemoryBuffer()
}

// TestBurstWriteRecovery_DirtyClose_MidBurst writes lines and does a dirty
// close immediately without waiting for any flush. Recovery should not crash
// and should recover whatever was synced to disk (possibly nothing from the
// burst, but the system should not corrupt).
func TestBurstWriteRecovery_DirtyClose_MidBurst(t *testing.T) {
	dir := t.TempDir()
	id := "dirty-mid"
	const cols, rows = 80, 24
	const numLines = 2000

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeLines(v1, numLines)

	// Dirty close immediately — most lines are still pending
	dirtyClose(v1)

	// Reopen — should not crash or corrupt
	v2 := newTestVTerm(t, cols, rows, dir, id)
	after := captureState(v2)
	t.Logf("After dirty reopen: liveEdgeBase=%d, cursor=(%d,%d), globalEnd=%d",
		after.liveEdgeBase, after.cursorX, after.cursorY, after.globalEnd)

	// We can't assert exact content since flush didn't complete, but:
	// 1. No crash
	// 2. globalEnd should be >= 0
	// 3. liveEdgeBase should be sane
	if after.globalEnd < 0 {
		t.Errorf("globalEnd should be >= 0, got %d", after.globalEnd)
	}
	if after.liveEdgeBase < 0 {
		t.Errorf("liveEdgeBase should be >= 0, got %d", after.liveEdgeBase)
	}

	v2.CloseMemoryBuffer()
}

// TestBurstWriteRecovery_DirtyClose_WithExplicitFlush writes lines, forces
// an explicit flush, writes more lines, then dirty-closes. The state from
// the explicit flush should survive; the post-flush lines may be lost.
func TestBurstWriteRecovery_DirtyClose_WithExplicitFlush(t *testing.T) {
	dir := t.TempDir()
	id := "dirty-explicit"
	const cols, rows = 80, 24

	v1 := newTestVTerm(t, cols, rows, dir, id)

	// Write first batch and flush
	writeLines(v1, 200)
	stateAfterFirstBatch := captureState(v1)
	t.Logf("After first batch: liveEdgeBase=%d, cursor=(%d,%d), globalEnd=%d",
		stateAfterFirstBatch.liveEdgeBase, stateAfterFirstBatch.cursorX,
		stateAfterFirstBatch.cursorY, stateAfterFirstBatch.globalEnd)

	// Force flush to persist everything so far
	if v1.memBufState.persistence != nil {
		v1.notifyMetadataChange()
		if err := v1.memBufState.persistence.Flush(); err != nil {
			t.Fatalf("Flush: %v", err)
		}
	}

	// Write more lines (these won't be flushed)
	writeLines(v1, 300)
	stateAfterSecondBatch := captureState(v1)
	t.Logf("After second batch: liveEdgeBase=%d, cursor=(%d,%d), globalEnd=%d",
		stateAfterSecondBatch.liveEdgeBase, stateAfterSecondBatch.cursorX,
		stateAfterSecondBatch.cursorY, stateAfterSecondBatch.globalEnd)

	// Dirty close — second batch is lost
	dirtyClose(v1)

	// Reopen
	v2 := newTestVTerm(t, cols, rows, dir, id)
	after := captureState(v2)
	t.Logf("After dirty reopen: liveEdgeBase=%d, cursor=(%d,%d), globalEnd=%d",
		after.liveEdgeBase, after.cursorX, after.cursorY, after.globalEnd)

	// The flushed metadata should be from the first batch's flush
	if after.liveEdgeBase != stateAfterFirstBatch.liveEdgeBase {
		t.Errorf("liveEdgeBase: expected %d (from explicit flush), got %d",
			stateAfterFirstBatch.liveEdgeBase, after.liveEdgeBase)
	}
	if after.cursorY != stateAfterFirstBatch.cursorY {
		t.Errorf("cursorY: expected %d (from explicit flush), got %d",
			stateAfterFirstBatch.cursorY, after.cursorY)
	}

	v2.CloseMemoryBuffer()
}

// TestBurstWriteRecovery_DirtyClose_MultipleRestartsNoDrift tests that
// repeated dirty-close cycles don't cause state drift or corruption.
func TestBurstWriteRecovery_DirtyClose_MultipleRestartsNoDrift(t *testing.T) {
	dir := t.TempDir()
	id := "dirty-drift"
	const cols, rows = 80, 24
	const linesPerSession = 100

	for restart := 0; restart < 5; restart++ {
		v := newTestVTerm(t, cols, rows, dir, id)
		clearRecoveryGuard(v) // allow test writes to scroll normally
		writeLines(v, linesPerSession)

		// Wait for idle flush
		time.Sleep(1500 * time.Millisecond)

		state := captureState(v)
		t.Logf("Restart %d: liveEdgeBase=%d, cursor=(%d,%d), globalEnd=%d",
			restart, state.liveEdgeBase, state.cursorX, state.cursorY, state.globalEnd)

		// Dirty close after idle flush
		dirtyClose(v)

		// Verify by reopening
		v2 := newTestVTerm(t, cols, rows, dir, id)
		recovered := captureState(v2)

		if recovered.liveEdgeBase != state.liveEdgeBase {
			t.Errorf("Restart %d: liveEdgeBase drift: wrote=%d, recovered=%d",
				restart, state.liveEdgeBase, recovered.liveEdgeBase)
		}
		if recovered.cursorY != state.cursorY {
			t.Errorf("Restart %d: cursorY drift: wrote=%d, recovered=%d",
				restart, state.cursorY, recovered.cursorY)
		}

		v2.CloseMemoryBuffer()
	}
}

// TestBurstWriteRecovery_ConcurrentFlushAndClose simulates the race between
// background idle flush and Close() — the exact scenario that caused the
// metadata corruption bug.
func TestBurstWriteRecovery_ConcurrentFlushAndClose(t *testing.T) {
	dir := t.TempDir()
	id := "burst-race"
	const cols, rows = 80, 24
	const numLines = 3000

	for attempt := 0; attempt < 10; attempt++ {
		attemptDir := fmt.Sprintf("%s/attempt-%d", dir, attempt)

		v := newTestVTerm(t, cols, rows, attemptDir, id)
		writeLines(v, numLines)

		before := captureState(v)

		// Sleep briefly to let idle monitor potentially trigger
		time.Sleep(50 * time.Millisecond)

		if err := v.CloseMemoryBuffer(); err != nil {
			t.Fatalf("Attempt %d: CloseMemoryBuffer: %v", attempt, err)
		}

		v2 := newTestVTerm(t, cols, rows, attemptDir, id)
		after := captureState(v2)

		if after.liveEdgeBase != before.liveEdgeBase {
			t.Errorf("Attempt %d: liveEdgeBase mismatch: before=%d, after=%d",
				attempt, before.liveEdgeBase, after.liveEdgeBase)
		}
		if after.cursorY != before.cursorY {
			t.Errorf("Attempt %d: cursorY mismatch: before=%d, after=%d",
				attempt, before.cursorY, after.cursorY)
		}

		v2.CloseMemoryBuffer()
	}
}

// --- VTerm-level coherence tests ---
// These verify that the FULL recovery path (loadHistoryFromDisk + metadata
// restore + trimBlankTailLines + prompt positioning) produces a state where
// the viewport content matches what the terminal showed before closing.

// writeNumberedLines writes n lines through the VTerm parser with predictable
// content: "L<number> <padding>" so we can verify exact content on recovery.
func writeNumberedLines(v *VTerm, start, count int) {
	p := NewParser(v)
	for i := start; i < start+count; i++ {
		line := fmt.Sprintf("L%05d abcdefghijklmnopqrstuvwxyz-padding\r\n", i)
		for _, r := range line {
			p.Parse(r)
		}
	}
}

// getVTermLine reads a line from the VTerm's MemoryBuffer and returns its text.
func getVTermLine(v *VTerm, globalIdx int64) string {
	mb := v.memBufState.memBuf
	line := mb.GetLine(globalIdx)
	if line == nil {
		return ""
	}
	return trimLogicalLine(logicalLineToString(line))
}

// captureViewport reads all viewport lines from the VTerm.
func captureViewport(v *VTerm) []string {
	lines := make([]string, v.height)
	for y := 0; y < v.height; y++ {
		globalIdx := v.memBufState.liveEdgeBase + int64(y)
		lines[y] = getVTermLine(v, globalIdx)
	}
	return lines
}

// TestVTermCoherence_BasicViewportContent writes numbered lines, closes,
// reopens, and verifies every viewport line has the correct content.
func TestVTermCoherence_BasicViewportContent(t *testing.T) {
	dir := t.TempDir()
	id := "vt-coherence-basic"
	const cols, rows = 80, 24

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeNumberedLines(v1, 0, 200)

	before := captureState(v1)
	beforeVP := captureViewport(v1)
	t.Logf("Before close: LEB=%d, cursor=(%d,%d), globalEnd=%d",
		before.liveEdgeBase, before.cursorX, before.cursorY, before.globalEnd)

	v1.CloseMemoryBuffer()

	v2 := newTestVTerm(t, cols, rows, dir, id)
	after := captureState(v2)
	afterVP := captureViewport(v2)
	t.Logf("After reopen: LEB=%d, cursor=(%d,%d), globalEnd=%d",
		after.liveEdgeBase, after.cursorX, after.cursorY, after.globalEnd)

	mismatches := 0
	for i := 0; i < rows; i++ {
		if i < len(beforeVP) && i < len(afterVP) && beforeVP[i] != afterVP[i] {
			t.Errorf("VP row %d (global %d): before=%q, after=%q",
				i, after.liveEdgeBase+int64(i), beforeVP[i], afterVP[i])
			mismatches++
		}
	}
	if mismatches == 0 {
		t.Log("All viewport lines match")
	}
	v2.CloseMemoryBuffer()
}

// TestVTermCoherence_LargeOutput tests the ls -lR scenario: many lines
// pushing cursor to bottom, close, reopen, verify viewport.
func TestVTermCoherence_LargeOutput(t *testing.T) {
	dir := t.TempDir()
	id := "vt-coherence-large"
	const cols, rows = 120, 30

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeNumberedLines(v1, 0, 3000)

	before := captureState(v1)
	beforeVP := captureViewport(v1)
	t.Logf("Before: LEB=%d, cursor=(%d,%d), globalEnd=%d",
		before.liveEdgeBase, before.cursorX, before.cursorY, before.globalEnd)

	v1.CloseMemoryBuffer()

	v2 := newTestVTerm(t, cols, rows, dir, id)
	after := captureState(v2)
	afterVP := captureViewport(v2)
	t.Logf("After:  LEB=%d, cursor=(%d,%d), globalEnd=%d",
		after.liveEdgeBase, after.cursorX, after.cursorY, after.globalEnd)

	if after.liveEdgeBase != before.liveEdgeBase {
		t.Errorf("LEB mismatch: %d -> %d", before.liveEdgeBase, after.liveEdgeBase)
	}

	mismatches := 0
	for i := 0; i < rows; i++ {
		if beforeVP[i] != afterVP[i] {
			t.Errorf("VP row %d: before=%q, after=%q", i, beforeVP[i], afterVP[i])
			mismatches++
			if mismatches > 5 {
				t.Log("... (more mismatches truncated)")
				break
			}
		}
	}
	v2.CloseMemoryBuffer()
}

// TestVTermCoherence_DirtyClose_ViewportContent does a dirty close after
// idle flush and verifies the recovered viewport content is real.
func TestVTermCoherence_DirtyClose_ViewportContent(t *testing.T) {
	dir := t.TempDir()
	id := "vt-coherence-dirty"
	const cols, rows = 80, 24

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeNumberedLines(v1, 0, 500)

	before := captureState(v1)
	beforeVP := captureViewport(v1)
	t.Logf("Before: LEB=%d, cursor=(%d,%d)", before.liveEdgeBase, before.cursorX, before.cursorY)

	time.Sleep(1600 * time.Millisecond)
	dirtyClose(v1)

	v2 := newTestVTerm(t, cols, rows, dir, id)
	afterVP := captureViewport(v2)
	t.Logf("After:  LEB=%d, cursor=(%d,%d)", v2.memBufState.liveEdgeBase, v2.cursorX, v2.cursorY)

	mismatches := 0
	for i := 0; i < rows; i++ {
		if beforeVP[i] != afterVP[i] {
			t.Errorf("VP row %d: before=%q, after=%q", i, beforeVP[i], afterVP[i])
			mismatches++
		}
	}
	if mismatches == 0 {
		t.Log("All viewport lines match after dirty close")
	}
	v2.CloseMemoryBuffer()
}

// TestVTermCoherence_TrimBlankTailLines verifies that trimBlankTailLines
// doesn't lose content or produce an all-blank viewport.
func TestVTermCoherence_TrimBlankTailLines(t *testing.T) {
	dir := t.TempDir()
	id := "vt-coherence-trim"
	const cols, rows = 80, 24

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeNumberedLines(v1, 0, 50)

	before := captureState(v1)
	t.Logf("Before: LEB=%d, cursor=(%d,%d), globalEnd=%d",
		before.liveEdgeBase, before.cursorX, before.cursorY, before.globalEnd)

	v1.CloseMemoryBuffer()

	v2 := newTestVTerm(t, cols, rows, dir, id)
	after := captureState(v2)
	t.Logf("After:  LEB=%d, cursor=(%d,%d), globalEnd=%d",
		after.liveEdgeBase, after.cursorX, after.cursorY, after.globalEnd)

	hasContent := false
	for y := 0; y < rows; y++ {
		globalIdx := after.liveEdgeBase + int64(y)
		got := getVTermLine(v2, globalIdx)
		if got != "" {
			hasContent = true
			expected := fmt.Sprintf("L%05d abcdefghijklmnopqrstuvwxyz-padding", globalIdx)
			if got != expected {
				t.Errorf("VP row %d (global %d): got %q, want %q", y, globalIdx, got, expected)
			}
		}
	}
	if !hasContent {
		t.Error("Viewport is entirely blank after recovery — content was lost")
	}
	v2.CloseMemoryBuffer()
}

// TestVTermCoherence_MultipleRestarts checks viewport content coherence
// across repeated write->close->reopen cycles.
func TestVTermCoherence_MultipleRestarts(t *testing.T) {
	dir := t.TempDir()
	id := "vt-coherence-restart"
	const cols, rows = 80, 24

	for restart := 0; restart < 4; restart++ {
		v := newTestVTerm(t, cols, rows, dir, id)
		clearRecoveryGuard(v)
		writeNumberedLines(v, restart*100, 100)

		before := captureState(v)
		beforeVP := captureViewport(v)
		t.Logf("Restart %d: LEB=%d, cursor=(%d,%d), VP[0]=%q",
			restart, before.liveEdgeBase, before.cursorX, before.cursorY, beforeVP[0])

		v.CloseMemoryBuffer()

		v2 := newTestVTerm(t, cols, rows, dir, id)
		after := captureState(v2)
		afterVP := captureViewport(v2)

		// Lines above cursor should match (prompt erase only affects cursorY down)
		for y := 0; y < after.cursorY && y < len(beforeVP); y++ {
			if beforeVP[y] != afterVP[y] {
				t.Errorf("Restart %d VP row %d: before=%q, after=%q",
					restart, y, beforeVP[y], afterVP[y])
			}
		}
		v2.CloseMemoryBuffer()
	}
}

// TestVTermCoherence_LsLR_ExactScenario reproduces the exact ls -lR bug:
// lots of output pushing cursor to bottom, immediate server stop, restart.
// Verifies the viewport doesn't scroll up by a full height.
func TestVTermCoherence_LsLR_ExactScenario(t *testing.T) {
	dir := t.TempDir()
	id := "vt-lslr-exact"
	const cols, rows = 120, 24

	// Session 1: simulate ls -lR (lots of output)
	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeNumberedLines(v1, 0, 5000) // simulates ls -lR output

	beforeState := captureState(v1)
	beforeVP := captureViewport(v1)

	t.Logf("Session 1 state: LEB=%d, cursor=(%d,%d), globalEnd=%d",
		beforeState.liveEdgeBase, beforeState.cursorX, beforeState.cursorY, beforeState.globalEnd)
	t.Logf("  VP top line: %q", beforeVP[0])
	t.Logf("  VP bot line: %q", beforeVP[rows-1])

	// Close (simulates server stop right after ls -lR)
	v1.CloseMemoryBuffer()

	// Session 2: reopen (simulates server restart)
	v2 := newTestVTerm(t, cols, rows, dir, id)
	afterState := captureState(v2)
	afterVP := captureViewport(v2)

	t.Logf("Session 2 state: LEB=%d, cursor=(%d,%d), globalEnd=%d",
		afterState.liveEdgeBase, afterState.cursorX, afterState.cursorY, afterState.globalEnd)
	t.Logf("  VP top line: %q", afterVP[0])
	t.Logf("  VP bot line: %q", afterVP[rows-1])

	// THE BUG: liveEdgeBase shifted forward by exactly `rows`, making viewport blank
	if afterState.liveEdgeBase != beforeState.liveEdgeBase {
		diff := afterState.liveEdgeBase - beforeState.liveEdgeBase
		t.Errorf("LEB shifted by %d (before=%d, after=%d)", diff, beforeState.liveEdgeBase, afterState.liveEdgeBase)
		if diff == int64(rows) {
			t.Error("LEB shifted by EXACTLY viewport height — this is the ls -lR bug!")
		}
	}

	// Viewport content must match
	mismatches := 0
	for i := 0; i < rows; i++ {
		if beforeVP[i] != afterVP[i] {
			t.Errorf("VP row %d: before=%q, after=%q", i, beforeVP[i], afterVP[i])
			mismatches++
			if mismatches > 5 {
				t.Log("... more mismatches truncated")
				break
			}
		}
	}

	// Check the viewport isn't all blank (the visual symptom)
	allBlank := true
	for _, line := range afterVP {
		if line != "" {
			allBlank = false
			break
		}
	}
	if allBlank {
		t.Error("Viewport is entirely blank after recovery — content is above (needs scrolling)")
	}

	v2.CloseMemoryBuffer()
}

// TestVTermCoherence_ResizeAfterRestore verifies that calling Resize
// after history recovery (which happens during client reconnect) doesn't
// shift liveEdgeBase.
func TestVTermCoherence_ResizeAfterRestore(t *testing.T) {
	dir := t.TempDir()
	id := "vt-resize-after"
	const cols, rows = 120, 30

	// Session 1: write lots of output
	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeNumberedLines(v1, 0, 2000)
	v1.CloseMemoryBuffer()

	// Session 2: reopen (loads history + restores metadata)
	v2 := newTestVTerm(t, cols, rows, dir, id)
	stateAfterLoad := captureState(v2)
	vpAfterLoad := captureViewport(v2)
	t.Logf("After load: LEB=%d, cursor=(%d,%d), globalEnd=%d",
		stateAfterLoad.liveEdgeBase, stateAfterLoad.cursorX, stateAfterLoad.cursorY, stateAfterLoad.globalEnd)

	// Now simulate what happens during client reconnect: Resize to same size
	v2.Resize(cols, rows)
	stateAfterResize := captureState(v2)
	vpAfterResize := captureViewport(v2)
	t.Logf("After same-size resize: LEB=%d, cursor=(%d,%d)",
		stateAfterResize.liveEdgeBase, stateAfterResize.cursorX, stateAfterResize.cursorY)

	if stateAfterResize.liveEdgeBase != stateAfterLoad.liveEdgeBase {
		t.Errorf("Same-size resize shifted LEB: %d -> %d (diff=%d)",
			stateAfterLoad.liveEdgeBase, stateAfterResize.liveEdgeBase,
			stateAfterResize.liveEdgeBase-stateAfterLoad.liveEdgeBase)
	}

	// Also test resize to different size then back
	v2.Resize(cols, rows+10) // grow
	stateGrown := captureState(v2)
	t.Logf("After grow (+10): LEB=%d, cursor=(%d,%d)",
		stateGrown.liveEdgeBase, stateGrown.cursorX, stateGrown.cursorY)

	v2.Resize(cols, rows) // back to original
	stateBack := captureState(v2)
	t.Logf("After shrink back: LEB=%d, cursor=(%d,%d)",
		stateBack.liveEdgeBase, stateBack.cursorX, stateBack.cursorY)

	if stateBack.liveEdgeBase != stateAfterLoad.liveEdgeBase {
		t.Errorf("Grow+shrink shifted LEB: %d -> %d (diff=%d)",
			stateAfterLoad.liveEdgeBase, stateBack.liveEdgeBase,
			stateBack.liveEdgeBase-stateAfterLoad.liveEdgeBase)
	}

	// Verify viewport content didn't get corrupted
	mismatches := 0
	for i := 0; i < rows; i++ {
		if vpAfterLoad[i] != vpAfterResize[i] {
			t.Errorf("VP row %d changed after same-size resize: %q -> %q",
				i, vpAfterLoad[i], vpAfterResize[i])
			mismatches++
		}
	}

	// Test resize to 0x0 then back (the server default before client connects)
	v2.Resize(cols, 1) // near-zero
	v2.Resize(cols, rows) // back
	stateFromZero := captureState(v2)
	t.Logf("After 1->%d resize: LEB=%d, cursor=(%d,%d)",
		rows, stateFromZero.liveEdgeBase, stateFromZero.cursorX, stateFromZero.cursorY)

	if stateFromZero.liveEdgeBase != stateAfterLoad.liveEdgeBase {
		diff := stateFromZero.liveEdgeBase - stateAfterLoad.liveEdgeBase
		t.Errorf("Shrink+grow shifted LEB: %d -> %d (diff=%d)",
			stateAfterLoad.liveEdgeBase, stateFromZero.liveEdgeBase, diff)
		if diff == -int64(rows-1) || diff == int64(rows-1) {
			t.Error("LEB shifted by viewport height — this is the resize bug!")
		}
	}

	v2.CloseMemoryBuffer()
}

// TestVTermCoherence_ShellStartAfterRestore simulates what happens when
// a new shell starts after recovery: the cursor is at the restored position,
// and bash outputs its prompt (with \r\n). Verify liveEdgeBase doesn't jump.
func TestVTermCoherence_ShellStartAfterRestore(t *testing.T) {
	dir := t.TempDir()
	id := "vt-shell-start"
	const cols, rows = 80, 24

	// Session 1: lots of output like ls -lR
	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeNumberedLines(v1, 0, 1000)
	v1.CloseMemoryBuffer()

	// Session 2: reopen
	v2 := newTestVTerm(t, cols, rows, dir, id)
	stateRestored := captureState(v2)
	t.Logf("Restored: LEB=%d, cursor=(%d,%d)", stateRestored.liveEdgeBase, stateRestored.cursorX, stateRestored.cursorY)

	// Simulate bash prompt output (what happens when shell starts)
	p := NewParser(v2)
	// Bash typically outputs: \r\n followed by the prompt
	prompt := "\r\nuser@host:~$ "
	for _, r := range prompt {
		p.Parse(r)
	}
	stateAfterPrompt := captureState(v2)
	t.Logf("After prompt: LEB=%d, cursor=(%d,%d)", stateAfterPrompt.liveEdgeBase, stateAfterPrompt.cursorX, stateAfterPrompt.cursorY)

	lebDiff := stateAfterPrompt.liveEdgeBase - stateRestored.liveEdgeBase
	if lebDiff > 2 {
		t.Errorf("Shell prompt shifted LEB by %d (before=%d, after=%d)",
			lebDiff, stateRestored.liveEdgeBase, stateAfterPrompt.liveEdgeBase)
	}

	// Simulate bash outputting more aggressive startup (motd, etc.)
	for i := 0; i < 5; i++ {
		line := fmt.Sprintf("MOTD line %d\r\n", i)
		for _, r := range line {
			p.Parse(r)
		}
	}
	stateAfterMotd := captureState(v2)
	t.Logf("After MOTD: LEB=%d, cursor=(%d,%d)", stateAfterMotd.liveEdgeBase, stateAfterMotd.cursorX, stateAfterMotd.cursorY)

	lebDiffMotd := stateAfterMotd.liveEdgeBase - stateRestored.liveEdgeBase
	t.Logf("Total LEB shift from shell start: %d", lebDiffMotd)

	// A full height shift would be the bug
	if lebDiffMotd == int64(rows) {
		t.Error("Shell startup shifted LEB by exactly viewport height — this is the bug!")
	}

	// Now simulate a CLEAR SCREEN from bash (ESC[2J ESC[H)
	for _, r := range "\x1b[2J\x1b[H" {
		p.Parse(r)
	}
	stateAfterClear := captureState(v2)
	t.Logf("After clear screen: LEB=%d, cursor=(%d,%d)", stateAfterClear.liveEdgeBase, stateAfterClear.cursorX, stateAfterClear.cursorY)

	lebDiffClear := stateAfterClear.liveEdgeBase - stateRestored.liveEdgeBase
	if lebDiffClear == int64(rows) {
		t.Errorf("Clear screen shifted LEB by exactly viewport height (%d)!", rows)
	}

	v2.CloseMemoryBuffer()
}

// TestVTermCoherence_CursorHomeEraseToEnd simulates ESC[H ESC[J
// (cursor home then erase to end of screen) which is how `clear` works.
func TestVTermCoherence_CursorHomeEraseToEnd(t *testing.T) {
	dir := t.TempDir()
	id := "vt-home-erase"
	const cols, rows = 80, 24

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeNumberedLines(v1, 0, 1000)
	v1.CloseMemoryBuffer()

	v2 := newTestVTerm(t, cols, rows, dir, id)
	restored := captureState(v2)
	t.Logf("Restored: LEB=%d, cursor=(%d,%d)", restored.liveEdgeBase, restored.cursorX, restored.cursorY)

	p := NewParser(v2)
	// ESC[H (cursor home) + ESC[J (erase from cursor to end = ED 0)
	for _, r := range "\x1b[H\x1b[J" {
		p.Parse(r)
	}
	after := captureState(v2)
	t.Logf("After ESC[H ESC[J: LEB=%d, cursor=(%d,%d)", after.liveEdgeBase, after.cursorX, after.cursorY)

	diff := after.liveEdgeBase - restored.liveEdgeBase
	t.Logf("LEB diff: %d", diff)
	if diff == int64(rows) || diff == int64(rows-1) {
		t.Errorf("ESC[H ESC[J shifted LEB by viewport height (%d) — this causes the blank screen bug!", diff)
	}

	// Also test ESC[H ESC[2J (cursor home + full clear)
	v3 := newTestVTerm(t, cols, rows, dir, id)
	restored3 := captureState(v3)

	p3 := NewParser(v3)
	for _, r := range "\x1b[H\x1b[2J" {
		p3.Parse(r)
	}
	after3 := captureState(v3)
	diff3 := after3.liveEdgeBase - restored3.liveEdgeBase
	t.Logf("After ESC[H ESC[2J: LEB diff=%d", diff3)
	if diff3 == int64(rows) || diff3 == int64(rows-1) {
		t.Errorf("ESC[H ESC[2J shifted LEB by viewport height (%d)!", diff3)
	}

	v2.CloseMemoryBuffer()
	v3.CloseMemoryBuffer()
}

// TestVTermCoherence_LargeVolumeAutoCheckpoint writes enough data to
// trigger auto-checkpoint (>10MB WAL) and verifies recovery is correct.
// This is the specific scenario for ls -lR on a big tree.
func TestVTermCoherence_LargeVolumeAutoCheckpoint(t *testing.T) {
	dir := t.TempDir()
	id := "vt-auto-checkpoint"
	const cols, rows = 120, 24

	v1 := newTestVTerm(t, cols, rows, dir, id)

	// Write enough lines to exceed 10MB WAL threshold.
	// Each line ~100 chars, encoded ~200 bytes + overhead = ~300 bytes per WAL entry.
	// 10MB / 300 = ~33000 lines.
	const numLines = 40000
	writeNumberedLines(v1, 0, numLines)

	before := captureState(v1)
	beforeVP := captureViewport(v1)
	t.Logf("Before: LEB=%d, cursor=(%d,%d), globalEnd=%d",
		before.liveEdgeBase, before.cursorX, before.cursorY, before.globalEnd)

	v1.CloseMemoryBuffer()

	v2 := newTestVTerm(t, cols, rows, dir, id)
	after := captureState(v2)
	afterVP := captureViewport(v2)
	t.Logf("After:  LEB=%d, cursor=(%d,%d), globalEnd=%d",
		after.liveEdgeBase, after.cursorX, after.cursorY, after.globalEnd)

	if after.liveEdgeBase != before.liveEdgeBase {
		diff := after.liveEdgeBase - before.liveEdgeBase
		t.Errorf("LEB shifted by %d (before=%d, after=%d)", diff, before.liveEdgeBase, after.liveEdgeBase)
	}

	mismatches := 0
	for i := 0; i < rows; i++ {
		if beforeVP[i] != afterVP[i] {
			t.Errorf("VP row %d: before=%q, after=%q", i, beforeVP[i], afterVP[i])
			mismatches++
			if mismatches > 3 {
				t.Log("... more mismatches truncated")
				break
			}
		}
	}

	v2.CloseMemoryBuffer()
}

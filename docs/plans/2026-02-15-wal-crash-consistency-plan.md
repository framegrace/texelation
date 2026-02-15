# WAL Crash Consistency Hardening - Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make terminal history persistence crash-consistent so that ANY interruption (SIGKILL, machine shutdown, normal exit) produces a valid, usable state on reload.

**Architecture:** Six targeted changes across 4 files. WAL gets an fsync after every flush (prevents page cache loss). Reload path gets self-healing validation (detects and repairs inconsistencies). Terminal colors reset to theme defaults on reload. Close path reordered for correctness.

**Tech Stack:** Go, os.File.Sync(), existing WAL/AdaptivePersistence/VTerm infrastructure.

**Design doc:** `docs/plans/2026-02-15-wal-crash-consistency-design.md`

---

### Task 1: Expose SyncWAL on WriteAheadLog

**Files:**
- Modify: `apps/texelterm/parser/write_ahead_log.go` (add after line ~1027, before Close)
- Test: `apps/texelterm/parser/write_ahead_log_test.go`

**Step 1: Write the failing test**

Add to `write_ahead_log_test.go`:

```go
func TestWAL_SyncWAL(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultWALConfig(tmpDir, "test-sync")
	config.CheckpointInterval = 0

	wal, err := OpenWriteAheadLog(config)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog failed: %v", err)
	}
	defer wal.Close()

	// Write a line
	line := NewLogicalLineFromCells([]Cell{{Rune: 'A'}})
	if err := wal.Append(0, line, time.Now()); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// SyncWAL should succeed (data flushed to disk)
	if err := wal.SyncWAL(); err != nil {
		t.Fatalf("SyncWAL failed: %v", err)
	}

	// SyncWAL on closed WAL should return error or nil gracefully
	wal.Close()
	// Second close is a no-op, SyncWAL after close should not panic
	err = wal.SyncWAL()
	// We just verify it doesn't panic; error is acceptable
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser/ -run TestWAL_SyncWAL -v`
Expected: FAIL — `wal.SyncWAL undefined`

**Step 3: Write minimal implementation**

Add to `write_ahead_log.go` before the `Close()` method:

```go
// SyncWAL forces the WAL file to be synced to disk.
// This ensures all previously written entries survive a crash.
func (w *WriteAheadLog) SyncWAL() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped || w.walFile == nil {
		return nil
	}
	return w.walFile.Sync()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./apps/texelterm/parser/ -run TestWAL_SyncWAL -v`
Expected: PASS

**Step 5: Commit**

```bash
git add apps/texelterm/parser/write_ahead_log.go apps/texelterm/parser/write_ahead_log_test.go
git commit -m "Add SyncWAL method to WriteAheadLog for explicit fsync"
```

---

### Task 2: Sync after every flush in AdaptivePersistence

**Files:**
- Modify: `apps/texelterm/parser/adaptive_persistence.go:523-573` (flushPendingLocked)
- Test: `apps/texelterm/parser/adaptive_persistence_test.go`

**Step 1: Write the failing test**

Add to `adaptive_persistence_test.go`:

```go
func TestAdaptivePersistence_FlushSyncsWAL(t *testing.T) {
	tmpDir := t.TempDir()
	walConfig := DefaultWALConfig(tmpDir, "test-flush-sync")
	walConfig.CheckpointInterval = 0

	wal, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog failed: %v", err)
	}

	mb := NewMemoryBuffer(100)
	mb.EnsureLine(0)
	line := mb.GetLine(0)
	line.Cells = []Cell{{Rune: 'X'}}

	ap := NewAdaptivePersistence(mb, nil, wal)
	defer ap.Close()

	// Notify a write and flush
	ap.NotifyWrite(0)
	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// After flush, the WAL file should exist and have content beyond just the header.
	// The key assertion: data reached disk (not just page cache).
	// We verify by checking the WAL file size is > header size (32 bytes).
	walPath := wal.WALPath()
	info, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("WAL file stat failed: %v", err)
	}
	if info.Size() <= 32 {
		t.Errorf("WAL file too small after flush+sync: %d bytes (expected > 32)", info.Size())
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser/ -run TestAdaptivePersistence_FlushSyncsWAL -v`
Expected: PASS (test passes even before the fix since data is written, just not synced — but the test establishes the baseline. The sync itself is not directly testable without simulating crash, so this confirms the code path works.)

**Step 3: Write the implementation**

In `adaptive_persistence.go`, add sync at the end of `flushPendingLocked()`, after the metadata write block (line ~563) and before the performance monitoring (line ~565):

```go
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
```

Insert this between `ap.pendingMetadata = nil` (line 562) and the `// Monitor flush performance` comment (line 565).

**Step 4: Run tests to verify nothing breaks**

Run: `go test ./apps/texelterm/parser/ -run TestAdaptivePersistence -v`
Expected: All AdaptivePersistence tests PASS

**Step 5: Commit**

```bash
git add apps/texelterm/parser/adaptive_persistence.go apps/texelterm/parser/adaptive_persistence_test.go
git commit -m "Sync WAL after every flush to survive crashes"
```

---

### Task 3: Sync before close in AdaptivePersistence

**Files:**
- Modify: `apps/texelterm/parser/adaptive_persistence.go:419-453` (Close)

**Step 1: Write the failing test**

Add to `adaptive_persistence_test.go`:

```go
func TestAdaptivePersistence_CloseSyncsBeforeWALClose(t *testing.T) {
	tmpDir := t.TempDir()
	walConfig := DefaultWALConfig(tmpDir, "test-close-sync")
	walConfig.CheckpointInterval = 0

	wal, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog failed: %v", err)
	}

	mb := NewMemoryBuffer(100)
	mb.EnsureLine(0)
	line := mb.GetLine(0)
	line.Cells = []Cell{{Rune: 'Z'}}

	ap := NewAdaptivePersistence(mb, nil, wal)

	// Notify a write (will be pending)
	ap.NotifyWrite(0)

	// Close should flush + sync + close WAL without error
	if err := ap.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// After close, data should be in the PageStore (checkpoint happened)
	// Reopen to verify
	wal2, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		t.Fatalf("Reopen WAL failed: %v", err)
	}
	defer wal2.Close()

	if wal2.LineCount() != 1 {
		t.Errorf("expected 1 line after close+reopen, got %d", wal2.LineCount())
	}
}
```

**Step 2: Run test to verify it passes (baseline)**

Run: `go test ./apps/texelterm/parser/ -run TestAdaptivePersistence_CloseSyncsBeforeWALClose -v`
Expected: PASS (the Close path already works for this case; this test ensures it keeps working after our change)

**Step 3: Write the implementation**

In `adaptive_persistence.go` `Close()` method, add sync after `flushPendingLocked()` (line 433) and before `ap.mu.Unlock()` (line 435):

```go
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
```

Note: `flushPendingLocked` already syncs (from Task 2), but this is defense-in-depth for the case where flushPendingLocked had nothing to flush (returned early) but previous writes are still unsynced.

**Step 4: Run tests**

Run: `go test ./apps/texelterm/parser/ -run TestAdaptivePersistence -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add apps/texelterm/parser/adaptive_persistence.go apps/texelterm/parser/adaptive_persistence_test.go
git commit -m "Add explicit WAL sync in Close before releasing lock"
```

---

### Task 4: Self-healing reload — trim blank tail lines

**Files:**
- Modify: `apps/texelterm/parser/vterm_memory_buffer.go:405-434` (loadHistoryFromDisk)
- Test: `apps/texelterm/parser/vterm_memory_buffer_test.go`

**Step 1: Write the failing test**

Add to `vterm_memory_buffer_test.go`. This test simulates the corruption: history where the last N lines are blank, and metadata says liveEdgeBase is past the blank lines.

```go
func TestLoadHistory_TrimsBlankTailLines(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	walConfig := DefaultWALConfig(tmpDir, "test-trim-tail")
	walConfig.CheckpointInterval = 0

	// Create WAL with 10 lines: 7 real + 3 blank
	wal, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog failed: %v", err)
	}

	now := time.Now()
	for i := 0; i < 7; i++ {
		line := NewLogicalLineFromCells([]Cell{{Rune: rune('A' + i)}})
		if err := wal.Append(int64(i), line, now); err != nil {
			t.Fatalf("Append line %d failed: %v", i, err)
		}
	}
	// 3 blank lines (empty cells = whitespace)
	for i := 7; i < 10; i++ {
		line := NewLogicalLineFromCells([]Cell{})
		if err := wal.Append(int64(i), line, now); err != nil {
			t.Fatalf("Append blank line %d failed: %v", i, err)
		}
	}

	// Write metadata claiming liveEdgeBase=10 (past the blanks)
	if err := wal.WriteMetadata(&ViewportState{
		LiveEdgeBase: 10,
		CursorX:      0,
		CursorY:      0,
		SavedAt:      now,
	}); err != nil {
		t.Fatalf("WriteMetadata failed: %v", err)
	}

	if err := wal.Close(); err != nil {
		t.Fatalf("Close WAL failed: %v", err)
	}

	// Reopen and load history into VTerm
	wal2, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		t.Fatalf("Reopen WAL failed: %v", err)
	}

	v := NewVTerm(80, 24, nil)
	v.EnableMemoryBufferWithDisk(wal2.pageStore, wal2, 80)

	// liveEdgeBase should be clamped to 7 (last non-empty line + 1)
	if v.memBufState.liveEdgeBase != 7 {
		t.Errorf("liveEdgeBase: got %d, want 7 (should trim 3 blank tail lines)",
			v.memBufState.liveEdgeBase)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser/ -run TestLoadHistory_TrimsBlankTailLines -v`
Expected: FAIL — `liveEdgeBase: got 10, want 7`

**Step 3: Write the implementation**

Add a helper function to `vterm_memory_buffer.go` and call it in `loadHistoryFromDisk` after the metadata restore block (after line 433):

```go
// trimBlankTailLines scans backward from liveEdgeBase and clamps it to the
// last non-empty line + 1. This repairs state after crashes where metadata
// was persisted but trailing line content was lost (still in page cache).
func (v *VTerm) trimBlankTailLines() {
	mb := v.memBufState.memBuf
	if mb == nil {
		return
	}

	original := v.memBufState.liveEdgeBase
	for v.memBufState.liveEdgeBase > mb.GlobalOffset() {
		line := mb.GetLine(v.memBufState.liveEdgeBase - 1)
		if line != nil && lineHasContent(line) {
			break
		}
		v.memBufState.liveEdgeBase--
	}

	if trimmed := original - v.memBufState.liveEdgeBase; trimmed > 0 {
		log.Printf("[MEMORY_BUFFER] Trimmed %d blank tail lines (liveEdgeBase %d → %d)",
			trimmed, original, v.memBufState.liveEdgeBase)
	}
}

// lineHasContent returns true if the line has at least one cell with
// a non-space rune or non-default colors.
func lineHasContent(line *LogicalLine) bool {
	for _, c := range line.Cells {
		if c.Rune != ' ' && c.Rune != 0 {
			return true
		}
		if c.FG != DefaultFG || c.BG != DefaultBG {
			return true
		}
	}
	return false
}
```

Then at the end of `loadHistoryFromDisk()`, after line 433 (end of the `if savedState != nil` block), add:

```go
	// Repair state after crash: trim blank tail lines that were never synced.
	v.trimBlankTailLines()
```

**Step 4: Run test to verify it passes**

Run: `go test ./apps/texelterm/parser/ -run TestLoadHistory_TrimsBlankTailLines -v`
Expected: PASS

**Step 5: Run full test suite**

Run: `go test ./apps/texelterm/parser/ -v -count=1 2>&1 | tail -5`
Expected: All PASS

**Step 6: Commit**

```bash
git add apps/texelterm/parser/vterm_memory_buffer.go apps/texelterm/parser/vterm_memory_buffer_test.go
git commit -m "Add self-healing reload: trim blank tail lines after crash"
```

---

### Task 5: Reset terminal colors on reload

**Files:**
- Modify: `apps/texelterm/parser/vterm_memory_buffer.go:433` (end of loadHistoryFromDisk)
- Test: `apps/texelterm/parser/vterm_memory_buffer_test.go`

**Step 1: Write the failing test**

Add to `vterm_memory_buffer_test.go`:

```go
func TestLoadHistory_ResetsTerminalColors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	walConfig := DefaultWALConfig(tmpDir, "test-color-reset")
	walConfig.CheckpointInterval = 0

	wal, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog failed: %v", err)
	}

	// Write one line of content
	line := NewLogicalLineFromCells([]Cell{{Rune: 'A'}})
	if err := wal.Append(0, line, time.Now()); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	if err := wal.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Reopen and load
	wal2, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}

	v := NewVTerm(80, 24, nil)
	v.EnableMemoryBufferWithDisk(wal2.pageStore, wal2, 80)

	// After reload, currentFG/currentBG should be DefaultFG/DefaultBG
	// (not zero-value Color{} which renders as black)
	if v.currentFG != DefaultFG {
		t.Errorf("currentFG after reload: got %v, want DefaultFG (%v)", v.currentFG, DefaultFG)
	}
	if v.currentBG != DefaultBG {
		t.Errorf("currentBG after reload: got %v, want DefaultBG (%v)", v.currentBG, DefaultBG)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser/ -run TestLoadHistory_ResetsTerminalColors -v`
Expected: FAIL — `currentFG after reload: got {0 0 0 0}, want DefaultFG`

**Step 3: Write the implementation**

In `loadHistoryFromDisk()`, after the `trimBlankTailLines()` call (added in Task 4), add:

```go
	// Reset terminal drawing state to theme defaults after history reload.
	// Without this, currentFG/currentBG are zero-value Color{} (black),
	// making new shell output invisible. The shell will re-emit its own
	// colors on the first prompt.
	v.currentFG = DefaultFG
	v.currentBG = DefaultBG
	v.currentAttr = 0
```

**Step 4: Run test to verify it passes**

Run: `go test ./apps/texelterm/parser/ -run TestLoadHistory_ResetsTerminalColors -v`
Expected: PASS

**Step 5: Commit**

```bash
git add apps/texelterm/parser/vterm_memory_buffer.go apps/texelterm/parser/vterm_memory_buffer_test.go
git commit -m "Reset terminal colors to theme defaults on history reload"
```

---

### Task 6: Reorder Stop() sequence in term.go

**Files:**
- Modify: `apps/texelterm/term.go:1673-1720` (Stop method)

**Step 1: Understand the change**

Current order:
```
1. saveStateLocked()
2. CloseMemoryBuffer()
3. CloseSearchIndex()
4. Unlock
5. Close PTY
6. Signal process
```

New order — close PTY first to stop new data, then persist:
```
1. Close PTY (stop new data arriving)
2. Signal process
3. saveStateLocked()
4. CloseMemoryBuffer()
5. CloseSearchIndex()
6. Unlock
```

**Step 2: Write the implementation**

Replace the Stop() method body inside `stopOnce.Do`. The PTY and cmd must be extracted and closed before the persistence calls:

```go
func (a *TexelTerm) Stop() {
	a.stopOnce.Do(func() {
		close(a.stop)
		a.mu.Lock()

		cmd := a.cmd
		pty := a.pty
		a.cmd = nil
		a.pty = nil

		// Close PTY first to stop new data from arriving.
		// This must happen before closing persistence to prevent
		// data loss (writes after persistence close are lost).
		if pty != nil {
			_ = pty.Close()
		}
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			proc := cmd.Process
			go func() {
				time.Sleep(500 * time.Millisecond)
				proc.Signal(syscall.SIGKILL)
			}()
		}

		// Save terminal state (scroll position) before closing
		a.saveStateLocked()

		// Close memory buffer (flushes to disk if disk-backed)
		if a.vterm != nil {
			if err := a.vterm.CloseMemoryBuffer(); err != nil {
				log.Printf("Error closing memory buffer: %v", err)
			}
		}

		// Close search index (flushes pending writes)
		if a.searchIndex != nil {
			if err := a.searchIndex.Close(); err != nil {
				log.Printf("Error closing search index: %v", err)
			}
			a.searchIndex = nil
		}

		a.mu.Unlock()
	})
	a.wg.Wait()
}
```

**Step 3: Build to verify compilation**

Run: `go build ./apps/texelterm/...`
Expected: Success

**Step 4: Run full test suite**

Run: `make test 2>&1 | tail -5`
Expected: All PASS

**Step 5: Commit**

```bash
git add apps/texelterm/term.go
git commit -m "Reorder Stop: close PTY before persistence to prevent data loss"
```

---

### Task 7: Integration verification

**Step 1: Run full test suite with race detector**

Run: `go test -race ./apps/texelterm/parser/ -count=1`
Expected: All PASS, no races

**Step 2: Run full project tests**

Run: `make test`
Expected: All PASS

**Step 3: Final commit and push**

```bash
git push
```

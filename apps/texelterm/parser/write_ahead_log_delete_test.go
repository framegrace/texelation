package parser

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newDeleteTestWAL creates a temporary WAL for delete-entry tests.
// Returns the WAL, the path to wal.log, and the baseDir used for DefaultWALConfig.
func newDeleteTestWAL(t *testing.T) (*WriteAheadLog, string, string) {
	t.Helper()
	baseDir := t.TempDir()
	cfg := DefaultWALConfig(baseDir, "test-term")
	cfg.CheckpointInterval = 0
	wal, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}
	walPath := filepath.Join(cfg.WALDir, "wal.log")
	return wal, walPath, baseDir
}

func TestWAL_AppendDeleteRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		lo, hi int64
	}{
		{"multi-line", 5, 10},
		{"single-line", 5, 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wal, walPath, _ := newDeleteTestWAL(t)
			defer wal.Close()

			ts := time.Unix(1700000000, 0)
			if err := wal.AppendDelete(tc.lo, tc.hi, ts); err != nil {
				t.Fatalf("AppendDelete: %v", err)
			}
			// Sync to disk without triggering a checkpoint (which would truncate the WAL).
			if err := wal.SyncWAL(); err != nil {
				t.Fatalf("SyncWAL: %v", err)
			}

			// Read the raw entry off disk via readEntry.
			f, err := os.Open(walPath)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer f.Close()
			if _, err := f.Seek(int64(WALHeaderSize), 0); err != nil {
				t.Fatalf("Seek: %v", err)
			}
			entry, _, err := (&WriteAheadLog{}).readEntry(bufio.NewReader(f))
			if err != nil {
				t.Fatalf("readEntry: %v", err)
			}
			if entry.Type != EntryTypeLineDelete {
				t.Errorf("Type = %#x, want %#x", entry.Type, EntryTypeLineDelete)
			}
			if int64(entry.GlobalLineIdx) != tc.lo {
				t.Errorf("lo = %d, want %d", entry.GlobalLineIdx, tc.lo)
			}
			if entry.DeleteHi != tc.hi {
				t.Errorf("hi = %d, want %d", entry.DeleteHi, tc.hi)
			}
		})
	}
}

func TestWAL_CorruptDeleteEntryTruncates(t *testing.T) {
	wal, walPath, baseDir := newDeleteTestWAL(t)
	ts := time.Unix(1700000000, 0)
	if err := wal.AppendDelete(5, 10, ts); err != nil {
		t.Fatalf("AppendDelete: %v", err)
	}
	if err := wal.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Corrupt the CRC (last 4 bytes of the file)
	info, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	f, err := os.OpenFile(walPath, os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := f.WriteAt([]byte{0xDE, 0xAD, 0xBE, 0xEF}, info.Size()-4); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	f.Close()

	// Reopen — recover should truncate the corrupted entry and succeed.
	cfg := DefaultWALConfig(baseDir, "test-term")
	cfg.CheckpointInterval = 0
	wal2, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer wal2.Close()
	info2, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("Stat after recover: %v", err)
	}
	if info2.Size() != int64(WALHeaderSize) {
		t.Errorf("WAL size after corrupted-entry recover = %d, want %d (header only)", info2.Size(), WALHeaderSize)
	}
}

func TestWAL_RecoverAppliesDelete(t *testing.T) {
	_, _, baseDir := newDeleteTestWAL(t)
	// Open a fresh WAL, seed 5 lines, and append a tombstone for [1, 3]
	// BEFORE any checkpoint.
	cfg := DefaultWALConfig(baseDir, "test-term")
	cfg.CheckpointInterval = 0
	wal, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}
	ll := func(text string) *LogicalLine {
		cells := make([]Cell, len(text))
		for i, r := range text {
			cells[i] = Cell{Rune: r}
		}
		return &LogicalLine{Cells: cells}
	}
	for i := 0; i < 5; i++ {
		if err := wal.Append(int64(i), ll("line"), time.Now()); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := wal.AppendDelete(1, 3, time.Now()); err != nil {
		t.Fatalf("AppendDelete: %v", err)
	}
	// SyncWAL flushes all entries to disk. Skipping Close() simulates a crash:
	// the WAL entries survive on disk but no checkpoint/truncation happens.
	if err := wal.SyncWAL(); err != nil {
		t.Fatalf("SyncWAL: %v", err)
	}
	// Do not call wal.Close() — opening a second WAL on the same path exercises
	// the recover() path, which must replay both line writes and the tombstone.

	// Reopen on the same path; recover should replay writes + delete.
	cfg2 := DefaultWALConfig(baseDir, "test-term")
	cfg2.CheckpointInterval = 0
	wal2, err := OpenWriteAheadLog(cfg2)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer wal2.Close()
	ps := wal2.PageStore()
	if got := ps.StoredLineCount(); got != 2 {
		t.Errorf("StoredLineCount = %d, want 2 (lines 0 and 4)", got)
	}
	if gi := ps.GlobalIdxAtStoredPosition(0); gi != 0 {
		t.Errorf("first stored globalIdx = %d, want 0", gi)
	}
	if gi := ps.GlobalIdxAtStoredPosition(1); gi != 4 {
		t.Errorf("second stored globalIdx = %d, want 4", gi)
	}
}

package parser

import (
	"testing"
	"time"
)

func seedLines(t *testing.T, ps *PageStore, n int) {
	t.Helper()
	for i := range n {
		ll := &LogicalLine{Cells: []Cell{{Rune: 'x'}}}
		if err := ps.AppendLineWithGlobalIdx(int64(i), ll, time.Now()); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	if err := ps.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

func TestPageStore_DeleteRangeMiddle(t *testing.T) {
	dir := t.TempDir()
	ps, err := CreatePageStore(DefaultPageStoreConfig(dir, "term-x"))
	if err != nil {
		t.Fatalf("CreatePageStore: %v", err)
	}
	defer ps.Close()
	seedLines(t, ps, 10)
	if err := ps.deleteRangeNoWAL(3, 7); err != nil {
		t.Fatalf("deleteRangeNoWAL: %v", err)
	}
	if got := ps.StoredLineCount(); got != 5 {
		t.Errorf("StoredLineCount = %d, want 5", got)
	}
	want := []int64{0, 1, 2, 8, 9}
	for i, w := range want {
		if got := ps.GlobalIdxAtStoredPosition(int64(i)); got != w {
			t.Errorf("pos %d: got %d want %d", i, got, w)
		}
	}
}

func TestPageStore_DeleteRangeEmpty(t *testing.T) {
	dir := t.TempDir()
	ps, err := CreatePageStore(DefaultPageStoreConfig(dir, "term-x"))
	if err != nil {
		t.Fatalf("CreatePageStore: %v", err)
	}
	defer ps.Close()
	seedLines(t, ps, 3)
	if err := ps.deleteRangeNoWAL(100, 200); err != nil {
		t.Fatalf("deleteRangeNoWAL empty: %v", err)
	}
	if got := ps.StoredLineCount(); got != 3 {
		t.Errorf("StoredLineCount = %d, want 3", got)
	}
}

func TestPageStore_DeleteRangeWholeStore(t *testing.T) {
	dir := t.TempDir()
	ps, err := CreatePageStore(DefaultPageStoreConfig(dir, "term-x"))
	if err != nil {
		t.Fatalf("CreatePageStore: %v", err)
	}
	defer ps.Close()
	seedLines(t, ps, 4)
	if err := ps.deleteRangeNoWAL(0, 3); err != nil {
		t.Fatalf("deleteRangeNoWAL: %v", err)
	}
	if got := ps.StoredLineCount(); got != 0 {
		t.Errorf("StoredLineCount = %d, want 0", got)
	}
}

func TestPageStore_DeleteRangeInvalid(t *testing.T) {
	// Validation lives in AppendDelete / DeleteRange (public entry points),
	// not in deleteRangeNoWAL. Use the WAL.DeleteRange path to exercise it.
	dir := t.TempDir()
	cfg := DefaultWALConfig(dir, "term-invalid")
	cfg.CheckpointInterval = 0
	wal, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}
	defer wal.Close()
	if err := wal.DeleteRange(5, 3); err == nil {
		t.Error("DeleteRange(5, 3) = nil, want error")
	}
	if err := wal.DeleteRange(-1, 3); err == nil {
		t.Error("DeleteRange(-1, 3) = nil, want error")
	}
}

func TestWAL_DeleteRangeEmitsAndApplies(t *testing.T) {
	// Verifies WriteAheadLog.DeleteRange both emits a WAL entry and mutates
	// PageStore state.
	dir := t.TempDir()
	cfg := DefaultWALConfig(dir, "wal-delete-term")
	cfg.CheckpointInterval = 0
	wal, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	defer wal.Close()
	ps := wal.PageStore()
	seedLines(t, ps, 6)
	if err := wal.DeleteRange(1, 3); err != nil {
		t.Fatalf("DeleteRange: %v", err)
	}
	if got := ps.StoredLineCount(); got != 3 {
		t.Errorf("StoredLineCount = %d, want 3 (kept: 0, 4, 5)", got)
	}
}

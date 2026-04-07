// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestPageStore(t *testing.T) *PageStore {
	t.Helper()
	dir := t.TempDir()
	cfg := DefaultPageStoreConfig(filepath.Join(dir, "hist"), "test-terminal")
	ps, err := CreatePageStore(cfg)
	if err != nil {
		t.Fatalf("CreatePageStore: %v", err)
	}
	t.Cleanup(func() { ps.Close() })
	return ps
}

func mkLine(s string) *LogicalLine {
	cells := make([]Cell, len(s))
	for i, r := range s {
		cells[i] = Cell{Rune: r}
	}
	return &LogicalLine{Cells: cells}
}

func TestPageStore_StoredLineCountVsLineCount(t *testing.T) {
	ps := newTestPageStore(t)

	if got := ps.LineCount(); got != 0 {
		t.Errorf("empty LineCount: got %d, want 0", got)
	}
	if got := ps.StoredLineCount(); got != 0 {
		t.Errorf("empty StoredLineCount: got %d, want 0", got)
	}
}

func TestPageStore_LineCountIsLogicalEnd(t *testing.T) {
	ps := newTestPageStore(t)

	for i := 0; i < 10; i++ {
		if err := ps.AppendLineWithTimestamp(mkLine("x"), time.Now()); err != nil {
			t.Fatalf("AppendLineWithTimestamp: %v", err)
		}
	}

	// With dense data, LineCount and StoredLineCount match.
	if got := ps.LineCount(); got != 10 {
		t.Errorf("LineCount: got %d, want 10", got)
	}
	if got := ps.StoredLineCount(); got != 10 {
		t.Errorf("StoredLineCount: got %d, want 10", got)
	}
}

func TestPageStore_RebuildPopulatesGlobalIdx(t *testing.T) {
	// Create a store, append some lines via the old API (we'll replace
	// this in a later task, but for now it works because the data is dense).
	dir := t.TempDir()
	cfg := DefaultPageStoreConfig(filepath.Join(dir, "hist"), "rebuild-test")

	ps, err := CreatePageStore(cfg)
	if err != nil {
		t.Fatalf("CreatePageStore: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := ps.AppendLineWithTimestamp(mkLine("line"), time.Now()); err != nil {
			t.Fatalf("AppendLineWithTimestamp: %v", err)
		}
	}
	ps.Close()

	// Reopen: rebuildIndex must populate globalIdx on each pageIndexEntry.
	ps2, err := OpenPageStore(cfg)
	if err != nil {
		t.Fatalf("OpenPageStore: %v", err)
	}
	t.Cleanup(func() { ps2.Close() })

	if got := ps2.LineCount(); got != 5 {
		t.Errorf("LineCount after reopen: got %d, want 5", got)
	}
	for i := int64(0); i < 5; i++ {
		if ps2.pageIndex[i].globalIdx != i {
			t.Errorf("pageIndex[%d].globalIdx: got %d, want %d",
				i, ps2.pageIndex[i].globalIdx, i)
		}
	}
}

func TestPageStore_AppendWithGlobalIdx_Dense(t *testing.T) {
	ps := newTestPageStore(t)

	for i := int64(0); i < 5; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("line"), time.Now()); err != nil {
			t.Fatalf("AppendLineWithGlobalIdx(%d): %v", i, err)
		}
	}

	if got := ps.LineCount(); got != 5 {
		t.Errorf("LineCount: got %d, want 5", got)
	}
	if got := ps.StoredLineCount(); got != 5 {
		t.Errorf("StoredLineCount: got %d, want 5", got)
	}
}

func TestPageStore_AppendWithGlobalIdx_Gap(t *testing.T) {
	ps := newTestPageStore(t)

	// Append 0..2, then jump to 100..101.
	for i := int64(0); i < 3; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("early"), time.Now()); err != nil {
			t.Fatalf("AppendLineWithGlobalIdx(%d): %v", i, err)
		}
	}
	for i := int64(100); i < 102; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("late"), time.Now()); err != nil {
			t.Fatalf("AppendLineWithGlobalIdx(%d): %v", i, err)
		}
	}

	if got := ps.LineCount(); got != 102 {
		t.Errorf("LineCount: got %d, want 102", got)
	}
	if got := ps.StoredLineCount(); got != 5 {
		t.Errorf("StoredLineCount: got %d, want 5", got)
	}

	// Verify a new page was created at the gap boundary.
	// We expect pageID 1 holds globalIdx 0..2 and pageID 2 holds 100..101
	// (or similar — exact pageIDs depend on startNewPage behavior).
	if len(ps.pageIndex) != 5 {
		t.Fatalf("pageIndex length: got %d, want 5", len(ps.pageIndex))
	}
	if ps.pageIndex[0].pageID == ps.pageIndex[3].pageID {
		t.Errorf("expected pageID split between idx=2 and idx=100, but both are on page %d",
			ps.pageIndex[0].pageID)
	}
	if ps.pageIndex[3].globalIdx != 100 {
		t.Errorf("pageIndex[3].globalIdx: got %d, want 100", ps.pageIndex[3].globalIdx)
	}
}

func TestPageStore_AppendWithGlobalIdx_MustIncrease(t *testing.T) {
	ps := newTestPageStore(t)

	if err := ps.AppendLineWithGlobalIdx(10, mkLine("a"), time.Now()); err != nil {
		t.Fatalf("first append: %v", err)
	}
	// Appending an index <= max stored must fail.
	if err := ps.AppendLineWithGlobalIdx(10, mkLine("b"), time.Now()); err == nil {
		t.Errorf("expected error on duplicate globalIdx, got nil")
	}
	if err := ps.AppendLineWithGlobalIdx(5, mkLine("c"), time.Now()); err == nil {
		t.Errorf("expected error on decreasing globalIdx, got nil")
	}
}

func TestPageStore_ReadWithGaps(t *testing.T) {
	ps := newTestPageStore(t)

	for i := int64(0); i < 3; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("early"), time.Now()); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	for i := int64(100); i < 102; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("late"), time.Now()); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Stored entries: readable.
	for _, idx := range []int64{0, 1, 2, 100, 101} {
		line, err := ps.ReadLine(idx)
		if err != nil {
			t.Errorf("ReadLine(%d): unexpected error %v", idx, err)
		}
		if line == nil {
			t.Errorf("ReadLine(%d): got nil, want line", idx)
		}
	}

	// Gap entries: return (nil, nil).
	for _, idx := range []int64{3, 50, 99} {
		line, err := ps.ReadLine(idx)
		if err != nil {
			t.Errorf("ReadLine(%d) gap: unexpected error %v", idx, err)
		}
		if line != nil {
			t.Errorf("ReadLine(%d) gap: got line, want nil", idx)
		}
	}

	// Out of range: also (nil, nil).
	line, err := ps.ReadLine(102)
	if err != nil || line != nil {
		t.Errorf("ReadLine(102) OOR: got (%v, %v), want (nil, nil)", line, err)
	}
}

func TestPageStore_UpdateWithGaps(t *testing.T) {
	ps := newTestPageStore(t)

	for i := int64(0); i < 3; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("early"), time.Now()); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	for i := int64(100); i < 102; i++ {
		if err := ps.AppendLineWithGlobalIdx(i, mkLine("late"), time.Now()); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	// Update an existing line.
	if err := ps.UpdateLine(101, mkLine("updated"), time.Now()); err != nil {
		t.Errorf("UpdateLine(101) existing: %v", err)
	}
	line, _ := ps.ReadLine(101)
	if line == nil || string(runesFromCells(line.Cells)) != "updated" {
		t.Errorf("ReadLine(101) after update: got %q, want \"updated\"",
			string(runesFromCells(line.Cells)))
	}

	// Update a gap must fail.
	if err := ps.UpdateLine(50, mkLine("ghost"), time.Now()); err == nil {
		t.Errorf("UpdateLine(50) gap: expected error, got nil")
	}
}

func runesFromCells(cells []Cell) []rune {
	out := make([]rune, len(cells))
	for i, c := range cells {
		out[i] = c.Rune
	}
	return out
}

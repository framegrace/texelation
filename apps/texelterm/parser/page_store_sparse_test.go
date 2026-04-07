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

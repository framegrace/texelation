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

// Ensure mkLine and time are used by later tasks — suppress unused-import errors.
var _ = time.Now

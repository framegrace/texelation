// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"path/filepath"
	"testing"
	"time"
)

// TestWAL_LineWrite_NoWrap_Roundtrip ensures the NoWrap flag on LogicalLine
// survives a WAL Append -> Close -> reopen -> recover -> PageStore.ReadLine
// round-trip. It exercises the shared encodeLineData/decodeLineDataV2 path
// (flag bit 0x08) from the WAL side.
func TestWAL_LineWrite_NoWrap_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultWALConfig(filepath.Join(dir, "hist"), "wal-nowrap-test")
	cfg.CheckpointInterval = 0
	cfg.CheckpointMaxSize = 1 << 30

	wal1, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog #1: %v", err)
	}

	ll := &LogicalLine{Cells: []Cell{{Rune: 'A'}}, NoWrap: true}
	if err := wal1.Append(42, ll, time.Now()); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Close without checkpoint so the entry must be recovered from the WAL
	// on next open (replay writes it to PageStore).
	if err := wal1.Close(); err != nil {
		t.Fatalf("wal1.Close: %v", err)
	}

	wal2, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog #2: %v", err)
	}
	defer wal2.Close()

	got, err := wal2.ReadLine(42)
	if err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	if got == nil {
		t.Fatal("line 42 nil after WAL recovery")
	}
	if !got.NoWrap {
		t.Errorf("NoWrap lost through WAL round-trip")
	}
}

// TestWAL_LineWrite_NoWrap_DefaultFalse confirms that omitting NoWrap
// preserves the zero-value on the other side.
func TestWAL_LineWrite_NoWrap_DefaultFalse(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultWALConfig(filepath.Join(dir, "hist"), "wal-nowrap-default-test")
	cfg.CheckpointInterval = 0
	cfg.CheckpointMaxSize = 1 << 30

	wal1, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog #1: %v", err)
	}

	ll := &LogicalLine{Cells: []Cell{{Rune: 'B'}}}
	if err := wal1.Append(7, ll, time.Now()); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := wal1.Close(); err != nil {
		t.Fatalf("wal1.Close: %v", err)
	}

	wal2, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog #2: %v", err)
	}
	defer wal2.Close()

	got, err := wal2.ReadLine(7)
	if err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	if got == nil {
		t.Fatal("line 7 nil after WAL recovery")
	}
	if got.NoWrap {
		t.Errorf("default NoWrap should be false")
	}
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/close_clamp_test.go
// Summary: Regression tests for CloseMemoryBuffer's flush-before-metadata
// ordering and LiveEdgeBase clamp. These guard against a class of bugs
// where metadata persisted at close time references line indices that
// never actually reached the WAL, producing a blank band above the
// viewport at reload.

package parser

import (
	"testing"
)

// TestClose_MetadataNeverExceedsWalContent verifies that after
// CloseMemoryBuffer returns, the metadata saved in the WAL has a
// LiveEdgeBase that is <= the WAL's NextGlobalIdx. If a bug advances
// liveEdgeBase past flushed content, reload would hover over a void.
func TestClose_MetadataNeverExceedsWalContent(t *testing.T) {
	dir := t.TempDir()
	id := "close-clamp-leb"
	const cols, rows = 120, 24
	const numLines = 3000

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeNumberedLines(v1, 0, numLines)

	lebBefore := v1.memBufState.liveEdgeBase
	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	// Reopen read-only via a fresh VTerm and inspect what the WAL knows.
	v2 := newTestVTerm(t, cols, rows, dir, id)
	defer v2.CloseMemoryBuffer()

	wal := v2.memBufState.persistence.wal
	if wal == nil {
		t.Fatal("expected WAL on reopened session")
	}
	walCount := wal.NextGlobalIdx()

	leb := v2.memBufState.liveEdgeBase
	if leb > walCount {
		t.Errorf("LiveEdgeBase(%d) > walNextIdx(%d): metadata references unstored lines",
			leb, walCount)
	}
	// Cursor global must also be within stored content.
	cursorGlobal := leb + int64(v2.cursorY)
	if cursorGlobal > walCount && walCount > 0 {
		t.Errorf("cursorGlobal(%d) > walNextIdx(%d)", cursorGlobal, walCount)
	}
	t.Logf("before close: leb=%d, after reload: leb=%d, walNextIdx=%d",
		lebBefore, leb, walCount)
}

// TestClose_ViewportTopLineMatchesStoredContent verifies that the line
// at liveEdgeBase after reload contains the content we expect — not a
// blank placeholder. If metadata points past stored content, the top of
// the viewport is a gap and this test fails.
func TestClose_ViewportTopLineMatchesStoredContent(t *testing.T) {
	dir := t.TempDir()
	id := "close-clamp-top"
	const cols, rows = 120, 24
	const numLines = 2000

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeNumberedLines(v1, 0, numLines)

	beforeVP := captureViewport(v1)
	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	v2 := newTestVTerm(t, cols, rows, dir, id)
	defer v2.CloseMemoryBuffer()

	afterVP := captureViewport(v2)

	// The viewport must not be entirely blank — that's the visual symptom
	// of metadata-past-content that this test guards against.
	anyContent := false
	for _, line := range afterVP {
		if line != "" {
			anyContent = true
			break
		}
	}
	if !anyContent {
		t.Errorf("viewport entirely blank after reload (before top=%q)", beforeVP[0])
	}

	// Top viewport line must match.
	if afterVP[0] != beforeVP[0] {
		t.Errorf("viewport[0] mismatch: before=%q after=%q", beforeVP[0], afterVP[0])
	}
}

// TestClose_FlushHappensBeforeMetadataWrite verifies the ordering
// invariant: at the moment metadata is written, all sweep lines have
// already been persisted. We check this by looking at walNextIdx on
// reopen — if Flush ran AFTER metadata, the dirty sweep's writes could
// be dropped (old bug: metadata clamped against stale walNextIdx).
func TestClose_FlushHappensBeforeMetadataWrite(t *testing.T) {
	dir := t.TempDir()
	id := "close-clamp-order"
	const cols, rows = 120, 24
	const numLines = 1500

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeNumberedLines(v1, 0, numLines)

	expectedLeb := v1.memBufState.liveEdgeBase
	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	v2 := newTestVTerm(t, cols, rows, dir, id)
	defer v2.CloseMemoryBuffer()

	wal := v2.memBufState.persistence.wal
	walCount := wal.NextGlobalIdx()

	// The sweep lines must have reached the WAL. That means walCount
	// should cover every line in [liveEdgeBase, liveEdgeBase+rows-1).
	// The current cursor line (rows-1) isn't committed by a newline so
	// doesn't make it to WAL — that's fine, the invariant is that
	// everything the sweep WROTE actually got flushed before metadata.
	minExpected := expectedLeb + int64(rows-1)
	if walCount < minExpected {
		t.Errorf("walNextIdx(%d) < expected sweep-tail(%d): close flushed metadata before content",
			walCount, minExpected)
	}

	// And the saved metadata should have reached what was expected.
	if v2.memBufState.liveEdgeBase != expectedLeb {
		t.Errorf("liveEdgeBase mismatch: expected %d, got %d",
			expectedLeb, v2.memBufState.liveEdgeBase)
	}
}

// TestClose_ClampAdjustsCursorY verifies that when the clamp fires, the
// cursor row is adjusted so cursorGlobal doesn't exceed walNextIdx.
// Otherwise a reload would show the cursor hovering in a gap.
func TestClose_ClampAdjustsCursorY(t *testing.T) {
	dir := t.TempDir()
	id := "close-clamp-cursor"
	const cols, rows = 120, 24

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeNumberedLines(v1, 0, 500)

	// Artificially advance liveEdgeBase past the WAL frontier to force
	// the clamp to fire. This simulates the real-world eviction race
	// where parser advances the live edge but content doesn't reach WAL.
	walNext := v1.memBufState.persistence.wal.NextGlobalIdx()
	v1.memBufState.liveEdgeBase = walNext + 100 // 100 lines past WAL
	v1.cursorY = rows - 1                       // cursor at bottom of viewport

	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	v2 := newTestVTerm(t, cols, rows, dir, id)
	defer v2.CloseMemoryBuffer()

	walCount := v2.memBufState.persistence.wal.NextGlobalIdx()
	leb := v2.memBufState.liveEdgeBase
	cursorGlobal := leb + int64(v2.cursorY)

	if leb > walCount {
		t.Errorf("LiveEdgeBase(%d) > walNextIdx(%d): clamp did not fire", leb, walCount)
	}
	if cursorGlobal > walCount {
		t.Errorf("cursorGlobal(%d) > walNextIdx(%d): cursor not adjusted after clamp",
			cursorGlobal, walCount)
	}
}

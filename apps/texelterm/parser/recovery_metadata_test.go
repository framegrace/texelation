// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Coverage for recovery of MainScreenState at session reload. These tests
// target three specific failure modes:
//
//  1. `CursorGlobalIdx < WriteTop` in saved metadata — physically impossible
//     (the cursor lives inside the write window which starts at WriteTop),
//     but prior to decoder-side Validate() this was accepted and the load
//     guard silently fell through with cursorY = 0.
//
//  2. `WriteBottomHWM` silently re-derived on reload — if the saved HWM
//     isn't persisted, a grown viewport anchors against writeTop+height-1
//     and writeTop retreats into scrollback (the pre-sparse liveEdgeBase
//     bug).
//
//  3. `PromptStartLine > pageStoreLineCount` in saved metadata — pointer
//     into empty scrollback. Restoring verbatim leaves prompt-aware ops
//     (scroll-to-prompt, erase-to-prompt) pointing at non-existent rows.
//
// Each case is exercised by flushing real content to the PageStore, then
// writing deliberately bogus (or specifically-constructed) MainScreenState
// straight to the WAL (bypassing the AdaptivePersistence snapshot path) and
// reopening the session.

package parser

import (
	"testing"
	"time"
)

// TestRecovery_RejectsCursorBelowWriteTop verifies that a MainScreenState
// whose CursorGlobalIdx is strictly less than WriteTop is rejected at the
// WAL decode boundary (via MainScreenState.Validate()). Recovery must fall
// back to a fresh cursor rather than restoring a cursor that lives in
// scrollback (outside the write window).
func TestRecovery_RejectsCursorBelowWriteTop(t *testing.T) {
	dir := t.TempDir()
	id := "vt-recovery-cursor-below-writetop"
	const cols, rows = 80, 24

	t.Setenv("HOME", t.TempDir())

	// Session 1: write some lines, then inject bad metadata directly into
	// the WAL so Validate() on reload is what catches it (not the
	// AdaptivePersistence clamp on flush).
	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeNumberedLines(v1, 0, 50)

	ap := v1.mainScreenPersistence
	if ap == nil || ap.wal == nil {
		t.Fatalf("AdaptivePersistence/WAL not initialized")
	}
	// Flush pending content so PageStore holds ≥ 50 lines. Without this, the
	// outer load-time guard (`WriteTop <= pageStoreLineCount`) would reject
	// the bad metadata regardless of whether Validate() is working, masking
	// a regression in Validate().
	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := ap.wal.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	badMeta := &MainScreenState{
		WriteTop:        30,
		ContentEnd:      50,
		CursorGlobalIdx: 10, // 10 < WriteTop (30) — invalid
		CursorCol:       0,
		PromptStartLine: -1,
		WorkingDir:      "",
		SavedAt:         time.Now(),
	}
	if err := ap.wal.WriteMainScreenState(badMeta); err != nil {
		t.Fatalf("WriteMainScreenState: %v", err)
	}
	if err := ap.wal.SyncWAL(); err != nil {
		t.Fatalf("SyncWAL: %v", err)
	}
	dirtyClose(v1)

	// Session 2: reopen. The bad entry should fail Validate() during WAL
	// replay, leaving recoveredMainScreenState as nil (no prior valid state)
	// or at the last checkpoint. Either way, the cursor must not land below
	// a stale WriteTop.
	v2 := newTestVTerm(t, cols, rows, dir, id)
	defer v2.CloseMemoryBuffer()

	cursorGI, _ := v2.mainScreen.Cursor()
	writeTop := v2.mainScreen.WriteTop()

	if cursorGI < writeTop {
		t.Errorf("recovered cursor below writeTop: cursorGI=%d writeTop=%d", cursorGI, writeTop)
	}
	// The bad entry in particular should not have been restored verbatim.
	if cursorGI == 10 && writeTop == 30 {
		t.Errorf("corrupt metadata was restored verbatim: cursorGI=10 writeTop=30")
	}
}

// TestRecovery_RestoresWriteBottomHWM verifies that writeBottomHWM survives
// a session round-trip. Without persistence the new session reinitialized
// HWM to writeTop+height-1, so a subsequent grow-on-resize would anchor
// against that diminished value and writeTop would retreat into scrollback
// — the same failure mode as the pre-sparse liveEdgeBase bug.
//
// Scenario: session 1 writes enough lines to move HWM well past the initial
// height-1, then we manually persist metadata carrying an HWM slightly
// beyond what derive-from-writeTop would produce. Session 2 must load the
// saved HWM; the test catches the "silently re-derived" failure mode.
func TestRecovery_RestoresWriteBottomHWM(t *testing.T) {
	dir := t.TempDir()
	id := "vt-recovery-hwm"
	const cols, rows = 80, 24

	t.Setenv("HOME", t.TempDir())

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeNumberedLines(v1, 0, 200)

	realHWM := v1.mainScreen.WriteBottomHWM()
	realWriteTop := v1.mainScreen.WriteTop()
	realCursorGI, realCursorCol := v1.mainScreen.Cursor()
	realContentEnd := v1.ContentEnd()

	ap := v1.mainScreenPersistence
	if ap == nil || ap.wal == nil {
		t.Fatalf("AdaptivePersistence/WAL not initialized")
	}
	// Force content out of the pending queue and into the PageStore so the
	// reload guard (`WriteTop <= pageStoreLineCount`) accepts the metadata
	// we are about to inject. Without this, BestEffort batching can leave
	// PageStore with only a handful of lines at dirtyClose time.
	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := ap.wal.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	// Push HWM beyond the value derive-from-writeTop would produce so we can
	// tell them apart after reload. Both values are persisted, the restore
	// path must honor the larger.
	savedHWM := realHWM + 100
	meta := &MainScreenState{
		WriteTop:        realWriteTop,
		ContentEnd:      realContentEnd,
		CursorGlobalIdx: realCursorGI,
		CursorCol:       realCursorCol,
		PromptStartLine: -1,
		WorkingDir:      "",
		WriteBottomHWM:  savedHWM,
		SavedAt:         time.Now(),
	}
	if err := ap.wal.WriteMainScreenState(meta); err != nil {
		t.Fatalf("WriteMainScreenState: %v", err)
	}
	if err := ap.wal.SyncWAL(); err != nil {
		t.Fatalf("SyncWAL: %v", err)
	}
	dirtyClose(v1)

	v2 := newTestVTerm(t, cols, rows, dir, id)
	defer v2.CloseMemoryBuffer()

	if got := v2.mainScreen.WriteBottomHWM(); got != savedHWM {
		t.Errorf("WriteBottomHWM: got %d, want %d (persistence silently dropped HWM?)",
			got, savedHWM)
	}
}

// TestRecovery_DiscardsStalePromptStartLine verifies that a PromptStartLine
// pointing past the last persisted line is discarded on reload (reset to
// -1, "unknown"). This covers the case where metadata advanced but the
// referenced prompt line never made it to PageStore — downstream
// prompt-aware operations must not dereference that index.
func TestRecovery_DiscardsStalePromptStartLine(t *testing.T) {
	dir := t.TempDir()
	id := "vt-recovery-stale-prompt"
	const cols, rows = 80, 24

	t.Setenv("HOME", t.TempDir())

	v1 := newTestVTerm(t, cols, rows, dir, id)
	writeNumberedLines(v1, 0, 50)

	realContentEnd := v1.ContentEnd()
	realWriteTop := v1.mainScreen.WriteTop()
	realCursorGI, realCursorCol := v1.mainScreen.Cursor()

	ap := v1.mainScreenPersistence
	if ap == nil || ap.wal == nil {
		t.Fatalf("AdaptivePersistence/WAL not initialized")
	}
	// Flush pending content to PageStore so the reload guard accepts the
	// metadata. Otherwise the whole entry is rejected and the clamp path
	// we're trying to exercise is never hit (the default PromptStartLine
	// of -1 would make the test pass by coincidence).
	if err := ap.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := ap.wal.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	// Preserve the other fields so only PromptStartLine is stale. Point the
	// prompt far past any line that could ever reach PageStore.
	staleMeta := &MainScreenState{
		WriteTop:        realWriteTop,
		ContentEnd:      realContentEnd,
		CursorGlobalIdx: realCursorGI,
		CursorCol:       realCursorCol,
		PromptStartLine: realContentEnd + 500, // way past the end
		WorkingDir:      "/tmp",
		SavedAt:         time.Now(),
	}
	if err := ap.wal.WriteMainScreenState(staleMeta); err != nil {
		t.Fatalf("WriteMainScreenState: %v", err)
	}
	if err := ap.wal.SyncWAL(); err != nil {
		t.Fatalf("SyncWAL: %v", err)
	}
	dirtyClose(v1)

	v2 := newTestVTerm(t, cols, rows, dir, id)
	defer v2.CloseMemoryBuffer()

	// The stale prompt position must have been discarded (reset to -1).
	if v2.PromptStartGlobalLine == staleMeta.PromptStartLine {
		t.Errorf("stale PromptStartLine %d was restored verbatim", v2.PromptStartGlobalLine)
	}
	if v2.PromptStartGlobalLine != -1 {
		// Tolerate an alternative clamp (e.g. to pageStoreLineCount-1) but
		// fail loudly if it's still pointing into phantom territory.
		if pageStoreLineCount := v2.mainScreenPageStore.LineCount(); v2.PromptStartGlobalLine >= pageStoreLineCount {
			t.Errorf("PromptStartGlobalLine %d still past PageStore end %d",
				v2.PromptStartGlobalLine, pageStoreLineCount)
		}
	}
}

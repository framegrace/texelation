// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/osc133_anchor_persistence_test.go
// Summary: Regression tests for OSC 133 anchor persistence across server
// restart (issue #186). Covers the WAL binary encoding (round-trip +
// backward-compat when older payloads lack the trailing anchor fields),
// and the end-to-end VTerm save/reload cycle including stale-anchor
// discard when the reference exceeds the PageStore's line count.

package parser

import (
	"encoding/binary"
	"fmt"
	"testing"
	"time"
)

// TestEncodeDecodeMainScreenState_AnchorsRoundtrip exercises the binary
// encoding for the two new OSC 133 anchor fields. Each field is a trailing
// int64 past WriteBottomHWM.
func TestEncodeDecodeMainScreenState_AnchorsRoundtrip(t *testing.T) {
	original := &MainScreenState{
		WriteTop:         100,
		ContentEnd:       250,
		CursorGlobalIdx:  240,
		CursorCol:        12,
		PromptStartLine:  230,
		InputStartLine:   235,
		CommandStartLine: 240,
		WorkingDir:       "/home/user/work",
		WriteBottomHWM:   280,
		SavedAt:          time.Unix(1700000000, 0).UTC(),
	}

	encoded, err := encodeMainScreenState(original)
	if err != nil {
		t.Fatalf("encodeMainScreenState: %v", err)
	}
	decoded, err := decodeMainScreenState(encoded)
	if err != nil {
		t.Fatalf("decodeMainScreenState: %v", err)
	}

	if decoded.InputStartLine != original.InputStartLine {
		t.Errorf("InputStartLine: got %d, want %d", decoded.InputStartLine, original.InputStartLine)
	}
	if decoded.CommandStartLine != original.CommandStartLine {
		t.Errorf("CommandStartLine: got %d, want %d", decoded.CommandStartLine, original.CommandStartLine)
	}
	// Sanity-check the previously-added trailing field to make sure appending
	// more trailing fields didn't shift the HWM offset.
	if decoded.WriteBottomHWM != original.WriteBottomHWM {
		t.Errorf("WriteBottomHWM: got %d, want %d (offset regressed?)", decoded.WriteBottomHWM, original.WriteBottomHWM)
	}
}

// TestDecodeMainScreenState_BackwardCompat_NoTrailingAnchors synthesizes a
// pre-issue-#186 binary payload (everything up to and including
// WriteBottomHWM but no InputStartLine / CommandStartLine trailers) and
// verifies both fields default to -1 ("unknown"). Without this guarantee,
// an upgrade would silently load a bogus anchor of 0 and the next ED 2
// repaint would rewind to globalIdx 0.
func TestDecodeMainScreenState_BackwardCompat_NoTrailingAnchors(t *testing.T) {
	const cwd = "/tmp/test"
	cwdBytes := []byte(cwd)
	// Layout as it stood before #186: fixed 46 + cwd + 8 bytes HWM.
	size := 46 + len(cwdBytes) + 8
	buf := make([]byte, size)
	binary.LittleEndian.PutUint64(buf[0:8], 100)  // WriteTop
	binary.LittleEndian.PutUint64(buf[8:16], 150) // ContentEnd
	binary.LittleEndian.PutUint64(buf[16:24], 110) // CursorGlobalIdx
	binary.LittleEndian.PutUint32(buf[24:28], 3)   // CursorCol
	binary.LittleEndian.PutUint64(buf[28:36], 108) // PromptStartLine
	binary.LittleEndian.PutUint64(buf[36:44], uint64(time.Unix(1700000000, 0).UnixNano()))
	binary.LittleEndian.PutUint16(buf[44:46], uint16(len(cwdBytes)))
	copy(buf[46:46+len(cwdBytes)], cwdBytes)
	binary.LittleEndian.PutUint64(buf[46+len(cwdBytes):46+len(cwdBytes)+8], 145) // WriteBottomHWM

	decoded, err := decodeMainScreenState(buf)
	if err != nil {
		t.Fatalf("decodeMainScreenState (old format): %v", err)
	}

	// Old fields must still decode correctly.
	if decoded.WriteBottomHWM != 145 {
		t.Errorf("WriteBottomHWM: got %d, want 145", decoded.WriteBottomHWM)
	}
	if decoded.PromptStartLine != 108 {
		t.Errorf("PromptStartLine: got %d, want 108", decoded.PromptStartLine)
	}
	// New anchor fields default to -1 for pre-#186 payloads.
	if decoded.InputStartLine != -1 {
		t.Errorf("InputStartLine: got %d, want -1 (default for pre-#186 format)", decoded.InputStartLine)
	}
	if decoded.CommandStartLine != -1 {
		t.Errorf("CommandStartLine: got %d, want -1 (default for pre-#186 format)", decoded.CommandStartLine)
	}
}

// TestEnableMemoryBuffer_RestoresOSC133Anchors writes content, marks all
// three OSC 133 anchors, closes the VTerm, reopens from disk, and verifies
// each anchor came back. This is the acceptance test for issue #186: a
// mid-Claude-session server restart preserves the exact rewind target.
func TestEnableMemoryBuffer_RestoresOSC133Anchors(t *testing.T) {
	dir := t.TempDir()
	id := "anchor-persist"
	const cols, rows = 80, 24

	// --- Session 1 ---
	v1 := newTestVTerm(t, cols, rows, dir, id)
	p := NewParser(v1)

	// Write a few lines of output and set each OSC 133 anchor on its own
	// line so the three fields end up at distinct globalIdx values; the
	// test can then tell from the reload which restored value came from
	// which field.
	for i := 0; i < 3; i++ {
		parseString(p, fmt.Sprintf("banner line %d\r\n", i))
	}
	v1.MarkPromptStart()
	parseString(p, "fake-prompt-line\r\n")
	v1.MarkInputStart()
	parseString(p, "fake-input-line\r\n")
	v1.MarkCommandStart()

	prompt := v1.PromptStartGlobalLine
	input := v1.InputStartGlobalLine
	command := v1.CommandStartGlobalLine
	if prompt < 0 || input < 0 || command < 0 {
		t.Fatalf("setup: all three anchors must be set; got prompt=%d input=%d command=%d",
			prompt, input, command)
	}
	// They should be distinct — otherwise the test can't distinguish which
	// field survived which.
	if prompt == input || input == command {
		t.Fatalf("setup: anchors should be distinct; got prompt=%d input=%d command=%d",
			prompt, input, command)
	}

	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	// --- Session 2: reload from disk ---
	v2 := newTestVTerm(t, cols, rows, dir, id)
	defer v2.CloseMemoryBuffer()

	if v2.PromptStartGlobalLine != prompt {
		t.Errorf("PromptStartGlobalLine: got %d, want %d (not restored)", v2.PromptStartGlobalLine, prompt)
	}
	if v2.InputStartGlobalLine != input {
		t.Errorf("InputStartGlobalLine: got %d, want %d (not restored)", v2.InputStartGlobalLine, input)
	}
	if v2.CommandStartGlobalLine != command {
		t.Errorf("CommandStartGlobalLine: got %d, want %d (not restored)", v2.CommandStartGlobalLine, command)
	}
}

// TestED2_RewindAfterRestart_UsesRestoredCommandStart is the full
// regression for issue #186: it drives OSC 133 anchors in session 1,
// closes the VTerm (simulating a server shutdown), reopens from disk,
// overflows the viewport on the reloaded instance, then issues ESC[2J
// and verifies that writeTop rewinds to the *restored* CommandStart
// anchor. Without #186 the anchor would have decoded as 0 and the
// rewind would have landed at globalIdx 0 instead.
func TestED2_RewindAfterRestart_UsesRestoredCommandStart(t *testing.T) {
	dir := t.TempDir()
	id := "anchor-ed2-restart"
	const cols, rows = 40, 10

	// --- Session 1: set up anchors, then close. ---
	v1 := newTestVTerm(t, cols, rows, dir, id)
	p1 := NewParser(v1)

	parseString(p1, "shell banner\r\n")
	v1.MarkPromptStart()
	parseString(p1, "prompt> ")
	v1.MarkInputStart()
	parseString(p1, "claude\r\n")
	v1.MarkCommandStart()
	originalCmd := v1.CommandStartGlobalLine
	if originalCmd < 0 {
		t.Fatalf("setup: MarkCommandStart did not set CommandStartGlobalLine")
	}
	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	// --- Session 2: reload, overflow the viewport, issue ED 2. ---
	v2 := newTestVTerm(t, cols, rows, dir, id)
	defer v2.CloseMemoryBuffer()

	if v2.CommandStartGlobalLine != originalCmd {
		t.Fatalf("CommandStartGlobalLine not restored: got %d, want %d",
			v2.CommandStartGlobalLine, originalCmd)
	}

	p2 := NewParser(v2)
	writeFullFrameOverflow(p2, 30)
	if got := v2.mainScreen.WriteTop(); got <= originalCmd {
		t.Fatalf("setup: writeTop=%d did not advance past restored CommandStart=%d",
			got, originalCmd)
	}

	parseString(p2, "\x1b[2J")

	if got := v2.mainScreen.WriteTop(); got != originalCmd {
		t.Errorf("writeTop after ED 2: got %d, want %d (restored CommandStart anchor)",
			got, originalCmd)
	}
}

// TestEnableMemoryBuffer_DiscardsStaleAnchors verifies that the restore path
// discards InputStart / CommandStart anchors pointing past the last
// persisted line, matching the existing PromptStartLine behavior. This
// guards against metadata that was written just before a crash without the
// referenced lines reaching disk.
func TestEnableMemoryBuffer_DiscardsStaleAnchors(t *testing.T) {
	dir := t.TempDir()
	id := "anchor-stale"
	const cols, rows = 80, 24

	// Session 1: just open + close to produce a valid PageStore and WAL
	// without writing any lines.
	v1 := newTestVTerm(t, cols, rows, dir, id)
	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	// Hand-craft a MainScreenState with anchors that point past the empty
	// PageStore. Write it into the WAL via a second open.
	cfg := DefaultWALConfig(dir, id)
	wal, err := OpenWriteAheadLog(cfg)
	if err != nil {
		t.Fatalf("OpenWriteAheadLog: %v", err)
	}
	stale := &MainScreenState{
		WriteTop:         0,
		ContentEnd:       -1,
		CursorGlobalIdx:  0,
		CursorCol:        0,
		PromptStartLine:  500,
		InputStartLine:   600,
		CommandStartLine: 700,
		SavedAt:          time.Now(),
	}
	if err := wal.WriteMainScreenState(stale); err != nil {
		t.Fatalf("WriteMainScreenState: %v", err)
	}
	if err := wal.Close(); err != nil {
		t.Fatalf("wal.Close: %v", err)
	}

	// Session 2: reload. All three anchors should be discarded because they
	// exceed pageStoreLineCount (which is 0 — no lines were written).
	v2 := newTestVTerm(t, cols, rows, dir, id)
	defer v2.CloseMemoryBuffer()

	if v2.PromptStartGlobalLine != -1 {
		t.Errorf("PromptStartGlobalLine: got %d, want -1 (stale should have been discarded)",
			v2.PromptStartGlobalLine)
	}
	if v2.InputStartGlobalLine != -1 {
		t.Errorf("InputStartGlobalLine: got %d, want -1 (stale should have been discarded)",
			v2.InputStartGlobalLine)
	}
	if v2.CommandStartGlobalLine != -1 {
		t.Errorf("CommandStartGlobalLine: got %d, want -1 (stale should have been discarded)",
			v2.CommandStartGlobalLine)
	}
}

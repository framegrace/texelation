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

// TestED2_ClearsOverflowPastViewport guards the "phantom scrollback"
// bug: an earlier TUI in the session wrote cells past the current
// viewport (HWM > writeTop+height-1). When a later "homing" ED 2 fires
// — the canonical TUI-launch signal — those overflow cells linger as
// stale scrollback and bleed through once the new frame scrolls or the
// user scrolls down, even across a server restart that persists them.
// ED 2 must wipe [writeTop+height, HWM] in addition to the viewport.
func TestED2_ClearsOverflowPastViewport(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const cols, rows = 40, 10
	v := NewVTerm(cols, rows)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Fill the sparse store with distinctive TUI-style content spanning
	// well past a single viewport's worth of rows. Plain writes advance
	// writeTop with the cursor, so HWM stays equal to the viewport
	// bottom — that's the "no overflow yet" baseline.
	for i := 0; i < rows*3; i++ {
		parseString(p, fmt.Sprintf("tui1-row-%02d %s\r\n", i, "yyyyyyy"))
	}
	hwmBefore := v.mainScreen.WriteBottomHWM()

	// Force writeTop backward to simulate the post-restore condition:
	// persisted HWM points past the shell's current viewport, so
	// whatever the old TUI wrote in [rewind+rows, HWM] is now phantom
	// scrollback the new shell won't overwrite. RewindWriteTop keeps
	// HWM monotonic — exactly the asymmetry the ED 2 clear must handle.
	rewindTo := hwmBefore - int64(rows*2)
	if rewindTo < 0 {
		t.Fatalf("setup: hwmBefore %d too small to rewind", hwmBefore)
	}
	v.mainScreen.RewindWriteTop(rewindTo)
	writeTop := v.mainScreen.WriteTop()
	if writeTop != rewindTo {
		t.Fatalf("setup: writeTop %d != rewindTo %d", writeTop, rewindTo)
	}
	if hwm := v.mainScreen.WriteBottomHWM(); hwm <= writeTop+int64(rows)-1 {
		t.Fatalf("setup: HWM %d not past viewport bottom %d after rewind",
			hwm, writeTop+int64(rows)-1)
	}

	// New TUI homes with ED 2. No OSC 133 anchors are set, so the
	// anchor-rewind branch is a no-op — the overflow clean-slate pass
	// is the only thing standing between the old TUI's cells and the
	// user's scrollback.
	parseString(p, "\x1b[2J")

	writeTopAfter := v.mainScreen.WriteTop()
	for gi := writeTopAfter + int64(rows); gi <= hwmBefore; gi++ {
		cells := v.mainScreen.ReadLine(gi)
		if cells != nil && lineHasSparseContent(cells) {
			t.Errorf("overflow line %d still has content after ED 2: %q",
				gi, cellsPreview(cells))
		}
	}
}

// TestED2_PreservesScrollbackAboveViewport is the counterpart guarantee:
// only overflow rows *past* the viewport get wiped by ED 2's new
// clean-slate pass. Legitimate scrollback above writeTop (prior shell
// output the user wants to see when scrolling up) must stay intact.
func TestED2_PreservesScrollbackAboveViewport(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const cols, rows = 40, 10
	v := NewVTerm(cols, rows)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Plain scrollback that should survive ED 2. Enough lines to push
	// writeTop well past 0.
	const scrollbackLines = 25
	for i := 0; i < scrollbackLines; i++ {
		parseString(p, fmt.Sprintf("scroll-%02d\r\n", i))
	}
	writeTopBefore := v.mainScreen.WriteTop()
	if writeTopBefore == 0 {
		t.Fatalf("setup: writeTop did not advance; test needs scrollback")
	}

	parseString(p, "\x1b[2J")

	// Every row above the new writeTop that had content before must
	// still have content.
	writeTopAfter := v.mainScreen.WriteTop()
	for gi := int64(0); gi < writeTopAfter; gi++ {
		cells := v.mainScreen.ReadLine(gi)
		if cells == nil || !lineHasSparseContent(cells) {
			t.Errorf("scrollback line %d wrongly cleared by ED 2", gi)
		}
	}
}

// cellsPreview turns a cell slice into a short printable string for
// failure messages. Intentionally tiny — we just need to see which TUI
// content bled through when a test fails.
func cellsPreview(cells []Cell) string {
	const max = 20
	out := make([]rune, 0, max)
	for i, c := range cells {
		if i >= max {
			break
		}
		if c.Rune == 0 {
			out = append(out, ' ')
		} else {
			out = append(out, c.Rune)
		}
	}
	return string(out)
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

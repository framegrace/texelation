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

// TestEnableMemoryBuffer_BlanksLiveViewportOnRestore_MidCommand guards
// the "stale frame bleed-through" bug: when a TUI like Claude is running
// at server shutdown (OSC 133;C fired, no 133;D yet), the sparse store
// holds the last-drawn frame cells at the restored live range
// [writeTop, max(writeTop+height-1, HWM)]. On reload, the respawned
// shell only writes the cells it actually draws (prompt, a new command
// line), so every untouched cell — blank rows, trailing columns past
// the command — would show the old TUI frame. The restore path must
// blank this range so the new session starts on a clean canvas.
// Scrollback above writeTop must NOT be cleared.
func TestEnableMemoryBuffer_BlanksLiveViewportOnRestore_MidCommand(t *testing.T) {
	dir := t.TempDir()
	id := "restore-blank-viewport-midcmd"
	const cols, rows = 40, 10

	// --- Session 1: shell session with a running TUI command. ---
	v1 := newTestVTerm(t, cols, rows, dir, id)
	p1 := NewParser(v1)

	// Scrollback we want preserved.
	parseString(p1, "scrollback line A\r\n")
	parseString(p1, "scrollback line B\r\n")
	scrollbackEnd := v1.mainScreen.WriteTop() + int64(v1.cursorY) - 1

	// Bash prompt + input + command-start (simulates `claude` launching).
	v1.MarkPromptStart()
	parseString(p1, "prompt> ")
	v1.MarkInputStart()
	parseString(p1, "claude\r\n")
	v1.MarkCommandStart()
	if v1.CommandStartGlobalLine < 0 {
		t.Fatalf("setup: MarkCommandStart did not set CommandStartGlobalLine")
	}

	// Fill the remaining viewport rows with "TUI" content that must NOT
	// survive restore — these are cells Claude drew in its last frame.
	for i := 0; i < rows; i++ {
		parseString(p1, fmt.Sprintf("tui-frame-row-%02d padded %s\r\n", i, "zzzzzzz"))
	}
	writeTopBefore := v1.mainScreen.WriteTop()
	hwmBefore := v1.mainScreen.WriteBottomHWM()
	if hwmBefore < writeTopBefore+int64(rows)-1 {
		t.Fatalf("setup: HWM %d did not reach end of viewport (writeTop=%d, rows=%d)",
			hwmBefore, writeTopBefore, rows)
	}

	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	// --- Session 2: reload. ---
	v2 := newTestVTerm(t, cols, rows, dir, id)
	defer v2.CloseMemoryBuffer()

	writeTopAfter := v2.mainScreen.WriteTop()
	if writeTopAfter != writeTopBefore {
		t.Fatalf("writeTop not restored: got %d, want %d", writeTopAfter, writeTopBefore)
	}

	// Live range [writeTop, HWM] must be fully blank so the new session
	// doesn't see stale TUI content bleeding through.
	for gi := writeTopAfter; gi <= hwmBefore; gi++ {
		cells := v2.mainScreen.ReadLine(gi)
		if cells != nil && lineHasSparseContent(cells) {
			t.Errorf("live-range line %d still has content after restore: %q",
				gi, cellsPreview(cells))
		}
	}

	// Scrollback above writeTop must be preserved.
	for gi := int64(0); gi <= scrollbackEnd; gi++ {
		cells := v2.mainScreen.ReadLine(gi)
		if cells == nil || !lineHasSparseContent(cells) {
			t.Errorf("scrollback line %d was wrongly cleared", gi)
		}
	}
}

// TestEnableMemoryBuffer_PreservesViewportOnRestore_IdleShell is the
// counterpart: when no command is running at shutdown (CommandStart
// < 0), the viewport holds legitimate plain-text output the user wants
// to see on reload. The restore path must NOT blank it.
func TestEnableMemoryBuffer_PreservesViewportOnRestore_IdleShell(t *testing.T) {
	dir := t.TempDir()
	id := "restore-preserve-viewport-idle"
	const cols, rows = 40, 10

	v1 := newTestVTerm(t, cols, rows, dir, id)
	p1 := NewParser(v1)

	// Plain output — no OSC 133;C. Think `ls` output followed by a
	// returned shell prompt.
	const contentLines = 8
	for i := 0; i < contentLines; i++ {
		parseString(p1, fmt.Sprintf("output-line-%02d\r\n", i))
	}

	// Snapshot the populated lines as session 1 saw them so session 2
	// can be compared cell-for-cell.
	writeTopBefore := v1.mainScreen.WriteTop()
	before := make([][]Cell, contentLines)
	for i := 0; i < contentLines; i++ {
		before[i] = v1.mainScreen.ReadLine(writeTopBefore + int64(i))
		if before[i] == nil || !lineHasSparseContent(before[i]) {
			t.Fatalf("setup: line %d has no content in session 1", i)
		}
	}

	if err := v1.CloseMemoryBuffer(); err != nil {
		t.Fatalf("CloseMemoryBuffer: %v", err)
	}

	v2 := newTestVTerm(t, cols, rows, dir, id)
	defer v2.CloseMemoryBuffer()

	if v2.CommandStartGlobalLine >= 0 {
		t.Fatalf("setup invariant: CommandStartGlobalLine should be -1, got %d",
			v2.CommandStartGlobalLine)
	}

	for i := 0; i < contentLines; i++ {
		got := v2.mainScreen.ReadLine(writeTopBefore + int64(i))
		if got == nil || !lineHasSparseContent(got) {
			t.Errorf("idle-shell line %d (globalIdx %d) was wrongly cleared on restore",
				i, writeTopBefore+int64(i))
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

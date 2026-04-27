// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/desktop_publisher_test.go
// Summary: Exercises desktop publisher behaviour to ensure the server runtime remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"testing"

	texelcore "github.com/framegrace/texelui/core"
	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/protocol"
	"github.com/framegrace/texelation/texel"
)

type publisherScreenDriver struct{}

func (publisherScreenDriver) Init() error                                    { return nil }
func (publisherScreenDriver) Fini()                                          {}
func (publisherScreenDriver) Size() (int, int)                               { return 80, 24 }
func (publisherScreenDriver) SetStyle(tcell.Style)                           {}
func (publisherScreenDriver) HideCursor()                                    {}
func (publisherScreenDriver) Show()                                          {}
func (publisherScreenDriver) PollEvent() tcell.Event                         { return nil }
func (publisherScreenDriver) SetContent(int, int, rune, []rune, tcell.Style) {}
func (publisherScreenDriver) GetContent(int, int) (rune, []rune, tcell.Style, int) {
	return ' ', nil, tcell.StyleDefault, 1
}

type simpleApp struct {
	title string
}

func (s *simpleApp) Run() error            { return nil }
func (s *simpleApp) Stop()                 {}
func (s *simpleApp) Resize(cols, rows int) {}
func (s *simpleApp) Render() [][]texelcore.Cell {
	return [][]texelcore.Cell{{{Ch: 'a', Style: tcell.StyleDefault}}}
}
func (s *simpleApp) GetTitle() string               { return s.title }
func (s *simpleApp) HandleKey(ev *tcell.EventKey)   {}
func (s *simpleApp) SetRefreshNotifier(chan<- bool) {}

// LastDelta decodes and returns the most recent BufferDelta enqueued on
// the session for the given pane. Fails the test if none found.
func sessionLastDelta(t *testing.T, s *Session, paneID [16]byte) protocol.BufferDelta {
	t.Helper()
	diffs := s.Pending(0)
	var found *protocol.BufferDelta
	for i := range diffs {
		if diffs[i].Message.Type != protocol.MsgBufferDelta {
			continue
		}
		decoded, err := protocol.DecodeBufferDelta(diffs[i].Payload)
		if err != nil {
			t.Fatalf("decode delta: %v", err)
		}
		if decoded.PaneID == paneID {
			d := decoded
			found = &d
		}
	}
	if found == nil {
		t.Fatalf("no BufferDelta for pane %x", paneID[:4])
	}
	return *found
}

func TestDesktopPublisherProducesDiffs(t *testing.T) {
	driver := publisherScreenDriver{}
	lifecycle := texel.NoopAppLifecycle{}

	shellFactory := func() texelcore.App { return &simpleApp{title: "shell"} }

	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}
	desktop.SwitchToWorkspace(1)
	desktop.ActiveWorkspace().AddApp(&simpleApp{title: "initial"})

	session := NewSession([16]byte{1}, 512)
	publisher := NewDesktopPublisher(desktop, session)
	if err := publisher.Publish(); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	// simpleApp is not a RowGlobalIdxProvider, so its snapshot is marked
	// AltScreen=true and published without needing a viewport. Expect at
	// least one delta with the AltScreen flag set.
	diffs := session.Pending(0)
	if len(diffs) == 0 {
		t.Fatalf("expected at least one diff")
	}
	sawAltScreen := false
	for _, diff := range diffs {
		if diff.Message.Type != protocol.MsgBufferDelta {
			t.Fatalf("unexpected message type %v", diff.Message.Type)
		}
		delta, err := protocol.DecodeBufferDelta(diff.Payload)
		if err != nil {
			t.Fatalf("decode delta failed: %v", err)
		}
		if len(delta.Rows) == 0 {
			t.Fatalf("expected rows in delta")
		}
		if delta.Flags&protocol.BufferDeltaAltScreen != 0 {
			sawAltScreen = true
		}
	}
	if !sawAltScreen {
		t.Fatalf("expected at least one delta with BufferDeltaAltScreen flag (non-terminal app)")
	}
}

// buildSyntheticSnap builds a PaneSnapshot with `rows` rows, each carrying
// globalIdx = startGid + rowIndex. The snapshot is NOT alt-screen.
func buildSyntheticSnap(paneID [16]byte, rows int, startGid int64) texel.PaneSnapshot {
	buf := make([][]texel.Cell, rows)
	gid := make([]int64, rows)
	for y := 0; y < rows; y++ {
		buf[y] = []texel.Cell{{Ch: rune('A' + (y % 26)), Style: tcell.StyleDefault}}
		gid[y] = startGid + int64(y)
	}
	return texel.PaneSnapshot{
		ID:           paneID,
		Title:        "synthetic",
		Buffer:       buf,
		RowGlobalIdx: gid,
		AltScreen:    false,
	}
}

// buildSyntheticAltSnap builds a PaneSnapshot with `rows` x `cols` cells
// and AltScreen=true (all globalIdxs -1).
func buildSyntheticAltSnap(paneID [16]byte, rows, cols int) texel.PaneSnapshot {
	buf := make([][]texel.Cell, rows)
	gid := make([]int64, rows)
	for y := 0; y < rows; y++ {
		row := make([]texel.Cell, cols)
		for x := 0; x < cols; x++ {
			row[x] = texel.Cell{Ch: 'x', Style: tcell.StyleDefault}
		}
		buf[y] = row
		gid[y] = -1
	}
	return texel.PaneSnapshot{
		ID:           paneID,
		Title:        "alt",
		Buffer:       buf,
		RowGlobalIdx: gid,
		AltScreen:    true,
	}
}

// publishSnaps drives a single encode+enqueue pass for the given snapshots
// without needing a live DesktopEngine. It constructs a real
// DesktopPublisher and invokes publishSnapshotsLocked so tests exercise
// the production encode loop and automatically track any future changes.
func publishSnaps(t *testing.T, session *Session, snaps []texel.PaneSnapshot) {
	t.Helper()
	pub := &DesktopPublisher{
		session:      session,
		prevBuffers:  make(map[[16]byte][][]texel.Cell),
		lastViewport: make(map[[16]byte]ClientViewport),
	}
	pub.mu.Lock()
	defer pub.mu.Unlock()
	if err := pub.publishSnapshotsLocked(snaps); err != nil {
		t.Fatalf("publishSnapshotsLocked: %v", err)
	}
}

func TestPublisher_ClipsToViewport(t *testing.T) {
	paneID := [16]byte{0xAA}
	session := NewSession([16]byte{1}, 512)

	session.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID:        paneID,
		ViewTopIdx:    100,
		ViewBottomIdx: 123,
		Rows:          24,
		Cols:          80,
		AutoFollow:    false,
	})

	// Pane has 200 rows of content; globalIdxs run 0..199.
	snap := buildSyntheticSnap(paneID, 200, 0)
	publishSnaps(t, session, []texel.PaneSnapshot{snap})

	delta := sessionLastDelta(t, session, paneID)
	if delta.Flags&protocol.BufferDeltaAltScreen != 0 {
		t.Fatalf("main-screen pane should not set AltScreen flag")
	}
	// Expect RowBase = ViewTopIdx - Rows = 100 - 24 = 76.
	if delta.RowBase != 76 {
		t.Fatalf("RowBase: got %d want 76", delta.RowBase)
	}
	if len(delta.Rows) == 0 {
		t.Fatalf("expected rows in clipped delta")
	}
	for _, row := range delta.Rows {
		globalIdx := delta.RowBase + int64(row.Row)
		if globalIdx < 76 || globalIdx > 147 {
			t.Fatalf("row %d (idx %d) outside resident window [76,147]", row.Row, globalIdx)
		}
	}
	// Window is inclusive [76, 147] = 72 rows. Pane has content for each.
	if len(delta.Rows) != 72 {
		t.Fatalf("expected 72 rows in window, got %d", len(delta.Rows))
	}
}

func TestPublisher_AltScreenSetsFlag(t *testing.T) {
	paneID := [16]byte{0xBB}
	session := NewSession([16]byte{2}, 512)
	snap := buildSyntheticAltSnap(paneID, 24, 80)
	publishSnaps(t, session, []texel.PaneSnapshot{snap})

	delta := sessionLastDelta(t, session, paneID)
	if delta.Flags&protocol.BufferDeltaAltScreen == 0 {
		t.Fatalf("alt-screen pane should set AltScreen flag")
	}
	if delta.RowBase != 0 {
		t.Fatalf("alt-screen pane should have RowBase=0, got %d", delta.RowBase)
	}
	if len(delta.Rows) != 24 {
		t.Fatalf("alt-screen delta: want 24 rows, got %d", len(delta.Rows))
	}
}

func TestBufferToDelta_DecorationRowsIncluded(t *testing.T) {
	// 5-row buffer: rowIdx 0 = top border (-1), rowIdx 1..3 = content gids,
	// rowIdx 4 = bottom border (-1).
	rows := [][]texel.Cell{
		{{Ch: '+'}, {Ch: '-'}, {Ch: '+'}}, // border
		{{Ch: 'a'}, {Ch: 'b'}, {Ch: 'c'}}, // content
		{{Ch: 'd'}, {Ch: 'e'}, {Ch: 'f'}}, // content
		{{Ch: 'g'}, {Ch: 'h'}, {Ch: 'i'}}, // content
		{{Ch: '+'}, {Ch: '-'}, {Ch: '+'}}, // border
	}
	snap := texel.PaneSnapshot{
		ID:             [16]byte{0xab},
		Buffer:         rows,
		RowGlobalIdx:   []int64{-1, 100, 101, 102, -1},
		ContentTopRow:  1,
		NumContentRows: 3,
	}
	vp := ClientViewport{Rows: 3, AutoFollow: true}
	prev := [][]texel.Cell(nil)

	delta := bufferToDelta(snap, prev, 1, vp)

	if len(delta.DecorRows) != 2 {
		t.Fatalf("expected 2 DecorRows, got %d: %+v", len(delta.DecorRows), delta.DecorRows)
	}
	gotIdx := map[uint16]bool{delta.DecorRows[0].RowIdx: true, delta.DecorRows[1].RowIdx: true}
	if !gotIdx[0] || !gotIdx[4] {
		t.Fatalf("expected decoration rows at rowIdx 0 and 4, got %v", gotIdx)
	}
	if len(delta.Rows) != 3 {
		t.Fatalf("expected 3 content Rows, got %d", len(delta.Rows))
	}
}

func TestBufferToDelta_DecorationRowsDiffed(t *testing.T) {
	rows := [][]texel.Cell{
		{{Ch: '+'}}, // border
		{{Ch: 'a'}}, // content
		{{Ch: '+'}}, // border
	}
	snap := texel.PaneSnapshot{
		ID:             [16]byte{0xab},
		Buffer:         rows,
		RowGlobalIdx:   []int64{-1, 100, -1},
		ContentTopRow:  1,
		NumContentRows: 1,
	}
	vp := ClientViewport{Rows: 1, AutoFollow: true}
	prev := [][]texel.Cell{
		{{Ch: '+'}},
		{{Ch: 'a'}},
		{{Ch: '+'}},
	}
	delta := bufferToDelta(snap, prev, 1, vp)
	if len(delta.DecorRows) != 0 {
		t.Fatalf("expected 0 DecorRows when borders unchanged, got %d", len(delta.DecorRows))
	}
	if len(delta.Rows) != 0 {
		t.Fatalf("expected 0 content Rows when content unchanged, got %d", len(delta.Rows))
	}
}

func TestBufferToDelta_DecorationRowsDiffPartial(t *testing.T) {
	rows := [][]texel.Cell{
		{{Ch: '+'}}, // border (will change)
		{{Ch: 'a'}}, // content
		{{Ch: '+'}}, // border (unchanged)
	}
	snap := texel.PaneSnapshot{
		ID:             [16]byte{0xab},
		Buffer:         rows,
		RowGlobalIdx:   []int64{-1, 100, -1},
		ContentTopRow:  1,
		NumContentRows: 1,
	}
	vp := ClientViewport{Rows: 1, AutoFollow: true}
	prev := [][]texel.Cell{
		{{Ch: '#'}}, // different
		{{Ch: 'a'}}, // same
		{{Ch: '+'}}, // same
	}
	delta := bufferToDelta(snap, prev, 1, vp)
	if len(delta.DecorRows) != 1 || delta.DecorRows[0].RowIdx != 0 {
		t.Fatalf("expected 1 DecorRows entry at rowIdx 0, got %+v", delta.DecorRows)
	}
}

func TestBufferToDelta_TexelTermInternalStatusbar(t *testing.T) {
	// 6-row layout: rowIdx 0 = top border, [1..3] = content, rowIdx 4 = app
	// internal statusbar (gid=-1), rowIdx 5 = bottom border.
	rows := [][]texel.Cell{
		{{Ch: '+'}},
		{{Ch: 'a'}},
		{{Ch: 'b'}},
		{{Ch: 'c'}},
		{{Ch: 'S'}},
		{{Ch: '+'}},
	}
	snap := texel.PaneSnapshot{
		ID:             [16]byte{0xab},
		Buffer:         rows,
		RowGlobalIdx:   []int64{-1, 100, 101, 102, -1, -1},
		ContentTopRow:  1,
		NumContentRows: 3,
	}
	vp := ClientViewport{Rows: 3, AutoFollow: true}
	delta := bufferToDelta(snap, nil, 1, vp)
	got := map[uint16]bool{}
	for _, r := range delta.DecorRows {
		got[r.RowIdx] = true
	}
	if !got[0] || !got[4] || !got[5] {
		t.Fatalf("expected DecorRows at rowIdx 0, 4, 5 (top + statusbar + bottom), got %v", got)
	}
}

func TestBufferToDelta_AltScreenLeavesDecorRowsEmpty(t *testing.T) {
	rows := [][]texel.Cell{{{Ch: 'x'}}}
	snap := texel.PaneSnapshot{
		ID:           [16]byte{0xab},
		Buffer:       rows,
		RowGlobalIdx: []int64{-1},
		AltScreen:    true,
	}
	vp := ClientViewport{Rows: 1, AltScreen: true}
	delta := bufferToDelta(snap, nil, 1, vp)
	if len(delta.DecorRows) != 0 {
		t.Fatalf("alt-screen must not emit DecorRows, got %d", len(delta.DecorRows))
	}
}

func TestBufferToDelta_ZeroContentSnapshot(t *testing.T) {
	// Status pane shape: every row is decoration (no content gids).
	// Server must emit all rows in DecorRows and zero content rows.
	rows := [][]texel.Cell{
		{{Ch: 'a'}},
		{{Ch: 'b'}},
		{{Ch: 'c'}},
	}
	snap := texel.PaneSnapshot{
		ID:             [16]byte{0xab},
		Buffer:         rows,
		RowGlobalIdx:   []int64{-1, -1, -1},
		ContentTopRow:  0,
		NumContentRows: 0,
	}
	vp := ClientViewport{Rows: 0, AutoFollow: false}
	delta := bufferToDelta(snap, nil, 1, vp)
	if len(delta.Rows) != 0 {
		t.Fatalf("expected 0 content Rows for zero-content pane, got %d", len(delta.Rows))
	}
	if len(delta.DecorRows) != 3 {
		t.Fatalf("expected 3 DecorRows for zero-content pane, got %d", len(delta.DecorRows))
	}
}

// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package texel

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// snapshotTestApp is a minimal non-terminal App for snapshot tests. It
// returns a fixed-size rendered buffer and does not implement
// RowGlobalIdxProvider — so its rows must default to -1 in the snapshot.
type snapshotTestApp struct {
	title    string
	cols     int
	rows     int
	notifier chan<- bool
}

func (a *snapshotTestApp) Run() error                       { return nil }
func (a *snapshotTestApp) Stop()                            {}
func (a *snapshotTestApp) Resize(cols, rows int)            { a.cols, a.rows = cols, rows }
func (a *snapshotTestApp) GetTitle() string                 { return a.title }
func (a *snapshotTestApp) HandleKey(*tcell.EventKey)        {}
func (a *snapshotTestApp) SetRefreshNotifier(c chan<- bool) { a.notifier = c }
func (a *snapshotTestApp) Render() [][]Cell {
	rows := a.rows
	cols := a.cols
	if rows <= 0 {
		rows = 1
	}
	if cols <= 0 {
		cols = 1
	}
	out := make([][]Cell, rows)
	for y := range out {
		out[y] = make([]Cell, cols)
		for x := range out[y] {
			out[y][x] = Cell{Ch: ' ', Style: tcell.StyleDefault}
		}
	}
	return out
}

// newSnapshotTestPane constructs a bare pane with a mock app for snapshot
// tests. The pane rectangle is set directly rather than through a workspace
// so we avoid pulling in DesktopEngine / layout machinery.
func newSnapshotTestPane(w, h int) *pane {
	p := newPane(nil)
	p.absX0, p.absY0 = 0, 0
	p.absX1, p.absY1 = w, h
	// Inner app content sits at (1,1) inside a (w,h) border.
	app := &snapshotTestApp{title: "mock", cols: w - 2, rows: h - 2}
	p.app = app
	return p
}

func TestPaneSnapshot_RowGlobalIdxInvariant(t *testing.T) {
	p := newSnapshotTestPane(20, 6)
	snap := capturePaneSnapshot(p)

	if snap.Buffer == nil {
		t.Fatal("expected non-nil buffer for pane snapshot")
	}
	if len(snap.RowGlobalIdx) != len(snap.Buffer) {
		t.Fatalf("len(RowGlobalIdx)=%d, len(Buffer)=%d — must match",
			len(snap.RowGlobalIdx), len(snap.Buffer))
	}

	// Mock (non-terminal) app: every row must be -1 since the app does
	// not implement RowGlobalIdxProvider.
	for y, gi := range snap.RowGlobalIdx {
		if gi != -1 {
			t.Errorf("row %d globalIdx = %d, want -1 for non-terminal app", y, gi)
		}
	}

	// Borders at the first and last rows must be -1 regardless of app type.
	h := len(snap.Buffer)
	if h >= 1 && snap.RowGlobalIdx[0] != -1 {
		t.Errorf("top border row: globalIdx = %d, want -1", snap.RowGlobalIdx[0])
	}
	if h >= 2 && snap.RowGlobalIdx[h-1] != -1 {
		t.Errorf("bottom border row: globalIdx = %d, want -1", snap.RowGlobalIdx[h-1])
	}
}

// TestPaneSnapshot_RowGlobalIdxLengthMatchesBuffer exercises the invariant
// across a few pane sizes, including the degenerate "pane too small to
// decorate" case where the buffer is still produced but has no borders.
func TestPaneSnapshot_RowGlobalIdxLengthMatchesBuffer(t *testing.T) {
	for _, sz := range []struct{ w, h int }{
		{10, 5},
		{20, 8},
		{4, 4},
	} {
		p := newSnapshotTestPane(sz.w, sz.h)
		snap := capturePaneSnapshot(p)
		if len(snap.RowGlobalIdx) != len(snap.Buffer) {
			t.Errorf("size %dx%d: len(RowGlobalIdx)=%d, len(Buffer)=%d",
				sz.w, sz.h, len(snap.RowGlobalIdx), len(snap.Buffer))
		}
	}
}

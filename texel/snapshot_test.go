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

func TestCapturePaneSnapshot_ContentBoundsComputed(t *testing.T) {
	// 6-row pane: [0]=-1 (top border), [1..3]=content, [4]=-1 (app statusbar), [5]=-1 (bottom border)
	rowIdx := []int64{-1, 100, 101, 102, -1, -1}
	top, num := computeContentBounds(rowIdx)
	if top != 1 || num != 3 {
		t.Fatalf("expected top=1 num=3, got top=%d num=%d", top, num)
	}
}

func TestCapturePaneSnapshot_ContentBoundsAllDecoration(t *testing.T) {
	// All -1 rows: zero content, top=0 num=0.
	rowIdx := []int64{-1, -1, -1}
	top, num := computeContentBounds(rowIdx)
	if top != 0 || num != 0 {
		t.Fatalf("expected top=0 num=0, got top=%d num=%d", top, num)
	}
}

func TestCapturePaneSnapshot_ContentBoundsEmpty(t *testing.T) {
	top, num := computeContentBounds(nil)
	if top != 0 || num != 0 {
		t.Fatalf("expected top=0 num=0, got top=%d num=%d", top, num)
	}
}

func TestComputeContentBounds_MidRangeHolesTolerated(t *testing.T) {
	// Mid-range gid<0 holes are legitimate — they represent unwritten
	// content rows in a fresh terminal. The bounds span from the first
	// to the last gid>=0; the renderer renders the holes as blank cells.
	rowIdx := []int64{-1, 100, -1, 102, -1}
	top, num := computeContentBounds(rowIdx)
	if top != 1 || num != 3 {
		t.Fatalf("expected (1, 3) for [first..last] span across mid-hole, got (%d, %d)", top, num)
	}
}

// snapshotTestTerminalApp implements RowGlobalIdxProvider so applyStructuralBounds
// can exercise the texterm-shaped content path.
type snapshotTestTerminalApp struct {
	snapshotTestApp
	rowIdx []int64
}

func (a *snapshotTestTerminalApp) RowGlobalIdx() []int64 {
	out := make([]int64, len(a.rowIdx))
	copy(out, a.rowIdx)
	return out
}

func newTerminalSnapshotPane(w, h int, rowIdx []int64) *pane {
	p := newPane(nil)
	p.absX0, p.absY0 = 0, 0
	p.absX1, p.absY1 = w, h
	app := &snapshotTestTerminalApp{
		snapshotTestApp: snapshotTestApp{title: "term", cols: w - 2, rows: h - 2},
		rowIdx:          rowIdx,
	}
	p.app = app
	return p
}

// TestApplyStructuralBounds_TerminalPane verifies the geometry-only bounds
// helper used by GeometryForClient. The pane is a 6-tall texterm-style
// pane with three populated content gids; the helper must report
// ContentTopRow=1, NumContentRows=h-2 (=4), AltScreen=false. This is the
// same answer capturePaneSnapshot returns for the same pane — the two
// must agree so the resize path (GeometryForClient) does not silently
// blank the client's content bounds.
func TestApplyStructuralBounds_TerminalPane(t *testing.T) {
	rowIdx := []int64{100, 101, 102, 103} // h-2 = 4 entries
	p := newTerminalSnapshotPane(20, 6, rowIdx)

	var snap PaneSnapshot
	applyStructuralBounds(&snap, p)

	if snap.AltScreen {
		t.Fatalf("expected AltScreen=false for terminal pane")
	}
	if snap.ContentTopRow != 1 {
		t.Fatalf("expected ContentTopRow=1, got %d", snap.ContentTopRow)
	}
	if snap.NumContentRows != 4 {
		t.Fatalf("expected NumContentRows=4 (h=6 → h-2), got %d", snap.NumContentRows)
	}

	// And the answer must match capturePaneSnapshot for the same pane —
	// otherwise the geometry-only path drifts from the full path.
	full := capturePaneSnapshot(p)
	if full.ContentTopRow != snap.ContentTopRow || full.NumContentRows != snap.NumContentRows {
		t.Fatalf("structural bounds drift: full=(top=%d,num=%d) geometry=(top=%d,num=%d)",
			full.ContentTopRow, full.NumContentRows, snap.ContentTopRow, snap.NumContentRows)
	}
}

// TestApplyStructuralBounds_TerminalPaneWithStatusbar tests the texterm
// internal-statusbar pattern: appIdx[len-1] < 0 reduces NumContentRows by 1.
func TestApplyStructuralBounds_TerminalPaneWithStatusbar(t *testing.T) {
	rowIdx := []int64{100, 101, 102, -1} // statusbar at the bottom
	p := newTerminalSnapshotPane(20, 6, rowIdx)

	var snap PaneSnapshot
	applyStructuralBounds(&snap, p)

	if snap.NumContentRows != 3 {
		t.Fatalf("expected NumContentRows=3 (h=6 → h-2-1), got %d", snap.NumContentRows)
	}
	if snap.ContentTopRow != 1 {
		t.Fatalf("expected ContentTopRow=1, got %d", snap.ContentTopRow)
	}

	// Must agree with the full snapshot path.
	full := capturePaneSnapshot(p)
	if full.ContentTopRow != snap.ContentTopRow || full.NumContentRows != snap.NumContentRows {
		t.Fatalf("structural bounds drift with trailing statusbar: full=(top=%d,num=%d) geometry=(top=%d,num=%d)",
			full.ContentTopRow, full.NumContentRows, snap.ContentTopRow, snap.NumContentRows)
	}
}

// TestApplyStructuralBounds_NonTerminalPane verifies that an app without
// RowGlobalIdxProvider gets AltScreen=true and zero content bounds —
// matching what capturePaneSnapshot would set.
func TestApplyStructuralBounds_NonTerminalPane(t *testing.T) {
	p := newSnapshotTestPane(20, 6)
	var snap PaneSnapshot
	applyStructuralBounds(&snap, p)
	if !snap.AltScreen {
		t.Fatalf("expected AltScreen=true for non-RowGlobalIdxProvider app")
	}
	if snap.NumContentRows != 0 {
		t.Fatalf("expected NumContentRows=0 for non-terminal app, got %d", snap.NumContentRows)
	}
}

// snapshotTestStyledTerminalApp paints a non-default-styled content row.
// This exercises the case where the BufferWidget actually paints into the
// pane buffer (cells with style != tcell.StyleDefault are NOT skipped).
type snapshotTestStyledTerminalApp struct {
	snapshotTestTerminalApp
}

func (a *snapshotTestStyledTerminalApp) Render() [][]Cell {
	rows := a.rows
	cols := a.cols
	if rows <= 0 {
		rows = 1
	}
	if cols <= 0 {
		cols = 1
	}
	// Use a style with explicit foreground so it is non-zero — BufferWidget
	// will paint these cells into the pane buffer instead of skipping them.
	contentStyle := tcell.StyleDefault.Foreground(tcell.ColorRed)
	out := make([][]Cell, rows)
	for y := range out {
		out[y] = make([]Cell, cols)
		for x := range out[y] {
			out[y][x] = Cell{Ch: 'X', Style: contentStyle}
		}
	}
	return out
}

// snapshotTestNoResizeApp is a styled terminal app that IGNORES Resize
// requests and always returns a render buffer at its initial dimensions.
// This mimics the sparseFakeApp pattern (Resize is a no-op) which makes
// it possible for appBuffer to be larger than the pane's drawable area.
type snapshotTestNoResizeApp struct {
	snapshotTestStyledTerminalApp
}

func (a *snapshotTestNoResizeApp) Resize(cols, rows int) {
	// no-op — keep initial dimensions
}

// TestPaneRenderBuffer_BordersSurviveOversizedAppBuffer reproduces the
// d3921cb concern: an app whose Render() returns a buffer LARGER than the
// pane's ClientRect. Without the d3921cb clamp, BufferWidget would paint
// past col W-1 and row H-1, overwriting the borders. With d3921cb the
// clamp keeps the child painting inside.
func TestPaneRenderBuffer_BordersSurviveOversizedAppBuffer(t *testing.T) {
	const w, h = 20, 6
	rowIdx := []int64{100, 101, 102, 103}
	p := newPane(nil)
	p.absX0, p.absY0 = 0, 0
	p.absX1, p.absY1 = w, h
	app := &snapshotTestNoResizeApp{
		snapshotTestStyledTerminalApp: snapshotTestStyledTerminalApp{
			snapshotTestTerminalApp: snapshotTestTerminalApp{
				// Initialize with FULL pane dims, so Render returns a w×h
				// buffer instead of the expected (w-2)×(h-2).
				snapshotTestApp: snapshotTestApp{title: "term", cols: w, rows: h},
				rowIdx:          rowIdx,
			},
		},
	}
	p.app = app

	buf := p.renderBuffer(false)
	if len(buf) != h {
		t.Fatalf("render buffer height = %d, want %d", len(buf), h)
	}
	for y, row := range buf {
		if len(row) != w {
			t.Fatalf("row %d width = %d, want %d", y, len(row), w)
		}
	}

	// Interior rows must keep '│' at col 0 and col W-1 even though the
	// app's render buffer was oversized.
	for y := 1; y < h-1; y++ {
		got0 := buf[y][0].Ch
		gotR := buf[y][w-1].Ch
		if got0 != '│' && got0 != '|' {
			t.Errorf("row %d col 0 = %q, want '│' (left border overwritten by oversized app buffer)", y, got0)
		}
		if gotR != '│' && gotR != '|' {
			t.Errorf("row %d col W-1 = %q, want '│' (right border overwritten by oversized app buffer)", y, gotR)
		}
	}
	// Bottom row keeps its horizontal border characters.
	if buf[h-1][1].Ch != '─' && buf[h-1][1].Ch != '-' {
		t.Errorf("bottom border row col 1 = %q, want '─'", buf[h-1][1].Ch)
	}
	// Top row corners are still '╭' and '╮' (or '┌' / '┐').
	if buf[0][0].Ch != '╭' && buf[0][0].Ch != '┌' {
		t.Errorf("top border row col 0 = %q, want corner glyph", buf[0][0].Ch)
	}
	if buf[0][w-1].Ch != '╮' && buf[0][w-1].Ch != '┐' {
		t.Errorf("top border row col W-1 = %q, want corner glyph", buf[0][w-1].Ch)
	}
}

// TestPaneRenderBuffer_BordersSurviveStyledContent reproduces the server-side
// half of the issue #199 follow-up "patchy border / 1-col left shift" bug.
//
// Setup: a terminal-style app whose Render() returns a (w-2, h-2) buffer
// of cells with a NON-DEFAULT style (so BufferWidget paints them, not
// skips them). This mirrors texterm after `ls<enter>` — the prompt is
// painted with explicit fg/bg from the palette, no longer pure
// StyleDefault. The pane must still produce border chars at col 0 and
// col W-1 of every interior row.
//
// If this fails, the SetBuffer-then-clamp dance in pane_render.go is
// not actually keeping the BufferWidget inside ClientRect, and the
// child is overwriting the side borders.
func TestPaneRenderBuffer_BordersSurviveStyledContent(t *testing.T) {
	const w, h = 20, 6
	rowIdx := []int64{100, 101, 102, 103} // h-2 = 4 entries, all content
	p := newPane(nil)
	p.absX0, p.absY0 = 0, 0
	p.absX1, p.absY1 = w, h
	app := &snapshotTestStyledTerminalApp{
		snapshotTestTerminalApp: snapshotTestTerminalApp{
			snapshotTestApp: snapshotTestApp{title: "term", cols: w - 2, rows: h - 2},
			rowIdx:          rowIdx,
		},
	}
	p.app = app

	buf := p.renderBuffer(false)
	if len(buf) != h {
		t.Fatalf("render buffer height = %d, want %d", len(buf), h)
	}
	for y, row := range buf {
		if len(row) != w {
			t.Fatalf("row %d width = %d, want %d", y, len(row), w)
		}
	}

	// Top and bottom rows are the horizontal border rows; corners/horizontals.
	// Interior rows (y in [1, h-2]) must have a vertical bar at col 0 and col w-1.
	for y := 1; y < h-1; y++ {
		got0 := buf[y][0].Ch
		gotR := buf[y][w-1].Ch
		if got0 != '│' && got0 != '|' {
			t.Errorf("row %d col 0 = %q, want '│' (left border overwritten by content)", y, got0)
		}
		if gotR != '│' && gotR != '|' {
			t.Errorf("row %d col W-1 = %q, want '│' (right border overwritten by content)", y, gotR)
		}
		// Interior content cells (col 1..w-2) should be the app's 'X'.
		for x := 1; x < w-1; x++ {
			if buf[y][x].Ch != 'X' {
				t.Errorf("row %d col %d = %q, want 'X' (content shifted)", y, x, buf[y][x].Ch)
			}
		}
	}
}

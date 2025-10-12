package client

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"texelation/protocol"
)

func TestBufferCacheApplyDelta(t *testing.T) {
	cache := NewBufferCache()
	var id [16]byte
	id[0] = 1

	delta := protocol.BufferDelta{
		PaneID:   id,
		Revision: 1,
		Styles: []protocol.StyleEntry{{
			AttrFlags: protocol.AttrBold,
			FgModel:   protocol.ColorModelRGB,
			FgValue:   0x112233,
			BgModel:   protocol.ColorModelDefault,
		}},
		Rows: []protocol.RowDelta{
			{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "Hello", StyleIndex: 0}}},
			{Row: 1, Spans: []protocol.CellSpan{{StartCol: 2, Text: "World", StyleIndex: 0}}},
		},
	}

	cache.ApplyDelta(delta)
	var rows []string
	var cells []Cell
	cache.ForEachPaneSorted(func(p *PaneState) {
		if p.ID != id {
			return
		}
		rows = append([]string(nil), p.Rows()...)
		cells = append([]Cell(nil), p.RowCells(0)...)
	})
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0] != "Hello" {
		t.Fatalf("unexpected row0 %q", rows[0])
	}
	if rows[1] != "  World" {
		t.Fatalf("unexpected row1 %q", rows[1])
	}
	if len(cells) < 5 {
		t.Fatalf("expected 5 cells, got %d", len(cells))
	}
	fg, _, attrs := cells[0].Style.Decompose()
	if attrs&tcell.AttrBold == 0 {
		t.Fatalf("expected bold style")
	}
	if r, g, b := fg.RGB(); r != 0x11 || g != 0x22 || b != 0x33 {
		t.Fatalf("unexpected fg colour %x,%x,%x", r, g, b)
	}

	delta2 := protocol.BufferDelta{
		PaneID:   id,
		Revision: 2,
		Rows:     []protocol.RowDelta{{Row: 0, Spans: []protocol.CellSpan{{StartCol: 5, Text: "!", StyleIndex: 0}}}},
	}
	cache.ApplyDelta(delta2)
	rows = nil
	cache.ForEachPaneSorted(func(p *PaneState) {
		if p.ID == id {
			rows = append([]string(nil), p.Rows()...)
		}
	})
	if rows[0] != "Hello!" {
		t.Fatalf("expected Hello!, got %q", rows[0])
	}
}

func TestBufferCacheApplySnapshot(t *testing.T) {
	cache := NewBufferCache()
	var id [16]byte
	id[0] = 2

	snapshot := protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID:   id,
			Revision: 3,
			Title:    "pane",
			Rows:     []string{"abc", "def"},
		}},
	}

	cache.ApplySnapshot(snapshot)
	var rows []string
	cache.ForEachPaneSorted(func(p *PaneState) {
		if p.ID == id {
			rows = append([]string(nil), p.Rows()...)
		}
	})
	if rows[1] != "def" {
		t.Fatalf("unexpected row %q", rows[1])
	}
}

func TestBufferCacheApplySnapshotPrunesMissingPanes(t *testing.T) {
	cache := NewBufferCache()
	var id1, id2 [16]byte
	id1[0] = 1
	id2[0] = 2
	cache.ApplySnapshot(protocol.TreeSnapshot{Panes: []protocol.PaneSnapshot{{PaneID: id1}, {PaneID: id2}}})

	cache.ApplySnapshot(protocol.TreeSnapshot{Panes: []protocol.PaneSnapshot{{PaneID: id2}}})
	count := 0
	cache.ForEachPaneSorted(func(p *PaneState) {
		count++
		if p.ID != id2 {
			t.Fatalf("expected remaining pane id2, got %v", p.ID)
		}
	})
	if count != 1 {
		t.Fatalf("expected single pane after prune, got %d", count)
	}
}

func TestBufferCacheSetPaneFlags(t *testing.T) {
	cache := NewBufferCache()
	var id [16]byte
	id[0] = 9
	pane := cache.SetPaneFlags(id, true, true, 7)
	if !pane.Active || !pane.Resizing {
		t.Fatalf("expected flags to be set")
	}
	if pane.ZOrder != 7 {
		t.Fatalf("expected z-order 7, got %d", pane.ZOrder)
	}
	cache.SetPaneFlags(id, false, true, -3)
	if pane.Active || !pane.Resizing || pane.ZOrder != -3 {
		t.Fatalf("expected active=false resizing=true z=-3, got %+v", pane)
	}
}

func TestBufferCacheResumeFlow(t *testing.T) {
	cache := NewBufferCache()
	var id [16]byte
	copy(id[:], []byte{0xaa, 0xbb, 0xcc, 0xdd})

	delta := protocol.BufferDelta{
		PaneID:   id,
		Revision: 1,
		Rows: []protocol.RowDelta{{
			Row:   0,
			Spans: []protocol.CellSpan{{StartCol: 0, Text: "hello", StyleIndex: 0}},
		}},
	}
	cache.ApplyDelta(delta)
	var revision uint32
	var rows []string
	cache.ForEachPaneSorted(func(p *PaneState) {
		if p.ID == id {
			revision = p.Revision
			rows = append([]string(nil), p.Rows()...)
		}
	})
	if revision != 1 {
		t.Fatalf("expected revision 1, got %d", revision)
	}
	if len(rows) == 0 || rows[0] != "hello" {
		t.Fatalf("expected delta content, got %q", rows)
	}

	snapshot := protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID:   id,
			Revision: 5,
			Title:    "pane",
			Rows:     []string{"world"},
			X:        4,
			Y:        3,
			Width:    10,
			Height:   2,
		}},
	}
	cache.ApplySnapshot(snapshot)
	revision = 0
	rows = nil
	var rect clientRect
	cache.ForEachPaneSorted(func(p *PaneState) {
		if p.ID == id {
			revision = p.Revision
			rows = append([]string(nil), p.Rows()...)
			rect = p.Rect
		}
	})
	if revision != 5 {
		t.Fatalf("expected revision 5 after snapshot, got %d", revision)
	}
	if len(rows) == 0 || rows[0] != "world" {
		t.Fatalf("expected snapshot content 'world', got %q", rows)
	}
	if rect.X != 4 || rect.Y != 3 || rect.Width != 10 || rect.Height != 2 {
		t.Fatalf("unexpected rect %+v", rect)
	}

	staleDelta := protocol.BufferDelta{
		PaneID:   id,
		Revision: 4,
		Rows:     []protocol.RowDelta{{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "stale", StyleIndex: 0}}}},
	}
	cache.ApplyDelta(staleDelta)
	rows = nil
	cache.ForEachPaneSorted(func(p *PaneState) {
		if p.ID == id {
			rows = append([]string(nil), p.Rows()...)
		}
	})
	if len(rows) == 0 || rows[0] != "world" {
		t.Fatalf("stale delta should be ignored, got %q", rows)
	}

	resumeDelta := protocol.BufferDelta{
		PaneID:   id,
		Revision: 6,
		Rows:     []protocol.RowDelta{{Row: 0, Spans: []protocol.CellSpan{{StartCol: 5, Text: "!", StyleIndex: 0}}}},
	}
	cache.ApplyDelta(resumeDelta)
	revision = 0
	rows = nil
	cache.ForEachPaneSorted(func(p *PaneState) {
		if p.ID == id {
			revision = p.Revision
			rows = append([]string(nil), p.Rows()...)
		}
	})
	if revision != 6 {
		t.Fatalf("expected revision 6, got %d", revision)
	}
	if len(rows) == 0 || rows[0] != "world!" {
		t.Fatalf("expected merged resume delta, got %q", rows)
	}
}

func TestBufferCacheLayoutPanesOrdersByGeometry(t *testing.T) {
	cache := NewBufferCache()

	var id1, id2, id3 [16]byte
	id1[0] = 1
	id2[0] = 2
	id3[0] = 3

	snapshot := protocol.TreeSnapshot{Panes: []protocol.PaneSnapshot{
		{PaneID: id2, X: 20, Y: 0, Width: 10, Height: 5},
		{PaneID: id1, X: 0, Y: 0, Width: 10, Height: 5},
		{PaneID: id3, X: 0, Y: 10, Width: 10, Height: 5},
	}}
	cache.ApplySnapshot(snapshot)

	var ids [][16]byte
	cache.ForEachPaneSorted(func(p *PaneState) {
		ids = append(ids, p.ID)
	})
	if len(ids) != 3 {
		t.Fatalf("expected 3 panes, got %d", len(ids))
	}
	if ids[0] != id1 {
		t.Fatalf("expected pane id1 first, got %v", ids[0])
	}
	if ids[1] != id2 {
		t.Fatalf("expected pane id2 second, got %v", ids[1])
	}
	if ids[2] != id3 {
		t.Fatalf("expected pane id3 third, got %v", ids[2])
	}
}

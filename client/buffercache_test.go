package client

import (
	"testing"

	"texelation/protocol"
)

func TestBufferCacheApplyDelta(t *testing.T) {
	cache := NewBufferCache()
	var id [16]byte
	id[0] = 1

	delta := protocol.BufferDelta{
		PaneID:   id,
		Revision: 1,
		Rows: []protocol.RowDelta{
			{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "Hello", StyleIndex: 0}}},
			{Row: 1, Spans: []protocol.CellSpan{{StartCol: 2, Text: "World", StyleIndex: 0}}},
		},
	}

	state := cache.ApplyDelta(delta)
	if state == nil {
		t.Fatalf("expected pane state")
	}
	rows := state.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0] != "Hello" {
		t.Fatalf("unexpected row0 %q", rows[0])
	}
	if rows[1] != "  World" {
		t.Fatalf("unexpected row1 %q", rows[1])
	}

	delta2 := protocol.BufferDelta{
		PaneID:   id,
		Revision: 2,
		Rows:     []protocol.RowDelta{{Row: 0, Spans: []protocol.CellSpan{{StartCol: 5, Text: "!", StyleIndex: 0}}}},
	}
	state = cache.ApplyDelta(delta2)
	rows = state.Rows()
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
	panes := cache.AllPanes()
	if len(panes) != 1 {
		t.Fatalf("expected 1 pane, got %d", len(panes))
	}
	rows := panes[0].Rows()
	if rows[1] != "def" {
		t.Fatalf("unexpected row %q", rows[1])
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
	state := cache.ApplyDelta(delta)
	if state.Revision != 1 {
		t.Fatalf("expected revision 1, got %d", state.Revision)
	}
	if got := state.Rows()[0]; got != "hello" {
		t.Fatalf("expected delta content, got %q", got)
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
	panes := cache.AllPanes()
	if len(panes) != 1 {
		t.Fatalf("expected 1 pane after snapshot, got %d", len(panes))
	}
	pane := panes[0]
	if pane.Revision != 5 {
		t.Fatalf("expected revision 5 after snapshot, got %d", pane.Revision)
	}
	if got := pane.Rows()[0]; got != "world" {
		t.Fatalf("expected snapshot content 'world', got %q", got)
	}
	if pane.Rect.X != 4 || pane.Rect.Y != 3 || pane.Rect.Width != 10 || pane.Rect.Height != 2 {
		t.Fatalf("unexpected rect %+v", pane.Rect)
	}

	staleDelta := protocol.BufferDelta{
		PaneID:   id,
		Revision: 4,
		Rows:     []protocol.RowDelta{{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "stale", StyleIndex: 0}}}},
	}
	cache.ApplyDelta(staleDelta)
	if got := pane.Rows()[0]; got != "world" {
		t.Fatalf("stale delta should be ignored, got %q", got)
	}

	resumeDelta := protocol.BufferDelta{
		PaneID:   id,
		Revision: 6,
		Rows:     []protocol.RowDelta{{Row: 0, Spans: []protocol.CellSpan{{StartCol: 5, Text: "!", StyleIndex: 0}}}},
	}
	state = cache.ApplyDelta(resumeDelta)
	if state.Revision != 6 {
		t.Fatalf("expected revision 6, got %d", state.Revision)
	}
	if got := state.Rows()[0]; got != "world!" {
		t.Fatalf("expected merged resume delta, got %q", got)
	}
}

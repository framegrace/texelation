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

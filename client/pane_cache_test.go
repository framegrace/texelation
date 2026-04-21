// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package client

import (
	"testing"

	"github.com/framegrace/texelation/protocol"
)

func TestPaneCache_ApplyDeltaMainScreen(t *testing.T) {
	pc := NewPaneCache()
	pc.ApplyDelta(protocol.BufferDelta{
		RowBase: 1_000,
		Rows: []protocol.RowDelta{
			{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "hello", StyleIndex: 0}}},
			{Row: 2, Spans: []protocol.CellSpan{{StartCol: 0, Text: "world", StyleIndex: 0}}},
		},
		Styles: []protocol.StyleEntry{{AttrFlags: 0}},
	})
	if row, ok := pc.RowAt(1_000); !ok || !rowStartsWith(row, "hello") {
		t.Fatalf("globalIdx 1000 missing or wrong: ok=%v row=%v", ok, row)
	}
	if _, ok := pc.RowAt(1_001); ok {
		t.Fatalf("globalIdx 1001 should be absent (no delta row)")
	}
	if row, ok := pc.RowAt(1_002); !ok || !rowStartsWith(row, "world") {
		t.Fatalf("globalIdx 1002 missing or wrong")
	}
}

func TestPaneCache_ApplyDeltaAltScreen(t *testing.T) {
	pc := NewPaneCache()
	pc.ApplyDelta(protocol.BufferDelta{
		Flags:  protocol.BufferDeltaAltScreen,
		Rows:   []protocol.RowDelta{{Row: 3, Spans: []protocol.CellSpan{{StartCol: 0, Text: "vim", StyleIndex: 0}}}},
		Styles: []protocol.StyleEntry{{AttrFlags: 0}},
	})
	if !pc.IsAltScreen() {
		t.Fatalf("expected alt-screen mode")
	}
	if row, ok := pc.AltRowAt(3); !ok || !rowStartsWith(row, "vim") {
		t.Fatalf("alt row 3 missing")
	}
}

func TestPaneCache_EvictsOutsideWindow(t *testing.T) {
	pc := NewPaneCache()
	pc.ApplyDelta(protocol.BufferDelta{
		RowBase: 0,
		Rows: []protocol.RowDelta{
			{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "a", StyleIndex: 0}}},
			{Row: 1, Spans: []protocol.CellSpan{{StartCol: 0, Text: "b", StyleIndex: 0}}},
			{Row: 2, Spans: []protocol.CellSpan{{StartCol: 0, Text: "c", StyleIndex: 0}}},
		},
		Styles: []protocol.StyleEntry{{AttrFlags: 0}},
	})
	// Viewport is [1,2] + 1× overscan of 2 rows → [−1, 4]; hysteresis 1.5×.
	pc.Evict(1, 2, 2)
	if _, ok := pc.RowAt(0); !ok {
		t.Fatalf("row 0 inside hysteresis band, should be retained")
	}
	// Now slam the viewport far away — eviction should clear row 0.
	pc.Evict(1_000, 1_002, 2)
	if _, ok := pc.RowAt(0); ok {
		t.Fatalf("row 0 should be evicted after viewport jump")
	}
}

func rowStartsWith(row []Cell, s string) bool {
	if len(row) < len(s) {
		return false
	}
	var buf []rune
	for _, c := range row {
		buf = append(buf, c.Ch)
	}
	return string(buf[:len(s)]) == s
}

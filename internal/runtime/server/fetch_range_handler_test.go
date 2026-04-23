// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/apps/texelterm/parser/sparse"
	"github.com/framegrace/texelation/protocol"
)

func TestFetchRangeHandler_Basic(t *testing.T) {
	st := sparse.NewStore(80)
	st.Set(100, 0, parser.Cell{Rune: 'a'})
	st.Set(101, 0, parser.Cell{Rune: 'b'})

	resp, err := ServeFetchRange(st, protocol.FetchRange{
		LoIdx: 100,
		HiIdx: 102,
	}, 42)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if resp.Revision != 42 {
		t.Fatalf("revision: got %d want 42", resp.Revision)
	}
	if len(resp.Rows) != 2 {
		t.Fatalf("rows: got %d want 2", len(resp.Rows))
	}
	if resp.Rows[0].GlobalIdx != 100 {
		t.Fatalf("row[0].GlobalIdx: got %d want 100", resp.Rows[0].GlobalIdx)
	}
}

func TestFetchRangeHandler_EmptyStoreNotBelowRetention(t *testing.T) {
	st := sparse.NewStore(80)
	// No writes — OldestRetained() returns -1.
	resp, err := ServeFetchRange(st, protocol.FetchRange{
		LoIdx: 0,
		HiIdx: 10,
	}, 0)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if resp.Flags&protocol.FetchRangeBelowRetention != 0 {
		t.Fatalf("expected no BelowRetention flag on empty store, got flags=%b", resp.Flags)
	}
	if resp.Flags&protocol.FetchRangeEmpty == 0 {
		t.Fatalf("expected FetchRangeEmpty flag on empty store, got flags=%b", resp.Flags)
	}
}

func TestFetchRangeHandler_BelowRetention(t *testing.T) {
	st := sparse.NewStore(80)
	st.Set(500, 0, parser.Cell{Rune: 'a'})
	resp, err := ServeFetchRange(st, protocol.FetchRange{
		LoIdx: 0,
		HiIdx: 10,
	}, 1)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if resp.Flags&protocol.FetchRangeBelowRetention == 0 {
		t.Fatalf("expected BelowRetention flag")
	}
}

func TestFetchRangeHandler_EmptyRange(t *testing.T) {
	st := sparse.NewStore(80)
	st.Set(50, 0, parser.Cell{Rune: 'x'})

	req := protocol.FetchRange{
		RequestID: 0xABCD,
		PaneID:    [16]byte{0x01, 0x02},
		LoIdx:     10,
		HiIdx:     10, // empty range: lo == hi
	}
	resp, err := ServeFetchRange(st, req, 7)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if resp.Flags&protocol.FetchRangeEmpty == 0 {
		t.Fatalf("expected FetchRangeEmpty flag, got flags=%b", resp.Flags)
	}
	if resp.RequestID != req.RequestID {
		t.Fatalf("RequestID echo: got %d want %d", resp.RequestID, req.RequestID)
	}
	if resp.PaneID != req.PaneID {
		t.Fatalf("PaneID echo mismatch")
	}
}

func TestFetchRangeHandler_SparseHoles(t *testing.T) {
	st := sparse.NewStore(80)
	st.Set(100, 0, parser.Cell{Rune: 'a'})
	// idx 101 intentionally omitted — sparse hole
	st.Set(102, 0, parser.Cell{Rune: 'c'})

	resp, err := ServeFetchRange(st, protocol.FetchRange{
		LoIdx: 100,
		HiIdx: 103,
	}, 1)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if len(resp.Rows) != 2 {
		t.Fatalf("rows: got %d want 2", len(resp.Rows))
	}
	if resp.Rows[0].GlobalIdx != 100 {
		t.Fatalf("row[0].GlobalIdx: got %d want 100", resp.Rows[0].GlobalIdx)
	}
	if resp.Rows[1].GlobalIdx != 102 {
		t.Fatalf("row[1].GlobalIdx: got %d want 102", resp.Rows[1].GlobalIdx)
	}
}

func TestFetchRangeHandler_FlagPropagation(t *testing.T) {
	st := sparse.NewStore(80)

	// Row 200: last cell has Wrapped=true
	st.Set(200, 0, parser.Cell{Rune: 'a'})
	st.Set(200, 1, parser.Cell{Rune: 'b', Wrapped: true})

	// Row 201: NoWrap set via SetRowNoWrap
	st.Set(201, 0, parser.Cell{Rune: 'x'})
	st.SetRowNoWrap(201, true)

	resp, err := ServeFetchRange(st, protocol.FetchRange{
		LoIdx: 200,
		HiIdx: 202,
	}, 1)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if len(resp.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(resp.Rows))
	}

	row200 := resp.Rows[0]
	if row200.GlobalIdx != 200 {
		t.Fatalf("row200 GlobalIdx: got %d want 200", row200.GlobalIdx)
	}
	if !row200.Wrapped {
		t.Fatalf("row200: expected Wrapped=true")
	}

	row201 := resp.Rows[1]
	if row201.GlobalIdx != 201 {
		t.Fatalf("row201 GlobalIdx: got %d want 201", row201.GlobalIdx)
	}
	if !row201.NoWrap {
		t.Fatalf("row201: expected NoWrap=true")
	}
}

func TestFetchRangeHandler_RequestIDAndPaneIDEcho(t *testing.T) {
	st := sparse.NewStore(80)
	st.Set(0, 0, parser.Cell{Rune: 'z'})

	paneID := [16]byte{
		0xCA, 0xFE, 0xBA, 0xBE, 0x00, 0x01, 0x02, 0x03,
		0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B,
	}
	req := protocol.FetchRange{
		RequestID: 0xCAFEBABE,
		PaneID:    paneID,
		LoIdx:     0,
		HiIdx:     1,
	}
	resp, err := ServeFetchRange(st, req, 99)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if resp.RequestID != 0xCAFEBABE {
		t.Fatalf("RequestID: got 0x%X want 0xCAFEBABE", resp.RequestID)
	}
	if resp.PaneID != paneID {
		t.Fatalf("PaneID mismatch: got %v want %v", resp.PaneID, paneID)
	}
}

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

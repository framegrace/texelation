// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package protocol

import (
	"reflect"
	"testing"
)

func makePickerID(b byte) [16]byte {
	var id [16]byte
	for i := range id {
		id[i] = b
	}
	return id
}

func TestSessionSummary_RoundTrip(t *testing.T) {
	in := SessionSummary{
		SessionID:      makePickerID(0xAB),
		Label:          "work-laptop",
		LastActive:     1714752000,
		PaneCount:      4,
		FirstPaneTitle: "nvim · texelation/cmd/texelation",
		Pinned:         true,
		HasThumbnail:   true,
		Layout: &TreeNodeSnapshot{
			PaneIndex:   -1,
			Split:       SplitHorizontal,
			SplitRatios: []float32{0.5, 0.5},
			Children: []TreeNodeSnapshot{
				{PaneIndex: 0, Split: SplitNone},
				{PaneIndex: 1, Split: SplitNone},
			},
		},
	}
	encoded, err := EncodeSessionSummary(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, consumed, err := DecodeSessionSummary(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if consumed != len(encoded) {
		t.Fatalf("consumed=%d, len=%d", consumed, len(encoded))
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch:\n in=%#v\nout=%#v", in, out)
	}
}

func TestSessionSummary_NilLayout(t *testing.T) {
	in := SessionSummary{
		SessionID:    makePickerID(0x01),
		Label:        "untitled",
		LastActive:   100,
		PaneCount:    1,
		HasThumbnail: false,
		Layout:       nil,
	}
	encoded, err := EncodeSessionSummary(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, _, err := DecodeSessionSummary(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Layout != nil {
		t.Fatalf("expected nil Layout after round-trip, got %#v", out.Layout)
	}
}

func TestLiveSummary_RoundTrip(t *testing.T) {
	in := LiveSummary{
		SessionID:       makePickerID(0xCD),
		Label:           "debug-cluster",
		AttachedClients: 2,
		PaneCount:       3,
		LastInputAt:     1714752100,
	}
	encoded, err := EncodeLiveSummary(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, consumed, err := DecodeLiveSummary(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if consumed != len(encoded) {
		t.Fatalf("consumed=%d, len=%d", consumed, len(encoded))
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch:\n in=%#v\nout=%#v", in, out)
	}
}

func TestListSessionsRequest_RoundTrip(t *testing.T) {
	in := ListSessionsRequest{}
	encoded, err := EncodeListSessionsRequest(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(encoded) != 0 {
		t.Fatalf("expected empty payload for v5 request, got %d bytes", len(encoded))
	}
	out, err := DecodeListSessionsRequest(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip mismatch: in=%#v out=%#v", in, out)
	}
}

func TestListSessionsResponse_RoundTrip(t *testing.T) {
	in := ListSessionsResponse{
		Live: []LiveSummary{
			{SessionID: makePickerID(0xAA), Label: "live-1", PaneCount: 2, LastInputAt: 200},
		},
		Stored: []SessionSummary{
			{SessionID: makePickerID(0xBB), Label: "stored-1", LastActive: 100, PaneCount: 1},
			{SessionID: makePickerID(0xCC), Label: "stored-2", LastActive: 50, PaneCount: 4, Pinned: true, HasThumbnail: true},
		},
	}
	encoded, err := EncodeListSessionsResponse(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeListSessionsResponse(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch:\n in=%#v\nout=%#v", in, out)
	}
}

func TestListSessionsResponse_EmptyBoth(t *testing.T) {
	in := ListSessionsResponse{}
	encoded, err := EncodeListSessionsResponse(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeListSessionsResponse(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Live) != 0 || len(out.Stored) != 0 {
		t.Fatalf("expected empty slices, got Live=%d Stored=%d", len(out.Live), len(out.Stored))
	}
}

func TestRecoverSessionRequest_RoundTrip(t *testing.T) {
	in := RecoverSessionRequest{
		SessionID: makePickerID(0xFF),
		NewLabel:  "renamed-on-recover",
	}
	encoded, err := EncodeRecoverSessionRequest(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeRecoverSessionRequest(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch: in=%#v out=%#v", in, out)
	}
}

func TestRenameSessionRequest_RoundTrip(t *testing.T) {
	in := RenameSessionRequest{SessionID: makePickerID(0x11), NewLabel: "edit"}
	encoded, err := EncodeRenameSessionRequest(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeRenameSessionRequest(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch: in=%#v out=%#v", in, out)
	}
}

func TestSessionOpResponse_RoundTrip(t *testing.T) {
	cases := []SessionOpResponse{
		{OpType: OpRename, OK: true},
		{OpType: OpRename, OK: false, Error: "session not found"},
		{OpType: OpDelete, OK: true},
		{OpType: OpDelete, OK: false, Error: "session is live"},
	}
	for i, in := range cases {
		encoded, err := EncodeSessionOpResponse(in)
		if err != nil {
			t.Fatalf("[%d] encode: %v", i, err)
		}
		out, err := DecodeSessionOpResponse(encoded)
		if err != nil {
			t.Fatalf("[%d] decode: %v", i, err)
		}
		if !reflect.DeepEqual(in, out) {
			t.Fatalf("[%d] round-trip mismatch: in=%#v out=%#v", i, in, out)
		}
	}
}

func TestDeleteSessionRequest_RoundTrip(t *testing.T) {
	in := DeleteSessionRequest{SessionID: makePickerID(0x22)}
	encoded, err := EncodeDeleteSessionRequest(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeDeleteSessionRequest(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip mismatch: in=%#v out=%#v", in, out)
	}
}

func TestFetchThumbnailRequest_RoundTrip(t *testing.T) {
	in := FetchThumbnailRequest{SessionID: makePickerID(0x33)}
	encoded, err := EncodeFetchThumbnailRequest(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeFetchThumbnailRequest(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip mismatch: in=%#v out=%#v", in, out)
	}
}

func TestFetchThumbnailResponse_RoundTrip(t *testing.T) {
	cases := []FetchThumbnailResponse{
		{OK: true, PNG: []byte{0x89, 0x50, 0x4E, 0x47}},
		{OK: false, Error: "missing"},
		{OK: true, PNG: nil},
	}
	for i, in := range cases {
		encoded, err := EncodeFetchThumbnailResponse(in)
		if err != nil {
			t.Fatalf("[%d] encode: %v", i, err)
		}
		out, err := DecodeFetchThumbnailResponse(encoded)
		if err != nil {
			t.Fatalf("[%d] decode: %v", i, err)
		}
		if !pickerBytesEqual(in.PNG, out.PNG) || in.OK != out.OK || in.Error != out.Error {
			t.Fatalf("[%d] round-trip mismatch: in=%#v out=%#v", i, in, out)
		}
	}
}

func pickerBytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

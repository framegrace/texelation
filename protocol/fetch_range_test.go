package protocol

import (
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
)

func TestFetchRange_RoundTrip(t *testing.T) {
	in := FetchRange{
		RequestID:    7,
		PaneID:       [16]byte{0xAA},
		LoIdx:        1_000,
		HiIdx:        1_050,
		AsOfRevision: 42,
	}
	raw, err := EncodeFetchRange(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeFetchRange(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out != in {
		t.Fatalf("mismatch: %#v vs %#v", out, in)
	}
}

func TestFetchRange_Validation(t *testing.T) {
	// Encode enforces Validate() symmetrically with Decode so malformed
	// FetchRange requests never make it onto the wire in the first place.
	// Decode must still reject them in case a peer violates the contract.

	t.Run("lo > hi rejected on encode", func(t *testing.T) {
		_, err := EncodeFetchRange(FetchRange{LoIdx: 30, HiIdx: 20})
		if !errors.Is(err, ErrFetchRangeInverted) {
			t.Fatalf("expected ErrFetchRangeInverted on encode, got %v", err)
		}
	})

	t.Run("lo > hi rejected on decode", func(t *testing.T) {
		// Encode a valid payload then hand-edit LoIdx in place so the wire
		// bytes carry an invariant violation the decoder must catch.
		raw, err := EncodeFetchRange(FetchRange{LoIdx: 10, HiIdx: 20})
		if err != nil {
			t.Fatalf("encode good: %v", err)
		}
		var lo int64 = 30
		binary.LittleEndian.PutUint64(raw[20:28], uint64(lo))
		_, err = DecodeFetchRange(raw)
		if !errors.Is(err, ErrFetchRangeInverted) {
			t.Fatalf("expected ErrFetchRangeInverted on decode, got %v", err)
		}
	})

	t.Run("lo == hi accepted (empty range)", func(t *testing.T) {
		raw, err := EncodeFetchRange(FetchRange{LoIdx: 15, HiIdx: 15})
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		out, err := DecodeFetchRange(raw)
		if err != nil {
			t.Fatalf("lo==hi should be accepted, got error: %v", err)
		}
		if out.LoIdx != out.HiIdx {
			t.Fatalf("round-trip of empty range mismatch")
		}
	})

	t.Run("negative lo rejected on encode", func(t *testing.T) {
		_, err := EncodeFetchRange(FetchRange{LoIdx: -1, HiIdx: 10})
		if !errors.Is(err, ErrFetchRangeNegative) {
			t.Fatalf("expected ErrFetchRangeNegative on encode, got %v", err)
		}
	})

	t.Run("negative lo rejected on decode", func(t *testing.T) {
		raw, err := EncodeFetchRange(FetchRange{LoIdx: 5, HiIdx: 10})
		if err != nil {
			t.Fatalf("encode good: %v", err)
		}
		var neg int64 = -1
		binary.LittleEndian.PutUint64(raw[20:28], uint64(neg))
		_, err = DecodeFetchRange(raw)
		if !errors.Is(err, ErrFetchRangeNegative) {
			t.Fatalf("expected ErrFetchRangeNegative on decode, got %v", err)
		}
	})
}

func TestFetchRangeResponse_RoundTrip(t *testing.T) {
	in := FetchRangeResponse{
		RequestID: 7,
		PaneID:    [16]byte{0xAA},
		Revision:  99,
		Flags:     FetchRangeNone,
		Rows: []LogicalRow{
			{GlobalIdx: 1_000, Wrapped: false, NoWrap: false, Spans: []CellSpan{{StartCol: 0, Text: "hi", StyleIndex: 0}}},
			{GlobalIdx: 1_001, Wrapped: true, Spans: []CellSpan{{StartCol: 0, Text: "continuation", StyleIndex: 0}}},
		},
		Styles: []StyleEntry{{AttrFlags: 0}},
	}
	raw, err := EncodeFetchRangeResponse(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeFetchRangeResponse(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("mismatch:\n got %#v\n want %#v", out, in)
	}
}

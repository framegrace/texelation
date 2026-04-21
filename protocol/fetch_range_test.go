package protocol

import (
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

func TestDecodeFetchRange_Validation(t *testing.T) {
	good := FetchRange{
		RequestID: 1,
		LoIdx:     10,
		HiIdx:     20,
	}

	t.Run("lo > hi rejected", func(t *testing.T) {
		bad := good
		bad.LoIdx = 30 // > HiIdx
		raw, err := EncodeFetchRange(bad)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		_, err = DecodeFetchRange(raw)
		if !errors.Is(err, ErrFetchRangeInverted) {
			t.Fatalf("expected ErrFetchRangeInverted, got %v", err)
		}
	})

	t.Run("lo == hi accepted (empty range)", func(t *testing.T) {
		eq := good
		eq.LoIdx = 15
		eq.HiIdx = 15
		raw, err := EncodeFetchRange(eq)
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

	t.Run("negative lo rejected", func(t *testing.T) {
		bad := good
		bad.LoIdx = -1
		bad.HiIdx = 10
		raw, err := EncodeFetchRange(bad)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		_, err = DecodeFetchRange(raw)
		if !errors.Is(err, ErrFetchRangeNegative) {
			t.Fatalf("expected ErrFetchRangeNegative, got %v", err)
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

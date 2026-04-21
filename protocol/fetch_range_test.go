package protocol

import (
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

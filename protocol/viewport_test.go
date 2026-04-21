package protocol

import "testing"

func TestViewportUpdate_RoundTrip(t *testing.T) {
	in := ViewportUpdate{
		PaneID:         [16]byte{0xDE, 0xAD, 0xBE, 0xEF},
		AltScreen:      false,
		ViewTopIdx:     1_000,
		ViewBottomIdx:  1_023,
		WrapSegmentIdx: 2,
		Rows:           24,
		Cols:           80,
		AutoFollow:     true,
	}
	raw, err := EncodeViewportUpdate(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeViewportUpdate(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip mismatch:\n got  %#v\n want %#v", out, in)
	}
}

package protocol

import "testing"

func TestBufferDeltaRoundTrip(t *testing.T) {
	var pane [16]byte
	copy(pane[:], []byte("pane-1234567890"))

	delta := BufferDelta{
		PaneID:   pane,
		Revision: 7,
		Flags:    BufferDeltaNone,
		Styles: []StyleEntry{
			{AttrFlags: 1, FgModel: ColorModelRGB, FgValue: 0x112233, BgModel: ColorModelDefault, BgValue: 0},
			{AttrFlags: 2, FgModel: ColorModelANSI16, FgValue: 3, BgModel: ColorModelRGB, BgValue: 0x445566},
		},
		Rows: []RowDelta{
			{Row: 0, Spans: []CellSpan{{StartCol: 0, Text: "hello", StyleIndex: 0}}},
			{Row: 1, Spans: []CellSpan{{StartCol: 5, Text: "world", StyleIndex: 1}}},
		},
	}

	payload, err := EncodeBufferDelta(delta)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := DecodeBufferDelta(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.Revision != delta.Revision || len(decoded.Rows) != len(delta.Rows) || len(decoded.Styles) != len(delta.Styles) {
		t.Fatalf("metadata mismatch")
	}
	for i := range delta.Rows {
		if decoded.Rows[i].Row != delta.Rows[i].Row || len(decoded.Rows[i].Spans) != len(delta.Rows[i].Spans) {
			t.Fatalf("row mismatch")
		}
		for j := range delta.Rows[i].Spans {
			got := decoded.Rows[i].Spans[j]
			want := delta.Rows[i].Spans[j]
			if got.StartCol != want.StartCol || got.StyleIndex != want.StyleIndex || got.Text != want.Text {
				t.Fatalf("span mismatch: %#v vs %#v", got, want)
			}
		}
	}
}

func TestBufferDeltaInvalid(t *testing.T) {
	if _, err := DecodeBufferDelta([]byte("short")); err == nil {
		t.Fatalf("expected error for short payload")
	}
}

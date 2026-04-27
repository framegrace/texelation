// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: protocol/buffer_delta_test.go
// Summary: Exercises buffer delta behaviour to ensure the protocol definitions remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: Keep changes backward-compatible; any additions require coordinated version bumps.

package protocol

import (
	"errors"
	"reflect"
	"testing"
)

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

func TestBufferDeltaDynamicColorRoundTrip(t *testing.T) {
	delta := BufferDelta{
		PaneID:   [16]byte{1},
		Revision: 1,
		Styles: []StyleEntry{
			{
				AttrFlags: AttrHasDynamic,
				FgModel:   ColorModelRGB,
				FgValue:   0xFF0000,
				BgModel:   ColorModelRGB,
				BgValue:   0x0000FF,
				DynBG: DynColorDesc{
					Type:  2, // pulse
					Base:  0x89B4FA,
					Speed: 6,
					Min:   0.7,
					Max:   1.0,
				},
			},
		},
		Rows: []RowDelta{{Row: 0, Spans: []CellSpan{{StartCol: 0, Text: "x", StyleIndex: 0}}}},
	}
	encoded, err := EncodeBufferDelta(delta)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeBufferDelta(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Styles[0].DynBG.Type != 2 {
		t.Errorf("expected pulse type 2, got %d", decoded.Styles[0].DynBG.Type)
	}
	if decoded.Styles[0].DynBG.Base != 0x89B4FA {
		t.Errorf("base color mismatch: %x", decoded.Styles[0].DynBG.Base)
	}
	if decoded.Styles[0].DynBG.Speed != 6 {
		t.Errorf("speed mismatch: %f", decoded.Styles[0].DynBG.Speed)
	}
	if decoded.Styles[0].DynBG.Min != 0.7 {
		t.Errorf("min mismatch: %f", decoded.Styles[0].DynBG.Min)
	}
	if decoded.Styles[0].DynBG.Max != 1.0 {
		t.Errorf("max mismatch: %f", decoded.Styles[0].DynBG.Max)
	}
	// DynFG should be zero (not set)
	if decoded.Styles[0].DynFG.Type != 0 {
		t.Errorf("expected DynFG type 0, got %d", decoded.Styles[0].DynFG.Type)
	}
}

func TestBufferDeltaStaticBackwardCompat(t *testing.T) {
	delta := BufferDelta{
		PaneID:   [16]byte{1},
		Revision: 1,
		Styles:   []StyleEntry{{FgModel: ColorModelRGB, FgValue: 0xFF0000}},
		Rows:     []RowDelta{{Row: 0, Spans: []CellSpan{{StartCol: 0, Text: "x", StyleIndex: 0}}}},
	}
	encoded, err := EncodeBufferDelta(delta)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeBufferDelta(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Styles[0].AttrFlags&AttrHasDynamic != 0 {
		t.Error("static style should not have dynamic flag")
	}
	if decoded.Styles[0].DynBG.Type != 0 {
		t.Error("static style should have zero DynBG")
	}
}

func TestBufferDeltaGradientRoundTrip(t *testing.T) {
	delta := BufferDelta{
		PaneID:   [16]byte{1},
		Revision: 1,
		Styles: []StyleEntry{
			{
				AttrFlags: AttrHasDynamic,
				FgModel:   ColorModelRGB,
				FgValue:   0xFFFFFF,
				BgModel:   ColorModelRGB,
				BgValue:   0x000000,
				DynBG: DynColorDesc{
					Type:   4,
					Base:   0,
					Easing: 2,
					Stops: []DynColorStopDesc{
						{Position: 0, Color: DynColorDesc{Type: 2, Base: 0x89B4FA, Speed: 6, Min: 0.7, Max: 1.0}},
						{Position: 0.3, Color: DynColorDesc{Type: 2, Base: 0x89B4FA, Speed: 6, Min: 0.7, Max: 1.0}},
						{Position: 1.0, Color: DynColorDesc{Type: 1, Base: 0x1E1E2E}},
					},
				},
			},
		},
		Rows: []RowDelta{{Row: 0, Spans: []CellSpan{{StartCol: 0, Text: "x", StyleIndex: 0}}}},
	}
	encoded, err := EncodeBufferDelta(delta)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeBufferDelta(encoded)
	if err != nil {
		t.Fatal(err)
	}
	bg := decoded.Styles[0].DynBG
	if bg.Type != 4 {
		t.Fatalf("expected linear gradient type 4, got %d", bg.Type)
	}
	if len(bg.Stops) != 3 {
		t.Fatalf("expected 3 stops, got %d", len(bg.Stops))
	}
	if bg.Stops[0].Color.Type != 2 {
		t.Errorf("stop 0 should be pulse (2), got %d", bg.Stops[0].Color.Type)
	}
	if bg.Stops[0].Color.Speed != 6 {
		t.Errorf("stop 0 speed mismatch: %f", bg.Stops[0].Color.Speed)
	}
	if bg.Stops[2].Color.Type != 1 {
		t.Errorf("stop 2 should be solid (1), got %d", bg.Stops[2].Color.Type)
	}
	if bg.Stops[2].Color.Base != 0x1E1E2E {
		t.Errorf("stop 2 base mismatch: %x", bg.Stops[2].Color.Base)
	}
}

func TestBufferDeltaInvalid(t *testing.T) {
	if _, err := DecodeBufferDelta([]byte("short")); err == nil {
		t.Fatalf("expected error for short payload")
	}
}

func TestBufferDelta_AltScreenFlagRoundTrip(t *testing.T) {
	in := BufferDelta{
		PaneID: [16]byte{9},
		Flags:  BufferDeltaAltScreen,
		Rows:   []RowDelta{{Row: 3, Spans: []CellSpan{{StartCol: 0, Text: "x", StyleIndex: 0}}}},
		Styles: []StyleEntry{{AttrFlags: 0}},
	}
	raw, err := EncodeBufferDelta(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeBufferDelta(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Flags&BufferDeltaAltScreen == 0 {
		t.Fatalf("AltScreen flag lost")
	}
}

func TestBufferDelta_RowBaseRoundTrip(t *testing.T) {
	in := BufferDelta{
		PaneID:   [16]byte{1, 2, 3},
		Revision: 42,
		Flags:    BufferDeltaNone,
		RowBase:  1_234_567,
		Rows: []RowDelta{
			{Row: 0, Spans: []CellSpan{{StartCol: 0, Text: "hello", StyleIndex: 0}}},
		},
		Styles: []StyleEntry{{AttrFlags: 0}},
	}
	raw, err := EncodeBufferDelta(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeBufferDelta(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.RowBase != in.RowBase {
		t.Fatalf("RowBase: got %d want %d", out.RowBase, in.RowBase)
	}
}

// Regression: encoders must reject a StyleIndex that points past the Styles
// table. Symmetric with the decode-side check — catches producer bugs at the
// serialization boundary rather than shipping invalid bytes.
func TestEncodeBufferDeltaRejectsStyleIndexOutOfRange(t *testing.T) {
	cases := []struct {
		name  string
		delta BufferDelta
	}{
		{
			name: "no styles, any index",
			delta: BufferDelta{
				Rows: []RowDelta{{Row: 0, Spans: []CellSpan{{Text: "x", StyleIndex: 0}}}},
			},
		},
		{
			name: "index equals length",
			delta: BufferDelta{
				Styles: []StyleEntry{{}},
				Rows:   []RowDelta{{Row: 0, Spans: []CellSpan{{Text: "x", StyleIndex: 1}}}},
			},
		},
		{
			name: "index past end",
			delta: BufferDelta{
				Styles: []StyleEntry{{}, {}},
				Rows:   []RowDelta{{Row: 0, Spans: []CellSpan{{Text: "x", StyleIndex: 99}}}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := EncodeBufferDelta(tc.delta); err != ErrStyleIndexOutOfRange {
				t.Errorf("EncodeBufferDelta err = %v, want ErrStyleIndexOutOfRange", err)
			}
		})
	}
}

func TestEncodeDecodeBufferDelta_DecorRoundTrip(t *testing.T) {
	original := BufferDelta{
		PaneID:   [16]byte{0xab, 0xcd},
		Revision: 7,
		Flags:    BufferDeltaNone,
		RowBase:  100,
		Styles: []StyleEntry{
			{AttrFlags: 0, FgModel: ColorModelDefault, BgModel: ColorModelDefault},
		},
		Rows: []RowDelta{
			{Row: 0, Spans: []CellSpan{{StartCol: 0, Text: "hi", StyleIndex: 0}}},
		},
		DecorRows: []DecorRowDelta{
			{RowIdx: 0, Spans: []CellSpan{{StartCol: 0, Text: "+", StyleIndex: 0}}},
			{RowIdx: 22, Spans: []CellSpan{{StartCol: 0, Text: "-", StyleIndex: 0}}},
		},
	}
	encoded, err := EncodeBufferDelta(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeBufferDelta(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(original, decoded) {
		t.Fatalf("round-trip mismatch:\n  want %+v\n  got  %+v", original, decoded)
	}
}

func TestEncodeDecodeBufferDelta_EmptyDecorRoundTrip(t *testing.T) {
	original := BufferDelta{
		PaneID:   [16]byte{0xff},
		Revision: 1,
		Flags:    BufferDeltaAltScreen,
		RowBase:  0,
		Styles:   []StyleEntry{{AttrFlags: 0, FgModel: ColorModelDefault, BgModel: ColorModelDefault}},
		Rows:     []RowDelta{{Row: 0, Spans: []CellSpan{{StartCol: 0, Text: "x", StyleIndex: 0}}}},
		// DecorRows intentionally nil
	}
	encoded, err := EncodeBufferDelta(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeBufferDelta(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.DecorRows) != 0 {
		t.Fatalf("expected empty DecorRows, got %+v", decoded.DecorRows)
	}
}

func TestDecodeBufferDelta_TruncatedDecorTailErrPayloadShort(t *testing.T) {
	// Build a valid v3 payload then chop the trailing 2-byte decor count.
	original := BufferDelta{
		PaneID:   [16]byte{0x01},
		Revision: 1,
		Styles:   []StyleEntry{{AttrFlags: 0, FgModel: ColorModelDefault, BgModel: ColorModelDefault}},
		Rows:     []RowDelta{{Row: 0, Spans: []CellSpan{{StartCol: 0, Text: "x", StyleIndex: 0}}}},
	}
	encoded, err := EncodeBufferDelta(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	truncated := encoded[:len(encoded)-2]
	if _, err := DecodeBufferDelta(truncated); !errors.Is(err, ErrPayloadShort) {
		t.Fatalf("expected ErrPayloadShort on truncated v3, got %v", err)
	}
}

func TestDecodeBufferDelta_TruncatedMidDecorRow(t *testing.T) {
	// Build a payload with one DecorRow that has one span, then truncate
	// inside the per-row body (after the row+spanCount header but before
	// the span itself).
	original := BufferDelta{
		PaneID:   [16]byte{0x02},
		Revision: 1,
		Styles:   []StyleEntry{{AttrFlags: 0, FgModel: ColorModelDefault, BgModel: ColorModelDefault}},
		DecorRows: []DecorRowDelta{
			{RowIdx: 0, Spans: []CellSpan{{StartCol: 0, Text: "border", StyleIndex: 0}}},
		},
	}
	encoded, err := EncodeBufferDelta(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Chop off the last 6 bytes (the per-span StartCol+TextLen+StyleIdx
	// header), leaving the row+spanCount header dangling without span data.
	truncated := encoded[:len(encoded)-len("border")-6]
	if _, err := DecodeBufferDelta(truncated); !errors.Is(err, ErrPayloadShort) {
		t.Fatalf("expected ErrPayloadShort on mid-row truncation, got %v", err)
	}
}

func TestDecodeBufferDelta_RejectsExcessiveRowIdx(t *testing.T) {
	// Hand-craft a payload with RowIdx > MaxDecorRowIdx (out of sane pane height).
	original := BufferDelta{
		PaneID:   [16]byte{0x03},
		Revision: 1,
		Styles:   []StyleEntry{{AttrFlags: 0, FgModel: ColorModelDefault, BgModel: ColorModelDefault}},
		DecorRows: []DecorRowDelta{
			{RowIdx: MaxDecorRowIdx + 1, Spans: []CellSpan{{StartCol: 0, Text: "x", StyleIndex: 0}}},
		},
	}
	encoded, err := EncodeBufferDelta(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := DecodeBufferDelta(encoded); !errors.Is(err, ErrInvalidSpan) {
		t.Fatalf("expected ErrInvalidSpan for excessive RowIdx, got %v", err)
	}
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: protocol/buffer_delta_test.go
// Summary: Exercises buffer delta behaviour to ensure the protocol definitions remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: Keep changes backward-compatible; any additions require coordinated version bumps.

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

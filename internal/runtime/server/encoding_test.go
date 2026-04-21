// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/protocol"
)

// TestStyleTable_Dedup verifies that inserting the same cell style twice returns
// the same index, and a different style produces a new index.
func TestStyleTable_Dedup(t *testing.T) {
	table := newStyleTable()

	// cellA and cellADup differ only in Rune; style (Attr, colors) is identical.
	cellA := parser.Cell{Rune: 'a', Attr: parser.AttrBold, FG: parser.Color{Mode: parser.ColorModeDefault}}
	cellADup := parser.Cell{Rune: 'z', Attr: parser.AttrBold, FG: parser.Color{Mode: parser.ColorModeDefault}}
	cellB := parser.Cell{Rune: 'b', Attr: parser.AttrUnderline, FG: parser.Color{Mode: parser.ColorModeDefault}}

	idx0 := table.indexOf(cellA)
	idx0Dup := table.indexOf(cellADup)
	if idx0 != idx0Dup {
		t.Fatalf("same style should yield same index: got %d and %d", idx0, idx0Dup)
	}

	idx1 := table.indexOf(cellB)
	if idx1 == idx0 {
		t.Fatalf("different style should yield different index: both got %d", idx0)
	}
	if len(table.entries()) != 2 {
		t.Fatalf("expected 2 entries in style table, got %d", len(table.entries()))
	}
}

// TestEncodeParserCellsToSpans_SpanBreaking verifies three cells A, A, B (differing
// style on the third) produce exactly two spans with correct StartCol values.
func TestEncodeParserCellsToSpans_SpanBreaking(t *testing.T) {
	table := newStyleTable()

	cells := []parser.Cell{
		{Rune: 'H', Attr: parser.AttrBold},
		{Rune: 'i', Attr: parser.AttrBold},
		{Rune: '!', Attr: parser.AttrUnderline},
	}

	spans := encodeParserCellsToSpans(cells, table)
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}
	if spans[0].StartCol != 0 {
		t.Fatalf("first span StartCol: got %d want 0", spans[0].StartCol)
	}
	if spans[0].Text != "Hi" {
		t.Fatalf("first span Text: got %q want %q", spans[0].Text, "Hi")
	}
	if spans[1].StartCol != 2 {
		t.Fatalf("second span StartCol: got %d want 2", spans[1].StartCol)
	}
	if spans[1].Text != "!" {
		t.Fatalf("second span Text: got %q want %q", spans[1].Text, "!")
	}
}

// TestParserColorToProto covers all four ColorMode → ColorModel mappings.
func TestParserColorToProto(t *testing.T) {
	tests := []struct {
		name      string
		color     parser.Color
		wantModel protocol.ColorModel
		wantValue uint32
	}{
		{
			name:      "Default",
			color:     parser.Color{Mode: parser.ColorModeDefault},
			wantModel: protocol.ColorModelDefault,
			wantValue: 0,
		},
		{
			name:      "Standard ANSI16",
			color:     parser.Color{Mode: parser.ColorModeStandard, Value: 3},
			wantModel: protocol.ColorModelANSI16,
			wantValue: 3,
		},
		{
			name:      "256-color",
			color:     parser.Color{Mode: parser.ColorMode256, Value: 200},
			wantModel: protocol.ColorModelANSI256,
			wantValue: 200,
		},
		{
			name:      "RGB packed",
			color:     parser.Color{Mode: parser.ColorModeRGB, R: 0x12, G: 0x34, B: 0x56},
			wantModel: protocol.ColorModelRGB,
			wantValue: (0x12 << 16) | (0x34 << 8) | 0x56,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotModel, gotValue := parserColorToProto(tc.color)
			if gotModel != tc.wantModel {
				t.Errorf("model: got %v want %v", gotModel, tc.wantModel)
			}
			if gotValue != tc.wantValue {
				t.Errorf("value: got 0x%X want 0x%X", gotValue, tc.wantValue)
			}
		})
	}
}

// TestStyleTable_AttrMapping verifies all five parser attrs map to the correct
// protocol bits, Blink is not emitted, and AttrHasDynamic is never set.
func TestStyleTable_AttrMapping(t *testing.T) {
	attrCases := []struct {
		name       string
		parserAttr parser.Attribute
		wantBit    uint16
	}{
		{"Bold", parser.AttrBold, protocol.AttrBold},
		{"Underline", parser.AttrUnderline, protocol.AttrUnderline},
		{"Reverse", parser.AttrReverse, protocol.AttrReverse},
		{"Dim", parser.AttrDim, protocol.AttrDim},
		{"Italic", parser.AttrItalic, protocol.AttrItalic},
	}
	for _, tc := range attrCases {
		t.Run(tc.name, func(t *testing.T) {
			table := newStyleTable()
			cell := parser.Cell{Rune: 'x', Attr: tc.parserAttr}
			idx := table.indexOf(cell)
			entries := table.entries()
			if int(idx) >= len(entries) {
				t.Fatalf("index %d out of range for entries len %d", idx, len(entries))
			}
			entry := entries[idx]
			if entry.AttrFlags&tc.wantBit == 0 {
				t.Errorf("expected bit 0x%X set in AttrFlags 0x%X", tc.wantBit, entry.AttrFlags)
			}
			// Blink must not be set (parser has no Blink attr)
			if entry.AttrFlags&protocol.AttrBlink != 0 {
				t.Errorf("unexpected Blink bit in AttrFlags 0x%X", entry.AttrFlags)
			}
			// AttrHasDynamic must not be set
			if entry.AttrFlags&protocol.AttrHasDynamic != 0 {
				t.Errorf("unexpected AttrHasDynamic bit in AttrFlags 0x%X", entry.AttrFlags)
			}
		})
	}
}

// TestEncodeParserCellsToSpans_Empty verifies empty input produces nil/empty spans
// and leaves the style table untouched.
func TestEncodeParserCellsToSpans_Empty(t *testing.T) {
	table := newStyleTable()
	spans := encodeParserCellsToSpans(nil, table)
	if len(spans) != 0 {
		t.Fatalf("expected no spans for empty input, got %d", len(spans))
	}
	if len(table.entries()) != 0 {
		t.Fatalf("expected no style entries for empty input, got %d", len(table.entries()))
	}
}

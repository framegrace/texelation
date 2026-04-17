// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import "testing"

func TestPage_LineData_NoWrapRoundTrip(t *testing.T) {
	orig := &LogicalLine{Cells: []Cell{{Rune: 'x'}}, NoWrap: true}
	data := encodeLineData(orig)
	got, err := decodeLineData(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.NoWrap {
		t.Errorf("NoWrap did not round-trip")
	}
}

func TestPage_LineData_BackwardCompat_NoWrapAbsent(t *testing.T) {
	// Old records have bit 0x08 clear; decode should yield NoWrap=false.
	orig := &LogicalLine{Cells: []Cell{{Rune: 'y'}}}
	data := encodeLineData(orig)
	got, err := decodeLineData(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.NoWrap {
		t.Errorf("default NoWrap should be false")
	}
}

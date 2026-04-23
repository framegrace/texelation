// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package protocol

import (
	"errors"
	"testing"
)

func TestPaneViewportState_RoundTrip(t *testing.T) {
	in := PaneViewportState{
		PaneID:         [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		AltScreen:      false,
		ViewBottomIdx:  123456,
		WrapSegmentIdx: 3,
		AutoFollow:     true,
		ViewportRows:   24,
		ViewportCols:   80,
	}
	raw, err := EncodePaneViewportState(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, consumed, err := DecodePaneViewportState(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if consumed != len(raw) {
		t.Fatalf("consumed=%d len=%d", consumed, len(raw))
	}
	if out != in {
		t.Fatalf("round-trip mismatch: got %+v want %+v", out, in)
	}
}

func TestPaneViewportState_AltScreen(t *testing.T) {
	in := PaneViewportState{
		PaneID:         [16]byte{0xaa},
		AltScreen:      true,
		ViewBottomIdx:  0,
		WrapSegmentIdx: 0,
		AutoFollow:     false,
		ViewportRows:   10,
		ViewportCols:   40,
	}
	raw, err := EncodePaneViewportState(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, _, err := DecodePaneViewportState(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip mismatch: got %+v want %+v", out, in)
	}
}

func TestPaneViewportState_ZeroDim(t *testing.T) {
	bad := PaneViewportState{
		PaneID:       [16]byte{1},
		ViewportRows: 0,
		ViewportCols: 80,
	}
	if _, err := EncodePaneViewportState(bad); !errors.Is(err, ErrPaneViewportZeroDim) {
		t.Fatalf("want ErrPaneViewportZeroDim, got %v", err)
	}
}

func TestPaneViewportState_ShortPayload(t *testing.T) {
	short := make([]byte, 10)
	if _, _, err := DecodePaneViewportState(short); !errors.Is(err, ErrPayloadShort) {
		t.Fatalf("want ErrPayloadShort, got %v", err)
	}
}

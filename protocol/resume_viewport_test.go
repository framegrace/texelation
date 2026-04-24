// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package protocol

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestResumeRequest_RoundTripEmptyViewports(t *testing.T) {
	in := ResumeRequest{
		SessionID:    [16]byte{1, 2, 3},
		LastSequence: 42,
	}
	raw, err := EncodeResumeRequest(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeResumeRequest(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.SessionID != in.SessionID || out.LastSequence != in.LastSequence {
		t.Fatalf("core fields mismatch: got %+v want %+v", out, in)
	}
	if len(out.PaneViewports) != 0 {
		t.Fatalf("PaneViewports: got %d want 0", len(out.PaneViewports))
	}
}

func TestResumeRequest_RoundTripWithViewports(t *testing.T) {
	in := ResumeRequest{
		SessionID:    [16]byte{9},
		LastSequence: 100,
		PaneViewports: []PaneViewportState{
			{PaneID: [16]byte{1}, ViewBottomIdx: 500, WrapSegmentIdx: 2, AutoFollow: false, ViewportRows: 24, ViewportCols: 80},
			{PaneID: [16]byte{2}, AltScreen: true, ViewportRows: 24, ViewportCols: 80},
		},
	}
	raw, err := EncodeResumeRequest(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeResumeRequest(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.PaneViewports) != len(in.PaneViewports) {
		t.Fatalf("PaneViewports len: got %d want %d", len(out.PaneViewports), len(in.PaneViewports))
	}
	for i := range in.PaneViewports {
		if out.PaneViewports[i] != in.PaneViewports[i] {
			t.Fatalf("PaneViewports[%d]: got %+v want %+v", i, out.PaneViewports[i], in.PaneViewports[i])
		}
	}
}

func TestDecodeResumeRequest_RejectsTrailingBytes(t *testing.T) {
	in := ResumeRequest{
		SessionID:    [16]byte{1},
		LastSequence: 1,
		PaneViewports: []PaneViewportState{
			{PaneID: [16]byte{9}, ViewBottomIdx: 1, ViewportRows: 24, ViewportCols: 80},
		},
	}
	raw, err := EncodeResumeRequest(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	raw = append(raw, 0xff) // stray trailing byte
	if _, err := DecodeResumeRequest(raw); !errors.Is(err, ErrExtraBytes) {
		t.Fatalf("want ErrExtraBytes, got %v", err)
	}
}

func TestDecodeResumeRequest_RejectsShortTail(t *testing.T) {
	// Valid encoding of ONE PaneViewportState entry...
	in := ResumeRequest{
		SessionID:    [16]byte{1},
		LastSequence: 1,
		PaneViewports: []PaneViewportState{
			{PaneID: [16]byte{9}, ViewBottomIdx: 1, ViewportRows: 24, ViewportCols: 80},
		},
	}
	raw, err := EncodeResumeRequest(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// ...but rewrite the count header to claim TWO entries. The second
	// entry's bytes are missing, so the decoder must fail with
	// ErrPayloadShort (propagated from DecodePaneViewportState).
	binary.LittleEndian.PutUint16(raw[24:26], 2)
	if _, err := DecodeResumeRequest(raw); !errors.Is(err, ErrPayloadShort) {
		t.Fatalf("want ErrPayloadShort, got %v", err)
	}
}

func TestEncodeResumeRequest_RejectsTooManyViewports(t *testing.T) {
	too := ResumeRequest{
		SessionID:     [16]byte{1},
		PaneViewports: make([]PaneViewportState, 0x10000), // 65536 > 0xFFFF
	}
	if _, err := EncodeResumeRequest(too); !errors.Is(err, ErrTooManyPaneViewports) {
		t.Fatalf("want ErrTooManyPaneViewports, got %v", err)
	}
}

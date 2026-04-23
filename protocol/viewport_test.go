package protocol

import (
	"encoding/binary"
	"errors"
	"testing"
)

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

// TestViewportUpdate_BoolCombos exercises all four (AltScreen, AutoFollow)
// combinations and verifies they all round-trip correctly.
func TestViewportUpdate_BoolCombos(t *testing.T) {
	base := ViewportUpdate{
		PaneID:         [16]byte{0x01},
		ViewTopIdx:     50,
		ViewBottomIdx:  74,
		WrapSegmentIdx: 0,
		Rows:           25,
		Cols:           80,
	}
	cases := []struct {
		altScreen  bool
		autoFollow bool
	}{
		{false, false},
		{false, true},
		{true, false},
		{true, true},
	}
	for _, tc := range cases {
		v := base
		v.AltScreen = tc.altScreen
		v.AutoFollow = tc.autoFollow
		raw, err := EncodeViewportUpdate(v)
		if err != nil {
			t.Fatalf("encode altScreen=%v autoFollow=%v: %v", tc.altScreen, tc.autoFollow, err)
		}
		out, err := DecodeViewportUpdate(raw)
		if err != nil {
			t.Fatalf("decode altScreen=%v autoFollow=%v: %v", tc.altScreen, tc.autoFollow, err)
		}
		if out != v {
			t.Fatalf("altScreen=%v autoFollow=%v: round-trip mismatch\n got  %#v\n want %#v",
				tc.altScreen, tc.autoFollow, out, v)
		}
	}
}

// TestViewportUpdate_WrapSegmentIdx verifies zero and nonzero WrapSegmentIdx round-trip.
func TestViewportUpdate_WrapSegmentIdx(t *testing.T) {
	base := ViewportUpdate{
		ViewTopIdx:    0,
		ViewBottomIdx: 23,
		Rows:          24,
		Cols:          80,
	}

	for _, wsi := range []uint16{0, 1, 0xFFFF} {
		base.WrapSegmentIdx = wsi
		raw, err := EncodeViewportUpdate(base)
		if err != nil {
			t.Fatalf("encode WrapSegmentIdx=%d: %v", wsi, err)
		}
		out, err := DecodeViewportUpdate(raw)
		if err != nil {
			t.Fatalf("decode WrapSegmentIdx=%d: %v", wsi, err)
		}
		if out.WrapSegmentIdx != wsi {
			t.Fatalf("WrapSegmentIdx=%d: got %d after round-trip", wsi, out.WrapSegmentIdx)
		}
	}
}

func TestViewportUpdate_Validation(t *testing.T) {
	// Validation is symmetric: Encode refuses malformed messages so peers
	// cannot inject them, and Decode refuses bad wire bytes from a buggy
	// peer.  Exercise both halves for each invariant.
	good := ViewportUpdate{
		ViewTopIdx:    100,
		ViewBottomIdx: 200,
		Rows:          24,
		Cols:          80,
	}

	t.Run("inverted top>bottom rejected on encode", func(t *testing.T) {
		bad := good
		bad.ViewTopIdx = 300
		if _, err := EncodeViewportUpdate(bad); !errors.Is(err, ErrViewportInverted) {
			t.Fatalf("expected ErrViewportInverted on encode, got %v", err)
		}
	})

	t.Run("inverted top>bottom rejected on decode", func(t *testing.T) {
		raw, err := EncodeViewportUpdate(good)
		if err != nil {
			t.Fatalf("encode good: %v", err)
		}
		var top int64 = 300
		binary.LittleEndian.PutUint64(raw[17:25], uint64(top))
		if _, err := DecodeViewportUpdate(raw); !errors.Is(err, ErrViewportInverted) {
			t.Fatalf("expected ErrViewportInverted on decode, got %v", err)
		}
	})

	t.Run("inverted allowed when altscreen", func(t *testing.T) {
		v := good
		v.AltScreen = true
		v.ViewTopIdx = 300 // > ViewBottomIdx — OK in alt-screen
		raw, err := EncodeViewportUpdate(v)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if _, err := DecodeViewportUpdate(raw); err != nil {
			t.Fatalf("alt-screen inverted should be accepted, got error: %v", err)
		}
	})

	t.Run("zero rows rejected on encode", func(t *testing.T) {
		bad := good
		bad.Rows = 0
		if _, err := EncodeViewportUpdate(bad); !errors.Is(err, ErrViewportZeroDim) {
			t.Fatalf("expected ErrViewportZeroDim on encode, got %v", err)
		}
	})

	t.Run("zero rows rejected on decode", func(t *testing.T) {
		raw, err := EncodeViewportUpdate(good)
		if err != nil {
			t.Fatalf("encode good: %v", err)
		}
		binary.LittleEndian.PutUint16(raw[35:37], 0)
		if _, err := DecodeViewportUpdate(raw); !errors.Is(err, ErrViewportZeroDim) {
			t.Fatalf("expected ErrViewportZeroDim on decode, got %v", err)
		}
	})

	t.Run("zero cols rejected on encode", func(t *testing.T) {
		bad := good
		bad.Cols = 0
		if _, err := EncodeViewportUpdate(bad); !errors.Is(err, ErrViewportZeroDim) {
			t.Fatalf("expected ErrViewportZeroDim on encode, got %v", err)
		}
	})

	t.Run("zero cols rejected on decode", func(t *testing.T) {
		raw, err := EncodeViewportUpdate(good)
		if err != nil {
			t.Fatalf("encode good: %v", err)
		}
		binary.LittleEndian.PutUint16(raw[37:39], 0)
		if _, err := DecodeViewportUpdate(raw); !errors.Is(err, ErrViewportZeroDim) {
			t.Fatalf("expected ErrViewportZeroDim on decode, got %v", err)
		}
	})
}

package protocol

import (
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

func TestDecodeViewportUpdate_Validation(t *testing.T) {
	// Helper: encode a valid ViewportUpdate then corrupt a field.
	good := ViewportUpdate{
		ViewTopIdx:    100,
		ViewBottomIdx: 200,
		Rows:          24,
		Cols:          80,
	}

	t.Run("inverted top>bottom rejected when not altscreen", func(t *testing.T) {
		bad := good
		bad.ViewTopIdx = 300 // > ViewBottomIdx
		raw, err := EncodeViewportUpdate(bad)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		_, err = DecodeViewportUpdate(raw)
		if !errors.Is(err, ErrViewportInverted) {
			t.Fatalf("expected ErrViewportInverted, got %v", err)
		}
	})

	t.Run("inverted allowed when altscreen", func(t *testing.T) {
		bad := good
		bad.AltScreen = true
		bad.ViewTopIdx = 300 // > ViewBottomIdx — OK in alt-screen
		raw, err := EncodeViewportUpdate(bad)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		_, err = DecodeViewportUpdate(raw)
		if err != nil {
			t.Fatalf("alt-screen inverted should be accepted, got error: %v", err)
		}
	})

	t.Run("zero rows rejected", func(t *testing.T) {
		bad := good
		bad.Rows = 0
		raw, err := EncodeViewportUpdate(bad)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		_, err = DecodeViewportUpdate(raw)
		if !errors.Is(err, ErrViewportZeroDim) {
			t.Fatalf("expected ErrViewportZeroDim, got %v", err)
		}
	})

	t.Run("zero cols rejected", func(t *testing.T) {
		bad := good
		bad.Cols = 0
		raw, err := EncodeViewportUpdate(bad)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		_, err = DecodeViewportUpdate(raw)
		if !errors.Is(err, ErrViewportZeroDim) {
			t.Fatalf("expected ErrViewportZeroDim, got %v", err)
		}
	})
}

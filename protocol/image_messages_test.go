// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package protocol

import (
	"testing"
)

func TestImageUploadRoundTrip(t *testing.T) {
	original := ImageUpload{
		PaneID:    [16]byte{1, 2, 3},
		SurfaceID: 42,
		Width:     320,
		Height:    240,
		Format:    0,
		Data:      []byte("fake-png-data-here"),
	}
	encoded, err := EncodeImageUpload(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeImageUpload(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.PaneID != original.PaneID {
		t.Errorf("PaneID mismatch")
	}
	if decoded.SurfaceID != original.SurfaceID {
		t.Errorf("SurfaceID: got %d, want %d", decoded.SurfaceID, original.SurfaceID)
	}
	if decoded.Width != original.Width || decoded.Height != original.Height {
		t.Errorf("dimensions: got %dx%d, want %dx%d", decoded.Width, decoded.Height, original.Width, original.Height)
	}
	if string(decoded.Data) != string(original.Data) {
		t.Errorf("data mismatch")
	}
}

func TestImagePlaceRoundTrip(t *testing.T) {
	original := ImagePlace{
		PaneID:    [16]byte{4, 5, 6},
		SurfaceID: 7,
		X:         10,
		Y:         20,
		W:         30,
		H:         15,
		ZIndex:    -1,
	}
	encoded, err := EncodeImagePlace(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeImagePlace(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded != original {
		t.Errorf("got %+v, want %+v", decoded, original)
	}
}

func TestImageDeleteRoundTrip(t *testing.T) {
	original := ImageDelete{
		PaneID:    [16]byte{7, 8, 9},
		SurfaceID: 99,
	}
	encoded, err := EncodeImageDelete(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeImageDelete(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded != original {
		t.Errorf("got %+v, want %+v", decoded, original)
	}
}

func TestImageResetRoundTrip(t *testing.T) {
	original := ImageReset{
		PaneID: [16]byte{10, 11, 12},
	}
	encoded, err := EncodeImageReset(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeImageReset(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded != original {
		t.Errorf("got %+v, want %+v", decoded, original)
	}
}

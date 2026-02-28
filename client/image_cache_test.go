// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: client/image_cache_test.go
// Summary: Exercises ImageCache behaviour to ensure graphics surfaces are tracked correctly.
// Usage: Executed during `go test` to guard against regressions.
// Notes: Uses a tiny 2x2 PNG for all upload tests to keep allocations minimal.

package client

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func makeTinyPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestImageCacheUpload(t *testing.T) {
	cache := NewImageCache()
	paneID := [16]byte{1}
	pngData := makeTinyPNG()
	cache.Upload(paneID, 42, 100, 50, pngData)

	img := cache.Get(42)
	if img == nil {
		t.Fatal("expected cached image")
	}
	if img.Width != 100 || img.Height != 50 {
		t.Errorf("dimensions: got %dx%d, want 100x50", img.Width, img.Height)
	}
	if img.Decoded == nil {
		t.Error("expected decoded image")
	}
}

func TestImageCacheGetMissing(t *testing.T) {
	cache := NewImageCache()
	if cache.Get(999) != nil {
		t.Error("expected nil for missing surface")
	}
}

func TestImageCachePlaceAndPlacements(t *testing.T) {
	cache := NewImageCache()
	paneID := [16]byte{1}
	cache.Upload(paneID, 1, 10, 10, makeTinyPNG())
	cache.Place(paneID, 1, 5, 10, 20, 8, -1)

	placements := cache.Placements(paneID)
	if len(placements) != 1 {
		t.Fatalf("expected 1 placement, got %d", len(placements))
	}
	if placements[0].SurfaceID != 1 {
		t.Errorf("surface ID: got %d, want 1", placements[0].SurfaceID)
	}
	if placements[0].X != 5 || placements[0].Y != 10 {
		t.Errorf("position: got (%d,%d), want (5,10)", placements[0].X, placements[0].Y)
	}
	if placements[0].ZIndex != -1 {
		t.Errorf("zIndex: got %d, want -1", placements[0].ZIndex)
	}
}

func TestImageCacheResetPlacements(t *testing.T) {
	cache := NewImageCache()
	paneID := [16]byte{1}
	cache.Upload(paneID, 1, 10, 10, makeTinyPNG())
	cache.Place(paneID, 1, 0, 0, 10, 10, 0)
	cache.ResetPlacements(paneID)

	placements := cache.Placements(paneID)
	if len(placements) != 0 {
		t.Errorf("expected 0 placements after reset, got %d", len(placements))
	}
	if cache.Get(1) == nil {
		t.Error("image data should be preserved after reset")
	}
}

func TestImageCacheDelete(t *testing.T) {
	cache := NewImageCache()
	paneID := [16]byte{1}
	cache.Upload(paneID, 1, 10, 10, makeTinyPNG())
	cache.Place(paneID, 1, 0, 0, 10, 10, 0)
	cache.Delete(paneID, 1)

	if cache.Get(1) != nil {
		t.Error("expected nil after delete")
	}
	placements := cache.Placements(paneID)
	if len(placements) != 0 {
		t.Error("expected placements cleared after delete")
	}
}

func TestImageCacheMultiplePlacements(t *testing.T) {
	cache := NewImageCache()
	paneID := [16]byte{1}
	cache.Place(paneID, 1, 0, 0, 10, 10, 0)
	cache.Place(paneID, 2, 20, 0, 10, 10, 0)

	placements := cache.Placements(paneID)
	if len(placements) != 2 {
		t.Fatalf("expected 2 placements, got %d", len(placements))
	}
}

func TestImageCachePlacementsReturnsCopy(t *testing.T) {
	cache := NewImageCache()
	paneID := [16]byte{1}
	cache.Place(paneID, 1, 0, 0, 10, 10, 0)

	p1 := cache.Placements(paneID)
	p1[0].X = 999 // modify the copy

	p2 := cache.Placements(paneID)
	if p2[0].X == 999 {
		t.Error("Placements should return a copy, not a reference")
	}
}

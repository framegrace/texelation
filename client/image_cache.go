// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: client/image_cache.go
// Summary: Stores uploaded images and active placements per pane for the client runtime.
// Usage: Imported by the remote renderer to manage graphics surfaces during live sessions.
// Notes: Thread-safe via sync.RWMutex; Placements returns a copy to avoid data races.

package client

import (
	"bytes"
	"image"
	_ "image/png"
	"sync"
)

// CachedImage holds uploaded image data and a decoded form for half-block fallback.
type CachedImage struct {
	PaneID  [16]byte
	Data    []byte      // PNG bytes from server
	Decoded image.Image // decoded once for half-block rendering
	Width   int
	Height  int
}

// ImagePlacement describes where to display a cached image.
type ImagePlacement struct {
	SurfaceID uint32
	X, Y      int
	W, H      int
	ZIndex    int
}

// ImageCache stores uploaded images and active placements per pane.
type ImageCache struct {
	mu         sync.RWMutex
	images     map[uint32]*CachedImage
	placements map[[16]byte][]ImagePlacement
}

// NewImageCache creates an empty image cache.
func NewImageCache() *ImageCache {
	return &ImageCache{
		images:     make(map[uint32]*CachedImage),
		placements: make(map[[16]byte][]ImagePlacement),
	}
}

// Upload stores image data and pre-decodes for half-block fallback.
func (c *ImageCache) Upload(paneID [16]byte, surfaceID uint32, width, height int, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	decoded, _, _ := image.Decode(bytes.NewReader(data))
	c.images[surfaceID] = &CachedImage{
		PaneID:  paneID,
		Data:    data,
		Decoded: decoded,
		Width:   width,
		Height:  height,
	}
}

// Get returns a cached image by surface ID, or nil if not found.
func (c *ImageCache) Get(surfaceID uint32) *CachedImage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.images[surfaceID]
}

// Place adds a placement for a pane.
func (c *ImageCache) Place(paneID [16]byte, surfaceID uint32, x, y, w, h, zIndex int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.placements[paneID] = append(c.placements[paneID], ImagePlacement{
		SurfaceID: surfaceID,
		X:         x,
		Y:         y,
		W:         w,
		H:         h,
		ZIndex:    zIndex,
	})
}

// Placements returns all active placements for a pane. The returned slice is a
// copy and safe to use without holding the cache lock.
func (c *ImageCache) Placements(paneID [16]byte) []ImagePlacement {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]ImagePlacement, len(c.placements[paneID]))
	copy(result, c.placements[paneID])
	return result
}

// ResetPlacements clears all placements for a pane. Image data is preserved so
// surfaces can be re-placed without re-uploading.
func (c *ImageCache) ResetPlacements(paneID [16]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.placements[paneID] = nil
}

// Delete removes image data for a surface and any placements referencing it.
func (c *ImageCache) Delete(paneID [16]byte, surfaceID uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.images, surfaceID)
	pls := c.placements[paneID]
	filtered := pls[:0]
	for _, p := range pls {
		if p.SurfaceID != surfaceID {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		delete(c.placements, paneID)
	} else {
		c.placements[paneID] = filtered
	}
}

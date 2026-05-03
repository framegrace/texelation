// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/thumbnail.go
// Summary: Lifecycle thumbnail capture orchestrator (Plan F.1).
// Usage: Called from Manager.ShutdownSessions and Manager.Close on
//   the last-disconnect transition. Renders via a ThumbnailRenderer
//   (production: *DesktopSink, see thumbnail_renderer.go), downscales
//   via the shared internal/thumbnail primitive, and atomically writes
//   to <basedir>/sessions/<id>.png.

package server

import (
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"path/filepath"

	"github.com/framegrace/texelation/internal/thumbnail"
)

// ThumbnailRenderer produces a PNG-suitable image for a given session.
// Implemented by *DesktopSink in production (see thumbnail_renderer.go);
// tests inject a stub.
//
// Returns ok=false when the session has nothing meaningful to capture
// (empty workspace, no live buffer) so the caller can skip the disk
// write rather than store an all-black PNG.
type ThumbnailRenderer interface {
	RenderSessionThumbnail(id [16]byte) (image.Image, bool)
}

// captureThumbnail renders id via the renderer (if non-nil), downscales
// to 480×270 via the shared primitive, and writes the result to
// <basedir>/sessions/<id>.png. A panicking renderer is recovered to an
// error so a buggy implementation cannot prevent shutdown. nil renderer
// or empty basedir is a silent no-op.
func captureThumbnail(basedir string, id [16]byte, r ThumbnailRenderer) (retErr error) {
	if r == nil || basedir == "" {
		return nil
	}
	defer func() {
		if rec := recover(); rec != nil {
			retErr = fmt.Errorf("thumbnail: renderer panic: %v", rec)
		}
	}()
	img, ok := r.RenderSessionThumbnail(id)
	if !ok {
		return nil
	}
	if img == nil {
		return errors.New("thumbnail: renderer returned ok=true with nil image")
	}
	scaled := thumbnail.DownscaleAspectFit(img, 480, 270)
	pngPath := filepath.Join(basedir, SessionsDirName, hex.EncodeToString(id[:])+".png")
	return thumbnail.WritePNGAtomic(pngPath, scaled)
}

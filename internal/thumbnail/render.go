// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/thumbnail/render.go
// Summary: Shared image-rendering primitive used by server lifecycle
// thumbnail capture (Plan F.1) and client user-initiated screenshots.
// Knows about cells; does not know about sessions, panes, or sockets.

package thumbnail

import (
	"errors"
	"fmt"
	"image"
	stddraw "image/draw"
	"image/png"
	"os"

	xdraw "golang.org/x/image/draw"

	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/graphics/textrender"
)

// ErrEmptyGrid is returned by RenderGrid for nil or zero-row inputs.
var ErrEmptyGrid = errors.New("thumbnail: empty cell grid")

// RenderGrid renders a cell grid to an image using the system text
// renderer (font auto-detected). Callers are responsible for supplying
// a non-empty grid; this function does not synthesize background fill
// for the empty case.
func RenderGrid(grid [][]texelcore.Cell) (image.Image, error) {
	if len(grid) == 0 {
		return nil, ErrEmptyGrid
	}
	if len(grid[0]) == 0 {
		return nil, ErrEmptyGrid
	}
	fontPath, err := textrender.DetectFont()
	if err != nil {
		return nil, fmt.Errorf("thumbnail: font detect: %w", err)
	}
	renderer, err := textrender.New(textrender.Config{FontPath: fontPath})
	if err != nil {
		return nil, fmt.Errorf("thumbnail: renderer: %w", err)
	}
	return renderer.Render(grid), nil
}

// WritePNGAtomic encodes img to path via tmp+rename so a crash mid-
// write doesn't leave a half-PNG. Used both by server lifecycle
// capture and by the client screenshot path.
func WritePNGAtomic(path string, img image.Image) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("thumbnail: create %s: %w", tmp, err)
	}
	encErr := png.Encode(f, img)
	syncErr := f.Sync()
	closeErr := f.Close()
	if encErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		switch {
		case encErr != nil:
			return fmt.Errorf("thumbnail: encode: %w", encErr)
		case syncErr != nil:
			return fmt.Errorf("thumbnail: sync: %w", syncErr)
		default:
			return fmt.Errorf("thumbnail: close: %w", closeErr)
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("thumbnail: rename %s: %w", path, err)
	}
	return nil
}

// DownscaleAspectFit returns a (targetW × targetH) RGBA image with src
// drawn into the centred subrect that preserves src's aspect ratio.
// Background outside the scaled rect is left transparent (zero pixels);
// callers wanting an opaque background should fill before passing.
func DownscaleAspectFit(src image.Image, targetW, targetH int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	srcW := src.Bounds().Dx()
	srcH := src.Bounds().Dy()
	if srcW == 0 || srcH == 0 {
		return dst
	}
	srcRatio := float64(srcW) / float64(srcH)
	dstRatio := float64(targetW) / float64(targetH)
	var scaledW, scaledH int
	if srcRatio > dstRatio {
		scaledW = targetW
		scaledH = int(float64(targetW) / srcRatio)
	} else {
		scaledH = targetH
		scaledW = int(float64(targetH) * srcRatio)
	}
	if scaledW < 1 {
		scaledW = 1
	}
	if scaledH < 1 {
		scaledH = 1
	}
	offsetX := (targetW - scaledW) / 2
	offsetY := (targetH - scaledH) / 2
	dstRect := image.Rect(offsetX, offsetY, offsetX+scaledW, offsetY+scaledH)
	xdraw.ApproxBiLinear.Scale(dst, dstRect, src, src.Bounds(), stddraw.Over, nil)
	return dst
}

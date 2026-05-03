// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package thumbnail

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	texelcore "github.com/framegrace/texelui/core"
)

func makeTestImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x80, A: 0xFF})
		}
	}
	return img
}

func TestRenderGrid_Smoke(t *testing.T) {
	grid := [][]texelcore.Cell{
		{{Ch: 'h'}, {Ch: 'e'}, {Ch: 'l'}, {Ch: 'l'}},
		{{Ch: 'o'}, {Ch: ' '}, {Ch: 't'}, {Ch: 'x'}},
	}
	img, err := RenderGrid(grid)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if img == nil {
		t.Fatalf("nil image")
	}
	if img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 {
		t.Fatalf("zero-sized image: %v", img.Bounds())
	}
}

func TestRenderGrid_EmptyGrid(t *testing.T) {
	if _, err := RenderGrid(nil); err == nil {
		t.Fatalf("expected error on nil grid")
	}
	if _, err := RenderGrid([][]texelcore.Cell{}); err == nil {
		t.Fatalf("expected error on empty grid")
	}
}

func TestWritePNGAtomic_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thumb.png")
	if err := WritePNGAtomic(path, makeTestImage(40, 30)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp file not cleaned: stat err=%v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if img.Bounds().Dx() != 40 || img.Bounds().Dy() != 30 {
		t.Fatalf("decoded dims = %dx%d, want 40x30", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestWritePNGAtomic_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thumb.png")
	if err := WritePNGAtomic(path, makeTestImage(20, 20)); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WritePNGAtomic(path, makeTestImage(40, 40)); err != nil {
		t.Fatalf("second write: %v", err)
	}
	f, _ := os.Open(path)
	defer f.Close()
	img, _ := png.Decode(f)
	if img.Bounds().Dx() != 40 {
		t.Fatalf("expected overwrite to 40x40, got %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestDownscaleAspectFit_Wider(t *testing.T) {
	src := makeTestImage(800, 200)
	out := DownscaleAspectFit(src, 480, 270)
	if out.Bounds().Dx() != 480 || out.Bounds().Dy() != 270 {
		t.Fatalf("expected 480x270 canvas, got %dx%d", out.Bounds().Dx(), out.Bounds().Dy())
	}
}

func TestDownscaleAspectFit_Taller(t *testing.T) {
	src := makeTestImage(200, 800)
	out := DownscaleAspectFit(src, 480, 270)
	if out.Bounds().Dx() != 480 || out.Bounds().Dy() != 270 {
		t.Fatalf("expected 480x270 canvas, got %dx%d", out.Bounds().Dx(), out.Bounds().Dy())
	}
}

func TestDownscaleAspectFit_AlreadySmall(t *testing.T) {
	src := makeTestImage(100, 80)
	out := DownscaleAspectFit(src, 480, 270)
	if out.Bounds().Dx() != 480 || out.Bounds().Dy() != 270 {
		t.Fatalf("expected 480x270 canvas, got %dx%d", out.Bounds().Dx(), out.Bounds().Dy())
	}
}

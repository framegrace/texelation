// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"encoding/hex"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

// fakeRenderer is a stub ThumbnailRenderer used by trigger tests so
// we don't drag textrender + a real font into the unit harness.
type fakeRenderer struct {
	calls int
}

func (f *fakeRenderer) RenderSessionThumbnail(id [16]byte) (image.Image, bool) {
	f.calls++
	img := image.NewRGBA(image.Rect(0, 0, 80, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			img.Set(x, y, color.RGBA{R: 0x40, G: 0x40, B: 0x40, A: 0xFF})
		}
	}
	return img, true
}

type skipRenderer struct{}

func (skipRenderer) RenderSessionThumbnail(id [16]byte) (image.Image, bool) {
	return nil, false
}

type panicRenderer struct{}

func (panicRenderer) RenderSessionThumbnail(id [16]byte) (image.Image, bool) {
	panic("intentional test panic")
}

func TestCaptureThumbnail_WritesPNG(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := [16]byte{0x77}
	r := &fakeRenderer{}
	if err := captureThumbnail(dir, id, r); err != nil {
		t.Fatalf("capture: %v", err)
	}
	pngPath := filepath.Join(dir, SessionsDirName, hex.EncodeToString(id[:])+".png")
	if _, err := os.Stat(pngPath); err != nil {
		t.Fatalf("stat png: %v", err)
	}
}

func TestCaptureThumbnail_RendererSkip(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := [16]byte{0x88}
	if err := captureThumbnail(dir, id, skipRenderer{}); err != nil {
		t.Fatalf("capture: %v", err)
	}
	pngPath := filepath.Join(dir, SessionsDirName, hex.EncodeToString(id[:])+".png")
	if _, err := os.Stat(pngPath); !os.IsNotExist(err) {
		t.Errorf("expected no PNG when renderer skips, stat err=%v", err)
	}
}

func TestCaptureThumbnail_NilRendererSilent(t *testing.T) {
	if err := captureThumbnail(t.TempDir(), [16]byte{}, nil); err != nil {
		t.Errorf("expected nil error for nil renderer, got %v", err)
	}
}

func TestCaptureThumbnail_RendererPanicSurvives(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755)
	if err := captureThumbnail(dir, [16]byte{0x99}, panicRenderer{}); err == nil {
		t.Errorf("expected error from panicking renderer")
	}
}

func TestManager_CaptureOnShutdown(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0x55}
	if err := os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25_000_000); err != nil { // 25ms in ns
		t.Fatalf("enable: %v", err)
	}
	r := &fakeRenderer{}
	m.SetThumbnailRenderer(r)
	if _, err := m.NewSessionWithID(id); err != nil {
		t.Fatalf("new: %v", err)
	}
	m.ShutdownSessions()
	if r.calls < 1 {
		t.Errorf("expected at least 1 thumbnail render, got %d", r.calls)
	}
	pngPath := filepath.Join(dir, SessionsDirName, hex.EncodeToString(id[:])+".png")
	if _, err := os.Stat(pngPath); err != nil {
		t.Errorf("expected PNG after shutdown, stat err=%v", err)
	}
}

func TestManager_CaptureOnLastDisconnect(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0x66}
	if err := os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25_000_000); err != nil {
		t.Fatalf("enable: %v", err)
	}
	r := &fakeRenderer{}
	m.SetThumbnailRenderer(r)
	if _, err := m.NewSessionWithID(id); err != nil {
		t.Fatalf("new: %v", err)
	}
	m.Close(id)
	if r.calls < 1 {
		t.Errorf("expected thumbnail render on Close, got %d", r.calls)
	}
	pngPath := filepath.Join(dir, SessionsDirName, hex.EncodeToString(id[:])+".png")
	if _, err := os.Stat(pngPath); err != nil {
		t.Errorf("expected PNG after Close, stat err=%v", err)
	}
}

func TestManager_NoCaptureWhenRendererUnset(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0xAA}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25_000_000); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := m.NewSessionWithID(id); err != nil {
		t.Fatalf("new: %v", err)
	}
	m.Close(id)
	pngPath := filepath.Join(dir, SessionsDirName, hex.EncodeToString(id[:])+".png")
	if _, err := os.Stat(pngPath); !os.IsNotExist(err) {
		t.Errorf("expected no PNG when renderer unset, stat err=%v", err)
	}
}

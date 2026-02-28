// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/remote_graphics_test.go
// Summary: Tests for RemoteGraphicsProvider that sends graphics commands over the protocol.
// Usage: Executed during `go test` to verify remote image surface behaviour.

package server

import (
	"bytes"
	"image/color"
	"image/png"
	"sync"
	"testing"

	texelcore "github.com/framegrace/texelui/core"

	"github.com/framegrace/texelation/protocol"
)

type capturedMessage struct {
	msgType uint8
	payload []byte
}

func newTestSender() (MessageSender, *[]capturedMessage, *sync.Mutex) {
	var msgs []capturedMessage
	var mu sync.Mutex
	sender := func(msgType uint8, payload []byte) {
		mu.Lock()
		cp := make([]byte, len(payload))
		copy(cp, payload)
		msgs = append(msgs, capturedMessage{msgType, cp})
		mu.Unlock()
	}
	return sender, &msgs, &mu
}

func TestRemoteGraphicsCapability(t *testing.T) {
	sender, _, _ := newTestSender()
	paneID := [16]byte{1, 2, 3}
	p := NewRemoteGraphicsProvider(paneID, sender)

	if got := p.Capability(); got != texelcore.GraphicsKitty {
		t.Errorf("Capability() = %v, want GraphicsKitty (%v)", got, texelcore.GraphicsKitty)
	}
}

func TestRemoteGraphicsCreateSurface(t *testing.T) {
	sender, _, _ := newTestSender()
	paneID := [16]byte{1, 2, 3}
	p := NewRemoteGraphicsProvider(paneID, sender)

	s1 := p.CreateSurface(100, 50)
	if s1.ID() != 1 {
		t.Errorf("first surface ID = %d, want 1", s1.ID())
	}
	buf := s1.Buffer()
	if buf == nil {
		t.Fatal("Buffer() returned nil")
	}
	bounds := buf.Bounds()
	if bounds.Dx() != 100 || bounds.Dy() != 50 {
		t.Errorf("buffer size = %dx%d, want 100x50", bounds.Dx(), bounds.Dy())
	}

	s2 := p.CreateSurface(200, 100)
	if s2.ID() != 2 {
		t.Errorf("second surface ID = %d, want 2", s2.ID())
	}
}

func TestRemoteGraphicsUpdateSendsUpload(t *testing.T) {
	sender, msgs, mu := newTestSender()
	paneID := [16]byte{0xAA, 0xBB}
	p := NewRemoteGraphicsProvider(paneID, sender)

	s := p.CreateSurface(4, 4)
	// Draw a red pixel at (0,0).
	s.Buffer().Set(0, 0, color.RGBA{R: 255, A: 255})

	if err := s.Update(); err != nil {
		t.Fatalf("Update() error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(*msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*msgs))
	}
	msg := (*msgs)[0]
	if msg.msgType != uint8(protocol.MsgImageUpload) {
		t.Errorf("msgType = %d, want MsgImageUpload (%d)", msg.msgType, protocol.MsgImageUpload)
	}

	// Decode the payload and verify fields.
	upload, err := protocol.DecodeImageUpload(msg.payload)
	if err != nil {
		t.Fatalf("DecodeImageUpload: %v", err)
	}
	if upload.PaneID != paneID {
		t.Errorf("PaneID = %x, want %x", upload.PaneID, paneID)
	}
	if upload.SurfaceID != s.ID() {
		t.Errorf("SurfaceID = %d, want %d", upload.SurfaceID, s.ID())
	}
	if upload.Width != 4 || upload.Height != 4 {
		t.Errorf("dimensions = %dx%d, want 4x4", upload.Width, upload.Height)
	}
	if upload.Format != 0 {
		t.Errorf("Format = %d, want 0 (PNG)", upload.Format)
	}

	// Verify the data is a valid PNG containing the red pixel.
	img, err := png.Decode(bytes.NewReader(upload.Data))
	if err != nil {
		t.Fatalf("PNG decode error: %v", err)
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if r>>8 != 255 || g>>8 != 0 || b>>8 != 0 || a>>8 != 255 {
		t.Errorf("pixel(0,0) = (%d,%d,%d,%d), want (255,0,0,255)", r>>8, g>>8, b>>8, a>>8)
	}
}

func TestRemoteGraphicsPlaceSendsPlace(t *testing.T) {
	sender, msgs, mu := newTestSender()
	paneID := [16]byte{0xCC}
	p := NewRemoteGraphicsProvider(paneID, sender)

	s := p.CreateSurface(80, 24)

	// Build a minimal Painter for the Place call.
	buf := make([][]texelcore.Cell, 24)
	for i := range buf {
		buf[i] = make([]texelcore.Cell, 80)
	}
	painter := texelcore.NewPainter(buf, texelcore.Rect{X: 0, Y: 0, W: 80, H: 24})

	rect := texelcore.Rect{X: 5, Y: 10, W: 30, H: 12}
	s.Place(painter, rect, 3)

	mu.Lock()
	defer mu.Unlock()

	if len(*msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*msgs))
	}
	msg := (*msgs)[0]
	if msg.msgType != uint8(protocol.MsgImagePlace) {
		t.Errorf("msgType = %d, want MsgImagePlace (%d)", msg.msgType, protocol.MsgImagePlace)
	}

	place, err := protocol.DecodeImagePlace(msg.payload)
	if err != nil {
		t.Fatalf("DecodeImagePlace: %v", err)
	}
	if place.PaneID != paneID {
		t.Errorf("PaneID mismatch")
	}
	if place.SurfaceID != s.ID() {
		t.Errorf("SurfaceID = %d, want %d", place.SurfaceID, s.ID())
	}
	if place.X != 5 || place.Y != 10 || place.W != 30 || place.H != 12 {
		t.Errorf("rect = (%d,%d,%d,%d), want (5,10,30,12)", place.X, place.Y, place.W, place.H)
	}
	if place.ZIndex != 3 {
		t.Errorf("ZIndex = %d, want 3", place.ZIndex)
	}
}

func TestRemoteGraphicsDeleteSendsDelete(t *testing.T) {
	sender, msgs, mu := newTestSender()
	paneID := [16]byte{0xDD}
	p := NewRemoteGraphicsProvider(paneID, sender)

	s := p.CreateSurface(10, 10)
	surfaceID := s.ID()

	s.Delete()

	mu.Lock()
	defer mu.Unlock()

	if len(*msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*msgs))
	}
	msg := (*msgs)[0]
	if msg.msgType != uint8(protocol.MsgImageDelete) {
		t.Errorf("msgType = %d, want MsgImageDelete (%d)", msg.msgType, protocol.MsgImageDelete)
	}

	del, err := protocol.DecodeImageDelete(msg.payload)
	if err != nil {
		t.Fatalf("DecodeImageDelete: %v", err)
	}
	if del.PaneID != paneID {
		t.Errorf("PaneID mismatch")
	}
	if del.SurfaceID != surfaceID {
		t.Errorf("SurfaceID = %d, want %d", del.SurfaceID, surfaceID)
	}

	// Buffer should be nil after delete.
	if s.Buffer() != nil {
		t.Error("Buffer() should return nil after Delete()")
	}
}

func TestRemoteGraphicsResetSendsReset(t *testing.T) {
	sender, msgs, mu := newTestSender()
	paneID := [16]byte{0xEE, 0xFF}
	p := NewRemoteGraphicsProvider(paneID, sender)

	p.Reset()

	mu.Lock()
	defer mu.Unlock()

	if len(*msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*msgs))
	}
	msg := (*msgs)[0]
	if msg.msgType != uint8(protocol.MsgImageReset) {
		t.Errorf("msgType = %d, want MsgImageReset (%d)", msg.msgType, protocol.MsgImageReset)
	}

	reset, err := protocol.DecodeImageReset(msg.payload)
	if err != nil {
		t.Fatalf("DecodeImageReset: %v", err)
	}
	if reset.PaneID != paneID {
		t.Errorf("PaneID = %x, want %x", reset.PaneID, paneID)
	}
}

func TestRemoteGraphicsSurfaceIDsIncrement(t *testing.T) {
	sender, _, _ := newTestSender()
	p := NewRemoteGraphicsProvider([16]byte{}, sender)

	ids := make(map[uint32]bool)
	for i := 0; i < 100; i++ {
		s := p.CreateSurface(1, 1)
		if ids[s.ID()] {
			t.Fatalf("duplicate surface ID %d at iteration %d", s.ID(), i)
		}
		ids[s.ID()] = true
	}
}

func TestRemoteGraphicsMultipleOperations(t *testing.T) {
	sender, msgs, mu := newTestSender()
	paneID := [16]byte{0x42}
	p := NewRemoteGraphicsProvider(paneID, sender)

	s := p.CreateSurface(2, 2)
	s.Buffer().Set(0, 0, color.RGBA{G: 255, A: 255})

	if err := s.Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}

	buf := make([][]texelcore.Cell, 24)
	for i := range buf {
		buf[i] = make([]texelcore.Cell, 80)
	}
	painter := texelcore.NewPainter(buf, texelcore.Rect{X: 0, Y: 0, W: 80, H: 24})
	s.Place(painter, texelcore.Rect{X: 0, Y: 0, W: 2, H: 2}, 0)
	s.Delete()
	p.Reset()

	mu.Lock()
	defer mu.Unlock()

	if len(*msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(*msgs))
	}

	expected := []uint8{
		uint8(protocol.MsgImageUpload),
		uint8(protocol.MsgImagePlace),
		uint8(protocol.MsgImageDelete),
		uint8(protocol.MsgImageReset),
	}
	for i, want := range expected {
		if (*msgs)[i].msgType != want {
			t.Errorf("msg[%d].msgType = %d, want %d", i, (*msgs)[i].msgType, want)
		}
	}
}

// Verify the provider satisfies the core.GraphicsProvider interface at compile time.
var _ texelcore.GraphicsProvider = (*RemoteGraphicsProvider)(nil)

// Verify remoteSurface satisfies core.ImageSurface at compile time.
func TestRemoteGraphicsSurfaceImplementsInterface(t *testing.T) {
	sender, _, _ := newTestSender()
	p := NewRemoteGraphicsProvider([16]byte{}, sender)
	s := p.CreateSurface(1, 1)
	var _ texelcore.ImageSurface = s

	// Verify the concrete type returned is *remoteSurface.
	if _, ok := s.(*remoteSurface); !ok {
		t.Errorf("CreateSurface returned %T, want *remoteSurface", s)
	}
}

// Verify Update encodes a decodable PNG even for an empty (zero) buffer.
func TestRemoteGraphicsUpdateEmptyBuffer(t *testing.T) {
	sender, msgs, mu := newTestSender()
	p := NewRemoteGraphicsProvider([16]byte{}, sender)
	s := p.CreateSurface(8, 8)

	if err := s.Update(); err != nil {
		t.Fatalf("Update() on empty buffer: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	upload, err := protocol.DecodeImageUpload((*msgs)[0].payload)
	if err != nil {
		t.Fatalf("DecodeImageUpload: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(upload.Data))
	if err != nil {
		t.Fatalf("PNG decode: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 8 || bounds.Dy() != 8 {
		t.Errorf("decoded image size = %dx%d, want 8x8", bounds.Dx(), bounds.Dy())
	}
	// All pixels should be transparent black (zero value of RGBA).
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if r != 0 || g != 0 || b != 0 || a != 0 {
				t.Errorf("pixel(%d,%d) = (%d,%d,%d,%d), want all zero", x, y, r, g, b, a)
			}
		}
	}
}

// Use the compile-time interface check from BLANK-4.
var _ texelcore.ImageSurface = (*remoteSurface)(nil)

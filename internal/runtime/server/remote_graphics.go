// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/remote_graphics.go
// Summary: RemoteGraphicsProvider sends image commands to a texelation client via protocol messages.
// Usage: Created per-pane by the server runtime when an app requests graphics capabilities.

package server

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"sync"

	"github.com/framegrace/texelation/protocol"
	"github.com/framegrace/texelui/core"
)

// MessageSender sends an encoded protocol message to the client.
type MessageSender func(msgType uint8, payload []byte)

// remoteSurface implements core.ImageSurface for the remote provider.
type remoteSurface struct {
	provider *RemoteGraphicsProvider
	id       uint32
	buf      *image.RGBA
	width    uint16
	height   uint16
}

func (s *remoteSurface) ID() uint32          { return s.id }
func (s *remoteSurface) Buffer() *image.RGBA { return s.buf }

func (s *remoteSurface) Update() error {
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, s.buf); err != nil {
		return fmt.Errorf("png encode: %w", err)
	}
	payload, err := protocol.EncodeImageUpload(protocol.ImageUpload{
		PaneID:    s.provider.paneID,
		SurfaceID: s.id,
		Width:     s.width,
		Height:    s.height,
		Format:    0, // PNG
		Data:      pngBuf.Bytes(),
	})
	if err != nil {
		return fmt.Errorf("encode image upload: %w", err)
	}
	s.provider.send(uint8(protocol.MsgImageUpload), payload)
	return nil
}

func (s *remoteSurface) Place(p *core.Painter, rect core.Rect, zIndex int) {
	payload, _ := protocol.EncodeImagePlace(protocol.ImagePlace{
		PaneID:    s.provider.paneID,
		SurfaceID: s.id,
		X:         uint16(rect.X),
		Y:         uint16(rect.Y),
		W:         uint16(rect.W),
		H:         uint16(rect.H),
		ZIndex:    int8(zIndex),
	})
	s.provider.send(uint8(protocol.MsgImagePlace), payload)
}

func (s *remoteSurface) Delete() {
	payload, _ := protocol.EncodeImageDelete(protocol.ImageDelete{
		PaneID:    s.provider.paneID,
		SurfaceID: s.id,
	})
	s.provider.send(uint8(protocol.MsgImageDelete), payload)
	s.buf = nil
}

// RemoteGraphicsProvider sends graphics commands to a texelation client.
type RemoteGraphicsProvider struct {
	mu     sync.Mutex
	paneID [16]byte
	send   MessageSender
	nextID uint32
}

// NewRemoteGraphicsProvider creates a provider that sends messages via sender.
func NewRemoteGraphicsProvider(paneID [16]byte, sender MessageSender) *RemoteGraphicsProvider {
	return &RemoteGraphicsProvider{
		paneID: paneID,
		send:   sender,
		nextID: 1,
	}
}

// Capability reports that this provider supports Kitty-level image rendering.
func (p *RemoteGraphicsProvider) Capability() core.GraphicsCapability {
	return core.GraphicsKitty
}

// CreateSurface allocates an RGBA buffer and returns a surface that sends
// protocol messages when updated, placed, or deleted.
func (p *RemoteGraphicsProvider) CreateSurface(width, height int) core.ImageSurface {
	p.mu.Lock()
	defer p.mu.Unlock()
	id := p.nextID
	p.nextID++
	return &remoteSurface{
		provider: p,
		id:       id,
		buf:      image.NewRGBA(image.Rect(0, 0, width, height)),
		width:    uint16(width),
		height:   uint16(height),
	}
}

// Reset tells the client to clear all active placements for this pane.
func (p *RemoteGraphicsProvider) Reset() {
	payload, _ := protocol.EncodeImageReset(protocol.ImageReset{
		PaneID: p.paneID,
	})
	p.send(uint8(protocol.MsgImageReset), payload)
}

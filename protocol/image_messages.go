// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: protocol/image_messages.go
// Summary: Image protocol messages for GraphicsProvider v2.
// Usage: Encode/decode image upload, placement, deletion, and reset messages.

package protocol

import (
	"encoding/binary"
	"fmt"
)

// ImageUpload carries image data from server to client.
type ImageUpload struct {
	PaneID    [16]byte
	SurfaceID uint32
	Width     uint16
	Height    uint16
	Format    uint8 // 0=PNG
	Data      []byte
}

// ImagePlace tells the client to display a cached image.
type ImagePlace struct {
	PaneID    [16]byte
	SurfaceID uint32
	X, Y      uint16
	W, H      uint16
	ZIndex    int8
}

// ImageDelete tells the client to free image data.
type ImageDelete struct {
	PaneID    [16]byte
	SurfaceID uint32
}

// ImageReset tells the client to clear all placements for a pane.
type ImageReset struct {
	PaneID [16]byte
}

// EncodeImageUpload serialises an ImageUpload into a byte slice.
func EncodeImageUpload(u ImageUpload) ([]byte, error) {
	// Layout: PaneID(16) + SurfaceID(4) + Width(2) + Height(2) + Format(1) + DataLen(4) + Data
	buf := make([]byte, 29+len(u.Data))
	copy(buf[0:16], u.PaneID[:])
	binary.LittleEndian.PutUint32(buf[16:20], u.SurfaceID)
	binary.LittleEndian.PutUint16(buf[20:22], u.Width)
	binary.LittleEndian.PutUint16(buf[22:24], u.Height)
	buf[24] = u.Format
	binary.LittleEndian.PutUint32(buf[25:29], uint32(len(u.Data)))
	copy(buf[29:], u.Data)
	return buf, nil
}

// DecodeImageUpload deserialises an ImageUpload from a byte slice.
func DecodeImageUpload(b []byte) (ImageUpload, error) {
	if len(b) < 29 {
		return ImageUpload{}, fmt.Errorf("image upload too short: %d", len(b))
	}
	var u ImageUpload
	copy(u.PaneID[:], b[0:16])
	u.SurfaceID = binary.LittleEndian.Uint32(b[16:20])
	u.Width = binary.LittleEndian.Uint16(b[20:22])
	u.Height = binary.LittleEndian.Uint16(b[22:24])
	u.Format = b[24]
	dataLen := binary.LittleEndian.Uint32(b[25:29])
	if len(b) < 29+int(dataLen) {
		return ImageUpload{}, fmt.Errorf("image upload data truncated")
	}
	u.Data = make([]byte, dataLen)
	copy(u.Data, b[29:29+dataLen])
	return u, nil
}

// EncodeImagePlace serialises an ImagePlace into a byte slice.
func EncodeImagePlace(p ImagePlace) ([]byte, error) {
	// Layout: PaneID(16) + SurfaceID(4) + X(2) + Y(2) + W(2) + H(2) + ZIndex(1) = 29
	buf := make([]byte, 29)
	copy(buf[0:16], p.PaneID[:])
	binary.LittleEndian.PutUint32(buf[16:20], p.SurfaceID)
	binary.LittleEndian.PutUint16(buf[20:22], p.X)
	binary.LittleEndian.PutUint16(buf[22:24], p.Y)
	binary.LittleEndian.PutUint16(buf[24:26], p.W)
	binary.LittleEndian.PutUint16(buf[26:28], p.H)
	buf[28] = byte(p.ZIndex)
	return buf, nil
}

// DecodeImagePlace deserialises an ImagePlace from a byte slice.
func DecodeImagePlace(b []byte) (ImagePlace, error) {
	if len(b) < 29 {
		return ImagePlace{}, fmt.Errorf("image place too short: %d", len(b))
	}
	var p ImagePlace
	copy(p.PaneID[:], b[0:16])
	p.SurfaceID = binary.LittleEndian.Uint32(b[16:20])
	p.X = binary.LittleEndian.Uint16(b[20:22])
	p.Y = binary.LittleEndian.Uint16(b[22:24])
	p.W = binary.LittleEndian.Uint16(b[24:26])
	p.H = binary.LittleEndian.Uint16(b[26:28])
	p.ZIndex = int8(b[28])
	return p, nil
}

// EncodeImageDelete serialises an ImageDelete into a byte slice.
func EncodeImageDelete(d ImageDelete) ([]byte, error) {
	// Layout: PaneID(16) + SurfaceID(4) = 20
	buf := make([]byte, 20)
	copy(buf[0:16], d.PaneID[:])
	binary.LittleEndian.PutUint32(buf[16:20], d.SurfaceID)
	return buf, nil
}

// DecodeImageDelete deserialises an ImageDelete from a byte slice.
func DecodeImageDelete(b []byte) (ImageDelete, error) {
	if len(b) < 20 {
		return ImageDelete{}, fmt.Errorf("image delete too short: %d", len(b))
	}
	var d ImageDelete
	copy(d.PaneID[:], b[0:16])
	d.SurfaceID = binary.LittleEndian.Uint32(b[16:20])
	return d, nil
}

// EncodeImageReset serialises an ImageReset into a byte slice.
func EncodeImageReset(r ImageReset) ([]byte, error) {
	buf := make([]byte, 16)
	copy(buf[0:16], r.PaneID[:])
	return buf, nil
}

// DecodeImageReset deserialises an ImageReset from a byte slice.
func DecodeImageReset(b []byte) (ImageReset, error) {
	if len(b) < 16 {
		return ImageReset{}, fmt.Errorf("image reset too short: %d", len(b))
	}
	var r ImageReset
	copy(r.PaneID[:], b[0:16])
	return r, nil
}

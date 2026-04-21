// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
)

var (
	// ErrViewportInverted is returned when ViewTopIdx > ViewBottomIdx in a
	// non-alt-screen ViewportUpdate.
	ErrViewportInverted = errors.New("protocol: viewport top > bottom")
	// ErrViewportZeroDim is returned when Rows or Cols is zero.
	ErrViewportZeroDim = errors.New("protocol: viewport has zero dimension")
)

// ViewportUpdate is sent by the client whenever its per-pane viewport changes
// (scroll, resize, alt-screen enter/exit). The server uses it to clip
// subsequent BufferDeltas.
type ViewportUpdate struct {
	PaneID         [16]byte
	AltScreen      bool
	ViewTopIdx     int64
	ViewBottomIdx  int64
	WrapSegmentIdx uint16
	Rows           uint16
	Cols           uint16
	AutoFollow     bool
}

func EncodeViewportUpdate(v ViewportUpdate) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 45))
	buf.Write(v.PaneID[:])
	var bools uint8
	if v.AltScreen {
		bools |= 1 << 0
	}
	if v.AutoFollow {
		bools |= 1 << 1
	}
	buf.WriteByte(bools)
	if err := binary.Write(buf, binary.LittleEndian, v.ViewTopIdx); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, v.ViewBottomIdx); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, v.WrapSegmentIdx); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, v.Rows); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, v.Cols); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodeViewportUpdate(b []byte) (ViewportUpdate, error) {
	var v ViewportUpdate
	// 16 + 1 + 8 + 8 + 2 + 2 + 2 = 39
	if len(b) < 39 {
		return v, ErrPayloadShort
	}
	copy(v.PaneID[:], b[:16])
	bools := b[16]
	v.AltScreen = bools&(1<<0) != 0
	v.AutoFollow = bools&(1<<1) != 0
	v.ViewTopIdx = int64(binary.LittleEndian.Uint64(b[17:25]))
	v.ViewBottomIdx = int64(binary.LittleEndian.Uint64(b[25:33]))
	v.WrapSegmentIdx = binary.LittleEndian.Uint16(b[33:35])
	v.Rows = binary.LittleEndian.Uint16(b[35:37])
	v.Cols = binary.LittleEndian.Uint16(b[37:39])
	if !v.AltScreen && v.ViewTopIdx > v.ViewBottomIdx {
		return ViewportUpdate{}, ErrViewportInverted
	}
	if v.Rows == 0 || v.Cols == 0 {
		return ViewportUpdate{}, ErrViewportZeroDim
	}
	return v, nil
}

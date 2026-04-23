// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: protocol/pane_viewport_state.go
// Summary: PaneViewportState wire type + encoder/decoder for viewport-aware resume.
// Usage: Carried inside ResumeRequest.PaneViewports; server uses it to re-seat
//
//	each pane's sparse.ViewWindow before the first post-resume publish.

package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// ErrPaneViewportZeroDim is returned when ViewportRows or ViewportCols is zero.
var ErrPaneViewportZeroDim = errors.New("protocol: pane viewport has zero dimension")

// PaneViewportState is the per-pane resume payload in MsgResumeRequest. It
// describes where each pane was scrolled to at disconnect so the server can
// land the pane's ViewWindow exactly there on reconnect.
//
// AltScreen=true causes the server to skip scroll resolution; the pane's own
// alt-screen buffer is sent verbatim on first paint.
//
// AutoFollow=true causes the server to clamp ViewBottomIdx to Store.Max() at
// first-paint time, so the client lands at the live edge. Scroll fields are
// still required on the wire so the server can fall back if AutoFollow flips
// while the payload is in flight (cheap defensive default, not correctness).
type PaneViewportState struct {
	PaneID         [16]byte
	AltScreen      bool
	AutoFollow     bool
	ViewBottomIdx  int64
	WrapSegmentIdx uint16
	ViewportRows   uint16
	ViewportCols   uint16
}

// EncodedPaneViewportStateSize is the fixed wire size per entry:
// 16 (paneID) + 1 (bools) + 8 (ViewBottomIdx) + 2 (WrapSegmentIdx) + 2 (Rows) + 2 (Cols) = 31.
const EncodedPaneViewportStateSize = 31

func (p PaneViewportState) Validate() error {
	if p.ViewportRows == 0 || p.ViewportCols == 0 {
		return ErrPaneViewportZeroDim
	}
	return nil
}

func EncodePaneViewportState(p PaneViewportState) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(make([]byte, 0, EncodedPaneViewportStateSize))
	if _, err := buf.Write(p.PaneID[:]); err != nil {
		return nil, err
	}
	var bools uint8
	if p.AltScreen {
		bools |= 1 << 0
	}
	if p.AutoFollow {
		bools |= 1 << 1
	}
	if err := buf.WriteByte(bools); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, p.ViewBottomIdx); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, p.WrapSegmentIdx); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, p.ViewportRows); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, p.ViewportCols); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodePaneViewportState reads one entry from b and returns it along with the
// number of bytes consumed. Used by DecodeResumeRequest for the list tail.
func DecodePaneViewportState(b []byte) (PaneViewportState, int, error) {
	var p PaneViewportState
	if len(b) < EncodedPaneViewportStateSize {
		return p, 0, ErrPayloadShort
	}
	copy(p.PaneID[:], b[:16])
	bools := b[16]
	p.AltScreen = bools&(1<<0) != 0
	p.AutoFollow = bools&(1<<1) != 0
	p.ViewBottomIdx = int64(binary.LittleEndian.Uint64(b[17:25]))
	p.WrapSegmentIdx = binary.LittleEndian.Uint16(b[25:27])
	p.ViewportRows = binary.LittleEndian.Uint16(b[27:29])
	p.ViewportCols = binary.LittleEndian.Uint16(b[29:31])
	if p.ViewportRows == 0 || p.ViewportCols == 0 {
		return PaneViewportState{}, 0, ErrPaneViewportZeroDim
	}
	return p, EncodedPaneViewportStateSize, nil
}

// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
)

var (
	// ErrFetchRangeInverted is returned when LoIdx > HiIdx.
	// Note: LoIdx == HiIdx is a valid empty range and is allowed.
	ErrFetchRangeInverted = errors.New("protocol: fetch range lo > hi")
	// ErrFetchRangeNegative is returned when LoIdx < 0.
	ErrFetchRangeNegative = errors.New("protocol: fetch range lo index is negative")
)

// FetchRangeFlags is a bitmask set on FetchRangeResponse.
//
// Flag semantics and valid combinations:
//
//   - FetchRangeAltScreenActive is mutually exclusive with all other flags.
//     When the pane is in alt-screen the server short-circuits before checking
//     retention or row content, so BelowRetention and Empty are never set.
//
//   - FetchRangeBelowRetention and FetchRangeEmpty may legitimately co-occur.
//     This happens when part of the requested range has been evicted from the
//     retention window and the remaining resident rows happen to lie outside
//     [LoIdx, HiIdx) — the response has no rows but the client should know
//     that some content was evicted, not merely absent.
//
//   - Clients must ignore unknown bits for forward-compatibility.  Future
//     versions of the protocol may add new flags without a version bump.
type FetchRangeFlags uint8

const (
	FetchRangeNone            FetchRangeFlags = 0
	FetchRangeAltScreenActive FetchRangeFlags = 1 << 0
	FetchRangeBelowRetention  FetchRangeFlags = 1 << 1
	FetchRangeEmpty           FetchRangeFlags = 1 << 2
)

// FetchRange is a client → server request for a slice of scrollback.
// LoIdx is inclusive; HiIdx is exclusive. AsOfRevision is informational
// (server stamps the response with its own Revision at read time).
type FetchRange struct {
	RequestID    uint32
	PaneID       [16]byte
	LoIdx        int64
	HiIdx        int64
	AsOfRevision uint32
}

// LogicalRow is one row in a FetchRangeResponse. Spans use the same shared
// Styles table as BufferDelta rows.
type LogicalRow struct {
	GlobalIdx int64
	Wrapped   bool
	NoWrap    bool
	Spans     []CellSpan
}

// FetchRangeResponse is the server → client reply.
type FetchRangeResponse struct {
	RequestID uint32
	PaneID    [16]byte
	Revision  uint32
	Flags     FetchRangeFlags
	Styles    []StyleEntry
	Rows      []LogicalRow
}

// Validate rejects malformed FetchRange requests.  Applied symmetrically on
// encode and decode.
func (f FetchRange) Validate() error {
	if f.LoIdx < 0 {
		return ErrFetchRangeNegative
	}
	if f.LoIdx > f.HiIdx {
		return ErrFetchRangeInverted
	}
	return nil
}

func EncodeFetchRange(f FetchRange) ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(make([]byte, 0, 40))
	if err := binary.Write(buf, binary.LittleEndian, f.RequestID); err != nil {
		return nil, err
	}
	if _, err := buf.Write(f.PaneID[:]); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, f.LoIdx); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, f.HiIdx); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, f.AsOfRevision); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodeFetchRange(b []byte) (FetchRange, error) {
	var f FetchRange
	// 4 + 16 + 8 + 8 + 4 = 40
	if len(b) < 40 {
		return f, ErrPayloadShort
	}
	f.RequestID = binary.LittleEndian.Uint32(b[:4])
	copy(f.PaneID[:], b[4:20])
	f.LoIdx = int64(binary.LittleEndian.Uint64(b[20:28]))
	f.HiIdx = int64(binary.LittleEndian.Uint64(b[28:36]))
	f.AsOfRevision = binary.LittleEndian.Uint32(b[36:40])
	if f.LoIdx < 0 {
		return FetchRange{}, ErrFetchRangeNegative
	}
	if f.LoIdx > f.HiIdx {
		return FetchRange{}, ErrFetchRangeInverted
	}
	return f, nil
}

func EncodeFetchRangeResponse(r FetchRangeResponse) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 64))
	if err := binary.Write(buf, binary.LittleEndian, r.RequestID); err != nil {
		return nil, err
	}
	if _, err := buf.Write(r.PaneID[:]); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, r.Revision); err != nil {
		return nil, err
	}
	if err := buf.WriteByte(byte(r.Flags)); err != nil {
		return nil, err
	}

	if len(r.Styles) > 0xFFFF || len(r.Rows) > 0xFFFF {
		return nil, ErrBufferTooLarge
	}
	if err := binary.Write(buf, binary.LittleEndian, uint16(len(r.Styles))); err != nil {
		return nil, err
	}
	// Reuse the same inline style-entry encoding as BufferDelta — factor out
	// if the duplication grows. For now it's two sites.
	for _, s := range r.Styles {
		// Dynamic colors are not supported in FetchRange rows for v1.
		if s.AttrFlags&AttrHasDynamic != 0 {
			return nil, ErrUnsupportedDynamicColor
		}
		if err := binary.Write(buf, binary.LittleEndian, s.AttrFlags); err != nil {
			return nil, err
		}
		if err := buf.WriteByte(byte(s.FgModel)); err != nil {
			return nil, err
		}
		if err := binary.Write(buf, binary.LittleEndian, s.FgValue); err != nil {
			return nil, err
		}
		if err := buf.WriteByte(byte(s.BgModel)); err != nil {
			return nil, err
		}
		if err := binary.Write(buf, binary.LittleEndian, s.BgValue); err != nil {
			return nil, err
		}
	}

	if err := binary.Write(buf, binary.LittleEndian, uint16(len(r.Rows))); err != nil {
		return nil, err
	}
	for _, row := range r.Rows {
		if err := binary.Write(buf, binary.LittleEndian, row.GlobalIdx); err != nil {
			return nil, err
		}
		var flags uint8
		if row.Wrapped {
			flags |= 1 << 0
		}
		if row.NoWrap {
			flags |= 1 << 1
		}
		if err := buf.WriteByte(flags); err != nil {
			return nil, err
		}
		if len(row.Spans) > 0xFFFF {
			return nil, ErrBufferTooLarge
		}
		if err := binary.Write(buf, binary.LittleEndian, uint16(len(row.Spans))); err != nil {
			return nil, err
		}
		for _, span := range row.Spans {
			textBytes := []byte(span.Text)
			if len(textBytes) > 0xFFFF {
				return nil, ErrInvalidSpan
			}
			if int(span.StyleIndex) >= len(r.Styles) {
				return nil, ErrStyleIndexOutOfRange
			}
			if err := binary.Write(buf, binary.LittleEndian, span.StartCol); err != nil {
				return nil, err
			}
			if err := binary.Write(buf, binary.LittleEndian, uint16(len(textBytes))); err != nil {
				return nil, err
			}
			if err := binary.Write(buf, binary.LittleEndian, span.StyleIndex); err != nil {
				return nil, err
			}
			if _, err := buf.Write(textBytes); err != nil {
				return nil, err
			}
		}
	}
	return buf.Bytes(), nil
}

func DecodeFetchRangeResponse(b []byte) (FetchRangeResponse, error) {
	var r FetchRangeResponse
	// 4 + 16 + 4 + 1 = 25
	if len(b) < 25 {
		return r, ErrPayloadShort
	}
	r.RequestID = binary.LittleEndian.Uint32(b[:4])
	copy(r.PaneID[:], b[4:20])
	r.Revision = binary.LittleEndian.Uint32(b[20:24])
	r.Flags = FetchRangeFlags(b[24])
	b = b[25:]

	if len(b) < 2 {
		return r, ErrPayloadShort
	}
	styleCount := binary.LittleEndian.Uint16(b[:2])
	b = b[2:]
	r.Styles = make([]StyleEntry, styleCount)
	for i := 0; i < int(styleCount); i++ {
		if len(b) < 12 {
			return r, ErrPayloadShort
		}
		r.Styles[i].AttrFlags = binary.LittleEndian.Uint16(b[:2])
		r.Styles[i].FgModel = ColorModel(b[2])
		r.Styles[i].FgValue = binary.LittleEndian.Uint32(b[3:7])
		r.Styles[i].BgModel = ColorModel(b[7])
		r.Styles[i].BgValue = binary.LittleEndian.Uint32(b[8:12])
		b = b[12:]
		if r.Styles[i].AttrFlags&AttrHasDynamic != 0 {
			return r, ErrUnsupportedDynamicColor
		}
	}

	if len(b) < 2 {
		return r, ErrPayloadShort
	}
	rowCount := binary.LittleEndian.Uint16(b[:2])
	b = b[2:]
	r.Rows = make([]LogicalRow, rowCount)
	for i := 0; i < int(rowCount); i++ {
		if len(b) < 11 { // globalIdx(8)+flags(1)+spanCount(2)
			return r, ErrPayloadShort
		}
		r.Rows[i].GlobalIdx = int64(binary.LittleEndian.Uint64(b[:8]))
		rowFlags := b[8]
		r.Rows[i].Wrapped = rowFlags&(1<<0) != 0
		r.Rows[i].NoWrap = rowFlags&(1<<1) != 0
		spanCount := binary.LittleEndian.Uint16(b[9:11])
		b = b[11:]
		spans := make([]CellSpan, spanCount)
		for s := 0; s < int(spanCount); s++ {
			if len(b) < 6 {
				return r, ErrPayloadShort
			}
			startCol := binary.LittleEndian.Uint16(b[:2])
			textLen := binary.LittleEndian.Uint16(b[2:4])
			styleIndex := binary.LittleEndian.Uint16(b[4:6])
			b = b[6:]
			if len(b) < int(textLen) {
				return r, ErrPayloadShort
			}
			if int(styleIndex) >= int(styleCount) {
				return r, ErrStyleIndexOutOfRange
			}
			spans[s] = CellSpan{StartCol: startCol, Text: string(b[:textLen]), StyleIndex: styleIndex}
			b = b[textLen:]
		}
		r.Rows[i].Spans = spans
	}
	return r, nil
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: protocol/buffer_delta.go
// Summary: Implements buffer delta capabilities for the protocol definitions.
// Usage: Shared by clients and servers to encode buffer delta messages over the wire.
// Notes: Keep changes backward-compatible; any additions require coordinated version bumps.

package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
)

// BufferDeltaFlags describes optional encoding tweaks.
type BufferDeltaFlags uint8

const (
	BufferDeltaNone      BufferDeltaFlags = 0
	BufferDeltaAltScreen BufferDeltaFlags = 1 << 0
)

// MaxDecorRowIdx caps a decoration row's absolute rowIdx to a sane pane
// height. Real panes never exceed a few thousand rows; values above this
// signal a corrupt or hostile payload.
const MaxDecorRowIdx uint16 = 4096

// ColorModel represents how colours are encoded for a style.
type ColorModel uint8

const (
	ColorModelDefault ColorModel = iota
	ColorModelANSI16
	ColorModelANSI256
	ColorModelRGB
)

// StyleEntry captures the styling information applied to spans. Values are raw
// integers; it is up to higher layers to translate to tcell.Style.
// DynColorStopDesc describes a single gradient stop in the protocol.
type DynColorStopDesc struct {
	Position float32
	Color    DynColorDesc // always Solid/Pulse/Fade, no nested gradients
}

// DynColorDesc is the protocol representation of a DynamicColor descriptor.
type DynColorDesc struct {
	Type   uint8
	Base   uint32
	Target uint32
	Easing uint8
	Speed  float32
	Min    float32
	Max    float32
	Stops  []DynColorStopDesc // nil for non-gradient types
}

type StyleEntry struct {
	AttrFlags uint16
	FgModel   ColorModel
	FgValue   uint32
	BgModel   ColorModel
	BgValue   uint32
	DynFG     DynColorDesc
	DynBG     DynColorDesc
}

const (
	AttrBold uint16 = 1 << iota
	AttrUnderline
	AttrReverse
	AttrBlink
	AttrDim
	AttrItalic
)

// AttrHasDynamic indicates dynamic color descriptors follow the base style.
const AttrHasDynamic uint16 = 1 << 8

// CellSpan covers a contiguous set of cells on a row that share the same style.
type CellSpan struct {
	StartCol   uint16
	Text       string
	StyleIndex uint16
}

// RowDelta captures updates for a single row.
type RowDelta struct {
	Row   uint16
	Spans []CellSpan
}

// DecorRowDelta carries a single positional decoration row (border, app
// statusbar). RowIdx is the absolute rowIdx in the pane buffer — distinct
// from RowDelta.Row, which is gid - RowBase. Wire byte layout matches
// RowDelta exactly; the type is separate to prevent accidental mixing.
type DecorRowDelta struct {
	RowIdx uint16
	Spans  []CellSpan
}

// BufferDelta is the payload sent by the server to update pane contents.
type BufferDelta struct {
	PaneID    [16]byte
	Revision  uint32
	Flags     BufferDeltaFlags
	RowBase   int64
	Styles    []StyleEntry
	Rows      []RowDelta
	DecorRows []DecorRowDelta // rows keyed by absolute rowIdx (borders + app decoration)
}

var (
	ErrBufferTooLarge          = errors.New("protocol: buffer delta exceeds limits")
	ErrInvalidSpan             = errors.New("protocol: invalid span")
	ErrStyleIndexOutOfRange    = errors.New("protocol: span style index out of range")
	ErrUnsupportedDynamicColor = errors.New("protocol: dynamic colors unsupported in this message type")
)

// EncodeBufferDelta serialises the delta into a compact binary representation.
func EncodeBufferDelta(delta BufferDelta) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 64))
	buf.Write(delta.PaneID[:])
	if err := binary.Write(buf, binary.LittleEndian, delta.Revision); err != nil {
		return nil, err
	}
	buf.WriteByte(byte(delta.Flags))
	if err := binary.Write(buf, binary.LittleEndian, delta.RowBase); err != nil {
		return nil, err
	}

	if len(delta.Styles) > 0xFFFF || len(delta.Rows) > 0xFFFF {
		return nil, ErrBufferTooLarge
	}

	if err := binary.Write(buf, binary.LittleEndian, uint16(len(delta.Styles))); err != nil {
		return nil, err
	}
	for _, style := range delta.Styles {
		if err := binary.Write(buf, binary.LittleEndian, style.AttrFlags); err != nil {
			return nil, err
		}
		if err := buf.WriteByte(byte(style.FgModel)); err != nil {
			return nil, err
		}
		if err := binary.Write(buf, binary.LittleEndian, style.FgValue); err != nil {
			return nil, err
		}
		if err := buf.WriteByte(byte(style.BgModel)); err != nil {
			return nil, err
		}
		if err := binary.Write(buf, binary.LittleEndian, style.BgValue); err != nil {
			return nil, err
		}
		if style.AttrFlags&AttrHasDynamic != 0 {
			for _, d := range [2]DynColorDesc{style.DynFG, style.DynBG} {
				if err := buf.WriteByte(d.Type); err != nil {
					return nil, err
				}
				if err := binary.Write(buf, binary.LittleEndian, d.Base); err != nil {
					return nil, err
				}
				if err := binary.Write(buf, binary.LittleEndian, d.Target); err != nil {
					return nil, err
				}
				if err := buf.WriteByte(d.Easing); err != nil {
					return nil, err
				}
				if err := binary.Write(buf, binary.LittleEndian, d.Speed); err != nil {
					return nil, err
				}
				if err := binary.Write(buf, binary.LittleEndian, d.Min); err != nil {
					return nil, err
				}
				if err := binary.Write(buf, binary.LittleEndian, d.Max); err != nil {
					return nil, err
				}
				// Gradient stops (variable length, only for Type >= 4)
				if d.Type >= 4 {
					if len(d.Stops) > 0xFF {
						return nil, ErrBufferTooLarge
					}
					if err := buf.WriteByte(uint8(len(d.Stops))); err != nil {
						return nil, err
					}
					for _, s := range d.Stops {
						if err := binary.Write(buf, binary.LittleEndian, s.Position); err != nil {
							return nil, err
						}
						if err := buf.WriteByte(s.Color.Type); err != nil {
							return nil, err
						}
						if err := binary.Write(buf, binary.LittleEndian, s.Color.Base); err != nil {
							return nil, err
						}
						if err := binary.Write(buf, binary.LittleEndian, s.Color.Target); err != nil {
							return nil, err
						}
						if err := buf.WriteByte(s.Color.Easing); err != nil {
							return nil, err
						}
						if err := binary.Write(buf, binary.LittleEndian, s.Color.Speed); err != nil {
							return nil, err
						}
						if err := binary.Write(buf, binary.LittleEndian, s.Color.Min); err != nil {
							return nil, err
						}
						if err := binary.Write(buf, binary.LittleEndian, s.Color.Max); err != nil {
							return nil, err
						}
					}
				}
			}
		}
	}

	if err := binary.Write(buf, binary.LittleEndian, uint16(len(delta.Rows))); err != nil {
		return nil, err
	}
	for _, row := range delta.Rows {
		if len(row.Spans) > 0xFFFF {
			return nil, ErrBufferTooLarge
		}
		if err := binary.Write(buf, binary.LittleEndian, row.Row); err != nil {
			return nil, err
		}
		if err := binary.Write(buf, binary.LittleEndian, uint16(len(row.Spans))); err != nil {
			return nil, err
		}
		for _, span := range row.Spans {
			textBytes := []byte(span.Text)
			if len(textBytes) > 0xFFFF {
				return nil, ErrInvalidSpan
			}
			if int(span.StyleIndex) >= len(delta.Styles) {
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
			if len(textBytes) > 0 {
				if _, err := buf.Write(textBytes); err != nil {
					return nil, err
				}
			}
		}
	}

	if len(delta.DecorRows) > 0xFFFF {
		return nil, ErrBufferTooLarge
	}
	if err := binary.Write(buf, binary.LittleEndian, uint16(len(delta.DecorRows))); err != nil {
		return nil, err
	}
	for _, row := range delta.DecorRows {
		if row.RowIdx > MaxDecorRowIdx {
			return nil, ErrInvalidSpan
		}
		if len(row.Spans) > 0xFFFF {
			return nil, ErrBufferTooLarge
		}
		if err := binary.Write(buf, binary.LittleEndian, row.RowIdx); err != nil {
			return nil, err
		}
		if err := binary.Write(buf, binary.LittleEndian, uint16(len(row.Spans))); err != nil {
			return nil, err
		}
		for _, span := range row.Spans {
			textBytes := []byte(span.Text)
			if len(textBytes) > 0xFFFF {
				return nil, ErrInvalidSpan
			}
			if int(span.StyleIndex) >= len(delta.Styles) {
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
			if len(textBytes) > 0 {
				if _, err := buf.Write(textBytes); err != nil {
					return nil, err
				}
			}
		}
	}

	return buf.Bytes(), nil
}

// DecodeBufferDelta reverses EncodeBufferDelta.
func DecodeBufferDelta(b []byte) (BufferDelta, error) {
	var delta BufferDelta
	if len(b) < 29 { // paneID(16)+revision(4)+flags(1)+rowBase(8)
		return delta, ErrPayloadShort
	}
	copy(delta.PaneID[:], b[:16])
	delta.Revision = binary.LittleEndian.Uint32(b[16:20])
	delta.Flags = BufferDeltaFlags(b[20])
	delta.RowBase = int64(binary.LittleEndian.Uint64(b[21:29]))
	b = b[29:]

	if len(b) < 2 {
		return delta, ErrPayloadShort
	}
	styleCount := binary.LittleEndian.Uint16(b[:2])
	b = b[2:]
	delta.Styles = make([]StyleEntry, styleCount)
	for i := 0; i < int(styleCount); i++ {
		if len(b) < 12 { // attr(2) + fgModel(1)+fg(4)+bgModel(1)+bg(4)
			return delta, ErrPayloadShort
		}
		delta.Styles[i].AttrFlags = binary.LittleEndian.Uint16(b[:2])
		delta.Styles[i].FgModel = ColorModel(b[2])
		delta.Styles[i].FgValue = binary.LittleEndian.Uint32(b[3:7])
		delta.Styles[i].BgModel = ColorModel(b[7])
		delta.Styles[i].BgValue = binary.LittleEndian.Uint32(b[8:12])
		b = b[12:]
		if delta.Styles[i].AttrFlags&AttrHasDynamic != 0 {
			if len(b) < 44 {
				return delta, ErrPayloadShort
			}
			for _, d := range [2]*DynColorDesc{&delta.Styles[i].DynFG, &delta.Styles[i].DynBG} {
				d.Type = b[0]
				d.Base = binary.LittleEndian.Uint32(b[1:5])
				d.Target = binary.LittleEndian.Uint32(b[5:9])
				d.Easing = b[9]
				d.Speed = math.Float32frombits(binary.LittleEndian.Uint32(b[10:14]))
				d.Min = math.Float32frombits(binary.LittleEndian.Uint32(b[14:18]))
				d.Max = math.Float32frombits(binary.LittleEndian.Uint32(b[18:22]))
				b = b[22:]
				// Gradient stops
				if d.Type >= 4 {
					if len(b) < 1 {
						return delta, ErrPayloadShort
					}
					stopCount := int(b[0])
					b = b[1:]
					if len(b) < stopCount*26 {
						return delta, ErrPayloadShort
					}
					d.Stops = make([]DynColorStopDesc, stopCount)
					for j := 0; j < stopCount; j++ {
						d.Stops[j].Position = math.Float32frombits(binary.LittleEndian.Uint32(b[:4]))
						b = b[4:]
						d.Stops[j].Color.Type = b[0]
						d.Stops[j].Color.Base = binary.LittleEndian.Uint32(b[1:5])
						d.Stops[j].Color.Target = binary.LittleEndian.Uint32(b[5:9])
						d.Stops[j].Color.Easing = b[9]
						d.Stops[j].Color.Speed = math.Float32frombits(binary.LittleEndian.Uint32(b[10:14]))
						d.Stops[j].Color.Min = math.Float32frombits(binary.LittleEndian.Uint32(b[14:18]))
						d.Stops[j].Color.Max = math.Float32frombits(binary.LittleEndian.Uint32(b[18:22]))
						b = b[22:]
					}
				}
			}
		}
	}

	if len(b) < 2 {
		return delta, ErrPayloadShort
	}
	rowCount := binary.LittleEndian.Uint16(b[:2])
	b = b[2:]
	delta.Rows = make([]RowDelta, rowCount)
	for i := 0; i < int(rowCount); i++ {
		if len(b) < 4 {
			return delta, ErrPayloadShort
		}
		row := binary.LittleEndian.Uint16(b[:2])
		spanCount := binary.LittleEndian.Uint16(b[2:4])
		b = b[4:]
		spans := make([]CellSpan, spanCount)
		for s := 0; s < int(spanCount); s++ {
			if len(b) < 6 {
				return delta, ErrPayloadShort
			}
			startCol := binary.LittleEndian.Uint16(b[:2])
			textLen := binary.LittleEndian.Uint16(b[2:4])
			styleIndex := binary.LittleEndian.Uint16(b[4:6])
			b = b[6:]
			if len(b) < int(textLen) {
				return delta, ErrPayloadShort
			}
			text := string(b[:textLen])
			b = b[textLen:]
			if int(styleIndex) >= int(styleCount) {
				return delta, ErrStyleIndexOutOfRange
			}
			spans[s] = CellSpan{StartCol: startCol, Text: text, StyleIndex: styleIndex}
		}
		delta.Rows[i] = RowDelta{Row: row, Spans: spans}
	}

	// v3 tail: DecorRows. The 2-byte count is mandatory — no v2 fallback.
	if len(b) < 2 {
		return delta, ErrPayloadShort
	}
	decorCount := binary.LittleEndian.Uint16(b[:2])
	b = b[2:]
	delta.DecorRows = make([]DecorRowDelta, decorCount)
	for i := 0; i < int(decorCount); i++ {
		if len(b) < 4 {
			return delta, ErrPayloadShort
		}
		rowIdx := binary.LittleEndian.Uint16(b[:2])
		spanCount := binary.LittleEndian.Uint16(b[2:4])
		b = b[4:]
		// Defensive bound: a sane pane is at most a few thousand rows
		// tall. A RowIdx beyond MaxDecorRowIdx indicates a corrupt or
		// hostile payload — reject rather than balloon client memory.
		if rowIdx > MaxDecorRowIdx {
			return delta, ErrInvalidSpan
		}
		spans := make([]CellSpan, spanCount)
		for s := 0; s < int(spanCount); s++ {
			if len(b) < 6 {
				return delta, ErrPayloadShort
			}
			startCol := binary.LittleEndian.Uint16(b[:2])
			textLen := binary.LittleEndian.Uint16(b[2:4])
			styleIndex := binary.LittleEndian.Uint16(b[4:6])
			b = b[6:]
			if len(b) < int(textLen) {
				return delta, ErrPayloadShort
			}
			text := string(b[:textLen])
			b = b[textLen:]
			if int(styleIndex) >= int(styleCount) {
				return delta, ErrStyleIndexOutOfRange
			}
			spans[s] = CellSpan{StartCol: startCol, Text: text, StyleIndex: styleIndex}
		}
		delta.DecorRows[i] = DecorRowDelta{RowIdx: rowIdx, Spans: spans}
	}
	if len(b) != 0 {
		return delta, ErrPayloadShort
	}
	return delta, nil
}

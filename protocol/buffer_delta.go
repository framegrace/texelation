package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// BufferDeltaFlags describes optional encoding tweaks.
type BufferDeltaFlags uint8

const (
	BufferDeltaNone BufferDeltaFlags = 0
)

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
type StyleEntry struct {
    AttrFlags uint16
    FgModel   ColorModel
    FgValue   uint32
    BgModel   ColorModel
    BgValue   uint32
}

const (
    AttrBold uint16 = 1 << iota
    AttrUnderline
    AttrReverse
    AttrBlink
    AttrDim
    AttrItalic
)

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

// BufferDelta is the payload sent by the server to update pane contents.
type BufferDelta struct {
	PaneID   [16]byte
	Revision uint32
	Flags    BufferDeltaFlags
	Styles   []StyleEntry
	Rows     []RowDelta
}

var (
	ErrBufferTooLarge = errors.New("protocol: buffer delta exceeds limits")
	errInvalidSpan    = errors.New("protocol: invalid span")
)

// EncodeBufferDelta serialises the delta into a compact binary representation.
func EncodeBufferDelta(delta BufferDelta) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 64))
	buf.Write(delta.PaneID[:])
	if err := binary.Write(buf, binary.LittleEndian, delta.Revision); err != nil {
		return nil, err
	}
	buf.WriteByte(byte(delta.Flags))

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
				return nil, errInvalidSpan
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
	if len(b) < 21 { // paneID(16)+revision(4)+flags(1)
		return delta, errPayloadShort
	}
	copy(delta.PaneID[:], b[:16])
	delta.Revision = binary.LittleEndian.Uint32(b[16:20])
	delta.Flags = BufferDeltaFlags(b[20])
	b = b[21:]

	if len(b) < 2 {
		return delta, errPayloadShort
	}
	styleCount := binary.LittleEndian.Uint16(b[:2])
	b = b[2:]
	delta.Styles = make([]StyleEntry, styleCount)
	for i := 0; i < int(styleCount); i++ {
		if len(b) < 12 { // attr(2) + fgModel(1)+fg(4)+bgModel(1)+bg(4)
			return delta, errPayloadShort
		}
		delta.Styles[i].AttrFlags = binary.LittleEndian.Uint16(b[:2])
		delta.Styles[i].FgModel = ColorModel(b[2])
		delta.Styles[i].FgValue = binary.LittleEndian.Uint32(b[3:7])
		delta.Styles[i].BgModel = ColorModel(b[7])
		delta.Styles[i].BgValue = binary.LittleEndian.Uint32(b[8:12])
		b = b[12:]
	}

	if len(b) < 2 {
		return delta, errPayloadShort
	}
	rowCount := binary.LittleEndian.Uint16(b[:2])
	b = b[2:]
	delta.Rows = make([]RowDelta, rowCount)
	for i := 0; i < int(rowCount); i++ {
		if len(b) < 4 {
			return delta, errPayloadShort
		}
		row := binary.LittleEndian.Uint16(b[:2])
		spanCount := binary.LittleEndian.Uint16(b[2:4])
		b = b[4:]
		spans := make([]CellSpan, spanCount)
		for s := 0; s < int(spanCount); s++ {
			if len(b) < 6 {
				return delta, errPayloadShort
			}
			startCol := binary.LittleEndian.Uint16(b[:2])
			textLen := binary.LittleEndian.Uint16(b[2:4])
			styleIndex := binary.LittleEndian.Uint16(b[4:6])
			b = b[6:]
			if len(b) < int(textLen) {
				return delta, errPayloadShort
			}
			text := string(b[:textLen])
			b = b[textLen:]
			spans[s] = CellSpan{StartCol: startCol, Text: text, StyleIndex: styleIndex}
		}
		delta.Rows[i] = RowDelta{Row: row, Spans: spans}
	}

	return delta, nil
}

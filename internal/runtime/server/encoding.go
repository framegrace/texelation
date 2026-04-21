// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"errors"
	"strings"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/protocol"
)

// maxStyleEntries is the maximum number of distinct styles a styleTable can
// hold before indexOf returns errStyleTableFull.  The wire format uses uint16
// indices, so 0xFFFF (65535) is the hard cap; we cap one below to keep
// uint16 arithmetic safe.
const maxStyleEntries = 0xFFFF

// errStyleTableFull is returned by indexOf when the style table has reached
// maxStyleEntries distinct entries. Callers should surface a partial result
// and log the condition.
var errStyleTableFull = errors.New("encode: style table full (>65534 distinct styles)")

// parserStyleKey is the deduplication key for parser.Cell styles.
type parserStyleKey struct {
	attrFlags uint16
	fgModel   protocol.ColorModel
	fgValue   uint32
	bgModel   protocol.ColorModel
	bgValue   uint32
}

// styleTable accumulates unique StyleEntry values and returns their indices.
type styleTable struct {
	index        map[parserStyleKey]uint16
	styleEntries []protocol.StyleEntry
}

func newStyleTable() *styleTable {
	return &styleTable{
		index: make(map[parserStyleKey]uint16),
	}
}

// indexOf returns the index in the shared style table for the style encoded by
// cell, adding a new entry if this style has not been seen before.
// Returns errStyleTableFull if the table already has maxStyleEntries distinct
// styles; the caller must short-circuit span building on that error to prevent
// silent uint16 wraparound in the wire format.
func (t *styleTable) indexOf(cell parser.Cell) (uint16, error) {
	attrFlags := uint16(0)
	if cell.Attr&parser.AttrBold != 0 {
		attrFlags |= protocol.AttrBold
	}
	if cell.Attr&parser.AttrUnderline != 0 {
		attrFlags |= protocol.AttrUnderline
	}
	if cell.Attr&parser.AttrReverse != 0 {
		attrFlags |= protocol.AttrReverse
	}
	if cell.Attr&parser.AttrDim != 0 {
		attrFlags |= protocol.AttrDim
	}
	if cell.Attr&parser.AttrItalic != 0 {
		attrFlags |= protocol.AttrItalic
	}

	fgModel, fgValue := parserColorToProto(cell.FG)
	bgModel, bgValue := parserColorToProto(cell.BG)

	key := parserStyleKey{
		attrFlags: attrFlags,
		fgModel:   fgModel,
		fgValue:   fgValue,
		bgModel:   bgModel,
		bgValue:   bgValue,
	}
	if idx, ok := t.index[key]; ok {
		return idx, nil
	}
	if len(t.styleEntries) >= maxStyleEntries {
		return 0, errStyleTableFull
	}
	idx := uint16(len(t.styleEntries))
	t.styleEntries = append(t.styleEntries, protocol.StyleEntry{
		AttrFlags: attrFlags,
		FgModel:   fgModel,
		FgValue:   fgValue,
		BgModel:   bgModel,
		BgValue:   bgValue,
	})
	t.index[key] = idx
	return idx, nil
}

// entries returns the accumulated style entries in insertion order.
func (t *styleTable) entries() []protocol.StyleEntry {
	return t.styleEntries
}

// parserColorToProto converts a parser.Color to the protocol (ColorModel, value) pair.
func parserColorToProto(c parser.Color) (protocol.ColorModel, uint32) {
	switch c.Mode {
	case parser.ColorModeDefault:
		return protocol.ColorModelDefault, 0
	case parser.ColorModeStandard:
		return protocol.ColorModelANSI16, uint32(c.Value)
	case parser.ColorMode256:
		return protocol.ColorModelANSI256, uint32(c.Value)
	case parser.ColorModeRGB:
		return protocol.ColorModelRGB, (uint32(c.R) << 16) | (uint32(c.G) << 8) | uint32(c.B)
	default:
		return protocol.ColorModelDefault, 0
	}
}

// encodeParserCellsToSpans groups consecutive cells with the same style into
// CellSpan values. The caller owns the returned slice.
// Returns errStyleTableFull if the style table overflows during encoding; in
// that case the returned spans cover only the cells processed up to the error.
func encodeParserCellsToSpans(cells []parser.Cell, t *styleTable) ([]protocol.CellSpan, error) {
	if len(cells) == 0 {
		return nil, nil
	}
	var spans []protocol.CellSpan
	var builders []*strings.Builder

	for x, cell := range cells {
		idx, err := t.indexOf(cell)
		if err != nil {
			// Finalise text for spans built so far and return partial result.
			for i := range spans {
				spans[i].Text = builders[i].String()
			}
			return spans, err
		}
		if len(spans) == 0 || spans[len(spans)-1].StyleIndex != idx {
			spans = append(spans, protocol.CellSpan{StartCol: uint16(x), StyleIndex: idx})
			builders = append(builders, &strings.Builder{})
		}
		builders[len(builders)-1].WriteRune(cell.Rune)
	}
	for i := range spans {
		spans[i].Text = builders[i].String()
	}
	return spans, nil
}

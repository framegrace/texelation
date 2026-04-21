// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"strings"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/protocol"
)

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
func (t *styleTable) indexOf(cell parser.Cell) uint16 {
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
		return idx
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
	return idx
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
func encodeParserCellsToSpans(cells []parser.Cell, t *styleTable) []protocol.CellSpan {
	if len(cells) == 0 {
		return nil
	}
	var spans []protocol.CellSpan
	var builders []*strings.Builder

	for x, cell := range cells {
		idx := t.indexOf(cell)
		if len(spans) == 0 || spans[len(spans)-1].StyleIndex != idx {
			spans = append(spans, protocol.CellSpan{StartCol: uint16(x), StyleIndex: idx})
			builders = append(builders, &strings.Builder{})
		}
		builders[len(builders)-1].WriteRune(cell.Rune)
	}
	for i := range spans {
		spans[i].Text = builders[i].String()
	}
	return spans
}

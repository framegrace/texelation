// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/page.go
// Summary: Page format for disk-based terminal history storage.
//
// Page Format (64KB target):
//   Header (64 bytes):
//     Magic: "TXPAGE01" (8 bytes)
//     Version: uint32 (4 bytes) - value 1
//     PageID: uint64 (8 bytes)
//     State: uint8 (1 byte) - LIVE=0, WARM=1, FROZEN=2
//     Flags: uint8 (1 byte) - ENCRYPTED=0x01, COMPRESSED=0x02
//     LineCount: uint32 (4 bytes)
//     FirstGlobalIdx: uint64 (8 bytes) - first line's global index
//     FirstTimestamp: int64 (8 bytes) - UnixNano
//     LastTimestamp: int64 (8 bytes) - UnixNano
//     UncompressedSize: uint32 (4 bytes)
//     CompressedSize: uint32 (4 bytes) - 0 if not compressed
//     Reserved: [6]byte
//
//   Line Index (LineCount * 16 bytes):
//     Per-line: Offset(4) + Timestamp(8) + Flags(2) + Reserved(2)
//
//   Line Data (variable):
//     Per-line: CellCount(4) + FixedWidth(4) + Cells(CellCount * 16)

package parser

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

// Page format constants
const (
	PageMagic      = "TXPAGE01"
	PageVersion    = 1
	PageHeaderSize = 64
	LineIndexSize  = 16 // Per-line index entry size
	TargetPageSize = 64 * 1024
	PageCellSize   = 16 // 4 bytes rune + 4 bytes FG + 4 bytes BG + 4 bytes attrs
)

// PageState represents the lifecycle state of a page.
type PageState uint8

const (
	PageStateLive   PageState = 0 // Currently being written
	PageStateWarm   PageState = 1 // Complete, uncompressed, in pages/
	PageStateFrozen PageState = 2 // Compressed, in archive/
)

// PageFlags represent optional features applied to a page.
type PageFlags uint8

const (
	PageFlagEncrypted  PageFlags = 1 << 0 // Page data is encrypted
	PageFlagCompressed PageFlags = 1 << 1 // Page data is compressed
)

// LineFlags represent metadata about individual lines.
type LineFlags uint16

const (
	LineFlagIsCommand  LineFlags = 1 << 0 // Line is a shell command (OSC 133)
	LineFlagFixedWidth LineFlags = 1 << 1 // Line has FixedWidth > 0
)

// --- Cell Encoding ---

// encodeCell writes a Cell to the buffer (PageCellSize bytes).
func encodeCell(cell Cell, buf []byte) {
	// Rune (4 bytes)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(cell.Rune))

	// Foreground: mode(1) + value(4)
	buf[4] = byte(cell.FG.Mode)
	binary.LittleEndian.PutUint32(buf[5:9], encodeColorValue(cell.FG))

	// Background: mode(1) + value(4)
	buf[9] = byte(cell.BG.Mode)
	binary.LittleEndian.PutUint32(buf[10:14], encodeColorValue(cell.BG))

	// Attributes (2 bytes)
	binary.LittleEndian.PutUint16(buf[14:16], uint16(cell.Attr))
}

// decodeCell reads a Cell from the buffer.
func decodeCell(buf []byte) Cell {
	cell := Cell{}

	// Rune
	cell.Rune = rune(binary.LittleEndian.Uint32(buf[0:4]))

	// Foreground
	fgMode := ColorMode(buf[4])
	cell.FG = decodeColorFromValue(fgMode, binary.LittleEndian.Uint32(buf[5:9]))

	// Background
	bgMode := ColorMode(buf[9])
	cell.BG = decodeColorFromValue(bgMode, binary.LittleEndian.Uint32(buf[10:14]))

	// Attributes
	cell.Attr = Attribute(binary.LittleEndian.Uint16(buf[14:16]))

	return cell
}

// PageHeader is the 64-byte header at the start of each page file.
type PageHeader struct {
	Magic            [8]byte   // "TXPAGE01"
	Version          uint32    // Format version (1)
	PageID           uint64    // Sequential page number
	State            PageState // LIVE/WARM/FROZEN
	Flags            PageFlags // Compression/encryption flags
	LineCount        uint32    // Number of lines in this page
	FirstGlobalIdx   uint64    // Global index of first line in page
	FirstTimestamp   int64     // UnixNano of first line
	LastTimestamp    int64     // UnixNano of last line
	UncompressedSize uint32    // Size of line data (uncompressed)
	CompressedSize   uint32    // Size after compression (0 if uncompressed)
	Reserved         [6]byte   // Future use
}

// LineIndexEntry is the 16-byte metadata for each line in the page.
type LineIndexEntry struct {
	Offset    uint32    // Byte offset into line data section
	Timestamp int64     // UnixNano when line was written
	Flags     LineFlags // Line metadata flags
	Reserved  uint16    // Future use
}

// Page represents a complete page with header, index, and line data.
type Page struct {
	Header PageHeader
	Index  []LineIndexEntry
	Lines  []*LogicalLine

	// Cached line data for efficient size calculation
	lineDataCache []byte
}

// NewPage creates a new empty page.
func NewPage(pageID uint64, firstGlobalIdx uint64) *Page {
	p := &Page{
		Header: PageHeader{
			Version:        PageVersion,
			PageID:         pageID,
			State:          PageStateLive,
			FirstGlobalIdx: firstGlobalIdx,
		},
		Index: make([]LineIndexEntry, 0),
		Lines: make([]*LogicalLine, 0),
	}
	copy(p.Header.Magic[:], PageMagic)
	return p
}

// AddLine attempts to add a line to the page.
// Returns true if the line was added, false if adding it would exceed target size.
// The timestamp defaults to time.Now() if zero.
func (p *Page) AddLine(line *LogicalLine, timestamp time.Time, flags LineFlags) bool {
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	// Calculate size if we add this line
	lineData := encodeLineData(line)
	newSize := p.calculateSizeWithLine(lineData)

	if newSize > TargetPageSize && p.Header.LineCount > 0 {
		// Would exceed target size and we already have lines
		return false
	}

	// Add line to page
	ts := timestamp.UnixNano()

	// Calculate offset into line data
	var offset uint32
	if len(p.lineDataCache) > 0 {
		offset = uint32(len(p.lineDataCache))
	}

	// Set flags
	if line.FixedWidth > 0 {
		flags |= LineFlagFixedWidth
	}

	entry := LineIndexEntry{
		Offset:    offset,
		Timestamp: ts,
		Flags:     flags,
	}

	p.Index = append(p.Index, entry)
	p.Lines = append(p.Lines, line)
	p.lineDataCache = append(p.lineDataCache, lineData...)

	// Update header
	p.Header.LineCount++
	p.Header.LastTimestamp = ts
	p.Header.UncompressedSize = uint32(len(p.lineDataCache))

	if p.Header.LineCount == 1 {
		p.Header.FirstTimestamp = ts
	}

	return true
}

// Size returns the current serialized size of the page in bytes.
func (p *Page) Size() int {
	return PageHeaderSize +
		int(p.Header.LineCount)*LineIndexSize +
		len(p.lineDataCache)
}

// calculateSizeWithLine returns what the size would be if we added a line.
func (p *Page) calculateSizeWithLine(lineData []byte) int {
	return PageHeaderSize +
		(int(p.Header.LineCount)+1)*LineIndexSize +
		len(p.lineDataCache) + len(lineData)
}

// IsFull returns true if adding another line would exceed target page size.
// This is a conservative estimate for an average-sized line.
func (p *Page) IsFull(line *LogicalLine) bool {
	lineData := encodeLineData(line)
	return p.calculateSizeWithLine(lineData) > TargetPageSize
}

// WriteTo serializes the page to a writer.
func (p *Page) WriteTo(w io.Writer) (int64, error) {
	var written int64

	// Write header
	n, err := p.writeHeader(w)
	written += n
	if err != nil {
		return written, fmt.Errorf("failed to write page header: %w", err)
	}

	// Write line index
	n, err = p.writeIndex(w)
	written += n
	if err != nil {
		return written, fmt.Errorf("failed to write line index: %w", err)
	}

	// Write line data
	n, err = p.writeLineData(w)
	written += n
	if err != nil {
		return written, fmt.Errorf("failed to write line data: %w", err)
	}

	return written, nil
}

// writeHeader writes the 64-byte header.
func (p *Page) writeHeader(w io.Writer) (int64, error) {
	buf := make([]byte, PageHeaderSize)

	// Magic (8 bytes)
	copy(buf[0:8], p.Header.Magic[:])

	// Version (4 bytes)
	binary.LittleEndian.PutUint32(buf[8:12], p.Header.Version)

	// PageID (8 bytes)
	binary.LittleEndian.PutUint64(buf[12:20], p.Header.PageID)

	// State (1 byte)
	buf[20] = byte(p.Header.State)

	// Flags (1 byte)
	buf[21] = byte(p.Header.Flags)

	// LineCount (4 bytes)
	binary.LittleEndian.PutUint32(buf[22:26], p.Header.LineCount)

	// FirstGlobalIdx (8 bytes)
	binary.LittleEndian.PutUint64(buf[26:34], p.Header.FirstGlobalIdx)

	// FirstTimestamp (8 bytes)
	binary.LittleEndian.PutUint64(buf[34:42], uint64(p.Header.FirstTimestamp))

	// LastTimestamp (8 bytes)
	binary.LittleEndian.PutUint64(buf[42:50], uint64(p.Header.LastTimestamp))

	// UncompressedSize (4 bytes)
	binary.LittleEndian.PutUint32(buf[50:54], p.Header.UncompressedSize)

	// CompressedSize (4 bytes)
	binary.LittleEndian.PutUint32(buf[54:58], p.Header.CompressedSize)

	// Reserved (6 bytes) - leave as zeros

	n, err := w.Write(buf)
	return int64(n), err
}

// writeIndex writes the line index entries.
func (p *Page) writeIndex(w io.Writer) (int64, error) {
	var written int64
	buf := make([]byte, LineIndexSize)

	for _, entry := range p.Index {
		// Offset (4 bytes)
		binary.LittleEndian.PutUint32(buf[0:4], entry.Offset)

		// Timestamp (8 bytes)
		binary.LittleEndian.PutUint64(buf[4:12], uint64(entry.Timestamp))

		// Flags (2 bytes)
		binary.LittleEndian.PutUint16(buf[12:14], uint16(entry.Flags))

		// Reserved (2 bytes) - leave as zeros
		buf[14] = 0
		buf[15] = 0

		n, err := w.Write(buf)
		written += int64(n)
		if err != nil {
			return written, err
		}
	}

	return written, nil
}

// writeLineData writes the encoded line data.
func (p *Page) writeLineData(w io.Writer) (int64, error) {
	if len(p.lineDataCache) == 0 {
		return 0, nil
	}
	n, err := w.Write(p.lineDataCache)
	return int64(n), err
}

// ReadFrom deserializes a page from a reader.
func (p *Page) ReadFrom(r io.Reader) (int64, error) {
	var read int64

	// Read header
	n, err := p.readHeader(r)
	read += n
	if err != nil {
		return read, fmt.Errorf("failed to read page header: %w", err)
	}

	// Read line index
	n, err = p.readIndex(r)
	read += n
	if err != nil {
		return read, fmt.Errorf("failed to read line index: %w", err)
	}

	// Read line data
	n, err = p.readLineData(r)
	read += n
	if err != nil {
		return read, fmt.Errorf("failed to read line data: %w", err)
	}

	return read, nil
}

// readHeader reads and validates the 64-byte header.
func (p *Page) readHeader(r io.Reader) (int64, error) {
	buf := make([]byte, PageHeaderSize)
	n, err := io.ReadFull(r, buf)
	if err != nil {
		return int64(n), err
	}

	// Magic
	copy(p.Header.Magic[:], buf[0:8])
	if string(p.Header.Magic[:]) != PageMagic {
		return int64(n), fmt.Errorf("invalid page magic: %q", p.Header.Magic[:])
	}

	// Version
	p.Header.Version = binary.LittleEndian.Uint32(buf[8:12])
	if p.Header.Version != PageVersion {
		return int64(n), fmt.Errorf("unsupported page version: %d", p.Header.Version)
	}

	// PageID
	p.Header.PageID = binary.LittleEndian.Uint64(buf[12:20])

	// State
	p.Header.State = PageState(buf[20])

	// Flags
	p.Header.Flags = PageFlags(buf[21])

	// LineCount
	p.Header.LineCount = binary.LittleEndian.Uint32(buf[22:26])

	// FirstGlobalIdx
	p.Header.FirstGlobalIdx = binary.LittleEndian.Uint64(buf[26:34])

	// FirstTimestamp
	p.Header.FirstTimestamp = int64(binary.LittleEndian.Uint64(buf[34:42]))

	// LastTimestamp
	p.Header.LastTimestamp = int64(binary.LittleEndian.Uint64(buf[42:50]))

	// UncompressedSize
	p.Header.UncompressedSize = binary.LittleEndian.Uint32(buf[50:54])

	// CompressedSize
	p.Header.CompressedSize = binary.LittleEndian.Uint32(buf[54:58])

	return int64(n), nil
}

// readIndex reads the line index entries.
func (p *Page) readIndex(r io.Reader) (int64, error) {
	var read int64
	p.Index = make([]LineIndexEntry, p.Header.LineCount)

	buf := make([]byte, LineIndexSize)
	for i := uint32(0); i < p.Header.LineCount; i++ {
		n, err := io.ReadFull(r, buf)
		read += int64(n)
		if err != nil {
			return read, fmt.Errorf("failed to read index entry %d: %w", i, err)
		}

		p.Index[i] = LineIndexEntry{
			Offset:    binary.LittleEndian.Uint32(buf[0:4]),
			Timestamp: int64(binary.LittleEndian.Uint64(buf[4:12])),
			Flags:     LineFlags(binary.LittleEndian.Uint16(buf[12:14])),
			Reserved:  binary.LittleEndian.Uint16(buf[14:16]),
		}
	}

	return read, nil
}

// readLineData reads and decodes all line data.
func (p *Page) readLineData(r io.Reader) (int64, error) {
	if p.Header.UncompressedSize == 0 {
		p.Lines = make([]*LogicalLine, 0)
		return 0, nil
	}

	// Read all line data at once
	p.lineDataCache = make([]byte, p.Header.UncompressedSize)
	n, err := io.ReadFull(r, p.lineDataCache)
	if err != nil {
		return int64(n), err
	}

	// Decode each line using index offsets
	p.Lines = make([]*LogicalLine, p.Header.LineCount)
	for i := uint32(0); i < p.Header.LineCount; i++ {
		offset := p.Index[i].Offset

		// Determine end offset
		var endOffset uint32
		if i+1 < p.Header.LineCount {
			endOffset = p.Index[i+1].Offset
		} else {
			endOffset = uint32(len(p.lineDataCache))
		}

		if offset > uint32(len(p.lineDataCache)) || endOffset > uint32(len(p.lineDataCache)) {
			return int64(n), fmt.Errorf("line %d offset out of bounds", i)
		}

		line, err := decodeLineData(p.lineDataCache[offset:endOffset])
		if err != nil {
			return int64(n), fmt.Errorf("failed to decode line %d: %w", i, err)
		}
		p.Lines[i] = line
	}

	return int64(n), nil
}

// GetLine returns a line by its index within this page.
// Returns nil if index is out of bounds.
func (p *Page) GetLine(index int) *LogicalLine {
	if index < 0 || index >= len(p.Lines) {
		return nil
	}
	return p.Lines[index]
}

// GetTimestamp returns the timestamp for a line by its index within this page.
func (p *Page) GetTimestamp(index int) time.Time {
	if index < 0 || index >= len(p.Index) {
		return time.Time{}
	}
	return time.Unix(0, p.Index[index].Timestamp)
}

// UpdateLine updates an existing line and its timestamp by index within this page.
// Returns error if the index is out of bounds.
// This method invalidates the line data cache and recalculates offsets.
func (p *Page) UpdateLine(index int, line *LogicalLine, timestamp time.Time) error {
	if index < 0 || index >= len(p.Lines) {
		return fmt.Errorf("line index %d out of bounds (0-%d)", index, len(p.Lines)-1)
	}
	if line == nil {
		return fmt.Errorf("line cannot be nil")
	}

	// Update the line
	p.Lines[index] = line

	// Update the timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	p.Index[index].Timestamp = timestamp.UnixNano()

	// Update flags for fixed-width
	if line.FixedWidth > 0 {
		p.Index[index].Flags |= LineFlagFixedWidth
	} else {
		p.Index[index].Flags &^= LineFlagFixedWidth
	}

	// Rebuild the line data cache since line sizes may have changed
	p.rebuildLineDataCache()

	return nil
}

// rebuildLineDataCache reconstructs the lineDataCache from the current Lines.
// This is called after UpdateLine to ensure offsets and data are consistent.
func (p *Page) rebuildLineDataCache() {
	p.lineDataCache = nil

	for i, line := range p.Lines {
		lineData := encodeLineData(line)

		// Update offset in index
		if i < len(p.Index) {
			p.Index[i].Offset = uint32(len(p.lineDataCache))
		}

		p.lineDataCache = append(p.lineDataCache, lineData...)
	}

	// Update header
	p.Header.UncompressedSize = uint32(len(p.lineDataCache))

	// Update timestamps if needed
	if len(p.Index) > 0 {
		p.Header.FirstTimestamp = p.Index[0].Timestamp
		p.Header.LastTimestamp = p.Index[len(p.Index)-1].Timestamp
	}
}

// encodeLineData serializes a LogicalLine to bytes (v2 format).
// Format: Flags(1) + CellCount(4) + FixedWidth(4) + Cells(N*16) + [OverlayWidth(4) + OverlayCellCount(4) + OverlayCells(M*16)]
func encodeLineData(line *LogicalLine) []byte {
	var flags byte
	if line.Overlay != nil {
		flags |= 0x01
	}
	if line.Synthetic {
		flags |= 0x02
	}

	cellCount := uint32(len(line.Cells))
	size := 1 + 4 + 4 + int(cellCount)*PageCellSize
	if flags&0x01 != 0 {
		size += 4 + 4 + len(line.Overlay)*PageCellSize
	}

	buf := make([]byte, size)
	offset := 0

	buf[offset] = flags
	offset++

	binary.LittleEndian.PutUint32(buf[offset:offset+4], cellCount)
	offset += 4

	binary.LittleEndian.PutUint32(buf[offset:offset+4], uint32(line.FixedWidth))
	offset += 4

	cellBuf := make([]byte, PageCellSize)
	for _, cell := range line.Cells {
		encodeCell(cell, cellBuf)
		copy(buf[offset:offset+PageCellSize], cellBuf)
		offset += PageCellSize
	}

	if flags&0x01 != 0 {
		binary.LittleEndian.PutUint32(buf[offset:offset+4], uint32(line.OverlayWidth))
		offset += 4

		overlayCellCount := uint32(len(line.Overlay))
		binary.LittleEndian.PutUint32(buf[offset:offset+4], overlayCellCount)
		offset += 4

		for _, cell := range line.Overlay {
			encodeCell(cell, cellBuf)
			copy(buf[offset:offset+PageCellSize], cellBuf)
			offset += PageCellSize
		}
	}

	return buf
}

// decodeLineData deserializes bytes to a LogicalLine.
// Tries v2 format first, falls back to v1 for backward compatibility.
func decodeLineData(data []byte) (*LogicalLine, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("line data too short: %d bytes", len(data))
	}

	if line, err := decodeLineDataV2(data); err == nil {
		return line, nil
	}

	return decodeLineDataV1(data)
}

// decodeLineDataV1 decodes the original format: CellCount(4) + FixedWidth(4) + Cells
func decodeLineDataV1(data []byte) (*LogicalLine, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("v1 line data too short: %d bytes", len(data))
	}

	cellCount := binary.LittleEndian.Uint32(data[0:4])
	fixedWidth := binary.LittleEndian.Uint32(data[4:8])

	expectedSize := 8 + int(cellCount)*PageCellSize
	if len(data) != expectedSize {
		return nil, fmt.Errorf("v1 line data size mismatch: expected %d, got %d", expectedSize, len(data))
	}

	cells := make([]Cell, cellCount)
	offset := 8
	for i := uint32(0); i < cellCount; i++ {
		cells[i] = decodeCell(data[offset : offset+PageCellSize])
		offset += PageCellSize
	}

	return &LogicalLine{
		Cells:      cells,
		FixedWidth: int(fixedWidth),
	}, nil
}

// decodeLineDataV2 decodes v2 format: Flags(1) + CellCount(4) + FixedWidth(4) + Cells + [Overlay]
func decodeLineDataV2(data []byte) (*LogicalLine, error) {
	if len(data) < 9 {
		return nil, fmt.Errorf("v2 line data too short: %d bytes", len(data))
	}

	flags := data[0]
	if flags & ^byte(0x03) != 0 {
		return nil, fmt.Errorf("invalid v2 flags: 0x%02x", flags)
	}

	offset := 1
	cellCount := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	fixedWidth := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	cellsEnd := offset + int(cellCount)*PageCellSize
	if len(data) < cellsEnd {
		return nil, fmt.Errorf("v2 cells truncated")
	}

	cells := make([]Cell, cellCount)
	for i := uint32(0); i < cellCount; i++ {
		cells[i] = decodeCell(data[offset : offset+PageCellSize])
		offset += PageCellSize
	}

	line := &LogicalLine{
		Cells:      cells,
		FixedWidth: int(fixedWidth),
		Synthetic:  flags&0x02 != 0,
	}

	if flags&0x01 != 0 {
		if len(data) < offset+8 {
			return nil, fmt.Errorf("v2 overlay header truncated")
		}

		line.OverlayWidth = int(binary.LittleEndian.Uint32(data[offset : offset+4]))
		offset += 4

		overlayCellCount := binary.LittleEndian.Uint32(data[offset : offset+4])
		offset += 4

		overlayEnd := offset + int(overlayCellCount)*PageCellSize
		if len(data) < overlayEnd {
			return nil, fmt.Errorf("v2 overlay cells truncated")
		}

		line.Overlay = make([]Cell, overlayCellCount)
		for i := uint32(0); i < overlayCellCount; i++ {
			line.Overlay[i] = decodeCell(data[offset : offset+PageCellSize])
			offset += PageCellSize
		}
	}

	if offset != len(data) {
		return nil, fmt.Errorf("v2 size mismatch: consumed %d, data has %d", offset, len(data))
	}

	return line, nil
}

// lineDataSize calculates the serialized size of a LogicalLine (v2 format).
func lineDataSize(line *LogicalLine) int {
	size := 1 + 4 + 4 + len(line.Cells)*PageCellSize
	if line.Overlay != nil {
		size += 4 + 4 + len(line.Overlay)*PageCellSize
	}
	return size
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/history_indexed.go
// Summary: Indexed history file format for efficient random access.
// Usage: Supports on-demand loading of history lines without reading entire file.
//
// File Format (v2):
//
//   ┌──────────────────────────────────────────┐
//   │ Header (32 bytes)                        │
//   │   Magic: "TXHIST02" (8 bytes)            │
//   │   Flags: uint32 (compressed, encrypted)  │
//   │   LineCount: uint64                      │
//   │   IndexOffset: uint64 (offset to index)  │
//   │   Reserved: 4 bytes                      │
//   ├──────────────────────────────────────────┤
//   │ Line Data (variable)                     │
//   │   Each line:                             │
//   │     CellCount: uint32                    │
//   │     Cells: CellCount * 14 bytes each     │
//   │       Rune: int32                        │
//   │       FG: uint32 (Color)                 │
//   │       BG: uint32 (Color)                 │
//   │       Attr: uint16                       │
//   ├──────────────────────────────────────────┤
//   │ Index (8 bytes per line)                 │
//   │   Offset[0]: uint64 (offset to line 0)   │
//   │   Offset[1]: uint64 (offset to line 1)   │
//   │   ...                                    │
//   └──────────────────────────────────────────┘

package parser

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"
)

const (
	indexedHistoryMagic   = "TXHIST02"
	indexedHeaderSize     = 32
	indexedColorSize      = 5              // Mode(1) + Value(1) + R(1) + G(1) + B(1)
	indexedCellSize       = 4 + 5 + 5 + 2  // rune(4) + fg(5) + bg(5) + attr(2) = 16
	indexedIndexEntrySize = 8              // uint64 offset
)

// IndexedHistoryFile provides random access to persisted history lines.
// It keeps the file open and uses an in-memory index for fast seeking.
type IndexedHistoryFile struct {
	file      *os.File
	path      string
	lineCount int64
	index     []int64 // Offset of each line in file
	mu        sync.RWMutex
}

// OpenIndexedHistory opens an existing indexed history file for reading.
// Returns nil if file doesn't exist or is in old format.
func OpenIndexedHistory(path string) (*IndexedHistoryFile, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	// Read header
	header := make([]byte, indexedHeaderSize)
	if _, err := io.ReadFull(file, header); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	// Check magic
	if string(header[:8]) != indexedHistoryMagic {
		file.Close()
		return nil, nil // Old format, not an error
	}

	// Parse header
	// flags := binary.LittleEndian.Uint32(header[8:12]) // For future use
	lineCount := binary.LittleEndian.Uint64(header[12:20])
	indexOffset := binary.LittleEndian.Uint64(header[20:28])

	// Read index
	if _, err := file.Seek(int64(indexOffset), io.SeekStart); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to seek to index: %w", err)
	}

	index := make([]int64, lineCount)
	for i := int64(0); i < int64(lineCount); i++ {
		var offset uint64
		if err := binary.Read(file, binary.LittleEndian, &offset); err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to read index entry %d: %w", i, err)
		}
		index[i] = int64(offset)
	}

	return &IndexedHistoryFile{
		file:      file,
		path:      path,
		lineCount: int64(lineCount),
		index:     index,
	}, nil
}

// LineCount returns the total number of lines in the file.
func (h *IndexedHistoryFile) LineCount() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lineCount
}

// ReadLine reads a single logical line at the given index.
func (h *IndexedHistoryFile) ReadLine(index int64) (*LogicalLine, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if index < 0 || index >= h.lineCount {
		return nil, fmt.Errorf("line index %d out of range [0, %d)", index, h.lineCount)
	}

	// Seek to line
	offset := h.index[index]
	if _, err := h.file.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek to line %d: %w", index, err)
	}

	// Read cell count
	var cellCount uint32
	if err := binary.Read(h.file, binary.LittleEndian, &cellCount); err != nil {
		return nil, fmt.Errorf("failed to read cell count: %w", err)
	}

	// Read cells
	cells := make([]Cell, cellCount)
	for i := uint32(0); i < cellCount; i++ {
		cell, err := h.readCell()
		if err != nil {
			return nil, fmt.Errorf("failed to read cell %d: %w", i, err)
		}
		cells[i] = cell
	}

	return NewLogicalLineFromCells(cells), nil
}

// ReadLineRange reads multiple lines efficiently.
// Returns lines in chronological order (startIdx first).
func (h *IndexedHistoryFile) ReadLineRange(startIdx, endIdx int64) ([]*LogicalLine, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx > h.lineCount {
		endIdx = h.lineCount
	}
	if startIdx >= endIdx {
		return nil, nil
	}

	count := endIdx - startIdx
	lines := make([]*LogicalLine, count)

	// Seek to first line
	offset := h.index[startIdx]
	if _, err := h.file.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek to line %d: %w", startIdx, err)
	}

	// Read lines sequentially (more efficient than seeking to each)
	for i := int64(0); i < count; i++ {
		var cellCount uint32
		if err := binary.Read(h.file, binary.LittleEndian, &cellCount); err != nil {
			return nil, fmt.Errorf("failed to read cell count for line %d: %w", startIdx+i, err)
		}

		cells := make([]Cell, cellCount)
		for j := uint32(0); j < cellCount; j++ {
			cell, err := h.readCell()
			if err != nil {
				return nil, fmt.Errorf("failed to read cell: %w", err)
			}
			cells[j] = cell
		}

		lines[i] = NewLogicalLineFromCells(cells)
	}

	return lines, nil
}

func (h *IndexedHistoryFile) readCell() (Cell, error) {
	var buf [indexedCellSize]byte
	if _, err := io.ReadFull(h.file, buf[:]); err != nil {
		return Cell{}, err
	}

	return Cell{
		Rune: rune(binary.LittleEndian.Uint32(buf[0:4])),
		FG: Color{
			Mode:  ColorMode(buf[4]),
			Value: buf[5],
			R:     buf[6],
			G:     buf[7],
			B:     buf[8],
		},
		BG: Color{
			Mode:  ColorMode(buf[9]),
			Value: buf[10],
			R:     buf[11],
			G:     buf[12],
			B:     buf[13],
		},
		Attr: Attribute(binary.LittleEndian.Uint16(buf[14:16])),
	}, nil
}

// Close closes the history file.
func (h *IndexedHistoryFile) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.file != nil {
		err := h.file.Close()
		h.file = nil
		return err
	}
	return nil
}

// IndexedHistoryWriter writes history in the indexed format.
type IndexedHistoryWriter struct {
	file    *os.File
	path    string
	offsets []int64 // Track offset of each line
	pos     int64   // Current write position
	mu      sync.Mutex
}

// NewIndexedHistoryWriter creates a new writer for indexed history.
func NewIndexedHistoryWriter(path string) (*IndexedHistoryWriter, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	w := &IndexedHistoryWriter{
		file:    file,
		path:    path,
		offsets: make([]int64, 0, 1000),
		pos:     indexedHeaderSize, // Skip header, write it at the end
	}

	// Seek past header (we'll write it when closing)
	if _, err := file.Seek(indexedHeaderSize, io.SeekStart); err != nil {
		file.Close()
		return nil, err
	}

	return w, nil
}

// WriteLine writes a logical line and records its offset.
func (w *IndexedHistoryWriter) WriteLine(line *LogicalLine) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Record offset
	w.offsets = append(w.offsets, w.pos)

	// Write cell count
	cellCount := uint32(len(line.Cells))
	if err := binary.Write(w.file, binary.LittleEndian, cellCount); err != nil {
		return err
	}
	w.pos += 4

	// Write cells
	for _, cell := range line.Cells {
		if err := w.writeCell(cell); err != nil {
			return err
		}
		w.pos += indexedCellSize
	}

	return nil
}

// WriteLines writes multiple logical lines.
func (w *IndexedHistoryWriter) WriteLines(lines []*LogicalLine) error {
	for _, line := range lines {
		if err := w.WriteLine(line); err != nil {
			return err
		}
	}
	return nil
}

func (w *IndexedHistoryWriter) writeCell(cell Cell) error {
	var buf [indexedCellSize]byte
	binary.LittleEndian.PutUint32(buf[0:4], uint32(cell.Rune))
	// FG color
	buf[4] = byte(cell.FG.Mode)
	buf[5] = cell.FG.Value
	buf[6] = cell.FG.R
	buf[7] = cell.FG.G
	buf[8] = cell.FG.B
	// BG color
	buf[9] = byte(cell.BG.Mode)
	buf[10] = cell.BG.Value
	buf[11] = cell.BG.R
	buf[12] = cell.BG.G
	buf[13] = cell.BG.B
	// Attr
	binary.LittleEndian.PutUint16(buf[14:16], uint16(cell.Attr))
	_, err := w.file.Write(buf[:])
	return err
}

// Close writes the index and header, then closes the file.
func (w *IndexedHistoryWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return nil
	}

	// Record index offset
	indexOffset := w.pos

	// Write index
	for _, offset := range w.offsets {
		if err := binary.Write(w.file, binary.LittleEndian, uint64(offset)); err != nil {
			w.file.Close()
			return fmt.Errorf("failed to write index: %w", err)
		}
	}

	// Seek to start and write header
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		w.file.Close()
		return fmt.Errorf("failed to seek to header: %w", err)
	}

	header := make([]byte, indexedHeaderSize)
	copy(header[0:8], indexedHistoryMagic)
	binary.LittleEndian.PutUint32(header[8:12], 0)                      // flags
	binary.LittleEndian.PutUint64(header[12:20], uint64(len(w.offsets))) // line count
	binary.LittleEndian.PutUint64(header[20:28], uint64(indexOffset))   // index offset

	if _, err := w.file.Write(header); err != nil {
		w.file.Close()
		return fmt.Errorf("failed to write header: %w", err)
	}

	err := w.file.Close()
	w.file = nil
	return err
}

// LineCount returns the number of lines written so far.
func (w *IndexedHistoryWriter) LineCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.offsets)
}

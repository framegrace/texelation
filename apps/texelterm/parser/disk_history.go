// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/disk_history.go
// Summary: Indexed disk storage for scrollback history with O(1) random access.
// Usage: Provides persistent storage layer for three-level scrollback architecture.

package parser

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"
)

const (
	// TXHIST02 format constants
	diskHistoryMagic   = "TXHIST02"
	diskHistoryVersion = 1
	diskHeaderSize     = 32 // magic(8) + version(4) + flags(4) + lineCount(8) + indexOffset(8)
	diskCellSize       = 16 // rune(4) + fg(5) + bg(5) + attr(2)
)

// DiskHistoryConfig holds configuration for disk history storage.
type DiskHistoryConfig struct {
	// Path is the file path for the history file.
	Path string

	// SyncWrites forces fsync after each write (slower but safer).
	SyncWrites bool

	// IndexFlushInterval is how many lines to write before updating the index.
	// 0 means only update index on close.
	IndexFlushInterval int
}

// DefaultDiskHistoryConfig returns sensible defaults.
func DefaultDiskHistoryConfig(path string) DiskHistoryConfig {
	return DiskHistoryConfig{
		Path:               path,
		SyncWrites:         false,
		IndexFlushInterval: 0, // Only on close
	}
}

// DiskHistory provides indexed disk storage for logical lines.
// It supports incremental appending and O(1) random access reads.
//
// File format (TXHIST02):
//
//	Header (32 bytes):
//	  Magic: "TXHIST02" (8 bytes)
//	  Version: uint32 (4 bytes)
//	  Flags: uint32 (4 bytes)
//	  LineCount: uint64 (8 bytes)
//	  IndexOffset: uint64 (8 bytes)
//
//	Line Data (variable, repeated):
//	  CellCount: uint32 (4 bytes)
//	  Cells: CellCount * 16 bytes each
//
//	Index (at IndexOffset, written on close):
//	  Offset[0]: uint64 (file offset of line 0)
//	  Offset[1]: uint64 (file offset of line 1)
//	  ...
type DiskHistory struct {
	config DiskHistoryConfig

	file   *os.File
	writer *bufio.Writer

	// In-memory index of line offsets (built during write, loaded on read)
	index []uint64

	// Current write position (where next line will be written)
	writeOffset uint64

	// Number of lines written
	lineCount uint64

	// Whether we're in write mode (vs read-only)
	writeMode bool

	mu sync.RWMutex
}

// OpenDiskHistory opens an existing history file for reading.
// Returns nil, nil if file doesn't exist.
func OpenDiskHistory(path string) (*DiskHistory, error) {
	// Check if file exists
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if info.Size() < diskHeaderSize {
		// File too small, treat as non-existent
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	dh := &DiskHistory{
		config:    DiskHistoryConfig{Path: path},
		file:      file,
		writeMode: false,
	}

	// Read and validate header
	if err := dh.readHeader(); err != nil {
		file.Close()
		return nil, err
	}

	// Load index
	if err := dh.loadIndex(); err != nil {
		file.Close()
		return nil, err
	}

	return dh, nil
}

// CreateDiskHistory creates a new history file for writing.
// If the file exists with old format, it's replaced.
func CreateDiskHistory(config DiskHistoryConfig) (*DiskHistory, error) {
	// Check if file exists and has valid format
	existing, err := OpenDiskHistory(config.Path)
	if err == nil && existing != nil {
		// Valid existing file - we could migrate, but for now just close and replace
		existing.Close()
	}

	// Create new file (truncate if exists)
	file, err := os.Create(config.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}

	dh := &DiskHistory{
		config:      config,
		file:        file,
		writer:      bufio.NewWriter(file),
		index:       make([]uint64, 0, 1000),
		writeOffset: diskHeaderSize,
		lineCount:   0,
		writeMode:   true,
	}

	// Write placeholder header (will be updated on close)
	if err := dh.writeHeader(); err != nil {
		file.Close()
		os.Remove(config.Path)
		return nil, err
	}

	return dh, nil
}

// readHeader reads and validates the file header.
func (dh *DiskHistory) readHeader() error {
	header := make([]byte, diskHeaderSize)
	if _, err := io.ReadFull(dh.file, header); err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	// Validate magic
	magic := string(header[0:8])
	if magic != diskHistoryMagic {
		return fmt.Errorf("invalid magic: %s (expected %s)", magic, diskHistoryMagic)
	}

	// Read version
	version := binary.LittleEndian.Uint32(header[8:12])
	if version != diskHistoryVersion {
		return fmt.Errorf("unsupported version: %d", version)
	}

	// Read flags (reserved for future use)
	// flags := binary.LittleEndian.Uint32(header[12:16])

	// Read line count
	dh.lineCount = binary.LittleEndian.Uint64(header[16:24])

	// Read index offset
	// indexOffset stored at header[24:32]

	return nil
}

// writeHeader writes the file header.
func (dh *DiskHistory) writeHeader() error {
	header := make([]byte, diskHeaderSize)

	// Magic
	copy(header[0:8], diskHistoryMagic)

	// Version
	binary.LittleEndian.PutUint32(header[8:12], diskHistoryVersion)

	// Flags (reserved)
	binary.LittleEndian.PutUint32(header[12:16], 0)

	// Line count (placeholder, updated on close)
	binary.LittleEndian.PutUint64(header[16:24], dh.lineCount)

	// Index offset (placeholder, updated on close)
	binary.LittleEndian.PutUint64(header[24:32], 0)

	// Seek to beginning and write
	if _, err := dh.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek: %w", err)
	}

	if _, err := dh.writer.Write(header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	return nil
}

// loadIndex loads the line offset index from the end of the file.
func (dh *DiskHistory) loadIndex() error {
	if dh.lineCount == 0 {
		dh.index = make([]uint64, 0)
		return nil
	}

	// Read index offset from header
	header := make([]byte, diskHeaderSize)
	if _, err := dh.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek to header: %w", err)
	}
	if _, err := io.ReadFull(dh.file, header); err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	indexOffset := binary.LittleEndian.Uint64(header[24:32])
	if indexOffset == 0 {
		// Index not written yet - scan file to build index
		return dh.rebuildIndex()
	}

	// Seek to index
	if _, err := dh.file.Seek(int64(indexOffset), io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek to index: %w", err)
	}

	// Read index entries
	dh.index = make([]uint64, dh.lineCount)
	for i := uint64(0); i < dh.lineCount; i++ {
		var offset uint64
		if err := binary.Read(dh.file, binary.LittleEndian, &offset); err != nil {
			return fmt.Errorf("failed to read index entry %d: %w", i, err)
		}
		dh.index[i] = offset
	}

	return nil
}

// rebuildIndex scans the file to rebuild the index.
// Used when opening a file that wasn't closed properly.
func (dh *DiskHistory) rebuildIndex() error {
	dh.index = make([]uint64, 0, dh.lineCount)

	// Seek past header
	if _, err := dh.file.Seek(diskHeaderSize, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek past header: %w", err)
	}

	reader := bufio.NewReader(dh.file)
	offset := uint64(diskHeaderSize)

	for i := uint64(0); i < dh.lineCount; i++ {
		dh.index = append(dh.index, offset)

		// Read cell count
		var cellCount uint32
		if err := binary.Read(reader, binary.LittleEndian, &cellCount); err != nil {
			if err == io.EOF {
				// Truncated file - adjust line count
				dh.lineCount = uint64(len(dh.index))
				break
			}
			return fmt.Errorf("failed to read cell count at line %d: %w", i, err)
		}

		// Skip cells
		lineSize := 4 + uint64(cellCount)*diskCellSize
		offset += lineSize

		// Skip the cell data
		skipBytes := int64(cellCount) * diskCellSize
		if _, err := reader.Discard(int(skipBytes)); err != nil {
			return fmt.Errorf("failed to skip cells at line %d: %w", i, err)
		}
	}

	return nil
}

// AppendLine writes a logical line to disk.
// The line is written immediately; index is updated in memory.
func (dh *DiskHistory) AppendLine(line *LogicalLine) error {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	if !dh.writeMode {
		return fmt.Errorf("disk history not opened for writing")
	}

	// Record offset for this line
	dh.index = append(dh.index, dh.writeOffset)

	// Write cell count
	cellCount := uint32(len(line.Cells))
	if err := binary.Write(dh.writer, binary.LittleEndian, cellCount); err != nil {
		return fmt.Errorf("failed to write cell count: %w", err)
	}

	// Write cells
	cellBuf := make([]byte, diskCellSize)
	for _, cell := range line.Cells {
		encodeDiskCell(cell, cellBuf)
		if _, err := dh.writer.Write(cellBuf); err != nil {
			return fmt.Errorf("failed to write cell: %w", err)
		}
	}

	// Update write position
	dh.writeOffset += 4 + uint64(cellCount)*diskCellSize
	dh.lineCount++

	// Sync if configured
	if dh.config.SyncWrites {
		if err := dh.writer.Flush(); err != nil {
			return fmt.Errorf("failed to flush: %w", err)
		}
		if err := dh.file.Sync(); err != nil {
			return fmt.Errorf("failed to sync: %w", err)
		}
	}

	return nil
}

// ReadLine reads a single line by index.
// Returns nil if index is out of bounds.
func (dh *DiskHistory) ReadLine(index int64) (*LogicalLine, error) {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	if index < 0 || index >= int64(len(dh.index)) {
		return nil, nil
	}

	// Flush writer if in write mode to ensure data is readable
	if dh.writeMode && dh.writer != nil {
		if err := dh.writer.Flush(); err != nil {
			return nil, fmt.Errorf("failed to flush before read: %w", err)
		}
	}

	offset := dh.index[index]
	return dh.readLineAt(offset)
}

// ReadLineRange reads a range of lines [start, end).
// Returns lines that exist within the range.
func (dh *DiskHistory) ReadLineRange(start, end int64) ([]*LogicalLine, error) {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	if start < 0 {
		start = 0
	}
	if end > int64(len(dh.index)) {
		end = int64(len(dh.index))
	}
	if start >= end {
		return nil, nil
	}

	// Flush writer if in write mode to ensure data is readable
	if dh.writeMode && dh.writer != nil {
		if err := dh.writer.Flush(); err != nil {
			return nil, fmt.Errorf("failed to flush before read: %w", err)
		}
	}

	result := make([]*LogicalLine, 0, end-start)

	for i := start; i < end; i++ {
		line, err := dh.readLineAt(dh.index[i])
		if err != nil {
			return nil, fmt.Errorf("failed to read line %d: %w", i, err)
		}
		result = append(result, line)
	}

	return result, nil
}

// readLineAt reads a line at the given file offset.
func (dh *DiskHistory) readLineAt(offset uint64) (*LogicalLine, error) {
	// Seek to offset
	if _, err := dh.file.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek to %d: %w", offset, err)
	}

	// Read cell count
	var cellCount uint32
	if err := binary.Read(dh.file, binary.LittleEndian, &cellCount); err != nil {
		return nil, fmt.Errorf("failed to read cell count: %w", err)
	}

	// Read cells
	cells := make([]Cell, cellCount)
	cellBuf := make([]byte, diskCellSize)

	for i := uint32(0); i < cellCount; i++ {
		if _, err := io.ReadFull(dh.file, cellBuf); err != nil {
			return nil, fmt.Errorf("failed to read cell %d: %w", i, err)
		}
		cells[i] = decodeDiskCell(cellBuf)
	}

	return &LogicalLine{Cells: cells}, nil
}

// LineCount returns the number of lines stored.
func (dh *DiskHistory) LineCount() int64 {
	dh.mu.RLock()
	defer dh.mu.RUnlock()
	return int64(dh.lineCount)
}

// Close finalizes the file, writing the index and updating the header.
func (dh *DiskHistory) Close() error {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	if dh.file == nil {
		return nil
	}

	if dh.writeMode {
		// Flush writer
		if dh.writer != nil {
			if err := dh.writer.Flush(); err != nil {
				return fmt.Errorf("failed to flush writer: %w", err)
			}
		}

		// Write index at current position
		indexOffset := dh.writeOffset

		// Seek to write position
		if _, err := dh.file.Seek(int64(indexOffset), io.SeekStart); err != nil {
			return fmt.Errorf("failed to seek for index: %w", err)
		}

		// Write index entries
		for _, offset := range dh.index {
			if err := binary.Write(dh.file, binary.LittleEndian, offset); err != nil {
				return fmt.Errorf("failed to write index entry: %w", err)
			}
		}

		// Update header with final line count and index offset
		header := make([]byte, diskHeaderSize)
		copy(header[0:8], diskHistoryMagic)
		binary.LittleEndian.PutUint32(header[8:12], diskHistoryVersion)
		binary.LittleEndian.PutUint32(header[12:16], 0) // flags
		binary.LittleEndian.PutUint64(header[16:24], dh.lineCount)
		binary.LittleEndian.PutUint64(header[24:32], indexOffset)

		if _, err := dh.file.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("failed to seek to header: %w", err)
		}

		if _, err := dh.file.Write(header); err != nil {
			return fmt.Errorf("failed to update header: %w", err)
		}

		if err := dh.file.Sync(); err != nil {
			return fmt.Errorf("failed to sync: %w", err)
		}
	}

	err := dh.file.Close()
	dh.file = nil
	dh.writer = nil
	return err
}

// encodeDiskCell encodes a Cell to the buffer.
// Format: rune(4) + fg_mode(1) + fg_value(4) + bg_mode(1) + bg_value(4) + attr(2)
func encodeDiskCell(cell Cell, buf []byte) {
	// Rune (4 bytes)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(cell.Rune))

	// Foreground: mode(1) + value(4)
	buf[4] = byte(cell.FG.Mode)
	binary.LittleEndian.PutUint32(buf[5:9], encodeDiskColorValue(cell.FG))

	// Background: mode(1) + value(4)
	buf[9] = byte(cell.BG.Mode)
	binary.LittleEndian.PutUint32(buf[10:14], encodeDiskColorValue(cell.BG))

	// Attributes (2 bytes)
	binary.LittleEndian.PutUint16(buf[14:16], uint16(cell.Attr))
}

// decodeDiskCell decodes a Cell from the buffer.
func decodeDiskCell(buf []byte) Cell {
	cell := Cell{}

	// Rune
	cell.Rune = rune(binary.LittleEndian.Uint32(buf[0:4]))

	// Foreground
	fgMode := ColorMode(buf[4])
	cell.FG = decodeDiskColorValue(fgMode, binary.LittleEndian.Uint32(buf[5:9]))

	// Background
	bgMode := ColorMode(buf[9])
	cell.BG = decodeDiskColorValue(bgMode, binary.LittleEndian.Uint32(buf[10:14]))

	// Attributes
	cell.Attr = Attribute(binary.LittleEndian.Uint16(buf[14:16]))

	return cell
}

// encodeDiskColorValue encodes a Color's value into a uint32.
func encodeDiskColorValue(c Color) uint32 {
	if c.Mode == ColorModeRGB {
		return (uint32(c.R) << 16) | (uint32(c.G) << 8) | uint32(c.B)
	}
	return uint32(c.Value)
}

// decodeDiskColorValue decodes a Color from mode and value.
func decodeDiskColorValue(mode ColorMode, value uint32) Color {
	if mode == ColorModeRGB {
		return Color{
			Mode: ColorModeRGB,
			R:    uint8((value >> 16) & 0xFF),
			G:    uint8((value >> 8) & 0xFF),
			B:    uint8(value & 0xFF),
		}
	}
	return Color{
		Mode:  mode,
		Value: uint8(value & 0xFF),
	}
}

// Path returns the file path.
func (dh *DiskHistory) Path() string {
	return dh.config.Path
}

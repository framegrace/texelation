// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/history_store.go
// Summary: File-based persistence for terminal history.
// Usage: Handles writing/reading history to/from disk with compression.
// Notes: Designed to support encryption in the future.

package parser

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	historyFileVersion = 1
	historyMagic       = "TXHIST01" // 8 bytes
	cellEncodedSize    = 18         // rune(4) + fg(4) + bg(4) + attr(1) + wrapped(1) + flags(4)
)

// FileFlags represents flags in the history file header.
type FileFlags uint32

const (
	FlagCompressed FileFlags = 1 << 0
	FlagEncrypted  FileFlags = 1 << 1
)

// HistoryStore handles file-based persistence of terminal history.
type HistoryStore struct {
	// File paths
	baseDir     string
	sessionFile string
	metaFile    string

	// Write pipeline: file → buffer → gzip (→ encryption in future)
	file       *os.File
	bufWriter  *bufio.Writer
	gzipWriter *gzip.Writer

	// Configuration
	compress          bool
	encryptionEnabled bool

	// Stats
	lineCount    int
	bytesWritten int64

	// Synchronization
	mu sync.Mutex
}

// NewHistoryStore creates a new history store for persistence.
// Uses pane-ID-based file naming (from metadata.SessionID) in ~/.texelation/scrollback/
// for persistent scrollback across server restarts.
// Note: Compression is disabled for append-mode files (can't append to gzip).
func NewHistoryStore(config HistoryConfig, metadata SessionMetadata) (*HistoryStore, error) {
	// Use simpler directory structure for pane-ID-based persistence
	scrollbackDir := filepath.Join(config.PersistDir, "scrollback")

	if err := os.MkdirAll(scrollbackDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create scrollback directory: %w", err)
	}

	// Construct file paths using session ID (which is the pane ID for persistent sessions)
	// Note: No compression for append-mode files (gzip doesn't support appending)
	ext := ".hist"
	if config.Encrypt {
		ext += ".enc"
	}

	sessionFile := filepath.Join(scrollbackDir, metadata.SessionID+ext)
	metaFile := filepath.Join(scrollbackDir, metadata.SessionID+".meta")

	// Open history file for appending (to support restarts)
	file, err := os.OpenFile(sessionFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open history file: %w", err)
	}

	store := &HistoryStore{
		baseDir:           config.PersistDir,
		sessionFile:       sessionFile,
		metaFile:          metaFile,
		file:              file,
		compress:          false, // Never compress append-mode files
		encryptionEnabled: config.Encrypt,
		lineCount:         0,
		bytesWritten:      0,
	}

	// Check if this is a new file (size == 0) and write header if needed
	fileInfo, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat history file: %w", err)
	}

	if fileInfo.Size() == 0 {
		// New file - write header
		if err := store.writeHeader(); err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to write header: %w", err)
		}
	} else {
		// Existing file - skip header, we're appending
		store.bytesWritten = fileInfo.Size()
	}

	// Set up write pipeline (no gzip for append-mode)
	store.bufWriter = bufio.NewWriter(file)

	return store, nil
}

// writeHeader writes the file format header.
func (hs *HistoryStore) writeHeader() error {
	// Header format: TXHIST01[flags:4 bytes]
	var flags FileFlags
	if hs.compress {
		flags |= FlagCompressed
	}
	if hs.encryptionEnabled {
		flags |= FlagEncrypted
	}

	header := make([]byte, len(historyMagic)+4)
	copy(header, historyMagic)
	binary.LittleEndian.PutUint32(header[len(historyMagic):], uint32(flags))

	n, err := hs.file.Write(header)
	if err != nil {
		return err
	}
	hs.bytesWritten += int64(n)
	return nil
}

// getWriter returns the appropriate writer based on compression/encryption settings.
func (hs *HistoryStore) getWriter() io.Writer {
	if hs.gzipWriter != nil {
		return hs.gzipWriter
	}
	return hs.bufWriter
}

// WriteLines writes multiple lines to the history file.
func (hs *HistoryStore) WriteLines(lines [][]Cell) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	for _, line := range lines {
		if err := hs.writeLineLocked(line); err != nil {
			return err
		}
	}

	// Flush to ensure data is written
	return hs.flushLocked()
}

// writeLineLocked writes a single line to the file (caller must hold lock).
func (hs *HistoryStore) writeLineLocked(line []Cell) error {
	writer := hs.getWriter()

	// Line format: [length:4 bytes][cell data...]
	lineLen := len(line)
	lenBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenBuf, uint32(lineLen))

	if _, err := writer.Write(lenBuf); err != nil {
		return err
	}

	// Write each cell
	cellBuf := make([]byte, cellEncodedSize)
	for _, cell := range line {
		if err := encodeCell(cell, cellBuf); err != nil {
			return err
		}
		if _, err := writer.Write(cellBuf); err != nil {
			return err
		}
	}

	hs.lineCount++
	return nil
}

// encodeCell encodes a Cell into a byte buffer.
// Format: rune(4) + fg_mode(1) + fg_value(4) + bg_mode(1) + bg_value(4) + attr(1) + wrapped(1) + padding(1)
func encodeCell(cell Cell, buf []byte) error {
	if len(buf) < cellEncodedSize {
		return fmt.Errorf("buffer too small")
	}

	// Rune (4 bytes)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(cell.Rune))

	// Foreground color mode (1 byte) + value (4 bytes)
	buf[4] = byte(cell.FG.Mode)
	binary.LittleEndian.PutUint32(buf[5:9], encodeColor(cell.FG))

	// Background color mode (1 byte) + value (4 bytes)
	buf[9] = byte(cell.BG.Mode)
	binary.LittleEndian.PutUint32(buf[10:14], encodeColor(cell.BG))

	// Attributes (1 byte)
	buf[14] = byte(cell.Attr)

	// Wrapped flag (1 byte)
	if cell.Wrapped {
		buf[15] = 1
	} else {
		buf[15] = 0
	}

	// Padding (2 bytes)
	buf[16] = 0
	buf[17] = 0

	return nil
}

// encodeColor encodes a Color into a uint32.
// For RGB mode: pack R, G, B into bytes
// For other modes: just the value
func encodeColor(c Color) uint32 {
	if c.Mode == ColorModeRGB {
		return (uint32(c.R) << 16) | (uint32(c.G) << 8) | uint32(c.B)
	}
	return uint32(c.Value)
}

// decodeCell decodes a Cell from a byte buffer (for future reading).
func decodeCell(buf []byte) (Cell, error) {
	if len(buf) < cellEncodedSize {
		return Cell{}, fmt.Errorf("buffer too small")
	}

	cell := Cell{}

	// Rune
	cell.Rune = rune(binary.LittleEndian.Uint32(buf[0:4]))

	// Foreground
	cell.FG.Mode = ColorMode(buf[4])
	cell.FG = decodeColor(cell.FG.Mode, binary.LittleEndian.Uint32(buf[5:9]))

	// Background
	cell.BG.Mode = ColorMode(buf[9])
	cell.BG = decodeColor(cell.BG.Mode, binary.LittleEndian.Uint32(buf[10:14]))

	// Attributes
	cell.Attr = Attribute(buf[14])

	// Wrapped
	cell.Wrapped = buf[15] != 0

	return cell, nil
}

// decodeColor decodes a Color from a uint32.
func decodeColor(mode ColorMode, value uint32) Color {
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

// flushLocked flushes all buffered data to disk (caller must hold lock).
func (hs *HistoryStore) flushLocked() error {
	// Flush gzip writer if present
	if hs.gzipWriter != nil {
		if err := hs.gzipWriter.Flush(); err != nil {
			return err
		}
	}

	// Flush buffer writer
	if hs.bufWriter != nil {
		if err := hs.bufWriter.Flush(); err != nil {
			return err
		}
	}

	// Sync to disk
	return hs.file.Sync()
}

// Close closes the history store and writes metadata.
func (hs *HistoryStore) Close(metadata SessionMetadata) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	// Flush remaining data
	if err := hs.flushLocked(); err != nil {
		fmt.Fprintf(os.Stderr, "Error flushing history: %v\n", err)
	}

	// Close gzip writer
	if hs.gzipWriter != nil {
		if err := hs.gzipWriter.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing gzip writer: %v\n", err)
		}
	}

	// Close file
	if hs.file != nil {
		if err := hs.file.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing history file: %v\n", err)
		}
	}

	// Write metadata file
	metadata.Encrypted = hs.encryptionEnabled
	if err := hs.writeMetadata(metadata); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	return nil
}

// writeMetadata writes session metadata to a JSON file.
func (hs *HistoryStore) writeMetadata(metadata SessionMetadata) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(hs.metaFile, data, 0600); err != nil {
		return err
	}

	return nil
}

// LineCount returns the number of lines written.
func (hs *HistoryStore) LineCount() int {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	return hs.lineCount
}

// BytesWritten returns the number of bytes written to the file.
func (hs *HistoryStore) BytesWritten() int64 {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	// Get current file size
	if hs.file != nil {
		if info, err := hs.file.Stat(); err == nil {
			return info.Size()
		}
	}

	return hs.bytesWritten
}

// LoadLines reads existing history from a file and returns the lines.
// This is used to restore scrollback when reopening a persisted session.
func LoadHistoryLines(sessionFile string) ([][]Cell, error) {
	// Check if file exists
	fileInfo, err := os.Stat(sessionFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No existing history, not an error
		}
		return nil, fmt.Errorf("failed to stat history file: %w", err)
	}

	// Empty file means no history yet
	if fileInfo.Size() == 0 {
		return nil, nil
	}

	// Open file for reading
	file, err := os.Open(sessionFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open history file: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	// Read and validate header
	header := make([]byte, len(historyMagic)+4)
	if _, err := io.ReadFull(reader, header); err != nil {
		if err == io.EOF {
			return nil, nil // Empty file
		}
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	// Validate magic
	if string(header[:len(historyMagic)]) != historyMagic {
		return nil, fmt.Errorf("invalid history file magic")
	}

	// Read flags (for future use)
	_ = binary.LittleEndian.Uint32(header[len(historyMagic):])

	// Read lines until EOF
	var lines [][]Cell
	cellBuf := make([]byte, cellEncodedSize)

	for {
		// Read line length
		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(reader, lenBuf); err != nil {
			if err == io.EOF {
				break // Normal end of file
			}
			return nil, fmt.Errorf("failed to read line length: %w", err)
		}

		lineLen := binary.LittleEndian.Uint32(lenBuf)

		// Read cells for this line
		line := make([]Cell, lineLen)
		for i := uint32(0); i < lineLen; i++ {
			if _, err := io.ReadFull(reader, cellBuf); err != nil {
				return nil, fmt.Errorf("failed to read cell %d: %w", i, err)
			}

			cell, err := decodeCell(cellBuf)
			if err != nil {
				return nil, fmt.Errorf("failed to decode cell %d: %w", i, err)
			}

			line[i] = cell
		}

		lines = append(lines, line)
	}

	return lines, nil
}

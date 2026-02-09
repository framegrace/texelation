// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/write_ahead_log.go
// Summary: Write-Ahead Log for crash recovery of terminal history.
//
// WAL Format:
//   Header (32 bytes):
//     Magic: "TXWAL001" (8 bytes)
//     Version: uint32 (4 bytes) - value 1
//     TerminalID: [16]byte (UUID bytes)
//     LastCheckpoint: uint64 (8 bytes) - global line index
//
//   Entry (variable, repeated):
//     EntryType: uint8 (1 byte) - LINE_WRITE=0x01, LINE_MODIFY=0x02, CHECKPOINT=0x03
//     GlobalLineIdx: uint64 (8 bytes)
//     Timestamp: int64 (8 bytes) - UnixNano
//     DataLen: uint32 (4 bytes)
//     Data: [DataLen]byte - serialized LogicalLine
//     CRC32: uint32 (4 bytes)
//
// The WAL owns a PageStore and coordinates checkpoints. On startup, it recovers
// uncommitted entries by replaying them to the PageStore.

package parser

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// WAL format constants
const (
	WALMagic      = "TXWAL001"
	WALVersion    = uint32(1)
	WALHeaderSize = 32
	WALEntryBase  = 1 + 8 + 8 + 4 + 4 // type + lineIdx + timestamp + dataLen + crc32 (no data)
)

// WAL entry types
const (
	EntryTypeLineWrite  uint8 = 0x01
	EntryTypeLineModify uint8 = 0x02
	EntryTypeCheckpoint uint8 = 0x03
	EntryTypeMetadata   uint8 = 0x04 // Viewport state (scroll position, cursor)
)

// WALConfig holds configuration for the write-ahead log.
type WALConfig struct {
	// WALDir is the directory containing the wal.log file.
	// Typically: ~/.local/share/texelation/history/terminals/<uuid>/
	WALDir string

	// TerminalID is the terminal's UUID (stored in header for validation).
	TerminalID string

	// PageStoreConfig is passed to the owned PageStore.
	PageStoreConfig PageStoreConfig

	// CheckpointInterval is how often to auto-checkpoint (0 = disabled).
	CheckpointInterval time.Duration

	// CheckpointMaxSize triggers checkpoint when WAL exceeds this size in bytes.
	// Default: 10MB
	CheckpointMaxSize int64
}

// DefaultWALConfig returns sensible defaults.
func DefaultWALConfig(baseDir, terminalID string) WALConfig {
	return WALConfig{
		WALDir:             filepath.Join(baseDir, "terminals", terminalID),
		TerminalID:         terminalID,
		PageStoreConfig:    DefaultPageStoreConfig(baseDir, terminalID),
		CheckpointInterval: 30 * time.Second,
		CheckpointMaxSize:  10 * 1024 * 1024, // 10MB
	}
}

// WALHeader is the 32-byte header at the start of the WAL file.
type WALHeader struct {
	Magic          [8]byte  // "TXWAL001"
	Version        uint32   // Format version (1)
	TerminalID     [16]byte // UUID bytes
	LastCheckpoint uint64   // Global line index of last checkpoint
}

// WALEntry represents a single entry in the WAL.
type WALEntry struct {
	Type          uint8
	GlobalLineIdx uint64
	Timestamp     time.Time
	Line          *LogicalLine   // nil for CHECKPOINT and METADATA entries
	Metadata      *ViewportState // nil for non-METADATA entries
}

// WriteAheadLog provides crash recovery for terminal history.
// It owns a PageStore and coordinates checkpoints.
type WriteAheadLog struct {
	config WALConfig

	// WAL file
	walPath string
	walFile *os.File
	header  WALHeader

	// Owned PageStore
	pageStore *PageStore

	// State
	walSize        int64  // Current WAL file size
	entriesWritten int64  // Entries since last checkpoint
	nextGlobalIdx  int64  // Next line index to assign
	lastCheckpoint uint64 // Last checkpointed line index

	// Recovered metadata from WAL replay (nil if no metadata entry found)
	recoveredMetadata *ViewportState

	// Current metadata (written via WriteMetadata, persisted on checkpoint)
	currentMetadata *ViewportState

	// Checkpoint timer
	checkpointTimer *time.Timer
	stopCh          chan struct{}
	stopped         bool

	// Time function for testing
	nowFunc func() time.Time

	mu sync.Mutex
}

// OpenWriteAheadLog opens or creates a WAL with its owned PageStore.
// If a WAL exists with uncommitted entries, they are recovered to PageStore.
func OpenWriteAheadLog(config WALConfig) (*WriteAheadLog, error) {
	return openWriteAheadLogWithNow(config, time.Now)
}

// openWriteAheadLogWithNow allows injecting a custom time function for testing.
func openWriteAheadLogWithNow(config WALConfig, nowFunc func() time.Time) (*WriteAheadLog, error) {
	// Ensure directory exists
	if err := os.MkdirAll(config.WALDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	walPath := filepath.Join(config.WALDir, "wal.log")

	w := &WriteAheadLog{
		config:  config,
		walPath: walPath,
		nowFunc: nowFunc,
		stopCh:  make(chan struct{}),
	}

	// Copy terminal ID to header
	copy(w.header.Magic[:], WALMagic)
	w.header.Version = WALVersion
	w.copyTerminalIDToHeader(config.TerminalID)

	// Try to open existing WAL
	existingWAL, err := os.OpenFile(walPath, os.O_RDWR, 0644)
	if err == nil {
		// WAL exists - read header and recover
		if err := w.readHeader(existingWAL); err != nil {
			existingWAL.Close()
			// Corrupted header - try to preserve existing PageStore data
			if err := w.createFreshWALPreservingPageStore(); err != nil {
				return nil, err
			}
		} else {
			w.walFile = existingWAL
			w.lastCheckpoint = w.header.LastCheckpoint

			// Open or create PageStore
			if err := w.openPageStore(); err != nil {
				w.walFile.Close()
				return nil, err
			}

			// Recover uncommitted entries
			if err := w.recover(); err != nil {
				w.walFile.Close()
				w.pageStore.Close()
				return nil, fmt.Errorf("WAL recovery failed: %w", err)
			}
		}
	} else if os.IsNotExist(err) {
		// No WAL - create fresh
		if err := w.createFreshWAL(); err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("failed to open WAL: %w", err)
	}

	// Sync nextGlobalIdx with PageStore
	w.nextGlobalIdx = w.pageStore.LineCount()

	// Get WAL file size
	if info, err := w.walFile.Stat(); err == nil {
		w.walSize = info.Size()
	}

	// Start checkpoint timer if configured
	if config.CheckpointInterval > 0 {
		w.startCheckpointTimer()
	}

	return w, nil
}

// copyTerminalIDToHeader converts the string terminal ID to bytes.
func (w *WriteAheadLog) copyTerminalIDToHeader(terminalID string) {
	// Simple copy - truncate or pad to 16 bytes
	copy(w.header.TerminalID[:], terminalID)
}

// createFreshWAL creates a new WAL file and PageStore.
func (w *WriteAheadLog) createFreshWAL() error {
	// Create WAL file
	file, err := os.Create(w.walPath)
	if err != nil {
		return fmt.Errorf("failed to create WAL file: %w", err)
	}
	w.walFile = file

	// Write header
	if err := w.writeHeader(); err != nil {
		w.walFile.Close()
		os.Remove(w.walPath)
		return err
	}

	w.walSize = WALHeaderSize

	// Create fresh PageStore
	ps, err := CreatePageStore(w.config.PageStoreConfig)
	if err != nil {
		w.walFile.Close()
		os.Remove(w.walPath)
		return fmt.Errorf("failed to create PageStore: %w", err)
	}
	w.pageStore = ps

	return nil
}

// createFreshWALPreservingPageStore creates a fresh WAL but preserves existing PageStore data.
// Used when WAL header is corrupted but PageStore may still have valid data.
func (w *WriteAheadLog) createFreshWALPreservingPageStore() error {
	// Create WAL file
	file, err := os.Create(w.walPath)
	if err != nil {
		return fmt.Errorf("failed to create WAL file: %w", err)
	}
	w.walFile = file

	// Write header
	if err := w.writeHeader(); err != nil {
		w.walFile.Close()
		os.Remove(w.walPath)
		return err
	}

	w.walSize = WALHeaderSize

	// Try to open existing PageStore first to preserve history
	ps, err := OpenPageStore(w.config.PageStoreConfig)
	if err != nil {
		w.walFile.Close()
		os.Remove(w.walPath)
		return fmt.Errorf("failed to open PageStore: %w", err)
	}
	if ps == nil {
		// No existing PageStore - create new one
		ps, err = CreatePageStore(w.config.PageStoreConfig)
		if err != nil {
			w.walFile.Close()
			os.Remove(w.walPath)
			return fmt.Errorf("failed to create PageStore: %w", err)
		}
	}
	w.pageStore = ps

	// Update checkpoint to match existing PageStore state
	lineCount := w.pageStore.LineCount()
	if lineCount > 0 {
		w.header.LastCheckpoint = uint64(lineCount - 1)
		w.lastCheckpoint = w.header.LastCheckpoint
		// Rewrite header with updated checkpoint
		if err := w.writeHeader(); err != nil {
			return fmt.Errorf("failed to update header after preserving PageStore: %w", err)
		}
	}

	return nil
}

// openPageStore opens an existing PageStore or creates one.
func (w *WriteAheadLog) openPageStore() error {
	ps, err := OpenPageStore(w.config.PageStoreConfig)
	if err != nil {
		return fmt.Errorf("failed to open PageStore: %w", err)
	}
	if ps == nil {
		// No existing PageStore - create new
		ps, err = CreatePageStore(w.config.PageStoreConfig)
		if err != nil {
			return fmt.Errorf("failed to create PageStore: %w", err)
		}
	}
	w.pageStore = ps
	return nil
}

// writeHeader writes the WAL header to the file.
func (w *WriteAheadLog) writeHeader() error {
	buf := make([]byte, WALHeaderSize)

	// Magic (8 bytes)
	copy(buf[0:8], w.header.Magic[:])

	// Version (4 bytes)
	binary.LittleEndian.PutUint32(buf[8:12], w.header.Version)

	// TerminalID (16 bytes)
	copy(buf[12:28], w.header.TerminalID[:])

	// LastCheckpoint (8 bytes) - positioned to allow in-place update
	binary.LittleEndian.PutUint64(buf[24:32], w.header.LastCheckpoint)

	if _, err := w.walFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek to header: %w", err)
	}

	if _, err := w.walFile.Write(buf); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	return nil
}

// readHeader reads and validates the WAL header.
func (w *WriteAheadLog) readHeader(file *os.File) error {
	buf := make([]byte, WALHeaderSize)
	if _, err := io.ReadFull(file, buf); err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	// Validate magic
	if string(buf[0:8]) != WALMagic {
		return fmt.Errorf("invalid WAL magic: %q", buf[0:8])
	}

	// Validate version
	version := binary.LittleEndian.Uint32(buf[8:12])
	if version != WALVersion {
		return fmt.Errorf("unsupported WAL version: %d", version)
	}

	copy(w.header.Magic[:], buf[0:8])
	w.header.Version = version
	copy(w.header.TerminalID[:], buf[12:28])
	w.header.LastCheckpoint = binary.LittleEndian.Uint64(buf[24:32])

	return nil
}

// Append writes a line to the WAL.
// The line is journaled but not yet committed to PageStore.
func (w *WriteAheadLog) Append(lineIdx int64, line *LogicalLine, timestamp time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped {
		return fmt.Errorf("WAL is closed")
	}

	// Determine entry type
	entryType := EntryTypeLineWrite
	if lineIdx < w.nextGlobalIdx {
		entryType = EntryTypeLineModify
	}

	// Encode entry
	entryData, err := w.encodeEntry(entryType, uint64(lineIdx), timestamp, line)
	if err != nil {
		return fmt.Errorf("failed to encode entry: %w", err)
	}

	// Append to WAL file
	if _, err := w.walFile.Write(entryData); err != nil {
		return fmt.Errorf("failed to write entry: %w", err)
	}

	w.walSize += int64(len(entryData))
	w.entriesWritten++

	if lineIdx >= w.nextGlobalIdx {
		w.nextGlobalIdx = lineIdx + 1
	}

	// Check if we should auto-checkpoint due to size
	if w.config.CheckpointMaxSize > 0 && w.walSize >= w.config.CheckpointMaxSize {
		// Checkpoint in background to avoid blocking writes
		go func() {
			w.Checkpoint()
		}()
	}

	return nil
}

// WriteMetadata writes viewport state (scroll position, cursor) to the WAL.
// Multiple metadata writes are OK - only the last one matters on recovery.
// This ensures metadata is crash-safe alongside content.
func (w *WriteAheadLog) WriteMetadata(state *ViewportState) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.writeMetadataLocked(state)
}

// writeMetadataLocked writes metadata with the lock already held.
func (w *WriteAheadLog) writeMetadataLocked(state *ViewportState) error {
	if w.stopped {
		return fmt.Errorf("WAL is closed")
	}

	if state == nil {
		return nil
	}

	// Store current metadata for checkpoint
	w.currentMetadata = state

	// Encode metadata to JSON
	metadataBytes, err := encodeViewportState(state)
	if err != nil {
		return fmt.Errorf("failed to encode metadata: %w", err)
	}

	// Encode as WAL entry (using GlobalLineIdx=0 as reserved for metadata)
	entryData, err := w.encodeMetadataEntry(metadataBytes, w.nowFunc())
	if err != nil {
		return fmt.Errorf("failed to encode metadata entry: %w", err)
	}

	// Append to WAL file
	if _, err := w.walFile.Write(entryData); err != nil {
		return fmt.Errorf("failed to write metadata entry: %w", err)
	}

	w.walSize += int64(len(entryData))
	w.entriesWritten++

	return nil
}

// GetRecoveredMetadata returns the metadata recovered from WAL replay.
// Returns nil if no metadata was found in the WAL.
func (w *WriteAheadLog) GetRecoveredMetadata() *ViewportState {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.recoveredMetadata
}

// encodeMetadataEntry serializes a metadata WAL entry to bytes.
func (w *WriteAheadLog) encodeMetadataEntry(metadataBytes []byte, timestamp time.Time) ([]byte, error) {
	// Calculate total size
	totalSize := WALEntryBase + len(metadataBytes)
	buf := make([]byte, totalSize)

	// EntryType (1 byte)
	buf[0] = EntryTypeMetadata

	// GlobalLineIdx (8 bytes) - use 0 as reserved marker for metadata
	binary.LittleEndian.PutUint64(buf[1:9], 0)

	// Timestamp (8 bytes)
	binary.LittleEndian.PutUint64(buf[9:17], uint64(timestamp.UnixNano()))

	// DataLen (4 bytes)
	binary.LittleEndian.PutUint32(buf[17:21], uint32(len(metadataBytes)))

	// Data (variable)
	if len(metadataBytes) > 0 {
		copy(buf[21:21+len(metadataBytes)], metadataBytes)
	}

	// CRC32 (4 bytes) - covers everything except CRC itself
	crc := crc32.ChecksumIEEE(buf[:totalSize-4])
	binary.LittleEndian.PutUint32(buf[totalSize-4:], crc)

	return buf, nil
}

// encodeViewportState serializes ViewportState to bytes.
func encodeViewportState(state *ViewportState) ([]byte, error) {
	// Use a simple binary format for efficiency:
	// ScrollOffset (8 bytes) + LiveEdgeBase (8 bytes) + CursorX (4 bytes) + CursorY (4 bytes) + SavedAt (8 bytes)
	buf := make([]byte, 32)
	binary.LittleEndian.PutUint64(buf[0:8], uint64(state.ScrollOffset))
	binary.LittleEndian.PutUint64(buf[8:16], uint64(state.LiveEdgeBase))
	binary.LittleEndian.PutUint32(buf[16:20], uint32(state.CursorX))
	binary.LittleEndian.PutUint32(buf[20:24], uint32(state.CursorY))
	binary.LittleEndian.PutUint64(buf[24:32], uint64(state.SavedAt.UnixNano()))
	return buf, nil
}

// decodeViewportState deserializes ViewportState from bytes.
func decodeViewportState(data []byte) (*ViewportState, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("metadata too short: %d bytes", len(data))
	}
	return &ViewportState{
		ScrollOffset: int64(binary.LittleEndian.Uint64(data[0:8])),
		LiveEdgeBase: int64(binary.LittleEndian.Uint64(data[8:16])),
		CursorX:      int(binary.LittleEndian.Uint32(data[16:20])),
		CursorY:      int(binary.LittleEndian.Uint32(data[20:24])),
		SavedAt:      time.Unix(0, int64(binary.LittleEndian.Uint64(data[24:32]))),
	}, nil
}

// encodeEntry serializes a WAL entry to bytes.
func (w *WriteAheadLog) encodeEntry(entryType uint8, lineIdx uint64, timestamp time.Time, line *LogicalLine) ([]byte, error) {
	// Encode line data
	var lineData []byte
	if line != nil && entryType != EntryTypeCheckpoint {
		lineData = encodeLineData(line)
	}

	// Calculate total size
	totalSize := WALEntryBase + len(lineData)
	buf := make([]byte, totalSize)

	// EntryType (1 byte)
	buf[0] = entryType

	// GlobalLineIdx (8 bytes)
	binary.LittleEndian.PutUint64(buf[1:9], lineIdx)

	// Timestamp (8 bytes)
	binary.LittleEndian.PutUint64(buf[9:17], uint64(timestamp.UnixNano()))

	// DataLen (4 bytes)
	binary.LittleEndian.PutUint32(buf[17:21], uint32(len(lineData)))

	// Data (variable)
	if len(lineData) > 0 {
		copy(buf[21:21+len(lineData)], lineData)
	}

	// CRC32 (4 bytes) - covers everything except CRC itself
	crc := crc32.ChecksumIEEE(buf[:totalSize-4])
	binary.LittleEndian.PutUint32(buf[totalSize-4:], crc)

	return buf, nil
}

// recover reads uncommitted entries and replays them to PageStore.
func (w *WriteAheadLog) recover() error {
	// Seek past header
	if _, err := w.walFile.Seek(WALHeaderSize, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek past header: %w", err)
	}

	reader := bufio.NewReader(w.walFile)
	var entries []WALEntry
	var lastValidPos int64 = WALHeaderSize
	var lastMetadata *ViewportState // Track last metadata entry

	// Read all entries
	for {
		entry, bytesRead, err := w.readEntry(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Corrupted entry - stop reading, truncate to last valid position
			break
		}

		lastValidPos += int64(bytesRead)

		// Skip entries before last checkpoint
		if entry.Type == EntryTypeCheckpoint {
			// Checkpoint found - clear pending entries and metadata
			entries = nil
			lastMetadata = nil
			w.lastCheckpoint = entry.GlobalLineIdx
		} else if entry.Type == EntryTypeMetadata {
			// Track metadata entries (last one wins)
			lastMetadata = entry.Metadata
		} else {
			entries = append(entries, entry)
		}
	}

	// Store recovered metadata
	w.recoveredMetadata = lastMetadata

	// Truncate WAL to last valid position (remove corrupted data)
	if err := w.walFile.Truncate(lastValidPos); err != nil {
		return fmt.Errorf("failed to truncate WAL: %w", err)
	}

	// Seek to end for appending
	if _, err := w.walFile.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("failed to seek to end: %w", err)
	}

	if len(entries) == 0 {
		return nil // Clean state
	}

	// Replay entries to PageStore using two-pass approach (same as checkpoint)
	// IMPORTANT: Check if line already exists to avoid duplication.
	// This can happen if PageStore was flushed but WAL wasn't checkpointed before crash.
	pageStoreLineCount := w.pageStore.LineCount()

	// Pass 1: Append only truly new lines (LineWrite with index >= current count)
	for _, entry := range entries {
		if entry.Type == EntryTypeLineWrite && entry.Line != nil {
			lineIdx := int64(entry.GlobalLineIdx)
			if lineIdx >= pageStoreLineCount {
				// This is a new line - append it
				if err := w.pageStore.AppendLineWithTimestamp(entry.Line, entry.Timestamp); err != nil {
					return fmt.Errorf("failed to replay line %d: %w", entry.GlobalLineIdx, err)
				}
				pageStoreLineCount++ // Track new count for subsequent entries
			}
			// If lineIdx < pageStoreLineCount, the line already exists - skip append
			// (it will be handled in Pass 2 if there's a corresponding LineModify)
		}
	}

	// Pass 2: Update modified lines (LineModify) and any LineWrite entries for existing lines
	for _, entry := range entries {
		lineIdx := int64(entry.GlobalLineIdx)
		if entry.Line != nil && lineIdx < w.pageStore.LineCount() {
			if entry.Type == EntryTypeLineModify || entry.Type == EntryTypeLineWrite {
				if err := w.pageStore.UpdateLine(lineIdx, entry.Line, entry.Timestamp); err != nil {
					return fmt.Errorf("failed to update line %d in PageStore: %w", entry.GlobalLineIdx, err)
				}
			}
		}
	}

	// Flush PageStore
	if err := w.pageStore.Flush(); err != nil {
		return fmt.Errorf("failed to flush PageStore after recovery: %w", err)
	}

	// Update checkpoint marker
	currentIdx := w.pageStore.LineCount()
	if currentIdx > 0 {
		w.header.LastCheckpoint = uint64(currentIdx - 1)
	}
	w.lastCheckpoint = w.header.LastCheckpoint

	// Truncate WAL to mark recovery complete (don't re-read entries)
	if err := w.truncateWAL(); err != nil {
		return fmt.Errorf("failed to truncate WAL after recovery: %w", err)
	}

	w.entriesWritten = 0
	w.walSize = WALHeaderSize

	return nil
}

// readEntry reads a single entry from the reader.
// Returns the entry, bytes read, and any error.
func (w *WriteAheadLog) readEntry(r *bufio.Reader) (WALEntry, int, error) {
	// Read fixed-size header portion
	headerBuf := make([]byte, 21) // type(1) + lineIdx(8) + timestamp(8) + dataLen(4)
	n, err := io.ReadFull(r, headerBuf)
	if err != nil {
		return WALEntry{}, n, err
	}

	entryType := headerBuf[0]
	lineIdx := binary.LittleEndian.Uint64(headerBuf[1:9])
	timestamp := time.Unix(0, int64(binary.LittleEndian.Uint64(headerBuf[9:17])))
	dataLen := binary.LittleEndian.Uint32(headerBuf[17:21])

	// Read data
	var lineData []byte
	if dataLen > 0 {
		lineData = make([]byte, dataLen)
		if _, err := io.ReadFull(r, lineData); err != nil {
			return WALEntry{}, n, fmt.Errorf("failed to read entry data: %w", err)
		}
	}

	// Read CRC
	crcBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, crcBuf); err != nil {
		return WALEntry{}, n, fmt.Errorf("failed to read CRC: %w", err)
	}
	storedCRC := binary.LittleEndian.Uint32(crcBuf)

	// Verify CRC
	totalSize := 21 + int(dataLen) + 4
	fullEntry := make([]byte, totalSize)
	copy(fullEntry[0:21], headerBuf)
	if dataLen > 0 {
		copy(fullEntry[21:21+dataLen], lineData)
	}
	copy(fullEntry[totalSize-4:], crcBuf)

	expectedCRC := crc32.ChecksumIEEE(fullEntry[:totalSize-4])
	if storedCRC != expectedCRC {
		return WALEntry{}, totalSize, fmt.Errorf("CRC mismatch: stored=%x, computed=%x", storedCRC, expectedCRC)
	}

	// Decode data based on entry type
	var line *LogicalLine
	var metadata *ViewportState

	switch entryType {
	case EntryTypeLineWrite, EntryTypeLineModify:
		if dataLen > 0 {
			line, err = decodeLineData(lineData)
			if err != nil {
				return WALEntry{}, totalSize, fmt.Errorf("failed to decode line: %w", err)
			}
		}
	case EntryTypeMetadata:
		if dataLen > 0 {
			metadata, err = decodeViewportState(lineData)
			if err != nil {
				return WALEntry{}, totalSize, fmt.Errorf("failed to decode metadata: %w", err)
			}
		}
	case EntryTypeCheckpoint:
		// No data to decode
	}

	return WALEntry{
		Type:          entryType,
		GlobalLineIdx: lineIdx,
		Timestamp:     timestamp,
		Line:          line,
		Metadata:      metadata,
	}, totalSize, nil
}

// Checkpoint flushes uncommitted entries to PageStore and truncates the WAL.
func (w *WriteAheadLog) Checkpoint() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.checkpointLocked()
}

// checkpointLocked performs checkpoint with lock held.
func (w *WriteAheadLog) checkpointLocked() error {
	if w.stopped {
		return nil
	}

	// Sync WAL file before reading to ensure all writes are on disk
	if err := w.walFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync WAL before checkpoint: %w", err)
	}

	// Read all entries from WAL and replay to PageStore
	entries, lastMetadata, err := w.readWALEntriesWithMetadata()
	if err != nil {
		return fmt.Errorf("failed to read WAL entries: %w", err)
	}

	// Replay entries to PageStore in two passes:
	// Pass 1: Process all LINE_WRITE entries (append new lines)
	// Pass 2: Process all LINE_MODIFY entries (update existing lines)
	// This ensures modifications can always find their target lines.

	// Pass 1: Write all new lines
	for _, entry := range entries {
		if entry.Type == EntryTypeLineWrite && entry.Line != nil {
			if err := w.pageStore.AppendLineWithTimestamp(entry.Line, entry.Timestamp); err != nil {
				return fmt.Errorf("failed to append line %d to PageStore: %w", entry.GlobalLineIdx, err)
			}
		}
	}

	// Pass 2: Update modified lines (now all target lines exist)
	for _, entry := range entries {
		if entry.Type == EntryTypeLineModify && entry.Line != nil {
			if err := w.pageStore.UpdateLine(int64(entry.GlobalLineIdx), entry.Line, entry.Timestamp); err != nil {
				return fmt.Errorf("failed to update line %d in PageStore: %w", entry.GlobalLineIdx, err)
			}
		}
	}

	// Flush PageStore to disk
	if err := w.pageStore.Flush(); err != nil {
		return fmt.Errorf("failed to flush PageStore: %w", err)
	}

	// Update current metadata from WAL entries if present
	// This ensures GetRecoveredMetadata returns the right value after checkpoint
	if lastMetadata != nil {
		w.currentMetadata = lastMetadata
	}

	// Update checkpoint in header
	currentIdx := w.pageStore.LineCount()
	if currentIdx > 0 {
		w.header.LastCheckpoint = uint64(currentIdx - 1)
	}
	w.lastCheckpoint = w.header.LastCheckpoint

	// Sync WAL file
	if err := w.walFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync WAL: %w", err)
	}

	// Truncate WAL - create new file with just header
	if err := w.truncateWAL(); err != nil {
		return fmt.Errorf("failed to truncate WAL: %w", err)
	}

	w.entriesWritten = 0
	w.walSize = WALHeaderSize

	// Re-write current metadata to the fresh WAL so it survives checkpoint
	// This is necessary because the metadata entries were cleared with the WAL
	if w.currentMetadata != nil {
		if err := w.writeMetadataLocked(w.currentMetadata); err != nil {
			return fmt.Errorf("failed to persist metadata after checkpoint: %w", err)
		}
	}

	// Sync WAL after metadata re-write to ensure it reaches disk.
	// Without this, Close() → walFile.Close() does NOT guarantee sync on Linux,
	// and the metadata can be lost if the process exits before OS flushes page cache.
	// On reload, stale metadata from a previous checkpoint would be used, causing
	// liveEdgeBase/cursor to be wrong relative to actual content.
	if err := w.walFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync WAL after checkpoint: %w", err)
	}

	return nil
}

// readWALEntries reads all entries from the WAL file since last checkpoint.
// Deprecated: Use readWALEntriesWithMetadata to also get metadata.
func (w *WriteAheadLog) readWALEntries() ([]WALEntry, error) {
	entries, _, err := w.readWALEntriesWithMetadata()
	return entries, err
}

// readWALEntriesWithMetadata reads all entries and tracks the last metadata entry.
func (w *WriteAheadLog) readWALEntriesWithMetadata() ([]WALEntry, *ViewportState, error) {
	// Seek to start of entries (after header)
	if _, err := w.walFile.Seek(WALHeaderSize, io.SeekStart); err != nil {
		return nil, nil, fmt.Errorf("failed to seek to entries: %w", err)
	}

	reader := bufio.NewReader(w.walFile)
	var entries []WALEntry
	var lastMetadata *ViewportState

	for {
		entry, _, err := w.readEntry(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Corrupted entry - stop reading
			break
		}

		// Handle different entry types
		switch entry.Type {
		case EntryTypeCheckpoint:
			// Checkpoint found - clear entries and metadata before this checkpoint
			entries = nil
			lastMetadata = nil
		case EntryTypeMetadata:
			// Track last metadata entry (don't add to entries list)
			lastMetadata = entry.Metadata
		default:
			// LINE_WRITE and LINE_MODIFY entries
			entries = append(entries, entry)
		}
	}

	// Seek back to end for future appends
	if _, err := w.walFile.Seek(0, io.SeekEnd); err != nil {
		return nil, nil, fmt.Errorf("failed to seek to end: %w", err)
	}

	return entries, lastMetadata, nil
}

// truncateWAL creates a fresh WAL file with only the header.
func (w *WriteAheadLog) truncateWAL() error {
	// Close current file
	if err := w.walFile.Close(); err != nil {
		return fmt.Errorf("failed to close WAL: %w", err)
	}

	// Create temp file
	tmpPath := w.walPath + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		// Try to reopen original
		w.walFile, _ = os.OpenFile(w.walPath, os.O_RDWR|os.O_APPEND, 0644)
		return fmt.Errorf("failed to create temp WAL: %w", err)
	}

	// Write header to temp
	w.walFile = tmpFile
	if err := w.writeHeader(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		w.walFile, _ = os.OpenFile(w.walPath, os.O_RDWR|os.O_APPEND, 0644)
		return fmt.Errorf("failed to write temp header: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		w.walFile, _ = os.OpenFile(w.walPath, os.O_RDWR|os.O_APPEND, 0644)
		return fmt.Errorf("failed to sync temp WAL: %w", err)
	}

	tmpFile.Close()

	// Atomic rename
	if err := os.Rename(tmpPath, w.walPath); err != nil {
		os.Remove(tmpPath)
		w.walFile, _ = os.OpenFile(w.walPath, os.O_RDWR|os.O_APPEND, 0644)
		return fmt.Errorf("failed to rename temp WAL: %w", err)
	}

	// Reopen for append
	w.walFile, err = os.OpenFile(w.walPath, os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to reopen WAL: %w", err)
	}

	return nil
}

// startCheckpointTimer starts the periodic checkpoint timer.
func (w *WriteAheadLog) startCheckpointTimer() {
	w.checkpointTimer = time.AfterFunc(w.config.CheckpointInterval, func() {
		w.mu.Lock()
		stopped := w.stopped
		w.mu.Unlock()

		if !stopped {
			w.Checkpoint()
			w.startCheckpointTimer()
		}
	})
}

// ReadLine reads a line from the PageStore.
func (w *WriteAheadLog) ReadLine(index int64) (*LogicalLine, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.pageStore.ReadLine(index)
}

// ReadLineRange reads a range of lines from PageStore.
func (w *WriteAheadLog) ReadLineRange(start, end int64) ([]*LogicalLine, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.pageStore.ReadLineRange(start, end)
}

// ReadLineWithTimestamp reads a line and its timestamp.
func (w *WriteAheadLog) ReadLineWithTimestamp(index int64) (*LogicalLine, time.Time, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.pageStore.ReadLineWithTimestamp(index)
}

// LineCount returns the total number of lines.
func (w *WriteAheadLog) LineCount() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.pageStore.LineCount()
}

// GetTimestamp returns the timestamp for a line.
func (w *WriteAheadLog) GetTimestamp(index int64) (time.Time, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.pageStore.GetTimestamp(index)
}

// FindLineAt finds the line closest to the given time.
func (w *WriteAheadLog) FindLineAt(t time.Time) (int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.pageStore.FindLineAt(t)
}

// Path returns the WAL directory path.
func (w *WriteAheadLog) Path() string {
	return w.config.WALDir
}

// WALPath returns the path to the wal.log file.
func (w *WriteAheadLog) WALPath() string {
	return w.walPath
}

// Close performs a final checkpoint and closes the WAL and PageStore.
func (w *WriteAheadLog) Close() error {
	w.mu.Lock()

	if w.stopped {
		w.mu.Unlock()
		return nil
	}

	// Stop checkpoint timer first (before final checkpoint)
	if w.checkpointTimer != nil {
		w.checkpointTimer.Stop()
	}

	// Final checkpoint - must happen BEFORE setting stopped=true
	// because checkpointLocked() returns early if stopped is true
	checkpointErr := w.checkpointLocked()

	// Mark as stopped after checkpoint completes
	w.stopped = true

	w.mu.Unlock()

	// Close stop channel
	close(w.stopCh)

	// Close files
	var walErr, psErr error
	if w.walFile != nil {
		walErr = w.walFile.Close()
	}
	if w.pageStore != nil {
		psErr = w.pageStore.Close()
	}

	if checkpointErr != nil {
		return checkpointErr
	}
	if walErr != nil {
		return walErr
	}
	return psErr
}

// Compile-time interface check
var _ HistoryWriterWithTimestamp = (*WriteAheadLog)(nil)

// AppendLine implements HistoryWriter interface.
func (w *WriteAheadLog) AppendLine(line *LogicalLine) error {
	w.mu.Lock()
	idx := w.nextGlobalIdx
	w.mu.Unlock()

	return w.Append(idx, line, w.nowFunc())
}

// AppendLineWithTimestamp implements HistoryWriterWithTimestamp interface.
func (w *WriteAheadLog) AppendLineWithTimestamp(line *LogicalLine, timestamp time.Time) error {
	w.mu.Lock()
	idx := w.nextGlobalIdx
	w.mu.Unlock()

	return w.Append(idx, line, timestamp)
}

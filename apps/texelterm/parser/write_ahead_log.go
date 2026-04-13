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
	EntryTypeLineWrite        uint8 = 0x01
	EntryTypeLineModify       uint8 = 0x02
	EntryTypeCheckpoint       uint8 = 0x03
	EntryTypeMetadata         uint8 = 0x04 // Legacy ViewportState (scroll position, cursor)
	EntryTypeMainScreenState  uint8 = 0x05 // Sparse MainScreenState (writeTop, cursor globalIdx)
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
	Type            uint8
	GlobalLineIdx   uint64
	Timestamp       time.Time
	Line            *LogicalLine     // nil for CHECKPOINT and METADATA entries
	Metadata        *ViewportState   // nil for non-METADATA entries
	MainScreenState *MainScreenState // nil for non-MAIN_SCREEN_STATE entries
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

	// Recovered MainScreenState from WAL replay (nil if no entry found)
	recoveredMainScreenState *MainScreenState

	// Current metadata (written via WriteMetadata, persisted on checkpoint)
	currentMetadata *ViewportState

	// Current sparse metadata (written via WriteMainScreenState, persisted on checkpoint)
	currentMainScreenState *MainScreenState

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

// PageStore returns the WAL's owned PageStore.
func (w *WriteAheadLog) PageStore() *PageStore {
	return w.pageStore
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

	// Check if we should auto-checkpoint due to size.
	if w.config.CheckpointMaxSize > 0 && w.walSize >= w.config.CheckpointMaxSize {
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

// RecoveredMetadata returns the metadata recovered from WAL replay.
// Returns nil if no metadata was found in the WAL.
func (w *WriteAheadLog) RecoveredMetadata() *ViewportState {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.recoveredMetadata
}

// WriteMainScreenState writes sparse MainScreenState to the WAL.
// Multiple writes are OK — only the last one matters on recovery.
func (w *WriteAheadLog) WriteMainScreenState(state *MainScreenState) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped {
		return fmt.Errorf("WAL is closed")
	}
	if state == nil {
		return nil
	}

	metadataBytes, err := encodeMainScreenState(state)
	if err != nil {
		return fmt.Errorf("failed to encode MainScreenState: %w", err)
	}

	entryData, err := w.encodeMetadataEntryWithType(EntryTypeMainScreenState, metadataBytes, w.nowFunc())
	if err != nil {
		return fmt.Errorf("failed to encode entry: %w", err)
	}

	if _, err := w.walFile.Write(entryData); err != nil {
		return fmt.Errorf("failed to write MainScreenState entry: %w", err)
	}

	w.walSize += int64(len(entryData))
	w.entriesWritten++
	w.currentMainScreenState = state
	return nil
}

// RecoveredMainScreenState returns the MainScreenState recovered from WAL replay.
// Returns nil if no MainScreenState entry was found.
func (w *WriteAheadLog) RecoveredMainScreenState() *MainScreenState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.recoveredMainScreenState
}

// encodeMainScreenState serializes MainScreenState to bytes.
// Layout: WriteTop(8) ContentEnd(8) CursorGlobalIdx(8) CursorCol(4)
//
//	PromptStartLine(8) SavedAt(8) CWDLen(2) CWD(variable)
func encodeMainScreenState(state *MainScreenState) ([]byte, error) {
	cwdBytes := []byte(state.WorkingDir)
	totalSize := 8 + 8 + 8 + 4 + 8 + 8 + 2 + len(cwdBytes)
	buf := make([]byte, totalSize)
	binary.LittleEndian.PutUint64(buf[0:8], uint64(state.WriteTop))
	binary.LittleEndian.PutUint64(buf[8:16], uint64(state.ContentEnd))
	binary.LittleEndian.PutUint64(buf[16:24], uint64(state.CursorGlobalIdx))
	binary.LittleEndian.PutUint32(buf[24:28], uint32(state.CursorCol))
	binary.LittleEndian.PutUint64(buf[28:36], uint64(state.PromptStartLine))
	binary.LittleEndian.PutUint64(buf[36:44], uint64(state.SavedAt.UnixNano()))
	binary.LittleEndian.PutUint16(buf[44:46], uint16(len(cwdBytes)))
	copy(buf[46:], cwdBytes)
	return buf, nil
}

// decodeMainScreenState deserializes MainScreenState from bytes.
func decodeMainScreenState(data []byte) (*MainScreenState, error) {
	if len(data) < 46 {
		return nil, fmt.Errorf("MainScreenState data too short: %d bytes", len(data))
	}
	state := &MainScreenState{
		WriteTop:        int64(binary.LittleEndian.Uint64(data[0:8])),
		ContentEnd:      int64(binary.LittleEndian.Uint64(data[8:16])),
		CursorGlobalIdx: int64(binary.LittleEndian.Uint64(data[16:24])),
		CursorCol:       int(binary.LittleEndian.Uint32(data[24:28])),
		PromptStartLine: int64(binary.LittleEndian.Uint64(data[28:36])),
		SavedAt:         time.Unix(0, int64(binary.LittleEndian.Uint64(data[36:44]))),
	}
	cwdLen := int(binary.LittleEndian.Uint16(data[44:46]))
	if len(data) >= 46+cwdLen {
		state.WorkingDir = string(data[46 : 46+cwdLen])
	}
	return state, nil
}

// encodeMetadataEntryWithType is like encodeMetadataEntry but uses the given type byte.
func (w *WriteAheadLog) encodeMetadataEntryWithType(entryType uint8, metadataBytes []byte, timestamp time.Time) ([]byte, error) {
	totalSize := WALEntryBase + len(metadataBytes)
	buf := make([]byte, totalSize)
	buf[0] = entryType
	binary.LittleEndian.PutUint64(buf[1:9], 0) // GlobalLineIdx reserved=0
	binary.LittleEndian.PutUint64(buf[9:17], uint64(timestamp.UnixNano()))
	binary.LittleEndian.PutUint32(buf[17:21], uint32(len(metadataBytes)))
	if len(metadataBytes) > 0 {
		copy(buf[21:21+len(metadataBytes)], metadataBytes)
	}
	crc := crc32.ChecksumIEEE(buf[:totalSize-4])
	binary.LittleEndian.PutUint32(buf[totalSize-4:], crc)
	return buf, nil
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
// Format: 32 bytes (original) + 8 bytes PromptStartLine + 2 bytes CWD length + CWD string.
func encodeViewportState(state *ViewportState) ([]byte, error) {
	cwdBytes := []byte(state.WorkingDir)
	totalSize := 32 + 8 + 2 + len(cwdBytes)
	buf := make([]byte, totalSize)
	// Original 32 bytes
	binary.LittleEndian.PutUint64(buf[0:8], uint64(state.ScrollOffset))
	binary.LittleEndian.PutUint64(buf[8:16], uint64(state.LiveEdgeBase))
	binary.LittleEndian.PutUint32(buf[16:20], uint32(state.CursorX))
	binary.LittleEndian.PutUint32(buf[20:24], uint32(state.CursorY))
	binary.LittleEndian.PutUint64(buf[24:32], uint64(state.SavedAt.UnixNano()))
	// New fields
	binary.LittleEndian.PutUint64(buf[32:40], uint64(state.PromptStartLine))
	binary.LittleEndian.PutUint16(buf[40:42], uint16(len(cwdBytes)))
	copy(buf[42:], cwdBytes)
	return buf, nil
}

// decodeViewportState deserializes ViewportState from bytes.
// Backward-compatible: old 32-byte payloads decode with PromptStartLine=-1, WorkingDir="".
func decodeViewportState(data []byte) (*ViewportState, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("metadata too short: %d bytes", len(data))
	}
	state := &ViewportState{
		ScrollOffset:    int64(binary.LittleEndian.Uint64(data[0:8])),
		LiveEdgeBase:    int64(binary.LittleEndian.Uint64(data[8:16])),
		CursorX:         int(binary.LittleEndian.Uint32(data[16:20])),
		CursorY:         int(binary.LittleEndian.Uint32(data[20:24])),
		SavedAt:         time.Unix(0, int64(binary.LittleEndian.Uint64(data[24:32]))),
		PromptStartLine: -1, // default for old format
	}
	// Extended fields (bytes 32+)
	if len(data) >= 40 {
		state.PromptStartLine = int64(binary.LittleEndian.Uint64(data[32:40]))
	}
	if len(data) >= 42 {
		cwdLen := int(binary.LittleEndian.Uint16(data[40:42]))
		if len(data) >= 42+cwdLen {
			state.WorkingDir = string(data[42 : 42+cwdLen])
		}
	}
	return state, nil
}

// ReadWALWorkingDir reads the last known working directory from a WAL file.
// This is a standalone read-only function that can be called before the full WAL is opened.
// Returns empty string if WAL doesn't exist or has no CWD.
func ReadWALWorkingDir(basePath, terminalID string) string {
	cfg := DefaultWALConfig(basePath, terminalID)
	walPath := filepath.Join(cfg.WALDir, "wal.log")

	f, err := os.Open(walPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	// Skip header
	if _, err := f.Seek(WALHeaderSize, io.SeekStart); err != nil {
		return ""
	}

	reader := bufio.NewReader(f)
	var lastCWD string

	// Scan all entries looking for the last metadata with a CWD
	for {
		entry, err := readEntryStandalone(reader)
		if err != nil {
			break
		}
		if entry.Metadata != nil && entry.Metadata.WorkingDir != "" {
			lastCWD = entry.Metadata.WorkingDir
		}
	}
	return lastCWD
}

// readEntryStandalone reads a single WAL entry without requiring a WriteAheadLog instance.
func readEntryStandalone(r *bufio.Reader) (WALEntry, error) {
	headerBuf := make([]byte, 21)
	if _, err := io.ReadFull(r, headerBuf); err != nil {
		return WALEntry{}, err
	}

	entryType := headerBuf[0]
	lineIdx := binary.LittleEndian.Uint64(headerBuf[1:9])
	timestamp := time.Unix(0, int64(binary.LittleEndian.Uint64(headerBuf[9:17])))
	dataLen := binary.LittleEndian.Uint32(headerBuf[17:21])

	var lineData []byte
	if dataLen > 0 {
		lineData = make([]byte, dataLen)
		if _, err := io.ReadFull(r, lineData); err != nil {
			return WALEntry{}, err
		}
	}

	// Read and verify CRC
	crcBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, crcBuf); err != nil {
		return WALEntry{}, err
	}
	storedCRC := binary.LittleEndian.Uint32(crcBuf)

	totalSize := 21 + int(dataLen) + 4
	fullEntry := make([]byte, totalSize)
	copy(fullEntry[0:21], headerBuf)
	if dataLen > 0 {
		copy(fullEntry[21:21+dataLen], lineData)
	}
	copy(fullEntry[totalSize-4:], crcBuf)

	expectedCRC := crc32.ChecksumIEEE(fullEntry[:totalSize-4])
	if storedCRC != expectedCRC {
		return WALEntry{}, fmt.Errorf("CRC mismatch")
	}

	var metadata *ViewportState
	if entryType == EntryTypeMetadata && dataLen > 0 {
		metadata, _ = decodeViewportState(lineData)
	}

	return WALEntry{
		Type:          entryType,
		GlobalLineIdx: lineIdx,
		Timestamp:     timestamp,
		Metadata:      metadata,
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
	var lastMetadata *ViewportState     // Track last legacy metadata entry
	var lastMainScreenState *MainScreenState // Track last sparse metadata entry

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
			lastMainScreenState = nil
			w.lastCheckpoint = entry.GlobalLineIdx
		} else if entry.Type == EntryTypeMetadata {
			// Track legacy metadata entries (last one wins)
			lastMetadata = entry.Metadata
		} else if entry.Type == EntryTypeMainScreenState {
			// Track sparse metadata entries (last one wins)
			lastMainScreenState = entry.MainScreenState
		} else {
			entries = append(entries, entry)
		}
	}

	// Store recovered metadata
	w.recoveredMetadata = lastMetadata
	w.recoveredMainScreenState = lastMainScreenState

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

	// Replay entries to PageStore in WAL order using the unified write path.
	// AppendLineWithGlobalIdx handles append, update-in-place, and out-of-order
	// insert in one operation, so the LATEST WAL entry for each globalIdx wins.
	for _, entry := range entries {
		if entry.Line == nil {
			continue
		}
		if entry.Type != EntryTypeLineWrite && entry.Type != EntryTypeLineModify {
			continue
		}
		lineIdx := int64(entry.GlobalLineIdx)
		if err := w.pageStore.AppendLineWithGlobalIdx(lineIdx, entry.Line, entry.Timestamp); err != nil {
			return fmt.Errorf("failed to replay line %d: %w", entry.GlobalLineIdx, err)
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
	var mainScreenState *MainScreenState

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
	case EntryTypeMainScreenState:
		if dataLen > 0 {
			mainScreenState, err = decodeMainScreenState(lineData)
			if err != nil {
				return WALEntry{}, totalSize, fmt.Errorf("failed to decode MainScreenState: %w", err)
			}
		}
	case EntryTypeCheckpoint:
		// No data to decode
	}

	return WALEntry{
		Type:            entryType,
		GlobalLineIdx:   lineIdx,
		Timestamp:       timestamp,
		Line:            line,
		Metadata:        metadata,
		MainScreenState: mainScreenState,
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

	// Replay entries to PageStore in WAL order using the unified write
	// path. AppendLineWithGlobalIdx now supports out-of-order inserts and
	// updates, so the previous "Pass 1: appends, Pass 2: modifies" split
	// is unnecessary. Replaying in WAL order means the LATEST entry for
	// each globalIdx wins, which is the correct semantic.
	for _, entry := range entries {
		if entry.Line == nil {
			continue
		}
		if entry.Type != EntryTypeLineWrite && entry.Type != EntryTypeLineModify {
			continue
		}
		lineIdx := int64(entry.GlobalLineIdx)
		if err := w.pageStore.AppendLineWithGlobalIdx(lineIdx, entry.Line, entry.Timestamp); err != nil {
			return fmt.Errorf("failed to write line %d to PageStore: %w", entry.GlobalLineIdx, err)
		}
	}

	// Flush PageStore to disk
	if err := w.pageStore.Flush(); err != nil {
		return fmt.Errorf("failed to flush PageStore: %w", err)
	}

	// Update current metadata from WAL entries if present
	// This ensures RecoveredMetadata returns the right value after checkpoint
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

	// Re-write current metadata to the fresh WAL so it survives checkpoint.
	// This is necessary because the metadata entries were cleared with the WAL.
	if w.currentMetadata != nil {
		if err := w.writeMetadataLocked(w.currentMetadata); err != nil {
			return fmt.Errorf("failed to persist metadata after checkpoint: %w", err)
		}
	}
	if w.currentMainScreenState != nil {
		mssBytes, err := encodeMainScreenState(w.currentMainScreenState)
		if err == nil {
			entryData, err2 := w.encodeMetadataEntryWithType(EntryTypeMainScreenState, mssBytes, w.nowFunc())
			if err2 == nil {
				_, _ = w.walFile.Write(entryData)
				w.walSize += int64(len(entryData))
				w.entriesWritten++
			}
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
			// Track last legacy metadata entry (don't add to entries list)
			lastMetadata = entry.Metadata
		case EntryTypeMainScreenState:
			// Track last sparse metadata entry; also update currentMainScreenState
			w.currentMainScreenState = entry.MainScreenState
		default:
			// LINE_WRITE and LINE_MODIFY entries
			entries = append(entries, entry)
		}
	}

	// Also update lastMetadata with checkpoint-persisted currentMetadata
	if lastMetadata == nil {
		lastMetadata = w.currentMetadata
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

// NextGlobalIdx returns the next line index the WAL expects.
// This equals the total number of lines written (PageStore + WAL entries).
func (w *WriteAheadLog) NextGlobalIdx() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Include lines already in PageStore
	psCount := int64(0)
	if w.pageStore != nil {
		psCount = w.pageStore.LineCount()
	}
	if w.nextGlobalIdx > psCount {
		return w.nextGlobalIdx
	}
	return psCount
}

// SyncWAL forces the WAL file to be synced to disk.
// This ensures all previously written entries survive a crash.
func (w *WriteAheadLog) SyncWAL() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped || w.walFile == nil {
		return nil
	}
	return w.walFile.Sync()
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

// AppendLineWithGlobalIdx implements HistoryWriter interface.
func (w *WriteAheadLog) AppendLineWithGlobalIdx(globalIdx int64, line *LogicalLine, timestamp time.Time) error {
	return w.Append(globalIdx, line, timestamp)
}

// Compile-time interface check
var _ HistoryWriter = (*WriteAheadLog)(nil)

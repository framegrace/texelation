// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/history.go
// Summary: Implements infinite history management for terminal emulator.
// Usage: Manages large in-memory buffer and coordinates with disk persistence.
// Notes: Designed for encryption and privacy from the start.

package parser

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// DefaultMemoryLines is the default number of lines to keep in memory
	DefaultMemoryLines    = 100000
	defaultFlushInterval  = 5 * time.Second
	defaultCompress       = true
	defaultPersistEnabled = true
)

// HistoryConfig holds configuration for history management.
type HistoryConfig struct {
	MemoryLines      int           // Maximum lines to keep in memory
	PersistEnabled   bool          // Enable file persistence
	PersistDir       string        // Base directory for history files
	Compress         bool          // Enable gzip compression
	Encrypt          bool          // Enable encryption (future)
	FlushInterval    time.Duration // How often to flush to disk
	RespectMarkers   bool          // Respect privacy OSC sequences
	RedactPatterns   []string      // Regex patterns to redact
	EncryptionKey    []byte        // Encryption key (future)
}

// DefaultHistoryConfig returns default configuration.
func DefaultHistoryConfig() HistoryConfig {
	homeDir, _ := os.UserHomeDir()
	// Use ~/.texelation for scrollback persistence (simpler than .local/share/texelation)
	persistDir := filepath.Join(homeDir, ".texelation")

	return HistoryConfig{
		MemoryLines:      DefaultMemoryLines,
		PersistEnabled:   defaultPersistEnabled,
		PersistDir:       persistDir,
		Compress:         defaultCompress,
		Encrypt:          false,
		FlushInterval:    defaultFlushInterval,
		RespectMarkers:   true,
		RedactPatterns:   []string{},
	}
}

// SessionMetadata holds metadata about a terminal session.
type SessionMetadata struct {
	SessionID   string    `json:"session_id"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time,omitempty"`
	Command     string    `json:"command"`
	WorkingDir  string    `json:"working_dir"`
	Hostname    string    `json:"hostname"`
	Username    string    `json:"username"`
	LineCount   int       `json:"line_count"`
	FileSize    int64     `json:"file_size"`
	Encrypted   bool      `json:"encrypted"`
	PrivacyGaps int       `json:"privacy_gaps"` // Number of privacy mode blocks
}

// NewSessionMetadata creates metadata for a new session.
// If paneID is empty, generates a new UUID (for backwards compatibility).
func NewSessionMetadata(command, workingDir, paneID string) SessionMetadata {
	hostname, _ := os.Hostname()
	username := "unknown"
	if u, err := user.Current(); err == nil {
		username = u.Username
	}

	sessionID := paneID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	return SessionMetadata{
		SessionID:   sessionID,
		StartTime:   time.Now(),
		Command:     command,
		WorkingDir:  workingDir,
		Hostname:    hostname,
		Username:    username,
		LineCount:   0,
		FileSize:    0,
		Encrypted:   false,
		PrivacyGaps: 0,
	}
}

// HistoryManager manages in-memory circular buffer and coordinates persistence.
type HistoryManager struct {
	// In-memory circular buffer
	buffer  [][]Cell
	maxSize int
	head    int
	length  int

	// Session metadata
	metadata SessionMetadata

	// Privacy control
	privacyMode      bool
	privacyModeDepth int
	privacyGaps      int

	// Persistence
	store   *HistoryStore
	enabled bool
	config  HistoryConfig

	// Flush management
	flushTimer    *time.Timer
	stopFlush     chan struct{}
	pendingLines  [][]Cell
	lastFlushTime time.Time

	// Synchronization
	mu sync.RWMutex
}

// NewHistoryManager creates a new history manager.
// paneID should be the hex-encoded pane ID for persistent scrollback across restarts.
func NewHistoryManager(config HistoryConfig, command, workingDir, paneID string) (*HistoryManager, error) {
	metadata := NewSessionMetadata(command, workingDir, paneID)

	hm := &HistoryManager{
		buffer:        make([][]Cell, config.MemoryLines),
		maxSize:       config.MemoryLines,
		head:          0,
		length:        0,
		metadata:      metadata,
		privacyMode:   false,
		enabled:       config.PersistEnabled,
		config:        config,
		stopFlush:     make(chan struct{}),
		pendingLines:  make([][]Cell, 0, 100),
		lastFlushTime: time.Now(),
	}

	// Load existing history if persistence is enabled
	var existingLines [][]Cell
	if config.PersistEnabled {
		// Construct the session file path the same way NewHistoryStore does
		scrollbackDir := filepath.Join(config.PersistDir, "scrollback")
		ext := ".hist"
		if config.Encrypt {
			ext += ".enc"
		}
		sessionFile := filepath.Join(scrollbackDir, metadata.SessionID+ext)

		// Try to load existing history
		lines, err := LoadHistoryLines(sessionFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load existing history: %v\n", err)
		} else if len(lines) > 0 {
			existingLines = lines
			fmt.Fprintf(os.Stderr, "Loaded %d lines from existing history file\n", len(lines))
		}
	}

	// Initialize persistent storage if enabled
	if config.PersistEnabled {
		store, err := NewHistoryStore(config, metadata)
		if err != nil {
			// Log error but don't fail - degrade to memory-only
			fmt.Fprintf(os.Stderr, "Failed to initialize history storage: %v\n", err)
			hm.enabled = false
		} else {
			hm.store = store
			// Start periodic flush timer
			go hm.flushLoop()
		}
	}

	// Populate buffer with existing history
	if len(existingLines) > 0 {
		hm.ReplaceBuffer(existingLines)
	}

	return hm, nil
}

// AppendLine adds a new line to the history buffer.
// If persistence is enabled and not in privacy mode, queues for disk write.
func (hm *HistoryManager) AppendLine(line []Cell) {
	hm.mu.Lock()
	defer hm.mu.Unlock()


	// Make a copy of the line to avoid mutation
	lineCopy := make([]Cell, len(line))
	copy(lineCopy, line)

	// Add to circular buffer
	if hm.length < hm.maxSize {
		physicalIndex := (hm.head + hm.length) % hm.maxSize
		hm.buffer[physicalIndex] = lineCopy
		hm.length++
	} else {
		// Buffer is full, wrap around
		hm.head = (hm.head + 1) % hm.maxSize
		physicalIndex := (hm.head + hm.length - 1) % hm.maxSize
		hm.buffer[physicalIndex] = lineCopy
	}

	// Queue for persistence if enabled and not in privacy mode
	if hm.enabled && hm.store != nil && !hm.privacyMode {
		hm.pendingLines = append(hm.pendingLines, lineCopy)
	}

	hm.metadata.LineCount++
}

// GetLine retrieves a line from the history buffer.
// index is a logical index from 0 to Length()-1.
func (hm *HistoryManager) GetLine(index int) []Cell {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	if index < 0 || index >= hm.length {
		return nil
	}

	physicalIndex := (hm.head + index) % hm.maxSize
	return hm.buffer[physicalIndex]
}

// SetLine updates an existing line in the history buffer.
// This modifies only the in-memory buffer, not the persisted history.
// Used for operations that modify visible lines (scrolling, editing, etc.).
func (hm *HistoryManager) SetLine(index int, line []Cell) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	if index < 0 || index >= hm.length {
		return
	}

	physicalIndex := (hm.head + index) % hm.maxSize
	hm.buffer[physicalIndex] = line
}

// Length returns the total number of lines in history.
func (hm *HistoryManager) Length() int {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hm.length
}

// EnablePrivacyMode stops persisting new lines to disk.
// Lines are still kept in memory.
func (hm *HistoryManager) EnablePrivacyMode() {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	if !hm.privacyMode {
		hm.privacyMode = true
		hm.privacyGaps++
		hm.metadata.PrivacyGaps = hm.privacyGaps
		// Flush pending lines before entering privacy mode
		if hm.enabled && hm.store != nil {
			hm.flushPendingLinesLocked()
		}
	}
	hm.privacyModeDepth++
}

// DisablePrivacyMode resumes persisting lines to disk.
func (hm *HistoryManager) DisablePrivacyMode() {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	if hm.privacyModeDepth > 0 {
		hm.privacyModeDepth--
	}
	if hm.privacyModeDepth == 0 {
		hm.privacyMode = false
	}
}

// IsPrivacyMode returns whether privacy mode is currently active.
func (hm *HistoryManager) IsPrivacyMode() bool {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hm.privacyMode
}

// Flush writes all pending lines to disk immediately.
func (hm *HistoryManager) Flush() error {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	return hm.flushPendingLinesLocked()
}

// flushPendingLinesLocked writes pending lines to store (caller must hold lock).
func (hm *HistoryManager) flushPendingLinesLocked() error {
	if !hm.enabled || hm.store == nil || len(hm.pendingLines) == 0 {
		return nil
	}

	err := hm.store.WriteLines(hm.pendingLines)
	if err != nil {
		return fmt.Errorf("failed to write history: %w", err)
	}

	hm.pendingLines = hm.pendingLines[:0] // Clear pending
	hm.lastFlushTime = time.Now()
	return nil
}

// flushLoop periodically flushes pending lines to disk.
func (hm *HistoryManager) flushLoop() {
	ticker := time.NewTicker(hm.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hm.mu.Lock()
			if len(hm.pendingLines) > 0 {
				if err := hm.flushPendingLinesLocked(); err != nil {
					fmt.Fprintf(os.Stderr, "History flush error: %v\n", err)
				}
			}
			hm.mu.Unlock()

		case <-hm.stopFlush:
			return
		}
	}
}

// Close finalizes the session and closes files.
func (hm *HistoryManager) Close() error {
	// Stop flush loop
	close(hm.stopFlush)

	hm.mu.Lock()
	defer hm.mu.Unlock()

	// Close store
	if hm.store != nil {
		// Close the current store to flush buffers
		hm.metadata.EndTime = time.Now()
		hm.metadata.PrivacyGaps = hm.privacyGaps

		if err := hm.store.Close(hm.metadata); err != nil {
			return fmt.Errorf("failed to close history store: %w", err)
		}

		// Reopen the history file and write the ENTIRE buffer
		// This ensures we capture all in-place updates made via SetLine()
		allLines := make([][]Cell, hm.length)
		for i := 0; i < hm.length; i++ {
			physicalIndex := (hm.head + i) % hm.maxSize
			allLines[i] = hm.buffer[physicalIndex]
		}

		// Rewrite the history file with complete buffer
		if err := hm.rewriteHistoryFile(allLines); err != nil {
			return fmt.Errorf("failed to rewrite history file: %w", err)
		}
	}

	return nil
}

// rewriteHistoryFile rewrites the entire history file with the given lines.
// Called during Close() to ensure all in-memory updates are persisted.
func (hm *HistoryManager) rewriteHistoryFile(lines [][]Cell) error {
	// Construct the history file path
	scrollbackDir := filepath.Join(hm.config.PersistDir, "scrollback")
	ext := ".hist"
	sessionFile := filepath.Join(scrollbackDir, hm.metadata.SessionID+ext)

	// Delete the old file so NewHistoryStore creates a fresh one
	os.Remove(sessionFile)

	// Recreate the store (will create empty file since we deleted it)
	store, err := NewHistoryStore(hm.config, hm.metadata)
	if err != nil {
		return fmt.Errorf("failed to create new history store: %w", err)
	}

	// Write all lines
	if err := store.WriteLines(lines); err != nil {
		store.Close(hm.metadata) // Try to close cleanly
		return fmt.Errorf("failed to write lines: %w", err)
	}

	// Update metadata
	hm.metadata.LineCount = store.LineCount()
	hm.metadata.FileSize = store.BytesWritten()

	// Close the store
	if err := store.Close(hm.metadata); err != nil {
		return fmt.Errorf("failed to close rewritten store: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[REWRITE DEBUG] Rewrote history file with %d lines\n", len(lines))
	return nil
}

// GetMetadata returns the session metadata.
func (hm *HistoryManager) GetMetadata() SessionMetadata {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hm.metadata
}

// ReplaceBuffer replaces the entire history buffer with new content (used during reflow).
// This is used when terminal width changes and lines need to be re-wrapped.
func (hm *HistoryManager) ReplaceBuffer(newLines [][]Cell) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	// DEBUG: Log buffer replacement
	oldLen := hm.length
	newLen := len(newLines)
	fmt.Fprintf(os.Stderr, "[REPLACE DEBUG] ReplaceBuffer called: oldLen=%d, newLen=%d\n", oldLen, newLen)

	hm.head = 0
	hm.length = len(newLines)

	// Keep only the most recent lines if they exceed buffer size
	if hm.length > hm.maxSize {
		offset := hm.length - hm.maxSize
		for i := 0; i < hm.maxSize; i++ {
			hm.buffer[i] = newLines[offset+i]
		}
		hm.length = hm.maxSize
	} else {
		for i := 0; i < hm.length; i++ {
			hm.buffer[i] = newLines[i]
		}
	}
}

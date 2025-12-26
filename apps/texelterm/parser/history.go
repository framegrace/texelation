// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/history.go
// Summary: History configuration for terminal emulator scrollback.
// Usage: Provides configuration for the display buffer and disk persistence.

package parser

import (
	"os"
	"path/filepath"
	"time"
)

const (
	// DefaultMemoryLines is the default number of lines to keep in memory
	DefaultMemoryLines = 100000
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
	// Use ~/.texelation for scrollback persistence
	persistDir := filepath.Join(homeDir, ".texelation")

	return HistoryConfig{
		MemoryLines:      DefaultMemoryLines,
		PersistEnabled:   true,
		PersistDir:       persistDir,
		Compress:         true,
		Encrypt:          false,
		FlushInterval:    5 * time.Second,
		RespectMarkers:   true,
		RedactPatterns:   []string{},
	}
}

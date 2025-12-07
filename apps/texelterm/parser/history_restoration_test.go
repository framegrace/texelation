// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// TestScrollbackPersistence tests that scrollback is correctly saved and restored
func TestScrollbackPersistence(t *testing.T) {
	// Create temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.hist")

	// Write test file with header + 3 test lines
	f, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer f.Close()

	// Write header (magic 8 bytes + flags 4 bytes = 12 bytes total)
	header := make([]byte, len(historyMagic)+4)
	copy(header, []byte(historyMagic))
	// Flags at offset 8 (4 bytes)
	binary.LittleEndian.PutUint32(header[8:12], 0) // No flags
	if _, err := f.Write(header); err != nil {
		t.Fatalf("Failed to write header: %v", err)
	}

	// Write 3 test lines
	testLines := []string{
		"TEST LINE 1",
		"TEST LINE 2",
		"TEST LINE 3",
	}

	for _, line := range testLines {
		// Write line length
		lineLen := uint32(len(line))
		lenBuf := make([]byte, 4)
		binary.LittleEndian.PutUint32(lenBuf, lineLen)
		if _, err := f.Write(lenBuf); err != nil {
			t.Fatalf("Failed to write line length: %v", err)
		}

		// Write cells
		for _, ch := range line {
			cellBuf := make([]byte, cellEncodedSize)
			// Rune (4 bytes)
			binary.LittleEndian.PutUint32(cellBuf[0:4], uint32(ch))
			// FG mode (1 byte) = 0 (default)
			cellBuf[4] = 0
			// FG value (4 bytes) = 7 (white)
			binary.LittleEndian.PutUint32(cellBuf[5:9], 7)
			// BG mode (1 byte) = 0 (default)
			cellBuf[9] = 0
			// BG value (4 bytes) = 0 (black)
			binary.LittleEndian.PutUint32(cellBuf[10:14], 0)
			// Attributes (1 byte) = 0
			cellBuf[14] = 0
			// Wrapped (1 byte) = 0
			cellBuf[15] = 0
			// Padding (2 bytes)
			cellBuf[16] = 0
			cellBuf[17] = 0

			if _, err := f.Write(cellBuf); err != nil {
				t.Fatalf("Failed to write cell: %v", err)
			}
		}
	}

	if err := f.Sync(); err != nil {
		t.Fatalf("Failed to sync file: %v", err)
	}
	f.Close()

	// Now test LoadHistoryLines
	lines, err := LoadHistoryLines(testFile)
	if err != nil {
		t.Fatalf("LoadHistoryLines failed: %v", err)
	}

	if lines == nil {
		t.Fatal("LoadHistoryLines returned nil")
	}

	if len(lines) != 3 {
		t.Fatalf("Expected 3 lines, got %d", len(lines))
	}

	// Verify content
	for i, expected := range testLines {
		actual := ""
		for _, cell := range lines[i] {
			actual += string(cell.Rune)
		}
		if actual != expected {
			t.Errorf("Line %d mismatch: expected '%s', got '%s'", i, expected, actual)
		}
	}

	t.Logf("✓ Successfully loaded and verified %d lines of scrollback", len(lines))
}

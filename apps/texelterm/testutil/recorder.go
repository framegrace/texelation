// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/testutil/recorder.go
// Summary: Recording format and capture utilities for terminal comparison testing.
//
// Format: TXREC01
// A simple text-based format with metadata header and raw escape sequences.
//
// Example:
//   TXREC01
//   width: 80
//   height: 24
//   shell: bash -c "ls -la"
//   description: List directory contents
//   ---
//   <raw escape sequences and text>

package testutil

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	RecordingMagic   = "TXREC01"
	RecordingSep     = "---"
	DefaultWidth     = 80
	DefaultHeight    = 24
)

// Recording represents a captured terminal session.
type Recording struct {
	Metadata  RecordingMetadata
	Sequences []byte // Raw PTY output (escape sequences + text)
}

// RecordingMetadata holds information about the recording.
type RecordingMetadata struct {
	Width       int
	Height      int
	Shell       string
	Description string
	Timestamp   time.Time
}

// NewRecording creates a new empty recording with default dimensions.
func NewRecording(width, height int) *Recording {
	return &Recording{
		Metadata: RecordingMetadata{
			Width:     width,
			Height:    height,
			Timestamp: time.Now(),
		},
		Sequences: nil,
	}
}

// NewRecordingFromBytes creates a recording from raw escape sequence bytes.
// Useful for creating synthetic test cases.
func NewRecordingFromBytes(data []byte, width, height int) *Recording {
	return &Recording{
		Metadata: RecordingMetadata{
			Width:       width,
			Height:      height,
			Description: "synthetic",
			Timestamp:   time.Now(),
		},
		Sequences: data,
	}
}

// NewRecordingFromString creates a recording from a string (convenience wrapper).
func NewRecordingFromString(data string, width, height int) *Recording {
	return NewRecordingFromBytes([]byte(data), width, height)
}

// LoadRecording loads a recording from a TXREC01 file.
func LoadRecording(path string) (*Recording, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open recording file: %w", err)
	}
	defer f.Close()
	return ParseRecording(f)
}

// ParseRecording parses a recording from a reader.
func ParseRecording(r io.Reader) (*Recording, error) {
	reader := bufio.NewReader(r)

	// Read magic line
	magic, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read magic: %w", err)
	}
	magic = strings.TrimSpace(magic)
	if magic != RecordingMagic {
		return nil, fmt.Errorf("invalid magic: expected %q, got %q", RecordingMagic, magic)
	}

	rec := &Recording{
		Metadata: RecordingMetadata{
			Width:  DefaultWidth,
			Height: DefaultHeight,
		},
	}

	// Read metadata until separator
	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read metadata: %w", err)
		}

		line = strings.TrimSpace(line)
		if line == RecordingSep {
			break
		}

		// Parse key: value
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue // Skip malformed lines
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "width":
			if w, err := strconv.Atoi(value); err == nil {
				rec.Metadata.Width = w
			}
		case "height":
			if h, err := strconv.Atoi(value); err == nil {
				rec.Metadata.Height = h
			}
		case "shell":
			rec.Metadata.Shell = value
		case "description":
			rec.Metadata.Description = value
		case "timestamp":
			if t, err := time.Parse(time.RFC3339, value); err == nil {
				rec.Metadata.Timestamp = t
			}
		}
	}

	// Read remaining bytes as sequences
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		return nil, fmt.Errorf("read sequences: %w", err)
	}
	rec.Sequences = buf.Bytes()

	return rec, nil
}

// Save writes the recording to a TXREC01 file.
func (r *Recording) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create recording file: %w", err)
	}
	defer f.Close()
	return r.Write(f)
}

// Write writes the recording to a writer.
func (r *Recording) Write(w io.Writer) error {
	// Write magic
	if _, err := fmt.Fprintln(w, RecordingMagic); err != nil {
		return err
	}

	// Write metadata
	if _, err := fmt.Fprintf(w, "width: %d\n", r.Metadata.Width); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "height: %d\n", r.Metadata.Height); err != nil {
		return err
	}
	if r.Metadata.Shell != "" {
		if _, err := fmt.Fprintf(w, "shell: %s\n", r.Metadata.Shell); err != nil {
			return err
		}
	}
	if r.Metadata.Description != "" {
		if _, err := fmt.Fprintf(w, "description: %s\n", r.Metadata.Description); err != nil {
			return err
		}
	}
	if !r.Metadata.Timestamp.IsZero() {
		if _, err := fmt.Fprintf(w, "timestamp: %s\n", r.Metadata.Timestamp.Format(time.RFC3339)); err != nil {
			return err
		}
	}

	// Write separator
	if _, err := fmt.Fprintln(w, RecordingSep); err != nil {
		return err
	}

	// Write sequences
	if _, err := w.Write(r.Sequences); err != nil {
		return err
	}

	return nil
}

// CaptureCommand runs a shell command and captures its PTY output.
// Uses the `script` command for capture.
//
// Example:
//
//	rec, err := CaptureCommand("ls -la", 80, 24)
func CaptureCommand(shellCmd string, width, height int) (*Recording, error) {
	// Create temp file for script output
	tmpFile, err := os.CreateTemp("", "txrec-*.txt")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Build script command
	// script -q -c "command" output.txt
	// -q: quiet (no start/done messages)
	// -c: command to run
	cmd := exec.Command("script", "-q", "-c", shellCmd, tmpPath)

	// Set terminal size via environment
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("COLUMNS=%d", width),
		fmt.Sprintf("LINES=%d", height),
	)

	// Run the command
	if err := cmd.Run(); err != nil {
		// script may return non-zero if the command fails, but we still want the output
		// Only fail if script itself failed to run
		if _, ok := err.(*exec.ExitError); !ok {
			return nil, fmt.Errorf("run script: %w", err)
		}
	}

	// Read captured output
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("read script output: %w", err)
	}

	// Strip script header/footer if present
	data = stripScriptWrapper(data)

	return &Recording{
		Metadata: RecordingMetadata{
			Width:       width,
			Height:      height,
			Shell:       shellCmd,
			Description: fmt.Sprintf("Captured: %s", shellCmd),
			Timestamp:   time.Now(),
		},
		Sequences: data,
	}, nil
}

// stripScriptWrapper removes the "Script started" and "Script done" lines
// that some versions of script add.
func stripScriptWrapper(data []byte) []byte {
	lines := bytes.Split(data, []byte("\n"))
	var result [][]byte

	for _, line := range lines {
		lineStr := string(line)
		if strings.HasPrefix(lineStr, "Script started") ||
			strings.HasPrefix(lineStr, "Script done") {
			continue
		}
		result = append(result, line)
	}

	return bytes.Join(result, []byte("\n"))
}

// AppendSequence adds raw bytes to the recording.
func (r *Recording) AppendSequence(data []byte) {
	r.Sequences = append(r.Sequences, data...)
}

// AppendString adds a string to the recording (convenience wrapper).
func (r *Recording) AppendString(s string) {
	r.AppendSequence([]byte(s))
}

// AppendCSI adds a CSI sequence (ESC [ ...) to the recording.
func (r *Recording) AppendCSI(params string) {
	r.AppendString("\x1b[" + params)
}

// AppendText adds printable text to the recording.
func (r *Recording) AppendText(text string) {
	r.AppendString(text)
}

// AppendLF adds a line feed to the recording.
func (r *Recording) AppendLF() {
	r.AppendString("\n")
}

// AppendCR adds a carriage return to the recording.
func (r *Recording) AppendCR() {
	r.AppendString("\r")
}

// AppendCRLF adds CR+LF to the recording.
func (r *Recording) AppendCRLF() {
	r.AppendString("\r\n")
}

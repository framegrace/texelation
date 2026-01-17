// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/testutil/debug_capture.go
// Summary: Helper for capturing terminal sequences from live sessions.
//
// Usage:
//   1. Run: TEXELTERM_CAPTURE=/tmp/capture.txrec ./bin/texelterm
//   2. Run your app (e.g., codex), reproduce the issue
//   3. Exit texelterm
//   4. Analyze with: go run ./apps/texelterm/testutil/cmd/analyze /tmp/capture.txrec

package testutil

import (
	"os"
	"sync"
)

// CaptureWriter wraps an io.Writer and captures all written data.
type CaptureWriter struct {
	file     *os.File
	mu       sync.Mutex
	captured []byte
}

// NewCaptureWriter creates a writer that saves to file.
func NewCaptureWriter(path string) (*CaptureWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &CaptureWriter{file: f}, nil
}

// Write captures data and writes to file.
func (cw *CaptureWriter) Write(p []byte) (n int, err error) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	cw.captured = append(cw.captured, p...)
	return cw.file.Write(p)
}

// Close closes the file and returns captured data.
func (cw *CaptureWriter) Close() ([]byte, error) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	err := cw.file.Close()
	return cw.captured, err
}

// GetCaptured returns all captured data so far.
func (cw *CaptureWriter) GetCaptured() []byte {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	result := make([]byte, len(cw.captured))
	copy(result, cw.captured)
	return result
}

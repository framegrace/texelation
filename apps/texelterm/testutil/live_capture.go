// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/testutil/live_capture.go
// Summary: Live capture of terminal sessions for debugging visual bugs.
//
// Usage:
//   1. Set TEXELTERM_CAPTURE=/tmp/capture.txrec before running texelterm
//   2. Run your app, reproduce the issue
//   3. Exit texelterm
//   4. Use the recording with ReferenceComparator for analysis
//
// Integration:
//   capture := NewLiveCaptureFromEnv()
//   if capture != nil {
//       capture.StartCapture(width, height)
//       // ... in PTY read loop:
//       capture.CaptureBytes(data)
//       // ... on exit:
//       capture.Finalize()
//   }

package testutil

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// LiveCapture manages real-time recording during terminal sessions.
type LiveCapture struct {
	recording   *Recording
	outputPath  string
	captureFile *os.File
	mu          sync.Mutex
	started     bool
	finalized   bool
}

// CaptureEnvVar is the environment variable that enables live capture.
const CaptureEnvVar = "TEXELTERM_CAPTURE"

// NewLiveCaptureFromEnv creates a live capture if TEXELTERM_CAPTURE is set.
// Returns nil if environment variable is not set.
func NewLiveCaptureFromEnv() *LiveCapture {
	path := os.Getenv(CaptureEnvVar)
	if path == "" {
		return nil
	}
	return &LiveCapture{
		outputPath: path,
	}
}

// NewLiveCapture creates a live capture with an explicit output path.
func NewLiveCapture(outputPath string) *LiveCapture {
	return &LiveCapture{
		outputPath: outputPath,
	}
}

// IsEnabled returns true if live capture is active.
func IsEnabled() bool {
	return os.Getenv(CaptureEnvVar) != ""
}

// OutputPath returns the capture output path.
func (lc *LiveCapture) OutputPath() string {
	return lc.outputPath
}

// StartCapture begins recording with initial dimensions.
func (lc *LiveCapture) StartCapture(width, height int) error {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if lc.started {
		return fmt.Errorf("capture already started")
	}

	// Create output file
	f, err := os.Create(lc.outputPath)
	if err != nil {
		return fmt.Errorf("create capture file: %w", err)
	}
	lc.captureFile = f

	// Initialize recording
	lc.recording = NewRecording(width, height)
	lc.recording.Metadata.Description = "Live capture from texelterm"
	lc.recording.Metadata.Timestamp = time.Now()

	lc.started = true
	return nil
}

// CaptureBytes records raw PTY output.
func (lc *LiveCapture) CaptureBytes(data []byte) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if !lc.started || lc.finalized || lc.recording == nil {
		return
	}

	lc.recording.Sequences = append(lc.recording.Sequences, data...)
}

// CaptureString records a string (convenience wrapper).
func (lc *LiveCapture) CaptureString(s string) {
	lc.CaptureBytes([]byte(s))
}

// HandleResize updates recording metadata on terminal resize.
func (lc *LiveCapture) HandleResize(width, height int) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if lc.recording != nil {
		lc.recording.Metadata.Width = width
		lc.recording.Metadata.Height = height
	}
}

// SetDescription sets the recording description.
func (lc *LiveCapture) SetDescription(desc string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if lc.recording != nil {
		lc.recording.Metadata.Description = desc
	}
}

// SetShell sets the shell command in metadata.
func (lc *LiveCapture) SetShell(shell string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if lc.recording != nil {
		lc.recording.Metadata.Shell = shell
	}
}

// GetByteCount returns the number of bytes captured so far.
func (lc *LiveCapture) GetByteCount() int {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if lc.recording == nil {
		return 0
	}
	return len(lc.recording.Sequences)
}

// Finalize writes the recording to disk and closes.
func (lc *LiveCapture) Finalize() error {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if lc.finalized {
		return nil
	}

	if !lc.started || lc.recording == nil || lc.captureFile == nil {
		return fmt.Errorf("capture not started")
	}

	// Write recording
	if err := lc.recording.Write(lc.captureFile); err != nil {
		lc.captureFile.Close()
		return fmt.Errorf("write recording: %w", err)
	}

	if err := lc.captureFile.Close(); err != nil {
		return fmt.Errorf("close capture file: %w", err)
	}

	lc.finalized = true
	return nil
}

// IsStarted returns true if capture has started.
func (lc *LiveCapture) IsStarted() bool {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.started
}

// IsFinalized returns true if capture has been finalized.
func (lc *LiveCapture) IsFinalized() bool {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.finalized
}

// GetRecording returns a copy of the current recording.
// Useful for inspection during capture.
func (lc *LiveCapture) GetRecording() *Recording {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if lc.recording == nil {
		return nil
	}

	// Return a copy
	recCopy := &Recording{
		Metadata:  lc.recording.Metadata,
		Sequences: make([]byte, len(lc.recording.Sequences)),
	}
	recCopy.Metadata.Timestamp = lc.recording.Metadata.Timestamp
	copy(recCopy.Sequences, lc.recording.Sequences)
	return recCopy
}

// Abort cancels the capture without writing.
func (lc *LiveCapture) Abort() error {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if lc.captureFile != nil {
		lc.captureFile.Close()
		os.Remove(lc.outputPath)
	}

	lc.finalized = true
	return nil
}

// CaptureHook is an interface for terminal integration.
type CaptureHook interface {
	OnPTYWrite(data []byte)
	OnResize(width, height int)
	Flush() error
}

// Ensure LiveCapture implements CaptureHook
var _ CaptureHook = (*LiveCapture)(nil)

// OnPTYWrite implements CaptureHook.
func (lc *LiveCapture) OnPTYWrite(data []byte) {
	lc.CaptureBytes(data)
}

// OnResize implements CaptureHook.
func (lc *LiveCapture) OnResize(width, height int) {
	lc.HandleResize(width, height)
}

// Flush implements CaptureHook.
func (lc *LiveCapture) Flush() error {
	return lc.Finalize()
}

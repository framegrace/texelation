// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/lifecycle/pidfile.go
// Summary: PID file management for daemon lifecycle.

package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// PIDFile manages process ID file operations
type PIDFile interface {
	// Write creates/updates PID file with the given process ID
	Write(pid int) error

	// Read returns PID from file, or error if not exists/invalid
	Read() (int, error)

	// Remove deletes the PID file
	Remove() error

	// Exists checks if PID file exists
	Exists() bool

	// IsProcessRunning checks if the PID from file corresponds to a running process
	IsProcessRunning() bool

	// Path returns the PID file path
	Path() string
}

type standardPIDFile struct {
	path string
}

// NewPIDFile creates a new PID file manager
func NewPIDFile(path string) PIDFile {
	return &standardPIDFile{path: path}
}

func (p *standardPIDFile) Path() string {
	return p.path
}

func (p *standardPIDFile) Write(pid int) error {
	// Ensure directory exists
	dir := filepath.Dir(p.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create PID directory: %w", err)
	}

	content := fmt.Sprintf("%d\n", pid)
	if err := os.WriteFile(p.path, []byte(content), 0600); err != nil {
		return fmt.Errorf("write PID file: %w", err)
	}

	return nil
}

func (p *standardPIDFile) Read() (int, error) {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return 0, err
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("invalid PID format: %w", err)
	}

	if pid <= 0 {
		return 0, fmt.Errorf("invalid PID value: %d", pid)
	}

	return pid, nil
}

func (p *standardPIDFile) Remove() error {
	err := os.Remove(p.path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (p *standardPIDFile) Exists() bool {
	_, err := os.Stat(p.path)
	return err == nil
}

func (p *standardPIDFile) IsProcessRunning() bool {
	pid, err := p.Read()
	if err != nil || pid <= 0 {
		return false
	}

	// Send signal 0 to check if process exists
	// This works on both Linux and macOS
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = process.Signal(syscall.Signal(0))
	return err == nil
}

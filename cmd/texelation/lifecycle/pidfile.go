// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/lifecycle/pidfile.go
// Summary: PID file management for daemon lifecycle.

package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
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

	// IsLocked reports whether a running process holds an exclusive flock
	// on the PID file. Used by the supervisor as the canonical "is a server
	// alive" signal — unlike socket health, the lock is held continuously
	// from startup through clean shutdown, so a slow-flushing server still
	// reports as locked and is not mistaken for unresponsive.
	IsLocked() bool

	// AcquireExclusiveLock opens the PID file, writes the given pid, and
	// takes a non-blocking exclusive flock. Returns a closer that releases
	// the lock (and file descriptor) when called; the caller should keep
	// the closer alive for the lifetime of the server process. If another
	// process already holds the lock, returns ErrLocked without modifying
	// the file.
	AcquireExclusiveLock(pid int) (PIDLock, error)

	// WaitForUnlock blocks until the PID file is no longer locked by any
	// process, or the timeout elapses. Returns nil on release, or
	// context-like timeout error otherwise.
	WaitForUnlock(timeout time.Duration) error

	// Path returns the PID file path
	Path() string
}

// PIDLock represents a held exclusive lock on the PID file. Close releases
// the lock and file handle. The OS also releases the lock on process exit,
// which is the primary correctness guarantee.
type PIDLock interface {
	Close() error
}

// ErrLocked is returned by AcquireExclusiveLock when another process holds
// the lock. Callers should interpret this as "a server is already running".
var ErrLocked = errors.New("PID file is locked by another process")

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

// IsLocked reports whether another process holds an exclusive flock on
// the PID file. Implemented by attempting a non-blocking shared lock and
// immediately releasing it — if the attempt fails with EWOULDBLOCK an
// exclusive lock is held by someone else.
func (p *standardPIDFile) IsLocked() bool {
	f, err := os.OpenFile(p.path, os.O_RDONLY, 0)
	if err != nil {
		// No file → no lock possible.
		return false
	}
	defer f.Close()

	// Try a non-blocking shared lock. If an exclusive lock is held by
	// another process, this returns EWOULDBLOCK. If no lock is held,
	// it succeeds and we release immediately.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// AcquireExclusiveLock creates the PID file (if needed), writes the pid,
// and holds an exclusive flock for the returned PIDLock's lifetime. The
// file handle is kept open by the returned lock object — closing it
// (or process exit) releases the lock.
func (p *standardPIDFile) AcquireExclusiveLock(pid int) (PIDLock, error) {
	dir := filepath.Dir(p.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create PID directory: %w", err)
	}

	f, err := os.OpenFile(p.path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("open PID file: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("acquire flock: %w", err)
	}

	// Lock acquired. Truncate and write the fresh PID.
	if err := f.Truncate(0); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, fmt.Errorf("truncate PID file: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, fmt.Errorf("seek PID file: %w", err)
	}
	if _, err := fmt.Fprintf(f, "%d\n", pid); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, fmt.Errorf("write PID: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, fmt.Errorf("sync PID file: %w", err)
	}

	return &standardPIDLock{f: f}, nil
}

// WaitForUnlock polls IsLocked until it returns false or the timeout
// elapses. Polling is simpler than blocking flock and avoids holding a
// file descriptor in the caller.
func (p *standardPIDFile) WaitForUnlock(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	const pollInterval = 100 * time.Millisecond
	for {
		if !p.IsLocked() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for PID file %s to unlock", p.path)
		}
		time.Sleep(pollInterval)
	}
}

// standardPIDLock holds an exclusive flock on an open PID file. Close
// releases the lock and file descriptor. The OS also releases the lock
// automatically when the process exits, which is the primary guarantee.
type standardPIDLock struct {
	f *os.File
}

func (l *standardPIDLock) Close() error {
	if l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	err := l.f.Close()
	l.f = nil
	return err
}

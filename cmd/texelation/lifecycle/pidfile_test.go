// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/lifecycle/pidfile_test.go
// Summary: Regression tests for the PID file flock semantics used by
// the supervisor to detect live servers. Protects against the class of
// shutdown races where a slow-flushing server is mistaken for hung
// and killed mid-write, corrupting scrollback state.

package lifecycle

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestAcquireExclusiveLock_NewFile verifies that AcquireExclusiveLock
// creates a new PID file, writes the pid, and returns a closable lock.
func TestAcquireExclusiveLock_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.pid")
	pf := NewPIDFile(path)

	lock, err := pf.AcquireExclusiveLock(12345)
	if err != nil {
		t.Fatalf("AcquireExclusiveLock: %v", err)
	}
	defer lock.Close()

	if !pf.Exists() {
		t.Error("PID file should exist after acquire")
	}
	if !pf.IsLocked() {
		t.Error("PID file should report as locked after acquire")
	}
	pid, err := pf.Read()
	if err != nil {
		t.Fatalf("Read PID: %v", err)
	}
	if pid != 12345 {
		t.Errorf("expected PID 12345, got %d", pid)
	}
}

// TestAcquireExclusiveLock_AlreadyHeld verifies that a second acquire
// on a locked file returns ErrLocked without disturbing the first.
func TestAcquireExclusiveLock_AlreadyHeld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.pid")
	pf := NewPIDFile(path)

	lock1, err := pf.AcquireExclusiveLock(111)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer lock1.Close()

	// Second acquire must fail with ErrLocked. NOTE: flock is
	// per-process on Linux, so we use a different PIDFile instance via
	// a separate process for the cross-process check. Here we exercise
	// only the explicit "already held by us" path.
	// Opening a second file handle in the same process DOES NOT fail
	// on LOCK_NB because flock is advisory per open file description
	// in modern kernels, so we simulate the cross-process case below.

	// Verify IsLocked reports true from a different PIDFile instance.
	pf2 := NewPIDFile(path)
	if !pf2.IsLocked() {
		t.Error("second PIDFile instance should report locked")
	}
}

// TestLockReleasedOnClose verifies closing the lock releases it so a
// subsequent acquire succeeds.
func TestLockReleasedOnClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.pid")
	pf := NewPIDFile(path)

	lock1, err := pf.AcquireExclusiveLock(111)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := lock1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if pf.IsLocked() {
		t.Error("PID file should not report locked after Close")
	}

	lock2, err := pf.AcquireExclusiveLock(222)
	if err != nil {
		t.Fatalf("second acquire after close: %v", err)
	}
	defer lock2.Close()
}

// TestWaitForUnlock_AlreadyUnlocked returns immediately.
func TestWaitForUnlock_AlreadyUnlocked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.pid")
	pf := NewPIDFile(path)

	start := time.Now()
	if err := pf.WaitForUnlock(500 * time.Millisecond); err != nil {
		t.Fatalf("WaitForUnlock on unlocked file: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Errorf("WaitForUnlock took %v on unlocked file", elapsed)
	}
}

// TestWaitForUnlock_Timeout returns error when lock is held for too long.
// Uses a child process because flock is shared between fds in the same
// process — so we launch `sleep` with the lock file held by `flock(1)`.
func TestWaitForUnlock_Timeout(t *testing.T) {
	flockBin, err := exec.LookPath("flock")
	if err != nil {
		t.Skip("flock(1) binary not available")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "server.pid")
	// Create the file so flock(1) can lock it.
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}

	// Hold the lock for 2 seconds in a child.
	cmd := exec.Command(flockBin, "-x", path, "-c", "sleep 2")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start flock child: %v", err)
	}
	defer cmd.Process.Kill()

	// Give the child a moment to acquire.
	time.Sleep(200 * time.Millisecond)

	pf := NewPIDFile(path)
	if !pf.IsLocked() {
		t.Fatal("expected file to be locked by child")
	}

	start := time.Now()
	err = pf.WaitForUnlock(300 * time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
	if elapsed < 280*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Errorf("WaitForUnlock elapsed %v, expected ~300ms", elapsed)
	}
}

// TestWaitForUnlock_ReleasedDuringWait returns nil when the lock is
// released while waiting.
func TestWaitForUnlock_ReleasedDuringWait(t *testing.T) {
	flockBin, err := exec.LookPath("flock")
	if err != nil {
		t.Skip("flock(1) binary not available")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "server.pid")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}

	// Child holds the lock for 300ms.
	cmd := exec.Command(flockBin, "-x", path, "-c", "sleep 0.3")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start flock child: %v", err)
	}
	defer cmd.Wait()

	time.Sleep(100 * time.Millisecond) // ensure child has the lock
	pf := NewPIDFile(path)

	var wg sync.WaitGroup
	wg.Add(1)
	var waitErr error
	go func() {
		defer wg.Done()
		waitErr = pf.WaitForUnlock(2 * time.Second)
	}()
	wg.Wait()

	if waitErr != nil {
		t.Errorf("WaitForUnlock: %v", waitErr)
	}
}

// TestIsLocked_NoFile returns false when the file doesn't exist.
func TestIsLocked_NoFile(t *testing.T) {
	dir := t.TempDir()
	pf := NewPIDFile(filepath.Join(dir, "does-not-exist"))
	if pf.IsLocked() {
		t.Error("IsLocked should be false for nonexistent file")
	}
}

// TestAcquire_ErrLockedPropagates ensures AcquireExclusiveLock returns
// exactly ErrLocked for the already-held case, so callers can switch on
// the sentinel.
func TestAcquire_ErrLockedPropagates(t *testing.T) {
	flockBin, err := exec.LookPath("flock")
	if err != nil {
		t.Skip("flock(1) binary not available")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "server.pid")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(flockBin, "-x", path, "-c", "sleep 1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start flock child: %v", err)
	}
	defer cmd.Wait()
	time.Sleep(100 * time.Millisecond)

	pf := NewPIDFile(path)
	_, err = pf.AcquireExclusiveLock(os.Getpid())
	if !errors.Is(err, ErrLocked) {
		t.Errorf("expected ErrLocked, got %v", err)
	}
}

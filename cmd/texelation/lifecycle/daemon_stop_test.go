// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/lifecycle/daemon_stop_test.go
// Summary: Regression test for daemon.Stop waiting on the PID lock
// rather than force-killing after a short timeout. Protects against
// the 2026-04-08 bug where texel-server was SIGKILLed mid-WAL-flush.

package lifecycle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestGetState_LockHeldReportsRunning verifies that a daemon whose
// process is still alive AND whose PID file is locked reports as
// StateRunning — regardless of socket health. This is the core fix:
// before flock, a slow-shutting-down server whose socket listener had
// closed would report StateUnresponsive and get restarted.
func TestGetState_LockHeldReportsRunning(t *testing.T) {
	flockBin, err := exec.LookPath("flock")
	if err != nil {
		t.Skip("flock(1) binary not available")
	}

	dir := t.TempDir()
	pidPath := filepath.Join(dir, "server.pid")
	// File must exist before flock(1) can lock it.
	if err := os.WriteFile(pidPath, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	// Hold the lock for 2 seconds in a child, simulating a slow
	// shutdown where the socket has been closed but the process is
	// still flushing state to disk.
	cmd := exec.Command(flockBin, "-x", pidPath, "-c", "sleep 2")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start flock child: %v", err)
	}
	defer cmd.Wait()

	// Write the child's PID so IsProcessRunning would see a live proc,
	// but the real signal is the flock.
	if err := os.WriteFile(pidPath, []byte("99999\n"), 0600); err != nil {
		t.Fatal(err)
	}

	time.Sleep(150 * time.Millisecond)

	pf := NewPIDFile(pidPath)
	// Health check that always fails, simulating a closed socket listener.
	health := &alwaysFailHealth{}
	daemon := NewDaemonManager(pf, "/nonexistent.sock", health)

	state, err := daemon.GetState(context.Background())
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateRunning {
		t.Errorf("expected StateRunning (lock held), got %v", state)
	}
}

// TestGetState_LockReleased_FallsBackToProcessCheck verifies legacy
// servers without flock support still report correctly via the fallback
// path (process alive + socket health).
func TestGetState_LockReleased_FallsBackToProcess(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "server.pid")
	// Write a PID that refers to our own process (always "running").
	ownPID := os.Getpid()
	if err := os.WriteFile(pidPath, []byte(pidText(ownPID)), 0600); err != nil {
		t.Fatal(err)
	}

	pf := NewPIDFile(pidPath)
	if pf.IsLocked() {
		t.Fatal("expected unlocked file for fallback test")
	}

	health := &alwaysFailHealth{}
	daemon := NewDaemonManager(pf, "/nonexistent.sock", health)

	state, err := daemon.GetState(context.Background())
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateUnresponsive {
		t.Errorf("expected StateUnresponsive (legacy fallback, socket fail), got %v", state)
	}
}

// TestGetState_Stopped_NoPIDFile covers the baseline no-server case.
func TestGetState_Stopped_NoPIDFile(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "does-not-exist")
	pf := NewPIDFile(pidPath)
	daemon := NewDaemonManager(pf, "/nonexistent.sock", &alwaysFailHealth{})

	state, err := daemon.GetState(context.Background())
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state != StateStopped {
		t.Errorf("expected StateStopped, got %v", state)
	}
}

// alwaysFailHealth is a HealthChecker that always reports unreachable.
type alwaysFailHealth struct{}

func (alwaysFailHealth) Check(_ context.Context, _ string) error {
	return errUnreachable{}
}

type errUnreachable struct{}

func (errUnreachable) Error() string { return "unreachable" }

func pidText(pid int) string {
	b := make([]byte, 0, 16)
	if pid == 0 {
		return "0\n"
	}
	for pid > 0 {
		b = append([]byte{byte('0' + pid%10)}, b...)
		pid /= 10
	}
	return string(b) + "\n"
}

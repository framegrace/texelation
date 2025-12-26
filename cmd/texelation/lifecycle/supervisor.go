// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/lifecycle/supervisor.go
// Summary: Orchestrates daemon lifecycle with health checking.

package lifecycle

import (
	"context"
	"fmt"
	"time"
)

// StartResult contains the result of an EnsureRunning operation
type StartResult struct {
	WasStarted      bool   // True if the server was started (wasn't running before)
	WasRestarted    bool   // True if the server was restarted (was unresponsive)
	PreviousState   DaemonState
	CurrentState    DaemonState
	PID             int
}

// Supervisor orchestrates daemon lifecycle with health checking
type Supervisor struct {
	daemon        DaemonManager
	health        HealthChecker
	pidFile       PIDFile
	startupWait   time.Duration
	healthTimeout time.Duration
}

// SupervisorConfig configures the supervisor
type SupervisorConfig struct {
	StartupWait   time.Duration // How long to wait for server to become healthy after start
	HealthTimeout time.Duration // Timeout for health checks
}

// DefaultSupervisorConfig returns sensible defaults
func DefaultSupervisorConfig() SupervisorConfig {
	return SupervisorConfig{
		StartupWait:   5 * time.Second,
		HealthTimeout: 2 * time.Second,
	}
}

// NewSupervisor creates a new supervisor
func NewSupervisor(daemon DaemonManager, health HealthChecker, pidFile PIDFile, config SupervisorConfig) *Supervisor {
	if config.StartupWait == 0 {
		config.StartupWait = 5 * time.Second
	}
	if config.HealthTimeout == 0 {
		config.HealthTimeout = 2 * time.Second
	}
	return &Supervisor{
		daemon:        daemon,
		health:        health,
		pidFile:       pidFile,
		startupWait:   config.StartupWait,
		healthTimeout: config.HealthTimeout,
	}
}

// EnsureRunning ensures the server daemon is running and healthy
// Returns information about what actions were taken
func (s *Supervisor) EnsureRunning(ctx context.Context, opts ServerOptions) (*StartResult, error) {
	result := &StartResult{}

	// Get initial state
	state, err := s.daemon.GetState(ctx)
	if err != nil {
		return nil, fmt.Errorf("get daemon state: %w", err)
	}
	result.PreviousState = state

	switch state {
	case StateRunning:
		// Already running and healthy - nothing to do
		result.CurrentState = StateRunning
		result.PID = s.daemon.GetPID()
		return result, nil

	case StateUnresponsive:
		// Server is hung - force restart
		fmt.Printf("Server is unresponsive (PID %d), restarting...\n", s.daemon.GetPID())
		if err := s.daemon.Restart(ctx, opts); err != nil {
			return nil, fmt.Errorf("restart unresponsive server: %w", err)
		}
		result.WasRestarted = true
		result.WasStarted = true

	case StateStale:
		// PID file exists but process is gone - clean up and start
		fmt.Println("Cleaning up stale PID file...")
		s.pidFile.Remove()
		fallthrough

	case StateStopped, StateUnknown:
		// Server not running - start it
		fmt.Println("Starting texelation server...")
		if err := s.daemon.Start(ctx, opts); err != nil {
			return nil, fmt.Errorf("start server: %w", err)
		}
		result.WasStarted = true
	}

	// Wait for server to become healthy
	if err := s.waitForHealthy(ctx, opts.SocketPath); err != nil {
		return nil, fmt.Errorf("server failed to become healthy: %w", err)
	}

	result.CurrentState = StateRunning
	result.PID = s.daemon.GetPID()
	return result, nil
}

// waitForHealthy polls the server until healthy or timeout
func (s *Supervisor) waitForHealthy(ctx context.Context, socketPath string) error {
	deadline := time.Now().Add(s.startupWait)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		healthCtx, cancel := context.WithTimeout(ctx, s.healthTimeout)
		err := s.health.Check(healthCtx, socketPath)
		cancel()

		if err == nil {
			return nil
		}

		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for server to become healthy")
}

// GetState returns the current daemon state
func (s *Supervisor) GetState(ctx context.Context) (DaemonState, error) {
	return s.daemon.GetState(ctx)
}

// Stop stops the daemon
func (s *Supervisor) Stop(ctx context.Context) error {
	return s.daemon.Stop(ctx)
}

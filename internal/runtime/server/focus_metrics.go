// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/focus_metrics.go
// Summary: Implements focus metrics capabilities for the server runtime.
// Usage: Used by texel-server to coordinate focus metrics when hosting apps and sessions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"log"
	"sync"
	"time"

	"texelation/texel"
)

type FocusMetrics struct {
	mu         sync.Mutex
	last       [16]byte
	changes    uint64
	lastChange time.Time
	logger     *log.Logger
	once       sync.Once
}

type FocusStats struct {
	LastPaneID [16]byte
	Changes    uint64
	LastChange time.Time
}

func NewFocusMetrics(logger *log.Logger) *FocusMetrics {
	if logger == nil {
		logger = log.Default()
	}
	return &FocusMetrics{logger: logger}
}

func (f *FocusMetrics) Attach(desktop *texel.Desktop) {
	if desktop == nil {
		return
	}
	f.once.Do(func() {
		desktop.RegisterFocusListener(f)
	})
}

func (f *FocusMetrics) PaneFocused(paneID [16]byte) {
	f.mu.Lock()
	f.last = paneID
	f.changes++
	f.lastChange = time.Now()
	changes := f.changes
	f.mu.Unlock()

	if f.logger != nil {
		f.logger.Printf("metric focus pane=%x changes=%d", paneID[:4], changes)
	}
}

func (f *FocusMetrics) Snapshot() FocusStats {
	f.mu.Lock()
	defer f.mu.Unlock()
	return FocusStats{LastPaneID: f.last, Changes: f.changes, LastChange: f.lastChange}
}

var _ texel.DesktopFocusListener = (*FocusMetrics)(nil)

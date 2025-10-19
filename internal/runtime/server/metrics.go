// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/metrics.go
// Summary: Implements metrics capabilities for the server runtime.
// Usage: Used by texel-server to coordinate metrics when hosting apps and sessions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"log"
	"time"
)

// PublishLogger logs publish metrics to the provided logger.
type PublishLogger struct {
	logger *log.Logger
}

// NewPublishLogger creates a new publish observer that logs metrics.
func NewPublishLogger(l *log.Logger) *PublishLogger {
	if l == nil {
		l = log.Default()
	}
	return &PublishLogger{logger: l}
}

func (p *PublishLogger) ObservePublish(session *Session, paneCount int, duration time.Duration) {
	if p == nil || p.logger == nil || session == nil {
		return
	}
	id := session.ID()
	p.logger.Printf("publish session=%x panes=%d duration=%s", id[:4], paneCount, duration)
}

// SessionStatsObserver records session queue metrics.
type SessionStatsObserver interface {
	ObserveSessionStats(stats SessionStats)
}

// SessionStatsLogger logs session stats.
type SessionStatsLogger struct {
	logger *log.Logger
}

// NewSessionStatsLogger returns an observer that logs queue stats.
func NewSessionStatsLogger(l *log.Logger) *SessionStatsLogger {
	if l == nil {
		l = log.Default()
	}
	return &SessionStatsLogger{logger: l}
}

func (s *SessionStatsLogger) ObserveSessionStats(stats SessionStats) {
	if s == nil || s.logger == nil {
		return
	}
	s.logger.Printf("session=%x pending=%d dropped=%d last_seq=%d", stats.ID[:4], stats.PendingCount, stats.DroppedDiffs, stats.LastDroppedSeq)
}

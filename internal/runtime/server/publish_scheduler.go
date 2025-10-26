// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/publish_scheduler.go
// Summary: Per-pane debouncing for desktop publishes so panes refresh independently.

package server

import (
	"sync"
	"sync/atomic"
	"time"
)

type PanePublishListener interface {
	OnPanePublished(id [16]byte)
}

type publishScheduler struct {
	fallbackDelay time.Duration

	mu        sync.Mutex
	publisher *DesktopPublisher
	timers    map[[16]byte]*time.Timer

	fallbackCount atomic.Uint64
}

func newPublishScheduler(delay time.Duration) *publishScheduler {
	return &publishScheduler{
		fallbackDelay: delay,
		timers:        make(map[[16]byte]*time.Timer),
	}
}

func (s *publishScheduler) SetPublisher(publisher *DesktopPublisher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.publisher != nil {
		s.publisher.removePaneListener(s)
	}
	s.publisher = publisher
	if publisher != nil {
		publisher.addPaneListener(s)
	}
	for id, timer := range s.timers {
		timer.Stop()
		delete(s.timers, id)
	}
}

func (s *publishScheduler) RequestPublish(paneID [16]byte) {
	if s.publisher == nil {
		return
	}
	if isZeroPaneID(paneID) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if timer, ok := s.timers[paneID]; ok {
		timer.Reset(s.fallbackDelay)
		return
	}
	timer := time.AfterFunc(s.fallbackDelay, func() {
		s.triggerFallback(paneID)
	})
	s.timers[paneID] = timer
}

func (s *publishScheduler) triggerFallback(paneID [16]byte) {
	s.fallbackCount.Add(1)
	pub := s.getPublisher()
	if pub != nil {
		_ = pub.Publish()
	}
	s.mu.Lock()
	if timer, ok := s.timers[paneID]; ok {
		timer.Stop()
		delete(s.timers, paneID)
	}
	s.mu.Unlock()
}

func (s *publishScheduler) getPublisher() *DesktopPublisher {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.publisher
}

func (s *publishScheduler) NotifyRefresh(paneID [16]byte) {
	s.mu.Lock()
	if timer, ok := s.timers[paneID]; ok {
		timer.Stop()
		delete(s.timers, paneID)
	}
	s.mu.Unlock()
}

func (s *publishScheduler) ForcePublish() {
	var pub *DesktopPublisher
	s.mu.Lock()
	for _, timer := range s.timers {
		timer.Stop()
	}
	s.timers = make(map[[16]byte]*time.Timer)
	pub = s.publisher
	s.mu.Unlock()
	if pub != nil {
		_ = pub.Publish()
	}
}

func (s *publishScheduler) OnPanePublished(id [16]byte) {
	s.NotifyRefresh(id)
}

func (s *publishScheduler) FallbackCount() uint64 {
	return s.fallbackCount.Load()
}

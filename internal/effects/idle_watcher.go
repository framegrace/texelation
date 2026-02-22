// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/effects/idle_watcher.go
// Summary: Monitors input activity and fires callbacks when the user is idle.
// Usage: Used by the screensaver system to detect idle state and trigger activation.

package effects

import (
	"sync"
	"time"
)

// IdleWatcherConfig configures the idle watcher.
type IdleWatcherConfig struct {
	Timeout      time.Duration
	EffectID     string
	LockEnabled  bool
	LockTimeout  time.Duration
	OnActivate   func()
	OnDeactivate func()
	OnLock       func()
}

// IdleWatcher monitors input activity and fires callbacks on idle timeout.
type IdleWatcher struct {
	mu           sync.Mutex
	timeout      time.Duration
	effectID     string
	lockEnabled  bool
	lockTimeout  time.Duration
	timer        *time.Timer
	lockTimer    *time.Timer
	active       bool
	onActivate   func()
	onDeactivate func()
	onLock       func()
	stopped      bool
}

// NewIdleWatcher creates and starts an idle watcher.
func NewIdleWatcher(cfg IdleWatcherConfig) *IdleWatcher {
	w := &IdleWatcher{
		timeout:      cfg.Timeout,
		effectID:     cfg.EffectID,
		lockEnabled:  cfg.LockEnabled,
		lockTimeout:  cfg.LockTimeout,
		onActivate:   cfg.OnActivate,
		onDeactivate: cfg.OnDeactivate,
		onLock:       cfg.OnLock,
	}
	w.timer = time.AfterFunc(w.timeout, w.onIdle)
	return w
}

// EffectID returns the configured effect identifier.
func (w *IdleWatcher) EffectID() string {
	return w.effectID
}

// IsActive returns whether the screensaver is currently showing.
func (w *IdleWatcher) IsActive() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active
}

// ResetActivity resets the idle timer. If the screensaver is active,
// deactivates it via callback. The dismiss event is consumed by the caller.
func (w *IdleWatcher) ResetActivity() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}
	if w.active {
		w.active = false
		if w.lockTimer != nil {
			w.lockTimer.Stop()
			w.lockTimer = nil
		}
		if w.onDeactivate != nil {
			w.onDeactivate()
		}
	}
	w.timer.Reset(w.timeout)
}

// ActivateNow manually activates the screensaver immediately.
func (w *IdleWatcher) ActivateNow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped || w.active {
		return
	}
	w.active = true
	w.timer.Stop()
	if w.onActivate != nil {
		w.onActivate()
	}
	if w.lockEnabled && w.lockTimeout > 0 && w.onLock != nil {
		w.lockTimer = time.AfterFunc(w.lockTimeout, w.onLockFired)
	}
}

// Stop cleans up timers. Safe to call multiple times.
func (w *IdleWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	if w.timer != nil {
		w.timer.Stop()
	}
	if w.lockTimer != nil {
		w.lockTimer.Stop()
		w.lockTimer = nil
	}
}

func (w *IdleWatcher) onIdle() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped || w.active {
		return
	}
	w.active = true
	if w.onActivate != nil {
		w.onActivate()
	}
	if w.lockEnabled && w.lockTimeout > 0 && w.onLock != nil {
		w.lockTimer = time.AfterFunc(w.lockTimeout, w.onLockFired)
	}
}

func (w *IdleWatcher) onLockFired() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped || !w.active {
		return
	}
	if w.onLock != nil {
		w.onLock()
	}
}

// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package effects

import (
	"sync"
	"testing"
	"time"
)

func TestIdleWatcher_FiresAfterTimeout(t *testing.T) {
	var mu sync.Mutex
	activated := false
	w := NewIdleWatcher(IdleWatcherConfig{
		Timeout:  20 * time.Millisecond,
		EffectID: "crypt",
		OnActivate: func() {
			mu.Lock()
			activated = true
			mu.Unlock()
		},
		OnDeactivate: func() {},
	})
	defer w.Stop()

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	got := activated
	mu.Unlock()
	if !got {
		t.Fatal("expected screensaver to activate after timeout")
	}
}

func TestIdleWatcher_ResetActivityPrevents(t *testing.T) {
	var mu sync.Mutex
	activated := false
	w := NewIdleWatcher(IdleWatcherConfig{
		Timeout:  30 * time.Millisecond,
		EffectID: "crypt",
		OnActivate: func() {
			mu.Lock()
			activated = true
			mu.Unlock()
		},
		OnDeactivate: func() {},
	})
	defer w.Stop()

	time.Sleep(15 * time.Millisecond)
	w.ResetActivity()
	time.Sleep(15 * time.Millisecond)

	mu.Lock()
	got := activated
	mu.Unlock()
	if got {
		t.Fatal("expected screensaver NOT to activate after activity reset")
	}

	time.Sleep(40 * time.Millisecond)
	mu.Lock()
	got = activated
	mu.Unlock()
	if !got {
		t.Fatal("expected screensaver to activate after new timeout")
	}
}

func TestIdleWatcher_ResetActivityDeactivates(t *testing.T) {
	var mu sync.Mutex
	deactivated := false
	w := NewIdleWatcher(IdleWatcherConfig{
		Timeout:    20 * time.Millisecond,
		EffectID:   "crypt",
		OnActivate: func() {},
		OnDeactivate: func() {
			mu.Lock()
			deactivated = true
			mu.Unlock()
		},
	})
	defer w.Stop()

	time.Sleep(50 * time.Millisecond)

	w.ResetActivity()

	mu.Lock()
	got := deactivated
	mu.Unlock()
	if !got {
		t.Fatal("expected screensaver to deactivate on ResetActivity")
	}
}

func TestIdleWatcher_ActivateNow(t *testing.T) {
	var mu sync.Mutex
	activated := false
	w := NewIdleWatcher(IdleWatcherConfig{
		Timeout:  time.Hour,
		EffectID: "crypt",
		OnActivate: func() {
			mu.Lock()
			activated = true
			mu.Unlock()
		},
		OnDeactivate: func() {},
	})
	defer w.Stop()

	w.ActivateNow()

	mu.Lock()
	got := activated
	mu.Unlock()
	if !got {
		t.Fatal("expected screensaver to activate immediately")
	}
}

func TestIdleWatcher_IsActive(t *testing.T) {
	w := NewIdleWatcher(IdleWatcherConfig{
		Timeout:      20 * time.Millisecond,
		EffectID:     "crypt",
		OnActivate:   func() {},
		OnDeactivate: func() {},
	})
	defer w.Stop()

	if w.IsActive() {
		t.Fatal("should not be active initially")
	}

	time.Sleep(50 * time.Millisecond)

	if !w.IsActive() {
		t.Fatal("should be active after timeout")
	}

	w.ResetActivity()

	if w.IsActive() {
		t.Fatal("should not be active after reset")
	}
}

func TestIdleWatcher_LockTimerFires(t *testing.T) {
	var mu sync.Mutex
	lockFired := false
	w := NewIdleWatcher(IdleWatcherConfig{
		Timeout:      20 * time.Millisecond,
		EffectID:     "crypt",
		LockEnabled:  true,
		LockTimeout:  20 * time.Millisecond,
		OnActivate:   func() {},
		OnDeactivate: func() {},
		OnLock: func() {
			mu.Lock()
			lockFired = true
			mu.Unlock()
		},
	})
	defer w.Stop()

	time.Sleep(80 * time.Millisecond)

	mu.Lock()
	got := lockFired
	mu.Unlock()
	if !got {
		t.Fatal("expected lock timer to fire after screensaver")
	}
}

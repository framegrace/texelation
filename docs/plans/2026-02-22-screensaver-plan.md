# Screensaver System Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the hardcoded Ctrl+R crypt toggle with a general-purpose screensaver system that activates a configurable global effect after idle timeout.

**Architecture:** Client-side `IdleWatcher` struct in `internal/effects/` owns timer state and callbacks. It triggers effects via `TriggerScreensaver` (explicit activate/deactivate, not toggle). The watcher is created in `client_state.go` from system config, and `input_handler.go` calls `ResetActivity()` on every input event.

**Tech Stack:** Go 1.24, `time.Timer` for idle detection, `sync.Mutex` for thread safety, existing `effects.Manager` for triggering.

---

### Task 1: Add `TriggerScreensaver` trigger type

Replace `TriggerCryptToggle` with `TriggerScreensaver` in the effects trigger constants and update the config parser.

**Files:**
- Modify: `internal/effects/events.go:74`
- Modify: `internal/effects/config.go:146-148` (ParseTrigger)
- Modify: `internal/effects/config.go:177` (TriggerNames)

**Step 1: Replace trigger constant in events.go**

In `internal/effects/events.go`, replace line 74:
```go
// Before:
TriggerCryptToggle
// After:
TriggerScreensaver
```

**Step 2: Update ParseTrigger in config.go**

In `internal/effects/config.go`, replace the `"crypt.toggle"` case (line 146-147):
```go
// Before:
case "crypt.toggle":
    return TriggerCryptToggle, true
// After:
case "screensaver":
    return TriggerScreensaver, true
```

**Step 3: Update TriggerNames in config.go**

In `internal/effects/config.go`, replace `"crypt.toggle"` in TriggerNames() (line 177):
```go
// Before:
"crypt.toggle",
// After:
"screensaver",
```

**Step 4: Verify compilation**

Run: `go build ./internal/effects/`
Expected: Compilation errors in files that reference `TriggerCryptToggle` (crypt.go, input_handler.go, client_state.go). That's expected — we fix them in subsequent tasks.

**Step 5: Commit**

```bash
git add internal/effects/events.go internal/effects/config.go
git commit -m "Replace TriggerCryptToggle with TriggerScreensaver"
```

---

### Task 2: Update crypt effect for screensaver semantics

Change crypt effect from toggle on `TriggerCryptToggle` to explicit activate/deactivate on `TriggerScreensaver`.

**Files:**
- Modify: `internal/effects/crypt.go:32-37`

**Step 1: Update HandleTrigger in crypt.go**

Replace the `HandleTrigger` method (lines 32-37):
```go
// Before:
func (e *cryptEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerCryptToggle {
		return
	}
	e.active = !e.active
}

// After:
func (e *cryptEffect) HandleTrigger(trigger EffectTrigger) {
	if trigger.Type != TriggerScreensaver {
		return
	}
	e.active = trigger.Active
}
```

**Step 2: Verify crypt.go compiles**

Run: `go build ./internal/effects/`
Expected: PASS (crypt.go now compiles; input_handler.go and client_state.go still broken).

**Step 3: Commit**

```bash
git add internal/effects/crypt.go
git commit -m "Update crypt effect to use TriggerScreensaver"
```

---

### Task 3: Write IdleWatcher with tests (TDD)

Create the `IdleWatcher` struct and unit tests. The watcher owns timer state and fires callbacks on idle timeout.

**Files:**
- Create: `internal/effects/idle_watcher.go`
- Create: `internal/effects/idle_watcher_test.go`

**Step 1: Write the failing tests**

Create `internal/effects/idle_watcher_test.go`:
```go
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

	// Reset activity before timeout
	time.Sleep(15 * time.Millisecond)
	w.ResetActivity()
	time.Sleep(15 * time.Millisecond)

	mu.Lock()
	got := activated
	mu.Unlock()
	if got {
		t.Fatal("expected screensaver NOT to activate after activity reset")
	}

	// Now wait for the full timeout
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
		Timeout:      20 * time.Millisecond,
		EffectID:     "crypt",
		OnActivate:   func() {},
		OnDeactivate: func() {
			mu.Lock()
			deactivated = true
			mu.Unlock()
		},
	})
	defer w.Stop()

	// Wait for activation
	time.Sleep(50 * time.Millisecond)

	// Reset while active should deactivate
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
		Timeout:  time.Hour, // very long, won't fire naturally
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
		Timeout:     20 * time.Millisecond,
		EffectID:    "crypt",
		LockEnabled: true,
		LockTimeout: 20 * time.Millisecond,
		OnActivate:  func() {},
		OnDeactivate: func() {},
		OnLock: func() {
			mu.Lock()
			lockFired = true
			mu.Unlock()
		},
	})
	defer w.Stop()

	// Wait for screensaver + lock
	time.Sleep(80 * time.Millisecond)

	mu.Lock()
	got := lockFired
	mu.Unlock()
	if !got {
		t.Fatal("expected lock timer to fire after screensaver")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/effects/ -run TestIdleWatcher -v -count=1`
Expected: FAIL — `NewIdleWatcher` and `IdleWatcherConfig` undefined.

**Step 3: Write the IdleWatcher implementation**

Create `internal/effects/idle_watcher.go`:
```go
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
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/effects/ -run TestIdleWatcher -v -count=1`
Expected: All 6 tests PASS.

**Step 5: Run all effects tests**

Run: `go test ./internal/effects/ -v -count=1`
Expected: All tests PASS.

**Step 6: Commit**

```bash
git add internal/effects/idle_watcher.go internal/effects/idle_watcher_test.go
git commit -m "Add IdleWatcher with tests"
```

---

### Task 4: Remove Ctrl+R and hardcoded crypt binding

Remove the old Ctrl+R key handling and the hardcoded crypt effect registration.

**Files:**
- Modify: `internal/runtime/client/input_handler.go:37-46`
- Modify: `internal/runtime/client/client_state.go:156-159`

**Step 1: Remove Ctrl+R handling from input_handler.go**

Delete lines 37-46 in `input_handler.go`:
```go
// DELETE this block:
if ev.Key() == tcell.KeyCtrlR {
    if state.effects != nil {
        state.effects.HandleTrigger(effects.EffectTrigger{
            Type:      effects.TriggerCryptToggle,
            Timestamp: time.Now(),
        })
    }
    render(state, screen)
    return true
}
```

**Step 2: Remove hardcoded crypt binding from client_state.go**

Delete lines 156-159 in `client_state.go`:
```go
// DELETE this block:
// Always register the crypt (screen lock) effect regardless of theme bindings.
if cryptEff, err := effects.CreateEffect("crypt", nil); err == nil {
    manager.RegisterBinding(effects.Binding{Effect: cryptEff, Target: effects.TargetWorkspace, Event: effects.TriggerCryptToggle})
}
```

**Step 3: Verify compilation**

Run: `go build ./internal/runtime/client/`
Expected: PASS. No more references to `TriggerCryptToggle`.

Run: `go build ./...`
Expected: PASS.

**Step 4: Commit**

```bash
git add internal/runtime/client/input_handler.go internal/runtime/client/client_state.go
git commit -m "Remove Ctrl+R crypt toggle and hardcoded crypt binding"
```

---

### Task 5: Add screensaver config parsing

Add a helper to read screensaver config from the system config section.

**Files:**
- Create: `internal/effects/screensaver_config.go`
- Create: `internal/effects/screensaver_config_test.go`

**Step 1: Write the failing test**

Create `internal/effects/screensaver_config_test.go`:
```go
package effects

import (
	"testing"
	"time"
)

func TestParseScreensaverConfig_Defaults(t *testing.T) {
	cfg := ParseScreensaverConfig(nil)
	if cfg.Enabled {
		t.Fatal("expected disabled by default")
	}
	if cfg.Timeout != 5*time.Minute {
		t.Fatalf("expected 5m timeout, got %v", cfg.Timeout)
	}
	if cfg.EffectID != "crypt" {
		t.Fatalf("expected crypt effect, got %q", cfg.EffectID)
	}
	if cfg.LockEnabled {
		t.Fatal("expected lock disabled by default")
	}
	if cfg.LockTimeout != 15*time.Minute {
		t.Fatalf("expected 15m lock timeout, got %v", cfg.LockTimeout)
	}
}

func TestParseScreensaverConfig_Custom(t *testing.T) {
	section := map[string]interface{}{
		"enabled":              true,
		"timeout_minutes":      float64(10),
		"effect":               "rainbow",
		"lock_enabled":         true,
		"lock_timeout_minutes": float64(30),
	}
	cfg := ParseScreensaverConfig(section)
	if !cfg.Enabled {
		t.Fatal("expected enabled")
	}
	if cfg.Timeout != 10*time.Minute {
		t.Fatalf("expected 10m, got %v", cfg.Timeout)
	}
	if cfg.EffectID != "rainbow" {
		t.Fatalf("expected rainbow, got %q", cfg.EffectID)
	}
	if !cfg.LockEnabled {
		t.Fatal("expected lock enabled")
	}
	if cfg.LockTimeout != 30*time.Minute {
		t.Fatalf("expected 30m, got %v", cfg.LockTimeout)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/effects/ -run TestParseScreensaverConfig -v -count=1`
Expected: FAIL — `ParseScreensaverConfig` undefined.

**Step 3: Write the implementation**

Create `internal/effects/screensaver_config.go`:
```go
package effects

import "time"

// ScreensaverConfig holds parsed screensaver settings.
type ScreensaverConfig struct {
	Enabled     bool
	Timeout     time.Duration
	EffectID    string
	LockEnabled bool
	LockTimeout time.Duration
}

// ParseScreensaverConfig parses the "screensaver" section from system config.
// All fields are optional with sensible defaults.
func ParseScreensaverConfig(section map[string]interface{}) ScreensaverConfig {
	cfg := ScreensaverConfig{
		Enabled:     false,
		Timeout:     5 * time.Minute,
		EffectID:    "crypt",
		LockEnabled: false,
		LockTimeout: 15 * time.Minute,
	}
	if section == nil {
		return cfg
	}
	if v, ok := section["enabled"].(bool); ok {
		cfg.Enabled = v
	}
	if v, ok := section["timeout_minutes"].(float64); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Minute
	}
	if v, ok := section["effect"].(string); ok && v != "" {
		cfg.EffectID = v
	}
	if v, ok := section["lock_enabled"].(bool); ok {
		cfg.LockEnabled = v
	}
	if v, ok := section["lock_timeout_minutes"].(float64); ok && v > 0 {
		cfg.LockTimeout = time.Duration(v) * time.Minute
	}
	return cfg
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/effects/ -run TestParseScreensaverConfig -v -count=1`
Expected: Both tests PASS.

**Step 5: Commit**

```bash
git add internal/effects/screensaver_config.go internal/effects/screensaver_config_test.go
git commit -m "Add screensaver config parser"
```

---

### Task 6: Wire IdleWatcher into client state

Create the IdleWatcher in `applyEffectConfig()` and register the screensaver effect.

**Files:**
- Modify: `internal/runtime/client/client_state.go`

**Step 1: Add idleWatcher field to clientState**

Add to the `clientState` struct:
```go
idleWatcher *effects.IdleWatcher
```

**Step 2: Create IdleWatcher in applyEffectConfig**

At the end of `applyEffectConfig()`, after `s.effects = manager` (around line 163), add:
```go
// Stop previous idle watcher if any.
if s.idleWatcher != nil {
    s.idleWatcher.Stop()
    s.idleWatcher = nil
}

// Set up screensaver idle watcher from system config.
var screensaverSection map[string]interface{}
if cfg := config.System(); cfg != nil {
    if section := cfg.Section("screensaver"); section != nil {
        screensaverSection = section
    }
}
ssCfg := effects.ParseScreensaverConfig(screensaverSection)
if ssCfg.Enabled {
    // Register the screensaver effect.
    if ssEff, err := effects.CreateEffect(ssCfg.EffectID, nil); err == nil {
        manager.RegisterBinding(effects.Binding{
            Effect: ssEff,
            Target: effects.TargetWorkspace,
            Event:  effects.TriggerScreensaver,
        })
    }
    mgr := s.effects
    s.idleWatcher = effects.NewIdleWatcher(effects.IdleWatcherConfig{
        Timeout:     ssCfg.Timeout,
        EffectID:    ssCfg.EffectID,
        LockEnabled: ssCfg.LockEnabled,
        LockTimeout: ssCfg.LockTimeout,
        OnActivate: func() {
            mgr.HandleTrigger(effects.EffectTrigger{
                Type:   effects.TriggerScreensaver,
                Active: true,
            })
        },
        OnDeactivate: func() {
            mgr.HandleTrigger(effects.EffectTrigger{
                Type:   effects.TriggerScreensaver,
                Active: false,
            })
        },
    })
}
```

**Step 3: Verify compilation**

Run: `go build ./internal/runtime/client/`
Expected: PASS.

**Step 4: Commit**

```bash
git add internal/runtime/client/client_state.go
git commit -m "Wire IdleWatcher into client effect config"
```

---

### Task 7: Add Ctrl+S and idle reset to input handler

Add Ctrl+S to manually start the screensaver, call `ResetActivity()` on every input event, and consume input when the screensaver is active.

**Files:**
- Modify: `internal/runtime/client/input_handler.go`

**Step 1: Add screensaver dismissal at the top of EventKey handling**

After the restart notification dismissal block (line 31) and before the paste check (line 33), add:
```go
// Screensaver dismissal: any key while active consumes the event.
if state.idleWatcher != nil && state.idleWatcher.IsActive() {
    state.idleWatcher.ResetActivity()
    render(state, screen)
    return true
}
```

**Step 2: Add Ctrl+S handling**

After the paste check block (line 36), add:
```go
if ev.Key() == tcell.KeyCtrlS {
    if state.idleWatcher != nil {
        state.idleWatcher.ActivateNow()
        render(state, screen)
    }
    return true
}
```

**Step 3: Add idle reset for non-consumed key events**

At the very end of the EventKey case (after the `sendKeyEvent` and effect trigger block, before the closing of the case), add:
```go
if state.idleWatcher != nil {
    state.idleWatcher.ResetActivity()
}
```

**Step 4: Add screensaver dismissal and idle reset for mouse events**

At the start of the `EventMouse` case (before `selectionChanged := ...`), add:
```go
// Screensaver dismissal: any mouse event while active consumes the event.
if state.idleWatcher != nil && state.idleWatcher.IsActive() {
    state.idleWatcher.ResetActivity()
    render(state, screen)
    return true
}
```

At the end of the `EventMouse` case (after the clipboard copy block), add:
```go
if state.idleWatcher != nil {
    state.idleWatcher.ResetActivity()
}
```

**Step 5: Verify compilation**

Run: `go build ./internal/runtime/client/`
Expected: PASS.

Run: `go build ./...`
Expected: PASS.

**Step 6: Commit**

```bash
git add internal/runtime/client/input_handler.go
git commit -m "Add Ctrl+S screensaver start and idle reset"
```

---

### Task 8: Full build and test verification

Run all tests and verify the complete build.

**Files:** None (verification only)

**Step 1: Run all effects tests**

Run: `go test ./internal/effects/ -v -count=1`
Expected: All tests PASS (IdleWatcher tests + existing tests).

**Step 2: Run client runtime tests**

Run: `go test ./internal/runtime/client/ -v -count=1`
Expected: All tests PASS.

**Step 3: Run full test suite**

Run: `make test`
Expected: All tests PASS.

**Step 4: Run full build**

Run: `make build`
Expected: All binaries compile.

**Step 5: Commit (if any fixes needed)**

Only if previous steps required fixes.

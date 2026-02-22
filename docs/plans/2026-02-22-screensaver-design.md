# Screensaver System Design

**Date**: 2026-02-22

## Overview

A client-side idle watcher that activates a configurable global effect after a timeout. Replaces the hardcoded Ctrl+R crypt toggle with a general-purpose screensaver system. Future expansion adds a lock overlay with password prompt.

## Requirements

- Activate a configured global effect after N minutes of idle
- Any key or mouse event counts as activity and resets the idle timer
- Any input event dismisses the screensaver (consumed, not forwarded)
- Ctrl+S manually starts the screensaver on demand
- Remove Ctrl+R crypt toggle and `TriggerCryptToggle`
- Config in system config (`texelation.json`)
- Lock feature stubbed in config but not implemented yet

## Architecture

### Idle Watcher

A standalone `IdleWatcher` struct in `internal/effects/` that owns timer state and callbacks. Clean separation from the effects Manager — the watcher triggers effects, effects know nothing about idle.

```go
type IdleWatcher struct {
    mu            sync.Mutex
    timeout       time.Duration
    lockTimeout   time.Duration
    effectID      string
    lockEnabled   bool
    timer         *time.Timer
    lockTimer     *time.Timer
    active        bool         // screensaver is showing
    onActivate    func()       // callback: start effect
    onDeactivate  func()       // callback: stop effect
}
```

**Methods:**
- `ResetActivity()` — called on every input event. If screensaver is active, deactivates it via callback. Resets idle timer.
- `ActivateNow()` — manual activation (Ctrl+S). Starts effect immediately, starts lock timer if configured.
- `Stop()` — cleanup timers on shutdown.

**Internal:**
- `onIdle()` — idle timer fires. Calls `onActivate`, starts lock timer if configured.
- `onLock()` — lock timer fires. Future: activates lock overlay.

### Config

System config section in `texelation.json`:

```json
"screensaver": {
    "enabled": true,
    "timeout_minutes": 5,
    "effect": "crypt",
    "lock_enabled": false,
    "lock_timeout_minutes": 15
}
```

All fields optional with sensible defaults (enabled=false, timeout=5, effect="crypt", lock_enabled=false, lock_timeout=15).

### Event Flow

#### Idle activation
```
No input for timeout_minutes
  -> IdleWatcher.onIdle()
  -> onActivate callback
  -> Manager.HandleTrigger(TriggerScreensaver) activates configured effect
  -> Effect renders on next frame
```

#### Manual activation (Ctrl+S)
```
User presses Ctrl+S
  -> handleScreenEvent() calls idleWatcher.ActivateNow()
  -> onActivate callback
  -> Manager.HandleTrigger(TriggerScreensaver) activates configured effect
```

#### Dismissal
```
User presses any key/mouse while screensaver active
  -> handleScreenEvent() calls idleWatcher.ResetActivity()
  -> idleWatcher sees active=true, calls onDeactivate callback
  -> Manager.HandleTrigger(TriggerScreensaver) deactivates effect
  -> Input event consumed (not forwarded to server)
  -> Idle timer restarted
```

#### Lock escalation (future)
```
Screensaver active, lock_timeout_minutes pass
  -> IdleWatcher.onLock()
  -> Lock overlay activates (password prompt)
  -> Dismissal now requires password, not just any key
```

### Changes to Existing Code

#### Remove
- `input_handler.go`: Remove Ctrl+R handling for `TriggerCryptToggle`
- `triggers.go`: Remove `TriggerCryptToggle` constant
- `client_state.go`: Remove hardcoded crypt binding in `applyEffectConfig()`
- `config.go`: Remove crypt from `DefaultBindings()` if present

#### Add
- `triggers.go`: Add `TriggerScreensaver` constant
- `idle_watcher.go`: New `IdleWatcher` struct
- `input_handler.go`: Add Ctrl+S handling, call `ResetActivity()` on all events, consume input when screensaver active
- `client_state.go`: Create `IdleWatcher` from config, wire callbacks

#### Modify
- `crypt.go`: Handle `TriggerScreensaver` in addition to (or instead of) old trigger. Since we're removing `TriggerCryptToggle`, the crypt effect just needs to respond to `TriggerScreensaver` with a simple activate/deactivate (not toggle).

### Effect Activation Semantics

The screensaver trigger uses explicit activate/deactivate rather than toggle:
- `TriggerScreensaver` with `Active=true` activates the effect
- `TriggerScreensaver` with `Active=false` deactivates the effect

This avoids toggle state getting out of sync if multiple triggers fire. Effects that want to support screensaver mode should handle `TriggerScreensaver` and set their active state directly (not toggle).

### Thread Safety

- `IdleWatcher.mu` protects all mutable state
- Timer callbacks acquire `mu` before checking/modifying state
- `ResetActivity()` and `ActivateNow()` acquire `mu`
- Callbacks (`onActivate`, `onDeactivate`) called with `mu` held — they must not block or re-enter the watcher

### Testing

- Unit test `IdleWatcher` with short timeouts (10ms)
- Test: idle fires after timeout
- Test: `ResetActivity()` prevents firing
- Test: `ResetActivity()` deactivates when active
- Test: `ActivateNow()` activates immediately
- Test: lock timer fires after screensaver
- Integration: verify effect renders when screensaver active

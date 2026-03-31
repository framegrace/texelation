# Unified Game Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the client render loop's competing timing mechanisms with a single fixed-timestep game loop where animations advance by a constant delta and data events render immediately.

**Architecture:** One ticker drives all animations (effects + DynamicColors). The loop maintains a synthetic `effectsClock` that advances by exactly `dt` each tick and is passed to effects. `ctx.T` accumulates from a high-precision float64. Data-driven renders use zero delta. The effects manager drops `requestFrame`/timers and instead sends a one-time wake signal when effects activate.

**Tech Stack:** Go 1.24.3, texelation client runtime, texelui color package

**Spec:** `docs/superpowers/specs/2026-04-01-unified-game-loop-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `texelui/color/dynamic.go` (modify) | Add `DT float32` to `ColorContext` |
| `internal/effects/manager.go` (modify) | Remove requestFrame/timer/frameMu/renderCh, Update takes dt, synthetic clock, wake channel, HasActiveAnimations |
| `internal/runtime/client/client_state.go` (modify) | Remove `animStart`, add `tickAccum float64`, `frameDT float32` |
| `internal/runtime/client/app.go` (modify) | New game loop replacing old select |
| `internal/runtime/client/renderer.go` (modify) | Use `state.tickAccum` for ctx.T, `state.frameDT` for ctx.DT |
| `internal/runtime/client/renderer_test.go` (modify) | Update tests for new time fields |

---

## Task 1: Add DT to ColorContext in texelui

**Files:**
- Modify: `/home/marc/projects/texel/texelui/color/dynamic.go`

### Steps

- [ ] **Step 1: Add DT field to ColorContext**

In `/home/marc/projects/texel/texelui/color/dynamic.go`, add `DT` to the struct:

```go
type ColorContext struct {
	X, Y   int     // Widget-local coordinates
	W, H   int     // Widget dimensions
	PX, PY int     // Pane coordinates
	PW, PH int     // Pane dimensions
	SX, SY int     // Screen-absolute coordinates
	SW, SH int     // Screen dimensions
	T      float32 // Animation time in seconds (accumulated from deltas)
	DT     float32 // Delta time this frame in seconds (0 for data-driven renders)
}
```

- [ ] **Step 2: Run tests**

Run: `cd /home/marc/projects/texel/texelui && go test ./...`
Expected: all pass (DT defaults to zero, existing behavior unchanged).

- [ ] **Step 3: Commit**

```bash
cd /home/marc/projects/texel/texelui
git add color/dynamic.go
git commit -m "Add DT (delta time) field to ColorContext"
```

---

## Task 2: Refactor Effects Manager — Remove Timer Chain, Add Synthetic Clock and Wake Channel

**Files:**
- Modify: `/home/marc/projects/texel/texelation/internal/effects/manager.go`

### Steps

- [ ] **Step 1: Replace Manager struct fields**

Replace the `Manager` struct with:

```go
type Manager struct {
	mu               sync.RWMutex
	bindings         map[EffectTriggerType][]Effect
	paneEffects      []Effect
	workspaceEffects []Effect

	// Synthetic clock for deterministic effect timing.
	// Advances by dt each Update() call.
	effectsClock time.Time

	// Wake channel: effects send here when activating (0→1 active)
	// to signal the game loop to start the ticker.
	wakeCh chan<- struct{}

	// Initialization snapping
	initializing  bool
	initTimestamp time.Time
}
```

- [ ] **Step 2: Update NewManager**

```go
func NewManager() *Manager {
	return &Manager{
		bindings:         make(map[EffectTriggerType][]Effect),
		paneEffects:      make([]Effect, 0),
		workspaceEffects: make([]Effect, 0),
		effectsClock:     time.Now(),
		initializing:     true,
		initTimestamp:    time.Now().Add(-10 * time.Second),
	}
}
```

- [ ] **Step 3: Replace AttachRenderChannel with SetWakeChannel**

Remove `AttachRenderChannel`. Add:

```go
// SetWakeChannel sets the channel the manager uses to signal that
// effects have become active and the game loop should start ticking.
func (m *Manager) SetWakeChannel(ch chan<- struct{}) {
	m.mu.Lock()
	m.wakeCh = ch
	m.mu.Unlock()
}
```

- [ ] **Step 4: Remove requestFrame and RequestFrame**

Delete the entire `requestFrame()` method and the `RequestFrame()` export.

- [ ] **Step 5: Rewrite Update to use delta time and synthetic clock**

```go
// Update advances all effects by dt using the synthetic clock.
// Pass dt=0 for data-driven renders (effects don't advance).
func (m *Manager) Update(dt time.Duration) {
	if m == nil {
		return
	}

	m.effectsClock = m.effectsClock.Add(dt)
	now := m.effectsClock

	m.mu.RLock()
	panes := append([]Effect(nil), m.paneEffects...)
	workspaces := append([]Effect(nil), m.workspaceEffects...)
	m.mu.RUnlock()

	for _, eff := range panes {
		eff.Update(now)
	}
	for _, eff := range workspaces {
		eff.Update(now)
	}
}
```

- [ ] **Step 6: Add HasActiveAnimations**

```go
// HasActiveAnimations returns true if any pane or workspace effect is active.
func (m *Manager) HasActiveAnimations() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, eff := range m.paneEffects {
		if eff.Active() {
			return true
		}
	}
	for _, eff := range m.workspaceEffects {
		if eff.Active() {
			return true
		}
	}
	return false
}
```

- [ ] **Step 7: Update HandleTrigger to send wake signal instead of requestFrame**

Replace the `requestFrame()` call at the end of `HandleTrigger`:

```go
func (m *Manager) HandleTrigger(trigger EffectTrigger) {
	if m == nil {
		return
	}
	if trigger.Timestamp.IsZero() {
		trigger.Timestamp = m.effectsClock
	}
	m.mu.RLock()
	effects := append([]Effect(nil), m.bindings[trigger.Type]...)
	m.mu.RUnlock()
	for _, eff := range effects {
		eff.HandleTrigger(trigger)
	}
	// Wake the game loop if any effect is now active.
	if len(effects) > 0 {
		m.mu.RLock()
		ch := m.wakeCh
		m.mu.RUnlock()
		if ch != nil {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
}
```

Note: `trigger.Timestamp` now defaults to `effectsClock` instead of `time.Now()`.

- [ ] **Step 8: Update PaneStateTriggerTimestamp to use effectsClock**

```go
func (m *Manager) PaneStateTriggerTimestamp() time.Time {
	if m == nil {
		return time.Now()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.initializing {
		return m.initTimestamp
	}
	return m.effectsClock
}
```

- [ ] **Step 9: Update FinishInitialization (no frameMu)**

```go
func (m *Manager) FinishInitialization() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.initializing = false
	m.mu.Unlock()
}
```

- [ ] **Step 10: Update ResetPaneStates (no frameMu)**

The `past` timestamp already uses a fixed past value. No changes needed except removing any `frameMu` reference. Verify it uses `m.HandleTrigger` which now defaults timestamps to `effectsClock`.

Actually — `ResetPaneStates` sets explicit `Timestamp: past` on triggers, so it bypasses the default. This is correct: the past timestamp snaps the effects immediately regardless of `effectsClock`.

- [ ] **Step 11: Run build**

Run: `cd /home/marc/projects/texel/texelation && go build ./internal/effects/`
Expected: build errors in files that call `AttachRenderChannel`, `Update(time.Time)`, etc. These will be fixed in subsequent tasks. For now, verify `manager.go` itself compiles:

Run: `go vet ./internal/effects/`
Expected: may show errors from other files in the package. The manager itself should be correct.

- [ ] **Step 12: Commit**

```bash
git add internal/effects/manager.go
git commit -m "Effects manager: synthetic clock, wake channel, remove requestFrame timer chain"
```

---

## Task 3: Update clientState — Remove animStart, Add Tick Accumulator

**Files:**
- Modify: `/home/marc/projects/texel/texelation/internal/runtime/client/client_state.go`

### Steps

- [ ] **Step 1: Replace animStart with tick accumulator**

In `clientState`, replace:

```go
	// Animation time for client-side DynamicColor resolution
	animStart   time.Time
	dynAnimating bool // true when dynamic cells need continuous rendering
```

With:

```go
	// Fixed-timestep animation state
	tickAccum    float64 // accumulated animation time in seconds (high precision)
	frameDT      float32 // delta time for current frame (0 for data-driven renders)
	dynAnimating bool    // true when dynamic cells need continuous rendering
```

- [ ] **Step 2: Update setRenderChannel**

Replace `AttachRenderChannel` with `SetWakeChannel`:

```go
func (s *clientState) setRenderChannel(ch chan<- struct{}) {
	s.renderCh = ch
	if s.effects != nil {
		s.effects.SetWakeChannel(ch)
	}
}
```

Note: we reuse `renderCh` as the wake channel. When effects activate, they send to `renderCh` which wakes the game loop. The loop then checks `HasActiveAnimations()` and starts the ticker.

- [ ] **Step 3: Remove animStart from app.go initialization**

In `/home/marc/projects/texel/texelation/internal/runtime/client/app.go`, find where `clientState` is constructed (around line 65) and remove `animStart: time.Now()`:

```go
	state := &clientState{
		cache:                   client.NewBufferCache(),
		themeValues:             make(map[string]map[string]interface{}),
		defaultStyle:            tcell.StyleDefault,
		defaultFg:               tcell.ColorDefault,
		defaultBg:               tcell.ColorDefault,
		desktopBg:               tcell.ColorDefault,
		selectionFg:             tcell.ColorBlack,
		selectionBg:             tcell.NewRGBColor(232, 217, 255),
		showRestartNotification: opts.ShowRestartNotification,
	}
```

(Just remove the `animStart` line — it no longer exists.)

- [ ] **Step 4: Build check**

Run: `cd /home/marc/projects/texel/texelation && go build ./internal/runtime/client/ 2>&1`
Expected: errors from renderer.go referencing `animStart`. These will be fixed in Task 4.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/client/client_state.go internal/runtime/client/app.go
git commit -m "Replace animStart with tickAccum/frameDT, wire wake channel"
```

---

## Task 4: Update Renderer to Use Tick Accumulator

**Files:**
- Modify: `/home/marc/projects/texel/texelation/internal/runtime/client/renderer.go`

### Steps

- [ ] **Step 1: Update incrementalComposite — replace animStart with tickAccum**

In `incrementalComposite`, replace:

```go
animTime := float32(time.Since(state.animStart).Seconds())
```

With:

```go
animTime := float32(state.tickAccum)
```

There are TWO occurrences — one in `incrementalComposite` (line ~82) and one in `compositeInto` (line ~348). Replace both.

- [ ] **Step 2: Update render — pass dt to effects.Update**

In the `render` function, find:

```go
	if state.effects != nil {
		state.effects.Update(time.Now())
	}
```

This appears in TWO places (incremental path and inside `fullRender`). Replace both with:

```go
	if state.effects != nil {
		state.effects.Update(time.Duration(state.frameDT * float32(time.Second)))
	}
```

Wait — this is wrong. The `render` function shouldn't call `effects.Update()` at all anymore. The game loop calls it. Let me check...

Actually, looking at the spec: `effects.Update(dt)` is called by the game loop BEFORE `render()`. The render function should NOT call `effects.Update()`. Remove the `effects.Update()` calls from both the incremental path in `render()` and from `fullRender()`.

In `render()`, remove:

```go
	// Incremental path
	if state.effects != nil {
		state.effects.Update(time.Now())
	}
```

In `fullRender()`, remove:

```go
	if state.effects != nil {
		state.effects.Update(time.Now())
	}
```

- [ ] **Step 3: Update ColorContext construction**

In both `incrementalComposite` and `compositeInto`, the `ColorContext` is built with `T: animTime`. No change needed — `animTime` now comes from `state.tickAccum`. But also add `DT`:

Find all `color.ColorContext{` constructions in renderer.go and add `DT: state.frameDT`. There are 4 occurrences (2 in incrementalComposite, 2 in compositeInto). For each, add:

```go
ctx := color.ColorContext{
    X: col, Y: rowIdx,
    W: w, H: h,
    PX: x, PY: y,
    PW: w, PH: h,
    SX: targetX, SY: targetY,
    SW: screenW, SH: screenH,
    T:  animTime,
    DT: state.frameDT,
}
```

- [ ] **Step 4: Update renderer_test.go**

In the test file, find where `clientState` is constructed with `animStart`:

```go
state := &clientState{
    ...
    animStart: time.Now(),
}
```

Remove `animStart`. The tests use `incrementalComposite` and `ensurePrevBuffer` which now read `state.tickAccum` (defaults to 0, which is fine for tests).

- [ ] **Step 5: Build and test**

Run: `cd /home/marc/projects/texel/texelation && go build ./... && make test`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/client/renderer.go internal/runtime/client/renderer_test.go
git commit -m "Renderer uses tickAccum/frameDT, effects.Update called by game loop only"
```

---

## Task 5: Implement the Game Loop

**Files:**
- Modify: `/home/marc/projects/texel/texelation/internal/runtime/client/app.go`

### Steps

- [ ] **Step 1: Replace the entire render loop**

In `app.go`, replace everything from `const frameInterval` through the end of the `for` loop (lines 179-253) with:

```go
	const dt = 16 * time.Millisecond // ~60fps fixed timestep

	// Unified ticker: started when animations or effects are active, stopped when idle.
	var ticker *time.Ticker
	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
	}()

	// Wake channel for effects: when an effect activates, it sends here
	// to wake the loop and start the ticker. Reuses renderCh.
	// (Already wired in setRenderChannel → SetWakeChannel)

	for {
		// Start or stop the unified ticker.
		animating := state.dynAnimating
		if state.effects != nil && state.effects.HasActiveAnimations() {
			animating = true
		}
		if animating && ticker == nil {
			ticker = time.NewTicker(dt)
		} else if !animating && ticker != nil {
			ticker.Stop()
			ticker = nil
		}

		// Build channel ref — nil channel blocks forever in select.
		var tickCh <-chan time.Time
		if ticker != nil {
			tickCh = ticker.C
		}

		select {
		case <-tickCh:
			// Fixed-timestep tick: advance time, update effects, render.
			state.tickAccum += dt.Seconds()
			state.frameDT = float32(dt.Seconds())
			if state.effects != nil {
				state.effects.Update(dt)
			}
			render(state, screen)
			state.frameDT = 0

		case <-renderCh:
			// Data-driven render: delta/snapshot arrived. Render immediately, no time advance.
			state.frameDT = 0
			if state.effects != nil {
				state.effects.Update(0)
			}
			render(state, screen)

		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if !handleScreenEvent(ev, state, screen, conn, sessionID, &writeMu) {
				return nil
			}

		case <-doneCh:
			fmt.Println("Connection closed")
			return nil
		}

		if clip, ok := state.consumeClipboardSync(); ok && len(clip.Data) > 0 {
			debuglog.Printf("CLIPBOARD DEBUG: Setting system clipboard: len=%d", len(clip.Data))
			screen.SetClipboard(clip.Data)
		}
	}
```

- [ ] **Step 2: Remove initial render call with animStart**

Earlier in app.go (around line 149), there's:

```go
	render(state, screen)
```

This is the initial render before the loop starts. Keep it — but `state.tickAccum` is 0 and `state.frameDT` is 0 at this point. The initial render shows whatever data is cached (likely empty). This is fine.

- [ ] **Step 3: Build and test**

Run: `cd /home/marc/projects/texel/texelation && go build ./... && make test`
Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/client/app.go
git commit -m "Unified game loop: fixed-timestep ticker, event-driven data renders"
```

---

## Task 6: Integration Test and Manual Verification

**Files:**
- Modify: `/home/marc/projects/texel/texelation/internal/runtime/client/renderer_test.go`

### Steps

- [ ] **Step 1: Run full test suite**

Run: `cd /home/marc/projects/texel/texelation && make test && make build`
Expected: all pass, build clean.

- [ ] **Step 2: Manual verification**

Test with `./bin/texelation`:
1. **Idle CPU**: near zero with no animations
2. **Typing**: instant feedback, no added latency
3. **Tab nav mode (Pulse)**: smooth 60fps animation, tab and blend line in sync
4. **Focus change**: fade tint animates smoothly at 60fps
5. **Exit nav mode**: animation stops, ticker stops, CPU drops to zero
6. **Resize**: full render, then incremental

- [ ] **Step 3: Commit plan**

```bash
git add docs/superpowers/plans/2026-04-01-unified-game-loop.md
git commit -m "Add implementation plan: unified game loop"
```

---

## Task Summary

| Task | Description | Depends On |
|------|-------------|------------|
| 1 | Add DT to ColorContext (texelui) | — |
| 2 | Refactor effects Manager (synthetic clock, wake, remove timers) | — |
| 3 | Update clientState (tickAccum, frameDT, wake channel wiring) | 2 |
| 4 | Update renderer (use tickAccum, remove effects.Update calls) | 2, 3 |
| 5 | Implement the game loop (app.go) | 2, 3, 4 |
| 6 | Integration test + manual verification | 5 |

Tasks 1 and 2 are independent (different repos/packages).

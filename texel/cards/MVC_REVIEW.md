# TexelApps MVC Architecture Review

## Overview

The TexelApps architecture uses a card-based pipeline pattern that implements a clean separation of concerns aligned with MVC principles. This review evaluates the soundness of the design for standalone usage and future TUI environment development.

## Architecture Components

### 1. Model Layer (Data/State)

**Location**: Apps themselves (e.g., `TexelTerm`, `welcomeApp`)

**Responsibilities**:
- Maintains application state (buffers, dimensions, configuration)
- Handles business logic (VT parsing, data processing)
- Manages lifecycle (Run/Stop)

**Current Implementation**:
```go
type TexelTerm struct {
    vterm     *parser.VTerm    // State
    buf       [][]texel.Cell    // State
    width     int               // State
    height    int               // State
    // ...
}
```

**✅ Strengths**:
- State is encapsulated within app structs
- Clear ownership of data
- Apps can be tested in isolation

**⚠️ Potential Issues**:
- No explicit model interface - apps mix model/controller concerns
- No data binding/observer pattern for state changes
- State mutations scattered across methods

### 2. View Layer (Rendering)

**Location**: `Card.Render()` method + `Pipeline.Render()`

**Responsibilities**:
- Transforms model data into visual representation (`[][]texel.Cell`)
- Handles visual effects (effect cards such as flash, rainbow)
- Composes multiple views via pipeline

**Current Implementation**:
```go
type Card interface {
    Render(input [][]texel.Cell) [][]texel.Cell
    // ...
}
```

**✅ Strengths**:
- Clear separation: View receives model data, returns visual representation
- Pipeline composition allows post-processing (flash, rainbow effects)
- Views are pure functions (no side effects in Render)
- Cards can be chained for layered effects

**✅ Excellent Pattern**:
The pipeline composition is excellent:
```go
flash, _ := cards.NewEffectCard("flash", effects.EffectConfig{
    "duration_ms": 100,
    "color":       "#FFFFFF",
})
pipe := cards.NewPipeline(nil,
    cards.WrapApp(term),    // Model/Controller wrapped
    flash)                  // View layer post-processing
```

**⚠️ Potential Issues**:
- No explicit view update notifications (relies on refresh channel)
- View state (like effect activation flags) mixed with view logic
- Could benefit from explicit view interface separation

### 3. Controller Layer (Input/Events)

**Location**: `Card.HandleKey()`, `Card.HandleMessage()`, `ControlBus`

**Responsibilities**:
- Processes user input (keys, messages)
- Updates model state based on input
- Triggers view updates via control bus

**Current Implementation**:
```go
type Card interface {
    HandleKey(ev *tcell.EventKey)
    HandleMessage(msg texel.Message)
    // ...
}
```

**✅ Strengths**:
- Input handling clearly separated
- ControlBus provides decoupled communication (Observer pattern)
- Pipeline can intercept input via ControlFunc
- Messages provide state change notifications

**✅ Excellent Pattern**:
ControlBus decouples controllers from views:
```go
// App (Controller) triggers effect (View) without knowing implementation
bus.Trigger(cards.FlashTriggerID, nil)
```

**⚠️ Potential Issues**:
- HandleKey/HandleMessage are called on ALL cards (broadcast pattern)
- No explicit controller interface
- Could benefit from explicit input→model→view flow documentation

## Pipeline Architecture Analysis

### Current Flow

```
User Input → Pipeline.HandleKey()
    ↓
ControlFunc (optional intercept)
    ↓
Card.HandleKey() [broadcast to all cards]
    ↓
App (Controller updates Model)
    ↓
Card.Render() [chained pipeline]
    ↓
App.Render() → Base buffer
    ↓
EffectCard(Render) → Post-process overlay
    ↓
Final [][]texel.Cell
```

**✅ Strengths**:
1. **Composition over inheritance**: Cards compose via pipeline
2. **Separation of concerns**: Effects don't know about apps
3. **Testability**: Each card can be tested independently
4. **Extensibility**: Easy to add new cards without modifying apps

### ControlBus Pattern

The ControlBus implements an excellent Observer/Publish-Subscribe pattern:

```go
// Cards (Views) register capabilities
flashCard, _ := cards.NewEffectCard("flash", effects.EffectConfig{
    "duration_ms":   100,
    "color":         "#FFFFFF",
    "max_intensity": 0.75,
})
flashCard.RegisterControls(bus)

// Apps (Controllers) trigger without tight coupling
bus.Trigger(cards.FlashTriggerID, nil)
```

**✅ Excellent Design**:
- Decouples effects from apps
- Enables runtime discovery (`Capabilities()`)
- Apps don't need to know which effects are active
- Supports dynamic registration

## Issues and Recommendations

### 🔴 Critical: Mixed Concerns in App Interface

**Problem**: `texel.App` interface mixes Model, View, and Controller:

```go
type App interface {
    Run() error           // Controller lifecycle
    Stop()               // Controller lifecycle
    Resize(cols, rows int) // Controller notification
    Render() [][]Cell    // View
    GetTitle() string    // Model
    HandleKey(ev *tcell.EventKey) // Controller
    SetRefreshNotifier(...) // Controller coordination
}
```

**Recommendation**: Consider explicit interfaces:

```go
type Model interface {
    State() interface{} // or specific state type
    UpdateState(...) 
}

type View interface {
    Render(model Model) [][]texel.Cell
}

type Controller interface {
    HandleInput(ev *tcell.EventKey) error
    UpdateModel(model Model, input Input) error
}
```

However, this might be over-engineering for current needs. **Current approach is acceptable** if well-documented.

### 🟡 Medium: No Explicit State Change Notifications

**Problem**: Apps manually call refresh channel; no automatic state change detection.

**Current**:
```go
a.refreshChan <- true  // Manual trigger
```

**Recommendation**: Consider observer pattern for model changes:

```go
type ModelObserver interface {
    OnStateChanged(model Model)
}

// Or use channels for reactive updates
```

But current refresh channel approach is simple and works.

### 🟡 Medium: View State in View Layer

**Problem**: Effect cards maintain activation state, mixing view logic with view state.

**Current**:
```go
type EffectCard struct {
    mu     sync.Mutex
    effect effects.Effect // View state
    // ...
}
```

**Consideration**: This is actually fine - view-level state (like animation state) belongs in views. The separation is correct.

### 🟢 Minor: Documentation Gaps

**Recommendation**: Document MVC flow explicitly:

1. **Model**: App state (buffers, dimensions, configuration)
2. **View**: Card.Render() transforms model → visual
3. **Controller**: Card.HandleKey() processes input → updates model
4. **Coordination**: Pipeline orchestrates, ControlBus decouples

## Standalone App Readiness

### ✅ Ready for Standalone

The architecture is **sound for standalone apps**:

1. **Self-contained**: Apps implement all three MVC concerns
2. **Pipeline composition**: Can build complex apps from simple cards
3. **ControlBus**: Apps can trigger effects without desktop dependencies
4. **Lifecycle**: Run/Stop/Resize are well-defined

### Minimal TUI Environment Requirements

For a standalone TUI environment, you'll need:

1. **Screen driver wrapper**: Similar to `texel.ScreenDriver` but standalone
2. **Event loop**: Input polling + render loop (like Desktop.Run())
3. **App factory**: Way to instantiate apps independently
4. **Pipeline execution**: Same card pipeline can run standalone

**Current architecture supports this** - the Pipeline doesn't depend on Desktop.

## Recommendations Summary

### ✅ Keep As-Is (Good Patterns)
1. Card pipeline composition - excellent
2. ControlBus decoupling - excellent  
3. View post-processing via Render chain - excellent
4. AppAdapter for legacy compatibility - good

### 🟡 Consider Improvements
1. **Document MVC roles explicitly** in package docs
2. **Add example standalone app** demonstrating pattern
3. **Consider model observer interface** for reactive updates (optional)
4. **Separate view state from view logic** if needed (but current is fine)

### ❌ Not Recommended
1. Don't over-engineer with strict MVC interfaces - current flexibility is valuable
2. Don't break backward compatibility with `texel.App` interface
3. Don't add unnecessary abstractions - current design is clean

## Conclusion

**Verdict: ✅ MVC architecture is sound and ready for standalone TUI environment**

The card-based pipeline pattern implements MVC principles effectively:
- **Model**: Encapsulated in apps
- **View**: Separate rendering in cards
- **Controller**: Input handling in cards/apps
- **Coordination**: Pipeline + ControlBus

The design is:
- ✅ Clean separation of concerns
- ✅ Composable and extensible
- ✅ Testable in isolation
- ✅ Ready for standalone usage
- ✅ Well-suited for minimal TUI environment

**Action Items**:
1. Add explicit MVC documentation to `cards` package
2. Create standalone app example showing the pattern
3. Document ControlBus usage patterns
4. Consider adding model observer interface (optional enhancement)

# tcell Coupling Analysis

## Executive Summary

The Texelation server currently has deep coupling to the tcell rendering library throughout its core (`texel/`) package. This violates the client-server architecture where:
- **Server** should be headless, managing only state and apps
- **Client** should handle all rendering and tcell interaction

**Impact**: ~8 high-severity issues preventing clean separation.

---

## Critical Issues

### 1. Cell Type Couples Domain to tcell

**Location**: `texel/cell.go:17`

```go
type Cell struct {
    Ch    rune
    Style tcell.Style  // ← tcell dependency
}
```

**Problem**: Every buffer in the system uses `tcell.Style`. This means:
- Protocol must serialize tcell.Style
- Server must understand tcell.Style
- Can't swap rendering backends without changing core types

**Impact Area**: Used in:
- All App.Render() return values
- Pane.renderBuffer() output
- Desktop.SnapshotBuffers()
- Protocol BufferDelta messages
- Client BufferCache

**Refactoring Options**:

**Option A: Protocol-Neutral Style** (Recommended)
```go
// texel/cell.go
type Style struct {
    Fg     Color
    Bg     Color
    Bold   bool
    Underline bool
    // ... other attrs
}

type Cell struct {
    Ch    rune
    Style Style
}
```
- Keep protocol clean
- Server never touches tcell
- Client converts Style → tcell.Style at render time
- **Effort**: HIGH (touches every render path)

**Option B: Keep tcell.Style, Isolate Conversion**
- Accept that Cell uses tcell.Style
- Move all tcell imports to client boundary
- Server creates tcell.Style but doesn't render with it
- **Effort**: MEDIUM (less refactoring)
- **Downside**: Still coupled to tcell API

**Option C: Do Nothing**
- Accept the coupling
- Document that server uses tcell types but not rendering
- **Effort**: NONE
- **Downside**: Architectural impurity remains

---

### 2. App Interface Requires tcell.EventKey

**Location**: `texel/app.go:41`

```go
type App interface {
    Run(quit <-chan struct{})
    Stop()
    Resize(width, height int)
    Render() [][]Cell
    GetTitle() string
    HandleKey(ev *tcell.EventKey)  // ← tcell dependency
    SetRefreshNotifier(chan<- bool)
}
```

**Problem**: All apps must accept `tcell.EventKey`. This means:
- Apps can't be tested without tcell
- Protocol must convert events to tcell format
- Can't use different input systems

**Impact Area**:
- All app implementations (texelterm, statusbar, welcome, clock)
- DesktopSink.HandleKeyEvent() (converts protocol → tcell)
- Desktop.InjectKeyEvent()

**Refactoring Options**:

**Option A: Protocol-Neutral KeyEvent**
```go
type KeyEvent struct {
    Key       KeyCode  // Custom enum
    Rune      rune
    Modifiers ModMask  // Custom flags
}

type App interface {
    HandleKey(ev KeyEvent)  // No tcell
}
```
- Clean separation
- Apps become testable without tcell
- **Effort**: MEDIUM (update 4 apps + Desktop)

**Option B: Keep tcell.EventKey**
- Document that tcell.EventKey is our input model
- Accept the dependency
- **Effort**: NONE

---

### 3. ScreenDriver Interface in Core Package

**Location**: `texel/runtime_interfaces.go:15-26`

```go
type ScreenDriver interface {
    Init() error
    Fini()
    Clear()
    Show()
    SetStyle(style tcell.Style)  // ← tcell
    Size() (width, height int)
    PollEvent() tcell.Event  // ← tcell
    SetContent(x, y int, mainc rune, combc []rune, style tcell.Style)  // ← tcell
    GetContent(x, y int) (rune, []rune, tcell.Style, int)  // ← tcell
    HasMouse() bool
}
```

**Problem**: This interface is in `texel/` but is purely a rendering abstraction. Server doesn't need it.

**Impact**:
- Desktop.display field uses this
- Desktop.Run() calls PollEvent()
- Only used by cmd/texel-server for simulation screen

**Refactoring Options**:

**Option A: Move to Client** (Recommended)
- Move ScreenDriver → `internal/runtime/client/`
- Desktop.Run() becomes client-only
- Server never instantiates Desktop.display
- **Effort**: MEDIUM (restructure Desktop initialization)

**Option B: Split Interface**
- Keep size/dimensions in texel/
- Move rendering methods to client/
- **Effort**: HIGH (complex refactor)

---

### 4. Desktop.Run() is a Rendering Loop

**Location**: `texel/desktop.go:327-396`

```go
func (d *Desktop) Run() error {
    eventChan := make(chan tcell.Event, 10)
    go func() {
        for {
            eventChan <- d.display.PollEvent()  // ← Polling UI events
        }
    }()

    for {
        select {
        case ev := <-eventChan:
            d.handleEvent(ev)  // ← Processing UI events
        case <-refreshChan:
            d.draw()  // ← Rendering
        }
    }
}
```

**Problem**: This is a **client rendering loop** in the core domain. Server should never call this.

**Current Reality**:
- Server DOES instantiate Desktop
- Server does NOT call Desktop.Run()
- Server calls Desktop.SnapshotBuffers() instead

**Refactoring Options**:

**Option A: Extract Client Runtime** (Recommended)
```go
// internal/runtime/client/loop.go
type DesktopClient struct {
    desktop *texel.Desktop
    screen  tcell.Screen
}

func (dc *DesktopClient) Run() error {
    // Move event loop here
}
```
- Desktop becomes pure state
- Client wraps Desktop for rendering
- **Effort**: MEDIUM

**Option B: Conditional Compilation**
- Mark Desktop.Run() as client-only with build tags
- Server code can't accidentally call it
- **Effort**: LOW

---

### 5. Pane Rendering Uses tcell Types

**Location**: `texel/pane.go:204-280`

```go
func (p *pane) renderBuffer(applyEffects bool) [][]Cell {
    // Uses tcell extensively:
    defstyle := tcell.StyleDefault.Background(desktopBg).Foreground(desktopFg)
    buffer[0][x] = Cell{Ch: tcell.RuneHLine, Style: borderStyle}
    buffer[0][0] = Cell{Ch: tcell.RuneULCorner, Style: borderStyle}
    // ...
}
```

**Problem**: This method runs on BOTH server (during snapshot) and client (during rendering).

**Current Reality**:
- Server calls `p.renderBuffer(false)` in snapshot.go:81
- applyEffects=false means no visual effects
- But still creates tcell.Style objects

**Refactoring Options**:

**Option A: Server Captures Raw App Output**
- Server never calls renderBuffer()
- Server just captures app.Render() directly
- Client adds borders during rendering
- **Effort**: HIGH (client must learn about borders)

**Option B: Accept Dual Usage**
- Both server and client can render panes
- Server rendering is "headless" but valid
- **Effort**: NONE

---

### 6. Theme System Returns tcell.Color

**Location**: `texel/theme/theme.go:47`

```go
func (t *Theme) GetColor(section, key string, fallback tcell.Color) tcell.Color {
    // ...
}
```

**Problem**: Theme values are tcell.Color, coupling config to rendering.

**Impact**: Desktop stores DefaultFgColor/DefaultBgColor as tcell.Color

**Refactoring Options**:

**Option A: Return RGB Values**
```go
type Color struct {
    R, G, B uint8
}

func (t *Theme) GetColor(section, key string, fallback Color) Color
```
- Client converts Color → tcell.Color when needed
- **Effort**: MEDIUM

**Option B: Keep tcell.Color**
- Theme is already a client concept
- Server reads themes for protocol purposes
- **Effort**: NONE

---

### 7. StatePayload Contains tcell.Color

**Location**: `texel/dispatcher.go:44`

```go
type StatePayload struct {
    // ...
    DesktopBgColor tcell.Color
}
```

**Problem**: Event payload carries rendering type.

**Simple Fix**: Change to RGB triple or hex string.

---

### 8. convertStyle() in Server Runtime

**Location**: `internal/runtime/server/desktop_publisher.go:133-150`

```go
func convertStyle(style tcell.Style) (styleKey, protocol.StyleEntry) {
    fg, bg, attrs := style.Decompose()
    // ...
}
```

**Problem**: Server runtime imports tcell to convert styles.

**Reality**: This is unavoidable if Cell uses tcell.Style. The conversion must happen somewhere.

**Options**:
- If Cell becomes protocol-neutral (Issue #1), this goes away
- Otherwise, accept this conversion as necessary bridge

---

## Recommended Action Plan

### Phase 1: Low-Hanging Fruit (COMPLETED ✓)
- [x] Remove server-side effects system (DONE - 723 lines)
- [x] Delete GraphicsOverlay.go (DONE - 272 lines)

### Phase 2: Documentation and Decision
- [x] Document tcell coupling issues (THIS FILE)
- [ ] **User Decision Required**: Choose refactoring approach

### Phase 3: Potential Refactoring (Based on Decision)

**If pursuing full separation**:
1. Create protocol.Style type (replaces tcell.Style in Cell)
2. Create protocol.KeyEvent type (replaces tcell.EventKey in App)
3. Move ScreenDriver to client package
4. Extract Desktop.Run() to client runtime wrapper
5. Make server snapshot capture app-only buffers (no borders)

**Estimated effort**: 2-3 days of refactoring + testing

**If accepting current architecture**:
1. Document that texel/ is a "shared domain" using tcell types
2. Clarify that tcell types are our protocol standard
3. Accept that server has tcell dependency (but doesn't render)

**Estimated effort**: 1 hour of documentation

---

## Questions for Architectural Decision

1. **Is full tcell independence worth the refactoring cost?**
   - Pro: Clean architecture, swappable rendering backends
   - Con: Large refactor, tcell is deeply embedded

2. **Should server render pane borders, or should client?**
   - Current: Server renders borders in snapshots
   - Alternative: Client adds borders during rendering

3. **Is tcell.Style as a protocol type acceptable?**
   - Pro: Works today, proven stable
   - Con: Protocol coupled to one library

4. **Should we split Desktop into ServerDesktop and ClientDesktop?**
   - Pro: Clear separation of concerns
   - Con: More code, duplication possible

---

## Current Status

**Working System**: The current architecture works correctly despite the coupling:
- Server snapshots use tcell types but don't render to screen
- Client applies all visual effects
- Protocol successfully transmits tcell.Style
- No actual bugs from this coupling

**Technical Debt**: The coupling is architectural impurity, not functional breakage.

**Recommendation**: **Document and accept current state** unless you plan to support alternative rendering backends (web, native GUI, etc.). The refactoring would be large and the practical benefit is unclear given that tcell works well for terminal use cases.

If you ever need web or GUI clients, THEN refactor to protocol-neutral types.

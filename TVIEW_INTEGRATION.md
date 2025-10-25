# TView Integration Architecture

## Overview

This document describes how tview (a TUI framework) is integrated into Texelation apps to provide rich terminal UI widgets without flickering or blocking.

## Key Design Principles

1. **App-level integration**: tview overlaying happens at the app level, not at the pane/desktop level
2. **Background thread**: tview runs in its own goroutine with its own event loop
3. **Thread-safe buffer access**: The VirtualScreen buffer is protected by a RWMutex
4. **No blocking**: App.Run() starts tview in background and returns immediately
5. **Continuous updates**: tview continuously updates the VirtualScreen buffer at its own pace
6. **On-demand reads**: App.Render() reads the current buffer state (thread-safe)

## Architecture

```
┌─────────────────────────────────────────┐
│  Texel App (e.g., StaticTViewWelcome)   │
│                                         │
│  Run() ──> Creates TViewApp             │
│            Starts tview in goroutine ─┐ │
│                                        │ │
│  Render() ──> Reads VirtualScreen ─┐  │ │
│                                     │  │ │
└─────────────────────────────────────┼──┼─┘
                                      │  │
                                      │  ▼
┌─────────────────────────────────────┼──────────────────────┐
│  TViewApp (tview wrapper)           │                      │
│                                     │                      │
│  ┌─────────────────────────────────┐│                      │
│  │  tview.Application.Run()        ││                      │
│  │  (blocking, in goroutine)       │◄─┘                    │
│  │                                  │                      │
│  │  Draw cycle:                     │                      │
│  │    SetContent() ─────┐           │                      │
│  │    Clear()     ──────┼──> backBuffer                    │
│  │    Fill()      ──────┘           │                      │
│  │    Show()  ────┐                 │                      │
│  │                │                 │                      │
│  └────────────────┼─────────────────┘                      │
│                   │                                        │
│                   ▼  (swap)                                │
│  ┌────────────────────────────────────┐                    │
│  │   VirtualScreen (double buffered)  │                    │
│  │                                    │                    │
│  │   backBuffer [][]Cell  ◄─── tview writes here          │
│  │   frontBuffer [][]Cell ◄─── readers see this ──────────┘
│  │                            (GetBuffer)                  │
│  │   Show() swaps buffers:                                │
│  │     frontBuffer, backBuffer = backBuffer, frontBuffer  │
│  │                                                         │
│  │   mu sync.RWMutex (protects both buffers & swap)       │
│  └─────────────────────────────────────┘                  │
└────────────────────────────────────────────────────────────┘
```

### Double Buffering Flow

1. **Frame N rendering**: tview draws frame N to `backBuffer`
   - Multiple SetContent/Clear/Fill calls
   - frontBuffer still contains frame N-1 (visible to readers)

2. **Frame N complete**: tview calls `Show()`
   - Swaps buffers (atomic operation under mutex)
   - frontBuffer now has frame N (visible to readers)
   - backBuffer now has frame N-1 (ready for next draw)

3. **Frame N+1 rendering**: tview starts drawing frame N+1 to `backBuffer`
   - Readers see stable frame N from frontBuffer
   - No tearing, no partial frames visible

## Key Components

### VirtualScreen (`internal/tviewbridge/virtual_screen.go`)

Implements `tcell.Screen` interface as an in-memory buffer:
- Captures all tview draw calls into `[][]texel.Cell`
- Thread-safe with `sync.RWMutex`
- `GetBuffer()` returns deep copy for thread safety
- All draw methods (SetContent, Clear, Fill) are protected by mutex

### TViewApp (`internal/tviewbridge/tview_app.go`)

Wraps tview.Application to work as a texel.App:
- `Run()`: Creates VirtualScreen, starts tview in goroutine, returns immediately
- `Render()`: Thread-safe read from VirtualScreen buffer
- `Stop()`: Stops tview event loop
- `Resize()`: Forwards resize to VirtualScreen
- `HandleKey()`: Posts events to VirtualScreen for tview to process

### StaticTViewWelcome (`apps/welcome/welcome_static_tview.go`)

Example app using tview integration:
- Creates tview widgets (TextView with colors, borders, etc.)
- Wraps in TViewApp
- Forwards all lifecycle calls to TViewApp
- `Render()` reads from TViewApp's VirtualScreen buffer

## Thread Safety and Frame Consistency

The design ensures both thread safety and frame consistency through:

1. **Double buffering**: VirtualScreen maintains two buffers (front and back)
   - tview draws to `backBuffer` (SetContent, Clear, Fill)
   - `Show()` swaps buffers: back becomes front, front becomes back
   - Readers always see `frontBuffer` (GetBuffer, GetContent, GetCells)
   - This eliminates "tearing" - readers never see partial/incomplete frames

2. **VirtualScreen mutex**: All buffer access and swaps are protected by RWMutex
   - Write operations (SetContent, etc.) use Lock/Unlock
   - Read operations (GetBuffer, etc.) use RLock/RUnlock
   - Buffer swap in Show() uses Lock/Unlock (atomic pointer swap)

3. **Deep copy on read**: `GetBuffer()` returns a copy, not a reference
   - Prevents readers from seeing buffer modifications

4. **Independent loops**: tview and texel run independently, no shared state
   - tview draws at its own pace
   - texel reads whenever Render() is called
   - No blocking or busy waiting

5. **Frame-based synchronization**: Show() acts as frame boundary
   - tview calls Show() after completing each frame
   - Buffer swap makes the completed frame visible to readers
   - Next frame starts drawing to the (now) back buffer

## Performance

- **No flickering**: tview runs at its own pace, desktop reads when needed
- **No busy waiting**: tview uses proper event loop
- **Minimal overhead**: GetBuffer() only copies when needed
- **Delta updates**: Protocol-level BufferDelta handles efficient client updates

## Example Usage

```go
// In your app factory
welcomeFactory := func() texel.App {
    return welcome.NewStaticTView()
}

// In the app implementation
func (w *MyTViewApp) Run() error {
    // Create tview widgets
    textView := tview.NewTextView().
        SetDynamicColors(true).
        SetText("[yellow]Hello[white] World!")

    // Wrap in TViewApp (starts tview in background)
    w.tviewApp = tviewbridge.NewTViewApp("My App", textView)
    return w.tviewApp.Run()
}

func (w *MyTViewApp) Render() [][]texel.Cell {
    // Read current buffer (thread-safe)
    return w.tviewApp.Render()
}
```

## Testing

Integration test: `internal/runtime/server/tview_welcome_test.go`
- Creates app, calls Run()
- Waits 100ms for initial render
- Verifies buffer contains expected content
- No blocking, no flickering

Run with:
```bash
go test -tags=integration ./internal/runtime/server -run TestTViewWelcomeRendersContent -v
```

## Future Enhancements

1. **Key event routing**: Forward specific keys to tview widgets for interactivity
2. **Focus management**: Switch between tview widgets and texel panes
3. **Compositing**: Overlay tview on top of base buffer (transparent cells)
4. **Multiple widgets**: Support multiple tview widgets in one app
5. **Mouse support**: Forward mouse events to tview

## Related Files

- `internal/tviewbridge/tview_app.go` - TViewApp wrapper
- `internal/tviewbridge/virtual_screen.go` - VirtualScreen implementation
- `apps/welcome/welcome_static_tview.go` - Example tview app
- `apps/welcome/welcome_simple_colored.go` - Non-tview alternative
- `texel/overlay.go` - Buffer compositing utilities

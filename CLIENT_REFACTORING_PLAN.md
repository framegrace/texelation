# Client Refactoring Plan - IN PROGRESS

## Status: 4/8 Complete (colors.go, ui_state.go, message_sender.go, input_handler.go extracted)

## Goal
Split monolithic `internal/runtime/client/app.go` (955 lines → ~100 lines) into 8 focused modules for better testability, maintainability, and reusability.

---

## Extraction Order (Bottom-Up by Dependencies)

### ✅ 1. colors.go - COMPLETED
**Lines**: ~40 (extracted from app.go lines 708-728)
**Dependencies**: None (pure utilities)
**Status**: ✓ Done (Commit 86f8818)

**Contents**:
- `parseHexColor(value string) (tcell.Color, bool)` - Parse hex color strings
- `colorFromRGB(rgb uint32) tcell.Color` - Convert packed RGB values

---

### ✅ 2. ui_state.go - COMPLETED
**Lines**: ~150 (extracted from app.go lines 40-145 + 632-705)
**Dependencies**: colors.go
**Status**: ✓ Done

**What to Extract**:

```go
// Type definition (lines 41-68)
type uiState struct {
    cache         *client.BufferCache
    clipboard     protocol.ClipboardData
    hasClipboard  bool
    theme         protocol.ThemeAck
    hasTheme      bool
    focus         protocol.PaneFocus
    hasFocus      bool
    themeValues   map[string]map[string]interface{}
    defaultStyle  tcell.Style
    defaultFg     tcell.Color
    defaultBg     tcell.Color
    workspaces    []int
    workspaceID   int
    activeTitle   string
    controlMode   bool
    subMode       rune
    desktopBg     tcell.Color
    zoomed        bool
    zoomedPane    [16]byte
    pasting       bool
    pasteBuf      []byte
    renderCh      chan<- struct{}
    effects       *effects.Manager
    resizeMu      sync.Mutex
    pendingResize protocol.Resize
    resizeSeq     uint64
}

// Methods (lines 70-146)
func (s *uiState) setRenderChannel(ch chan<- struct{})
func (s *uiState) setThemeValue(section, key string, value interface{})
func (s *uiState) scheduleResize(...)
func (s *uiState) applyEffectConfig()

// State update methods (lines 633-706)
func (s *uiState) updateTheme(update protocol.StateUpdate)
func (s *uiState) recomputeDefaultStyle()
func (s *uiState) applyStateUpdate(update protocol.StateUpdate)
```

**After Extraction**:
- Remove from app.go
- Keep imports: sync, time, tcell, effects, protocol, client, theme
- File header: "UI state management and theme application for client runtime"

---

### ✅ 3. message_sender.go - COMPLETED
**Lines**: ~70 (extracted from app.go lines 528-598)
**Dependencies**: None (uses only protocol, net, sync, tcell)
**Status**: ✓ Done

**What to Extract**:
```go
func sendResize(writeMu *sync.Mutex, conn net.Conn, sessionID [16]byte, screen tcell.Screen)
func sendResizeMessage(writeMu *sync.Mutex, conn net.Conn, sessionID [16]byte, resize protocol.Resize)
func sendKeyEvent(writeMu *sync.Mutex, conn net.Conn, sessionID [16]byte, ev *tcell.EventKey)
func sendPaste(writeMu *sync.Mutex, conn net.Conn, sessionID [16]byte, data []byte)
func writeMessage(writeMu *sync.Mutex, conn net.Conn, msg protocol.Message) error
```

**Implementation Detail**: All functions take `writeMu` and `conn` explicitly (no receiver).

---

### ✅ 4. input_handler.go - COMPLETED
**Lines**: ~175 (extracted from app.go lines 412-586)
**Dependencies**: message_sender.go, ui_state.go
**Status**: ✓ Done

**What to Extract**:
```go
func handleScreenEvent(ev tcell.Event, state *uiState, screen tcell.Screen, ...) bool
func consumePasteKey(state *uiState, ev *tcell.EventKey) bool
func isNetworkClosed(err error) bool
```

**Key Logic**:
- Keyboard event routing
- Mouse event handling
- Paste buffer accumulation
- Resize triggering
- Control mode echo (client-side F key flash)

---

### 5. renderer.go - NEXT
**Lines**: ~120 (app.go lines ~350-410)
**Dependencies**: ui_state.go, colors.go
**Status**: TODO

**What to Extract**:
```go
func render(state *uiState, screen tcell.Screen)
func applyZoomOverlay(buffer [][]client.Cell, state *uiState)
func blendColor(fg, bg tcell.Color, alpha float64) tcell.Color
```

**Key Logic**:
- Update effects timeline
- Create workspace buffer
- Composite panes by z-order
- Apply effects
- Render to tcell screen
- Handle zoom overlay

---

### 6. protocol_handler.go
**Lines**: ~150 (app.go lines 284-421)
**Dependencies**: ui_state.go
**Status**: TODO

**What to Extract**:
```go
func readLoop(conn net.Conn, state *uiState, sessionID [16]byte, ...)
func handleControlMessage(msg protocol.Message, state *uiState, ...) error
```

**Key Logic**:
- Network read loop
- Message decoding
- TreeSnapshot → BufferCache
- BufferDelta → BufferCache
- State updates
- Effect triggers

---

### 7. background_tasks.go
**Lines**: ~120 (app.go lines 774-888)
**Dependencies**: message_sender.go
**Status**: TODO

**What to Extract**:
```go
func pingLoop(conn net.Conn, sessionID [16]byte, writeMu *sync.Mutex, doneCh <-chan struct{})
func ackLoop(conn net.Conn, sessionID [16]byte, writeMu *sync.Mutex, pendingAck *atomic.Uint64, ackSignal <-chan struct{}, doneCh <-chan struct{})
func scheduleAck(pendingAck *atomic.Uint64, seq uint64, ackSignal chan<- struct{})
```

**Key Logic**:
- Ping/pong keep-alive (every 10s)
- Buffer acknowledgment loop
- Atomic ack scheduling

---

### 8. session.go
**Lines**: ~200 (app.go lines 148-282 + setup utilities)
**Dependencies**: All above modules
**Status**: TODO (FINAL)

**What to Extract**:
```go
// Keep in this file:
type Options struct { ... }
const resizeDebounce = 45 * time.Millisecond

func Run(opts Options) error  // Main orchestration
func setupLogging() (*os.File, error)
func formatPaneID(id [16]byte) string
```

**Key Logic**:
- Connection/reconnection
- Session initialization
- Theme loading
- Goroutine coordination (readLoop, pingLoop, ackLoop, render loop)
- Main event loop
- Cleanup on exit

---

## Build Strategy

**After each extraction**:
1. Create new file with proper header
2. Move code from app.go
3. Add necessary imports
4. Run `make build`
5. Fix any issues
6. Test again
7. Commit with message: "Extract [filename]: [brief description]"

**Important**: Keep app.go buildable at every step!

---

## Current State (After input_handler.go)

**app.go**: ~575 lines (was 955)
**Extracted**:
- 21 lines → colors.go
- 114 lines → ui_state.go
- 70 lines → message_sender.go
- 175 lines → input_handler.go
**Build**: ✓ Passing

---

## Next Steps

1. ✅ Extract colors.go
2. ✅ Extract ui_state.go (struct + methods)
3. Extract message_sender.go
4. Test build
5. Continue through list...

---

## Testing After Complete Refactoring

Once all files are extracted:
1. Run full build
2. Test client connection to server
3. Verify rendering works
4. Verify control mode works
5. Verify effects work
6. Verify reconnection works

---

## Line Count Tracking

| File | Lines | Status |
|------|-------|--------|
| colors.go | 40 | ✓ Done |
| ui_state.go | 150 | ✓ Done |
| message_sender.go | 70 | ✓ Done |
| input_handler.go | 175 | ✓ Done |
| renderer.go | ~120 | TODO |
| protocol_handler.go | ~150 | TODO |
| background_tasks.go | ~120 | TODO |
| session.go | ~200 | TODO |
| **Total** | **~1025** | **4/8** |
| app.go (after) | ~100 | Will contain Run() + imports + Options |

---

## Success Criteria

- ✅ Each module has single clear responsibility
- ✅ Dependencies flow in one direction (no cycles)
- ✅ Build passes after each extraction
- ✅ All functionality preserved
- ✅ Easier to test components in isolation
- ✅ Client code reusable for headless/automated clients

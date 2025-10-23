# Future Improvements

This document tracks architectural improvements that have been identified but deferred for later implementation.

---

## 1. Extract ControlModeHandler from DesktopEngine

**Priority**: Medium
**Effort**: Medium (2-3 hours)
**Benefit**: Better separation of concerns, testability

### Problem

DesktopEngine currently handles three distinct responsibilities:
1. Workspace coordination (switching between workspaces)
2. Control mode command processing (Ctrl+A commands)
3. Layout coordination (status panes, main area calculation)

The control mode logic (~200 lines) could be extracted into its own handler.

### Proposed Solution

Create `texel/control_mode.go` with:

```go
type ControlModeHandler struct {
    active          bool
    subMode         rune
    resizeSelection *selectedBorder
    desktop         *DesktopEngine
}

func (c *ControlModeHandler) Toggle()
func (c *ControlModeHandler) IsActive() bool
func (c *ControlModeHandler) HandleCommand(ev *tcell.EventKey, desktop *DesktopEngine)
```

### Benefits

- **Single Responsibility**: Each component has one clear purpose
- **Testability**: Control mode logic can be unit tested independently
- **Extensibility**: Easy to add new commands or keybinding configuration
- **Clarity**: State like `inControlMode` becomes `controlModeHandler.IsActive()`

### When to Implement

Consider this improvement when:
- Adding 5+ more control mode commands
- Implementing configurable keybindings
- Adding multiple input modes (vi mode, emacs mode, etc.)
- Control mode logic grows beyond 200 lines

### Implementation Notes

1. Create `texel/control_mode.go`
2. Move control mode state from DesktopEngine:
   - `inControlMode` → `ControlModeHandler.active`
   - `subControlMode` → `ControlModeHandler.subMode`
   - `resizeSelection` → `ControlModeHandler.resizeSelection`
3. Move methods:
   - `toggleControlMode()` → `Toggle()`
   - `handleControlMode()` → `HandleCommand()`
4. Update DesktopEngine:
   - Add `controlModeHandler *ControlModeHandler` field
   - Delegate to handler in `handleEvent()`
5. Update state broadcasting to use `controlModeHandler.IsActive()`

### Risks

- Circular dependency between DesktopEngine and ControlModeHandler
- More indirection in call paths
- Need careful initialization order

---

## 2. Split Client app.go into Focused Modules

**Priority**: High
**Effort**: High (4-6 hours)
**Benefit**: Huge - testability, maintainability, reusability

### Problem

`internal/runtime/client/app.go` is 955 lines with 7+ distinct responsibilities:
- Session/connection management
- Protocol message handling
- Rendering pipeline
- Input event handling
- Message sending
- Background tasks (ping/ack)
- UI state management
- Color/style helpers
- Effects coordination

This makes it hard to test components in isolation and impossible to reuse (e.g., headless client).

### Proposed Structure

```
internal/runtime/client/
├── session.go           - Run() loop, connection setup
├── protocol_handler.go  - Message decoding, readLoop()
├── renderer.go          - render(), buffer composition
├── input_handler.go     - Keyboard/mouse/paste handling
├── message_sender.go    - Protocol encoding, sendKey/sendPaste
├── background_tasks.go  - pingLoop(), ackLoop()
├── ui_state.go          - uiState struct, theme/state updates
├── colors.go            - Color parsing and conversion
└── effects.go           - Effect config, zoom overlay
```

### Benefits

- **Testability**: Each component can be unit tested
- **Reusability**: Protocol handler could be used in headless/automated clients
- **Clarity**: Clear data flow and responsibilities
- **Maintainability**: Changes isolated to specific files

### When to Implement

- After Screen→Workspace rename is stable
- Before adding major client features (web client, recording, etc.)

---

## 3. Remove Dead blit/blitDiff Functions

**Priority**: Low
**Effort**: Low (15 minutes)
**Benefit**: Small cleanup

### Problem

`texel/workspace.go` lines 559-575 contain unused `blit()` and `blitDiff()` functions that were part of the old rendering system.

### Solution

Simply delete these functions after verifying no references exist.

---

## 4. Consider Protocol-Neutral Cell/Style Types

**Priority**: Low (unless non-terminal clients planned)
**Effort**: Very High (1-2 weeks)
**Benefit**: Would enable web/GUI clients

### Current Architecture

- `Cell` uses `tcell.Style` directly
- Apps produce tcell buffers
- Protocol serializes tcell.Style
- Design decision: tcell is permanent backend

### If Requirements Change

If you ever need non-terminal clients (web UI, native GUI):
1. Create protocol-neutral `protocol.Style` type
2. Create protocol-neutral `protocol.Cell` type
3. Server works with protocol types
4. Terminal client converts `protocol.Style` → `tcell.Style` at render time

**Current Status**: NOT RECOMMENDED. tcell coupling is intentional for maximum speed.

---

## Notes

- Priorities are relative and may change based on project needs
- Effort estimates are rough guidelines
- Each improvement should be implemented in isolation with full testing
- Commit history preserved for educational purposes

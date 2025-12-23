# Texelterm Input Handling Rewrite State

## Branch
`fix/texelterm-input-handling`

## Rewrite Overview (Session 4)
The input management layer was rewritten from scratch to support the 3-level persistent history structure (Disk -> Memory -> DisplayBuffer).

### The Problem
The previous implementation used a "dual-write" approach where `VTerm` managed cursor position physically while `DisplayBuffer` tried to track a parallel `currentLogicalX`. This desynchronized during complex wrapping, history navigation, and edits, causing visual glitches (ghost characters, missing updates).

### The Solution: "Logical Editor" Architecture
We made `DisplayBuffer` the single source of truth for the current line's state.

1.  **Logical Cursor Mapping (`GetLogicalPos`)**:
    - Maps any physical viewport coordinate `(x, y)` to a precise `(LogicalLineIndex, Offset)`.
    - Handles wrapped lines correctly.

2.  **Logical Editor API**:
    - `DisplayBuffer.SetCursor(physX, physY)`: Updates internal logical cursor state.
    - `DisplayBuffer.Write(rune)`: Inserts/overwrites at the logical cursor position.
    - `DisplayBuffer.Erase(mode)`: Handles EL 0/1/2 on the logical line.

3.  **Robust Dirty Tracking**:
    - Any change to the logical line triggers `RebuildCurrentLine` and `MarkAllDirty` to ensure correct rendering of wrapped lines.

4.  **Cursor Synchronization on Resize**:
    - Implemented `GetPhysicalCursorPos` to map the logical cursor back to a physical coordinate.
    - `displayBufferResize` now updates `v.cursorX/Y` to match the logical cursor's new position after reflow.
    - This fixes the "cursor one line off" and "overwrite" issues during resize.

5.  **Viewport Logic Fix**:
    - Updated `contentLineCount` to always include the current line (even if empty), ensuring the viewport scrolls correctly to show the prompt.

### Test Results
All tests pass, including:
- `TestDisplayBuffer_ResizeReflow_RoundTrip`: Verifies cursor consistency after shrink/expand.
- `TestDisplayBuffer_ResizeCursorAdjustment`: Verifies logical-to-physical mapping.
- All existing integration tests.

### Files Modified
- `apps/texelterm/parser/display_buffer.go`
- `apps/texelterm/parser/vterm_display_buffer.go`
- `apps/texelterm/parser/vterm_cursor.go`
- `apps/texelterm/parser/vterm_scroll.go`
- `apps/texelterm/parser/display_buffer_logical_test.go`
- `apps/texelterm/term.go` (Added logging)

The system is now robust against wrapping edge cases, shell history navigation rewrites, and resize operations.

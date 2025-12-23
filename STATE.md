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
    - Handles wrapped lines correctly (e.g., Row 2 Col 0 maps to Offset 80 of the same logical line).

2.  **Logical Editor API**:
    - `DisplayBuffer.SetCursor(physX, physY)`: Updates internal logical cursor state.
    - `DisplayBuffer.Write(rune)`: Inserts/overwrites at the logical cursor position.
    - `DisplayBuffer.Erase(mode)`: Handles EL 0/1/2 on the logical line.
    - `DisplayBuffer.DeleteCharacters(n)` / `EraseCharacters(n)`: Handles DCH/ECH.

3.  **Robust Dirty Tracking**:
    - Any change to the logical line triggers `RebuildCurrentLine`.
    - Since re-wrapping can shift content vertically (changing height), we currently use `MarkAllDirty` to ensure correctness. This guarantees the visual state always matches the internal data.

4.  **Simplified VTerm Integration**:
    - `vterm_display_buffer.go` now delegates all edits to the Logical Editor.
    - Removed manual `currentLogicalX` tracking and synchronization logic.
    - `SetCursorPos` automatically syncs the logical cursor.

### Test Results
All tests pass, including:
- New logical mapping tests (`TestDisplayBuffer_GetLogicalPos`, `TestDisplayBuffer_LogicalEditor`)
- Regression tests for wrapping (`TestDisplayBuffer_WrapDirtyTrackingRegression`)
- All existing integration tests.

### Files Modified
- `apps/texelterm/parser/display_buffer.go`: Added Logical Editor logic.
- `apps/texelterm/parser/vterm_display_buffer.go`: Simplified to use Logical Editor.
- `apps/texelterm/parser/vterm_cursor.go`: Added sync call in `SetCursorPos`.
- `apps/texelterm/parser/vterm_scroll.go`: Removed obsolete sync logic.
- `apps/texelterm/parser/display_buffer_integration_test.go`: Updated to use new API.
- `apps/texelterm/parser/vterm_display_buffer_test.go`: Updated to use `SetCursorPos`.

The system is now robust against wrapping edge cases and shell history navigation rewrites.
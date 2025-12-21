# Texelterm Input Handling Investigation State

## Branch
`fix/texelterm-input-handling`

## Issue Description
When running texelterm standalone (`./bin/texelterm 2> /dev/null`) and typing at a bash prompt past the end of a line width:
- Cursor correctly wraps to the next line
- Characters don't appear on screen
- When pressing Enter, bash interprets the full line correctly (buffer is correct, display is wrong)

User suspected persistence might be related.

## Changes Made This Session

### 1. Fixed duplicate test function
- Removed duplicate `TestDisplayBuffer_WrapAfterLoadingHistory` from `display_buffer_integration_test.go`

### 2. Added debug logging infrastructure
Files modified:
- `apps/texelterm/parser/display_buffer.go` - Added `debugLog` field and `SetDebugLog` method
- `apps/texelterm/parser/vterm_display_buffer.go` - Added `SetDisplayBufferDebugLog` method
- `apps/texelterm/term.go` - Added `renderDebugLog` field and initialization

Enable debug logging:
```bash
TEXELTERM_DEBUG=1 ./bin/texelterm
```
Logs written to: `/tmp/texelterm-debug.log`

### 3. Added comprehensive wrap tests
- `TestDisplayBuffer_WrapAfterLoadingHistory` - Tests wrapping when history loaded from disk
- `TestDisplayBuffer_DevshellRunnerFlowWithDisk` - Tests devshell runner pattern with disk persistence

All tests pass.

## Investigation Findings

### Debug Log Analysis
From debug logs, the wrap functionality appears correct:
- When characters cause wrap, **both** old and new lines are marked dirty
- `Grid()` returns correct content with wrapped lines
- Dirty tracking: `dirtyLines=map[0:true 1:true]` when cursor moves to row 1

Example from debug log:
```
[RENDER] Render: cursorX=0, cursorY=1, allDirty=false, dirtyLines=map[0:true 1:true]
[RENDER]   vtermGrid[0]: "HIJKLMNOPQRSTUVWXYZ0123456789abcdefghijk"
[RENDER]   vtermGrid[1]: "                                        "
```

### Key Code Paths

1. **Character placement with wrap** (`apps/texelterm/parser/vterm.go:166-181`):
   - If `wrapNext` is true, calls `lineFeedForWrap()`
   - `lineFeedForWrap` marks old line dirty before moving cursor

2. **Line feed internal** (`apps/texelterm/parser/vterm_scroll.go:23-25`):
   - `v.MarkDirty(v.cursorY)` called before cursor moves

3. **Display buffer SetCell** (`apps/texelterm/parser/display_buffer.go:597-611`):
   - Calls `rebuildCurrentLinePhysical()`
   - Calls `scrollToLiveEdge()` if at live edge

4. **Render function** (`apps/texelterm/term.go:283-357`):
   - Gets `vtermGrid` from `vterm.Grid()`
   - Gets dirty lines from `vterm.GetDirtyLines()`
   - Only renders dirty lines to `a.buf`

### Possible Areas to Investigate

1. **Devshell runner flow** (`internal/devshell/runner.go:159-160`):
   - `app.HandleKey(tev)` followed immediately by `draw()`
   - Draw happens BEFORE PTY echo arrives
   - Could the pre-echo draw be clearing something?

2. **Disk persistence path** (`apps/texelterm/term.go:1386-1397`):
   - When pane ID exists, uses `EnableDisplayBufferWithDisk`
   - When no pane ID (standalone), uses `EnableDisplayBuffer` (memory-only)

3. **Cursor position sync** (`apps/texelterm/parser/vterm_display_buffer.go:144-156`):
   - `syncCursorWithDisplayBuffer` positions cursor at live edge row
   - Called after loading history

4. **Logical vs physical X tracking** (`apps/texelterm/parser/vterm_display_buffer.go:347-354`):
   - `displayBufferSetCursorFromPhysical` assumes physical X = logical X
   - This is only true when cursor is on first physical line of wrapped logical line

## Test Commands

Run wrap tests:
```bash
go test -v ./apps/texelterm/parser/ -run "TestDisplayBuffer_Wrap\|TestDisplayBuffer_Devshell"
```

Run all parser tests:
```bash
go test ./apps/texelterm/parser/
```

Test with debug logging:
```bash
rm -f /tmp/texelterm-debug.log
TEXELTERM_DEBUG=1 ./bin/texelterm
# Type past line width, then check:
cat /tmp/texelterm-debug.log
```

## Commits This Session
- `48146d1` - Add debug logging and wrap tests for display buffer

## Session 2 Findings (Continued Investigation)

### All Wrap Tests Pass
Created and ran comprehensive tests:
- `TestDisplayBuffer_40ColumnWrap` - 40-column terminal with 45 chars (matches debug log scenario)
- `TestDisplayBuffer_40ColumnWrapWithPrompt` - With bash-like colored prompt
- `TestDisplayBuffer_BashReadlineWrap` - Simulates bash readline behavior
- `TestDisplayBuffer_CursorMovementOnWrappedLine` - Cursor movement on wrapped lines

**All tests pass.** The core wrapping logic works correctly in test environment.

### Found Related Bug: Cursor Movement on Wrapped Lines
When cursor moves via escape sequences (e.g., arrow keys) on a wrapped line, `displayBufferSetCursorFromPhysical()` incorrectly maps:

```
Example: Line has 15 chars on 10-col terminal (wrapped to 2 physical lines)
- Cursor at row 1, col 5 (physical) = logicalX should be 15
- After left arrow x3: cursorX=2, cursorY=1
- BUG: logicalX becomes 2 (should be 12 = 10 + 2)
```

This causes:
1. Characters typed after cursor movement appear at wrong logical position
2. Dirty tracking doesn't mark the correct physical row
3. Grid() returns correct content, but renderBuf misses updates

Test output showing the bug:
```
CurrentLine content: "ABXDEFGHIJKLMNO" (len=15)  <- X is at position 2
Grid directly:
  grid[0]: "ABXDEFGHIJ"  <- Grid() shows X correctly
  grid[1]: "KLMNO"
RenderBuf after simulateRender:
  Row 0: "ABCDEFGHIJ"  <- But renderBuf doesn't have X!
  Row 1: "KLMNO"
```

The issue is that `cursorY=1` is marked dirty, but the change affects `row 0` in the display.

### Original Issue Still Not Reproduced
The original issue (characters not appearing during wrap while typing) was NOT reproduced in tests. All typing-with-wrap tests pass correctly. This suggests the issue may be:
1. Specific to bash's escape sequences during line editing
2. Related to timing/async behavior not captured in tests
3. A different scenario than what we're testing

### Potential Fixes Needed

#### Fix 1: `displayBufferSetCursorFromPhysical()` (vterm_display_buffer.go:347-354)
Current (buggy):
```go
func (v *VTerm) displayBufferSetCursorFromPhysical() {
    v.displayBuf.currentLogicalX = v.cursorX  // Wrong on wrapped lines!
}
```

Needs to calculate logicalX based on which physical row of the wrapped line the cursor is on.

#### Fix 2: Dirty Tracking for Display Buffer Writes
When `displayBufferPlaceChar` writes at a logicalX that maps to a different physical row than cursorY, that row should be marked dirty.

## Session 3: ROOT CAUSE FOUND AND FIXED

### The Bug
When typing past line width and wrapping, characters weren't appearing on screen because:
1. `placeChar()` sets `cursorX = 0` BEFORE calling `lineFeedForWrap()`
2. `lineFeedForWrap()` → `lineFeedInternal(false)` → `SetCursorPos(cursorY+1, cursorX)` with cursorX=0
3. `SetCursorPos` calls `displayBufferSetCursorFromPhysical()`
4. `displayBufferSetCursorFromPhysical()` sets `currentLogicalX = cursorX = 0`
5. Next character is written at logical position 0 instead of continuing the line

This caused:
- Character written at wrong position (position 0 of logical line)
- That position maps to a different physical row than cursor
- Only cursor's row was marked dirty
- So the change was never rendered to screen

### The Fix
Modified `lineFeedInternal()` in `apps/texelterm/parser/vterm_scroll.go`:

**Before:**
```go
func (v *VTerm) lineFeedInternal(commitLogical bool) {
    // ... SetCursorPos called, which resets currentLogicalX via displayBufferSetCursorFromPhysical
}
```

**After:**
```go
func (v *VTerm) lineFeedInternal(commitLogical bool) {
    // For auto-wrap (!commitLogical), preserve currentLogicalX since the logical line continues
    var savedLogicalX int
    if !commitLogical && v.displayBuf != nil {
        savedLogicalX = v.displayBuf.currentLogicalX
    }

    // ... rest of function including SetCursorPos ...

    // Restore currentLogicalX for auto-wrap
    if !commitLogical && v.displayBuf != nil {
        v.displayBuf.currentLogicalX = savedLogicalX
    }
}
```

### Files Modified
- `apps/texelterm/parser/vterm_scroll.go:23-75` - Save/restore currentLogicalX for wrap
- `apps/texelterm/parser/vterm_display_buffer.go` - Added debug logging (can be removed later)

### Test Results
All tests pass including:
- `TestDisplayBuffer_WrapWithoutScrollDirty`
- `TestDisplayBuffer_FreshTerminalWrap`
- `TestDisplayBuffer_40ColumnWrap`
- `TestDisplayBuffer_40ColumnWrapWithPrompt`
- `TestDisplayBuffer_LineWrapWithHistory`
- All other wrap-related tests

### Next Steps
1. **Manual test** - Run `TEXELTERM_DEBUG=1 ./bin/texelterm` to verify the fix works
2. **Commit the fix** if manual testing passes
3. **Optional**: Remove debug logging from vterm_display_buffer.go if no longer needed
4. **Keep monitoring** for edge cases with cursor movement on wrapped lines (displayBufferSetCursorFromPhysical still has a known issue)

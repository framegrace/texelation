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

## Next Steps to Try
1. Reproduce the issue with TEXELTERM_DEBUG=1 and analyze the log
2. Check if issue only occurs with disk persistence or also memory-only
3. Add more granular logging in the render path to identify exactly where content is lost
4. Compare behavior between standalone texelterm and texelterm within texel-server

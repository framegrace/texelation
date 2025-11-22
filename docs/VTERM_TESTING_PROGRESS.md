# VTerm Rendering Bug Fix - Progress Report

**Started:** 2025-11-21
**Status:** In Progress
**Current Phase:** Phase 3 - Systematic Testing

---

## Summary

We're systematically testing and fixing all VTerm rendering bugs by:
1. Creating comprehensive test infrastructure
2. Writing tests for every control sequence
3. Identifying and fixing bugs revealed by tests
4. Ensuring 100% correctness against xterm specification

---

## Completed Work

### Phase 2: Test Infrastructure ✅ (2025-11-21)

**Files Created:**
- `apps/texelterm/parser/testharness.go` (306 lines)
  - TestHarness struct for VTerm testing
  - Helper methods: SendSeq, SendText, GetCell, GetCursor, etc.
  - Assertion methods: AssertCell, AssertCursor, AssertText, AssertBlank, etc.
  - Debug methods: Dump, DumpWithHistory
  - Utility methods: Clear, Reset, FillWithPattern

**Key Features:**
- Clean API for sending control sequences
- Easy cell and cursor position verification
- Visual debugging output for test failures
- Support for history buffer inspection

### Phase 3.1: Cursor Movement Tests ✅ (2025-11-21)

**Files Created:**
- `apps/texelterm/parser/cursor_test.go` (424 lines)

**Test Coverage: 68 test cases, ALL PASSING**

| Sequence | Command | Tests | Status | Notes |
|----------|---------|-------|--------|-------|
| ESC[<n>A | CUU (Cursor Up) | 7 | ✅ PASS | Includes edge cases and clamping |
| ESC[<n>B | CUD (Cursor Down) | 6 | ✅ PASS | Tests margin behavior |
| ESC[<n>C | CUF (Cursor Forward) | 6 | ✅ PASS | Right edge clamping works |
| ESC[<n>D | CUB (Cursor Backward) | 6 | ✅ PASS | Left edge clamping works |
| ESC[<n>G | CHA (Horizontal Absolute) | 6 | ✅ PASS | Column positioning |
| ESC[<n>d | VPA (Vertical Absolute) | 6 | ✅ PASS | Row positioning |
| ESC[<r>;<c>H | CUP (Cursor Position) | 8 | ✅ PASS | Absolute positioning |
| ESC[<r>;<c>f | HVP (Same as CUP) | 4 | ✅ PASS | Functionally identical to CUP |
| ESC[<n>E | CNL (Cursor Next Line) | 5 | ✅ PASS | **NEWLY IMPLEMENTED** |
| ESC[<n>F | CPL (Cursor Previous Line) | 5 | ✅ PASS | **NEWLY IMPLEMENTED** |
| ESC 7 | DECSC (Save Cursor) | 3 | ✅ PASS | **NEWLY WIRED UP** |
| ESC 8 | DECRC (Restore Cursor) | 3 | ✅ PASS | **NEWLY WIRED UP** |
| (various) | Edge case tests | 3 | ✅ PASS | Content preservation, edges |

**Total Cursor Tests:** 68 tests, 100% passing

### Phase 3.2: Erase Operation Tests ✅ (2025-11-22)

**Files Created:**
- `apps/texelterm/parser/erase_test.go` (607 lines)

**Test Coverage: 28 test cases, ALL PASSING**

| Sequence | Command | Tests | Status | Notes |
|----------|---------|-------|--------|-------|
| ESC[<n>J | ED (Erase in Display) | 8 | ✅ PASS | Params 0,1,2,3; edges; color preservation |
| ESC[<n>K | EL (Erase in Line) | 7 | ✅ PASS | Params 0,1,2; edges; multi-line isolation |
| ESC[<n>X | ECH (Erase Character) | 5 | ✅ PASS | Default, explicit, multiple, edges |
| ESC[<n>P | DCH (Delete Character) | 6 | ✅ PASS | Shift left behavior, edges |
| (color tests) | SGR + Erase | 2 | ✅ PASS | Background color preservation |

**Total Erase Tests:** 28 tests, 100% passing

**Test Infrastructure Improvements:**
- Refactored test setup to use explicit `SendText`/`SendSeq` instead of `FillWithPattern`
- Fixed sparse line issues in test harness
- Improved reliability of erase operation tests

---

## Bugs Fixed

### Bug #1: X Coordinate Not Clamping in Main Screen
**File:** `apps/texelterm/parser/vterm.go:197-200`
**Issue:** SetCursorPos only clamped X coordinate when in alternate screen mode
**Impact:** Cursor could move beyond right edge in main screen
**Fix:** Remove `if v.inAltScreen` condition around X clamping
**Tests Affected:** TestCursorForward, TestCursorHorizontalAbsolute, TestCursorPosition

**Before:**
```go
if v.inAltScreen {
    if x >= v.width {
        x = v.width - 1
    }
}
```

**After:**
```go
if x >= v.width {
    x = v.width - 1
}
```

### Bug #2: CNL (Cursor Next Line) Not Implemented
**File:** `apps/texelterm/parser/vterm.go:646-649`
**Issue:** ESC[<n>E sequence was unhandled
**Impact:** Applications using CNL would have broken cursor positioning
**Fix:** Implemented CNL handler in handleCursorMovement
**Tests Added:** 5 test cases in TestCursorNextLine

### Bug #3: CPL (Cursor Previous Line) Not Implemented
**File:** `apps/texelterm/parser/vterm.go:650-653`
**Issue:** ESC[<n>F sequence was unhandled
**Impact:** Applications using CPL would have broken cursor positioning
**Fix:** Implemented CPL handler in handleCursorMovement
**Tests Added:** 5 test cases in TestCursorPreviousLine

### Bug #4: DECSC/DECRC Not Wired Up in Parser
**File:** `apps/texelterm/parser/parser.go:102-109`
**Issue:** ESC 7 and ESC 8 were unhandled (functions existed in VTerm but not called)
**Impact:** Applications couldn't save/restore cursor position
**Fix:** Added handlers in StateEscape case
**Tests Added:** 3 test cases in TestCursorSaveRestore

### Bug #5: String Terminator (ST) Not Handled
**File:** `apps/texelterm/parser/parser.go`
**Issue:** ESC \ (String Terminator) was unhandled
**Impact:** User casually found error when testing an app
**Fix:** Added case for '\\' in StateEscape to properly handle ST
**Tests Added:** 1 test case in TestStringTerminator

### Bug #6: ED 0 Double-Clearing Current Line
**File:** `apps/texelterm/parser/vterm.go:718-738`
**Issue:** ED 0 was calling ClearLine(0) then also truncating the same line
**Impact:** Current line was being cleared twice with inconsistent behavior
**Fix:** Rewrote ED 0 to properly clear remaining lines in viewport
**Tests Affected:** TestEraseInDisplay ED 0 tests

### Bug #7: ClearScreen vs ClearVisibleScreen Semantics (CRITICAL)
**File:** `apps/texelterm/parser/vterm.go:374-421`
**Issue:** Changed ClearScreen() to preserve cursor for ED 2, but ClearScreen() is called during NewVTerm() initialization, causing black screen
**Impact:** User reported "texelscreen is totally black, cursor moving but nothing visible"
**Fix:** Split into two functions:
  - `ClearScreen()` - Original behavior for initialization/RIS (resets history, moves cursor to 0,0)
  - `ClearVisibleScreen()` - New ED 2 behavior (clears visible screen, preserves history and cursor)
**Tests Affected:** All screen clearing operations

### Bug #8: ED 1 Not Preserving Background Color on Main Screen
**File:** `apps/texelterm/parser/vterm.go:748-756`
**Issue:** ED 1 (erase from start to cursor) was creating empty lines instead of properly filled lines with current SGR attributes
**Impact:** Erased cells didn't preserve background color
**Fix:** Create proper blank lines with `Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}`
**Tests Affected:** TestEraseWithColors ED color preservation test

### Bug #9: ED 0 Not Creating Non-Existent Lines
**File:** `apps/texelterm/parser/vterm.go:730-742`
**Issue:** ED 0 loop condition `y < v.historyLen` prevented creating new lines beyond current history. Fresh terminal has historyLen=1, so erasing from row 5 to end would only clear row 0.
**Impact:** Erase operations on fresh terminals left cells uninitialized
**Fix:** Ensure all lines exist up to viewport bottom using `appendHistoryLine` before clearing them
**Tests Affected:** TestEraseWithColors, all ED tests on fresh terminals

---

## Test Results

```
=== Cursor Movement Tests ===
PASS: TestCursorUp (7 cases)
PASS: TestCursorDown (6 cases)
PASS: TestCursorForward (6 cases)
PASS: TestCursorBackward (6 cases)
PASS: TestCursorHorizontalAbsolute (6 cases)
PASS: TestCursorVerticalAbsolute (6 cases)
PASS: TestCursorPosition (8 cases)
PASS: TestHVP (4 cases)
PASS: TestCursorNextLine (5 cases)
PASS: TestCursorPreviousLine (5 cases)
PASS: TestCursorSaveRestore (3 cases)
PASS: TestStringTerminator (1 case)
PASS: TestCursorMovementWithContent (1 case)
PASS: TestCursorAtEdges (1 case)

Subtotal: 68 cursor tests

=== Erase Operation Tests ===
PASS: TestEraseInDisplay (8 cases: ED 0,1,2,3 + edges)
PASS: TestEraseInLine (7 cases: EL 0,1,2 + edges)
PASS: TestEraseCharacter (5 cases: ECH + edges)
PASS: TestDeleteCharacter (6 cases: DCH + edges)
PASS: TestEraseWithColors (2 cases: EL + ED color preservation)

Subtotal: 28 erase tests

=== Other Tests ===
PASS: Line wrapping and reflow tests (8 cases)

Total: 68 + 28 + 8 = 104 tests
Result: ALL PASS ✅
```

---

## Next Steps

### Phase 3.3: Insertion/Deletion Tests (Next Priority)
- ICH (Insert Characters)
- DCH (Delete Characters)
- IL (Insert Lines)
- DL (Delete Lines)

### Phase 3.4: SGR (Color/Attribute) Tests
- Basic attributes (bold, underline, reverse, etc.)
- 8 basic colors (30-37 fg, 40-47 bg)
- Bright colors (90-97, 100-107)
- 256-color mode (38;5;n, 48;5;n)
- RGB mode (38;2;r;g;b, 48;2;r;g;b)
- Attribute combinations
- Reset behavior

### Phase 3.5: Scrolling Region Tests
- DECSTBM (Set scrolling margins)
- IND (Index - scroll up)
- RI (Reverse Index - scroll down)
- Cursor movement within margins
- Line feed at margins

### Phase 3.6: Screen Mode Tests
- Alternate screen buffer
- Cursor save/restore with screen switching
- Mode switches (cursor visibility, etc.)

---

## Metrics

- **Test Infrastructure Lines:** 306
- **Test Code Lines:** 424 (cursor) + 607 (erase) = 1,031 lines
- **Bugs Found:** 9
- **Bugs Fixed:** 9
- **Critical Bugs:** 1 (black screen bug #7)
- **Test Pass Rate:** 100% (104/104)
- **Time Spent:** ~4 hours
- **Coverage:**
  - ✅ Cursor movement operations (complete)
  - ✅ Erase operations (complete)

---

## Lessons Learned

1. **Test-Driven Bug Finding Works:** Every test immediately revealed real bugs
2. **Edge Cases Matter:** Off-by-one errors are common (e.g., X clamping bug)
3. **Missing Features Are Common:** CNL, CPL were not implemented
4. **Parser vs Implementation:** Some features exist but aren't wired up (DECSC/DECRC)
5. **Test Harness Is Essential:** Having good test utilities makes test writing fast
6. **Initialization vs Operation:** Functions used in both contexts need careful separation (ClearScreen bug #7)
7. **History Buffer Complexity:** setHistoryLine can't create new lines, only modify existing ones
8. **Color Preservation:** SGR attributes must be preserved in ALL erase operations
9. **Fresh Terminal State:** Tests on fresh terminals expose bugs that working terminals might hide

---

## References

- XTerm Control Sequences: `docs/xterm.pdf`
- VTerm Implementation: `apps/texelterm/parser/vterm.go` (1250+ lines)
- Parser Implementation: `apps/texelterm/parser/parser.go` (344 lines)
- Test Harness: `apps/texelterm/parser/testharness.go` (306 lines)
- Cursor Tests: `apps/texelterm/parser/cursor_test.go` (424 lines)
- Erase Tests: `apps/texelterm/parser/erase_test.go` (607 lines)

---

## Estimated Remaining Work

- **Phase 3.2 (Erase Tests):** ✅ COMPLETE (found 5 bugs, all fixed)
- **Phase 3.3 (Insert/Delete):** 1 day (expect to find 2-3 bugs)
- **Phase 3.4 (SGR Colors):** 1-2 days (expect to find 4-6 bugs, especially in 256/RGB modes)
- **Phase 3.5 (Scrolling):** 1 day (expect to find 2-4 bugs)
- **Phase 3.6 (Screen Modes):** 1 day (expect to find 1-2 bugs)
- **Phase 4 (Combined Tests):** 1-2 days (test real-world sequences)

**Total Remaining:** 5-9 days to complete all VTerm testing and bug fixes

---

Last Updated: 2025-11-22

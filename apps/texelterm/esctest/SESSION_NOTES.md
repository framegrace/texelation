# esctest Conversion - Session Notes

**Last Updated**: 2025-11-27
**Current Branch**: texelterm-bug-fixing
**Latest Commit**: [To be determined] (Batch 10)

## Current Status

**Total Tests**: 164
**Passing**: 164 (100%) ✓
**Failing**: 0

### Completed Batches

- **Batch 1-3**: Basic cursor movement, save/restore (58 tests) - ALL PASSING
- **Batch 4**: Character editing - DCH, ECH, REP (14 tests) - ALL PASSING
- **Batch 5**: Line editing - DL, IL (16 tests) - ALL PASSING
- **Batch 6**: Erase operations - ED, EL (13 tests) - ALL PASSING
- **Batch 7**: Scrolling - DECSTBM, IND, RI (22 tests) - ALL PASSING
- **Batch 8**: Scroll commands - SU, SD (18 tests) - ALL PASSING
- **Batch 9**: Tab operations - HTS, TBC (5 tests) - ALL PASSING
- **Batch 10**: Additional cursor movement - HPA, HPR, VPR, CBT, CHT (19 tests) - ALL PASSING

## Latest Changes (This Session)

### Batch 6: Erase Operations (Commit: fbe57e3)

**Files Created:**
- `apps/texelterm/esctest/ed_test.go` - 8 ED (Erase in Display) tests
- `apps/texelterm/esctest/el_test.go` - 5 EL (Erase in Line) tests

**Fixes Applied:**
- Fixed ED(3) to only clear scrollback, not visible screen (vterm.go:873-896)

**All 13 tests passing**

### Batch 7: Scrolling and Regions (Commit: 53866e5)

**Files Created:**
- `apps/texelterm/esctest/decstbm_test.go` - 10 DECSTBM tests (9 passing)
- `apps/texelterm/esctest/ind_test.go` - 6 IND tests (all passing)
- `apps/texelterm/esctest/ri_test.go` - 6 RI tests (3 passing)

**Implementations:**
1. **IND (Index) - ESC D** (parser.go:106-108, vterm.go:663-677)
   - Moves cursor down one line
   - Scrolls region up if at bottom margin
   - Respects left/right margins

2. **DECSTBM Cursor Movement** (vterm.go:1534-1535)
   - Now correctly moves cursor to origin (1,1) when setting margins

3. **IND/RI Left/Right Margin Handling** (vterm.go:666-677, 681-692)
   - Won't scroll when cursor outside left/right margins
   - Stays at margin boundary when outside margins

**Helper Functions Added** (helpers.go):
```go
func IND(d *Driver)  // ESC D - Index
func RI(d *Driver)   // ESC M - Reverse Index (already existed)
```

**18/22 tests passing**

### Scroll-Down Bug Fixes (Commit: 332533a)

**Fixed Issues:**
- All 4 failing Batch 7 tests now passing
- Test suite at 100% pass rate (119/119)

**Root Causes Identified:**
1. **Scroll-down content loss**: `scrollRegion()` attempted to write to history indices that didn't exist yet. The `setHistoryLine()` bounds check (`index >= historyLen`) caused writes to silently fail, losing content during reverse index operations.

2. **Unwanted viewport shifts**: `LineFeed()` was appending history lines even when cursor was at the bottom of the physical screen (outside scroll region). This caused `historyLen` to grow and shift `getTopHistoryLine()`, inadvertently scrolling the viewport.

**Fixes Applied** (vterm.go):
1. Lines 326-330: Ensure history buffer has all required lines before scroll-down:
   ```go
   // Ensure history buffer has all lines we'll be writing to
   endLogicalY := topHistory + bottom
   for v.historyLen <= endLogicalY {
       v.appendHistoryLine(make([]Cell, 0, v.width))
   }
   ```

2. Lines 272-283: Only append history lines when cursor will actually move:
   ```go
   } else if v.cursorY < v.height-1 {
       // Only append history lines when cursor will actually move down
       logicalY := v.cursorY + v.getTopHistoryLine()
       if logicalY+1 >= v.historyLen {
           v.appendHistoryLine(make([]Cell, 0, v.width))
       }
       v.SetCursorPos(v.cursorY+1, v.cursorX)
   } else {
       // At bottom of screen but not at scroll region bottom: stay put
       v.viewOffset = 0
       v.MarkAllDirty()
   }
   ```

**Tests Fixed:**
- `Test_RI_Scrolls` - RI scroll-down on main screen
- `Test_RI_ScrollsInTopBottomRegionStartingBelow` - RI with scroll region from below
- `Test_RI_ScrollsInTopBottomRegionStartingWithin` - RI within scroll region
- `Test_DECSTBM_CursorBelowRegionAtBottomTriesToScroll` - Scrolling outside margins

### Batch 8: SU/SD Scroll Commands (Commit: b8482e0)

**Files Created:**
- `apps/texelterm/esctest/su_test.go` - 9 SU (Scroll Up) tests
- `apps/texelterm/esctest/sd_test.go` - 9 SD (Scroll Down) tests

**Implementations:**
1. **SU (Scroll Up) - CSI Ps S** (vterm.go:739-744, helpers.go:241-248)
   - Scrolls content up within margins
   - Respects both top/bottom and left/right margins
   - Operates on entire region (not just from cursor)

2. **SD (Scroll Down) - CSI Ps T** (vterm.go:745-750, helpers.go:250-257)
   - Scrolls content down within margins
   - Respects both top/bottom and left/right margins
   - Operates on entire region (not just from cursor)

3. **scrollUpWithinMargins()** (vterm.go:346-424)
   - New function for SU when DECLRMM is active
   - Scrolls content up only within left/right margins
   - Preserves content outside margin columns
   - Similar to deleteLinesWithinMargins but operates on entire top/bottom region

4. **scrollDownWithinMargins()** (vterm.go:426-504)
   - New function for SD when DECLRMM is active
   - Scrolls content down only within left/right margins
   - Preserves content outside margin columns
   - Similar to insertLinesWithinMargins but operates on entire top/bottom region

**Key Differences from IND/RI:**
- IND/RI check cursor position and don't scroll if outside left/right margins
- SU/SD always scroll the region, but only the columns within margins when DECLRMM active
- SU/SD preserve content outside margin columns (rectangular scrolling)
- IND/RI shift entire lines (full-line scrolling)

**All 18 tests passing**

### Batch 9: Tab Operations (Commit: fa31d66)

**Files Created:**
- `apps/texelterm/esctest/hts_test.go` - 1 HTS (Horizontal Tab Set) test
- `apps/texelterm/esctest/tbc_test.go` - 4 TBC (Tab Clear) tests

**Implementations:**
1. **HTS (Horizontal Tab Set) - ESC H** (parser.go:112-115, vterm.go:806-809)
   - Sets a tab stop at the current cursor column
   - Simple implementation using existing tabStops map

2. **TBC (Tab Clear) - CSI Ps g** (vterm.go:957-958, 811-823)
   - Mode 0 or default: clear tab stop at cursor position
   - Mode 3: clear all tab stops
   - Uses delete() to remove specific tab or recreates map for "clear all"

**Helper Functions Added** (helpers.go:259-273):
```go
func HTS(d *Driver)           // ESC H - Set tab at cursor
func TBC(d *Driver, n ...int) // CSI g - Clear tabs
```

**Tab Infrastructure:**
- Tab stops already existed every 8 columns (0, 8, 16, 24...)
- Tab() function already implemented and working
- HTS/TBC complete the tab stop management

**All 5 tests passing**

### Batch 10: Additional Cursor Movement (Commit: TBD)

**Files Created:**
- `apps/texelterm/esctest/hpa_test.go` - 4 HPA (Horizontal Position Absolute) tests
- `apps/texelterm/esctest/hpr_test.go` - 4 HPR (Horizontal Position Relative) tests
- `apps/texelterm/esctest/vpr_test.go` - 4 VPR (Vertical Position Relative) tests
- `apps/texelterm/esctest/cbt_test.go` - 4 CBT (Cursor Backward Tab) tests
- `apps/texelterm/esctest/cht_test.go` - 3 CHT (Cursor Horizontal Tab) tests

**Implementations:**
1. **HPA (Horizontal Position Absolute) - CSI Ps `** (vterm.go:1070-1076)
   - Moves cursor to absolute column position
   - Respects origin mode (like CHA/VPA)
   - Clamps to screen boundaries

2. **HPR (Horizontal Position Relative) - CSI Ps a** (vterm.go:1077-1084)
   - Moves cursor right by n columns
   - Clamps to right edge
   - Relative movement, not absolute positioning

3. **VPR (Vertical Position Relative) - CSI Ps e** (vterm.go:1085-1092)
   - Moves cursor down by n rows
   - Clamps to bottom edge
   - Relative movement, not absolute positioning

4. **CBT (Cursor Backward Tab) - CSI Ps Z** (vterm.go:834-855, parser.go:912-913)
   - Moves cursor backward n tab stops
   - Ignores left/right margins (can reach column 1)
   - Stops at left edge if no more tab stops

5. **CHT (Cursor Horizontal Tab) - CSI Ps I** (vterm.go:806-832, parser.go:910-911)
   - Moves cursor forward n tab stops
   - Respects right margin when DECLRMM is active
   - Stops at right edge/margin if no more tab stops

**Helper Functions Added** (helpers.go:275-318):
```go
func HPA(d *Driver, n ...int)  // CSI ` - Horizontal Position Absolute
func HPR(d *Driver, n ...int)  // CSI a - Horizontal Position Relative
func VPR(d *Driver, n ...int)  // CSI e - Vertical Position Relative
func CBT(d *Driver, n ...int)  // CSI Z - Cursor Backward Tab
func CHT(d *Driver, n ...int)  // CSI I - Cursor Horizontal Tab
```

**Key Behaviors:**
- HPA/HPR/VPR respect origin mode for consistency with CHA/VPA
- CHT respects right margin (DEC terminal behavior)
- CBT ignores margins (ECMA-48 behavior)
- All commands clamp to screen/margin boundaries

**All 19 tests passing**

## Test Conversion Process

### Source
- Original: esctest2 Python tests (https://github.com/ThomasDickey/esctest2)
- Location: `/home/marc/projects/tde/esctest2/esctest/tests/`
- License: GPL v2
- Authors: George Nachman, Thomas E. Dickey

### Conversion Pattern

1. Read Python test file from esctest2
2. Create Go test file in `apps/texelterm/esctest/`
3. Convert test logic preserving intent
4. Add helper functions to `helpers.go` if needed
5. Run tests and fix failures
6. Update README.md with results
7. Commit batch

### Test File Structure

Each test file includes:
```go
// Package esctest provides a Go-native test framework...
// Original esctest2 source:
//   - Project: https://github.com/ThomasDickey/esctest2
//   - File: esctest/tests/[name].py
//   - Authors: George Nachman, Thomas E. Dickey
//   - License: GPL v2
package esctest

import "testing"

func Test_[Name]_[Description](t *testing.T) {
    d := NewDriver(80, 24)
    // Test implementation
}
```

### Key Testing Patterns

**Cursor Positioning (1-indexed):**
```go
CUP(d, NewPoint(x, y))  // Move to column x, row y
```

**Assertions:**
```go
AssertEQ(t, actual, expected)
AssertScreenCharsInRectEqual(t, d, NewRect(left, top, right, bottom), []string{"line1", "line2"})
```

**Margins:**
```go
DECSTBM(d, top, bottom)      // Set top/bottom margins
DECSLRM(d, left, right)      // Set left/right margins (requires DECLRMM)
DECSET(d, DECLRMM)           // Enable left/right margin mode
DECRESET(d, DECLRMM)         // Disable left/right margin mode
```

## Next Steps

### Potential Next Batches

With all scrolling infrastructure complete and working (IND, RI, SU, SD, DECSTBM), the foundation is solid for continuing conversions. Potential next batches could include:

- Batch 9+: Additional escape sequences from esctest2 test suite
- Focus on sequences that build on the working scroll/margin infrastructure
- Continue improving xterm compliance incrementally

## Running Tests

```bash
# All esctest tests
go test texelation/apps/texelterm/esctest -v

# Specific batch
go test texelation/apps/texelterm/esctest -v -run "Test_ED|Test_EL"

# Single test
go test texelation/apps/texelterm/esctest -v -run "Test_RI_Scrolls$"

# See summary
go test texelation/apps/texelterm/esctest | tail -20
```

## Important Files

**Test Framework:**
- `apps/texelterm/esctest/driver.go` - Headless terminal driver
- `apps/texelterm/esctest/types.go` - Point, Rect, Size types
- `apps/texelterm/esctest/helpers.go` - Escape sequence helpers and assertions

**Implementation:**
- `apps/texelterm/parser/vterm.go` - VTerm core (1500+ lines)
- `apps/texelterm/parser/parser.go` - Escape sequence parser

**Documentation:**
- `apps/texelterm/esctest/README.md` - Progress tracking, test results, fixed issues
- `apps/texelterm/esctest/SESSION_NOTES.md` - This file

## Key Implementation Details

### Margin Handling

**Top/Bottom Margins (DECSTBM):**
- Stored as 0-indexed: `marginTop`, `marginBottom`
- Default: 0 to height-1 (full screen)
- Commands: CUP, scrolling, line editing respect these

**Left/Right Margins (DECSLRM):**
- Requires `DECLRMM` mode enabled (CSI ? 69 h)
- Stored as 0-indexed: `marginLeft`, `marginRight`
- Default: 0 to width-1 (full screen)
- Commands: ICH, DCH, DL, IL, REP, IND, RI respect these

### Scrolling Logic

**Alt Screen** (simpler):
- Direct buffer manipulation
- No history
- Scroll up: copy buffer[top+1:bottom+1] to buffer[top:bottom], clear bottom
- Scroll down: copy buffer[top:bottom] to buffer[top+1:bottom+1], clear top

**Main Screen** (complex):
- Uses circular history buffer
- Scroll up: move lines up, push top to history, clear bottom
- Scroll down: move lines down, insert blank at top (ISSUE HERE)

### Common Gotchas

1. **Coordinates are 1-indexed in tests, 0-indexed internally**
2. **DECSTBM moves cursor to origin** - must call SetCursorPos(0, 0)
3. **Margin checks**: Commands outside margins behave differently
4. **History buffer**: Main screen requires careful line existence checks
5. **Driver methods**: Use `d.GetCursorPosition().X` not `d.GetCursorX()`

## Recent Git History

```
53866e5 Add Batch 7 esctest: Scrolling and regions (18/22 passing)
fbe57e3 Add Batch 6 esctest: ED and EL erase operations (13 tests)
1dd4618 Add Batch 5 esctest: DL and IL line editing (16 tests)
cf85eb1 Add Batch 4 esctest: Character editing (14 tests)
```

## Session Context for Next Time

When resuming:

1. **Check if RI failures should be fixed first** or continue with new batches
2. **Consider using Task tool** for complex debugging of RI scroll-down
3. **SU/SD tests are natural next step** if moving forward
4. **All test infrastructure is in place** - just need conversions and fixes

The conversion process is well-established and efficient. Each batch takes about 30-45 minutes including fixes.

# esctest - Terminal Emulation Compliance Tests

This package provides a Go-native test framework for validating terminal emulator compliance with xterm standards.

## Origin

This test suite is derived from **esctest2** by George Nachman and Thomas E. Dickey:
- **Project**: https://github.com/ThomasDickey/esctest2
- **Authors**: George Nachman, Thomas E. Dickey
- **License**: GPL v2

The original Python-based tests have been converted to Go to enable:
- Offline, deterministic testing without Python dependencies
- Direct integration with the texelterm parser without PTY overhead
- Fast execution as part of the standard Go test suite

## Current Status

### Framework Components

- ✅ **types.go** - Point, Rect, Size types for positioning and regions
- ✅ **driver.go** - Headless terminal driver for sending sequences and querying state
- ✅ **helpers.go** - Assertion functions and escape sequence commands

### Converted Tests

**Batch 1: Basic Cursor Movement**
- ✅ **cuu_test.go** - CUU (Cursor Up) - 5 tests, all passing
- ✅ **cud_test.go** - CUD (Cursor Down) - 5 tests, all passing
- ✅ **cuf_test.go** - CUF (Cursor Forward) - 5 tests, all passing
- ✅ **cub_test.go** - CUB (Cursor Backward) - 5 tests, all passing
- ✅ **cha_test.go** - CHA (Cursor Horizontal Absolute) - 6 tests, all passing
- ✅ **ich_test.go** - ICH (Insert Character) - 6 tests, all passing

**Batch 2: Advanced Cursor Movement**
- ✅ **vpa_test.go** - VPA (Vertical Position Absolute) - 4 tests, all passing
- ✅ **hvp_test.go** - HVP (Horizontal and Vertical Position) - 3 tests, all passing
- ✅ **cup_test.go** - CUP (Cursor Position) - 4 tests, all passing
- ✅ **cnl_test.go** - CNL (Cursor Next Line) - 5 tests, all passing
- ✅ **cpl_test.go** - CPL (Cursor Previous Line) - 5 tests, all passing

**Batch 3: Save/Restore Cursor**
- ✅ **save_restore_cursor_test.go** - DECSC/DECRC (Save/Restore Cursor) - 5 tests, all passing

**Batch 4: Character Editing**
- ✅ **dch_test.go** - DCH (Delete Character) - 6 tests, all passing
- ✅ **ech_test.go** - ECH (Erase Character) - 4 tests, all passing
- ✅ **rep_test.go** - REP (Repeat) - 4 tests, all passing

**Batch 5: Line Editing**
- ✅ **dl_test.go** - DL (Delete Line) - 10 tests, all passing
- ✅ **il_test.go** - IL (Insert Line) - 6 tests, all passing

**Batch 6: Erase Operations**
- ✅ **ed_test.go** - ED (Erase in Display) - 8 tests, all passing
- ✅ **el_test.go** - EL (Erase in Line) - 5 tests, all passing

**Batch 7: Scrolling and Regions**
- ✅ **decstbm_test.go** - DECSTBM (Set Top/Bottom Margins) - 10 tests, all passing
- ✅ **ind_test.go** - IND (Index) - 6 tests, all passing
- ✅ **ri_test.go** - RI (Reverse Index) - 6 tests, all passing

**Batch 8: Scroll Commands**
- ✅ **su_test.go** - SU (Scroll Up) - 9 tests, all passing
- ✅ **sd_test.go** - SD (Scroll Down) - 9 tests, all passing

**Batch 9: Tab Operations**
- ✅ **hts_test.go** - HTS (Horizontal Tab Set) - 1 test, all passing
- ✅ **tbc_test.go** - TBC (Tab Clear) - 4 tests, all passing

**Batch 10: Additional Cursor Movement**
- ✅ **hpa_test.go** - HPA (Horizontal Position Absolute) - 4 tests, all passing
- ✅ **hpr_test.go** - HPR (Horizontal Position Relative) - 4 tests, all passing
- ✅ **vpr_test.go** - VPR (Vertical Position Relative) - 4 tests, all passing
- ✅ **cbt_test.go** - CBT (Cursor Backward Tab) - 4 tests, all passing
- ✅ **cht_test.go** - CHT (Cursor Horizontal Tab) - 3 tests, all passing

### Test Results Summary

**Total**: 164 tests
**Passing**: 164 (100%) ✅
**Failing**: 0

All compliance tests passing! The following issues were fixed:

#### Fixed Issues

1. **Scroll Region Boundary Bug** (CUU)
   - Fixed cursor movement to properly distinguish between inside/outside scroll regions
   - Cursor now correctly moves to top of screen when starting above scroll region
   - See vterm.go:1112 (MoveCursorUp) and vterm.go:1131 (MoveCursorDown)

2. **DECLRMM Support** (Left/Right Margin Mode)
   - Implemented CSI ? 69 h/l (enable/disable left/right margin mode)
   - Implemented DECSLRM (CSI Ps ; Ps s) to set left/right margins
   - ICH now respects left/right margins when DECLRMM is active
   - See vterm.go:334, 371, 668, 1123, 887

3. **Horizontal Cursor Movement Margin Support** (CUF, CUB)
   - CUF (Cursor Forward) now respects left/right margins when DECLRMM is active
   - CUB (Cursor Backward) now respects left/right margins when DECLRMM is active
   - When cursor is inside margin region, movement stops at margin boundaries
   - When cursor is outside margins, movement stops at screen boundaries
   - See vterm.go:1194-1228

4. **DECOM (Origin Mode) Implementation**
   - Implemented CSI ? 6 h/l (enable/disable origin mode)
   - When enabled, cursor positioning (CUP, CHA, VPA) is relative to scroll region
   - Cursor position reporting adjusted to return relative coordinates in origin mode
   - Entering/exiting origin mode moves cursor to home position of region/screen
   - See vterm.go:328-331, 369-372, 726-748, driver.go:53-67, 1509-1512

5. **DECRC Origin Mode Reset** (DECSC/DECRC)
   - DECRC (Restore Cursor) now resets origin mode to off, per xterm behavior
   - Ensures cursor positioning returns to absolute screen coordinates after restore
   - See vterm.go:467-478

6. **DCH Left/Right Margin Support** (DCH)
   - DeleteCharacters now respects left/right margins when DECLRMM is active
   - Cursor outside margins: DCH does nothing
   - Cursor inside margins: DCH only deletes within margin boundaries
   - Similar pattern to ICH margin handling
   - See vterm.go:1004-1086

7. **REP (Repeat Character) Implementation** (REP)
   - Implemented CSI b (REP - Repeat previous graphic character)
   - Tracks last graphic character written in placeChar
   - REP respects both left/right and top/bottom margins via placeChar
   - See vterm.go:54 (lastGraphicChar field), 136 (tracking), 773 (handler), 1088-1099 (implementation)

8. **Margin-Aware Character Wrapping** (REP, general)
   - placeChar now wraps at right margin edge when DECLRMM active
   - Wrapping returns cursor to left margin instead of column 0
   - Fixes REP behavior with left/right margins
   - See vterm.go:138-148 (wrap to left margin), 180-209 (margin-aware wrapping)

9. **Main Screen Scrolling Within Margins** (LineFeed, scrollRegion)
   - LineFeed now checks for bottom margin on main screen and scrolls region
   - scrollRegion now works for both alt and main screens
   - Fixes scrolling behavior for commands that use margins (REP, etc.)
   - See vterm.go:259-285 (LineFeed), 287-339 (scrollRegion)

10. **DL/IL Left/Right Margin Support** (DL, IL)
    - DeleteLines and InsertLines now respect left/right margins when DECLRMM is active
    - Cursor outside margins: DL/IL does nothing
    - Cursor inside margins: DL/IL only operates within margin columns
    - Similar pattern to DCH margin handling
    - See vterm.go:1290-1417 (DeleteLines), 1144-1247 (InsertLines)

11. **DL/IL Scroll Region Compliance** (DL, IL)
    - Rewrote DeleteLines and InsertLines to only affect lines within scroll region
    - Lines outside top/bottom margins are now preserved correctly
    - Fixed main screen handling to work like alt screen for IL/DL
    - Prevents lines from being deleted from or inserted into history incorrectly
    - See vterm.go:1314-1347 (deleteFullLines), 1167-1205 (insertFullLines)

12. **DL/IL Margin Region Boundary Clamping** (DL, IL)
    - Fixed clearing loop in deleteLinesWithinMargins to clamp start position
    - Prevents negative indices when n > region height
    - Ensures only lines from cursor to marginBottom are affected
    - See vterm.go:1364-1367, 1401-1404

13. **ED 3 Scrollback-Only Clear** (ED)
    - Fixed ED(3) to only clear scrollback history, leaving visible screen intact
    - Previously incorrectly cleared both screen and scrollback
    - Now preserves visible screen content and only removes history above it
    - On alt screen (no scrollback), ED 3 correctly does nothing
    - See vterm.go:873-896

14. **IND (Index) Implementation** (IND)
    - Implemented ESC D (Index) command for cursor down with scroll
    - Moves cursor down one line, scrolls region up if at bottom margin
    - Respects left/right margins - won't scroll if cursor outside margins
    - Stops at bottom margin when outside left/right margins
    - See parser.go:106-108, vterm.go:663-677

15. **DECSTBM Cursor to Origin** (DECSTBM)
    - Fixed DECSTBM to move cursor to home position (1,1) per spec
    - Previously left cursor at current position
    - Ensures consistent behavior when setting scroll regions
    - See vterm.go:1534-1535

16. **IND/RI Left/Right Margin Handling** (IND, RI)
    - Fixed Index and ReverseIndex to respect left/right margins
    - When cursor outside margins: won't scroll region, but respects top/bottom limits
    - When at margin boundary outside left/right: stays at boundary
    - Prevents unwanted scrolling when cursor at edge of screen
    - See vterm.go:666-677, 681-692

17. **Scroll-Down Content Preservation** (RI)
    - Fixed scrollRegion to ensure history buffer has all required lines before scroll-down
    - Previously setHistoryLine() bounds check caused writes to silently fail
    - Content was lost during reverse index operations
    - Now creates missing lines before shifting content
    - See vterm.go:326-330

18. **LineFeed Viewport Shift Fix** (RI)
    - Fixed LineFeed to not append history lines when cursor at bottom of screen
    - Previously caused unwanted viewport shifts outside scroll regions
    - Now only appends lines when cursor will actually move down
    - Prevents historyLen growth that shifts getTopHistoryLine() incorrectly
    - See vterm.go:272-283

19. **SU/SD Left/Right Margin Support** (SU, SD)
    - Implemented scrollUpWithinMargins and scrollDownWithinMargins functions
    - SU/SD now respect both top/bottom and left/right margins
    - Content outside margins is preserved during scroll operations
    - Matches xterm behavior for rectangular region scrolling
    - See vterm.go:346-504, 740-750

20. **HTS/TBC Tab Operations** (HTS, TBC)
    - Implemented HTS (ESC H) to set tab stop at cursor column
    - Implemented TBC (CSI g) to clear tab stops
    - TBC mode 0: clear tab at cursor
    - TBC mode 3: clear all tabs
    - Tab stops already existed every 8 columns by default
    - See parser.go:112-115, vterm.go:806-823, 957-958

21. **Additional Cursor Movement Commands** (HPA, HPR, VPR, CBT, CHT)
    - Implemented HPA (CSI `) - Horizontal Position Absolute
    - Implemented HPR (CSI a) - Horizontal Position Relative (move right by n)
    - Implemented VPR (CSI e) - Vertical Position Relative (move down by n)
    - Implemented CBT (CSI Z) - Cursor Backward Tab (n tab stops back)
    - Implemented CHT (CSI I) - Cursor Horizontal Tab (n tab stops forward)
    - HPA and HPR respect origin mode like CHA
    - VPR respects origin mode like VPA
    - CHT respects right margin when DECLRMM is active
    - CBT ignores left/right margins (can tab to column 1)
    - See vterm.go:908-913, 1070-1091, 795-855

## Test Conversion Plan

### Priority 1: Basic Cursor Movement (Core Functionality)

These sequences are heavily used and critical for terminal operation:

- [ ] **cud_test.go** - CUD (Cursor Down)
- [ ] **cuf_test.go** - CUF (Cursor Forward)
- [ ] **cub_test.go** - CUB (Cursor Backward)
- [ ] **cha_test.go** - CHA (Cursor Horizontal Absolute)
- [ ] **vpa_test.go** - VPA (Vertical Position Absolute)
- [ ] **hvp_test.go** - HVP (Horizontal and Vertical Position)
- [ ] **cnl_test.go** - CNL (Cursor Next Line)
- [ ] **cpl_test.go** - CPL (Cursor Previous Line)

### Priority 2: Character Manipulation

Essential for text editing:

- [ ] **dch_test.go** - DCH (Delete Character)
- [ ] **ech_test.go** - ECH (Erase Character)
- [ ] **dl_test.go** - DL (Delete Line)
- [ ] **il_test.go** - IL (Insert Line)
- [ ] **ed_test.go** - ED (Erase in Display)
- [ ] **el_test.go** - EL (Erase in Line)

### Priority 3: Scrolling and Regions

Important for fullscreen applications:

- [ ] **decstbm_test.go** - DECSTBM (Set Top and Bottom Margins)
- [ ] **ind_test.go** - IND (Index)
- [ ] **ri_test.go** - RI (Reverse Index)
- [ ] **su_test.go** - SU (Scroll Up)
- [ ] **sd_test.go** - SD (Scroll Down)

### Priority 4: Character Sets and Attributes

Visual display and character encoding:

- [ ] **sgr_test.go** - SGR (Select Graphic Rendition)
- [ ] **sm_test.go** - SM (Set Mode)
- [ ] **rm_test.go** - RM (Reset Mode)
- [ ] **decset_test.go** - DECSET (DEC Private Mode Set)
- [ ] **decreset_test.go** - DECRESET (DEC Private Mode Reset)

### Priority 5: Device Reports and Queries

Terminal identification and state queries:

- [ ] **da_test.go** - DA (Device Attributes)
- [ ] **da2_test.go** - DA2 (Secondary Device Attributes)
- [ ] **dsr_test.go** - DSR (Device Status Report)
- [ ] **decrqss_test.go** - DECRQSS (Request Selection or Setting)

### Priority 6: Advanced Features

Less common but useful for full compliance:

- [ ] **decaln_test.go** - DECALN (Screen Alignment Test)
- [ ] **decbi_test.go** - DECBI (Back Index)
- [ ] **decfi_test.go** - DECFI (Forward Index)
- [ ] **decstr_test.go** - DECSTR (Soft Terminal Reset)
- [ ] **ris_test.go** - RIS (Reset to Initial State)
- [ ] **save_restore_cursor_test.go** - DECSC/DECRC (Save/Restore Cursor)

### Priority 7: xterm Extensions

xterm-specific features:

- [ ] **xterm_winops_test.go** - Window operations
- [ ] **manipulate_selection_data_test.go** - Selection/clipboard
- [ ] **change_color_test.go** - Color changes
- [ ] **change_dynamic_color_test.go** - Dynamic color changes

## Adding New Escape Sequences to helpers.go

When converting tests, you may need to add new escape sequence helper functions:

```go
// Example: Add CHA (Cursor Horizontal Absolute)
func CHA(d *Driver, x int) {
    d.WriteRaw(fmt.Sprintf("%s[%dG", ESC, x))
}
```

Common patterns:
- `CSI n A` → `fmt.Sprintf("%s[%dA", ESC, n)` - Cursor Up
- `CSI n ; m H` → `fmt.Sprintf("%s[%d;%dH", ESC, y, x)` - Cursor Position
- `CSI ? n h` → `fmt.Sprintf("%s[?%dh", ESC, n)` - DEC Private Mode Set
- `CSI ? n l` → `fmt.Sprintf("%s[?%dl", ESC, n)` - DEC Private Mode Reset

## Running Tests

```bash
# Run all tests
go test texelation/apps/texelterm/esctest

# Run specific test file
go test texelation/apps/texelterm/esctest -run Test_CUU

# Run with verbose output
go test -v texelation/apps/texelterm/esctest

# Run specific test
go test texelation/apps/texelterm/esctest -run Test_ICH_DefaultParam
```

## Test Conversion Guidelines

1. **Preserve test intent**: Keep the same test logic and expectations
2. **Add attribution**: Each file should cite the original esctest2 source
3. **Use 1-indexed coordinates**: VT standards use 1-indexed positions
4. **Handle optional parameters**: Use variadic args for sequences with optional params
5. **Document failures**: Note any failing tests and the reason

## Next Steps

1. Convert Priority 1 tests (basic cursor movement)
2. Fix DECLRMM support to pass failing ICH tests
3. Investigate CUU scroll region behavior
4. Continue with Priority 2-7 tests based on need
5. Consider automated conversion script for remaining tests

## Contributing

When adding new tests:
1. Follow the existing file naming convention: `<sequence>_test.go`
2. Add attribution header citing esctest2
3. Document test purpose in comments
4. Run tests and note any failures
5. Update this README with test status

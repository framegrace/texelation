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

### Test Results Summary

**Total**: 53 tests
**Passing**: 53 (100%) ✅
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

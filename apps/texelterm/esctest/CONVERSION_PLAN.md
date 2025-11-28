# esctest2 Conversion Plan

## Overview

**Total test files in esctest2**: 80
**Currently converted**: 2 (CUU, ICH)
**Remaining**: 78

## Execution Strategy

For each batch:
1. **Convert** - Port Python tests to Go, add to helpers.go as needed
2. **Run** - Execute tests, document failures
3. **Fix** - Implement missing features or fix bugs (iteratively)
4. **Commit** - Commit when all tests in batch pass

## Batch 1: Basic Cursor Movement (Priority 1A)

**Goal**: Core cursor positioning - heavily used, must work perfectly

| File | Sequence | Tests | Description |
|------|----------|-------|-------------|
| cud.py | CUD | 5 | Cursor Down |
| cuf.py | CUF | 5 | Cursor Forward |
| cub.py | CUB | 7 | Cursor Backward |
| cha.py | CHA | 6 | Cursor Horizontal Absolute |

**Total**: 23 tests
**Estimated complexity**: LOW - Similar to CUU, likely already implemented
**Expected failures**: 0-2 (may have scroll region edge cases)

## Batch 2: Advanced Cursor Movement (Priority 1B)

| File | Sequence | Tests | Description |
|------|----------|-------|-------------|
| vpa.py | VPA | ~4 | Vertical Position Absolute |
| hvp.py | HVP | ~4 | Horizontal and Vertical Position |
| cup.py | CUP | ~8 | Cursor Position (main positioning command) |
| cnl.py | CNL | ~4 | Cursor Next Line |
| cpl.py | CPL | ~4 | Cursor Previous Line |

**Total**: ~24 tests
**Estimated complexity**: LOW-MEDIUM
**Expected failures**: 0-3

## Batch 3: Save/Restore Cursor

| File | Sequence | Tests | Description |
|------|----------|-------|-------------|
| save_restore_cursor.py | DECSC/DECRC | ~6 | Save and restore cursor position |
| decrc.py | DECRC | ~3 | Restore cursor (DEC) |
| scorc.py | SCOSC/SCORC | ~3 | SCO save/restore |

**Total**: ~12 tests
**Estimated complexity**: LOW - Likely already implemented
**Expected failures**: 0-1

## Batch 4: Character Editing (Priority 2A)

| File | Sequence | Tests | Description |
|------|----------|-------|-------------|
| dch.py | DCH | ~6 | Delete Character |
| ech.py | ECH | ~4 | Erase Character |
| rep.py | REP | ~3 | Repeat previous character |

**Total**: ~13 tests
**Estimated complexity**: MEDIUM - DCH/ECH likely exist, REP may not
**Expected failures**: 1-3

## Batch 5: Line Editing (Priority 2B)

| File | Sequence | Tests | Description |
|------|----------|-------|-------------|
| dl.py | DL | ~6 | Delete Line |
| il.py | IL | ~6 | Insert Line |

**Total**: ~12 tests
**Estimated complexity**: MEDIUM - Likely implemented
**Expected failures**: 0-2

## Batch 6: Erase Operations (Priority 2C)

| File | Sequence | Tests | Description |
|------|----------|-------|-------------|
| ed.py | ED | ~8 | Erase in Display |
| el.py | EL | ~6 | Erase in Line |
| decsed.py | DECSED | ~4 | Selective Erase in Display |
| decsel.py | DECSEL | ~4 | Selective Erase in Line |

**Total**: ~22 tests
**Estimated complexity**: MEDIUM
**Expected failures**: 2-4 (selective erase may not be implemented)

## Batch 7: Scrolling and Regions (Priority 3)

| File | Sequence | Tests | Description |
|------|----------|-------|-------------|
| decstbm.py | DECSTBM | ~8 | Set Top/Bottom Margins (already used in tests) |
| ind.py | IND | ~4 | Index (scroll up) |
| ri.py | RI | ~4 | Reverse Index (scroll down) |
| su.py | SU | ~3 | Scroll Up |
| sd.py | SD | ~3 | Scroll Down |
| nel.py | NEL | ~3 | Next Line |

**Total**: ~25 tests
**Estimated complexity**: MEDIUM-HIGH
**Expected failures**: 2-5

## Batch 8: Attributes and Modes (Priority 4A)

| File | Sequence | Tests | Description |
|------|----------|-------|-------------|
| sgr.py | SGR | ~30 | Select Graphic Rendition (colors, bold, etc.) |
| sm.py | SM | ~8 | Set Mode |
| rm.py | RM | ~8 | Reset Mode |

**Total**: ~46 tests
**Estimated complexity**: HIGH - SGR is complex
**Expected failures**: 5-10 (many SGR attributes may not be fully tested)

## Batch 9: DEC Private Modes (Priority 4B)

| File | Sequence | Tests | Description |
|------|----------|-------|-------------|
| decset.py | DECSET | ~20 | DEC Private Mode Set (huge file) |

**Total**: ~20 tests
**Estimated complexity**: HIGH - Many modes may not be implemented
**Expected failures**: 5-15 (expect many unimplemented modes)

## Batch 10: Device Reports (Priority 5)

| File | Sequence | Tests | Description |
|------|----------|-------|-------------|
| da.py | DA | ~3 | Device Attributes |
| da2.py | DA2 | ~3 | Secondary Device Attributes |
| decdsr.py | DECDSR | ~6 | DEC Device Status Report |
| decrqss.py | DECRQSS | ~8 | Request Selection or Setting |
| decrqm.py | DECRQM | ~4 | Request Mode |

**Total**: ~24 tests
**Estimated complexity**: MEDIUM - Query/response sequences
**Expected failures**: 3-8 (requires PTY interaction simulation)

## Later Batches (Defer for now)

**Batch 11**: Advanced DEC features (DECALN, DECBI, DECFI, etc.) - ~20 tests
**Batch 12**: Character sets and tabulation - ~15 tests
**Batch 13**: xterm extensions (winops, colors, selection) - ~25 tests
**Batch 14**: Control characters (BS, CR, LF, FF, etc.) - ~10 tests
**Batch 15**: Obscure/less important sequences - ~15 tests

## Session Plan

### Today's Goal: Batches 1-3 (Core Cursor Movement)

1. **Batch 1** (23 tests) - Basic cursor movement
2. **Batch 2** (24 tests) - Advanced cursor movement
3. **Batch 3** (12 tests) - Save/restore cursor

**Total for session**: ~59 tests + 11 existing = **70 tests**

### Workflow Per Batch

1. **Convert** (~15-20 min per batch)
   - Read Python test file
   - Port to Go following existing pattern
   - Add any missing escape sequence helpers

2. **Test** (~2 min)
   - Run: `go test texelation/apps/texelterm/esctest -v`
   - Document failures

3. **Fix** (~5-30 min depending on complexity)
   - Investigate failures
   - Fix bugs or implement missing features
   - Re-test until all pass

4. **Commit** (~1 min)
   - Commit with descriptive message
   - Update README.md with results

5. **Iterate** - Move to next batch

## Success Criteria

- All converted tests passing (100%)
- Existing parser tests still passing
- Clear documentation of what was fixed
- Commit after each batch completion

## If We Hit Blockers

For any test that reveals a MAJOR missing feature (e.g., "character sets not implemented at all"):
- **Document** the failure clearly
- **Skip** and mark as known issue in README
- **Continue** with other tests
- **Revisit** in a dedicated session

## Next Session (if needed)

Continue with Batches 4-6 (character and line editing - another ~47 tests)

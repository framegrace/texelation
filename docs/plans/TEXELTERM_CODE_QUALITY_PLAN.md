# Texelterm Code Quality Refactoring Plan

**Status:** Ready for Review
**Author:** Claude Code
**Date:** 2025-12-26 (Updated after DisplayBuffer migration completed)

## Executive Summary

Following the DisplayBuffer migration (commit `0fe0663`), the texelterm codebase has technical debt from the organic growth and patching process. This plan addresses code quality issues that can be fixed **WITHOUT changing any functionality**.

### Main Themes
1. **Dead/Legacy code** that should be removed (~100 lines)
2. **Duplicated logic** that should be consolidated (~50 lines)
3. **Complex functions** that should be split into focused helpers
4. **Inconsistent patterns** that should be standardized

**Estimated total impact**: Remove ~150 lines, improve maintainability significantly

---

## Issue Categories

### Category A: Dead/Legacy Code (High Priority, Low Risk)

#### A1. Delete `history.go` - Completely Unused

**File**: `apps/texelterm/parser/history.go` (51 lines)

**Problem**: This file defines `HistoryConfig` with fields like `Compress`, `Encrypt`, `FlushInterval` that are **never implemented**. Only `MemoryLines` and `PersistDir` are extracted in `term.go:1325-1329`, then immediately converted to `ScrollbackHistoryConfig`.

**Evidence**:
```go
// history.go - fields that are NEVER USED:
Compress         bool          // Never read
Encrypt          bool          // Never read
FlushInterval    time.Duration // Never read
RespectMarkers   bool          // Never read
RedactPatterns   []string      // Never read
EncryptionKey    []byte        // Never read
```

**Action**:
1. Delete `history.go` entirely
2. Update `term.go` to accept `ScrollbackHistoryConfig` directly or use inline struct

**Risk**: None - purely dead code

---

#### A2. Remove Deprecated No-Op Functions

**Files**: `vterm.go`, `vterm_display_buffer.go`

**Problem**: Several functions exist only for backward compatibility but do nothing:

```go
// vterm.go:873-879
func WithDisplayBuffer(_ bool) Option {
    return func(_ *VTerm) {}  // No-op: display buffer is always enabled
}

// vterm_display_buffer.go:422-425
func (v *VTerm) displayBufferBackspace() {
    // No-op
}
```

**Action**: Delete these functions

**Risk**: Low - need to verify no external callers

---

### Category B: Duplicated Logic (Medium Priority)

#### B1. Consolidate Identical Trim Functions

**File**: `scrollback_history.go:285-299, 378-391`

**Problem**: `trimMemoryLocked()` and `trimAboveLocked()` are **100% identical**:

```go
// Both do EXACTLY the same thing:
func (h *ScrollbackHistory) trimMemoryLocked() {
    excess := len(h.lines) - h.config.MaxMemoryLines
    if excess <= 0 { return }
    for i := 0; i < excess; i++ { h.lines[i] = nil }
    h.lines = h.lines[excess:]
    h.windowStart += int64(excess)
}

func (h *ScrollbackHistory) trimAboveLocked() {
    // IDENTICAL CODE
}
```

**Action**: Delete `trimMemoryLocked()`, call `trimAboveLocked()` from `Append()`

---

#### B2. Consolidate Scroll-to-Live-Edge Variants

**File**: `display_buffer.go:470-507`

**Problem**: Two nearly identical 10-line functions with one subtle difference:

```go
func (db *DisplayBuffer) scrollToLiveEdge() {
    db.viewportTop = totalLines - db.height
    // Always updates
}

func (db *DisplayBuffer) scrollToLiveEdgeNoShrink() {
    // Only updates if scrolling DOWN (never shrinks viewport)
    if idealViewportTop > db.viewportTop {
        db.viewportTop = idealViewportTop
    }
}
```

**Action**: Merge into single function:

```go
func (db *DisplayBuffer) scrollToLiveEdge(allowShrink bool) {
    idealViewportTop := max(0, totalLines - db.height)
    if allowShrink || idealViewportTop > db.viewportTop {
        db.viewportTop = idealViewportTop
    }
    db.atLiveEdge = true
}
```

**Impact**: 5 callers use `scrollToLiveEdge()` (allowShrink=true), 8 use NoShrink variant

---

#### B3. Reduce Repetition in DisplayBuffer Edit Operations

**File**: `vterm_display_buffer.go:491-588`

**Problem**: Six functions repeat the same pattern:

```go
// Pattern repeated 6 times:
func (v *VTerm) displayBufferEraseToEndOfLine() {
    if v.displayBuf == nil || v.displayBuf.display == nil { return }
    v.displayBufferSetCursorFromPhysical(false)  // Same
    v.displayBuf.display.Erase(0)                 // Different
    v.MarkAllDirty()                              // Same
}
```

**Action**: Create helper to eliminate duplication:

```go
func (v *VTerm) withDisplayBufferOp(syncCursor bool, op func(*DisplayBuffer)) {
    if v.displayBuf == nil || v.displayBuf.display == nil { return }
    if syncCursor {
        v.displayBufferSetCursorFromPhysical(false)
    }
    op(v.displayBuf.display)
    v.MarkAllDirty()
}

// Then each operation becomes a one-liner:
func (v *VTerm) displayBufferEraseToEndOfLine() {
    v.withDisplayBufferOp(true, func(db *DisplayBuffer) { db.Erase(0) })
}
```

---

### Category C: Magic Numbers / Constants (Low Priority)

#### C1. Define Constants for Default Values

**Problem**: Same default values appear in 5+ locations:

| Value | Locations |
|-------|-----------|
| 200 (MarginAbove) | display_buffer.go:83, vterm_display_buffer.go:42,418,465 |
| 50 (MarginBelow) | display_buffer.go:86, vterm_display_buffer.go:44,419,466 |
| 5000 (MaxMemoryLines) | vterm_display_buffer.go:41, scrollback_history.go:75 |
| 80 (DefaultWidth) | display_buffer.go:89, live_editor.go:160,189,281 |

**Action**: Create constants in a single location:

```go
// display_buffer.go or a new constants.go
const (
    DefaultMarginAbove    = 200
    DefaultMarginBelow    = 50
    DefaultMaxMemoryLines = 5000
    DefaultWidth          = 80
    DefaultHeight         = 24
)
```

---

### Category D: Complex Function Simplification (Medium Priority)

#### D1. Split `SetCursor` - Too Many Responsibilities

**File**: `display_buffer.go:116-186` (70 lines)

**Problem**: This single function handles 3 distinct concerns:
1. Check if cursor is on/near live line → set cursor there
2. Check if should try uncommit → restore line from history
3. Log debug info if cursor is ignored

**Action**: Extract helpers with clear names:

```go
func (db *DisplayBuffer) SetCursor(physX, physY int) {
    if db.cursorIsOnLiveLine(physY) {
        db.setCursorOnLiveLine(physX, physY)
    } else if db.shouldTryUncommit(physY) {
        db.tryUncommitAndSetCursor(physX, physY)
    } else {
        db.logIgnoredCursor(physX, physY)
    }
}

func (db *DisplayBuffer) cursorIsOnLiveLine(physY int) bool {
    // Lines 120-137 extracted
}

func (db *DisplayBuffer) setCursorOnLiveLine(physX, physY int) {
    // Lines 139-162 extracted
}
// etc.
```

---

#### D2. Simplify `displayBufferSetCursorFromPhysical`

**File**: `vterm_display_buffer.go:354-407` (54 lines)

**Problem**: Complex dual-strategy function with confusing conditional logic:

```go
func (v *VTerm) displayBufferSetCursorFromPhysical(isRelativeMove bool) {
    sameRow := v.prevCursorY == v.cursorY
    useDelta := isRelativeMove && sameRow  // Complex condition

    if useDelta {
        // Delta-based sync (15 lines)
    } else {
        // Absolute sync (3 lines)
    }
    // Update prev state (4 lines)
    // Debug logging (8 lines)
}
```

**Action**: Extract strategies into named functions:

```go
func (v *VTerm) displayBufferSetCursorFromPhysical(isRelativeMove bool) {
    if v.shouldUseDeltaCursorSync(isRelativeMove) {
        v.syncCursorWithDelta()
    } else {
        v.syncCursorAbsolute()
    }
    v.updatePreviousCursorState()
}

func (v *VTerm) shouldUseDeltaCursorSync(isRelativeMove bool) bool {
    return isRelativeMove && v.prevCursorY == v.cursorY
}
```

---

### Category E: Documentation (Low Priority)

#### E1. Add Coordinate Terminology Comment

**Problem**: "Physical" vs "viewport" terminology is used inconsistently:
- `displayBufferSetCursorFromPhysical` - uses "physical"
- `GetPhysicalCursorPos` - returns "viewport" coords
- `viewportTop` - clearly viewport

**Action**: Add documentation comment at top of `display_buffer.go`:

```go
// Coordinate Terminology:
//
// - Viewport coords (physX, physY): 0-based position within visible screen area
//   - (0,0) is top-left of what user sees
//   - Used in: SetCursor, GetPhysicalCursorPos
//
// - Buffer coords (bufferIdx): index into db.lines array
//   - bufferIdx = viewportTop + physY
//   - Used in: internal line access
//
// - Logical offset: character position within a LogicalLine
//   - Used in: LiveEditor.cursorOffset
//
// - Physical lines: wrapped representation of logical lines at current width
//   - vs Logical lines: unwrapped, width-independent storage
```

---

## Implementation Order

### Phase 1: Safe Deletions (~15 min)
1. [ ] Delete `history.go` (51 lines)
2. [ ] Update `term.go` to remove HistoryConfig usage
3. [ ] Delete `WithDisplayBuffer` no-op function
4. [ ] Delete `displayBufferBackspace` no-op function
5. [ ] Run tests to verify

### Phase 2: Consolidate Duplicates (~30 min)
6. [ ] Delete `trimMemoryLocked`, update caller to use `trimAboveLocked`
7. [ ] Merge `scrollToLiveEdge` variants into single function with parameter
8. [ ] Update all 13 callers
9. [ ] Run tests to verify

### Phase 3: Extract Constants (~15 min)
10. [ ] Add constants block in `display_buffer.go`
11. [ ] Update all 10+ locations using magic numbers
12. [ ] Run tests to verify

### Phase 4: Add Helper Function (~20 min)
13. [ ] Add `withDisplayBufferOp` helper
14. [ ] Refactor 6 edit operation functions to use helper
15. [ ] Run tests to verify

### Phase 5: Split Complex Functions (~45 min)
16. [ ] Split `SetCursor` into 4 focused helpers
17. [ ] Split `displayBufferSetCursorFromPhysical` into 3 helpers
18. [ ] Run tests to verify

### Phase 6: Documentation (~10 min)
19. [ ] Add coordinate terminology comment block
20. [ ] Final test run

---

## Risk Assessment

| Change | Risk | Mitigation |
|--------|------|------------|
| Delete history.go | None | Dead code, never called |
| Delete no-op functions | Low | Grep for callers first |
| Merge trim functions | Low | Identical logic |
| Merge scroll functions | Medium | Test scroll behavior carefully |
| Add helper function | Low | Purely mechanical refactoring |
| Split complex functions | Medium | Extract exact existing logic |

---

## Success Metrics

- [ ] All 50+ existing parser tests pass
- [ ] Manual testing: typing, scrolling, resize, bash editing
- [ ] No behavior changes (this is purely structural)
- [ ] ~150 lines of code removed
- [ ] No function longer than 40 lines
- [ ] No duplicate 10+ line code blocks

---

## Files Modified Summary

| File | Action | Lines Changed |
|------|--------|---------------|
| `history.go` | **DELETE** | -51 |
| `term.go` | Remove HistoryConfig conversion | -10 |
| `vterm.go` | Remove WithDisplayBuffer | -5 |
| `vterm_display_buffer.go` | Add helper, remove no-op, split function | ~40 modified |
| `display_buffer.go` | Add constants, merge scroll, split SetCursor | ~60 modified |
| `scrollback_history.go` | Remove duplicate trim | -15 |

**Net result**: ~80 lines removed, better organization

---

*This plan focuses on code quality improvements that preserve exact existing behavior.*

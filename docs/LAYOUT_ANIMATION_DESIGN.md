# Layout Animation Design Decisions

**Date**: 2025-12-06
**Status**: Implemented server-side (Dec 2025)

## Context

Based on external architectural review feedback, expanding the layout system to animate splits and closes with smooth transitions. The final design runs entirely on the **server**: layout ratios animate in `texel/layout_transitions.go`, and the server streams intermediate snapshots/deltas to clients.

## Key Decisions

### A. LayoutAnimator Scope

**Decision**: Option 1 - Tree-internal component (APPROVED)

**Rationale**:
- Layout animation is Tree's responsibility, not a visual effect
- Simpler implementation with fewer moving parts
- Avoids violating separation of concerns (effects shouldn't modify layout)
- Can always be made pluggable later if theme configuration is needed

**Implementation**:
- LayoutAnimator lives in `texel/layout_transitions.go` inside the core `texel` package
- Not exposed through Effect interface
- Uses same Timeline primitive for consistency
- Tree operations (SplitActive, CloseActiveLeaf) directly control animations

### B. Timeline API Breaking Change

**Decision**: Accept breaking change - all Timeline methods require `now time.Time` parameter (APPROVED)

**Rationale**:
- Eliminates time source inconsistency between render loop and Timeline
- Prevents animation jitter from multiple `time.Now()` calls per frame
- Proper frame synchronization is critical for smooth animations
- Migration is straightforward with clear compiler errors

**Migration Required**:
- All `AnimateTo(key, target, duration)` → `AnimateTo(key, target, duration, now)`
- All `Get(key)` → `Get(key, now)`
- All `IsAnimating(key)` → `IsAnimating(key, now)`
- Update all current effects: fadeTint, rainbow, keyflash, any others

## Current Implementation

- **Server-driven**: `LayoutTransitionManager` (`texel/layout_transitions.go`) animates split ratios at ~60fps and broadcasts snapshots/deltas each frame.
- **Workspace integration**: `workspace.go` calls `AnimateSplit` and `AnimateRemoval` for splits/closes. A grace period skips animations during startup/restore.
- **Timeline unification**: Effects and layout share the timestamped `Timeline` helper to avoid jitter.
- **Config**: Parsed from `layout_transitions` in the theme (enabled, duration_ms, easing). `min_threshold` is parsed but not applied yet.

## Remaining Follow-ups
- Interrupt animations if a new split/close arrives mid-flight.
- Decide whether to keep or remove `min_threshold` (currently unused).
- Add coverage around animated splits/closes and update EFFECTS_GUIDE with a brief note.
- Consider optional workspace/pane swap animations in the future.

## Notes

- Commit after each phase for easy rollback
- Run full regression tests before each commit
- Update this document as implementation progresses

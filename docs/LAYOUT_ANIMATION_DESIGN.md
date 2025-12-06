# Layout Animation Design Decisions

**Date**: 2025-12-06
**Status**: Implementation in progress

## Context

Based on external architectural review feedback, expanding the effects system to support layout animations (split/remove/replace panes with smooth transitions).

## Key Decisions

### A. LayoutAnimator Scope

**Decision**: Option 1 - Tree-internal component (APPROVED)

**Rationale**:
- Layout animation is Tree's responsibility, not a visual effect
- Simpler implementation with fewer moving parts
- Avoids violating separation of concerns (effects shouldn't modify layout)
- Can always be made pluggable later if theme configuration is needed

**Implementation**:
- LayoutAnimator lives inside `texel/tree.go` or adjacent file
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

## Implementation Phases

### Phase 1: Foundation Fixes ✅ COMPLETED (commit 8e5ba39)
1. ✅ Timeline time source unification
2. ✅ Base effect helper classes (PaneEffectBase, WorkspaceEffectBase)
3. ✅ Migrate all existing effects (fadeTint, keyflash)

**Outcome**: Cleaner API, reduced effect code by ~30%, eliminated animation jitter

### Phase 2: Layout Animation System ✅ COMPLETED (commit 61dc222)
1. ✅ New event triggers (TriggerPaneSplit, TriggerPaneRemoving, TriggerPaneReplaced)
2. ✅ LayoutAnimator component
3. ✅ Integration into Tree operations (SplitActive, CloseActiveLeaf, Resize)

**Outcome**: Smooth split animations working, disabled by default, all tests passing

### Phase 3: Integration & Migration (FUTURE)
1. Emit layout triggers from Desktop layer (requires event bus access)
2. Create example visual effect using layout triggers
3. Optional: animated pane removal (requires ghost state)

### Phase 4: Documentation & Testing (FUTURE)
1. Update EFFECTS_GUIDE.md with layout animation examples
2. Add unit tests for LayoutAnimator
3. Integration tests for animated split scenarios
4. Document how to enable animations in applications

## Notes

- Commit after each phase for easy rollback
- Run full regression tests before each commit
- Update this document as implementation progresses

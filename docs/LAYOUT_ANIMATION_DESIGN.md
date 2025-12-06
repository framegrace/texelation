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

### Phase 1: Foundation Fixes (CURRENT)
1. Timeline time source unification
2. Base effect helper classes (PaneEffectBase, WorkspaceEffectBase)
3. Migrate all existing effects

### Phase 2: Layout Animation System
1. New event triggers (TriggerPaneSplit, TriggerPaneRemoving, TriggerPaneReplaced)
2. LayoutAnimator component
3. Integration into Tree operations

### Phase 3: Integration & Migration
1. Emit layout triggers from Tree
2. Demonstrate with example effect
3. Update Effect Manager if needed

### Phase 4: Documentation & Testing
1. Update EFFECTS_GUIDE.md
2. Add tests for new components
3. Integration tests for animated layout

## Notes

- Commit after each phase for easy rollback
- Run full regression tests before each commit
- Update this document as implementation progresses

# Issue #199 — Plan D: Cross-Restart Viewport Persistence (STUB)

**Status:** Stub — write the full TDD plan after Plan B lands.
**Depends on:** Plan A merged, Plan B merged.
**Spec reference:** Sub-problem 2 (cross-restart clause) + spec sequencing step 7.

## Goal

Best-effort resume of `PaneViewportState` across server restarts. On server restart, the next `ResumeRequest` for that session matches persisted viewport state; if found and `ViewBottomIdx` is still in the store, restore exactly. Else fall back to Plan B's missing-anchor policy.

## Scope

- Extend existing scrollback WAL / `PageStore` persistence to also carry per-pane `PaneViewportState` alongside cells.
- On `SnapshotStore` save cycles, include latest per-pane viewport.
- On `SnapshotStore` load at startup, reconstruct the viewport map keyed by `(SessionID, PaneID)`.
- On `ResumeRequest`, server layers persisted state onto whatever the client supplied — client-supplied wins when both are present (user has a newer intent than the snapshot).

## Out of scope

- Changing the on-disk scrollback format beyond adding viewport state.
- Cross-session viewport sharing (viewport is session-scoped).

## Open decision

Spec open question: per-session (simpler) vs per-client-identity (survives multiple clients on the same session). Decide during plan write-up. Default: per-session.

## Files touched (estimated)

- `internal/runtime/server/snapshot_store.go` — add viewport payload.
- `internal/runtime/server/snapshot_store_test.go`.
- `apps/texelterm/parser/page_store.go` — possibly; may not need if snapshot store is enough.
- Server startup wiring — load viewport map before accepting connections.

## Test checklist

- Save → shut down → restart → `ResumeRequest` without viewports lands at persisted position.
- Save → shut down → restart → `ResumeRequest` with viewports: client wins.
- Save → shut down → evict enough scrollback → restart → missing-anchor fallback fires.

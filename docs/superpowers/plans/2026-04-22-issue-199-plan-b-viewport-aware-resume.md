# Issue #199 — Plan B: Viewport-Aware Resume (STUB)

**Status:** Stub — write the full TDD plan after Plan A lands.
**Depends on:** Plan A merged.
**Spec reference:** Sub-problem 2 + spec sequencing step 5.

## Goal

On reconnect, land the client at the exact scrolled-back position they left — not the live edge. Applies to disconnect/reconnect within the same server process; cross-restart is Plan D.

## Scope

- Extend `protocol.ResumeRequest` with `PaneViewports []PaneViewportState`.
- Define `PaneViewportState{PaneID, AltScreen, ViewBottomIdx, WrapSegmentIdx, AutoFollow, ViewportRows, ViewportCols}`.
- Server first-paint logic per pane:
  1. AltScreen → send `altBuffer`, skip scroll resolution.
  2. AutoFollow → clamp to `Store.Max()`.
  3. Otherwise honor `ViewBottomIdx` exactly; compute ViewTop by walking `Wrapped` chains; place `WrapSegmentIdx` at bottom physical row.
- Missing-anchor policy: `ViewBottomIdx < Store.OldestRetained()` → snap to oldest, force `AutoFollow=false`.
- Client: persist last-known viewport per pane on disconnect; send in `ResumeRequest`.

## Out of scope

- Cross-restart persistence — Plan D.
- First-paint snapshot size optimizations beyond Plan A's overscan.

## Files touched (estimated)

- `protocol/messages.go` — extend `ResumeRequest`.
- `protocol/resume_test.go` — round-trip.
- `internal/runtime/server/session.go` — honor viewports on `ResumeAccept`.
- `internal/runtime/server/desktop_publisher.go` — first-paint path uses supplied viewport.
- `apps/texelterm/parser/sparse/view_window.go` — `WalkUpwardFromBottom(viewBottom, wrapSeg, rows, cols)` helper.
- `internal/runtime/client/client_loop.go` — build ResumeRequest from local ViewWindow state on disconnect.
- Integration tests.

## Test checklist

- Round-trip `ResumeRequest` with empty / populated `PaneViewports`.
- First-paint: `AutoFollow=true` clamps to live edge.
- First-paint: `AutoFollow=false` with valid anchor lands exactly.
- Missing anchor: snap to oldest + force `AutoFollow=false`.
- AltScreen: scroll fields ignored; altBuffer sent.
- Wrap-segment precision: row that wraps into multiple segments → correct segment is bottom.

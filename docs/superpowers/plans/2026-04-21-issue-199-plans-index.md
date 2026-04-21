# Issue #199 — Plans Index

Issue #199 (viewport-only client rendering) is split into five sequential PRs after pre-design audit and brainstorming. Spec: `docs/superpowers/specs/2026-04-20-issue-199-viewport-only-rendering-design.md`.

Each plan is independently mergeable and must ship on its own PR off `main`. Order matters — later plans depend on the wire formats introduced earlier.

| Plan | Scope | Spec steps | Status | File |
|------|-------|------------|--------|------|
| **A** | Viewport clipping + FetchRange foundation. New `BufferDelta.RowBase`, `Flags.AltScreen`, `MsgViewportUpdate`, `MsgFetchRange`/`Response`. Client sparse PaneCache. Publisher clips per-client. | 1–4 | **Written, ready for execution** | `2026-04-21-issue-199-plan-a-viewport-clipping-fetchrange.md` |
| **B** | Viewport-aware resume. Extend `MsgResumeRequest` with `[]PaneViewportState`. Server honors anchor + wrap-segment + autoFollow on first paint. Missing-anchor → snap-to-oldest, autoFollow=false. | 5 | Stub | `2026-04-22-issue-199-plan-b-viewport-aware-resume.md` |
| **C** | Server-side selection/copy. `MsgResolveBoundary`, `MsgCaptureSelection`, `MsgCaptureResult`. Client selection state migrates to `(globalIdx, col)` / `(screenRow, col)` with coord-space discriminator. Copy returns `{entryID, plain, ansi}`. | 6 | Stub | `2026-04-23-issue-199-plan-c-server-side-selection.md` |
| **D** | Cross-restart persistence of `PaneViewportState` alongside scrollback WAL/PageStore. Best-effort first-paint resume across server restarts. | 7 | Stub | `2026-04-24-issue-199-plan-d-viewport-persistence.md` |
| **E** | Statusbar "scrollback fetch pending" indicator. Statusbar app subscribes to server `EventDispatcher`; renders unobtrusive spinner/text while inflight fetches for visible rows exist. | 8 | Stub | `2026-04-25-issue-199-plan-e-statusbar-fetch-indicator.md` |

## Rules across the plans

- All plans stay on branch `design/issue-199-viewport-only-rendering` until merged. Per project CLAUDE.md: never commit directly to `main`.
- Each plan produces its own PR; A must land before B; B and C can land in either order but both must land before D uses resume semantics.
- After Plan A's protocol version bump, older clients cannot connect without a new version bump only if the wire format changes again. Plans B–E must treat the Plan A protocol version as the floor.
- Test rewrite budget (from spec): ~15–20 tests touched. Plan A absorbs most of it (steps 4b/4c). Later plans should be lighter.

## Post-ship follow-ups (separate tracking, not in any of the above)

- Investigate whether cross-scrollback selection worked in a previous build but regressed (user belief). Plan C supersedes the current selection path regardless, so this is archaeology, not blocking.
- Hysteresis / deadline tuning on the PaneCache based on profiling.
- Predictive prefetch thread — explicitly deferred unless measurement shows need.

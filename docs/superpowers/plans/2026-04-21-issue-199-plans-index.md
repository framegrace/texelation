# Issue #199 — Plans Index

Issue #199 (viewport-only client rendering) is split into seven sequential PRs after pre-design audit and brainstorming. Spec: `docs/superpowers/specs/2026-04-20-issue-199-viewport-only-rendering-design.md`. Plan F was added 2026-04-25 as a future-direction stub (not in the original spec).

Each plan is independently mergeable and must ship on its own PR off `main`. Order matters — later plans depend on the wire formats introduced earlier.

| Plan | Scope | Spec steps | Status | File |
|------|-------|------------|--------|------|
| **A** | Viewport clipping + FetchRange foundation. New `BufferDelta.RowBase`, `Flags.AltScreen`, `MsgViewportUpdate`, `MsgFetchRange`/`Response`. Client sparse PaneCache. Publisher clips per-client. | 1–4 | **MERGED** (PR #202, 2026-04-23) | `2026-04-21-issue-199-plan-a-viewport-clipping-fetchrange.md` |
| **B** | Viewport-aware resume. Extend `MsgResumeRequest` with `[]PaneViewportState`. Server honors anchor + wrap-segment + autoFollow on first paint. Missing-anchor → snap-to-oldest, autoFollow=false. | 5 | **MERGED** (PR #203, 2026-04-25) | `2026-04-22-issue-199-plan-b-viewport-aware-resume.md` |
| **C** | Server-side selection/copy. `MsgResolveBoundary`, `MsgCaptureSelection`, `MsgCaptureResult`. Client selection state migrates to `(globalIdx, col)` / `(screenRow, col)` with coord-space discriminator. Copy returns `{entryID, plain, ansi}`. | 6 | Stub | `2026-04-23-issue-199-plan-c-server-side-selection.md` |
| **D** | **Client-side** session/viewport persistence. Wire `sessionID` + viewport-tracker + `lastSequence` to disk; load at client startup so a fresh `texel-client` process resumes against the still-running daemon. **This is what makes Plan B real for users.** Includes Plan B carry-forward items (phantom pane pruning, eviction test, `ReadMessage` cap). | 7 (within-daemon-lifetime) | **In brainstorming (2026-04-25)** | `2026-04-24-issue-199-plan-d-viewport-persistence.md` |
| **D2** | **Server-side** cross-daemon-restart viewport persistence. Mirror the existing `MainScreenState`/WAL pattern at the session layer so daemon restarts also preserve viewport. Has open architectural questions (session rehydration vs full session-table persist) — defer until Plan D bakes. | 7 (cross-daemon-restart) | Stub | `2026-04-25-issue-199-plan-d2-server-viewport-persistence.md` |
| **E** | Statusbar "scrollback fetch pending" indicator. Statusbar app subscribes to server `EventDispatcher`; renders unobtrusive spinner/text while inflight fetches for visible rows exist. | 8 | Stub | `2026-04-25-issue-199-plan-e-statusbar-fetch-indicator.md` |
| **F** | **Session recovery / discovery.** New wire messages for "list known sessions" + "recover session by ID." Lets a user reconstruct client state from server side after losing the client's persistence file. Depends on D2 (server must persist sessions for the list to be non-empty). | (post-spec) | Stub | `2026-04-26-issue-199-plan-f-session-recovery.md` |

## Rules across the plans

- Each plan ships on its own feature branch off `main`; never commit directly to `main`. (Plans A and B both did so; future plans follow suit.)
- Dependency order: A → B → D → D2; C is independent of D/D2 and can be sequenced in parallel. E depends on A's FetchRange path being live, otherwise unconstrained.
- Backward compatibility is **not** a constraint at this point in the project's life — prefer clean implementations over preserving deprecated shapes. Stale on-disk state (Plan D, D2) should fail-and-overwrite, not auto-migrate.
- Test rewrite budget (from spec): ~15–20 tests touched. Plan A absorbed most of it. Later plans should be lighter.

## Post-ship follow-ups (separate tracking, not in any of the above)

- Investigate whether cross-scrollback selection worked in a previous build but regressed (user belief). Plan C supersedes the current selection path regardless, so this is archaeology, not blocking.
- Hysteresis / deadline tuning on the PaneCache based on profiling.
- Predictive prefetch thread — explicitly deferred unless measurement shows need.

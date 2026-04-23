# Issue #199 — Plan E: Statusbar Fetch-Pending Indicator (STUB)

**Status:** Stub — write the full TDD plan after Plan A lands.
**Depends on:** Plan A merged.
**Can ship independently of B/C/D.**
**Spec reference:** "Missed-deadline handling" in sub-problem 4 + spec sequencing step 8.

## Goal

When a visible row's `globalIdx` is not yet in the PaneCache and no fetch has resolved within ~50ms, the bottom statusbar surfaces a "scrollback fetch pending" indicator. User gets feedback during fast scroll-jumps without inline placeholder text in the content area.

Parallel user goal: make the statusbar a more-used surface. Currently under-utilized.

## Scope

- Server-side: `DesktopPublisher` emits a scalar "pending count" per pane on the existing `EventDispatcher`. Incremented when a fetch stays unresolved past 50ms; decremented on resolution.
- `apps/statusbar` subscribes to the event stream, aggregates across panes, renders an unobtrusive spinner + count.
- No new protocol wire surface — statusbar reads directly from the in-process `EventDispatcher`.

## Out of scope

- Reworking the statusbar's layout or theme system beyond adding one cell group.
- Inline "loading…" text in the content area — explicitly not wanted.

## Files touched (estimated)

- `internal/runtime/server/desktop_publisher.go` — pending-fetch tracker keyed by `(paneID, requestID)` with 50ms timer.
- `apps/statusbar/statusbar.go` — subscription + render.
- Theme JSON — new color slot if desired.

## Test checklist

- Pending event fires when fetch exceeds 50ms; clears on response.
- Statusbar renders indicator only while `pendingCount > 0`.
- Aggregate count across multiple panes is correct.
- AltScreen pane never contributes to the count (it opts out of fetches).

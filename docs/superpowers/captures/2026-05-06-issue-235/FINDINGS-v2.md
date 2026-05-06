# Issue #235 — capture findings v2 (2026-05-06, with client logs)

Capture files:
- `s1.log`, `s2.log`, `s3.log`, `s4.log` — full client+server logs
  produced by `scripts/repro-issue-235.sh`.

## Single root cause for symptoms #1, #2, and #4

`texel/desktop_engine_control_mode.go::toggleZoom` updates the
desktop's internal `zoomedPane` but **never calls
`broadcastStateUpdate()`**. The Zoomed / ZoomedPaneID fields live
in `StatePayload`, which only goes out via `EventStateUpdate` →
`MsgStateUpdate`. So the client never learns the desktop has
entered zoom mode.

Evidence is unambiguous across all four captures:

| Symptom | Server zoom toggles | Client `MsgStateUpdate` transitions | Client frames with `state.zoomed=true` |
|---------|---------------------|-------------------------------------|----------------------------------------|
| s1 | 17 | 0 | 0 |
| s2 | 27 | 0 | 0 |
| s3 | 71 | 0 | 0 |
| s4 | 19 | 0 | 0 |

Pane geometry *does* propagate (via `MsgTreeSnapshot` after
`broadcastTreeChanged`) and per-pane `ZOrder` changes *do*
propagate (via `MsgPaneState`, ZOrder=1000 confirmed on the
zoomed pane in s4 logs). What's missing is the desktop-level
"we are zoomed" flag that several render decisions key off.

## How this single root cause produces three different symptoms

### Symptom #1 — htop overlays zoomed shell

`incrementalComposite`'s skip block:

```go
if state.zoomed && pane.ID != state.zoomedPane { continue }
```

never fires (LHS is always false). Every dirty pane paints into
the screen buffer, including the alt-screen pane that's
constantly dirty due to htop's render tick. Between `fullRender`
calls (triggered by keypresses), the htop pane overwrites the
zoomed shell's region. Keypresses force a `fullRender` whose
partition logic correctly puts the (high-ZOrder) zoom pane on
top — that's why pressing a key clears the overlay.

### Symptom #2 — zoomed alt-screen shows partial fill

Same root cause as #1. The user's "whole left side" observation
is `compositeInto` painting the (still-dirty) non-zoomed panes
at their split rects on top of the zoomed pane area. The earlier
PaneCache.alt-rows-stale hypothesis from FINDINGS.md is **not**
the cause — server logs already showed full 62×255 emits cleanly
crossing the transition. The client cache is correct; the
compositor just doesn't know to suppress non-zoomed panes.

### Symptom #4 — Ctrl+a help dialog renders behind zoom

The capture shows the partition assignment after zoom + dialog:

```
pane=7dd7ad38 partition=normal   zorder=0    (statusbar)
pane=3750d91f partition=normal   zorder=0    (other shell, NOT zoomed)
pane=947bfd1d partition=floating zorder=100  (help dialog)
pane=0e5f47e4 partition=floating zorder=1000 (zoomed pane)
```

The zoomed pane is in the *floating* partition because its
`ZOrder=1000` from `MsgPaneState` exceeds the
`floatingZOrder=100` threshold. Within the floating partition,
panes paint in ZOrder-ascending order, so the help dialog (100)
paints **before** the zoom pane (1000), leaving the dialog
covered. The state-propagation bug means we can't differentiate
the zoom pane in `fullRender` to keep it out of floating —
without `state.zoomed`, the partition logic only has ZOrder to
work with, and ZOrder alone says "this pane is on top of
everything".

The dropped PR #236 commit `ec76646` made this worse by
explicitly forcing the zoom pane into floating; the reset
baseline is back to "implicit via ZOrder=1000" but the same
class of bug stands.

### Symptom #3 — server-emit wedge

Did not reproduce in this capture round either. Logs show 71
toggles in s3 with no wedge. Hypothesis stands that the wedge
was caused by one of the dropped PR #236 commits.

## The fix shape

Three coordinated changes, all small:

1. **Server.** `texel/desktop_engine_control_mode.go::toggleZoom`
   calls `d.broadcastStateUpdate()` after the existing
   `broadcastActivePaneChanged` / `broadcastTreeChanged` pair.
   This delivers the missing `MsgStateUpdate` to the client.

2. **Client `incrementalComposite`.** Add the skip we already
   have:
   ```go
   if state.zoomed && pane.ID != state.zoomedPane { continue }
   ```
   This is already present from earlier instrumentation work and
   needs no further change — it just starts firing once (1) is
   in place.

3. **Client `fullRender` partition.** When `state.zoomed`, force
   the zoomed pane into **normal**, not floating, regardless of
   its `ZOrder=1000`. This keeps modal floating panels above the
   zoomed pane while still painting the zoom pane on top of
   regular split panes (it sorts last within the normal
   partition by ZOrder).

   Note this is the *inverse* of what dropped commit `ec76646`
   did. ec76646 was correct in spirit (treat zoom specially in
   fullRender) but pushed it the wrong direction.

## Open questions / follow-ups

- **Are there other zoom-state-transitioning paths that also
  forget to broadcastStateUpdate?** `SwitchToWorkspace` clears
  `d.zoomedPane = nil` (desktop_engine_core.go:665) without a
  state broadcast. If the user zooms, then switches workspace,
  the client's `state.zoomed` would (incorrectly) stay true
  until something else triggers a state broadcast. Should be
  audited.
- **Should the test for symptom #1 be a publisher-level
  integration test (server emits MsgStateUpdate after zoom) or
  a client-state test (state.zoomed flips correctly)?** Both
  catch the regression. The server-side test is more targeted.

## Out of scope

- PaneCache.alt row-shape normalization (earlier hypothesis,
  now disproven for symptom #2).
- Bumping floating modal ZOrder above 1000 (the alternative to
  the partition force in fix #3). The partition force is
  smaller and keeps the modal/zoom relationship explicit.

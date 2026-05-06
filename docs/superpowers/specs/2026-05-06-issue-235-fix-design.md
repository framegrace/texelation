# Issue #235 — fix design

**Status:** Spec
**Parent investigation:** `docs/superpowers/specs/2026-05-06-issue-235-investigation.md`
**Findings:** `docs/superpowers/captures/2026-05-06-issue-235/FINDINGS-v2.md`

## Background

Capture data with `TEXELATION_DEBUG_ZOOM_FILE` proves that
`state.zoomed` never flips to true on the client across 134
server-side zoom toggles. Root cause: `toggleZoom` (and the
adjacent zoom-state-transitioning paths) update the desktop's
internal `zoomedPane` but never broadcast `EventStateUpdate`,
so the client never receives `MsgStateUpdate` with the new
Zoomed/ZoomedPaneID fields.

This single bug produces symptoms #1, #2, and #4. Symptom #3
did not reproduce on the reset baseline; we treat it as
resolved-by-baseline-reset and watch for regressions.

## Goal

Make the client's `state.zoomed` / `state.zoomedPane` track the
desktop's actual zoom state, and ensure the renderer's two
sort/skip decisions then produce the correct visible result for
all three reproducing symptoms.

## Out of scope

- Plan B viewport semantics, alt-screen cache normalization,
  protocol changes. None are needed.
- Refactoring of the dispatch / broadcast machinery.
- The cosmetic "force zoomed pane to top in fullRender via
  state.zoomed" approach from dropped commit `ec76646`. We do
  treat zoom specially in fullRender, but in the *opposite*
  direction (out of floating, into normal).

## Fix shape

Three coordinated changes:

### F1. Server: broadcast state on every zoom-state transition

Add `d.broadcastStateUpdate()` to every code path that mutates
`d.zoomedPane`. Audit identifies four such paths in
`texel/desktop_engine_core.go` and `desktop_engine_control_mode.go`:

1. `toggleZoom` (desktop_engine_control_mode.go:50) — sets
   `d.zoomedPane = nodeToZoom` (zoom-in) or `nil` (zoom-out).
2. `desktop_engine_control_mode.go:240–243` (the
   "ControlModeOff while zoomed" branch in the control-mode
   key handler — sets `d.zoomedPane = nil`).
3. `SwitchToWorkspace` (desktop_engine_core.go:665) — clears
   `d.zoomedPane = nil` when switching away.
4. `desktop_engine_core.go:716` — another zoomedPane clear path
   (verified by grep during implementation; the spec author
   inspected the file once and may have missed minor variants
   like the cleanup in workspace teardown). The implementing
   engineer **must grep `d.zoomedPane = ` across the whole
   `texel/` tree and ensure every mutation is paired with a
   `broadcastStateUpdate()` or already runs inside a function
   that calls one.**

`shouldBroadcastState` already deduplicates against
`d.lastState` (desktop_engine_core.go:536), so adding a
broadcast at a path that didn't actually change zoom state is a
no-op — calling broadcastStateUpdate "too often" is safe.

### F2. Client: keep the incrementalComposite skip

The skip already lives in `internal/runtime/client/renderer.go`
from the investigation work:

```go
if state.zoomed && pane.ID != state.zoomedPane {
    zoomdebug.Logf(...)
    continue
}
```

No code change in this file as part of the fix; the skip starts
firing correctly once F1 delivers `MsgStateUpdate`. The
zoomdebug log line stays for the duration of the issue's life
cycle (it gets stripped during the cleanup pass once all
symptoms are verified resolved).

### F3. Client: force zoomed pane out of the floating partition in fullRender

The zoomed pane has `ZOrder=ZOrderAnimation=1000` (set by
`SetZOrder` in `toggleZoom`, propagated via `MsgPaneState`).
The current partition logic in `fullRender` sends anything with
`ZOrder >= floatingZOrder=100` to the floating bucket, putting
the zoom pane and floating modals (Ctrl+a help dialog, etc.) in
the same bucket. Within floating, ascending ZOrder sort means
zoom (1000) paints over modal (100). That's symptom #4.

Fix: when `state.zoomed && pane.ID == state.zoomedPane`, place
the pane into `normalPanes` regardless of its ZOrder. The
SortedPanes list is already in ZOrder-ascending order, so the
zoom pane (ZOrder=1000) will paint last among normals — on top
of regular split panes — while floating modals retain their
on-top-of-everything position.

```go
for _, pane := range panes {
    if pane == nil {
        continue
    }
    partition := "normal"
    if state.zoomed && pane.ID == state.zoomedPane {
        normalPanes = append(normalPanes, pane)
    } else if pane.ZOrder >= floatingZOrder {
        floatingPanes = append(floatingPanes, pane)
        partition = "floating"
    } else {
        normalPanes = append(normalPanes, pane)
    }
    zoomdebug.Logf("  pane=%x partition=%s zorder=%d", pane.ID[:4], partition, pane.ZOrder)
}
```

The zoomdebug line stays because the FINDINGS-v2 evidence
relies on it; it gets removed during cleanup along with the
other instrumentation.

## Tests

### T1. Server-side regression test

Location: `texel/desktop_engine_test.go` or a new test file
under `texel/`.

Test: build a desktop engine with two panes, capture
EventStateUpdate broadcasts via the dispatcher, call
`toggleZoom`, assert that exactly one EventStateUpdate fired
with `Zoomed=true` and `ZoomedPaneID` matching the active
pane. Then call `toggleZoom` again, assert another
EventStateUpdate fired with `Zoomed=false`.

Tooling: `texel.Dispatcher` already supports test-only event
capture; if not, the test harness in
`texel/desktop_engine_integration_test.go` shows the existing
pattern. Pick whichever fits with one or two helper lines.

### T2. Server-side regression test for SwitchToWorkspace

Same pattern: zoom a pane in workspace 1, switch to workspace
2, assert an EventStateUpdate with `Zoomed=false` was
broadcast.

### T3. Client-side regression test for fullRender partition

Location: an existing or new test in
`internal/runtime/client/renderer_test.go`.

Test: construct a `clientState` with `zoomed=true`,
`zoomedPane=X`, and a SortedPanes list containing the zoom pane
(ZOrder=1000), a regular split pane (ZOrder=0), and a help
dialog (ZOrder=100). Run the partition split logic. Assert the
zoom pane is in `normalPanes` and the help dialog is in
`floatingPanes`.

If the partition logic isn't easily extractable (currently
inline in fullRender), refactor it into a small private helper
`partitionPanes(panes, state)` returning two slices. The
helper is purely structural; it makes the test possible
without spinning up a full screen.

## Cleanup

After the fix lands and is verified:

- Strip all zoomdebug `Logf` call sites added during the
  investigation (in `incrementalComposite`, `fullRender`,
  `MsgStateUpdate`, `publishSnapshotsLocked`,
  `recalculateLayout`).
- Delete the `internal/runtime/zoomdebug` package and its
  tests.
- Remove `Init` calls from the three entry points.
- Remove `scripts/repro-issue-235.sh`.
- Keep T1, T2, T3 as permanent regression tests.

The cleanup happens as a final separate commit on this same
branch so the diff is reviewable as "remove all #235
instrumentation" in isolation.

## Acceptance

- All four repros from `docs/issue-235-repros.md`:
  - **s1** (htop overlay): no overlay during the wait window;
    keypresses no longer required to clear non-existent
    overlay.
  - **s2** (zoomed alt-screen): full pane content fills the
    zoom rect on the next frame after toggle, with no
    other-pane content bleeding in.
  - **s3** (server-emit wedge): does not reproduce (already
    the case on the reset baseline).
  - **s4** (modal behind zoom): the Ctrl+a help dialog
    renders on top of the zoomed pane.
- `make test` passes including T1, T2, T3.
- The zoomdebug instrumentation is removed in a final commit;
  no `[zoom-debug]` strings remain in the merged tree.
- Issue #235 closes as fixed.

# Issue #235 — Zoom regression: investigation plan

**Status:** Spec — investigation-driven (Approach A from brainstorm 2026-05-06)
**Branch state at spec time:** `fix/issue-235-zoom-regression`, PR #236 open with five commits whose effects don't match the issue analysis.

## Background

PR #236 attempted a hypothesis-driven fix of the symptoms in issue #235 but
empirically does not fix them — the user still observes htop overlaying a
zoomed shell pane between keystrokes on a build of this branch. Code-level
review of the PR also surfaces a self-inflicted regression introduced by one
of its commits (modal Ctrl+a help dialog now renders behind the zoom pane).
We are switching to a repro-first, evidence-driven approach: confirm what
actually happens at run time, then write minimal targeted fixes whose tests
are derived from the captured signatures.

## Symptoms in scope

| # | Symptom | Repro |
|---|---------|-------|
| 1 | Non-zoomed pane updates flicker over the zoomed view; most visible when an htop / vim / kitty viewer is unzoomed and a shell is zoomed. Keypress in the zoomed pane (which forces a `fullRender`) clears the overlay; it returns on the next intermediate composite. | 2-pane split, htop left, shell right, zoom shell, observe between keystrokes. |
| 2 | Alt-screen apps (htop, vim) that are themselves zoomed render only the top-left quadrant of the zoom area; the rest stays stale or default-filled. | 2-pane split, run htop in one pane, zoom that pane. |
| 3 | After 3–10 zoom toggle cycles, the server appears to stop emitting visible zoom updates. Recovery requires `texelation --stop && texelation`. | Toggle Ctrl+a z repeatedly. |
| 4 | When a pane is zoomed, the Ctrl+a control-mode help dialog (a floating modal) renders **behind** the zoom pane. | Zoom any pane; press Ctrl+a; observe missing dialog. |

Symptom 4 is included because it is almost certainly a self-inflicted
regression from PR #236 commit `ec76646` ("Force zoomed pane to top in
fullRender via state.zoomed"), which puts the zoom pane into the same
floating partition as modal panels and lets it compete by ZOrder/stable-order
against the dialog. Reverting that commit should fix #4 outright; we keep #4
in the spec because confirming the revert fixes it is itself diagnostic
evidence that informs the rest of the work.

## Out of scope

- Refactoring the publisher / render layering. Fix bugs only.
- The "keep statusbar visible while zoomed" change (`0a0d8bc`). The user
  has confirmed that work was a self-inflicted patch over a defect this PR
  introduced; it has no reason to exist on `main` and is dropped along with
  the rest of the PR.
- Any change to Plan B viewport semantics beyond what is required to stop
  the symptom #3 wedge.

## Phase 0 — Drop PR #236's existing commits

Before any investigation work, reset the working branch to `main` so the
investigation runs against a clean baseline. The five commits on
`fix/issue-235-zoom-regression` are not retained as-is:

- `8c3e5fb` (incrementalComposite skip) — likely close to correct but the
  user reports it does not fix the visible bug. Re-derive after evidence.
- `aee1355` (publisher prev dim-mismatch invalidation) — a no-op for the
  symptom it claims to fix; `rowsEqual` already handles length mismatches.
- `2083d46` (skip publish when no client attached) — unrelated to the
  reproducible symptoms; if useful, file separately.
- `ec76646` (force zoomed pane to floating partition) — caused symptom #4.
- `0a0d8bc` (keep statusbar visible while zoomed) — patch over a self-
  inflicted regression.

Tactically: leave the existing PR open with a comment linking to this spec
explaining the reset, or close it and reopen a fresh PR after Phase 4. The
choice is editorial; the code action is the same.

## Phase 1 — Establish reliable repros

Land a `docs/issue-235-repros.md` (or extend this spec inline) documenting
exact steps for each symptom, including pane sizes, apps, and key sequences.
Goal: each engineer who picks up the work can reproduce in under 30 seconds.

For symptom #3 specifically, document the cycle count at which the wedge
first appeared in your local environment; this is a load-bearing piece of
evidence for whether the wedge is timing-dependent or deterministic after N.

## Phase 2 — Ephemeral instrumentation

All instrumentation is gated behind a single env var (`TEXELATION_DEBUG_ZOOM=1`)
and is removed before the final fix PRs merge. The goal is to capture the
exact runtime state at the moment the bug is visible, without reasoning
backward from code alone.

### Client-side

- **`internal/runtime/client/renderer.go::incrementalComposite`** — at
  function entry, log `state.zoomed`, `state.zoomedPane`, and a per-pane
  summary: `{paneID, dirty, animated, painted_or_skipped, reason}`.
- **`internal/runtime/client/renderer.go::fullRender`** — same fields,
  including which partition (normal vs floating) each pane fell into.
- **`internal/runtime/client/protocol_handler.go`** — at the `MsgStateUpdate`
  branch, log every flip of `Zoomed` / `ZoomedPaneID` with timestamp.

### Server-side

- **`internal/runtime/server/desktop_publisher.go::publishSnapshotsLocked`** —
  per pane: ID, snap.Rect, len(snap.Buffer), len(snap.Buffer[0]), AltScreen
  flag, `prev` shape, viewport `{Rows, Cols, ViewTopIdx, ViewBottomIdx, AutoFollow}`,
  `shouldInvalidate` outcome, whether the delta was emitted, and `len(delta.Rows)` /
  `len(delta.DecorRows)`.
- **`texel/desktop_status.go::recalculateLayout`** — when `zoomedPane != nil`,
  log the pane's pre/post `absX0,absY0,absX1,absY1`.

The publisher already has a `publisherDebug` env-gated logger; add a parallel
`zoomDebug` rather than overloading it, so we can keep zoom traces narrow.

## Phase 3 — Capture and diagnose

Run each repro with instrumentation enabled. The decision tree below
identifies which fix to write based on the captured logs.

### Symptom #1 (non-zoomed overlay)

Capture across the buggy window. Three possible signatures:

1. **`state.zoomed = false` at the offending frames.** Then the kept
   `incrementalComposite` skip cannot fire because its guard never matches.
   Bug is in client state propagation: `MsgStateUpdate` is delayed,
   coalesced, or dropped. Fix at the protocol/state layer.
2. **`state.zoomed = true` and the htop pane appears in the painted set.**
   The skip's condition is broken (e.g., `state.zoomedPane` mismatched).
   Fix at the conditional.
3. **`state.zoomed = true` and the htop pane is correctly skipped, but htop
   pixels still appear on the terminal screen.** Then a third path is
   writing into the screen buffer (a render loop, animation tick, or tcell
   sync from a stale `prevBuffer`). Trace from `screen.Show` backward.

### Symptom #2 (alt-screen quadrant)

Capture `snap.Rect`, `len(snap.Buffer)`, `len(snap.Buffer[0])` across the
zoom transition. Two possible signatures:

1. **`snap.Buffer` dims match `snap.Rect` after zoom.** The server is
   emitting full-size buffers. The bug is client-side: `PaneCache.alt`
   isn't growing (or shrinking) in step with the pane rect. Trace
   `ApplyDelta` and the client renderer's alt-screen draw.
2. **`snap.Buffer` dims stay at the pre-zoom size while `snap.Rect` grows.**
   The app's Resize is asynchronous or partial. Trace `app.Resize` in
   texelterm specifically when in alt-screen mode; verify whether Resize
   posts to the app's event loop and is processed before the next snapshot.

### Symptom #3 (server stops emitting)

Capture publisher logs across the cycle that triggers the wedge. Expected
signature: a frame where `len(delta.Rows) == 0` and `len(delta.DecorRows) == 0`
despite content actively changing. Cross-reference with `prev` shape and
viewport `{Rows, Cols}` to identify which gate is silencing the emit.

Leading hypothesis (to confirm or reject from logs): `lastViewport[paneID]`
holds a stale `{Rows, Cols}` from before the most recent zoom transition,
because the client never sent a `ViewportUpdate` after the geometry change.
If confirmed, fix by either (a) broadcasting a publisher-side state-reset on
`recalculateLayout` for any pane whose Rect changed, or (b) making the
client send a `ViewportUpdate` on tree-snapshot rect changes.

### Symptom #4 (modal behind zoom)

Reverting `ec76646` should make this symptom disappear immediately. If it
does not, the bug is older than the PR and we trace from there. The
expected fix path is to keep the zoom pane in the *normal* partition and
trust the existing `ZOrder=ZOrderAnimation` to put it on top of siblings,
while modal floating panels remain in the floating partition strictly above
everything.

## Phase 4 — Land fixes

One PR per confirmed root cause. Each PR:

- Strips its instrumentation before the final commit.
- Adds a regression test whose assertion comes from the captured log
  signature (e.g., for #3: a publisher-level test asserting that after a
  geometry change `lastViewport` has been invalidated, with the test
  recreating the exact viewport+resize sequence the logs revealed).
- Stays narrow — no opportunistic refactoring.

If two symptoms share a single root cause, they may share a PR; default to
splitting unless the diff would be redundant.

## Risks

- **Instrumentation overhead.** Per-frame logging at 60 fps can drown the
  bug. Mitigation: gate on `TEXELATION_DEBUG_ZOOM=1`, default off; for the
  symptom #3 wedge specifically, log only when `delta.Rows + delta.DecorRows`
  is empty.
- **Race-dependent symptoms.** If the wedge is timing-dependent, log
  capture may not reproduce it. Mitigation: capture multiple runs; if
  truly non-deterministic, use a deterministic reproduction harness (the
  existing `memconn` test infrastructure for client/server testing) to
  drive the exact sequence in a test rather than chasing a live repro.
- **Spec drift across multiple PRs.** Each PR should reference this spec
  and tick off which symptom(s) it closes; final PR closes the issue.

## Acceptance

Issue #235 is closed when:

- All four symptoms are not reproducible against `main`.
- Each fix has a regression test grounded in observed runtime behavior.
- No instrumentation remains in merged code.
- PR #236 is closed (either superseded by new PRs or rebased to a clean
  baseline before the new fixes land).

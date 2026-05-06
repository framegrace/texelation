# Issue #235 Investigation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reset PR #236 to a clean baseline, add env-gated instrumentation across the zoom render path, document the four repros from issue #235, and capture runtime evidence that will drive minimal targeted fixes in follow-up plans.

**Architecture:** Deterministic Phases 0–2 (reset + instrumentation + repro doc) followed by a structured Phase 3 capture protocol. Phase 4 (the actual fixes) is intentionally not planned here — its tasks depend on what the logs reveal and will be specced in follow-up plans grounded in the captured evidence.

**Tech Stack:** Go 1.24.3, tcell, the existing texelation client/server runtime. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-05-06-issue-235-investigation.md`

---

## File Structure

| Path | Status | Responsibility |
|------|--------|----------------|
| `internal/runtime/zoomdebug/zoomdebug.go` | Create | Env-gated (`TEXELATION_DEBUG_ZOOM=1`) logger with `[zoom-debug]` prefix. Single import point so the eventual strip-out is `git rm` + reverting the call sites. |
| `internal/runtime/client/renderer.go` | Modify | Add `zoomdebug` calls at the top of `incrementalComposite` and `fullRender` plus per-pane skip/paint summaries. |
| `internal/runtime/client/protocol_handler.go` | Modify | Add `zoomdebug` log on every `MsgStateUpdate` with the previous and new `Zoomed` / `ZoomedPaneID`. |
| `internal/runtime/server/desktop_publisher.go` | Modify | Add `zoomdebug` block per pane inside `publishSnapshotsLocked`. |
| `texel/desktop_status.go` | Modify | Add `zoomdebug` log inside `recalculateLayout` when `d.zoomedPane != nil`. |
| `docs/issue-235-repros.md` | Create | Concrete repro steps, expected observation, and a "captured logs" appendix template. |
| `docs/superpowers/captures/2026-05-06-issue-235/` | Create | Directory holding raw log captures, one file per symptom run. |

The instrumentation lives behind a single env var so default-off builds are unaffected. All instrumentation in this plan is removed before the eventual fix PRs merge — the follow-up plans must include a "strip instrumentation" task.

---

## Task 0: Reset working branch to a clean baseline

**Files:**
- Modify: working tree only — no source files touched in this task

The current branch carries five commits whose effects don't match the issue analysis. Per spec Phase 0 we drop those commits and rebuild from a clean baseline that contains only the spec.

- [ ] **Step 1: Confirm current branch state**

Run: `git log --oneline main..HEAD`

Expected: six commits, oldest five from PR #236 (`8c3e5fb`, `aee1355`, `2083d46`, `ec76646`, `0a0d8bc`) and the most recent (`0d4e502`) being the investigation spec.

- [ ] **Step 2: Snapshot the spec commit hash**

Run: `git log --format=%H -1 -- docs/superpowers/specs/2026-05-06-issue-235-investigation.md`

Record the hash (expected: `0d4e502...`); the next step will cherry-pick it.

- [ ] **Step 3: Reset branch to main and re-apply the spec commit**

```bash
git reset --hard origin/main
git cherry-pick 0d4e502
```

Expected: working tree clean, branch tip is the spec commit, `git log --oneline main..HEAD` shows exactly one commit.

- [ ] **Step 4: Do not force-push**

Leave the remote PR branch alone for now. Force-pushing to PR #236's remote branch is destructive and requires explicit user direction. The user will decide later whether to close PR #236 and open a fresh one or force-push the rewritten history. Note this in the eventual handoff.

---

## Task 1: Add the env-gated zoomdebug helper

**Files:**
- Create: `internal/runtime/zoomdebug/zoomdebug.go`

This is a 20-line shim. We give it its own package so call sites are grep-able (`zoomdebug.Logf`) for the eventual strip-out.

- [ ] **Step 1: Create the package**

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/zoomdebug/zoomdebug.go
// Summary: Env-gated logger for issue #235 investigation. Removed
// once the underlying bugs are fixed and their regression tests
// are in place.
//
// Usage:
//   zoomdebug.Logf("incrementalComposite: zoomed=%v zoomPane=%x panes=%d",
//       state.zoomed, state.zoomedPane[:4], len(panes))
//
// Gate: set TEXELATION_DEBUG_ZOOM=1 to enable. Default-off builds
// pay only an os.Getenv at process start plus a boolean check per
// call site.

package zoomdebug

import (
	"log"
	"os"
)

var enabled = os.Getenv("TEXELATION_DEBUG_ZOOM") == "1"

// Enabled reports whether zoom-debug logging is active. Call sites
// can use this to skip expensive formatting when disabled.
func Enabled() bool { return enabled }

// Logf writes a [zoom-debug] prefixed log line via the standard
// logger when the env gate is set. No-op otherwise.
func Logf(format string, args ...interface{}) {
	if !enabled {
		return
	}
	log.Printf("[zoom-debug] "+format, args...)
}
```

- [ ] **Step 2: Verify the package builds**

Run: `go build ./internal/runtime/zoomdebug/...`

Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/zoomdebug/zoomdebug.go
git commit -m "Add env-gated zoomdebug helper for issue #235 investigation"
```

---

## Task 2: Instrument client `incrementalComposite`

**Files:**
- Modify: `internal/runtime/client/renderer.go` (around the existing `incrementalComposite` definition near line 272)

The decision tree in the spec (Phase 3, symptom #1) needs three things from this function: state.zoomed at function entry, state.zoomedPane, and a per-pane "painted vs skipped vs reason" summary.

- [ ] **Step 1: Add the import**

Locate the existing import block in `internal/runtime/client/renderer.go` and add:

```go
"github.com/framegrace/texelation/internal/runtime/zoomdebug"
```

(Keep import grouping consistent with other internal imports already present.)

- [ ] **Step 2: Log at function entry**

Find `func incrementalComposite(state *clientState, screenW, screenH int) bool {`. Immediately after the function header (before any panes loop), add:

```go
zoomdebug.Logf("incrementalComposite: zoomed=%v zoomPane=%x screen=%dx%d",
	state.zoomed, state.zoomedPane[:4], screenW, screenH)
```

- [ ] **Step 3: Log per-pane decisions**

The current loop already has the symptom-#1 fix candidate skip block (`if state.zoomed && pane.ID != state.zoomedPane { continue }`). Replace that skip with an instrumented version that records whether the skip fired, whether the pane was dirty/animated, and whether it ended up painted:

```go
if state.zoomed && pane.ID != state.zoomedPane {
	zoomdebug.Logf("  pane=%x decision=skip reason=non-zoomed dirty=%v animated=%v",
		pane.ID[:4], pane.Dirty, pane.HasAnimated)
	continue
}
if !pane.Dirty && !pane.HasAnimated {
	zoomdebug.Logf("  pane=%x decision=skip reason=clean", pane.ID[:4])
	continue
}
zoomdebug.Logf("  pane=%x decision=paint dirty=%v animated=%v zorder=%d",
	pane.ID[:4], pane.Dirty, pane.HasAnimated, pane.ZOrder)
```

If `incrementalComposite` does not currently contain the symptom-#1 skip (because Task 0 reset the branch and dropped that commit), insert the instrumented skip above as the first decision check inside the panes loop, before the `if !pane.Dirty && !pane.HasAnimated` guard. The skip must remain in place — we are reintroducing it under instrumentation so we can confirm whether it is the correct fix.

- [ ] **Step 4: Build**

Run: `go build ./...`

Expected: no output, exit 0.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/client/renderer.go
git commit -m "Instrument incrementalComposite for issue #235 investigation"
```

---

## Task 3: Instrument client `fullRender`

**Files:**
- Modify: `internal/runtime/client/renderer.go` (around the existing `fullRender` definition near line 435)

We need fullRender's view of state.zoomed and how it partitions panes (normal vs floating) — that's what reveals whether the modal-behind-zoom symptom (#4) is partition-driven.

- [ ] **Step 1: Log at function entry**

At the top of `fullRender`, add:

```go
zoomdebug.Logf("fullRender: zoomed=%v zoomPane=%x panes=%d",
	state.zoomed, state.zoomedPane[:4], len(panes))
```

- [ ] **Step 2: Log per-pane partition decisions**

Inside the partition loop (the one that builds `normalPanes` / `floatingPanes`), replace the existing partition decision with an instrumented version:

```go
for _, pane := range panes {
	if pane == nil {
		continue
	}
	partition := "normal"
	if pane.ZOrder >= floatingZOrder {
		floatingPanes = append(floatingPanes, pane)
		partition = "floating"
	} else {
		normalPanes = append(normalPanes, pane)
	}
	zoomdebug.Logf("  pane=%x partition=%s zorder=%d", pane.ID[:4], partition, pane.ZOrder)
}
```

If the existing partition loop also forces zoomed-pane-to-floating (the `ec76646` change Task 0 dropped), do not reintroduce that block — symptom #4 strongly suggests it is harmful. The reset baseline relies on `ZOrder=ZOrderAnimation` to put the zoom pane on top of normal siblings, which is what we want to validate.

- [ ] **Step 3: Build**

Run: `go build ./...`

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/client/renderer.go
git commit -m "Instrument fullRender partition decisions for issue #235 investigation"
```

---

## Task 4: Instrument client `MsgStateUpdate` handling

**Files:**
- Modify: `internal/runtime/client/protocol_handler.go` (around the `case protocol.MsgStateUpdate:` branch near line 232)
- Modify: `internal/runtime/client/client_state.go` (the existing site that updates `s.zoomed` / `s.zoomedPane`)

We need to confirm whether `state.zoomed` is actually flipping when the user toggles zoom, and at what timestamp relative to render frames.

- [ ] **Step 1: Add the import to client_state.go**

Add to the import block:

```go
"github.com/framegrace/texelation/internal/runtime/zoomdebug"
```

- [ ] **Step 2: Log the flip in client_state.go**

Find the existing block (around line 470–478) that sets `s.zoomed` / `s.zoomedPane`. Wrap the assignment with before/after logging:

```go
prevZoomed := s.zoomed
prevPane := s.zoomedPane
if update.Zoomed {
	s.zoomed = true
	s.zoomedPane = update.ZoomedPaneID
} else {
	s.zoomed = false
	s.zoomedPane = [16]byte{}
}
if prevZoomed != s.zoomed || prevPane != s.zoomedPane {
	zoomdebug.Logf("MsgStateUpdate: zoomed %v->%v pane %x->%x",
		prevZoomed, s.zoomed, prevPane[:4], s.zoomedPane[:4])
}
```

If the existing code shape differs (e.g., uses different field names or a different conditional), preserve the existing logic unchanged and add the before/after capture around it. The exact assignment shape is not important — the diagnostic value is the log line on every observed transition.

- [ ] **Step 3: Build**

Run: `go build ./...`

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/client/client_state.go
git commit -m "Log zoom state transitions for issue #235 investigation"
```

---

## Task 5: Instrument server `publishSnapshotsLocked`

**Files:**
- Modify: `internal/runtime/server/desktop_publisher.go` (around `publishSnapshotsLocked` near line 170)

This is the heart of the symptom #3 wedge diagnosis. We need per-pane visibility into snapshot dims, prev shape, viewport, and the should-invalidate decision.

- [ ] **Step 1: Add the import**

Add to the import block:

```go
"github.com/framegrace/texelation/internal/runtime/zoomdebug"
```

- [ ] **Step 2: Log per-pane diagnostic data inside the snapshots loop**

Inside `publishSnapshotsLocked`, at the top of `for _, snap := range buffers {`, add the entry log:

```go
prevShapeRows := 0
prevShapeCols := 0
if prev := p.prevBuffers[snap.ID]; prev != nil {
	prevShapeRows = len(prev)
	if len(prev) > 0 {
		prevShapeCols = len(prev[0])
	}
}
snapRows := len(snap.Buffer)
snapCols := 0
if snapRows > 0 {
	snapCols = len(snap.Buffer[0])
}
zoomdebug.Logf("publish: pane=%x rect=%dx%d snap=%dx%d prev=%dx%d alt=%v",
	snap.ID[:4], snap.Rect.Width, snap.Rect.Height,
	snapRows, snapCols, prevShapeRows, prevShapeCols, snap.AltScreen)
```

Place this immediately after the `for _, snap := range buffers {` line, before the `vp, haveVP := p.session.Viewport(snap.ID)` lookup.

- [ ] **Step 3: Log viewport and shouldInvalidate**

After the existing `if haveVP { ... }` block (the one ending at `p.lastViewport[snap.ID] = vp`), add:

```go
zoomdebug.Logf("  pane=%x haveVP=%v vpRows=%d vpCols=%d autoFollow=%v",
	snap.ID[:4], haveVP, vp.Rows, vp.Cols, vp.AutoFollow)
```

If the reset baseline does not contain the `bufferDimsDiffer` invalidation block (the `aee1355` change Task 0 dropped), do not reintroduce it. We want to observe natural baseline behavior; if the logs reveal a real wedge, the follow-up plan reintroduces a justified invalidation with a regression test.

- [ ] **Step 4: Log the emit decision**

After the `delta := bufferToDelta(snap, prev, rev, vp)` line and before `if len(delta.Rows) == 0 && len(delta.DecorRows) == 0 { continue }`, add:

```go
zoomdebug.Logf("  pane=%x emit rows=%d decor=%d revision=%d",
	snap.ID[:4], len(delta.Rows), len(delta.DecorRows), rev)
```

- [ ] **Step 5: Build and run tests**

Run: `go build ./... && go test ./internal/runtime/server/... -count=1 -run TestPublisher`

Expected: builds; the small subset of publisher tests pass. Tests that reference `bufferDimsDiffer` or assert prev-shape transitions should not exist on the reset baseline.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/server/desktop_publisher.go
git commit -m "Instrument publishSnapshotsLocked for issue #235 investigation"
```

---

## Task 6: Instrument `recalculateLayout` zoom branch

**Files:**
- Modify: `texel/desktop_status.go` (around `recalculateLayout` near line 110)

We need to see whether `setDimensions` is being called with the expected fullscreen rect across a zoom toggle, and whether `zoomedPane` is non-nil when we expect it to be.

- [ ] **Step 1: Add the import**

Add to the import block:

```go
"github.com/framegrace/texelation/internal/runtime/zoomdebug"
```

- [ ] **Step 2: Log the zoom branch**

Replace the existing block:

```go
// Override zoomed pane to fill the full main area.
if d.zoomedPane != nil && d.zoomedPane.Pane != nil {
	d.zoomedPane.Pane.setDimensions(mainX, mainY, mainX+mainW, mainY+mainH)
}
```

with:

```go
if d.zoomedPane != nil && d.zoomedPane.Pane != nil {
	zoomdebug.Logf("recalculateLayout: zoom paneID=%x rect=(%d,%d,%d,%d)",
		d.zoomedPane.Pane.ID()[:4], mainX, mainY, mainX+mainW, mainY+mainH)
	d.zoomedPane.Pane.setDimensions(mainX, mainY, mainX+mainW, mainY+mainH)
} else {
	zoomdebug.Logf("recalculateLayout: no zoom pane")
}
```

- [ ] **Step 3: Build**

Run: `go build ./...`

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add texel/desktop_status.go
git commit -m "Instrument recalculateLayout zoom branch for issue #235 investigation"
```

---

## Task 7: Build the full binary and verify all tests still pass

**Files:**
- None modified

Sanity check before we go run the repros.

- [ ] **Step 1: Full build**

Run: `make build`

Expected: builds `texel-server`, `texel-client`, `texelation`, etc. without errors.

- [ ] **Step 2: Full test suite**

Run: `make test`

Expected: all tests pass. If any zoom-related test was previously passing on the PR-#236 baseline because of a fix this plan reverted, expect it to fail or be missing — capture the failure list and add it to the captures directory (Task 9 covers that file structure).

- [ ] **Step 3: Quick smoke run**

Run with the gate off to confirm zero output:

```bash
./bin/texelation --status
```

Expected: status line, no `[zoom-debug]` output.

Then run with the gate on against `texelation` server logs:

```bash
TEXELATION_DEBUG_ZOOM=1 ./bin/texel-server --help
```

Expected: usage; the gate has no effect on `--help`, but confirms the binary built with the env reading code present.

---

## Task 8: Author the repros document

**Files:**
- Create: `docs/issue-235-repros.md`

This is the runbook for Phase 3. An engineer (or you, on the next session) should be able to run any of the four symptoms in under 30 seconds and know exactly what to capture.

- [ ] **Step 1: Write the file**

```markdown
# Issue #235 — Reproduction Steps

Each repro assumes a clean local build of `texelation` with zoom-debug
instrumentation in place (see plan
`docs/superpowers/plans/2026-05-06-issue-235-investigation.md`, Tasks 1–6).

Run the server with the gate enabled:

```bash
make build
TEXELATION_DEBUG_ZOOM=1 ./bin/texelation 2> /tmp/zoom-debug-<symptom>.log
```

Where `<symptom>` is one of `s1`, `s2`, `s3`, `s4` matching the runs below.

After each capture, copy the log to
`docs/superpowers/captures/2026-05-06-issue-235/<symptom>.log` and
record observations inline at the top of the file (what you saw, exact
keys pressed, approximate timing).

## Symptom 1 — Non-zoomed alt-screen overlays zoomed shell

1. Open texelation with `TEXELATION_DEBUG_ZOOM=1`.
2. Split horizontally: `Ctrl+a v`.
3. In the left pane run `htop`; leave it idle.
4. Move focus to the right pane (`Ctrl+a` then arrow). It is a normal shell.
5. Zoom the shell: `Ctrl+a z`.
6. Wait 5–10 seconds without typing.
7. Observe: htop content periodically appears over the zoomed shell area.
8. Press a single key in the shell.
9. Observe: htop content disappears; the zoomed shell repaints fully.
10. Wait again; the overlay returns.

Capture window: from the zoom keystroke through three full overlay-then-clear cycles.

## Symptom 2 — Zoomed alt-screen renders only top-left quadrant

1. Open texelation with `TEXELATION_DEBUG_ZOOM=1`.
2. Split horizontally: `Ctrl+a v`.
3. In the left pane run `htop`.
4. With focus on the htop pane, zoom: `Ctrl+a z`.
5. Observe: the htop content fills only the top-left region (approx the
   pre-zoom split rect); the rest of the screen shows stale or default
   cells.
6. Unzoom (`Ctrl+a z`); confirm htop redraws correctly in the split rect.

Capture window: from one zoom keystroke through one unzoom keystroke.

## Symptom 3 — Server stops emitting visible zoom updates

1. Open texelation with `TEXELATION_DEBUG_ZOOM=1`.
2. Split horizontally: `Ctrl+a v`.
3. Run `htop` in either pane.
4. Toggle zoom: `Ctrl+a z`. Wait 1 second. Toggle again. Repeat 5–10 times.
5. Observe: at some cycle count, the screen stops updating in response to
   `Ctrl+a z`. State pane still responds (control-mode toggle works) but
   the zoom no longer takes visible effect.
6. Recovery: `texelation --stop && texelation`.

Capture window: from the first zoom toggle through the wedge.

Record the exact cycle count at which the wedge appeared. If the cycle
count varies across runs (timing-dependent) capture three runs and note
the range.

## Symptom 4 — Ctrl+a help dialog renders behind zoom

1. Open texelation with `TEXELATION_DEBUG_ZOOM=1`.
2. Zoom any pane: `Ctrl+a z`.
3. Press `Ctrl+a` (enter control mode).
4. Observe: the help dialog modal appears partially or fully hidden behind
   the zoomed pane.

Expected outcome on the reset baseline: this symptom should NOT reproduce,
because the offending commit (`ec76646`, "Force zoomed pane to top in
fullRender via state.zoomed") was dropped in plan Task 0. Capture the
fullRender log lines for one Ctrl+a press to confirm the dialog is in the
floating partition and the zoom pane is in the normal partition.

If symptom #4 still reproduces on the reset baseline, the bug is older than
PR #236 and the investigation widens — note that finding in the capture
file before moving on.
```

- [ ] **Step 2: Commit**

```bash
git add docs/issue-235-repros.md
git commit -m "Add issue #235 reproduction runbook"
```

---

## Task 9: Create the captures directory with templates

**Files:**
- Create: `docs/superpowers/captures/2026-05-06-issue-235/README.md`

A short pointer file that establishes the directory and tells the next session what to put here.

- [ ] **Step 1: Write the README**

```markdown
# Issue #235 — Capture Logs

Logs collected while running the four repros from
`docs/issue-235-repros.md` against a build that has the instrumentation
from `docs/superpowers/plans/2026-05-06-issue-235-investigation.md`
(Tasks 1–6).

One file per symptom run:

- `s1.log` — Symptom 1, non-zoomed alt-screen overlays zoomed shell.
- `s2.log` — Symptom 2, zoomed alt-screen quadrant rendering.
- `s3.log` — Symptom 3, server-emit wedge after N cycles.
- `s4.log` — Symptom 4, modal dialog behind zoom (expected: not
  reproducible on reset baseline).

Each file should begin with a header block documenting:

- Date of capture.
- Build commit hash (`git rev-parse HEAD` at capture time).
- Exact key sequence pressed.
- Wall-clock observation (what the user saw on screen, with timestamps
  if possible).
- Cycle count for symptom 3 (if applicable).

Raw log lines follow the header. Do not edit log lines — paste them
verbatim from the redirected stderr.
```

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/captures/2026-05-06-issue-235/README.md
git commit -m "Add issue #235 captures directory"
```

---

## Task 10: Run the four repros and capture logs

**Files:**
- Create: `docs/superpowers/captures/2026-05-06-issue-235/s1.log`
- Create: `docs/superpowers/captures/2026-05-06-issue-235/s2.log`
- Create: `docs/superpowers/captures/2026-05-06-issue-235/s3.log`
- Create: `docs/superpowers/captures/2026-05-06-issue-235/s4.log`

This task is interactive — it requires a real terminal session, not just file edits. The agent executing this plan should pause here and request the user run the repros, or escalate that the remaining tasks need a human-in-the-loop run. If the agent is the user, follow `docs/issue-235-repros.md` exactly.

- [ ] **Step 1: Capture symptom 1**

Follow `docs/issue-235-repros.md` § "Symptom 1". Save the redirected stderr to `/tmp/zoom-debug-s1.log`, then prepend the header described in the captures README and copy the result to `docs/superpowers/captures/2026-05-06-issue-235/s1.log`.

- [ ] **Step 2: Capture symptom 2**

Same procedure for symptom 2 → `s2.log`.

- [ ] **Step 3: Capture symptom 3**

Same procedure for symptom 3 → `s3.log`. Record the cycle count at which the wedge first appeared in the header.

- [ ] **Step 4: Capture symptom 4**

Same procedure for symptom 4 → `s4.log`. If the dialog renders correctly on the reset baseline, record that explicitly in the header (`Outcome: dialog visible on top, symptom #4 not reproduced — confirms ec76646 was the cause`).

- [ ] **Step 5: Commit captures**

```bash
git add docs/superpowers/captures/2026-05-06-issue-235/
git commit -m "Capture issue #235 baseline runtime logs"
```

---

## Task 11: Diagnose and write follow-up specs

**Files:**
- Create: `docs/superpowers/specs/2026-05-06-issue-235-symptom-1-fix-design.md` (and one per confirmed root cause)

This task is the handoff. Each capture file matches one of the decision-tree branches in spec §Phase 3. For each symptom that still reproduces, write a focused fix spec citing the exact log lines that prove the root cause.

- [ ] **Step 1: Walk the symptom-1 decision tree**

Open `s1.log`. Find the frames during the overlay window. For each frame, identify which of the three signatures matches:

1. `state.zoomed=false` at incrementalComposite — protocol/state propagation bug.
2. `state.zoomed=true` and the htop pane appears in the painted set — skip condition broken.
3. `state.zoomed=true` and the htop pane is correctly skipped, but the user still sees htop on screen — third path, trace from `screen.Show` backward.

Write a focused fix design spec at
`docs/superpowers/specs/2026-05-06-issue-235-symptom-1-fix-design.md`
citing the matching log lines.

- [ ] **Step 2: Walk the symptom-2 decision tree**

Open `s2.log`. Find the post-zoom frames. Determine whether `snap.Buffer` dims match `snap.Rect`:

- If yes — client-side cache bug; trace `PaneCache.alt` and the alt-screen draw path.
- If no — app `Resize` is async/partial; trace texelterm.

Write `2026-05-06-issue-235-symptom-2-fix-design.md`.

- [ ] **Step 3: Walk the symptom-3 decision tree**

Open `s3.log`. Find the wedge frame (`emit rows=0 decor=0` despite content changing). Inspect the `prev=...` and `vpRows=... vpCols=...` values across cycles to identify which gate is silencing the emit.

Write `2026-05-06-issue-235-symptom-3-fix-design.md`.

- [ ] **Step 4: Confirm symptom 4 status**

If `s4.log` shows the dialog rendering correctly, no spec is needed — the fix is "do not reintroduce ec76646", already enforced by Task 0. Document this conclusion in the closing section of the parent investigation spec
(`docs/superpowers/specs/2026-05-06-issue-235-investigation.md`).

If symptom 4 reproduced anyway, write `2026-05-06-issue-235-symptom-4-fix-design.md` with the fullRender partition trace.

- [ ] **Step 5: Stop here**

Each new symptom spec re-enters the brainstorming → writing-plans flow on its own. The fix PRs themselves are not part of this investigation plan — they will be one plan per spec, each with a strip-instrumentation task at the end.

Do not strip the instrumentation in this plan. The follow-up fix plans rely on it for their own regression captures, and stripping prematurely would force re-instrumentation.

---

## Self-Review

**Spec coverage check:**
- Phase 0 (drop PR commits) → Task 0. ✓
- Phase 1 (establish reliable repros) → Task 8 (`docs/issue-235-repros.md`). ✓
- Phase 2 (ephemeral instrumentation) — all five instrumentation points:
  - Client incrementalComposite → Task 2. ✓
  - Client fullRender → Task 3. ✓
  - Client MsgStateUpdate → Task 4. ✓
  - Server publishSnapshotsLocked → Task 5. ✓
  - recalculateLayout zoom branch → Task 6. ✓
  - zoomdebug helper package → Task 1. ✓
- Phase 3 (capture and diagnose) → Tasks 9, 10, 11. ✓
- Phase 4 (land fixes) is intentionally out-of-plan; Task 11 hands off to follow-up specs as the spec dictates. ✓

**Placeholder scan:** no TBDs, no "implement later", every code block contains the actual content. The interactive Task 10 is a known exception — it requires a human at a terminal — and is flagged as such.

**Type/symbol consistency:** the instrumentation calls use `zoomdebug.Logf` everywhere; the helper exports `Enabled()` and `Logf` per Task 1. Field references (`state.zoomed`, `state.zoomedPane`, `pane.ID`, `pane.Dirty`, `pane.HasAnimated`, `pane.ZOrder`, `snap.ID`, `snap.Rect`, `snap.Buffer`, `snap.AltScreen`, `vp.Rows`, `vp.Cols`, `vp.AutoFollow`, `d.zoomedPane`) all match the existing code surveyed during brainstorming. The skip condition uses `state.zoomedPane` consistently across Tasks 2 and 3 (matches `[16]byte` field on `clientState`).

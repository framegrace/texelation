# Issue #235 Symptom #2 — diagnose-then-fix design

**Status:** Spec
**Parent investigation:** `docs/superpowers/specs/2026-05-06-issue-235-investigation.md`
**Prerequisite:** `docs/superpowers/specs/2026-05-06-zoomdebug-file-output-design.md`
must land first so the client process can produce observable
zoomdebug traces.

## Symptom recap

Pane running an alt-screen app (htop, vim) is zoomed to
fullscreen. Only the top-left rectangle that matches the pane's
**pre-zoom** size renders content; the rest of the zoomed area
shows stale or default cells until the next render that fully
clears it.

## What we know from server logs

`docs/superpowers/captures/2026-05-06-issue-235/server-full.log`,
sliced into `s2-zoom-transition.log`, shows the relevant frames
for pane `b0c7224b` (alt=true) crossing a zoom transition:

```
publish: pane=b0c7224b rect=130x30 snap=30x130 prev=30x130 alt=true
  emit rows=0 decor=0 revision=624

# zoom toggled, recalculateLayout runs

publish: pane=b0c7224b rect=255x62 snap=62x255 prev=30x130 alt=true
  emit rows=62 decor=0 revision=625

publish: pane=b0c7224b rect=255x62 snap=62x255 prev=0x0 alt=true
  emit rows=62 decor=0 revision=626

publish: pane=b0c7224b rect=255x62 snap=62x255 prev=62x255 alt=true
  emit rows=0 decor=0 revision=627
```

The texelterm alt-screen virtual grid resizes correctly
(snap.Buffer goes 30×130 → 62×255). The publisher emits 62 full
rows on the wire, twice in close succession (revision 625 and
626 — the second one fires because `broadcastTreeChanged` resets
prev across all panes). Server side is doing the right thing.

## Working hypothesis

The bug is somewhere in the client between `ApplyDelta` and
`screen.Show`. Most likely candidates, ordered by suspected
likelihood:

1. **PaneCache.alt row widths stale at render time.** `putAlt`
   only sets the rows in the delta. If the renderer reads
   `c.alt[y]` and that row's `len(...)` is still the pre-zoom
   col count (130), iterating up to `pane.Rect.Width` (255) on
   the screen with `if x < len(row)` guards leaves cols 130–254
   blank. This produces exactly the "top-left quadrant"
   pattern.
2. **Renderer uses pane.Rect from a stale snapshot.** If the
   client's PaneState.Rect for the zoomed pane hasn't updated
   when the renderer composites, it would draw the alt content
   into the old rect and leave the rest of the screen
   default-filled. This is *less* likely because the user
   reports the unfilled area shows stale/blank cells, not the
   surrounding split context — which suggests the rect *did*
   grow but the cells inside are short.
3. **A third path entirely.** Image cache, kitty graphics,
   effects pipeline, dynamic colors. Unlikely on first pass but
   we hold this as the catch-all.

We will not write the fix until the diagnostic phase confirms or
rejects (1) and (2).

## Out of scope

- Server-side changes. Server data is correct.
- Symptom #1 / #3 / #4 fixes. Each gets its own spec.
- Refactoring of `PaneCache` or the alt-screen render path.
  Minimum-diff fix only.

## Phase D — Diagnostic instrumentation

Add zoomdebug calls in two client locations. The
`TEXELATION_DEBUG_ZOOM_FILE` capability from the prerequisite
spec is what makes these visible.

### D1. `client/pane_cache.go::ApplyDelta`, alt branch

Inside `if alt { c.putAlt(...) }`, log:

```
ApplyDelta alt: pane=<id4> rev=<delta.Revision> rows_in_delta=<n>
  max_col_seen=<m> alt_len_after=<len(c.alt)> first_row_len=<len(c.alt[0])>
```

This tells us, for each delta application, how many rows came
in, the maximum column of the widest row, and the resulting
shape of `c.alt`.

### D2. Client renderer, alt-screen draw

Wherever `client/buffercache.go` (or the equivalent render path)
sources rows for an alt-screen pane, log once per render frame
per zoomed-or-recently-resized alt pane:

```
render alt: pane=<id4> rect=<W>x<H> alt_len=<len(c.alt)>
  alt_width_min=<min row len> alt_width_max=<max row len>
```

This is the smoking-gun observation — if `alt_width_min` is
130 while `rect.W` is 255 during the buggy frame, hypothesis 1
is confirmed.

## Phase R — Repro and capture

Re-run the symptom #2 repro from `docs/issue-235-repros.md` with
both env vars set. Save the resulting capture to
`docs/superpowers/captures/2026-05-06-issue-235/s2-client.log`.
Expected window: from the moment `recalculateLayout: zoom
paneID=b0c7224b` fires through 5–10 render frames after.

## Phase F — Fix (branched on Phase R outcome)

### F.1. If hypothesis 1 confirmed (alt rows stale)

The fix is in `client/pane_cache.go::ApplyDelta`, alt branch.
Two viable approaches:

**F.1.a — Self-normalizing cache.** After processing all rows in
the delta, find the maximum column across the rows in `c.alt`,
then pad every row in `c.alt` to that width with a default cell.
Also truncate `c.alt` to `max(rows touched in this delta) + 1`
when the delta clearly represents a full repaint (revision
delta from prev cleared, or new pane size detectable). This
last truncation needs care because partial deltas should not
shrink the cache.

**F.1.b — Authoritative width on the wire.** Add a `Cols` field
to `BufferDelta` for alt-screen deltas. The server fills it
from `len(snap.Buffer[0])`. The client uses it to pad/truncate
each row to that width. Cleaner long-term but touches the
protocol; for an investigation fix this is heavier than needed.

**Recommendation:** F.1.a. Minimum diff, no protocol change,
matches the "self-correct on next frame" character of the rest
of the alt-screen path.

**Regression test:** `client/pane_cache_test.go`, simulate two
ApplyDelta calls. First: 30 alt rows at 130 cols. Second: 62
alt rows at 255 cols (full repaint, prev was nil). Assert
`len(c.alt) == 62` and `len(c.alt[i]) == 255` for all `i`.

### F.2. If hypothesis 2 confirmed (rect stale)

The fix is wherever `PaneState.Rect` is supposed to be updated
on TreeSnapshot but isn't. Spec a follow-up; do not extend this
spec.

### F.3. If neither (hypothesis 3)

Capture is enough to localize the third path. Spec a follow-up.

## Phase C — Cleanup

After the fix lands and is verified:

- Remove the D1/D2 instrumentation calls.
- Keep the regression test.
- The shared zoomdebug helper itself stays alive until all four
  symptoms are closed; this spec does not touch it.

## Acceptance

- The S2 repro from `docs/issue-235-repros.md` produces a
  fully-filled zoomed alt-screen pane on the next render frame
  after toggle.
- The new regression test fails on the pre-fix code and passes
  on post-fix code.
- No new diagnostic logging remains in the merged tree from this
  spec's work.

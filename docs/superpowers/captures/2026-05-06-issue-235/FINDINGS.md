# Issue #235 — capture findings (2026-05-06)

Capture file: `server-full.log` (~30k lines, recovered from
`/proc/<daemon-pid>/fd/2` after `~/.texelation/server.log` was
unlinked mid-run; the daemon's open fd kept writing to the
orphaned inode).

Caveat for everything below: **only server-side instrumentation
fired**. The client process's stderr is owned by tcell during a
running session, so the client-side zoomdebug calls
(`incrementalComposite`, `fullRender`, `MsgStateUpdate`) produced
no observable output. Any client-state conclusion is inferred
from absence of evidence on the wire, not direct logs.

## Pane → role map

Inferred from `alt=` observations and the user's described setups:

| Pane ID prefix | alt-screen | role |
|----------------|-----------|------|
| `b0c7224b`     | true      | htop (alt-screen) — the pane zoomed in S2 |
| `b6fc60fe`     | false     | shell (main-screen) — the pane zoomed in S1 |
| `ec6ea992`     | false     | shell — the pane zoomed in S4 |
| `cbacd5e4`     | true      | statusbar (rect 255×2) |
| `e45f8511`     | false     | another shell |
| `d455ee6f`     | true      | floating panel (46×19) — appeared briefly during S2 |
| `50a6c5ee`     | true      | floating panel (46×19) — appeared during S4 (Ctrl+a help) |

## Symptom 2 — zoomed alt-screen quadrant

**Server is doing the right thing.** The S2 zoom transition for
pane `b0c7224b` (alt-screen) is captured in
`s2-zoom-transition.log`. The relevant frames are:

```
publish: pane=b0c7224b rect=130x30 snap=30x130 prev=30x130 alt=true   # pre-zoom
  emit rows=0 decor=0 revision=624

# toggleZoom → recalculateLayout fires → publisher reset due to TreeChanged

publish: pane=b0c7224b rect=255x62 snap=62x255 prev=30x130 alt=true   # zoom transition
  emit rows=62 decor=0 revision=625

publish: pane=b0c7224b rect=255x62 snap=62x255 prev=0x0 alt=true      # post-tree-reset
  emit rows=62 decor=0 revision=626

publish: pane=b0c7224b rect=255x62 snap=62x255 prev=62x255 alt=true   # caught up
  emit rows=0 decor=0 revision=627
```

Snapshot dimensions grow from 30×130 to 62×255 cleanly across the
zoom toggle, and 62 full rows ship on the wire on each of the two
following frames. The texelterm alt-screen Resize is not the
problem — the alt-screen virtual grid resizes correctly and the
publisher emits the full new buffer.

**Therefore the bug is client-side.** The most likely client paths:

1. `client/pane_cache.go::PaneCache.alt` — the alt 2D buffer is
   indexed flat by row. `putAlt` extends but never shrinks. After
   a zoom in/out cycle the slice may carry stale rows beyond the
   new height. More important for the reported symptom: when the
   pane goes from a 30×130 alt buffer to 62×255, every existing
   row in `c.alt` is too short for the new column count. The
   alt-screen draw path probably reads `len(row)` and stops there,
   leaving the right portion of the screen rect blank — that's the
   "top-left quadrant only" pattern the user reported.
2. The renderer's alt-screen draw uses `pane.Rect.Width`/
   `pane.Rect.Height` (which now reflect 62×255) but reads cells
   from `c.alt[y]` whose length is still the pre-zoom 130 cols.
   Same effect.

**Recommended next-spec fix:** when `ApplyDelta` receives an
alt-screen delta, treat it as authoritative for the buffer shape.
Either resize each row to the maximum column observed in the
delta, or carry an explicit `width` field in the alt-screen branch
of `BufferDelta` (the row count is already implicit in the row
indices). A quick local repro in `pane_cache_test.go` would zoom
an alt-screen pane in one delta then check that row widths match
the new dimension on read.

## Symptom 1 — non-zoomed alt-screen overlays zoomed shell

**Server keeps emitting deltas for non-zoomed panes during zoom**
— in the S2 window, while pane `b0c7224b` was zoomed, the other
"active" panes still produced ~504 deltas each over 60 seconds
(8.4/s). That's correct: the server doesn't know what the client
chose to display.

**The bug therefore lives in the client compositor.** Without
client-side logs we cannot prove which of the three Phase-3
branches is firing:

- (a) `state.zoomed` not set when incrementalComposite runs
  (state propagation race),
- (b) skip condition broken (e.g. `state.zoomedPane` mismatched),
- (c) skip fires correctly but a third path writes htop pixels
  into the screen buffer.

To diagnose, the next iteration **must** capture client-side logs.
The current zoomdebug helper writes via `log.Printf`, which on the
client runs into the tcell-owned terminal and is invisible. Two
clean options:

1. Extend `zoomdebug` to honor `TEXELATION_DEBUG_ZOOM_FILE=/path`
   and open a file when set, falling back to `log.Printf` when
   not. Document setting that env var on `texelation` so the
   *client* (which forks from the launcher) can write its logs to
   a file the user can `tail -f`.
2. Have `texelation` redirect the launcher's stderr to a file
   itself before tcell takes over the terminal.

Option 1 is less invasive and more targeted. Recommended.

## Symptom 3 — server-emit wedge

**Did not reproduce.** The user could not trigger the wedge after
several toggles on the reset baseline. Plausible explanations:

- The wedge was caused by one of the dropped PR #236 commits.
  `aee1355` mutated `prevBuffers` shape on dim-mismatch, and
  `2083d46` gated publish on `HasAttachedClients`. Either could
  have introduced or amplified the wedge.
- The wedge is timing-dependent and was unlucky to not appear on
  this run. Possible but worth less weight than the first
  hypothesis given the user's reported confidence.

**Recommended next-spec action:** mark symptom 3 as resolved by
the reset, and watch for it during the symptom 1 / symptom 2 fix
work. If it returns, capture again.

## Symptom 4 — Ctrl+a help dialog hidden behind zoom

**Inconclusive from server logs alone.** The 50a6c5ee floating
panel (46×19, alt=true) appears in the publisher output during
the S4 window (`s4-zoom-with-dialog.log`), so the server is
emitting the dialog buffer. Whether the client's
`fullRender` partition put the dialog into the *floating*
partition (correct) or alongside the zoom pane (broken) requires
client-side instrumentation to confirm.

Spec hypothesis going in was that dropping commit `ec76646` would
fix #4 outright. The reset *did* drop that commit, and the server
is emitting the dialog correctly. If the user reports the dialog
now renders on top, hypothesis confirmed and no further fix is
needed. If the user reports it still renders behind, the bug is
older than PR #236 and we need client-side traces to localize it.

## Cross-cutting observation: tree-change diff reset is aggressive

Across the run, every `recalculateLayout` zoom transition is
followed within the same publish cycle by all panes' `prev=0x0`,
i.e., a publisher-wide diff reset. That happens because
`broadcastTreeChanged()` makes the server re-send a TreeSnapshot
to the client and the connection handler calls
`ResetDiffState()` on the publisher. This is correct as a
correctness measure but it costs a full re-emit of every pane on
every zoom toggle. Not a bug; flagging because it surfaces in the
log noise and could be relevant when the symptom 2 fix is being
tested (the alt-screen pane re-emits two full buffers per
transition, so any client-side mishandling will show up at the
zoom moment, not later).

## Recommended next steps

1. **Spec a client-log capture pass.** Extend `zoomdebug` with a
   `TEXELATION_DEBUG_ZOOM_FILE` env var. Re-run all four repros.
   Save logs.
2. **Spec a client-side fix for symptom 2.** Working hypothesis:
   `PaneCache.alt` row widths and length are not authoritative
   to the latest delta. Write a regression test that simulates the
   30×130 → 62×255 transition and asserts the alt buffer is shaped
   to match.
3. **Defer symptom 1 and symptom 4 root-cause specs** until the
   client log capture lands. The server-side data here doesn't
   constrain the bug enough to write a credible fix.
4. **Close symptom 3** as resolved-by-baseline-reset for now.

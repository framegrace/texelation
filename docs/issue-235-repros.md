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
`docs/superpowers/captures/2026-05-06-issue-235/<symptom>.log` and record
observations inline at the top of the file (what you saw, exact keys
pressed, approximate timing).

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

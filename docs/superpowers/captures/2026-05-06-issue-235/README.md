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

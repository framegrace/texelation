# Issue #199 — Plan D: Client-Side Session & Viewport Persistence (STUB)

**Status:** Stub — currently in brainstorming (2026-04-25). Spec write-up next.
**Depends on:** Plan A merged (PR #202), Plan B merged (PR #203).
**Spec reference:** Operationalizes sub-problem 2's *within-daemon-lifetime* clause (cross-process client). The cross-*daemon*-restart clause is split out to Plan D2 — see below.

## Why this scope (Layer 1 only)

Plan B's review surfaced a load-bearing gap: the client never persists its `sessionID`, so a fresh `texel-client` process always lands as a new session and Plan B's resume machinery is never exercised in production. **Without Plan D, Plan B has zero user-visible effect.** Layer 1 closes this gap and is sharply higher impact-per-line than Layer 2.

The full cross-restart story (sub-problem 2's "best-effort cross-restart resume") is split into two phases:

- **Plan D (this plan, Layer 1):** Client-side persistence — `sessionID`, `viewportTrackers`, `lastSequence`. Daemon stays in memory. Covers the common case: user closes/reopens the texel-client (or restarts texelation, which keeps the daemon alive).
- **Plan D2 (`2026-04-25-issue-199-plan-d2-server-viewport-persistence.md`, future):** Server-side persistence — `PaneViewportState` on disk via the existing `MainScreenState`/WAL pattern, daemon-restart-tolerant.

Phasing is consistent with the spec's "best-effort" framing and lets Layer 1 ship and bake before the heavier session-rehydration work in Layer 2.

## Goal

A fresh `texel-client` process (or `texelation` invocation that finds the daemon already running) re-attaches to its existing daemon-side session and lands at the exact viewport it left, without the user noticing the client was killed.

## What already exists (don't rebuild)

The resume *plumbing* is already in place. `texel-headless` (`client/cmd/texel-headless/main.go`) takes `--session <hex>` and `--resume-seq N`, calls `simple.Connect(&sessionID)` + `simple.RequestResume`, and works end-to-end in integration tests. The full client (`internal/runtime/client/app.go:54-59`) and `cmd/texelation/main.go` both wire a `Reconnect` flag through to the same call sites, but they zero the sessionID so the path is currently dead. **Plan D = fill in the disk layer that this scaffolding has been waiting for.** No new wire-format work; no new resume RPC; no new server message types.

## Scope (Layer 1)

- Persist on the client:
  - `sessionID` (mandatory — without this, `simple.Connect` allocates a new session and Plan B's resume machinery never fires).
  - `viewportTrackers` snapshot (so `snapshotAll()` produces meaningful `PaneViewports` on the resume request of a fresh process).
  - `lastSequence` (for delta replay continuity).
- Disk path: aligned with the project's existing convention (`~/.local/share/texelation/...` is where scrollback already lives).
- Format: simple JSON, atomic replace (write-temp-then-rename). One file per server socket so multiple texelations on one user account don't stomp each other.
- Write cadence: on clean disconnect, plus debounced periodic flush during the session.
- Read cadence: once at client startup, before `simple.Connect` populates the sessionID arg.
- Stale-state policy: server rejects sessionID → client wipes the file and connects fresh. Loud, no auto-migration. (No back-compat constraint in this project right now — prefer clean over forgiving.)

## Out of scope (deferred to Plan D2)

- Server-side disk persistence of `PaneViewportState`.
- Daemon-restart-tolerant resume.
- Changes to the WAL / `MainScreenState` payload.
- Anything touching `internal/runtime/server/snapshot_store.go` beyond bug fixes incidentally encountered.

## Carry-forward items from Plan B review (in scope here)

These were flagged during Plan B's three review rounds and naturally land in Plan D's surface area — folding them in avoids a separate cleanup PR:

- **Phantom pane-ID pruning** in `ClientViewports`: today the map grows with stale entries when the client supplies viewports for panes that no longer exist server-side. Prune at apply time.
- **Session-eviction-with-PaneViewports test** (gap in Plan B's coverage): server evicts the session while the client is offline; client reconnects with non-empty PaneViewports. Verify clean fall-through to fresh-connect.
- **`protocol.ReadMessage` payload-len cap** (pre-existing, plan-agnostic): up-to-4GB allocation on a malformed header. Cap to e.g. 16MB. Persistence will inherit wire exposure, so addressing it during D is appropriate even though it's not strictly Plan D scope.

## Files touched (estimated)

- `internal/runtime/client/app.go` — replace zero-sessionID with load-from-disk branch (~20 LOC change).
- `internal/runtime/client/persistence.go` (new) — read/write JSON state file with atomic replace.
- `internal/runtime/client/persistence_test.go` (new).
- `internal/runtime/server/client_viewport.go` — phantom-pane pruning at `ApplyResume`.
- `internal/runtime/server/client_viewport_test.go` — pruning + eviction-with-PaneViewports cases.
- `protocol/protocol.go` — `ReadMessage` size cap.
- `protocol/protocol_test.go` — cap regression test.

## Test checklist

- Persistence round-trip: write state, read it back, fields match.
- Atomic replace: crash mid-write leaves either old file or new file, never partial.
- Fresh client process with persisted state lands at saved viewport (integration test against in-memory daemon).
- Server rejects stale sessionID → client deletes file and connects fresh; subsequent clean disconnect writes the new sessionID.
- Phantom pane pruning: `ApplyResume` with mix of valid + nonexistent PaneIDs prunes the nonexistent entries.
- Session eviction + non-empty PaneViewports payload: clean fall-through to fresh-connect, no panic.
- `ReadMessage` rejects > 16 MB payloads with a typed error.

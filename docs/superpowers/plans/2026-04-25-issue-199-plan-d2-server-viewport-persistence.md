# Issue #199 — Plan D2: Server-Side Cross-Restart Viewport Persistence (STUB)

**Status:** Stub — write the full TDD plan after Plan D ships and bakes.
**Depends on:** Plan A merged, Plan B merged, **Plan D merged** (client-side persistence). Layer 2 is meaningless without Layer 1 — clients need a stable sessionID to look up server-side state with.
**Spec reference:** Sub-problem 2 cross-*daemon*-restart clause + spec sequencing step 7.

## Why this is a separate plan from Plan D

The 2026-04-25 brainstorming for Plan D split sub-problem 2 into two phases:

- **Plan D (Layer 1):** Client-side persistence (`sessionID`, viewport trackers, `lastSequence`). Covers the daemon-stays-alive case — the texelation supervisor's normal operating mode.
- **Plan D2 (this plan, Layer 2):** Server-side persistence of `PaneViewportState` so the daemon-restart case also resumes faithfully.

Phasing is consistent with the spec's "best-effort cross-restart" framing. Layer 1 captures the dominant user scenario; Layer 2 hardens the edge case where the daemon itself died.

## Goal

Best-effort resume of `PaneViewportState` across daemon restarts. After `texel-server` restarts, the next `MsgResumeRequest` for a still-known sessionID lands at the same viewport — with Plan D's missing-anchor policy as fallback if scrollback was evicted in the interim.

## The pattern to mirror (precedent in the tree)

The terminal-side WAL already has working cross-restart persistence for state that's structurally similar to per-session viewport:

- `MainScreenState` payload (entry type 0x05) in `apps/texelterm/parser/write_ahead_log.go` — written through `AdaptivePersistence`, restored at terminal startup via `vterm_memory_buffer.go:loadHistoryFromDisk`. Carries `liveEdgeBase`, `WriteBottomHWM`, OSC 133 anchors (PR #189), tombstones (PR #191).
- Crash-safety hardening done in PR #165 (write atomicity, recovery hardening).

That's the model. Plan D2's job is to layer per-pane `PaneViewportState` (`ViewBottomIdx`, `WrapSegmentIdx`, `AutoFollow`, `Rows`, `Cols`) on top of the same scaffolding — but at the *session* layer rather than the *terminal* layer.

## Load-bearing design constraint: snapshot frequency

The user's intent for D2: snapshot per-pane viewport state **as frequently as possible** while letting the existing `AdaptivePersistence` backpressure machinery throttle under load. **Do NOT build a parallel write path with its own cadence/throttling logic.** Instead:

- **Piggyback on the existing `AdaptivePersistence` instance** for each pane's terminal (`apps/texelterm/parser/adaptive_persistence.go`).
- **Extend the WAL framing** — either add a sibling entry type alongside `MainScreenState` (entry type 0x05) or fold viewport fields into `MainScreenState` itself. The latter is cleaner since `MainScreenState` already carries terminal-side viewport-shaped state (`liveEdgeBase`, anchors); adding session-side fields keeps everything that hydrates a pane in one payload.
- **Hook into the existing notify path** — `AdaptivePersistence.NotifyMetadataChange` or a sibling. Viewport changes call into this, the existing debouncer + write-through path handles the rest. Inherits all the rate-adaptive behavior that's already shipped and hardened (PR #165 atomicity, etc.).

This is also why D2 must come after D: D's client-side state file lives in a different process (the client) and would not benefit from `AdaptivePersistence` — viewport state at the daemon side is the only place the existing infrastructure applies.

## The hard architectural question

**Sessions are not persisted today.** When `texel-server` restarts, the session table is empty; the snapshot store restores the pane tree (and PaneIDs survive), but `Session` records are freshly allocated when clients reconnect. So Plan D2 has to choose:

- **A. Persist the session table** alongside viewport state. Daemon startup loads sessions into memory; resuming clients find their session intact. Cleanest semantically; biggest behavioral change.
- **B. Rehydrate-on-resume**: viewport state (and possibly minimal session metadata) lives on disk keyed by sessionID; on `MsgResumeRequest` for an unknown sessionID, server checks disk and recreates the session record from disk if found. Lazier; preserves the "session table is in-memory" invariant.
- **C. SnapshotStore extension only**: store last-known per-pane viewport in `SnapshotStore`'s JSON, **not** keyed by session. On resume, *any* sessionID for that pane gets the last-known viewport. Simplest; loses session faithfulness (multiple users on the same pane would see each other's last position).

A is most faithful, B is incremental, C is simplest. Defer the choice to Plan D2's brainstorming session — but the relevant brainstorm decisions are explicitly load-bearing here, so capture them in a fresh decisions memory before writing the plan.

## Open questions (resolve in Plan D2 brainstorm)

- Per-session vs per-client-identity persistence (spec open question; Plan D's brainstorm punted on this).
- On-disk format: extend `MainScreenState` (per-terminal-UUID), add a sibling WAL entry type, or new top-level file in `~/.local/share/texelation/sessions/`? The terminal-UUID indirection feels wrong because viewports are per-session, not per-terminal.
- TTL / GC policy for persisted session state.
- Race with WAL rotation / chunk boundary if Layer 2 piggybacks on the WAL.
- How does Plan D2 interact with the SnapshotStore's pane-tree-restore? (Order of operations on daemon boot.)

## Downstream consumer: Plan F (session recovery)

Plan F (`docs/superpowers/plans/2026-04-26-issue-199-plan-f-session-recovery.md`) lets a user with no client-side state pick a session from a server-side list and recover it. That requires Plan D2's persisted session record to include enough metadata for a human picker:

- `LastActive time.Time` — most-recent activity (any delta, key, viewport update).
- `Label string` — human-readable name (initially empty; future RPC may set it).
- `PaneCount int` — at-a-glance summary.
- `FirstPaneTitle string` — recognition aid ("ah, that's my Claude session").

Reserve these fields in Plan D2's on-disk schema even if not all are wired immediately — it's free at design time, costly later. No back-compat constraint binds the project today, so "design with Plan F in mind" is just hygiene, not over-engineering.

## Files touched (estimated, will refine in plan)

Likely to touch some subset of:

- `internal/runtime/server/snapshot_store.go` — possibly extend with viewport payload (option C path), or create a sibling store.
- `internal/runtime/server/server_boot.go` — load viewport map at startup before accepting connections.
- `internal/runtime/server/connection_handler.go` — `MsgResumeRequest` consults disk if session unknown (option B path).
- `internal/runtime/server/session.go` — possibly add session-rehydration constructor.
- `apps/texelterm/parser/write_ahead_log.go` — possibly extend `MainScreenState` (only if option C-as-per-terminal path is chosen).
- New: `internal/runtime/server/viewport_persistence.go` if a dedicated store is the right shape.

## Test checklist (sketch)

- Save → daemon shutdown → daemon restart → `MsgResumeRequest` with persisted client sessionID lands at saved viewport.
- Save → daemon shutdown → evict enough scrollback before restart → resume falls through to Plan B/D missing-anchor policy.
- Save → daemon crash (non-clean) → restart → state survives or fails cleanly (no panic, no half-written state).
- Daemon restart with no resuming clients → persisted state remains for later resume; no eager allocation.
- TTL expiry: stale state older than N is GC'd at startup.

## Why defer

- Layer 1 is the higher-impact-per-line work and fully unblocks the user-visible win from Plan B.
- Layer 2's design has open questions (rehydration model, eviction-reconciliation) that benefit from observing real Plan D usage first.
- The OSC 133 / `MainScreenState` precedent is stable enough to pick up later — no risk of the foundation rotting under us in the interim.

# Issue #199 — Plan F: Session Recovery / Discovery (STUB)

**Status:** Stub — write the full TDD plan after Plan D2 ships.
**Depends on:** Plan A merged, Plan B merged, Plan D merged, **Plan D2 merged** (server must persist sessions across daemon restart for "list sessions" to be non-empty).
**Spec reference:** Not in the original spec — surfaced during 2026-04-25 Plan D brainstorming as a future capability.

## Goal

A user who has lost their client-side state (deleted state file, fresh machine, reinstall, accidentally `rm -rf ~/.local/state/`, etc.) can recover an existing session by asking the server for a list of known sessions and picking one. After picking, the client reconstructs as much state as possible from the server side (sessionID, last-known viewport per pane, last sequence) and resumes against it.

## Why deferred

- Layer 1 (Plan D, client-side persistence) and Layer 2 (Plan D2, server-side persistence) must land first. Without D2, "list sessions" is empty after every daemon restart; without D, there's nothing to recover *to* on the client side.
- The UX surface (list / picker) belongs in the client and benefits from observing real recovery patterns before committing to a UI. May be CLI (`texelation --recover` → numbered list → pick) or a launcher card; defer the call.

## Sketch of approach (refine in Plan F brainstorm)

**New wire messages (post-Plan-D2 protocol bump):**

- `MsgListSessions` (client → server): no payload (or filter by ageThreshold).
- `MsgListSessionsResponse` (server → client): `[]SessionSummary` with fields like `{SessionID, LastActive, PaneCount, Label, FirstPaneTitle}`. Enough info for a human to identify which session is which.
- `MsgRecoverSession` (client → server): `{SessionID}`. Server returns a fresh `MsgConnectAccept` for that session (or error if it's been GC'd between list and recover).

**Client UX (one of):**

- CLI flag: `texelation --recover` prints the list, exits with `--recover=<id>` to actually recover. Two-step keeps it scriptable.
- Launcher card: `Mod+R` opens an in-pane picker. Nicer UX, more code.

**Recovery payload assembly:**

- Server already has `PaneViewportState` on disk (from Plan D2) keyed by sessionID.
- Recovery RPC returns: sessionID + persisted viewport states + last server-sequence + pane tree snapshot.
- Client writes a fresh state file with these contents and starts a normal resume flow.

## Server-side metadata that Plan D2 must capture (load-bearing for this plan)

For "list sessions" to be human-usable, Plan D2's persisted session record needs to include enough to display a meaningful picker. Ensure D2 captures:

- `LastActive time.Time` — most-recent activity (any delta, key, or viewport update).
- `Label string` — human-readable name. Initially empty; future: set via `MsgSetSessionLabel` or auto-derived from first pane title.
- `PaneCount int` — at-a-glance summary.
- `FirstPaneTitle string` — for "ah, that's my Claude session" recognition.

These don't all need to be wired in Plan D2's first cut, but D2's on-disk schema should reserve the fields so Plan F doesn't require a format migration. **No back-compat constraint on the project right now**, so this is "design with foresight" not "lock it forever."

## Out of scope (even for the eventual full plan)

- Cross-machine session migration (move a session's state to a different daemon). Different problem; would require server-side state export/import.
- Sharing a session across users. Different problem.
- Time-travel / scrollback-rewind beyond what's persisted (this is just durable resume, not history forensics).

## Files touched (estimated, sketch only)

- `protocol/protocol.go` + `protocol/messages.go` — three new message types + summary struct.
- `internal/runtime/server/connection_handler.go` — list/recover dispatch.
- `internal/runtime/server/session.go` — recover-by-ID constructor that pulls from Plan D2's on-disk store.
- `internal/runtime/client/recovery.go` (new) — list, picker, fresh state-file write.
- `cmd/texelation/main.go` + `client/cmd/texel-client/main.go` — `--recover` flag plumbing.

## Test checklist (sketch)

- List returns persisted sessions with metadata; empty list when none persisted.
- Recover with a valid ID hands the client a fresh state file + working resume.
- Recover with a stale/GC'd ID returns clean error; client can re-list.
- Concurrent list + recover doesn't race (server-side serialization on the persisted store).
- A client that recovers can immediately disconnect-and-reconnect via the normal Plan D path (recovery is just a one-shot bootstrap).

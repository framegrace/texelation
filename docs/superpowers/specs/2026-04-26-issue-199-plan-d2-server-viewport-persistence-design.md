# Issue #199 Plan D2 — Server-Side Cross-Daemon-Restart Viewport Persistence

**Date:** 2026-04-26
**Branch:** `feature/issue-199-plan-d2-server-viewport-persistence`
**Status:** Design — ready for plan
**Depends on:** Plan A (PR #202), Plan B (PR #203), Plan D (PR #204)
**Unblocks:** Plan F (`MsgListSessions` session recovery)

## Context

Plan D shipped client-side persistence: a fresh `texel-client` process resumes against a still-running daemon by loading `sessionID + lastSequence + per-pane viewport` from disk and replaying via `MsgResumeRequest`. That covers the dominant scenario (texelation supervisor keeps the daemon alive across client restarts).

Plan D2 closes the remaining gap: when the *daemon* restarts, the in-memory `Manager.sessions` table is empty. The next `MsgResumeRequest` for a previously-known sessionID returns `ErrSessionNotFound` and the client wipes its persisted state. Today this loses the user's viewport position even though the client still has every byte of state needed to recover.

D2 persists per-session viewport state (and a thin sleeve of session metadata for Plan F) on the server side, keyed by sessionID. On `MsgResumeRequest` for an unknown sessionID, the server consults disk and rehydrates a `Session` record before falling through to the existing resume path.

## Goals

1. After `texel-server` restarts, a `MsgResumeRequest` for a sessionID that was alive before the restart succeeds and lands the pane at its saved viewport.
2. Persistence cadence matches Plan D's debounced atomic-replace shape — no append-only WAL, no per-event journaling.
3. Persistence semantics are **latest-wins snapshot**. The on-disk file always reflects the last-known good state.
4. No automatic time-based GC. Session files survive until explicitly removed by the user (sessions may serve as templates or long-lived references).
5. Reserve schema fields for Plan F's session-discovery picker so D2's on-disk format is forward-compatible.
6. Eliminate writer-implementation drift between Plan D's client persistence and D2's session persistence by extracting a shared debounced atomic-JSON primitive.

## Non-goals

- Persisting the in-memory diff queue (it's bounded and ephemeral by design).
- Persisting `Session.NextSequence` (see "Cross-restart synchronization" below).
- A background eviction sweeper. Boot-time scan handles only corrupt files.
- Generalizing the existing terminal WAL into reusable infrastructure. The WAL is an append-only event journal; D2's needs are latest-wins snapshot. Different abstractions.
- Per-client-identity vs per-session keying — sessions stay keyed by `[16]byte` SessionID, same as today.

## Architecture

### Storage shape: rehydrate-on-resume (Option B)

`Manager.sessions` stays in-memory-only. New on-disk store under `<basedir>/sessions/<hex-sessionID>.json`, mirroring Plan D's `<basedir>/clients/<name>.json` layout. One file per session.

Lifecycle:

- **Boot** (`server_boot.go`): scan `<basedir>/sessions/`, parse each file. Drop entries that fail to parse (log + delete). Keep an in-memory index of loaded session metadata for O(1) `MsgResumeRequest` lookup. **Do not** allocate `Session` records yet — sessions only exist in `Manager.sessions` on demand.
- **Resume** (`connection_handler.go` / `handshake.go`): on `MsgResumeRequest` for unknown sessionID, consult the disk index. If found, allocate a fresh `Session` with that ID, pre-seed `ClientViewports` from `StoredSession.PaneViewports`, then proceed through the existing Plan B `ApplyResume` + `RestorePaneViewport` path.
- **Steady state**: any session-state mutation (viewport update, pane add/remove, session activity) feeds the writer; debounce coalesces bursts.
- **Close**: `Session.Close()` does **not** delete the file. Files persist until explicit user action. (Future tooling — Plan F's CLI — will surface deletion.)

### Why not Option A (persist full session table at boot)?

Eagerly hydrating sessions allocates state for clients that may never reconnect, changes the boot order (sessions before tree), and costs memory proportional to disk-file count rather than active-client count. Lazy rehydration matches the existing `SnapshotStore` pattern (load-on-need) and keeps "session table is in-memory" as an invariant.

### Why not Option C (extend `SnapshotStore` per-pane, not session-keyed)?

Plan F's session-discovery picker needs *session-level* metadata (LastActive, PaneCount, FirstPaneTitle) that doesn't have a natural per-pane home. Keying by pane also fails the multi-session-per-pane case (one client per pane could overwrite another's viewport).

### Why not piggyback on `AdaptivePersistence` / extend `MainScreenState`?

`AdaptivePersistence` is keyed per-terminal-UUID. Sessions span multiple panes with multiple terminals; session-level metadata has no natural per-terminal home, and "give me session X's viewports" would require iterating all terminals' WALs. The piggyback hint in the original stub assumed reuse of WAL throttling, but viewport state is small and low-rate (see "Frequency budget") — bespoke debounced writer is the right shape.

### Writer abstraction: `DebouncedAtomicJSONStore`

Today's tree has three latest-wins persistence shapes:

1. `internal/runtime/server/snapshot_store.go` — atomic temp+rename, no debounce, externally-driven cadence.
2. `internal/runtime/client/persistence.go` (Plan D) — atomic temp+rename, internal debounce.
3. `internal/runtime/server/<this plan>` — same shape as #2.

To avoid implementation drift between #2 and #3, this plan extracts a shared primitive:

- New package `internal/persistence/atomicjson/` (proposed location) exposes `DebouncedAtomicJSONStore[T any]` (or non-generic equivalent) with `Update(T)`, `Flush()`, `Close()`, plus standalone `Save[T]`, `Load[T]`, `Wipe` helpers.
- Refactor Plan D's client `Writer` to use the shared primitive (drops the bespoke implementation).
- D2's session writer is a second consumer.
- `SnapshotStore` is **not** migrated in this plan (different cadence model, no debounce). A future cleanup may unify it.

The shared primitive must preserve the load-bearing properties from Plan D's `persistence.go`:

- Atomic temp+rename (crash leaves either old or new file, never partial).
- Per-error-string log dedup with `saveErrRelogInterval` to prevent log floods on persistent failure.
- `wg`-based shutdown so timer goroutines complete before `Close` returns.
- Separate `saveMu` from `mu` to allow `Update` to land while a save is in flight.

### Frequency budget

- **Viewport scroll**: client-throttled at ~50ms in `flushFrame`. After throttling, server sees ≤20 Hz per pane.
- **Resize**: human-rate, ≤1 Hz.
- **Pane add/remove**: rare (workspace operations).
- **Activity timestamp** (`LastActive`): bumped on any of the above.

Server-side debounce of 250ms (matching Plan D's client) coalesces these into one write per session per burst. Bound: ≤4 writes/sec per active session under sustained scroll. Atomic JSON file ~1-4KB per session.

## On-disk schema

```go
// StoredSession is the on-disk representation of cross-restart session state.
// One file per session at <basedir>/sessions/<hex-sessionID>.json.
type StoredSession struct {
    SchemaVersion int                      `json:"schemaVersion"` // bump on breaking change
    SessionID     [16]byte                 `json:"-"`             // mirrored hex in JSON via custom marshaler
    LastActive    time.Time                `json:"lastActive"`    // updated on any state-bearing change
    Pinned        bool                     `json:"pinned"`        // reserved; future tooling honors this
    PaneViewports []StoredPaneViewport     `json:"paneViewports"`
    // Reserved for Plan F (populated at write time, no consumers in D2):
    Label          string `json:"label"`
    PaneCount      int    `json:"paneCount"`
    FirstPaneTitle string `json:"firstPaneTitle"`
}

type StoredPaneViewport struct {
    PaneID         [16]byte `json:"-"` // hex in JSON
    AltScreen      bool     `json:"altScreen"`
    AutoFollow     bool     `json:"autoFollow"`
    ViewBottomIdx  int64    `json:"viewBottomIdx"`
    WrapSegmentIdx uint16   `json:"wrapSegmentIdx"`
    Rows           uint16   `json:"rows"`
    Cols           uint16   `json:"cols"`
}
```

Schema version starts at `1`. Custom `MarshalJSON`/`UnmarshalJSON` convert `[16]byte` to lowercase hex (jq-friendly), matching the convention in Plan D's `persistence.go`.

### Excluded fields and why

- **`NextSequence`**: not persisted. The diff queue is in-memory and dies with the daemon, so persisting only the counter would lie about replayability. Cross-restart synchronization is handled by client-side reset on the post-resume `MsgTreeSnapshot` (see below).
- **Per-pane revision counters**: not persisted. Same rationale as `NextSequence`. The client also resets `pane.Revision` on the post-resume snapshot.
- **In-memory diff history**: not persisted. Recovery is via fresh snapshot, not replay.

## Cross-restart synchronization (client-side reset)

After daemon restart, the new server starts both `nextSequence` and per-pane `revisions` at zero. A still-alive client across the restart has stale `lastSequence` and `pane.Revision` from the dead daemon. Without intervention, the client would dedup-drop the new server's low-numbered messages as stale.

The fix: on receipt of the post-resume `MsgTreeSnapshot`, the client resets:

- Per-pane `BufferCache.pane.Revision = 0` (extends the same pattern Plan D already needed for new-publisher-on-existing-session).
- Top-level `lastSequence = 0` so subsequent diffs are accepted regardless of their sequence number relative to the pre-restart stream.

This makes the post-resume snapshot the single synchronization barrier for both counters. One rule, two consumers.

`MsgTreeSnapshot` is already emitted as part of every connect/resume flow, so no protocol change is required.

## Boot-time scan and corruption handling

```text
1. Server boot reaches server_boot.go after pane tree is restored from SnapshotStore.
2. List <basedir>/sessions/*.json.
3. For each file:
   a. ReadFile + Unmarshal.
   b. On error: log("session file %s parse failed (%v); deleting"); os.Remove; continue.
   c. On schema mismatch: log + delete + continue (no auto-migration; project has no back-compat constraint).
   d. On success: register in in-memory Manager.persistedSessions index keyed by SessionID.
4. Index is read-only after boot completes. New writes go through the live writer path.
```

`Manager.persistedSessions` (or similar — name TBD in plan) is a `map[[16]byte]*StoredSession` consulted only on `MsgResumeRequest` cache miss in `Manager.sessions`.

## Resume flow (revised)

```text
Client sends MsgConnectRequest{SessionID = X}
└── handleHandshake (handshake.go):
    ├── if X == zero: create new Session (existing path, no change)
    ├── if X in mgr.sessions: return existing Session (existing path, no change)
    └── else (cache miss):
        ├── if X in mgr.persistedSessions:
        │   ├── construct fresh Session{id: X, ...}
        │   ├── pre-seed Session.viewports from StoredSession.PaneViewports
        │   ├── register in mgr.sessions
        │   ├── remove from mgr.persistedSessions (now-live, future writes go through writer)
        │   └── proceed to MsgConnectAccept (resuming=true)
        └── else: return ErrSessionNotFound (existing behavior, client wipes)

Client receives MsgConnectAccept, sends MsgResumeRequest{PaneViewports, lastSequence}
└── existing Plan B flow runs unchanged:
    ├── Session.ApplyResume seeds viewports (overwrites pre-seeded values where client has fresher)
    ├── DesktopEngine.RestorePaneViewport per pane
    ├── ResetDiffState + Publish
    └── client receives TreeSnapshot → resets pane.Revision and lastSequence to 0
```

The pre-seed from disk and the client's `MsgResumeRequest.PaneViewports` are both valid sources of viewport state. The client's value wins because it's fresher (the client may have moved the viewport between its last persisted save and reconnect). When the client has no persisted viewport for a pane (e.g., post-Plan-D fresh-process recovery just reading sessionID), the disk-seed value carries through.

## Writer wiring

A single `*atomicjson.Store[StoredSession]` per session, stored on `*Session`:

```go
type Session struct {
    // ... existing fields ...
    persistWriter *atomicjson.Store[StoredSession]  // nil-safe: nil means persistence disabled
}
```

Update sites (each schedules a debounced write):

- `Session.ApplyViewportUpdate` (every `MsgViewportUpdate`)
- `Session.ApplyResume` (initial post-resume seed)
- `Session.EnqueueDiff` / `EnqueueImage` (bumps `LastActive` only — no viewport change, but proves the session is alive)
- A new `Session.RecordPaneAddRemove(paneCount, firstTitle)` hook called from the desktop publisher when the pane tree changes (updates `PaneCount` and `FirstPaneTitle` for Plan F)

Each call builds a fresh `StoredSession` from the current in-memory state and hands it to `Update`. The writer coalesces.

`Session.Close` calls `persistWriter.Close()` to flush the final state synchronously. The file is **not** deleted on close — see "Lifecycle" above.

## Test plan

Integration tests added to `internal/runtime/server/`:

1. **`TestD2_BasicCrossRestartResume`** — write session state, simulate daemon restart (drop and re-init `Manager`, run boot-scan), client sends `MsgResumeRequest`, verify viewport restored.
2. **`TestD2_BootScanIgnoresCorruptFile`** — write a malformed file alongside a good one; boot scan loads the good one and deletes the bad one.
3. **`TestD2_ResumeUnknownSessionStillFails`** — persisted index has session A, client requests session B → `ErrSessionNotFound` (no false-positive recovery).
4. **`TestD2_ClientFresherViewportWins`** — disk has viewport at gid=100, client's `MsgResumeRequest` carries gid=200 for the same pane → final state is gid=200.
5. **`TestD2_RehydrateThenRestoreLivePath`** — full lifecycle: persisted file → boot scan → resume → live `MsgViewportUpdate` lands and triggers debounced write back to disk.
6. **`TestD2_PinnedNotConsumedYet`** — write file with `Pinned=true`; survives boot scan; field round-trips. (Forward-compat assertion.)
7. **`TestD2_SchemaMismatchDeletes`** — write file with `SchemaVersion=999`; boot scan logs + deletes.

Unit tests for the new `atomicjson` package:

8. **Round-trip `Save`/`Load` for arbitrary `T`.**
9. **`Update` debouncing** — N rapid updates → exactly one disk write within N×debounce.
10. **`Close` flushes pending writes** — pending update at close time hits disk.
11. **Atomic-write semantics** — Save mid-write crash leaves either old or new file (simulate via failing rename).

Plan D regression tests must keep passing after the client-side `Writer` is refactored onto `atomicjson`.

## Files touched (estimate)

**New:**
- `internal/persistence/atomicjson/store.go` (~150 lines, extracted from Plan D's `Writer`)
- `internal/persistence/atomicjson/store_test.go`
- `internal/runtime/server/session_persistence.go` (StoredSession + Save/Load helpers; ~200 lines)
- `internal/runtime/server/session_persistence_test.go`
- `internal/runtime/server/persisted_sessions_test.go` (boot-scan tests)
- `internal/runtime/server/d2_resume_integration_test.go`

**Modified:**
- `internal/runtime/server/manager.go` — add `persistedSessions` index, lookup-with-rehydrate.
- `internal/runtime/server/handshake.go` — consult disk on cache miss.
- `internal/runtime/server/session.go` — add `persistWriter`, hook into Apply* + Enqueue* + Close.
- `internal/runtime/server/server_boot.go` — invoke session-dir scan after pane-tree restore.
- `internal/runtime/server/server.go` — wire `<basedir>/sessions/` path into `Manager` construction.
- `internal/runtime/client/persistence.go` — refactor `Writer` onto `atomicjson.Store`.
- `internal/runtime/client/app.go` — point at the new shared writer (no behavior change expected).
- `internal/runtime/client/cache.go` (or wherever `pane.Revision`/`lastSequence` live) — reset on `MsgTreeSnapshot` after resume.

## Open questions (deferred to plan or implementation)

- Exact home for `atomicjson` (`internal/persistence/atomicjson/` proposed; final location at writer's discretion).
- Whether `StoredSession.PaneCount` and `StoredSession.FirstPaneTitle` write paths run in this plan or are stubbed for Plan F. Recommend: wire them now; cost is trivial and Plan F's schema gets validated implicitly.
- Boot-scan path resolution — where does `<basedir>` come from at server boot (env var, flag, hardcoded path)? Mirror whatever `SnapshotStore` does today (likely `XDG_STATE_HOME`-based).
- Race between `Session.Close` flushing and a concurrent `Update` — Plan D's Writer handles this; the shared primitive must inherit the property.

## Risks

1. **Plan D regression risk during refactor.** The client `Writer` extraction changes a recently-shipped path. Mitigation: Plan D's full integration test suite must pass after the refactor, and the refactor is its own sequenced task before D2-specific code lands.
2. **Boot-scan latency.** If `<basedir>/sessions/` accumulates many files (no GC), boot reads them all. Bound: <100ms for 1000 sessions on typical SSDs (each file ~2KB, JSON parse cheap). Acceptable. Future tooling can offer manual cleanup.
3. **Disk-seeded viewport stale on resume.** If a client resumes with stale persisted state (rare — would require client edited its own file), the disk-seed value is overwritten by `MsgResumeRequest.PaneViewports`. Worst case is a single frame rendered at the disk-seed position before the client's viewport overrides — unobservable.
4. **`TreeSnapshot`-driven reset risks dropping non-resume snapshots.** A `TreeSnapshot` arrives on every connect *and* on workspace operations (split, kill, move). Resetting `pane.Revision`/`lastSequence` on every snapshot would corrupt the steady-state stream. Mitigation: only reset on the specific post-resume snapshot — gated by a "this connect is a resume" flag set in `MsgConnectAccept` handling, cleared after the next snapshot. This needs careful threading; the plan must call it out as a load-bearing invariant.

## Success criteria

- Manual e2e: start daemon, scroll back in a long session, kill `texel-server` (`SIGTERM`, then `SIGKILL` to test crash), restart daemon, reconnect with same client — pane lands at saved scroll position.
- Manual e2e: same as above but kill the client too (full client+daemon restart) — pane lands at saved scroll position.
- All existing tests (Plan A/B/D suites + race detector) green after the refactor.
- New tests in the plan above all green.
- No new `gofmt -d` diff. `go vet` clean.

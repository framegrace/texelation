# Plan D — Client-Side Session & Viewport Persistence (Issue #199)

**Status**: Design (approved 2026-04-25)
**Date**: 2026-04-26
**Issue**: [#199](https://github.com/framegrace/texelation/issues/199)
**Plan stub**: `docs/superpowers/plans/2026-04-24-issue-199-plan-d-viewport-persistence.md`
**Plans index**: `docs/superpowers/plans/2026-04-21-issue-199-plans-index.md`
**Parent spec**: `docs/superpowers/specs/2026-04-20-issue-199-viewport-only-rendering-design.md` (operationalizes sub-problem 2 within-daemon-lifetime clause)

## Background

Plan A (PR #202, merged 2026-04-23) established viewport clipping + FetchRange. Plan B (PR #203, merged 2026-04-25) extended `MsgResumeRequest` with `[]PaneViewportState` and made the server honor anchor + wrap-segment + autoFollow on first paint.

Plan B's three review rounds surfaced a load-bearing gap: in `internal/runtime/client/app.go:54-59` the client's sessionID is always zero, so `simple.Connect` always allocates a fresh session, and Plan B's resume machinery is never exercised in production. Across a `texel-client` restart — the dominant scenario under the texelation supervisor, which keeps the daemon alive — the user always lands in a freshly-allocated session and loses their scrolled-back position.

The resume *plumbing* already exists. `client/cmd/texel-headless/main.go:54-78` exercises the full `simple.Connect` + `simple.RequestResume` path end-to-end, fed by a `--session` CLI flag. What's missing is a disk layer to feed that scaffolding from a previous client invocation. This spec describes that disk layer.

## Goals

- A fresh `texel-client` process re-attaches to its existing daemon-side session and lands at the exact viewport it left, without the user noticing the client was killed.
- Multiple `texel-client` processes against the same daemon can each maintain their own independent persistent state (multi-monitor / multi-machine ready).
- Plan D ships with three carry-forward fixes from Plan B's review (phantom pane pruning, eviction-with-PaneViewports test, `ReadMessage` payload cap) folded in.
- No protocol changes. No new wire messages. No version bump.

## Non-goals

- Server-side cross-daemon-restart persistence. Deferred to **Plan D2** (`docs/superpowers/plans/2026-04-25-issue-199-plan-d2-server-viewport-persistence.md`).
- Session recovery / discovery from server-side state. Deferred to **Plan F** (`docs/superpowers/plans/2026-04-26-issue-199-plan-f-session-recovery.md`). Plan D's on-disk schema is recovery-compatible — a future `--recover` tool would write the same JSON shape — but the picker UX and the `MsgListSessions` wire path are out of scope.
- Auto-detection of per-monitor / per-display client identity. The user wires `--client-name` into their compositor / launcher; texelation does not try to infer it.
- Backward compatibility with any prior client persistence shape. The project has no back-compat constraint at this point — stale on-disk state must fail-and-overwrite, not auto-migrate.

## The Approach

### Storage location and format

```
${XDG_STATE_HOME:-~/.local/state}/texelation/client/
├── <socketHash>/
│   ├── default.json     # no --client-name given
│   ├── left.json        # texelation --client-name=left
│   └── right.json       # texelation --client-name=right
└── <other-socketHash>/
    └── default.json
```

- `socketHash` = first 16 hex chars of `SHA256(absolute socket path)`. Filesystem-safe; collision-resistant for any realistic number of sockets.
- `clientName` resolution: `--client-name` flag → `$TEXELATION_CLIENT_NAME` env var → literal `"default"`.
- Format: JSON, atomic-replace via `os.Rename` from a sibling `.tmp` file in the same directory.
- XDG_STATE_HOME (`~/.local/state` by default) is the correct dir per XDG semantics: volatile state, regenerable from server side, not user data. The existing scrollback WAL at `~/.local/share/texelation/` is technically miscategorized but stays where it is — moving it is out of scope.

### Schema

```json
{
  "socketPath":   "/tmp/texelation.sock",
  "sessionID":    "0123456789abcdef0123456789abcdef",
  "lastSequence": 12345,
  "writtenAt":    "2026-04-26T12:34:56Z",
  "paneViewports": [
    {
      "paneID":         "fedcba9876543210fedcba9876543210",
      "altScreen":      false,
      "autoFollow":     true,
      "viewBottomIdx":  9876,
      "wrapSegmentIdx": 0,
      "rows":           24,
      "cols":           80
    }
  ]
}
```

Field semantics mirror `protocol.PaneViewportState`. JSON encoding uses lowercase hex strings for `[16]byte` paneID/sessionID rather than base64 (readability; `jq`-friendly). On load, fields feed straight into `simple.RequestResume`'s payload — round-trip is symmetric.

The `socketPath` field is a sanity check: if it doesn't match the current `--socket` value, the file was for a different daemon and gets wiped before use. The `writtenAt` field is purely for debugging — never read by the runtime.

### Read cadence (startup)

Once at client startup, before the existing zero-sessionID branch in `internal/runtime/client/app.go:54-59`:

```
1. Resolve clientName and socketHash; build filePath.
2. persistence.Load(filePath):
     - file missing             → return zero state
     - parse error              → log warning, delete file, return zero state
     - socketPath mismatch      → log warning, delete file, return zero state
     - ok                       → return (sessionID, lastSequence, paneViewports)
3. Pass loaded sessionID into simple.Connect(&sessionID).
4. If state was loaded: simple.RequestResume(conn, sessionID, lastSeq, paneViewports).
5. If RequestResume returns ErrSessionNotFound:
     - persistence.Wipe(filePath)
     - retry simple.Connect(&zeroSessionID)
     - server allocates new session; persistence overwrites on next tick
```

### Write cadence (steady state and exit)

- **Debounce:** 250 ms after the last viewport-tracker change.
- **Skip-if-busy:** if a write is already in flight, drop the new request. The next debounce tick picks up the latest state.
- **Always-flush-on-clean-exit:** synchronous final write before process termination.
- **No background timer:** writes fire only on viewport-change events. Idle session = zero disk writes.

This bounds crash-loss to ≤250 ms of scroll movement (perceptually invisible), zero overhead when idle, never stalls the input loop. The atomic-replace gives crash-safety to either-old-or-new — never partial.

### Multi-client identity

`--client-name` (default `"default"`) lets multiple `texel-client` processes against the same daemon maintain independent state. Server-side already supports multiple connections per socket (each `Connect` allocates its own session); Plan D adds the missing client-side identity so a fresh process can disambiguate which slot to load.

- **CLI:** `texelation --client-name=foo` (also `texel-client --client-name=foo`).
- **Env fallback:** `TEXELATION_CLIENT_NAME=foo` for shell/launcher convenience.
- **Same-name collision:** two processes with the same `--client-name` get last-writer-wins on the file. No locks. Acceptable — it's user error to launch two `--client-name=left` simultaneously.
- **Cross-machine:** automatic via filesystem isolation. Each machine has its own `~/.local/state/texelation/client/`.
- **File-per-client (vs map-in-one-file):** chosen to avoid read-modify-write contention between concurrent clients on the same socket; `ls <socket-dir>/` provides equivalent discoverability.

### Server-side carry-forwards from Plan B review

Three items flagged during Plan B's three review rounds land in Plan D's surface area — folded in to avoid a separate cleanup PR:

1. **Phantom pane pruning** (`internal/runtime/server/client_viewport.go`): at `ApplyResume`, drop entries whose paneID is not in the current pane registry. Plan B's `TestIntegration_ResumeMultiplePaneViewports` explicitly tested that phantom entries persist; that's now reframed as a leak. Lookup uses the existing engine pane-registry path. Doc-comment the why (cross-restart resume + closed-pane case).

2. **Eviction-with-PaneViewports test** (`internal/runtime/server/client_viewport_test.go`): cover `ErrSessionNotFound` on `MsgResumeRequest` with non-empty PaneViewports. Plan B tested the components separately but never the combination. Assert clean connection close, no panic. New: `TestApplyResume_EvictedSessionWithPaneViewports`.

3. **`ReadMessage` payload cap** (`protocol/protocol.go`): introduce `const MaxPayloadLen = 16 * 1024 * 1024`. Reject earlier than `make([]byte, header.PayloadLen)`. Pre-existing security gap (up to 4 GB allocation on a malformed header); in scope here because Plan D's persistence inherits wire exposure when a recovered file is replayed against the server.

### Error handling matrix

| Failure mode | Behavior |
|---|---|
| File missing | Treat as fresh client; no error |
| Parse error / corrupt JSON | Log warning, delete file, continue fresh |
| `socketPath` mismatch | Wipe; treat as fresh |
| `os.WriteFile` / `os.Rename` fails | Log error, drop this write, retry on next debounce tick. Never crash the client. |
| Server rejects sessionID (`ErrSessionNotFound`) | Wipe, retry connect with zero sessionID, server allocates new session, persistence overwrites on next tick |
| Two same-`--client-name` processes | Last-writer-wins; no locks |

## Testing

### Unit tests (`internal/runtime/client/persistence_test.go`)

- Round-trip: marshal → unmarshal preserves all fields including paneID/sessionID hex format.
- Atomic-replace: simulated mid-write crash leaves only old or new file, never partial. Use a fault-injecting `os.Rename` shim or test-double.
- `socketPath` mismatch on load wipes the file.
- Parse error on load wipes the file.
- Coalescing: N rapid changes within a 250 ms debounce window produce 1 write.
- Skip-if-busy: a slow `Save` in-flight does not block the debounce loop; a follow-up change after the slow save completes triggers exactly one further write.

### Integration tests (extends Plan A/B in-memory daemon harness)

- `TestIntegration_PersistedClientResumesViewport` — fresh client process loads persisted state, lands at saved viewport. Exercises the full `Load → Connect → RequestResume → render` path.
- `TestIntegration_StaleSessionFallsBackClean` — server rejects stale sessionID → client wipes file → reconnects fresh; on next clean exit, file holds the new sessionID.
- `TestIntegration_MultiClientIsolation` — two clients with different `--client-name` against the same socket maintain independent state across restart.

### Server-side tests (`internal/runtime/server/client_viewport_test.go`)

- `TestApplyResume_PrunesPhantomPaneIDs` — `ApplyResume` with mix of valid + nonexistent paneIDs drops nonexistent entries. Asserts the map size and contents post-call.
- `TestApplyResume_EvictedSessionWithPaneViewports` — evicted session, client reconnects with non-empty PaneViewports → clean fall-through (`ErrSessionNotFound`), no panic, connection cleanly closed.

### Protocol tests (`protocol/protocol_test.go`)

- `TestReadMessage_PayloadTooLarge` — payloads > 16 MB rejected with typed error before the `make([]byte, n)` allocation.

### Race tests

- `go test -race ./internal/runtime/client/...` — debounced writer must not race with the connection loop's tracker reads. Likely use a small mutex around the in-memory snapshot the writer marshals.

## Migration and rollout

- Lockstep client deploy. The texelation supervisor restarts the daemon on version mismatch; no mixed-version concerns since Plan D doesn't change the wire.
- First run after upgrade: persistence file doesn't exist → behavior is identical to current client. New file appears on first clean exit.
- Old persistence files don't exist (this is the first iteration). No migration code.
- Future format changes: fail-and-overwrite, no migration. Bump filename if a major change is needed (`default.json` → `default.v2.json`); old files orphan and may be manually cleaned.

## Open questions for implementation

These are design-settled but need a concrete call during the plan:

- Exact debounce constant (250 ms is starting guess; tune on first profiling if input lag becomes visible during heavy scrolling).
- Whether `Save` invokes `fsync` on the temp file before `Rename`. Default: no — the rename's atomicity is enough for our durability target (≤250 ms loss). Add fsync only if a measured crash-loss exceeds the target.
- Logging level for "wiped stale state" cases. Likely `Info` for Wipe-on-server-reject (expected during normal daemon-restart recovery flows) and `Warn` for Wipe-on-parse-error (suggests file corruption).

## Future direction

- **Plan D2** (server-side cross-daemon-restart persistence): mirrors the existing `MainScreenState` WAL pattern at the session layer; piggybacks on the existing `AdaptivePersistence` instance per pane's terminal. Will write through the same debouncing + backpressure path that already ships and is hardened. See plan stub.
- **Plan F** (session recovery / discovery): client-side `--recover` UX + new `MsgListSessions` / `MsgRecoverSession` wire paths. Plan D's schema is already recovery-compatible — a recovery tool just builds the same JSON shape and atomic-writes it. No Plan D changes required when F lands.

## Sequencing within the PR

Implementation order (refined by `superpowers:writing-plans`):

1. New `internal/runtime/client/persistence.go` with `Load`, `Save`, `Wipe`, atomic-replace primitives. TDD against `persistence_test.go`.
2. `protocol/protocol.go` `ReadMessage` size cap + test (independent surface; lands first to catch any wire impact early).
3. `internal/runtime/server/client_viewport.go` phantom-pane pruning + tests (independent server-side change).
4. `internal/runtime/client/app.go` integration: load on startup, debouncer install, flush on exit. Wire into existing zero-sessionID branch at lines 54-59.
5. `cmd/texelation/main.go` and `client/cmd/texel-client/main.go` flag registration.
6. Integration tests against the in-memory daemon harness.
7. Race-detector pass; manual end-to-end verification per the plan file's verification checklist.

Each step is independently mergeable for local testing; PR-level commit is one logical change.

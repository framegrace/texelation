# Zoomdebug file output — design

**Status:** Spec
**Parent investigation:** `docs/superpowers/specs/2026-05-06-issue-235-investigation.md`
**Capture findings driving this work:**
`docs/superpowers/captures/2026-05-06-issue-235/FINDINGS.md`

## Background

The first capture pass for issue #235 produced ~30k lines of
*server-side* zoomdebug output but **zero client-side output**.
Cause: in a running texelation session the launcher's tcell
takes over the terminal, so the client process's stderr — where
`log.Printf` ends up by default — is owned by the terminal
display. Calls to `zoomdebug.Logf` inside
`incrementalComposite`, `fullRender`, and `MsgStateUpdate`
disappear into the curses display rather than producing
observable lines.

Without client-side traces we cannot finish diagnosing
symptoms #1 (htop overlay), #2 (alt-screen quadrant), or #4
(modal behind zoom). Symptom #2 in particular has a strong
hypothesis (PaneCache.alt row widths stale across zoom) that
needs a client-side render-time observation to confirm before
we write the fix.

## Goal

Make `zoomdebug.Logf` writable to a file when an env var is set,
so the client process can log even while tcell owns stderr.
Server process keeps its existing behaviour as a fallback (its
`log.Printf` already lands in `~/.texelation/server.log`).

## Out of scope

- Switching the server's logging to use the same file. The
  server already has a working sink and combining the two would
  obscure timeline analysis.
- A general-purpose runtime logger. This file lives only as long
  as issue #235 is open and is removed alongside the rest of the
  zoomdebug instrumentation when the underlying bugs are fixed.
- Log rotation, structured JSON output, severity levels. The
  whole package is throwaway.

## Design

### Environment variable

`TEXELATION_DEBUG_ZOOM_FILE=/path/to/log` — when set and
non-empty, zoomdebug opens the path with
`O_APPEND|O_CREATE|O_WRONLY` and routes `Logf` writes to it.
When unset or empty, behaviour is unchanged from the current
`log.Printf` path.

`TEXELATION_DEBUG_ZOOM` retains its meaning as the on/off gate.
The file env var alone does not enable logging — the user must
also set `TEXELATION_DEBUG_ZOOM=1`. This keeps the existing
default-off semantics and avoids surprise file growth.

If the file open fails at startup, `Init` logs a single warning
to `log.Printf` and zoomdebug falls back to the existing
`log.Printf` sink. The session continues. Failing-loud at startup
would be more correct in production code, but for an
investigation tool that's already gate-protected, silently
continuing is the safer default.

### Process role

Both `texelation` (which runs the client in-process) and
`texel-server` (which runs as the daemon) inherit the env var
through `exec.Command` default inheritance. Without
disambiguation their lines would interleave in the log without a
way to tell client decisions apart from server emit decisions.

The package exposes:

```go
func Init(role string)
```

Each entry point calls this exactly once before any `Logf` call:

- `cmd/texel-server/main.go` and `cmd/texelation/main.go` call
  `zoomdebug.Init("server")` early in main (before the daemon's
  publish loop fires).
- `client/cmd/texel-client/main.go` and the in-process client
  bootstrap inside `cmd/texelation/main.go` call
  `zoomdebug.Init("client")`.

Lines emitted via `Logf` are then prefixed with `[zoom-debug
<role>]` instead of the current `[zoom-debug ]`. When `Init` is
not called, the prefix is `[zoom-debug ?]` so the omission is
visible.

`Init` is idempotent: calling it twice with the same role is a
no-op; calling it twice with different roles overwrites the
prefix and logs a single warning. This prevents one accidental
double-call from silently corrupting later traces.

### Concurrency

`Logf` is called from multiple goroutines (renderer, protocol
handler, publish loop). The current implementation uses
`log.Printf`, which has its own internal mutex. The new
file-output path needs the same: a `sync.Mutex` around the file
write.

The file fd is leaked for the process lifetime (no close on
shutdown). This matches the existing texelation pattern of
not closing daemon log fds (see `cmd/texelation/lifecycle/daemon.go`
"We intentionally do NOT close this file" comment).

### Public API

```go
package zoomdebug

// Init records the process role and selects the output sink.
// Call once early in main, before any Logf call. Subsequent
// calls with the same role are no-ops; with a different role
// they log a warning and overwrite the role.
func Init(role string)

// Enabled reports whether zoom-debug logging is active.
// Unchanged.
func Enabled() bool

// Logf writes a "[zoom-debug <role>] " prefixed line. When
// TEXELATION_DEBUG_ZOOM_FILE is set, output goes to that file;
// otherwise to log.Printf. Unchanged when TEXELATION_DEBUG_ZOOM
// is unset.
func Logf(format string, args ...any)
```

`Init` and `Logf` are safe to call from any goroutine.

### Error handling

- File open failure on `Init`: warn via `log.Printf`, fall back
  to `log.Printf` sink, set role anyway.
- File write failure on `Logf`: silent. We don't want one bad
  line to spam stderr (which is tcell-owned on the client).
- Missing `Init`: prefix as `?`, output still works.

These are deliberate tradeoffs for a diagnostic tool — production
code would do the opposite (loud errors, fatal misuse).

## Testing

Unit test file `internal/runtime/zoomdebug/zoomdebug_test.go`:

1. Default state: `Enabled()` reflects the env var, `Logf` is a
   no-op when disabled.
2. With `TEXELATION_DEBUG_ZOOM=1` and no file env var, `Logf`
   writes through `log.Printf` (intercept via
   `log.SetOutput(buf)`).
3. With both env vars set, `Logf` writes to the file with the
   role prefix.
4. `Init("client")` then `Init("server")` produces a warning,
   subsequent `Logf` uses the second role.
5. Concurrent `Logf` from N goroutines writes N lines to the
   file (no interleaved partial writes).

Tests use `t.TempDir()` for the file path and `t.Setenv` for the
gate variables. The package's `enabled` and `outputFile` globals
need to be re-evaluated per test, so add a small `forTesting`
hook that re-reads the env vars and (re)opens the file. The hook
is only used by tests; production paths set state once at Init.

## Acceptance

- A user running
  `TEXELATION_DEBUG_ZOOM=1 TEXELATION_DEBUG_ZOOM_FILE=/tmp/zd.log ./bin/texelation`
  sees both client and server zoomdebug lines in `/tmp/zd.log`,
  prefixed `[zoom-debug client]` and `[zoom-debug server]`
  respectively.
- A user running with only `TEXELATION_DEBUG_ZOOM=1` sees no
  behaviour change versus the current implementation.
- A user running with no env var set sees no behaviour change.

## Follow-up

Once this lands, re-run the four issue-235 repros from
`docs/issue-235-repros.md` with the file env var set. Save the
captures alongside the existing server-side ones in
`docs/superpowers/captures/2026-05-06-issue-235/`. Those
client-side traces feed the symptom #2 diagnostic spec
(`docs/superpowers/specs/2026-05-06-issue-235-symptom-2-fix-design.md`)
and the symptom #1 / #4 follow-ups.

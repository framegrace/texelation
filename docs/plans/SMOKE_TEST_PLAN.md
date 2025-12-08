# Smoke & Integration Test Strategy

This plan reflects the smoke coverage we rely on today, plus near-term
enhancements we want to add.

## Current Suites

1. **Desktop headless smoke**
   - Command: `go test ./texel/...`
   - Uses the fake `ScreenDriver` to exercise workspace switching, pane splits,
     and status-pane layout. Run on every commit.

2. **Server runtime smoke**
   - Command: `go test ./internal/runtime/server`
   - Covers connection handshake, diff sequencing, snapshot persistence, and
     in-memory resume (`testutil/memconn`). Includes the offline retention
     integration test when run with `-tags=integration`.

3. **Client runtime smoke**
   - Command: `go test ./internal/runtime/client`
   - Ensures protocol handler, buffercache application, and effect bindings stay
     consistent during refactors.

4. **End-to-end stress harness**
   - Command: `go run ./cmd/texel-stress`
   - Simulates connect/run/resume cycles, large paste events, and effect
     overlays over real sockets. Used before releases and protocol changes.

5. **Headless renderer sanity**
   - Command: `go run ./client/cmd/texel-headless`
   - Validates basic rendering without opening a `tcell` screen; handy for CI
     smoke jobs.

## Near-Term Additions

| Item | Description |
|------|-------------|
| Protocol loopback | In-process server+client test asserting resume and diff replay behaviour using Unix sockets. |
| Snapshot regression | Golden snapshot JSON fixtures exercised by `snapshot_store_test.go` to detect schema drift. |
| Effect pipeline smoke | Deterministic pipeline test ensuring commonly composed effect cards (flash + fadeTint) render in the expected order. |
| Metrics watchdog | Lightweight CLI that asserts `cmd/texel-stress` reports diff backlog below configured threshold. |

## Automation

* Add a `make smoke` target that runs the desktop, client, and server packages.
* Once the loopback test lands, add a GitHub Actions workflow to execute the
  smoke target on pull requests.

Keep this document updated whenever new suites land or existing ones change.

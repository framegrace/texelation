# Phase 4 Work Status (Client Runtime)

## Completed
- Protocol now carries tree snapshots with pane geometry; server emits them on handshake/resume (`protocol/messages.go`, `server/server.go`, `server/connection.go`).
- Desktop sink and snapshot store enriched to include pane bounds, so clients can reconstruct layout (`server/desktop_sink.go`, `server/snapshot_store.go`).
- Client buffer cache understands snapshots/deltas, tracks pane rectangles, and the CLI renders multiple panes in-place while acknowledging deltas (`client/buffercache.go`, `client/cmd/texel-client/main.go`).
- Buffer cache exposes geometry-sorted panes and resume sequences now have dedicated unit coverage (`client/buffercache.go`, `client/buffercache_test.go`).
- CLI renderer respects pane rectangles during redraws to avoid linear stacking (`client/cmd/texel-client/main.go`).
- Resume scaffolding exists: `SimpleClient.RequestResume` sends `MsgResumeRequest` and the CLI uses cached sequence numbers to request snapshots/diffs.
- Resume integration test now uses a headless screen driver and sends an explicit initial snapshot before starting the connection loop, eliminating the tcell locale failure (`server/client_integration_test.go`).

## In Progress / Issues
- Clipboard/theme round-trip is wired at the protocol/event layer but the client does not yet display clipboard pulls or apply theme updates locally.
- Add deterministic multi-pane resume coverage (e.g., layout assertions) to guard against regressions.

## Next Steps
1. Add integration coverage for multi-pane resume/layout reconstruction.
2. Update the CLI to surface clipboard/theme events from the server.

---
_Last updated: 2025-10-03_

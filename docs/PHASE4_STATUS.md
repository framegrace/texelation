# Phase 4 Work Status (Client Runtime)

## Completed
- Protocol now carries tree snapshots with pane geometry; server emits them on handshake/resume (`protocol/messages.go`, `server/server.go`, `server/connection.go`).
- Desktop sink and snapshot store enriched to include pane bounds, so clients can reconstruct layout (`server/desktop_sink.go`, `server/snapshot_store.go`).
- Client buffer cache understands snapshots/deltas, tracks pane rectangles, and the CLI renders multiple panes in-place while acknowledging deltas (`client/buffercache.go`, `client/cmd/texel-client/main.go`).
- Buffer cache exposes geometry-sorted panes and resume sequences now have dedicated unit coverage (`client/buffercache.go`, `client/buffercache_test.go`).
- CLI renderer respects pane rectangles during redraws to avoid linear stacking (`client/cmd/texel-client/main.go`).
- CLI now surfaces clipboard and theme updates received from the server (`client/cmd/texel-client/main.go`).
- Buffer cache now preserves styled cell data, prunes panes missing from new snapshots, and exercises the richer path in unit tests (`client/buffercache.go`, `client/buffercache_test.go`).
- Remote renderer draws server-provided buffers with full styling, applies live theme defaults, and passes updated geometry tests (`client/cmd/texel-client/main.go`, `client/cmd/texel-client/main_test.go`).
- Server capture path now skips visual-effect overlays so pane diffs contain raw app output (`texel/pane.go`, `texel/snapshot.go`).
- Protocol gained `MsgStateUpdate`; connections forward desktop state and the remote renderer consumes it for status lines and control-mode overlays (`protocol/messages.go`, `server/connection.go`, `client/cmd/texel-client/main.go`).
- Remote renderer now re-applies inactive pane dimming based on streamed focus state so non-focused panes stay visually subdued (`client/cmd/texel-client/main.go`).
- Resume scaffolding exists: `SimpleClient.RequestResume` sends `MsgResumeRequest` and the CLI uses cached sequence numbers to request snapshots/diffs.
- Resume integration test now uses a headless screen driver and sends an explicit initial snapshot before starting the connection loop, eliminating the tcell locale failure (`server/client_integration_test.go`).

## In Progress / Issues
- Verify remaining desktop-only effects (pane resize highlight, zoom overlays) and port them as needed.

## Next Steps
1. Extend parity checks to pane resize/zoom treatments once control-mode fade stabilises.
2. Re-validate Phase 4 parity before moving on to offline retention work.

---
_Last updated: 2025-10-03 (session resumed)_

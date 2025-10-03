# Phase 4 Work Status (Client Runtime)

## Completed
- Protocol now carries tree snapshots with pane geometry; server emits them on handshake/resume (`protocol/messages.go`, `server/server.go`, `server/connection.go`).
- Desktop sink and snapshot store enriched to include pane bounds, so clients can reconstruct layout (`server/desktop_sink.go`, `server/snapshot_store.go`).
- Client buffer cache understands snapshots/deltas, tracks pane rectangles, and the CLI renders multiple panes in-place while acknowledging deltas (`client/buffercache.go`, `client/cmd/texel-client/main.go`).
- Resume scaffolding exists: `SimpleClient.RequestResume` sends `MsgResumeRequest` and the CLI uses cached sequence numbers to request snapshots/diffs.
- Resume integration test now uses a headless screen driver and sends an explicit initial snapshot before starting the connection loop, eliminating the tcell locale failure (`server/client_integration_test.go`).

## In Progress / Issues
- Layout reconstruction on the CLI is geometry-based but still linear; next step is to tile panes according to their rectangles and update on further deltas.
- Clipboard/theme round-trip is wired at the protocol/event layer but the client does not yet display clipboard pulls or apply theme updates locally.

## Next Steps
1. Add client-side tests around `BufferCache.ApplySnapshot` plus simulated resume sequences to ensure diffs apply after resume.
2. Enhance rendering so panes appear according to their recorded rectangles (respect width/height) instead of sequential stacking; update CLI to react to clipboard/theme events once available.

---
_Last updated: 2025-10-03_

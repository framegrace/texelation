# Phase 5 Notes

## Offline Resume Harness Ideas
- Build a `memConn` test helper that implements `net.Conn` over buffered channels so we can exercise the server connection loop without relying on Unix domain sockets (forbidden in CI sandbox).
- The helper would expose paired endpoints similar to `net.Pipe`, but with bounded channels so writes never block indefinitely when the test goroutine pauses.
- Use the helper inside an integration test that:
  1. Runs `handleHandshake` using a background goroutine.
  2. Seeds retained diffs by calling `DesktopPublisher.Publish` while no client is connected.
  3. Replays the resume handshake with controlled message scripting, asserting snapshots/deltas arrive and acknowledgements drain the queue.

## Boot Snapshot Replay Thoughts
- Current boot snapshot cache only feeds outbound snapshots. To fully restore desktop state, we need a hook that applies `protocol.TreeSnapshot` to a new `Desktop` instance before accepting clients.
- Potential approach: extend `Desktop` with a method that can accept `protocol.PaneSnapshot` data and rehydrate pane buffers (likely requires temporary app placeholders or app persistence metadata).
- Ensure rehydration happens before the first `Publish()` so initial diffs reflect the restored layout, not a fresh welcome screen.
- Prototype idea: add `desktop.ApplyTreeSnapshot(protocol.TreeSnapshot)` that seeds panes with lightweight placeholder apps rendering the stored buffers until the real apps reconnect; would require pane/app metadata in future phases.

## Open Questions
- How to persist per-pane app metadata (commands, shells) alongside buffer snapshots for full recovery?
- Should resume acknowledgements emit structured logs for easier ingestion into metrics backends?

_Last updated: 2025-10-03_

# Texelation Client/Server Architecture

This document captures the current architecture of the remote Texelation runtime
(Phase 4 client parity). It complements `CLIENT_SERVER_PLAN.md` and focuses on
module responsibilities, data flow, and operational workflows.

## High-Level Overview

```
+-----------------+      +------------------+      +----------------+
| Remote Client   |      | Unix Socket IPC |      | Server Runtime |
| (tcell front-end+------+  (protocol.go)   +------+  (desktop, apps) |
+-----------------+      +------------------+      +----------------+
        |                         |                        |
        |   MsgHello/Welcome      |                        |
        |------------------------>|                        |
        |   MsgConnect/Accept     |                        |
        |<------------------------|                        |
        |   MsgTreeSnapshot/Delta |                        |
        |<------------------------|                        |
        |   MsgKey/Mouse/Resize   |                        |
        |------------------------>|                        |
```

At its core the client is a thin renderer that applies the same buffer
snapshots the desktop used locally. The authoritative state (pane tree,
buffers, app lifecycle) remains in the server. Communication is via framed
messages defined in `protocol/`.

## Module Map

### Client

```
client/
  buffercache.go         // Applies TreeSnapshot/BufferDelta to local pane cache
  cmd/texel-client/      // Remote renderer (tcell screen)
  simple_client.go       // Handshake/resume helper (shared by tools)
```

Key responsibilities:

- Manage a `BufferCache` with pane geometry, styled cells, and active/resizing
  flags received from the server.
- Translate `MsgTreeSnapshot`, `MsgBufferDelta`, `MsgStateUpdate`, `MsgPaneState`,
  and `MsgResize` into local state and visuals.
- Emit `MsgKeyEvent`, `MsgMouseEvent`, `MsgResize`, `MsgClipboardGet/Set`, and
  `MsgThemeUpdate` back to the server.
- Render using `tcell` simulation screen (same primitives as the legacy desktop).

### Protocol

`protocol/` defines message framing (`protocol.go`) and payload encoders/decoders
(`messages.go`). Important message types for the remote flow:

- `MsgTreeSnapshot`: Full pane tree & buffers on connect/resume.
- `MsgBufferDelta`: Per-pane cell updates (canonical data path).
- `MsgStateUpdate`: Global desktop state (control mode, zoomed pane, workspace).
- `MsgPaneState`: Per-pane flag updates (active, resizing, etc.).
- `MsgResize`: Client terminal dimensions.
- Input & control messages (`MsgKeyEvent`, `MsgMouseEvent`, clipboard, theme).

### Server

```
server/
  server.go              // Session manager, socket accept loop
  connection.go         // Per-client connection state & message loop
  desktop_sink.go       // Bridges protocol events to the local Desktop
  desktop_publisher.go  // Captures pane snapshots -> BufferDelta
  tree_convert.go       // Desktop tree <-> protocol snapshot conversion
  session.go            // Sequencing, buffering, ack handling
  snapshot_store.go     // Load/save persisted desktop snapshots (JSON today)
```

Responsibilities:

- Maintain a running `texel.Desktop` identical to the local runtime: app
  lifecycle, pane layout, effects (now minimal), status panes.
- For each client connection, run `connection.serve()` which:
  - Performs the handshake (`Hello/Welcome/Connect`).
  - Streams `MsgTreeSnapshot` + subsequent `MsgBufferDelta` updates via
    `DesktopPublisher`.
  - Receives input/control messages and forwards them through `DesktopSink`.
  - Handles client resizes by calling `Desktop.SetViewportSize` and immediately
    pushing a fresh snapshot so the client re-renders with the new geometry.
- Track diff history per session (`session.go`) so reconnecting clients can
  resume from the last acked sequence.
- On startup the server loads any persisted desktop snapshot (via
  `snapshot_store.go`) before accepting clients, and it persists updates back to
  disk on schedule or structural changes.

### Desktop (texel/)

The existing desktop codebase is still the authoritative source of pane layout
logic. Important updates for the remote flow:

- `Desktop.SetViewportSize(cols, rows)`: overrides the size used in layout
  calculations so the server can match the remote terminal.
- `Desktop.PaneStates()`: exposes active/resizing flags for each pane.
- Animations (control mode fade, inactive/resizing tints) are disabled server
  side—those are handled by the client for responsiveness.

## Workflow Walkthroughs

### Connect / Initial Render

```
Client                               Server
  |                                    |
  | MsgHello ------------------------> |
  |                        MsgWelcome  |
  | <--------------------------------- |
  | MsgConnectRequest ---------------->|
  |                        MsgConnectAccept
  | <--------------------------------- |
  |                        MsgTreeSnapshot
  | <--------------------------------- |
  | apply snapshot + render            |
  |                                    |
```

- The client establishes the Unix domain socket connection and drives the
  `Hello`/`Connect` handshake using helpers in `client/simple_client.go`.
- `server.connection` responds with `MsgTreeSnapshot`, generated by
  `desktop_publisher.go` (`Desktop.SnapshotBuffers` + `tree_convert.go`).
- The client populates its `BufferCache` and renders immediately.
- Right after `screen.Init()` the client sends `MsgResize` with the actual
  terminal size so the server can recompute layout (`Desktop.SetViewportSize`).
  The server reacts by sending another snapshot/delta for the new geometry.

### Steady-State Updates

```
[Server Desktop] --capture--> BufferDelta --enqueue--> Session
Session.Pending --> connection.sendPending --> client ApplyDelta --> render
```

- `DesktopPublisher.Publish()` runs after any desktop change (input, resize,
  timed publish) and converts pane buffers into `MsgBufferDelta` frames.
- `server/session.EnquireDiff` assigns monotonically increasing sequence numbers
  and buffers them until acknowledged.
- `connection.sendPending()` pulls queued `DiffPacket`s and writes them to the
  socket. The client responds with `MsgBufferAck`, allowing the session to drop
  older diffs.
- Additional control frames:
  - `MsgStateUpdate` whenever control mode, workspace, or zoom state changes.
  - `MsgPaneState` whenever a pane's active/resizing flag flips.

### Input / Control Flow

```
Client UI -> MsgKeyEvent/MsgMouseEvent/MsgResize -> connection.serve()
  -> DesktopSink.Handle* -> Desktop.* handlers -> possible state updates
```

Concrete paths:

- Keys: `connection` decodes `MsgKeyEvent` and calls `DesktopSink.HandleKeyEvent`
  which forwards them through `Desktop.InjectKeyEvent`. Control-mode toggles,
  splits, zoom, etc. trigger state updates that are published back via 
  `DesktopPublisher + connection` (and optimistically applied client-side for
  responsiveness).
- Mouse: delivered to `Desktop.InjectMouseEvent` (still used by status panes,
  future selections).
- Resize: `MsgResize` -> `Desktop.SetViewportSize` -> immediate snapshot and
  publish.

### Resume Workflow

```
Client reconnects
  -> MsgResumeRequest(lastSeq)
     <- connection Replay: TreeSnapshot + Pending diffs beyond lastSeq
```

- On reconnect the client sends `MsgResumeRequest` with the last acked sequence.
- The server replays the latest snapshot (`Snapshot()`), followed by diff history
  newer than `lastSeq`. This is handled in `connection.serve()` after handshake.
- Normal delta streaming resumes once the backfill completes.

## Data Flow Summary

```
+----------------+     +------------------+     +--------------------+
| Desktop (Go)   | --> | DesktopPublisher | --> | Session (buffer &  |
| - Pane tree    |     | - Snapshots      |     |   sequencing)      |
| - Apps         |     | - BufferDelta    |     +---------+----------+
+--------+-------+     +--------+---------+               |
         ^                        |                       v
         |                        |            +----------------------+
         |                        +------------> connection (per client)
         |                                      - Read/Write protocol
         |                                      - Track resize/state
         |                                      - Drive DesktopSink
         |                                      - Send pending diffs
         +--------------------------------------+
```

## Feature Matrix

| Feature                     | Server Responsibility                            | Client Responsibility                      |
| --------------------------- | ------------------------------------------------- | ------------------------------------------- |
| Pane lifecycle/layout       | `Desktop` (apps, split/zoom, status panes)        | Render buffers delivered via protocol       |
| Buffer diffing              | `DesktopPublisher` (snapshot -> delta conversion) | Apply deltas (`client.BufferCache`)         |
| Active/resizing highlights  | Flag state & broadcast (`MsgPaneState`)           | Visual overlay + status lines               |
| Control mode                | Toggle & state publish                            | Visual overlay, optimistic toggling         |
| Resize                      | `Desktop.SetViewportSize`, fresh snapshot         | Measure terminal, send `MsgResize`          |
| Clipboard                   | `Desktop` handles store/fetch                     | Send `MsgClipboardSet/Get/Data`             |
| Theme updates               | `Desktop.HandleThemeUpdate` -> publish updates    | Apply `MsgThemeUpdate/Ack` to local style   |
| Stress harness              | `cmd/texel-stress` simulates connect/run/resume   | (Reader only) consumes stream for metrics   |

## Reference Documents

- `CLIENT_SERVER_PLAN.md`: Phase roadmap and milestones.
- `docs/PHASE4_STATUS.md`: Progress log with the latest parity notes.
- `docs/PHASE8_STATUS.md`: Performance tuning goals (useful when profiling).

Feel free to extend this document as new protocol messages or components are
added. An accurate diagram makes future manual modifications much simpler.

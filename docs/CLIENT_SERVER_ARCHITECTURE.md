# Texelation Client/Server Architecture

This document describes the **current** client/server runtime as it exists
today. Earlier phase and migration notes have been retired; this page should be
considered the single source of truth for how the remote desktop works, how the
modules interact, and which gaps we still plan to close.

---

## 1. High-Level Topology

```
┌──────────────────────┐        ┌────────────────┐        ┌──────────────────────┐
│ Remote Client (tcell │        │  Unix Socket   │        │   Server Runtime     │
│ renderer + effects)  ├────────┤  protocol.go   ├────────┤  (texel Desktop +    │
└──────────┬───────────┘        └────────┬───────┘        │   apps / sessions)   │
           │ Handshake (Hello/Connect)    │                └──────────┬───────────┘
           │ Buffer streaming (Tree/Delta)│                           │
           │ Input/control (Key/Mouse/etc)│                           │
           ▼                              ▼                           ▼
    `internal/runtime/client`     `protocol/messages.go`     `internal/runtime/server`
```

* The **server** owns the authoritative `texel.Desktop`, pane tree, app
  lifecycles, and persisted snapshot history.
* The **client** is a thin renderer. It mirrors buffers into a local
  `BufferCache`, applies visual effects, and pushes input/control events back to
  the server.
* Communication happens over framed protocol messages defined in
  `protocol/messages.go`. The framing supports resume by sequence number so
  clients can reconnect without losing buffered diffs.

---

## 2. Module Responsibilities

### 2.1 Client (`internal/runtime/client`)

| Package/File                     | Responsibility |
| -------------------------------- | -------------- |
| `app.go` (now ~100 lines)        | Entry-point wiring that scaffolds the runtime and delegates to focused modules. |
| `renderer.go`                    | Applies `BufferCache` contents to the `tcell` screen. |
| `buffercache.go`                 | Maintains pane geometry & styled cells based on `MsgTreeSnapshot` and `MsgBufferDelta`. |
| `protocol_handler.go`            | Decodes inbound frames, drives `clientState`, schedules re-renders. |
| `input_handler.go`               | Converts keyboard/mouse/paste events into protocol messages; performs optimistic UI updates (control mode overlay). |
| `message_sender.go`              | Encodes outbound messages; centralises error handling/retry semantics. |
| `background_tasks.go`            | Ping/ack loops, session liveness tracking. |
| `ui_state.go`                    | Aggregates server state (workspace, theme defaults, focus) and resolves effect bindings. |
| `effects/`                       | Effect registry used by both the remote client and the new card layer. |

Client-side optimisations:

* **Delta batching** – multiple `MsgBufferDelta` frames can be applied in a
  single render pass; the renderer only invalidates the rows that changed.
* **Optimistic control mode** – overlays are displayed immediately while the
  authoritative active/zoom state is confirmed via `MsgStateUpdate`.
* **Effect timeline integration** – the same easing/timeline helpers power
  client overlays and card-based effects, so animations remain smooth and
  consistent.

### 2.2 Protocol (`protocol/`)

* `protocol.go` – frame header (type, sequence, payload length) and CRC helpers.
* `messages.go` – concrete payload encoders/decoders. This layer is deliberately
  boring to keep compatibility stories straightforward.
* Message types important to the remote flow:
  - `MsgTreeSnapshot` – full pane tree & buffers (sent on connect/resume).
  - `MsgBufferDelta` – row-based cell updates streamed continuously.
  - `MsgStateUpdate`, `MsgPaneState` – broadcast desktop flags.
  - `MsgResize`, `MsgClipboard{Get,Set,Data}`, `MsgThemeUpdate/Ack`.
  - `MsgBufferAck` – lets the server trim diff history safely.

### 2.3 Server (`internal/runtime/server`)

| Package/File                | Responsibility |
| --------------------------- | -------------- |
| `server.go`                 | Socket accept loop, snapshot load/save orchestration. |
| `connection.go`             | Per-client state machine (handshake, streaming, resume). |
| `session.go`                | Sequencing, diff buffering, ack accounting. |
| `desktop_publisher.go`      | Converts desktop buffers into protocol deltas. |
| `desktop_sink.go`           | Applies inbound control/input to the desktop. |
| `tree_convert.go`           | Desktop tree ↔ protocol snapshot conversion. |
| `snapshot_store.go`         | Persistence of desktop snapshot JSON blobs. |
| `testutil/`                 | In-memory connections used in integration tests. |

Server optimisations:

* **Back-pressure aware streaming** – diffs are buffered per session until the
  client acks them; `session.go` enforces a cap to avoid unbounded growth.
* **Resume safety** – reconnecting clients send their last acked sequence
  number; the server replays buffered diffs after a snapshot so the client
  catches up without losing intermediate updates.
* **Viewport control** – each connection feeds remote terminal dimensions into
  `Desktop.SetViewportSize`, ensuring layout decisions (splits, status panes)
  match the client.
* **Snapshot persistence** – the latest tree+buffer snapshot survives restarts,
  shrinking reconnect time and enabling future crash recovery tooling.

### 2.4 Cards & Effects (`texel/cards`, `internal/effects`)

* `texel/cards/effect_card.go` adapts any registry effect into the card
  pipeline. Configuration mirrors theme JSON (duration, colors, trigger
  semantics, max intensity, etc.). Cards register control bus triggers of the
  form `effects.<effectID>` (e.g. `cards.FlashTriggerID`).
* Legacy card implementations (`FlashCard`, `RainbowCard`) have been removed in
  favour of the unified adapter.
* The effect registry (`internal/effects`) exposes:
  - `Effect` interface (`ID`, `Active`, `Update`, `HandleTrigger`,
    `ApplyWorkspace`, `ApplyPane`).
  - A shared `Timeline` easing helper used by the client runtime and cards.
  - Helpers for default-colour conversion so effects behave consistently even
    when apps emit `tcell.ColorDefault`.

---

## 3. Runtime Workflows

### 3.1 Connect

1. Client connects to the Unix socket and exchanges `MsgHello`/`MsgWelcome`.
2. Client sends `MsgConnectRequest` (or `MsgResumeRequest` if reconnecting).
3. Server responds with `MsgConnectAccept` followed by a full `MsgTreeSnapshot`.
4. Client populates its `BufferCache`, triggers an initial render, then sends
   `MsgResize` with the actual terminal size.
5. Server applies the resize (layout is recalculated instantly) and emits a new
   snapshot/delta reflecting the remote viewport.

### 3.2 Steady-State Streaming

```
Desktop mutation → DesktopPublisher → Session.enqueue(seq, delta)
   └─ connection.sendPending → protocol stream → client ApplyDelta → render
```

* The publisher runs after any desktop change (input, app update, timed tick).
* Diff packets are sequenced and buffered until acknowledged by the client.
* Client acks via `MsgBufferAck(lastSeq)`; the session drops older packets.

### 3.3 Input & Control

* Keys (`MsgKeyEvent`) feed into `DesktopSink.HandleKeyEvent`. Control-mode
  toggles, splits, etc. update desktop state which is broadcast back via
  `MsgStateUpdate`.
* Mouse (`MsgMouseEvent`) supports selection/status panes.
* Resizes feed into `Desktop.SetViewportSize` and immediately trigger a fresh
  snapshot.
* Clipboard traffic flows through `MsgClipboard{Get,Set,Data}`; the server owns
  the clipboard store so multiple clients remain consistent.

### 3.4 Resume

* Client reconnects with `MsgResumeRequest(lastAckedSeq)`.
* Server responds with a snapshot followed by each buffered diff newer than the
  sequence the client already rendered.
* Normal streaming resumes once the backfill completes.

---

## 4. Operational Notes & Tooling

* **Stress harness** (`cmd/texel-stress`) simulates clients to validate reconnect
  logic and delta batching under load.
* **Headless client** (`client/cmd/texel-headless`) decodes protocol streams in
  CI without a `tcell` screen.
* **Snapshot store** writes JSON blobs; a follow-up project will add rotation and
  change auditing.
* **Telemetry** (TODO) – currently logging-driven; future work should add wall
  clock metrics around diff queue depth and reconnect latency.

---

## 5. Future Improvements & Open Items

A consolidated backlog lives in `docs/FUTURE_ROADMAP.md`. Items from that list
that directly impact the architecture above include:

1. **Control mode handler extraction** – move the control-mode state machine out
   of `DesktopEngine` for clarity and easier testing.
2. **Client runtime modularisation** – complete the refactor of
   `internal/runtime/client` into focused packages.
3. **Snapshot store enhancements** – add rotation, metrics, and hazard logging
   so operations can monitor reconnect behaviour.
4. **Effect layering** – support ordered overlays (e.g. fadeTint + flash) via
   the effect manager without bespoke card composition.
5. **Diagnostics hooks** – surface diff backlog/latency metrics via a lightweight
   `/debug` endpoint.

Update this section whenever architecture-affecting work lands.

---

## 6. Quick Reference

| Capability              | Where to start                              |
| ----------------------- | ------------------------------------------- |
| Client handshake        | `internal/runtime/client/session.go`        |
| Delta publishing        | `internal/runtime/server/desktop_publisher.go` |
| Resume sequence logic   | `internal/runtime/server/session.go`        |
| Effect registration     | `internal/effects/registry.go`              |
| Effect card usage       | `texel/cards/effect_card.go`                |
| Protocol definitions    | `protocol/messages.go`                      |
| Snapshot persistence    | `internal/runtime/server/snapshot_store.go` |

Keep this document up to date whenever modules move or behaviour changes. A
clear architecture reference is critical now that the old migration phases have
been completed.

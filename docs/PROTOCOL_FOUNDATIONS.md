# Protocol Foundations

This document captures the **implemented** wire format used by the Texelation
client/server runtime. It supersedes the old phase planning notes and should be
kept up to date alongside `protocol/messages.go`.

## Transport & Framing

* **Transport** – Unix domain sockets (`/tmp/texelation.sock`) by default.
  Dial/listen helpers make it easy to swap to TCP/WebSockets in future.
* **Header layout** (little endian):

| Field        | Size | Notes |
|--------------|------|-------|
| `magic`      | 4    | Constant `0x54584c01` (`TXL\x01`). Guards against peers speaking another protocol. |
| `version`    | 1    | Schema version. Current runtime negotiates `0`. |
| `msg_type`   | 1    | Enumerated payload type (`MsgHello`, `MsgBufferDelta`, ...). |
| `flags`      | 1    | Bitmask reserved for compression/priority (0 today). |
| `reserved`   | 1    | Alignment / future use. |
| `session_id` | 16   | UUID identifying the desktop session. |
| `sequence`   | 8    | Monotonically increasing per session. Drives resume/ack logic. |
| `payload_len`| 4    | Length of payload in bytes. |
| `checksum`   | 4    | CRC32 (IEEE) of header (sans magic) + payload. A zero checksum disables the check. |

The header precedes the payload. `protocol.ReadMessage` and `WriteMessage`
reuse buffers internally to minimise allocations.

## Message Families

| Category             | Messages (Go enums)                                        | Notes |
|----------------------|------------------------------------------------------------|-------|
| Handshake            | `MsgHello`, `MsgWelcome`, `MsgPing`, `MsgPong`             | Wiring happens in `client/simple_client.go` and `server/connection.go`. |
| Session lifecycle    | `MsgConnectRequest`, `MsgConnectAccept`, `MsgResumeRequest`, `MsgDisconnectNotice` | Resume includes last acked sequence. |
| Snapshot & layout    | `MsgTreeSnapshot`, `MsgTreeDelta` (currently unused)       | Snapshot contains full pane tree + buffers. |
| Buffer streaming     | `MsgBufferDelta`, `MsgBufferAck`                           | Per-pane diff plus ack for pruning history. |
| State broadcasts     | `MsgStateUpdate`, `MsgPaneState`                           | Control mode, workspace, zoom, active/resizing flags. |
| Input & clipboard    | `MsgKeyEvent`, `MsgMouseEvent`, `MsgResize`, `MsgClipboard{Get,Set,Data}` | Two-way traffic; clipboard data can be binary-safe. |
| Theme & effects      | `MsgThemeUpdate`, `MsgThemeAck`                            | Keeps client palette/effect config aligned. |
| Diagnostics (future) | `MsgError`, `MsgMetricUpdate` (reserved)                   | Not emitted yet; placeholders in the enum. |

Message IDs stay below 128 to reserve the high bit for experimental extensions.

## Buffer Delta Encoding

`MsgBufferDelta` is the main payload streamed to clients:

* `pane_id` (`[16]byte`) – matches the UUID in the tree snapshot.
* `revision` (`uint32`) – incremented server-side for idempotency.
* `rows` – encoded as a list of row mutations:
  - `row_index` (`uint16`).
  - `span_count` (`uint16`).
  - For each span: `start_col`, `length`, `runes` (UTF-8), `style_index`.
* `style_palette` – table of `tcell.Style` entries (see below) referenced by
  spans via index to avoid repeating colour data.

### Style Serialization

Styles are encoded compactly:

| Field        | Size | Description |
|--------------|------|-------------|
| `attr_flags` | 1    | Bold, underline, reverse, blink, dim, italic. |
| `fg_model`   | 1    | `0` = default, `1` = ANSI, `2` = RGB. |
| `fg_value`   | 4    | Packed RGB or palette index. |
| `bg_model`   | 1    | Same scheme as foreground. |
| `bg_value`   | 4    | Packed RGB or palette index. |

The decoder converts these into `tcell.Style` when applying deltas. Palette
entries default to true RGB colours when effects blend with theme defaults.

## Sequencing & Resume

* Every frame increments the session `sequence`.
* Clients send `MsgBufferAck(ackSeq)` after applying a delta; servers drop all
  buffered diffs ≤ `ackSeq`.
* On reconnect the client sends `MsgResumeRequest` with the last acknowledged
  sequence. The server responds with:
  1. `MsgConnectAccept` (same as initial handshake).
  2. `MsgTreeSnapshot` (always sent to re-establish tree state).
  3. All buffered `MsgBufferDelta` frames with `sequence > ackSeq`.

If no diffs remain the client simply receives an empty stream after the
snapshot.

## Implementation Notes & Tests

* `protocol/messages_test.go` covers header framing, CRC validation, and short
  reads. Add regression cases there when extending the wire format.
* For buffer encoding/decoding, use the fixtures in `internal/runtime/server/testutil`
  and `client/buffercache_test.go`.
* `cmd/texel-stress` contains helpers that simulate reconnect storms and large
  clipboard transfers. Use it before changing sequencing semantics.

## Future Protocol Enhancements

1. **Compression flag** – evaluate a per-frame compression bit for massive
   buffers (useful for large background images or traces).
2. **Batched deltas** – allow multiple panes in a single `MsgBufferDelta` to
   reduce framing overhead during split-screen refresh bursts.
3. **Binary clipboard** – extend clipboard messages with chunked streaming to
   support file payloads.
4. **Metrics channel** – wire up `MsgMetricUpdate` for live telemetry
   (diff backlog, encode latency) without scraping logs.

Track these items in `docs/FUTURE_ROADMAP.md` once scoped.

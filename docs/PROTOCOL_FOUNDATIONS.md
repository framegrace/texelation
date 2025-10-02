# Protocol Foundations (Phase 2)

## Transport & Framing
- **Transport**: Unix domain sockets (`/tmp/texelation.sock`) for initial implementation; abstractions should allow swapping the dial/listen layer later (TCP/WebSocket).
- **Frame structure** (little endian):
  - `magic` (4 bytes) = `0x54584c01` (`TXL\x01`) guards against mismatched peers.
  - `version` (1 byte) = protocol schema version (start at `0`).
  - `msg_type` (1 byte) enumerating payload semantics.
  - `flags` (1 byte) bitmask (compression, ack required, etc.).
  - `reserved` (1 byte) for alignment/future expansion.
  - `session_id` (16 bytes UUID).
  - `sequence` (8 bytes) monotonically increasing per session.
  - `payload_len` (4 bytes) length of payload.
  - `checksum` (4 bytes) CRC32 of header (sans magic) + payload; `0` disables.

All fields precede the payload blob. Zero-copy decode by slicing into the incoming byte buffer where possible.

## Message Taxonomy
- **Control**: `HELLO`, `WELCOME`, `PING`, `PONG`, `GOODBYE`.
- **Session lifecycle**: `CONNECT_REQUEST`, `CONNECT_ACCEPT`, `RESUME_REQUEST`, `RESUME_DATA`, `DISCONNECT_NOTICE`.
- **Display tree**: `TREE_SNAPSHOT`, `TREE_DELTA` (structural changes including node IDs, split ratios).
- **Buffer updates**: `BUFFER_DELTA` streaming per-pane diffs; `BUFFER_ACK` to trim server history.
- **Input/events**: `KEY_EVENT`, `MOUSE_EVENT`, `CLIPBOARD_SET`, `CLIPBOARD_GET`, `THEME_UPDATE`.
- **Diagnostics**: `ERROR` (with codes), `METRIC_UPDATE` for optional telemetry.

Message types stay below 128 to reserve top bit for experimental/extension frames.

## Buffer Delta Encoding
- Pane buffer stored row-major. Deltas transmit:
  - `pane_id` (16 bytes UUID)
  - `revision` (uint32) incremented server-side.
  - `first_row` (uint16) + run count.
  - For each row run: `row_index` + `cell_span_count` + spans.
  - Each span: `start_col` (uint16), `end_col` (uint16), `attrs` (see below), UTF-8 for rune data.
- Styles compressed via palette table per message: header includes `style_count`, followed by encoded `tcell.Style` triplets (flags, fg RGB, bg RGB). Spans reference palette index (uint8/uint16 depending on size) + rune(s).
- Allow optional run-length encoding for repeated spaces with identical style (flagged via `flags`).

## tcell.Style Serialization
- Compose bitfield:
  - `attr_flags` (uint8): bold, underline, reverse, blink, dim, italic.
  - `fg_model` (uint8) + `fg_value` (either RGB24 or color ID).
  - `bg_model` (uint8) + `bg_value`.
- Align on 4-byte boundary; keep encoding simple enough for server/client to convert without instantiating tcell objects until necessary.

## Sequencing & Reliability
- Server maintains per-pane revision history and per-session sequence numbers.
- Client acknowledges highest contiguous sequence via `BUFFER_ACK` to let server prune history.
- On reconnect, client sends `RESUME_REQUEST` with last acknowledged sequence + snapshot hash to receive either `RESUME_DATA` (diff) or `TREE_SNAPSHOT` (full reset).

## Implementation Notes
- Introduce `protocol` Go package with:
  - `MessageType` enum, header struct, and `ReadMessage`/`WriteMessage` streaming helpers.
  - `Encoder`/`Decoder` that reuse buffers (sync.Pool) to minimise allocations.
  - Unit tests for header validation, CRC failures, short reads, etc.
- Provide fixture-driven tests for buffer deltas once serialization format implemented.
- Benchmark `EncodeBufferDelta` with representative pane content (80x24). Target <150µs encode on dev machine.

## Open Questions
- Compression toggle per frame or negotiated upfront?
- Should we allow multiplexing multiple panes per frame for batching?
- Strategy for clipboard-files or binary attachments (maybe dedicated `BLOB` message with streaming chunks).

These decisions can evolve, but the initial implementation should cover control, lifecycle, and buffer deltas with clear extension hooks.

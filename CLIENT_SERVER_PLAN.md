# Texelation Client/Server Migration Plan

## Context & Goals
- Split the current monolithic desktop runtime into a long-running server that hosts apps/panes and a lightweight client that renders view state, similar to tmux.
- Maintain uninterrupted app execution while clients disconnect/reconnect; preserve visual state through buffer diffs and periodic tree snapshots.
- Begin with Unix domain sockets for all IPC; design abstractions so TCP can be added later without rewriting higher layers.

## Phase 0 – Baseline Stabilization
- Freeze behaviour with regression checkpoints (screenshots, logs, buffer dumps) for later comparison.
- Ensure `make build`, `make test`, and `make lint` succeed; add smoke tests if missing.
- Document the current desktop/event architecture to align collaborators on existing responsibilities.

## Phase 1 – Component Boundary Definition
- Extract interfaces for rendering (`Renderer`), buffer management (`BufferStore`), event routing (`EventBus`), and app lifecycle (`AppRuntime`).
- Refactor `texel/desktop`, `texel/pane`, and `apps/*` to depend on these interfaces instead of concrete types.
- Add unit coverage around buffer mutations, pane split/resize operations, and theme defaults so later refactors are guarded.

## Phase 2 – Protocol Foundations
- Define versioned binary protocol framing (header with type, payload length, session ID, checksum) operating over Unix sockets.
- Model buffer deltas as row-based patches with run-length encoding for cell styles; include serialization helpers for `tcell.Style`, runes, and attributes.
- Add message types for events (keyboard/mouse), lifecycle (connect, ack, ping), pane tree sync, and snapshot transfer.
- Implement encoder/decoder utilities with benchmarks targeting low allocation and high throughput.

## Phase 3 – Server Runtime Extraction
- Create `server` package that instantiates the current desktop/app stack, accepts UDS connections, and handles session lifecycle. *(done via `server` package + CLI harness)*
- Maintain authoritative pane tree and buffers server-side; broadcast diffs to connected clients and queue updates when none are attached. *(realised with `DesktopPublisher` and integration tests)*
- Persist pane tree snapshots and per-pane metadata on interval (JSON or binary) with integrity checks to detect corruption. *(implemented snapshot store & timer loop)*

## Phase 4 – Client Runtime
- Implement `client` binary that connects via UDS, performs handshake, requests latest snapshot, and applies buffered diffs to local cache.
- Reuse existing `tcell` renderer but feed it from the local buffer cache; ensure resize events and input routing go through the protocol layer.
- Handle reconnects with backoff; on reconnect, request snapshot + incremental diffs since last acknowledged sequence number.
- Target UI parity with the local desktop so the standalone client becomes the primary renderer once Phase 6 completes (borders, splits, status panes).

## Phase 5 – Offline Operation Guarantees
- Ensure server continues running apps with no clients: queue outbound diffs, compact history, enforce retention limits.
- On resume, client requests snapshot at latest version; server prunes queued diffs once client acknowledges receipt.
- Add monitoring hooks to track queue sizes and dropped updates.

## Phase 6 – Signal & Event Plumbing
- Route all input/output signals (keyboard, mouse, focus, theme updates, clipboard) through the protocol.
- Provide acknowledgement pathways for critical events (pane creation/destruction) to keep state synchronized.
- Expand integration harnesses under `cmd/` to script disconnect/reconnect flows and verify event delivery end-to-end.

## Phase 7 – Persistence & Recovery
- Finalize snapshot format for display tree and buffers; include schema versioning for forward compatibility.
- Implement server boot sequence that loads last known-good snapshot before accepting clients; add tooling to inspect/repair snapshots.
- Schedule periodic persistence (e.g., every N seconds or after structural change) and document operational tuning.

## Phase 8 – Performance Tuning & Hardening
- Profile server/client under high-frequency updates; optimize serialization, diff batching, and network I/O.
- Introduce feature flags for experimental protocol changes; add CI steps running protocol benchmarks and long-lived soak tests.
- Capture metrics (latency, throughput, queue depth) to validate tmux-like responsiveness and guide future TCP support.

## Phase 10 – Final Client/Server Release
- Graduate the server harness (now `texel-server`) and remote CLI into the production binaries, rename CLIs, and align packaging/scripts with the new entry points.
- Port any remaining desktop-only UX polish (effects, status integrations, shortcuts) into the server/client pair to match the original monolith.
- Harden distribution assets: update docs, release notes, and build artifacts so remote mode becomes the default developer workflow.
- Capture operational runbooks covering snapshot recovery, diff retention tuning, and monitoring hooks to support production deployment.

## Future Considerations
- Abstract transport layer so TCP/WebSocket backends can be slotted in later.
- Explore compression strategies (zstd delta, dictionary-based) if diffs grow large.
- Plan for multi-client viewing (read-only observers) once single-client flow stabilizes.
- Support mapping individual client workspaces to arbitrary server workspaces so a single client can follow multiple servers (or multiple server workspaces) concurrently.
- Investigate running each app inside an isolated container/VM so Texelation server restarts do not terminate the processes; restart would simply reconnect to the long-lived container (aligns with Phase 5 resiliency goals).
- Explore CRIU/container checkpoint/restore integration to snapshot app state periodically or on shutdown, enabling fast resume even after host reboot.

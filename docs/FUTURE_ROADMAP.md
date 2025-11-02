# Texelation Future Roadmap

This document aggregates the architectural, testing, and tooling ideas that
were previously scattered across several planning memos. Items are grouped by
theme; update their status as work lands or priorities change.

---

## 1. Architectural Backlog

### 1.1 Plugin & App Extensibility
- **Dynamic app plugins** – Introduce an `AppPlugin` interface so apps can be
  discovered and configured at runtime (`Create(config)`, metadata, capability
  flags).
- **Process adapters** – Provide wrappers that expose external commands as
  `texel.App` instances (PTY-backed shells, Sixel-capable programs, etc.).
- **Declarative card layouts** – Allow users to define workspace/app pipelines
  via YAML/JSON (inspired by the mock `layout.yaml` example). This will tie into
  the future card sub-queue work.

### 1.2 Rendering & tcell Decoupling
- **Protocol-neutral styles** – Replace the direct `tcell.Style` usage in
  `texel.Cell` and protocol messages with a lightweight `Style` struct. Clients
  would convert `Style -> tcell.Style` during rendering.
- **Input abstraction** – Swap `tcell.EventKey` in the `texel.App` interface for
  a protocol-neutral key event to simplify testing and enable alternate input
  backends.
- **Screen driver relocation** – Move the `ScreenDriver` interface (and
  `Desktop.Run` rendering loop) into the client runtime so the server remains
  headless.
- **Multiple render backends** – Define a `RenderBackend` interface and provide
  implementations for tcell, web, etc. This becomes more practical once style
  decoupling is complete.

### 1.3 Desktop Services & SDK
- **Service split** – Break `DesktopEngine` into focused services (window
  manager, effect service, event service) to improve testability.
- **Control mode handler** – Extract the control-mode state machine into its own
  component for clarity and targeted tests.
- **Developer SDK** – Offer higher-level builders/helpers for assembling apps,
  including stubs for renderer/input/effect hooks.
- **Hot reload dev server** – Watch source files, reload apps/effects on the fly,
  and inject diagnostic overlays during development.

### 1.4 Effect System Enhancements
- Allow deterministic stacking/mixing of multiple effects without bespoke card
  composition.
- Provide an effect registry API for plugins (hot-reloadable effects, presets).
- Document a CLI preview tool for rapid effect iteration.

---

## 2. Runtime & Protocol Backlog

- **Snapshot store rotation & metrics** – Rotate persisted snapshots, log
  hazards, and surface reconnect latency metrics.
- **Compression toggle** – Investigate optional frame compression for large
  buffers.
- **Batched deltas** – Package multiple panes into a single `MsgBufferDelta` to
  reduce framing overhead during heavy redraws.
- **Binary clipboard streaming** – Extend clipboard messages to support large
  binary payloads.
- **Diagnostics channel** – Activate the reserved `MsgMetricUpdate` type for
  lightweight telemetry.

See `docs/PROTOCOL_FOUNDATIONS.md` for the current wire format details.

---

## 3. Testing & Tooling Roadmap

### 3.1 Integration Gaps
- Client/server split tests – simulate server-driven pane splits/removals and
  assert the headless client reflects the updated tree.
- Resize flow – verify end-to-end resize messaging (client → server → snapshot →
  client) with geometry assertions.
- Multi-client scenarios – ensure simultaneous clients stay in sync when one
  issues structural changes.
- Tree edge cases – cover deep nested splits, pane removal rebalance, focus
  transfer when active pane disappears.
- Large-scale operations – stress many panes (10+), big deltas, and multiple
  concurrent clients.
- Failure modes – inject connection drops, corrupted frames, and version
  mismatches to validate graceful handling.

### 3.2 Headless Client Harness
- Build a `TestClient` wrapper around `texel-headless` that queues scripted
  actions (send key, resize, wait for snapshot) and exposes assertions against
  the buffer cache.
- Use this harness to backfill the integration gaps listed above.

### 3.3 Smoke Automation
- Finalise the `make smoke` target (desktop, client, server packages).
- Add a GitHub Actions workflow running the smoke target plus the protocol
  loopback test once implemented.

Refer to `docs/SMOKE_TEST_PLAN.md` for the canonical smoke suite and near-term
 additions.

---

## 4. Minor Cleanups

- Remove legacy `blit` helpers from `texel/workspace.go` once confirmed unused.
- Finish the `internal/runtime/client` module split (some helpers still share
  the old monolithic structure).

---

Keep this roadmap current. When an item lands, either remove it or cross-link to
the implementation PR for historical context.

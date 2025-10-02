# Phase 0 Baseline Notes

## Regression Checkpoints
- **Baseline script**: Run `scripts/capture-baseline.sh` to refresh environment metadata and archive `ansiterm.log`; pass screenshot paths as arguments to copy them under `docs/baseline/screens/`.
- **Build snapshot**: `make build` emits the reference binary at `bin/texelation`; archive that artifact when tagging milestones.
- **Execution log**: Running `bin/texelation` writes terminal emulator traces to `ansiterm.log`. The capture script copies logs into `docs/baseline/ansiterm-<timestamp>.log` before major refactors.
- **Visual reference**: Launch within tmux or a detached terminal and capture a screenshot of the initial two-pane layout (shell + welcome). Supply the screenshot path to the capture script or copy manually into `docs/baseline/screens/`.
- **Theme state**: `texel/theme` persists user overrides to `~/.config/texelation/theme.json`. Export a copy when verifying color regressions and place alongside other artifacts.
- **Go toolchain fingerprint**: Baseline `go env` and `go list ./...` outputs live in `docs/baseline/go-env.txt` and `docs/baseline/module-list.txt`; refresh them with the capture script when upgrading Go or adding modules.

## Current Architecture Snapshot
- **Entry point (`main.go`)** wires up shell and welcome app factories, initializes the desktop, and registers theme defaults via `texel/theme`.
- **Desktop orchestration (`texel/desktop.go`)** owns pane focus, tiling, input dispatch, and animation sequencing, delegating drawing to the screen layer.
- **Pane & tree management (`texel/pane.go`, `texel/tree.go`)** maintain a hierarchical layout with buffers attached to each pane, handling split/resize mutations.
- **Rendering pipeline (`texel/screen.go`, `texel/cell.go`, `texel/effects*.go`)** converts buffers into tcell primitives and applies visual effects (fade, zoom, layout transitions).
- **Apps** implement `texel.App`; the terminal app (`apps/texelterm`) handles VT parsing, while `apps/statusbar` and `apps/welcome` render auxiliary UI.
- **Configuration** flows through `texel/theme.Config`, with defaults registered on startup; no persistent pane tree or session storage exists yet.

## Outstanding Baseline Tasks
- Evaluate whether baseline logs should be rotated or pruned after checkpoints to avoid uncontrolled growth.
- Decide whether to restore historical `cmd/*` harnesses or replace them with new smoke/integration tests before protocol changes begin.

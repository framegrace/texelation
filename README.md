# Texelation

**Texelation** is a fast, flexible text desktop built for terminals. It pairs a
headless server—responsible for panes, apps, and state—with a tcell-powered
client that renders the experience and applies UI effects. The result feels like
tmux on jet fuel: simple to run, easy to extend, and heavily themeable.

| Built-in Highlights | Description |
| -------------------- | ----------- |
| 🧩 Modular client/server | Server keeps authoritative state; clients can reconnect instantly and render the same buffers. |
| 🎛️ Card-based composition | Apps flow through a card pipeline, making overlays/effects reusable and easy to stack. |
| 🎨 Themeable effects | Customise overlays and colour schemes via JSON bindings shared between the desktop and card pipelines. Sample effects ship today; the pipeline is ready for richer animations tomorrow. |
| ⚡ Responsive & lean | Optimised buffer deltas, debounced resizes, snapshot persistence, and a lean protocol keep the UI snappy. |
| 🧪 Developer-friendly | First-class testing harnesses (`texel-headless`, memconn fixtures), clean package structure, and docs tuned for contributors. |

## TexelApps & Future TexelTui

TexelApps live under `apps/` and can run standalone (`go run ./cmd/texelterm`) or
inside the desktop pipeline. The current set includes the terminal emulator,
status bar, welcome pane, and clock. The pipeline infrastructure (cards,
effects, control bus) lays the groundwork for **TexelTui**—a forthcoming toolkit
for building rich text apps with minimal boilerplate.

Planned TexelApps improvements:

- Sub-queues and declarative card layouts for complex dashboards.
- Shared diagnostics overlays and widget libraries.
- TexelTui components for form input, charts, and animated layouts.

Stay tuned as TexelTui graduates from infancy to a full-fledged framework.

## Project Layout

- `cmd/texel-server` – production server harness that exposes the desktop via Unix sockets.
- `client/cmd/texel-client` – tcell-based remote renderer.
- `internal/runtime/server` – server runtime packages (connections, sessions, snapshots).
- `internal/runtime/client` – client runtime (rendering, handshake, tcell loops).
- `internal/effects` – reusable pane/workspace effect implementations.
- `apps` / `texel` – existing applications and desktop primitives shared by both halves.
- `protocol` – binary protocol definitions exchanged between server and client.

## Building

Use the supplied Makefile targets to build the production binaries into `./bin`:

```bash
make build
```

The resulting folder contains `texel-server` and `texel-client`. Install them into your `GOPATH/bin` with:

```bash
make install
```

To produce cross-compiled binaries for release testing, run:

```bash
make release
```

This writes platform-specific builds into `./dist` for Linux, macOS, and Windows (amd64 + arm64).

## Running Locally

Start the server harness:

```bash
make server
```

It listens on `/tmp/texelation.sock` by default. In a new terminal, launch the remote client:

```bash
make client
```

Both commands use a shared build cache in `.cache/` to avoid polluting `$GOCACHE`.

### Effect Configuration

Visual overlays are configured through the theme file and can also be composed
directly in card pipelines via `cards.NewEffectCard`. Bindings use the same
JSON-style structure in both cases:

```jsonc
"effects": {
  "bindings": [
    {"event": "pane.active", "target": "pane", "effect": "fadeTint"},
    {"event": "pane.resizing", "target": "pane", "effect": "fadeTint"},
    {"event": "workspace.control", "target": "workspace", "effect": "rainbow", "params": {"mix": 0.6}},
    {"event": "workspace.key", "target": "workspace", "effect": "flash", "params": {"keys": ["F"], "max_intensity": 0.75}}
  ]
}
```

See [`docs/EFFECTS_GUIDE.md`](docs/EFFECTS_GUIDE.md) for the complete
development workflow and [`docs/TEXEL_APP_GUIDE.md`](docs/TEXEL_APP_GUIDE.md)
for composing effect cards inside app pipelines.

## Documentation

- [Client/Server architecture](docs/CLIENT_SERVER_ARCHITECTURE.md)
- [Effect development guide](docs/EFFECTS_GUIDE.md)
- [Texel app & card pipeline guide](docs/TEXEL_APP_GUIDE.md)
- [Card control bus reference](docs/cards_control.md)
- [Contribution guide](docs/CONTRIBUTING.md)
- [Future roadmap](docs/FUTURE_ROADMAP.md)

These documents replace the old phase planning notes and are kept current as
features evolve.

## Testing

Unit tests (excluding long-running integration suites) can be executed with:

```bash
make test
```

The offline resume integration test has been moved behind the `integration` build tag. Run it explicitly when needed:

```bash
go test -tags=integration ./internal/runtime/server -run TestOfflineRetentionAndResumeWithMemConn
```

Additional helpers:

- `make fmt` – format all Go sources.
- `make lint` – run `go vet` on the module.
- `make tidy` – update dependencies.
- `make clean` – remove build artifacts (`bin`, `dist`, `.cache`).

## Release Checklist

1. `make tidy` to ensure `go.mod` is up to date.
2. `make test` for fast verification, plus optional integration tests.
3. `make build` (or `make release`) to produce binaries.
4. Tag the release (`git tag vX.Y.Z && git push --tags`).
5. Publish binaries from `dist/` if cross-platform artifacts are required.

## License

Texelation is licensed under the GNU Affero General Public License version 3.0 (or any later version at your option). See `LICENSE` for the full text. Contributions must be compatible with AGPLv3.

## Contributing

Issues and pull requests are welcome! Please run `make fmt test` before submitting patches and mention whether any integration tests were executed. For significant protocol or runtime changes, add notes to `docs/` explaining new behaviour.

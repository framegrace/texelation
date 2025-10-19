# Texelation

Texelation is a modular text-based desktop environment that now runs as a client/server pair. The server hosts apps and manages the pane graph, while the client renders buffers and routes user input over a binary protocol.

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

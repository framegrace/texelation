# Texelation: a Text Desktop Environment

**Texelation** is a fast, flexible **text desktop environment** built for
terminals. It pairs a headless server—responsible for panes, apps, and
state—with a tcell-powered client that renders the experience and applies UI
effects. The result feels like tmux on jet fuel: simple to run, easy to extend,
and heavily themeable.

## Highlights

- 🧱 **Tiling pane manager** with multi-workspace and mouse support. (still partial)
- 🎨 **Fully themeable** appearance including optional visual effects.
- 🧩 **TexelApps pipeline** for composing reusable cards/effects; apps can run
  inside Texelation or standalone.
- 🖥️ **TexelTerm**: a full terminal emulator rendered to a tcell buffer,
  designed for speed and future multi-backend support.
- 🧪 **Developer-friendly tooling** with headless renderers, snapshot-based
  tests, and a clean protocol.

## On the Roadmap

- Form-based configuration (no manual file edits).
- Remote networking (servers and clients on different hosts).
- Multi-host integration (distributed servers).
- Multi-client sessions (multi-monitor, collaborative setups).
- Rich graphical panels via Kitty protocol extensions.
- User-configurable key bindings.

## Features

| Highlight                          | Description                                                                                                                                             |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 🧠 Server-authoritative desktop    | Pane tree, buffers, and theme state live on the server. Clients are thin renderers that can reconnect instantly and resume with buffered deltas.        |
| 💾 Persistent session restore      | Full terminal pane state (buffers, tree, focus) is snapshotted to disk; sessions survive server restarts and resume where you left off.                 |
| 🎛️ Card pipeline & control bus     | Stack reusable cards (effects, diagnostics, overlays) around any app. Trigger capabilities via a control bus instead of wiring bespoke toggles.        |
| 🎨 Themeable effects & animations   | Registry-driven effects plus server-side layout transitions; configure via JSON, hot-reload on SIGHUP, and reuse inside card pipelines.                 |
| 🔌 App registry & wrappers          | Discover apps from manifests under `~/.config/texelation/apps/`; wrapper manifests turn TexelTerm into custom commands without writing Go code.        |
| 🧩 TexelUI widget library           | Label, Button, Input, Checkbox, TextArea, VBox/HBox layouts, and adapters to run TexelUI UIs as regular TexelApps.                                     |
| 🖥️ TexelTerm                       | Full terminal emulator with rich scrollback, selection, bracketed paste, and clipboard handling; embeddable in any pane or pipeline.                   |
| 🧪 Developer-friendly tooling       | Headless renderer, in-memory memconn tests, and a clear package layout for rapid iteration and CI-friendly checks.                                      |

## First Run (Quick Start)

1. Build the binaries (server, client, and apps) into `./bin`:
   ```bash
   make build-apps
   ```
2. Start the server (creates a default session if no snapshot exists):
   ```bash
   ./bin/texel-server
   ```
3. In another terminal, start the client against the same socket:
   ```bash
   ./bin/texel-client
   ```
4. Enter control mode with `Ctrl+A`, then `|` / `-` to split, `l` for the launcher, `h` for help, `x` to close a pane, `z` to zoom.

## Sessions & Persistence

- **Server snapshots**: By default the server loads/saves a snapshot at `~/.texelation/snapshot.json`. If the file exists, startup restores the prior pane tree/buffers; if not, a fresh session starts with the default app. Use `--from-scratch` to ignore any snapshot or `--snapshot <path>` to override the location.
- **Client reconnect**: The client uses `--reconnect` (default: true) to resume prior sessions; it will request a snapshot and buffered deltas on connect. Restarting the client against a running server should pick up where you left off.
- **Sockets**: Default socket is `/tmp/texelation.sock`; override with `--socket` on both server and client.

## Coding

Every line of code here was produced by multiple AIs--No human typed any of it.
Please check the note at the end.

## TexelApps & Future TexelTui

TexelApps live under `apps/` and can run **standalone** (`go run ./cmd/<app>`) or
inside the desktop pipeline. Texelation integration is optional—the same apps
work as normal binaries. The current set includes the terminal emulator, status
bar, launcher, help overlay, clock, and the flicker demo. The pipeline
infrastructure (cards, effects, control bus) lays the groundwork for **TexelTui**—
a forthcoming toolkit for building rich text apps with minimal boilerplate.

Planned TexelApps improvements:

- TexelTui components for form input, charts, and animated layouts.
- Sub-queues and declarative card layouts for complex dashboards.
- Shared diagnostics overlays and widget libraries.

Stay tuned as TexelTui graduates from infancy to a full-fledged framework.

## Keyboard & Mouse Cheat Sheet

### Workspace & Pane Control

- `Ctrl+A` → enter control mode; `Esc` exits.
- `|` / `-` in control mode → split vertically / horizontally.
- `x` in control mode → close active pane.
- `w` in control mode → swap panes using the arrow keys.
- `z` in control mode → toggle zoom on the focused pane.
- `1-9` in control mode → jump to workspace N.
- `Ctrl+Arrow` (control mode) → resize panes along the arrow direction.
- `Shift+Arrow` (anytime) → move focus among panes.
- Mouse click → activate the pane under the cursor.

### TexelTerm Navigation

- Mouse wheel → scroll terminal history.
- `Shift` + wheel → page through history (pane height per tick).
- `Alt` + wheel → fine-grained line scrolling.
- `Alt` + `PgUp`/`PgDn` → page through history via keyboard.
- `Alt` + `Up`/`Down` → scroll history line-by-line.
- Drag with mouse → select & copy text (highlight respects theme colours).

### Miscellaneous

- Selections honour theme colours defined under the `selection` section.

## Project Layout

- `cmd/texel-server` – production server harness that exposes the desktop via Unix sockets.
- `client/cmd/texel-client` – tcell-based remote renderer.
- `internal/runtime/server` – server runtime packages (connections, sessions, snapshots).
- `internal/runtime/client` – client runtime (rendering, handshake, tcell loops).
- `internal/effects` – reusable pane/workspace effect implementations.
- `apps` / `texel` – existing applications and desktop primitives shared by both halves.
- `protocol` – binary protocol definitions exchanged between server and client.

## Building

Use the Makefile helper to build the server, client, and standalone apps into `./bin`:

```bash
make build-apps
```

This produces `texel-server`, `texel-client`, `texelterm`, `flicker`, and `help`. You can also install the server/client directly with:

```bash
make install
```

For cross-compiled release artifacts:

```bash
make release
```

## Running Locally

Start the server harness (either from source or from `bin/`):

```bash
# From source with verbose logs and default socket
make server

# Or using the built binary
./bin/texel-server --socket /tmp/texelation.sock --default-app texelterm
```

Key server flags:
- `--socket` (default `/tmp/texelation.sock`)
- `--default-app` (launcher, texelterm, help)
- `--snapshot` path (default `~/.texelation/snapshot.json`)
- `--from-scratch` (ignore existing snapshot)
- `--verbose-logs` (enable verbose logging)
- `--pprof-cpu` / `--pprof-mem` (write profiles)

Launch the remote client against the same socket:

```bash
make client
# or
./bin/texel-client --socket /tmp/texelation.sock --reconnect
```

Client flags:
- `--socket` (Unix socket path)
- `--reconnect` (resume previous session; enabled by default)
- `--panic-log` (path to append panic stack traces)

## TexelUI Widgets (Standalone Demos)

TexelUI apps run without the desktop. Try the demos to see the widget set:

- Single TextArea with border: `go run ./cmd/texelui-demo`
- Dual TextAreas for focus/resize: `go run ./cmd/texelui-demo2`
- Widget showcase (Label, Input, Checkbox, Button with VBox/HBox layout): `go run ./texelui/examples/widget_demo.go`

Each demo opens directly in your terminal; no server/client needed.

### Effect Configuration

Visual overlays are configured through the theme file and can also be composed
directly in card pipelines via `cards.NewEffectCard`. Bindings use the same
JSON-style structure in both cases:

```jsonc
"effects": {
  "bindings": [
    {"event": "pane.active", "target": "pane", "effect": "fadeTint"},
    {"event": "pane.resizing", "target": "pane", "effect": "fadeTint"},
    {"event": "workspace.control", "target": "workspace", "effect": "rainbow", "params": {"mix": 0.6}}
  ]
}
```

See [Effect Guide](docs/EFFECTS_GUIDE.md) for the complete development workflow and [Texel App Guide](docs/TEXEL_APP_GUIDE.md) for composing effect cards inside app pipelines.

## Documentation

- [Client/Server architecture](docs/CLIENT_SERVER_ARCHITECTURE.md)
- [User app guide](docs/user/APPS.md)
- [Developer guide](docs/programmer/DEVELOPER_GUIDE.md)
- [Effect development guide](docs/EFFECTS_GUIDE.md)
- [Texel app & card pipeline guide](docs/TEXEL_APP_GUIDE.md)
- [Card control bus reference](docs/cards_control.md)
- [Contribution guide](docs/CONTRIBUTING.md)
- [Future roadmap](docs/FUTURE_ROADMAP.md)
- [Plans](docs/plans/README.md)

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
- `make build-apps` – compile the standalone TexelApps (e.g., texelterm, welcome) into `bin/` for distribution or embedding.

- `go run ./client/cmd/texel-headless` – drive the headless renderer to replicate UI interactions without opening a terminal (perfect for automated UI checks).
- `make clean` – remove build artifacts (`bin`, `dist`, `.cache`).

## Release Checklist

1. `make tidy` to ensure `go.mod` is up to date.
2. `make test` for fast verification, plus optional integration tests.
3. `make build` (or `make release`) to produce binaries.
4. Tag the release (`git tag vX.Y.Z && git push --tags`).
5. Publish binaries from `dist/` if cross-platform artifacts are required.

## License

Texelation is licensed under the GNU Affero General Public License version 3.0 (or any later version at your option). See `LICENSE` for the full text. Contributions must be compatible with AGPLv3.

## Acknowledgements

We'd like to thank **George Nachman** and **Thomas E. Dickey** for creating [esctest2](https://github.com/ThomasDickey/esctest2). Their comprehensive VT terminal test suite was invaluable in helping us catch and fix terminal emulation bugs in our project. The 285 tests derived from their work continue to ensure our terminal stays VT-compliant.

## An Important Note

Although I’m tech-savvy—and even if I was a relatively good Java programmer a quarter of a century ago, and tried to keep up by learning new languages now and then—my programming “fu” isn’t quite up to modern times. I’ve been on the systems side of the business for most of my life: sysadmin, architect, and DevOps. These days my work revolves around k8s, CI/CD, cloud APIs, and monitoring tools. The only languages I can honestly call myself “top” in are Bash and regex.

So this all started because I was frustrated by tmux/screen: lots of obscurities and a surprisingly steep learning curve for such simple tools. I’ve used them from the start and still use them daily, but once you get older you just want to stop being a rebel and use something that does the basics out of the box. I also want it to render FAST—everything should feel snappy.

During a quiet period at work, I went looking for a project and thought I’d try building a tmux alternative. The other source of frustration, “vi,” was:
a) Maybe too big of a project.
b) There’s no way I’m stopping using “vi.” It would be like losing a friend that’s been by your side every day for 40 years.

So tmux it was. And thought that it may be extended to be a complete TDE. A text/based Desktop environment.

I wanted to learn a new language and, coming from the k8s world, Go seemed right. I figured I’d start a small project using ChatGPT to plan it and scaffold the app for me to take over and… it got out of hand.

This entire project is now an experiment in “vibe coding.”

Every line so far has been written by an LLM (mostly Claude and ChatGPT CLI tools—I've tried others, but those work best for my method). I haven’t typed a single line of code myself. Even the commits have been made and documented by AI. (I only handle the branches and merging.)

All of this has been done through conversations with the AIs, and it’s been a fun, eye-opening experience. It has its challenges: sometimes the AI goes totally off-road following a strange implementation choice, and it’s hard to steer it back. On the other hand, having someone who can refactor big parts of the code as many times as needed is a blessing.

I'm quite impressed by the results, most of the architecture decisiosn have been mine, but all the messaging protocol including it's fantastic optimizactions and features (diff queuing and replay on connect particularly impressive) is purely CHatGPT 5's idea. Sometimes I had to explain the exact algorithm or data structure is the best, sometimes they provided surprising ideas that worked super well...

So here I publish my results. I think at the end would be a nice product to release. So here it is...
And it only took me the weekends of 4 or 6 months to this half useful state.

## Colaborations

At first thought it would be fun to only allow AI produced PR's, to keep the code "PURE" :) 

But that would be a nightmare to check and really, this being started by AI, at the end would be just a curiosity.

So everyone that finds this usable and wants to collaborate is open to do it, no matter the matter your brain is made of.

## Contributing

Issues and pull requests are welcome! Please run `make fmt test` before submitting patches and mention whether any integration tests were executed. For significant protocol or runtime changes, add notes to `docs/` explaining new behaviour.

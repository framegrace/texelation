# Texelation a TDE

**Texelation** is a fast, flexible **text desktop environment** built for terminals. It pairs a
headless server—responsible for panes, apps, and state—with a tcell-powered
client that renders the experience and applies UI effects. The result feels like
tmux on jet fuel: simple to run, easy to extend, and heavily themeable.

It currently has:
- A tiling panel manager
- multiple desktops
- themeable
- desktop effects which are easily extendible
- TexelApps Easy txext app making framwork that can run directly on texelaction or standalone.
- Texelterm a full fledged terminal emulator for fast rendering. (Independent of the hosting one)

On the plans: 
- Form based configuration: No more file editing
- Networking (servers and clients on different hosts)
- Multihost integration (Servers on different hosts)
- Multiclient integration (Multiple clients on the same server for multi-monitor, or many othe prossibilities)
- Grafic panels, using kitty protocol. (We have big plans for that)

## Features

| Built-in Highlights | Description |
| -------------------- | ----------- |
| 🧩 Modular client/server | Server keeps authoritative state; clients can reconnect instantly and render the same buffers. |
| 🎛️ Card-based composition | Apps flow through a card pipeline, making overlays/effects reusable and easy to stack. |
| 🎨 Themeable effects | Customise overlays and colour schemes via JSON bindings shared between the desktop and card pipelines. Sample effects ship today; the pipeline is ready for richer animations tomorrow. |
| ⚡ Responsive & lean | Optimised buffer deltas, debounced resizes, snapshot persistence, and a lean protocol keep the UI snappy. |
| 🧪 Developer-friendly | First-class testing harnesses (`texel-headless`, memconn fixtures), clean package structure, and docs tuned for contributors. |
| 🖥️ TexelTerm | Full terminal emulator rendered to a tcell buffer. Easily embeddable today and slated to gain multi-backend (including web) support. |

## Coding

Every line of code here was produced by multiple AIs--No human typed any of it.
Please check the note at the end.

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

See [Effect Guide](EFFECTS_GUIDE.md) for the complete development workflow and [Texel App Guide](TEXEL_APP_GUIDE.md) for composing effect cards inside app pipelines.

## Documentation

- [Client/Server architecture](CLIENT_SERVER_ARCHITECTURE.md)
- [Effect development guide](EFFECTS_GUIDE.md)
- [Texel app & card pipeline guide](TEXEL_APP_GUIDE.md)
- [Card control bus reference](cards_control.md)
- [Contribution guide](CONTRIBUTING.md)
- [Future roadmap](FUTURE_ROADMAP.md)

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

So everyone that finds this usable and wants to collaborate is open to do it, no matter the the matter your brain is made of.

## Contributing

Issues and pull requests are welcome! Please run `make fmt test` before submitting patches and mention whether any integration tests were executed. For significant protocol or runtime changes, add notes to `docs/` explaining new behaviour.

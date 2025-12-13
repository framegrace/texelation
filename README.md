# Texelation: a Text Desktop Environment

**Texelation** is a fast, flexible **text desktop environment** built for terminals. It pairs a headless server with a tcell-powered client, delivering a tmux-like experience with modern features: infinite persistent sessions, smooth animations, and full state restoration across server restarts.

## Key Features

- **Infinite Persistent Sessions** - Terminal output persists to disk with unlimited scrollback. Environment variables, working directory, and command history survive both shell and server restarts.
- **Smooth Layout Animations** - Server-side animated transitions when splitting or closing panes, configurable easing and duration.
- **Proper Terminal Reflow** - Resize your terminal and text reflows correctly, preserving logical lines across width changes.
- **Tiling Pane Manager** - Multi-workspace support with keyboard and mouse control.
- **Fully Themeable** - Visual effects, colors, and animations all configurable via JSON with hot-reload support.
- **Client/Server Architecture** - Disconnect and reconnect without losing state. Multiple clients can attach to the same session.

## What Makes It Different

| Feature | tmux/screen | Texelation |
|---------|-------------|------------|
| Session persistence | In-memory only | Disk-backed, survives server restart |
| Terminal reflow | No | Yes, O(viewport) resize |
| Environment restore | No | Full env + CWD restored |
| Layout animations | No | Smooth split/close transitions |
| Scrollback limit | Fixed buffer | Unlimited (disk-backed) |
| Visual effects | No | Configurable overlays and animations |

## Quick Start

```bash
# Build
make build

# Start server (in one terminal)
./bin/texel-server

# Connect client (in another terminal)
./bin/texel-client
```

**Control mode**: Press `Ctrl+A` then:
- `|` / `-` - Split vertically / horizontally
- `x` - Close pane
- `z` - Zoom/unzoom pane
- `1-9` - Switch workspace
- `l` - Open launcher
- `h` - Help overlay
- `Esc` - Exit control mode

## Architecture

```
┌─────────────────┐         ┌─────────────────┐
│  texel-client   │◄───────►│  texel-server   │
│  (tcell render) │  Unix   │  (pane tree,    │
│                 │  socket │   apps, state)  │
└─────────────────┘         └────────┬────────┘
                                     │
                            ┌────────▼────────┐
                            │   Persistence   │
                            │  - Snapshots    │
                            │  - Scrollback   │
                            │  - Environment  │
                            └─────────────────┘
```

The server owns all state: pane tree, terminal buffers, app lifecycles. Clients are thin renderers that can reconnect instantly and resume with buffered deltas.

## Terminal Persistence

Texelation's terminal emulator (TexelTerm) uses a three-level architecture for scrollback:

```
Disk History (unlimited) → Memory Window (~5000 lines) → Display Buffer (viewport)
```

**What persists across server restarts:**
- Full scrollback history (unlimited, disk-backed)
- Environment variables
- Working directory
- Per-terminal command history (bash HISTFILE isolation)

See [Terminal Persistence Architecture](docs/TERMINAL_PERSISTENCE_ARCHITECTURE.md) for details.

## Configuration

Texelation uses `~/.config/texelation/theme.json` for all configuration:

```json
{
  "texelterm": {
    "display_buffer_enabled": true
  },
  "layout_transitions": {
    "enabled": true,
    "duration_ms": 200,
    "easing": "smoothstep"
  },
  "effects": {
    "bindings": [
      {"event": "pane.active", "target": "pane", "effect": "fadeTint"}
    ]
  }
}
```

Hot-reload configuration with `kill -HUP $(pidof texel-server)`.

## Keyboard & Mouse

### Pane Control (in control mode after Ctrl+A)
- `|` / `-` - Split vertically / horizontally
- `x` - Close active pane
- `w` + arrows - Swap panes
- `z` - Toggle zoom
- `1-9` - Jump to workspace
- `Ctrl+Arrow` - Resize panes
- `Shift+Arrow` - Move focus (works outside control mode too)

### Terminal Navigation
- Mouse wheel - Scroll history
- `Shift+wheel` - Page through history
- `Alt+PgUp/PgDn` - Page history (keyboard)
- `Alt+Up/Down` - Line-by-line scroll
- Mouse drag - Select text

## Sessions & Persistence

- **Snapshots**: Server saves state to `~/.texelation/snapshot.json`. Use `--from-scratch` to start fresh.
- **Reconnect**: Client automatically resumes sessions. Restart the client anytime without losing state.
- **Environment**: Shell environment and CWD persist via `~/.texel-env-<pane-id>` files.

## Project Layout

```
cmd/texel-server/       Server binary
client/cmd/texel-client/ Client binary
apps/texelterm/         Terminal emulator
apps/*/                 Other apps (statusbar, launcher, etc.)
texel/                  Core desktop primitives
protocol/               Binary protocol definitions
internal/runtime/       Server and client runtime
internal/effects/       Visual effect implementations
```

## Building

```bash
make build        # Build server and client
make build-apps   # Build standalone apps too
make install      # Install to GOPATH/bin
make release      # Cross-compile for all platforms
make test         # Run tests
make fmt          # Format code
```

## Documentation

**Architecture:**
- [Client/Server Architecture](docs/CLIENT_SERVER_ARCHITECTURE.md)
- [Protocol Foundations](docs/PROTOCOL_FOUNDATIONS.md)
- [Terminal Persistence](docs/TERMINAL_PERSISTENCE_ARCHITECTURE.md)
- [Layout Animations](docs/LAYOUT_ANIMATION_DESIGN.md)

**Development:**
- [Developer Guide](docs/programmer/DEVELOPER_GUIDE.md)
- [Effect Development](docs/EFFECTS_GUIDE.md)
- [Texel App Guide](docs/TEXEL_APP_GUIDE.md)
- [Future Roadmap](docs/FUTURE_ROADMAP.md)

## Roadmap

- Remote networking (server/client on different hosts)
- Multi-client sessions (collaborative editing)
- Form-based configuration UI
- Rich graphics via Kitty protocol
- User-configurable key bindings

## License

AGPLv3 or later. See `LICENSE`.

## Acknowledgements

Thanks to **George Nachman** and **Thomas E. Dickey** for [esctest2](https://github.com/ThomasDickey/esctest2). Their VT terminal test suite helped ensure our terminal stays compliant.

---

## The Story Behind This Project

Every line of code here was written by AI (Claude and ChatGPT). I haven't typed a single line myself—even the commits are AI-generated.

I'm a sysadmin/DevOps person, not a programmer. This started as frustration with tmux/screen: too many obscurities, steep learning curve for simple tools. I wanted something that just works out of the box and renders FAST.

Using Go (fitting for a k8s person) and AI assistants, what started as "let me scaffold something small" turned into a full experiment in "vibe coding." Sometimes the AI goes off-road and it's hard to steer back. But having someone who can refactor endlessly is a blessing.

The architecture decisions are mine, but impressive features like the diff-queued protocol with replay-on-connect came purely from ChatGPT. It's been eye-opening.

This took about 4-6 months of weekends. Here it is.

## Contributing

Issues and PRs welcome! Run `make fmt test` before submitting. For significant changes, add documentation explaining new behavior.

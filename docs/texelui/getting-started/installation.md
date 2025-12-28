# Installation

This guide covers how to install and build TexelUI as part of the Texelation project.

## Prerequisites

### Required
- **Go 1.24.3** or later
- **Git** for cloning the repository
- **Make** for building (optional but recommended)

### Recommended
- A modern terminal with 256-color support
- Terminal size of at least 80x24 characters

## Installation Steps

### 1. Clone the Repository

```bash
git clone https://github.com/your-org/texelation.git
cd texelation
```

### 2. Build the Project

Using Make (recommended):
```bash
# Build core binaries (server, client)
make build

# Build all app binaries including texelui-demo
make build-apps
```

Or using Go directly:
```bash
# Build texelui-demo standalone app
go build -o bin/texelui-demo ./cmd/texelui-demo
```

### 3. Verify Installation

```bash
# Run the demo to verify everything works
./bin/texelui-demo
```

You should see a tabbed interface. Press `Ctrl+C` to exit.

## Project Structure

After building, you'll have these relevant binaries:

```
bin/
├── texelui-demo     # TexelUI widget showcase (standalone)
├── texel-server     # Texelation server
├── texel-client     # Texelation client
├── texelterm        # Terminal emulator (standalone)
└── help             # Help viewer (standalone)
```

## TexelUI Source Location

TexelUI source code is organized under the `texelui/` directory:

```
texelui/
├── core/            # Core interfaces and UIManager
│   ├── widget.go    # Widget interface and BaseWidget
│   ├── uimanager.go # Focus, events, rendering
│   ├── painter.go   # Drawing primitives
│   └── types.go     # Rect, Layout interface
├── widgets/         # Built-in widgets
│   ├── button.go
│   ├── input.go
│   ├── textarea.go
│   └── ...
├── primitives/      # Reusable building blocks
│   ├── scrollablelist.go
│   ├── grid.go
│   └── tabbar.go
├── layout/          # Layout managers
│   ├── vbox.go
│   └── hbox.go
├── adapter/         # texel.App adapter
│   └── texel_app.go
└── color/           # Color utilities (OKLCH, etc.)
```

## Development Setup

### IDE Configuration

For VS Code, add these settings for Go development:

```json
{
    "go.useLanguageServer": true,
    "go.lintTool": "golangci-lint",
    "go.formatTool": "gofmt"
}
```

### Running Tests

```bash
# Run all TexelUI tests
go test ./texelui/...

# Run specific widget tests
go test ./texelui/widgets/...

# Run with verbose output
go test -v ./texelui/...
```

## Troubleshooting

### Build Errors

**"package texelation/texelui/... not found"**
- Ensure you're in the texelation root directory
- Run `go mod tidy` to update dependencies

**"tcell: terminal does not support required features"**
- Your terminal may not support the required escape sequences
- Try a different terminal (iTerm2, Alacritty, Kitty, etc.)

### Runtime Issues

**Colors look wrong**
- Ensure your terminal supports 256 colors
- Check `TERM` environment variable (should be `xterm-256color` or similar)
- The theme system defaults to Catppuccin Mocha palette

**Mouse doesn't work**
- Ensure your terminal supports mouse events
- Some terminals require enabling mouse mode in settings

## Next Steps

- Continue to [Quickstart](quickstart.md) to build your first app
- Or jump to [Hello World](hello-world.md) for a minimal example

## Related Documentation

- [CLAUDE.md](../../../CLAUDE.md) - Build commands and project overview
- [Makefile](../../../Makefile) - All available make targets

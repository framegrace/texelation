# Integration Guide

How TexelUI runs in different environments.

## Overview

TexelUI applications can run in two modes:

```
┌─────────────────────────────────────────────────────────────────┐
│                        Your TexelUI App                         │
│                                                                 │
│  ┌─────────────────┐              ┌─────────────────┐          │
│  │   UIManager     │              │   UIManager     │          │
│  │   + Widgets     │              │   + Widgets     │          │
│  └────────┬────────┘              └────────┬────────┘          │
│           │                                │                    │
│           ▼                                ▼                    │
│  ┌─────────────────┐              ┌─────────────────┐          │
│  │    devshell     │              │    UIApp        │          │
│  │   (standalone)  │              │   (adapter)     │          │
│  └────────┬────────┘              └────────┬────────┘          │
│           │                                │                    │
└───────────┼────────────────────────────────┼────────────────────┘
            │                                │
            ▼                                ▼
┌─────────────────────┐          ┌─────────────────────┐
│   Terminal (tcell)  │          │   Texelation Pane   │
│   Direct rendering  │          │   Desktop managed   │
└─────────────────────┘          └─────────────────────┘
```

## Running Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| [Standalone](standalone-mode.md) | Direct terminal, full screen | Development, single-purpose tools |
| [TexelApp](texelapp-mode.md) | Inside Texelation desktop | Multi-app environment, integration |

## Quick Comparison

```
Standalone Mode:                 TexelApp Mode:
┌────────────────────────┐      ┌────────────────────────────────┐
│ Your App (full screen) │      │ Texelation Desktop             │
│                        │      │ ┌──────────┐ ┌──────────────┐ │
│                        │      │ │ Terminal │ │  Your App    │ │
│                        │      │ │          │ │              │ │
│                        │      │ │          │ │              │ │
│                        │      │ └──────────┘ └──────────────┘ │
└────────────────────────┘      └────────────────────────────────┘
```

## Choosing a Mode

### Use Standalone When:

- Building a single-purpose TUI tool
- Developing and testing UI components
- Creating utilities that run independently
- Maximum simplicity is needed

### Use TexelApp When:

- Building apps for the Texelation ecosystem
- Need multi-app window management
- Want system-wide theming
- Need inter-app communication
- Apps should coexist with terminals

## Code Portability

The same UIManager code works in both modes:

```go
// This code runs in BOTH modes
func createUI() *core.UIManager {
    ui := core.NewUIManager()
    ui.SetLayout(layout.NewVBox(1))

    ui.AddWidget(widgets.NewLabel(0, 0, 30, 1, "Hello, World!"))
    ui.AddWidget(widgets.NewButton(0, 0, 12, 1, "Click Me"))

    return ui
}
```

**Standalone:**
```go
func main() {
    ui := createUI()
    devshell.Run(ui)
}
```

**TexelApp:**
```go
func New() texel.App {
    ui := createUI()
    return adapter.NewUIApp(ui, nil)
}
```

## Integration Topics

| Topic | Description |
|-------|-------------|
| [Standalone Mode](standalone-mode.md) | Running with devshell |
| [TexelApp Mode](texelapp-mode.md) | Integration with Texelation |
| [Theme Integration](theme-integration.md) | Using Texelation themes |

## Architecture Deep Dive

### Standalone Architecture

```
┌───────────────────────────────────────────┐
│                  devshell                  │
│  ┌─────────────────────────────────────┐  │
│  │           UIManager                  │  │
│  │  ┌─────────┐ ┌─────────┐ ┌───────┐ │  │
│  │  │ Widget  │ │ Widget  │ │ ...   │ │  │
│  │  └─────────┘ └─────────┘ └───────┘ │  │
│  └─────────────────────────────────────┘  │
│                     │                      │
│                     ▼                      │
│  ┌─────────────────────────────────────┐  │
│  │         tcell.Screen                 │  │
│  │         (direct terminal)            │  │
│  └─────────────────────────────────────┘  │
└───────────────────────────────────────────┘
```

### TexelApp Architecture

```
┌────────────────────────────────────────────────────────────────┐
│                         Texelation                              │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │                        Desktop                            │  │
│  │  ┌────────────────────────────────────────────────────┐  │  │
│  │  │                    Screen (Workspace)              │  │  │
│  │  │  ┌───────────────────┐  ┌───────────────────────┐ │  │  │
│  │  │  │     Pane          │  │       Pane            │ │  │  │
│  │  │  │  ┌─────────────┐  │  │  ┌─────────────────┐  │ │  │  │
│  │  │  │  │   UIApp     │  │  │  │   Other App     │  │ │  │  │
│  │  │  │  │ (adapter)   │  │  │  │   (terminal)    │  │ │  │  │
│  │  │  │  │     │       │  │  │  │                 │  │ │  │  │
│  │  │  │  │ UIManager   │  │  │  │                 │  │ │  │  │
│  │  │  │  └─────────────┘  │  │  └─────────────────┘  │ │  │  │
│  │  │  └───────────────────┘  └───────────────────────┘ │  │  │
│  │  └────────────────────────────────────────────────────┘  │  │
│  └──────────────────────────────────────────────────────────┘  │
│                              │                                  │
│                              ▼                                  │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │                     Protocol                              │  │
│  │              (binary client/server)                       │  │
│  └──────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────┘
```

## Key Differences

| Aspect | Standalone | TexelApp |
|--------|------------|----------|
| **Screen** | Full terminal | Assigned pane rectangle |
| **Theming** | Manual setup | Automatic from Texelation |
| **Focus** | Always has focus | Managed by desktop |
| **Resize** | Terminal resize events | Pane resize events |
| **Lifecycle** | Main function | App interface methods |
| **Dependencies** | Just devshell | Full Texelation |

## Migration Path

Start standalone for development, then wrap for Texelation:

1. **Develop standalone** - Rapid iteration with devshell
2. **Create adapter** - Wrap UIManager in UIApp
3. **Register with Texelation** - Add to app registry
4. **Test in desktop** - Verify resize, focus, theming

## See Also

- [Standalone Mode](standalone-mode.md) - devshell details
- [TexelApp Mode](texelapp-mode.md) - adapter details
- [Theme Integration](theme-integration.md) - theming
- [Architecture](../core-concepts/architecture.md) - TexelUI internals

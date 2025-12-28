# TexelUI Documentation

TexelUI is the official widget library for building text-based user interfaces in Texelation. It provides a complete toolkit of widgets, layouts, and theming capabilities for creating rich terminal applications.

```
┌─────────────────────────────────────────────────────────────────┐
│  TexelUI Widget Showcase                                        │
├─────────────────────────────────────────────────────────────────┤
│  ► Inputs   Layouts   Widgets                                   │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│    Name:        [Enter your name____________]                   │
│                                                                 │
│    Country:     [Type to search...       ▼]                    │
│                                                                 │
│    Notes:       ┌──────────────────────────┐                   │
│                 │ Your notes here...       │                   │
│                 │                          │                   │
│                 └──────────────────────────┘                   │
│                                                                 │
│    [ Submit ]   [ Cancel ]                                     │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Why TexelUI?

- **Native Terminal Experience**: Built specifically for text-based interfaces, not a web wrapper
- **Dual Runtime**: Run standalone in any terminal OR embedded in Texelation desktop
- **Theme Integration**: Seamless integration with Texelation's Catppuccin-based theming
- **Lightweight**: No external dependencies beyond tcell for terminal handling
- **Composable**: Build complex UIs by combining simple, focused widgets

## Quick Links

| Section | Description |
|---------|-------------|
| [Getting Started](/texelui/getting-started/README.md) | Install, build, and run your first app |
| [Tutorials](/texelui/tutorials/README.md) | Step-by-step guides for common tasks |
| [Core Concepts](/texelui/core-concepts/README.md) | Architecture, events, rendering, theming |
| [Widgets](/texelui/widgets/README.md) | Complete reference for all widgets |
| [Primitives](/texelui/primitives/README.md) | Reusable building blocks for custom widgets |
| [Layout](/texelui/layout/README.md) | Layout managers: VBox, HBox, Absolute |
| [Integration](/texelui/integration/README.md) | Standalone mode vs TexelApp embedding |
| [API Reference](/texelui/api-reference/README.md) | Interfaces and type documentation |

## 5-Minute Quickstart

**1. Run the demo:**
```bash
make build-apps
./bin/texelui-demo
```

**2. Create a simple app:**
```go
package main

import (
    "texelation/internal/devshell"
    "texelation/texelui/adapter"
    "texelation/texelui/core"
    "texelation/texelui/widgets"
    "github.com/gdamore/tcell/v2"
)

func main() {
    devshell.Run(func(args []string) (texel.App, error) {
        ui := core.NewUIManager()

        // Create a button
        btn := widgets.NewButton(10, 5, 0, 0, "Hello World!")
        btn.OnClick = func() {
            // Button clicked!
        }

        ui.AddWidget(btn)
        ui.Focus(btn)

        return adapter.NewUIApp("My App", ui), nil
    }, nil)
}
```

**3. Next steps:** Follow a full walkthrough or browse common patterns.

Related documentation:
- [Complete Quickstart Guide](/texelui/getting-started/quickstart.md)
- [Hello World](/texelui/getting-started/hello-world.md)
- [Building a Form Tutorial](/texelui/tutorials/building-a-form.md)
- [Standalone vs TexelApp Mode](/texelui/tutorials/standalone-vs-texelapp.md)

## Widget Overview

TexelUI provides these widgets out of the box:

### Input Widgets
| Widget | Description |
|--------|-------------|
| [Input](/texelui/widgets/input.md) | Single-line text entry with placeholder and caret |
| [TextArea](/texelui/widgets/textarea.md) | Multi-line text editor with scrolling |
| [Checkbox](/texelui/widgets/checkbox.md) | Boolean toggle with label |
| [ComboBox](/texelui/widgets/combobox.md) | Dropdown with optional autocomplete |
| [ColorPicker](/texelui/widgets/colorpicker.md) | Color selection with multiple modes |

### Display Widgets
| Widget | Description |
|--------|-------------|
| [Label](/texelui/widgets/label.md) | Static text with alignment options |
| [Button](/texelui/widgets/button.md) | Clickable action trigger |

### Container Widgets
| Widget | Description |
|--------|-------------|
| [Pane](/texelui/widgets/pane.md) | Container with background and child support |
| [Border](/texelui/widgets/border.md) | Decorative border around content |
| [TabLayout](/texelui/widgets/tablayout.md) | Tabbed container with switchable panels |

### Primitives (Building Blocks)
| Primitive | Description |
|-----------|-------------|
| [ScrollableList](/texelui/primitives/scrollablelist.md) | Scrolling list with custom rendering |
| [Grid](/texelui/primitives/grid.md) | 2D grid with dynamic columns |
| [TabBar](/texelui/primitives/tabbar.md) | Horizontal tab navigation |

Related documentation:
- [Widgets Reference](/texelui/widgets/README.md)
- [Primitives Reference](/texelui/primitives/README.md)

## Architecture at a Glance

```
┌──────────────────────────────────────────────────────────────┐
│                      Your Application                         │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐      │
│  │   Widgets   │    │   Layout    │    │   Theming   │      │
│  │             │    │             │    │             │      │
│  │ • Button    │    │ • VBox      │    │ • Semantic  │      │
│  │ • Input     │    │ • HBox      │    │   Colors    │      │
│  │ • TextArea  │    │ • Absolute  │    │ • Palettes  │      │
│  │ • ComboBox  │    │             │    │             │      │
│  └──────┬──────┘    └──────┬──────┘    └──────┬──────┘      │
│         │                  │                  │              │
│         └────────────┬─────┴──────────────────┘              │
│                      │                                       │
│              ┌───────▼───────┐                              │
│              │  UIManager    │                              │
│              │               │                              │
│              │ • Focus       │                              │
│              │ • Events      │                              │
│              │ • Rendering   │                              │
│              └───────┬───────┘                              │
│                      │                                       │
│              ┌───────▼───────┐                              │
│              │    Adapter    │                              │
│              │   (UIApp)     │                              │
│              └───────┬───────┘                              │
│                      │                                       │
├──────────────────────┼───────────────────────────────────────┤
│                      ▼                                       │
│  ┌─────────────────────────────────────────────────────┐    │
│  │              Runtime Environment                     │    │
│  ├─────────────────────┬───────────────────────────────┤    │
│  │    Standalone       │       TexelApp Mode           │    │
│  │    (devshell)       │    (Texelation Desktop)       │    │
│  │                     │                               │    │
│  │  Direct tcell       │   Embedded in pane,           │    │
│  │  terminal access    │   protocol-based rendering    │    │
│  └─────────────────────┴───────────────────────────────┘    │
└──────────────────────────────────────────────────────────────┘
```

Related documentation:
- [Core Concepts Index](/texelui/core-concepts/README.md)
- [Architecture](/texelui/core-concepts/architecture.md)
- [Widget Interface](/texelui/core-concepts/widget-interface.md)
- [Focus and Events](/texelui/core-concepts/focus-and-events.md)
- [Rendering](/texelui/core-concepts/rendering.md)
- [Theming](/texelui/core-concepts/theming.md)

## Running Modes

TexelUI supports two runtime modes:

### Standalone Mode
Run directly in any terminal without the Texelation server:
```bash
./bin/texelui-demo
```
Perfect for development, testing, or simple tools.

### TexelApp Mode
Embed inside Texelation desktop as a managed application:
```go
// Register your app with the server
registry["my-app"] = func() texel.App {
    return myTexelUIApp()
}
```
Full integration with workspaces, effects, and the desktop environment.

Related documentation:
- [Integration Guide](/texelui/integration/README.md)
- [Standalone Mode](/texelui/integration/standalone-mode.md)
- [TexelApp Mode](/texelui/integration/texelapp-mode.md)
- [Standalone vs TexelApp](/texelui/tutorials/standalone-vs-texelapp.md)

## Theme Integration

TexelUI widgets automatically use Texelation's theme system:

```go
// Widgets automatically pick up theme colors
btn := widgets.NewButton(0, 0, 0, 0, "Themed Button")
// Uses: action.primary (background), text.inverse (foreground)

// Theme colors are semantic
// "action.primary" -> "accent" -> "@mauve" -> "#cba6f7"
```

Available semantic colors:
- `bg.surface`, `bg.base` - Backgrounds
- `text.primary`, `text.muted` - Text colors
- `action.primary`, `action.danger` - Action colors
- `border.active`, `border.focus` - Border colors

[Learn more about theming](/texelui/core-concepts/theming.md)

Related documentation:
- [Theme Integration Guide](/texelui/integration/theme-integration.md)
- [Theming Concepts](/texelui/core-concepts/theming.md)

## Contributing

TexelUI is part of the Texelation project. To contribute:

1. Read the [CONTRIBUTING.md](/CONTRIBUTING.md) guide
2. Check the [TEXELUI_PLAN.md](/plans/TEXELUI_PLAN.md) for roadmap
3. Look for widgets marked as "Next Priority"

## License

TexelUI is part of Texelation and shares its AGPL-3.0-or-later license.

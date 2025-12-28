# Standalone Mode

Running TexelUI applications directly in the terminal.

## Overview

Standalone mode uses `devshell` to run your TexelUI application directly in the terminal, without Texelation. Perfect for development and single-purpose tools.

```
┌─────────────────────────────────────┐
│            Terminal Window          │
│  ┌───────────────────────────────┐  │
│  │                               │  │
│  │      Your TexelUI App         │  │
│  │      (full screen)            │  │
│  │                               │  │
│  └───────────────────────────────┘  │
└─────────────────────────────────────┘
```

## Quick Start

```go
package main

import (
    "texelation/texelui/core"
    "texelation/texelui/widgets"
    "texelation/internal/devshell"
)

func main() {
    ui := core.NewUIManager()

    ui.AddWidget(widgets.NewLabel(5, 2, 30, 1, "Hello, TexelUI!"))
    ui.AddWidget(widgets.NewButton(5, 4, 12, 1, "Click Me"))

    devshell.Run(ui)
}
```

## devshell Package

The `devshell` package provides terminal initialization and event loop:

```go
import "texelation/internal/devshell"
```

### Run Function

```go
func Run(ui *core.UIManager) error
```

Starts the application:
1. Initializes tcell screen
2. Handles terminal resize
3. Routes keyboard/mouse events to UIManager
4. Renders on each frame
5. Cleans up on exit

### With Options

```go
func RunWithOptions(ui *core.UIManager, opts Options) error
```

**Options struct:**

```go
type Options struct {
    // Exit key (default: Escape)
    ExitKey tcell.Key

    // Enable mouse support (default: true)
    EnableMouse bool

    // Custom initialization
    OnInit func(screen tcell.Screen)

    // Custom cleanup
    OnExit func()
}
```

## Complete Example

```go
package main

import (
    "log"

    "github.com/gdamore/tcell/v2"
    "texelation/texelui/core"
    "texelation/texelui/layout"
    "texelation/texelui/widgets"
    "texelation/internal/devshell"
)

func main() {
    // Create UI manager
    ui := core.NewUIManager()
    ui.SetLayout(layout.NewVBox(1))

    // Add widgets
    title := widgets.NewLabel(0, 0, 40, 1, "Settings")
    title.Style = tcell.StyleDefault.Bold(true)

    nameLabel := widgets.NewLabel(0, 0, 10, 1, "Name:")
    nameInput := widgets.NewInput(0, 0, 30)

    emailLabel := widgets.NewLabel(0, 0, 10, 1, "Email:")
    emailInput := widgets.NewInput(0, 0, 30)

    saveBtn := widgets.NewButton(0, 0, 10, 1, "Save")
    cancelBtn := widgets.NewButton(0, 0, 10, 1, "Exit")

    // Wire up button actions
    saveBtn.OnActivate = func() {
        log.Printf("Saving: name=%s, email=%s",
            nameInput.Text(), emailInput.Text())
    }

    cancelBtn.OnActivate = func() {
        // Exit handled by devshell
    }

    // Add to UI
    ui.AddWidget(title)
    ui.AddWidget(nameLabel)
    ui.AddWidget(nameInput)
    ui.AddWidget(emailLabel)
    ui.AddWidget(emailInput)
    ui.AddWidget(saveBtn)
    ui.AddWidget(cancelBtn)

    // Run
    if err := devshell.Run(ui); err != nil {
        log.Fatal(err)
    }
}
```

## Event Handling

devshell routes events to UIManager automatically:

### Keyboard Events

```
┌─────────────────────────────────────────────────┐
│  User types key                                 │
│       │                                         │
│       ▼                                         │
│  tcell.PollEvent()                              │
│       │                                         │
│       ▼                                         │
│  devshell event loop                            │
│       │                                         │
│       ▼                                         │
│  ui.HandleKey(event)                            │
│       │                                         │
│       ▼                                         │
│  Focused widget's HandleKey()                   │
└─────────────────────────────────────────────────┘
```

### Mouse Events

```
┌─────────────────────────────────────────────────┐
│  User clicks mouse                              │
│       │                                         │
│       ▼                                         │
│  tcell.PollEvent()                              │
│       │                                         │
│       ▼                                         │
│  devshell event loop                            │
│       │                                         │
│       ▼                                         │
│  ui.HandleMouse(event)                          │
│       │                                         │
│       ▼                                         │
│  Widget at (x, y) receives event                │
└─────────────────────────────────────────────────┘
```

### Resize Events

```
┌─────────────────────────────────────────────────┐
│  Terminal window resized                        │
│       │                                         │
│       ▼                                         │
│  tcell.EventResize                              │
│       │                                         │
│       ▼                                         │
│  screen.Size() → (newW, newH)                   │
│       │                                         │
│       ▼                                         │
│  ui.Resize(newW, newH)                          │
│       │                                         │
│       ▼                                         │
│  Layout reapplied, widgets repositioned         │
└─────────────────────────────────────────────────┘
```

## Exit Handling

### Default Exit Key

Press `Escape` to exit (configurable).

### Custom Exit

```go
opts := devshell.Options{
    ExitKey: tcell.KeyCtrlQ,  // Ctrl+Q to exit
}
devshell.RunWithOptions(ui, opts)
```

### Programmatic Exit

```go
// In your widget or handler
devshell.RequestExit()
```

## Theming in Standalone

Without Texelation, you manage themes manually:

```go
import "github.com/gdamore/tcell/v2"

// Define styles
var (
    normalStyle   = tcell.StyleDefault.
        Foreground(tcell.ColorWhite).
        Background(tcell.ColorBlack)

    buttonStyle   = tcell.StyleDefault.
        Foreground(tcell.ColorBlack).
        Background(tcell.ColorBlue)

    inputStyle    = tcell.StyleDefault.
        Foreground(tcell.ColorWhite).
        Background(tcell.ColorDarkGray)
)

// Apply to widgets
button := widgets.NewButton(0, 0, 10, 1, "Click")
button.Style = buttonStyle

input := widgets.NewInput(0, 0, 30)
input.Style = inputStyle
```

Or use Texelation's theme system standalone:

```go
import "texelation/texel/theme"

// Load theme
themeData, _ := os.ReadFile("theme.json")
th, _ := theme.LoadFromBytes(themeData)

// Use theme colors
buttonStyle := tcell.StyleDefault.
    Foreground(th.Semantics.ActionPrimaryFg).
    Background(th.Semantics.ActionPrimaryBg)
```

## Development Workflow

### 1. Create Main File

```go
// cmd/myapp/main.go
package main

import (
    "texelation/internal/devshell"
    "texelation/apps/myapp"
)

func main() {
    ui := myapp.CreateUI()
    devshell.Run(ui)
}
```

### 2. Build and Run

```bash
go build -o myapp ./cmd/myapp
./myapp
```

### 3. Iterate

Edit code → rebuild → run. No server needed.

### 4. Later: Add TexelApp Support

When ready, add adapter for Texelation integration.

## Best Practices

### 1. Handle Terminal Oddities

```go
// Account for minimum terminal size
func (app *MyApp) Resize(w, h int) {
    if w < 40 || h < 10 {
        // Show "terminal too small" message
        return
    }
    // Normal resize
}
```

### 2. Clean Exit

```go
opts := devshell.Options{
    OnExit: func() {
        // Save state, cleanup resources
        saveConfig()
        closeFiles()
    },
}
```

### 3. Debug Output

```go
// Write to file, not terminal
f, _ := os.Create("debug.log")
log.SetOutput(f)
log.Println("Debug message")
```

### 4. Handle Ctrl+C

```go
// devshell handles SIGINT, but you can add cleanup
import "os/signal"

c := make(chan os.Signal, 1)
signal.Notify(c, os.Interrupt)
go func() {
    <-c
    cleanup()
    os.Exit(0)
}()
```

## Limitations

Standalone mode doesn't provide:

- Multi-app window management
- Automatic theming from Texelation
- Pane borders and decorations
- Desktop keyboard shortcuts (workspace switching, etc.)
- Session persistence

Use [TexelApp Mode](/texelui/integration/texelapp-mode.md) for these features.

## See Also

- [TexelApp Mode](/texelui/integration/texelapp-mode.md) - Running inside Texelation
- [Theme Integration](/texelui/integration/theme-integration.md) - Manual theming
- [Hello World](/texelui/getting-started/hello-world.md) - Minimal example

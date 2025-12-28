# TexelApp Mode

Running TexelUI applications inside Texelation.

## Overview

TexelApp mode integrates your TexelUI application into the Texelation desktop environment, enabling multi-app window management, theming, and more.

```
┌────────────────────────────────────────────────────────┐
│  Texelation Desktop                                    │
│  ┌────────────────────┐  ┌──────────────────────────┐ │
│  │  Terminal          │  │  Your TexelUI App        │ │
│  │                    │  │                          │ │
│  │  $ ls              │  │  ┌────────────────────┐  │ │
│  │  file.txt          │  │  │ Form               │  │ │
│  │  other.txt         │  │  │ Name: [________]   │  │ │
│  │  $ _               │  │  │ [Submit] [Cancel]  │  │ │
│  │                    │  │  └────────────────────┘  │ │
│  └────────────────────┘  └──────────────────────────┘ │
│  [Screen 1]  [Screen 2]  [Screen 3]     Status: Ready │
└────────────────────────────────────────────────────────┘
```

## The UIApp Adapter

The `adapter.UIApp` bridges UIManager to Texelation's App interface:

```go
import "texelation/texelui/adapter"

func New() texel.App {
    ui := core.NewUIManager()
    // ... add widgets ...
    return adapter.NewUIApp(ui, nil)
}
```

## Complete Example

```go
package myapp

import (
    "github.com/gdamore/tcell/v2"
    "texelation/texel"
    "texelation/texelui/adapter"
    "texelation/texelui/core"
    "texelation/texelui/layout"
    "texelation/texelui/widgets"
)

// New creates a new instance of the app
func New() texel.App {
    ui := core.NewUIManager()
    ui.SetLayout(layout.NewVBox(1))

    // Title
    title := widgets.NewLabel(0, 0, 40, 1, "Settings")
    title.Style = tcell.StyleDefault.Bold(true)

    // Form fields
    nameLabel := widgets.NewLabel(0, 0, 10, 1, "Name:")
    nameInput := widgets.NewInput(0, 0, 30)

    emailLabel := widgets.NewLabel(0, 0, 10, 1, "Email:")
    emailInput := widgets.NewInput(0, 0, 30)

    // Buttons
    saveBtn := widgets.NewButton(0, 0, 10, 1, "Save")
    saveBtn.OnActivate = func() {
        // Handle save
    }

    // Add widgets
    ui.AddWidget(title)
    ui.AddWidget(nameLabel)
    ui.AddWidget(nameInput)
    ui.AddWidget(emailLabel)
    ui.AddWidget(emailInput)
    ui.AddWidget(saveBtn)

    // Wrap in adapter
    return adapter.NewUIApp(ui, nil)
}
```

## Registering with Texelation

Add your app to the server's app registry:

```go
// cmd/texel-server/main.go
import "texelation/apps/myapp"

func main() {
    // ... server setup ...

    // Register app factory
    appRegistry["myapp"] = myapp.New

    // ... start server ...
}
```

## The App Interface

UIApp implements `texel.App`:

```go
type App interface {
    Run() error              // Start the app
    Stop()                   // Clean shutdown
    Resize(cols, rows int)   // Handle pane resize
    Render() [][]Cell        // Return cell buffer
    HandleKey(*tcell.EventKey)  // Process key input
}
```

### How UIApp Implements It

```go
type UIApp struct {
    ui     *core.UIManager
    config *Config
}

func (a *UIApp) Run() error {
    // Initialize UI
    return nil
}

func (a *UIApp) Stop() {
    // Cleanup
}

func (a *UIApp) Resize(cols, rows int) {
    a.ui.Resize(cols, rows)
}

func (a *UIApp) Render() [][]Cell {
    return a.ui.Render()
}

func (a *UIApp) HandleKey(ev *tcell.EventKey) {
    a.ui.HandleKey(ev)
}
```

## Configuration

Pass options via adapter.Config:

```go
config := &adapter.Config{
    // Refresh rate override
    RefreshRate: 30,  // fps

    // Custom initialization
    OnInit: func(ui *core.UIManager) {
        // Called after resize
    },
}

return adapter.NewUIApp(ui, config)
```

## Lifecycle Events

### Startup Sequence

```
1. Texelation creates pane
2. App factory called: New() → texel.App
3. App.Resize(cols, rows) with pane dimensions
4. App.Run() called
5. App.Render() called for initial display
```

### During Runtime

```
┌─────────────────────────────────────────────────────────────┐
│                                                             │
│  User action ───────►  Desktop ───────► Pane ──────► App   │
│                                                             │
│  - Keyboard          Routes to      Forwards    HandleKey  │
│  - Mouse             focused pane   events      HandleMouse│
│  - Resize                           to app      Resize     │
│                                                             │
│  Render loop ◄───────── Pane ◄─────── App.Render()         │
│                         Gets cell buffer                    │
└─────────────────────────────────────────────────────────────┘
```

### Shutdown Sequence

```
1. User closes pane or switches apps
2. App.Stop() called
3. App instance garbage collected
```

## Focus Management

### Pane Focus

When your pane gains/loses focus, Texelation handles the visual indicators (borders). Your app receives focus through the normal event flow.

### Widget Focus

Within your app, UIManager manages widget focus:

```go
// Check if pane has focus
if paneHasFocus {
    // Tab navigates between widgets
    ui.HandleKey(tabEvent)  // Moves focus
}
```

## Theming Integration

TexelApps can access Texelation's theme:

```go
// If SetTheme is called by Texelation
func (a *UIApp) SetTheme(theme *theme.Theme) {
    // Apply theme to widgets
    a.applyTheme(theme)
}

func (a *UIApp) applyTheme(th *theme.Theme) {
    buttonStyle := tcell.StyleDefault.
        Foreground(th.Semantics.ActionPrimaryFg).
        Background(th.Semantics.ActionPrimaryBg)

    for _, btn := range a.buttons {
        btn.Style = buttonStyle
    }
}
```

See [Theme Integration](/texelui/integration/theme-integration.md) for details.

## Optional Interfaces

UIApp can implement additional interfaces for extra features:

### StorageSetter

Access persistent storage:

```go
type StorageSetter interface {
    SetStorage(storage Storage)
}

func (a *UIApp) SetStorage(storage Storage) {
    a.storage = storage
}

// Later, save state
a.storage.Set("last_value", inputValue)
```

### PaneIDSetter

Know your pane ID:

```go
type PaneIDSetter interface {
    SetPaneID(id int)
}

func (a *UIApp) SetPaneID(id int) {
    a.paneID = id
}
```

### RefreshNotifier

Request redraws:

```go
type RefreshNotifierSetter interface {
    SetRefreshNotifier(chan<- bool)
}

func (a *UIApp) SetRefreshNotifier(ch chan<- bool) {
    a.refresh = ch
}

// Request redraw
a.refresh <- true
```

## Creating Both Modes

Support both standalone and TexelApp from the same code:

```go
// apps/myapp/myapp.go
package myapp

import (
    "texelation/texel"
    "texelation/texelui/adapter"
    "texelation/texelui/core"
)

// CreateUI builds the UI (shared)
func CreateUI() *core.UIManager {
    ui := core.NewUIManager()
    // ... add widgets ...
    return ui
}

// New creates a TexelApp
func New() texel.App {
    return adapter.NewUIApp(CreateUI(), nil)
}
```

```go
// cmd/myapp/main.go (standalone binary)
package main

import (
    "texelation/internal/devshell"
    "texelation/apps/myapp"
)

func main() {
    devshell.Run(myapp.CreateUI())
}
```

## Debugging

### Log Output

```go
import "log"

// Logs go to Texelation's log (usually stderr or log file)
log.Printf("MyApp: user clicked save")
```

### Render Debugging

```go
func (a *UIApp) Render() [][]Cell {
    cells := a.ui.Render()

    // Debug: print dimensions
    log.Printf("Render: %d rows", len(cells))
    if len(cells) > 0 {
        log.Printf("Render: %d cols", len(cells[0]))
    }

    return cells
}
```

## Best Practices

### 1. Handle Zero-Size Resize

```go
func (a *UIApp) Resize(cols, rows int) {
    if cols == 0 || rows == 0 {
        return  // Pane not visible
    }
    a.ui.Resize(cols, rows)
}
```

### 2. Clean Shutdown

```go
func (a *UIApp) Stop() {
    // Save state
    if a.storage != nil {
        a.saveState()
    }

    // Close resources
    if a.file != nil {
        a.file.Close()
    }
}
```

### 3. Minimal Render Work

```go
func (a *UIApp) Render() [][]Cell {
    // UIManager handles dirty tracking
    // Just return the buffer
    return a.ui.Render()
}
```

### 4. Respond to Theme Changes

```go
func (a *UIApp) SetTheme(theme *theme.Theme) {
    a.theme = theme
    a.updateStyles()
    // UIManager will redraw on next Render()
}
```

## See Also

- [Standalone Mode](/texelui/integration/standalone-mode.md) - Development mode
- [Theme Integration](/texelui/integration/theme-integration.md) - Using Texelation themes
- [Architecture](/texelui/core-concepts/architecture.md) - System overview

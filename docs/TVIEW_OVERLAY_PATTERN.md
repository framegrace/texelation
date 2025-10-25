# TView Overlay Pattern

## Overview

The **overlay pattern** adds tview widgets on top of existing apps with minimal changes. Your app continues to render normally - tview is just composited on top.

## Use Cases

- Add menu bars to terminals
- Add status bars to any app
- Add dialogs/popups over content
- Add HUDs to games

## The Pattern

### Step 1: Your Existing App (No Changes Needed!)

```go
// Your existing app - works as before
func New(title, shell string) texel.App {
    return &TexelTerm{
        title: title,
        shell: shell,
    }
}

func (t *TexelTerm) Render() [][]texel.Cell {
    // Your normal rendering - terminal output, game graphics, etc.
    return t.renderTerminal()
}

// All other methods stay the same (Run, Stop, Resize, HandleKey, etc.)
```

### Step 2: Define Your Overlay Widget

Create a function that returns the tview widget(s) you want to overlay:

```go
// In your app's package (e.g., apps/texelterm/menu_bar.go)
func CreateMenuBar() tview.Primitive {
    menuBar := tview.NewTextView()
    menuBar.SetBackgroundColor(tcell.ColorDarkBlue) // Opaque
    menuBar.SetText(" File  Edit  View  Help")

    // Position at top with flex
    flex := tview.NewFlex().SetDirection(tview.FlexRow).
        AddItem(menuBar, 1, 0, false).  // Menu at top (1 row, opaque)
        AddItem(nil, 0, 1, false)       // Rest transparent (base shows)

    return flex
}
```

### Step 3: Wrap Your App (Minimal Change!)

In your app factory, just wrap with `WithOverlay()`:

```go
import "texelation/internal/tviewapps"

func NewWithMenu(title, shell string) texel.App {
    baseApp := New(title, shell)
    return tviewapps.WithOverlay(baseApp, CreateMenuBar)
}
```

**That's it!** Your app now has a menu bar overlay.

## Complete Example: TexelTerm with Menu

### Before (No Menu)

```go
func main() {
    shellFactory := func() texel.App {
        return texelterm.New("shell", "/bin/bash")
    }
    // ...
}
```

### After (With Menu - 2 Lines Changed!)

```go
import "texelation/internal/tviewapps"

func main() {
    shellFactory := func() texel.App {
        baseApp := texelterm.New("shell", "/bin/bash")
        return tviewapps.WithOverlay(baseApp, texelterm.CreateMenuBar)
    }
    // ...
}
```

## How It Works

1. **Base App Renders**: Your app's `Render()` is called normally
2. **TView Renders**: The overlay widget renders to its own buffer
3. **Composite**: Overlay buffer is composited on top of base buffer
4. **Transparency**: Default background cells in overlay are transparent

## Transparency Rules

In your overlay widgets:

- `SetBackgroundColor(tcell.ColorDefault)` = **Transparent** (base shows through)
- `SetBackgroundColor(tcell.ColorDarkBlue)` = **Opaque** (blocks base)
- `nil` Flex items = **Transparent** (base shows through)

## Example Overlay Widgets

### Top Menu Bar

```go
func CreateMenuBar() tview.Primitive {
    menu := tview.NewTextView()
    menu.SetBackgroundColor(tcell.ColorDarkBlue)
    menu.SetText(" File  Edit  View  Help")

    return tview.NewFlex().SetDirection(tview.FlexRow).
        AddItem(menu, 1, 0, false).     // 1 row opaque menu
        AddItem(nil, 0, 1, false)       // Rest transparent
}
```

### Bottom Status Bar

```go
func CreateStatusBar() tview.Primitive {
    status := tview.NewTextView()
    status.SetBackgroundColor(tcell.ColorDarkGreen)
    status.SetText(" Ready | Line 1, Col 1")

    return tview.NewFlex().SetDirection(tview.FlexRow).
        AddItem(nil, 0, 1, false).       // Top transparent
        AddItem(status, 1, 0, false)     // 1 row opaque status
}
```

### Menu + Status (Both)

```go
func CreateMenuAndStatus() tview.Primitive {
    menu := tview.NewTextView()
    menu.SetBackgroundColor(tcell.ColorDarkBlue)
    menu.SetText(" File  Edit  View  Help")

    status := tview.NewTextView()
    status.SetBackgroundColor(tcell.ColorDarkGreen)
    status.SetText(" Ready")

    return tview.NewFlex().SetDirection(tview.FlexRow).
        AddItem(menu, 1, 0, false).      // Top
        AddItem(nil, 0, 1, false).       // Middle transparent
        AddItem(status, 1, 0, false)     // Bottom
}
```

### Centered Dialog

```go
func CreateDialog() tview.Primitive {
    dialog := tview.NewTextView()
    dialog.SetBackgroundColor(tcell.ColorDarkRed)
    dialog.SetBorder(true).SetTitle(" Alert ")
    dialog.SetText("\n  Are you sure?  \n\n  [Y]es  [N]o  ")

    // Center with transparent spacers
    return tview.NewFlex().
        AddItem(nil, 0, 1, false).       // Left spacer
        AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
            AddItem(nil, 0, 1, false).   // Top spacer
            AddItem(dialog, 5, 0, false). // Dialog box
            AddItem(nil, 0, 1, false),   // Bottom spacer
            30, 0, false).                // Width: 30 cols
        AddItem(nil, 0, 1, false)        // Right spacer
}
```

## Key Benefits

✅ **Minimal Changes** - Existing apps work as-is
✅ **Composable** - Stack multiple overlays
✅ **Transparent** - Base content shows through
✅ **Interactive** - Menus/forms work automatically
✅ **No Rewrites** - Add features without changing core logic

## When to Use Each Pattern

### Use Overlay Pattern When:
- Adding features to **existing apps** (menus, HUD, dialogs)
- App has primary content that should always be visible
- TView is **secondary/optional**

### Use SimpleTViewApp When:
- Creating **new apps** where tview is the main UI
- App is primarily tview widgets (forms, lists, dashboards)
- TView is **primary**

## See Also

- `apps/texelterm/menu_bar.go` - Menu bar examples
- `internal/tviewapps/overlay.go` - Implementation
- `internal/tviewapps/simple_app.go` - SimpleTViewApp (tview-first pattern)

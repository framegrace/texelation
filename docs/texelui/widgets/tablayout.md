# TabLayout

A low-level tabbed container with switchable content panels.

> **Note**: For most use cases, consider using [TabPanel](/texelui/widgets/tabpanel.md) instead, which provides a simpler API with `AddTab(label, content)`.

```
┌─────────────────────────────────────────┐
│ ► Tab 1   Tab 2   Tab 3                 │
├─────────────────────────────────────────┤
│                                         │
│   Content of selected tab               │
│                                         │
└─────────────────────────────────────────┘
```

## Import

```go
import (
    "texelation/texelui/widgets"
    "texelation/texelui/primitives"
)
```

## Constructor

```go
func NewTabLayout(tabs []primitives.TabItem) *TabLayout
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `tabs` | `[]TabItem` | Tab definitions |

Creates a tabbed container. Position defaults to (0,0) and size to 40x10.
Use `SetPosition(x, y)` and `Resize(w, h)` to adjust, or place in a layout container.

### TabItem

```go
type TabItem struct {
    Label string  // Tab display text
    ID    string  // Tab identifier
}
```

## Methods

| Method | Description |
|--------|-------------|
| `SetTabContent(idx int, w Widget)` | Set content for a tab |
| `SetActive(idx int)` | Switch to a tab |
| `ActiveIndex() int` | Get current tab index |

## Example

```go
package main

import (
    "log"

    "texelation/internal/devshell"
    "texelation/texel"
    "texelation/texelui/adapter"
    "texelation/texelui/core"
    "texelation/texelui/primitives"
    "texelation/texelui/widgets"
)

func main() {
    err := devshell.Run(func(args []string) (texel.App, error) {
        ui := core.NewUIManager()

        // Define tabs
        tabs := []primitives.TabItem{
            {Label: "Home", ID: "home"},
            {Label: "Settings", ID: "settings"},
            {Label: "About", ID: "about"},
        }

        // Create tab layout
        tabLayout := widgets.NewTabLayout(tabs)

        // Create content for each tab using VBox layouts
        homeVBox := widgets.NewVBox()
        homeVBox.AddChild(widgets.NewLabel("Welcome to the Home tab!"))
        tabLayout.SetTabContent(0, homeVBox)

        settingsVBox := widgets.NewVBox()
        settingsVBox.Spacing = 1
        settingsVBox.AddChild(widgets.NewLabel("Settings go here"))
        settingsVBox.AddChild(widgets.NewCheckbox("Enable feature"))
        tabLayout.SetTabContent(1, settingsVBox)

        aboutVBox := widgets.NewVBox()
        aboutVBox.AddChild(widgets.NewLabel("Version 1.0.0"))
        tabLayout.SetTabContent(2, aboutVBox)

        ui.AddWidget(tabLayout)
        ui.Focus(tabLayout)

        app := adapter.NewUIApp("TabLayout Demo", ui)
        app.OnResize(func(w, h int) {
            tabLayout.SetPosition(5, 3)
            tabLayout.Resize(60, 18)
        })
        return app, nil
    }, nil)

    if err != nil {
        log.Fatal(err)
    }
}
```

## Behavior

### Tab Navigation

| Key | Action |
|-----|--------|
| Left/Right | Switch tabs (when tab bar focused) |
| 1-9 | Jump to tab by number |
| Tab | Move focus between tab bar and content |
| Shift+Tab | Move focus backwards |

### Mouse

| Action | Result |
|--------|--------|
| Click tab | Switch to that tab |
| Click content | Focus content widget |

### Focus Areas

TabLayout has two focus areas:
1. **Tab bar** - Navigate between tabs
2. **Content** - Interact with tab content

When tab bar is focused:
- Left/Right switches tabs
- Number keys jump to tabs
- Tab moves focus to content

When content is focused:
- Tab moves focus back to tab bar
- Content widgets handle input normally

### Active Tab Indicator

The active tab is shown with:
- Focus marker `►` when focused
- Bold/reverse styling

```
► Home   Settings   About
  ^^^^
  Active tab (focused)
```

## Content Area

The content area is below the tab bar:

```
Total height: 18
┌─────────────────────────────┐
│ Tab bar (height: 1)        │ ← Row 0
├─────────────────────────────┤
│                             │
│ Content area (height: 17)  │ ← Rows 1-17
│                             │
└─────────────────────────────┘
```

Content widgets should be sized to fit this area.

## Programmatic Tab Switching

```go
tabLayout := widgets.NewTabLayout(tabs)

// Switch to second tab (index 1)
tabLayout.SetActive(1)

// Get current tab index
current := tabLayout.ActiveIndex()
```

## Dynamic Content

You can change tab content at runtime:

```go
// Initially empty tab
tabLayout.SetTabContent(0, nil)

// Later, set content
tabLayout.SetTabContent(0, myWidget)
```

## Complex Tab Content

Each tab can contain complex widget hierarchies using layout containers:

```go
// Create complex settings panel with Form widget
form := widgets.NewForm()

// Add form fields
themes := []string{"Light", "Dark", "System"}
form.AddField("Theme:", widgets.NewComboBox(themes, false))

fontInput := widgets.NewInput()
fontInput.Text = "14"
form.AddField("Font Size:", fontInput)

form.AddSpacer(1)

saveBtn := widgets.NewButton("Save")
form.AddFullWidthField(saveBtn, 1)

tabLayout.SetTabContent(1, form)
```

## Implementation Details

### Source File
`texelui/widgets/tablayout.go`

### Interfaces Implemented
- `core.Widget` (via `BaseWidget`)
- `core.MouseAware`
- `core.InvalidationAware`
- `core.ChildContainer`
- `core.HitTester`

### Uses Primitives
- `TabBar` for the tab header

### Content Rect Calculation

```go
func (tl *TabLayout) contentRect() core.Rect {
    return core.Rect{
        X: tl.Rect.X,
        Y: tl.Rect.Y + 1,  // Below tab bar
        W: tl.Rect.W,
        H: tl.Rect.H - 1,  // Minus tab bar
    }
}
```

## See Also

- [TabPanel](/texelui/widgets/tabpanel.md) - Higher-level tabbed container with simpler API
- [TabBar](/texelui/primitives/tabbar.md) - Tab bar primitive
- [Pane](/texelui/widgets/pane.md) - Container widget
- [Border](/texelui/widgets/border.md) - Border decorator

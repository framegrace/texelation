# TabLayout

A tabbed container with switchable content panels.

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
func NewTabLayout(x, y, w, h int, tabs []primitives.TabItem) *TabLayout
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `x` | `int` | X position |
| `y` | `int` | Y position |
| `w` | `int` | Total width |
| `h` | `int` | Total height |
| `tabs` | `[]TabItem` | Tab definitions |

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

    "github.com/gdamore/tcell/v2"
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
        tabLayout := widgets.NewTabLayout(5, 3, 60, 18, tabs)

        // Create content for each tab
        homePane := widgets.NewPane(0, 0, 60, 16, tcell.StyleDefault)
        homeLabel := widgets.NewLabel(2, 2, 40, 1, "Welcome to the Home tab!")
        homePane.AddChild(homeLabel)

        settingsPane := widgets.NewPane(0, 0, 60, 16, tcell.StyleDefault)
        settingsLabel := widgets.NewLabel(2, 2, 40, 1, "Settings go here")
        settingsCheck := widgets.NewCheckbox(2, 4, "Enable feature")
        settingsPane.AddChild(settingsLabel)
        settingsPane.AddChild(settingsCheck)

        aboutPane := widgets.NewPane(0, 0, 60, 16, tcell.StyleDefault)
        aboutLabel := widgets.NewLabel(2, 2, 40, 1, "Version 1.0.0")
        aboutPane.AddChild(aboutLabel)

        // Set content for each tab
        tabLayout.SetTabContent(0, homePane)
        tabLayout.SetTabContent(1, settingsPane)
        tabLayout.SetTabContent(2, aboutPane)

        ui.AddWidget(tabLayout)
        ui.Focus(tabLayout)

        return adapter.NewUIApp("TabLayout Demo", ui), nil
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
tabLayout := widgets.NewTabLayout(0, 0, 60, 20, tabs)

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

Each tab can contain complex widget hierarchies:

```go
// Create complex settings panel
settingsPane := widgets.NewPane(0, 0, 58, 15, tcell.StyleDefault)

// Add form elements
themeLabel := widgets.NewLabel(2, 2, 15, 1, "Theme:")
themeCombo := widgets.NewComboBox(18, 2, 25, themes, false)

fontLabel := widgets.NewLabel(2, 4, 15, 1, "Font Size:")
fontInput := widgets.NewInput(18, 4, 10)

saveBtn := widgets.NewButton(2, 8, 12, 1, "Save")

settingsPane.AddChild(themeLabel)
settingsPane.AddChild(themeCombo)
settingsPane.AddChild(fontLabel)
settingsPane.AddChild(fontInput)
settingsPane.AddChild(saveBtn)

tabLayout.SetTabContent(1, settingsPane)
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

- [TabBar](/texelui/primitives/tabbar.md) - Tab bar primitive
- [Pane](/texelui/widgets/pane.md) - Container widget
- [Border](/texelui/widgets/border.md) - Border decorator

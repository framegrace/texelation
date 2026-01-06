# TabPanel Widget

High-level tab container with simple AddTab API.

## Overview

TabPanel provides a tabbed interface for switching between multiple content panels. It combines a tab bar with switchable content areas and handles focus management automatically.

```
┌─────────────────────────────────────────────────────┐
│  ▸ General │ Advanced │ About                       │
├─────────────────────────────────────────────────────┤
│                                                     │
│    Content for "General" tab                        │
│                                                     │
│    [x] Enable feature                               │
│    [x] Show notifications                           │
│                                                     │
└─────────────────────────────────────────────────────┘
```

## Import

```go
import "texelation/texelui/widgets"
```

## Constructor

```go
func NewTabPanel() *TabPanel
```

Creates an empty TabPanel. Use AddTab to add tabs.

## Methods

### Tab Management

```go
// Add a new tab with name and content widget
func (tp *TabPanel) AddTab(name string, content Widget) int

// Add a tab with custom ID
func (tp *TabPanel) AddTabWithID(name, id string, content Widget) int

// Remove a tab by index
func (tp *TabPanel) RemoveTab(idx int)

// Remove all tabs
func (tp *TabPanel) ClearTabs()

// Get number of tabs
func (tp *TabPanel) TabCount() int
```

### Tab Selection

```go
// Get/Set active tab index
func (tp *TabPanel) ActiveIndex() int
func (tp *TabPanel) SetActive(idx int)

// Tab change callback
func (tp *TabPanel) OnTabChange(fn func(idx int))
```

### Content Updates

```go
// Update content for a specific tab
func (tp *TabPanel) SetTabContent(idx int, content Widget)
```

### Focus

```go
// Control focus behavior at boundaries
func (tp *TabPanel) SetTrapsFocus(trap bool)
func (tp *TabPanel) TrapsFocus() bool
```

## Example: Settings Dialog

```go
package main

import (
    "texelation/internal/devshell"
    "texelation/texel"
    "texelation/texelui/adapter"
    "texelation/texelui/core"
    "texelation/texelui/widgets"
)

func main() {
    devshell.Run(func(args []string) (texel.App, error) {
        ui := core.NewUIManager()

        // Create tab panel
        panel := widgets.NewTabPanel()

        // General settings tab
        general := widgets.NewVBox()
        general.Spacing = 1
        general.AddChild(widgets.NewCheckbox("Enable auto-save"))
        general.AddChild(widgets.NewCheckbox("Show welcome screen"))
        general.AddChild(widgets.NewCheckbox("Check for updates"))
        panel.AddTab("General", general)

        // Appearance tab
        appearance := widgets.NewVBox()
        appearance.Spacing = 1
        appearance.AddChild(widgets.NewCheckbox("Dark mode"))
        appearance.AddChild(widgets.NewCheckbox("Large fonts"))
        appearance.AddChild(widgets.NewCheckbox("High contrast"))
        panel.AddTab("Appearance", appearance)

        // About tab
        about := widgets.NewVBox()
        about.Spacing = 1
        about.AddChild(widgets.NewLabel("My Application v1.0"))
        about.AddChild(widgets.NewLabel("Copyright 2025"))
        panel.AddTab("About", about)

        ui.AddWidget(panel)
        ui.Focus(panel)

        app := adapter.NewUIApp("Settings", ui)
        app.OnResize(func(w, h int) {
            panel.SetPosition(2, 2)
            panel.Resize(w-4, h-4)
        })
        return app, nil
    }, nil)
}
```

## Keyboard Navigation

| Key | Action |
|-----|--------|
| Left/Right | Switch between tabs (when tab bar focused) |
| Tab | Move focus to content, then between content widgets |
| Shift+Tab | Move focus backwards |
| Enter/Space | Activate tab (when tab bar focused) |

## Tab Change Callback

React to tab changes:

```go
panel.OnTabChange(func(idx int) {
    switch idx {
    case 0:
        statusLabel.Text = "General settings"
    case 1:
        statusLabel.Text = "Appearance settings"
    case 2:
        statusLabel.Text = "About this app"
    }
})
```

## Dynamic Tabs

Add and remove tabs at runtime:

```go
// Add a tab
newIdx := panel.AddTab("New Tab", content)

// Remove a tab
panel.RemoveTab(newIdx)

// Replace content
panel.SetTabContent(0, newContent)
```

## Focus Trapping

By default, Tab/Shift+Tab can escape the panel. To trap focus inside:

```go
panel.SetTrapsFocus(true)  // Focus cycles within panel
```

## Common Patterns

### Form with Sections

```go
panel := widgets.NewTabPanel()

// Personal info section
personal := widgets.NewVBox()
personal.Spacing = 1
personal.AddChild(createFormRow("Name:", widgets.NewInput()))
personal.AddChild(createFormRow("Email:", widgets.NewInput()))
panel.AddTab("Personal", personal)

// Address section
address := widgets.NewVBox()
address.Spacing = 1
address.AddChild(createFormRow("Street:", widgets.NewInput()))
address.AddChild(createFormRow("City:", widgets.NewInput()))
address.AddChild(createFormRow("Country:", widgets.NewComboBox(countries, true)))
panel.AddTab("Address", address)
```

### Settings with Categories

```go
panel := widgets.NewTabPanel()

categories := map[string][]string{
    "General":    {"Auto-save", "Show tooltips", "Confirm exit"},
    "Editor":     {"Line numbers", "Word wrap", "Highlight line"},
    "Terminal":   {"Copy on select", "Scroll on output"},
}

for name, options := range categories {
    content := widgets.NewVBox()
    content.Spacing = 1
    for _, opt := range options {
        content.AddChild(widgets.NewCheckbox(opt))
    }
    panel.AddTab(name, content)
}
```

### Tab with ScrollPane

For tabs with lots of content:

```go
panel := widgets.NewTabPanel()

// Create scrollable content
sp := scroll.NewScrollPane()
content := widgets.NewVBox()
content.Spacing = 1
// ... add many widgets to content
sp.SetChild(content)
sp.SetContentHeight(50)  // Estimate content height

panel.AddTab("Long Content", sp)
```

## Comparison with TabLayout

| Feature | TabPanel | TabLayout |
|---------|----------|-----------|
| Constructor | `NewTabPanel()` | `NewTabLayout(tabs)` |
| Tab definition | Dynamic (`AddTab`) | Pre-defined in constructor |
| Use case | Runtime tab changes | Static tab structure |
| Simplicity | Higher-level | Lower-level |

Use **TabPanel** when:
- Adding/removing tabs dynamically
- Simple AddTab API is preferred

Use **TabLayout** when:
- Tabs are known at build time
- Need more control over tab bar

## Tips

1. **Set panel size** - Use OnResize to give TabPanel appropriate dimensions

2. **Focus trapping** - Enable for modal dialogs with tabs

3. **Scrollable content** - Wrap long content in ScrollPane

4. **Tab order** - Tabs are ordered by AddTab call order

## See Also

- [TabLayout](/texelui/widgets/tablayout.md) - Lower-level tab widget
- [VBox](/texelui/layout/vbox.md) - Common tab content container
- [ScrollPane](/texelui/layout/scrollpane.md) - For scrollable tab content

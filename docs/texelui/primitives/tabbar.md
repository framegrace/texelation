# TabBar

A horizontal tab navigation bar.

```
► Tab 1   Tab 2   Tab 3
  ^^^^^
  Active tab (focused)
```

## Import

```go
import "texelation/texelui/primitives"
```

## Constructor

```go
func NewTabBar(x, y, w int, tabs []TabItem) *TabBar
```

### TabItem

```go
type TabItem struct {
    Label string  // Display text
    ID    string  // Identifier
}
```

## Properties

| Property | Type | Description |
|----------|------|-------------|
| `Style` | `tcell.Style` | Normal tab style |
| `ActiveStyle` | `tcell.Style` | Active tab style |

## Methods

| Method | Description |
|--------|-------------|
| `SetActive(idx int)` | Set active tab |
| `ActiveIndex() int` | Get active tab index |
| `ActiveID() string` | Get active tab ID |

## Example

```go
package main

import (
    "texelation/texelui/core"
    "texelation/texelui/primitives"
)

func createTabBar() *primitives.TabBar {
    tabs := []primitives.TabItem{
        {Label: "Home", ID: "home"},
        {Label: "Settings", ID: "settings"},
        {Label: "About", ID: "about"},
    }

    tabBar := primitives.NewTabBar(5, 3, 50, tabs)
    return tabBar
}
```

## Behavior

### Keyboard Navigation

| Key | Action |
|-----|--------|
| Left | Previous tab |
| Right | Next tab |
| 1-9 | Jump to tab by number |

### Mouse

| Action | Result |
|--------|--------|
| Click tab | Select tab |

### Visual States

| State | Appearance |
|-------|------------|
| Inactive | Normal text |
| Active (unfocused) | Bold |
| Active (focused) | `►` marker + Bold + Reverse |

```
Unfocused:  Home   Settings   About
                   ^^^^^^^^ bold

Focused:  ► Home   Settings   About
          ^^^^^^ bold + reverse + marker
```

## Tab Switching

```go
tabBar := primitives.NewTabBar(0, 0, 50, tabs)

// Switch to second tab
tabBar.SetActive(1)

// Get current tab
idx := tabBar.ActiveIndex()  // 1
id := tabBar.ActiveID()      // "settings"
```

## Responding to Tab Changes

TabBar doesn't have a callback; check the active index after handling input:

```go
func (w *MyWidget) HandleKey(ev *tcell.EventKey) bool {
    oldIdx := w.tabBar.ActiveIndex()

    if w.tabBar.HandleKey(ev) {
        newIdx := w.tabBar.ActiveIndex()
        if newIdx != oldIdx {
            w.onTabChanged(newIdx)
        }
        return true
    }
    return false
}
```

## Used By

- **ColorPicker** - Mode switching (Semantic/Palette/OKLCH)
- **TabLayout** - Tab header

## Implementation Details

### Source File
`texelui/primitives/tabbar.go`

### Height

TabBar always has a height of 1.

### Tab Spacing

Tabs are separated by spaces:

```
Tab1   Tab2   Tab3
    ^^^    ^^^
    spacing
```

## See Also

- [TabLayout](../widgets/tablayout.md) - Full tabbed container
- [ScrollableList](scrollablelist.md) - Vertical list
- [Grid](grid.md) - 2D grid

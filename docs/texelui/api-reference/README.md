# API Reference

Complete reference for TexelUI interfaces and types.

## Package Structure

```
texelui/
├── core/           # Core types and interfaces
│   ├── widget.go       # Widget interface, BaseWidget
│   ├── uimanager.go    # UIManager, focus, events
│   ├── painter.go      # Drawing primitives
│   └── rect.go         # Geometry types
├── widgets/        # Standard widgets
│   ├── button.go
│   ├── input.go
│   ├── label.go
│   ├── checkbox.go
│   ├── textarea.go
│   ├── combobox.go
│   ├── colorpicker.go
│   ├── pane.go
│   ├── border.go
│   └── tablayout.go
├── primitives/     # Reusable building blocks
│   ├── scrollablelist.go
│   ├── grid.go
│   └── tabbar.go
├── layout/         # Layout managers
│   ├── vbox.go
│   └── hbox.go
└── adapter/        # Texelation integration
    └── texel_app.go
```

## Core Types

### Widget Interface

```go
type Widget interface {
    Position() Point
    SetPosition(x, y int)
    Size() Size
    Resize(w, h int)
    Draw(p *Painter)
    HandleKey(ev *tcell.EventKey) bool
    IsFocusable() bool
}
```

See [Interfaces Reference](interfaces.md) for all interfaces.

### Geometry Types

```go
// Point represents a 2D coordinate
type Point struct {
    X, Y int
}

// Size represents dimensions
type Size struct {
    W, H int
}

// Rect combines position and size
type Rect struct {
    X, Y, W, H int
}

// Rect methods
func (r Rect) Contains(x, y int) bool
func (r Rect) Intersects(other Rect) bool
func (r Rect) Intersection(other Rect) Rect
func (r Rect) IsEmpty() bool
```

### Cell Type

```go
// Cell represents a single terminal cell
type Cell struct {
    Rune  rune
    Style tcell.Style
}
```

## UIManager

Central manager for widgets, focus, and rendering.

```go
// Creation
func NewUIManager() *UIManager

// Widget Management
func (u *UIManager) AddWidget(w Widget)
func (u *UIManager) RemoveWidget(w Widget)
func (u *UIManager) ClearWidgets()
func (u *UIManager) Widgets() []Widget

// Layout
func (u *UIManager) SetLayout(l Layout)
func (u *UIManager) Layout() Layout

// Focus
func (u *UIManager) FocusedWidget() Widget
func (u *UIManager) SetFocus(w Widget)
func (u *UIManager) FocusNext()
func (u *UIManager) FocusPrev()

// Events
func (u *UIManager) HandleKey(ev *tcell.EventKey) bool
func (u *UIManager) HandleMouse(ev *tcell.EventMouse) bool

// Rendering
func (u *UIManager) Resize(w, h int)
func (u *UIManager) Render() [][]Cell

// Invalidation
func (u *UIManager) Invalidate()
func (u *UIManager) InvalidateRect(r Rect)
```

## Painter

Drawing primitives with automatic clipping.

```go
// Drawing
func (p *Painter) SetCell(x, y int, ch rune, style tcell.Style)
func (p *Painter) DrawText(x, y int, text string, style tcell.Style)
func (p *Painter) DrawTextClipped(x, y, maxW int, text string, style tcell.Style)
func (p *Painter) Fill(r Rect, ch rune, style tcell.Style)
func (p *Painter) Clear(r Rect, style tcell.Style)
func (p *Painter) DrawBorder(r Rect, style tcell.Style, borderType BorderType)

// Clipping
func (p *Painter) WithClip(r Rect) *Painter
func (p *Painter) Clip() Rect
```

## Layout Interface

```go
type Layout interface {
    Apply(container Rect, children []Widget)
}
```

**Implementations:**
- `layout.NewVBox(spacing int) *VBox`
- `layout.NewHBox(spacing int) *HBox`

## Standard Widgets

### Button

```go
func NewButton(x, y, w, h int, text string) *Button

// Properties
button.Text string
button.Style tcell.Style
button.FocusStyle tcell.Style
button.OnActivate func()
```

### Input

```go
func NewInput(x, y, width int) *Input

// Methods
func (i *Input) Text() string
func (i *Input) SetText(t string)
func (i *Input) Clear()

// Properties
input.Style tcell.Style
input.FocusStyle tcell.Style
input.Placeholder string
input.OnChange func(text string)
input.OnSubmit func(text string)
```

### Label

```go
func NewLabel(x, y, w, h int, text string) *Label

// Properties
label.Text string
label.Style tcell.Style
label.Align Alignment  // AlignLeft, AlignCenter, AlignRight
```

### Checkbox

```go
func NewCheckbox(x, y, w int, label string) *Checkbox

// Methods
func (c *Checkbox) Checked() bool
func (c *Checkbox) SetChecked(checked bool)
func (c *Checkbox) Toggle()

// Properties
checkbox.Label string
checkbox.Style tcell.Style
checkbox.CheckedStyle tcell.Style
checkbox.OnChange func(checked bool)
```

### TextArea

```go
func NewTextArea(x, y, w, h int) *TextArea

// Methods
func (t *TextArea) Text() string
func (t *TextArea) SetText(text string)
func (t *TextArea) AppendText(text string)
func (t *TextArea) Clear()

// Properties
textarea.Style tcell.Style
textarea.Wrap bool
textarea.ReadOnly bool
```

### ComboBox

```go
func NewComboBox(x, y, w int, options []string) *ComboBox

// Methods
func (c *ComboBox) SelectedIndex() int
func (c *ComboBox) SetSelectedIndex(idx int)
func (c *ComboBox) SelectedValue() string
func (c *ComboBox) SetOptions(options []string)

// Properties
combobox.Style tcell.Style
combobox.DropdownStyle tcell.Style
combobox.OnChange func(index int, value string)
```

### ColorPicker

```go
func NewColorPicker(x, y, w, h int) *ColorPicker

// Methods
func (c *ColorPicker) Color() tcell.Color
func (c *ColorPicker) SetColor(color tcell.Color)

// Properties
colorpicker.OnColorChange func(color tcell.Color)
```

### Pane

```go
func NewPane(x, y, w, h int, style tcell.Style) *Pane

// Child Management
func (p *Pane) AddChild(w Widget)
func (p *Pane) RemoveChild(w Widget)
func (p *Pane) ClearChildren()
func (p *Pane) Children() []Widget

// Properties
pane.Style tcell.Style
pane.Layout Layout
```

### Border

```go
func NewBorder(x, y, w, h int, style tcell.Style) *Border

// Methods
func (b *Border) SetContent(w Widget)
func (b *Border) Content() Widget

// Properties
border.Style tcell.Style
border.Title string
border.BorderType BorderType  // Single, Double, Rounded, etc.
```

### TabLayout

```go
func NewTabLayout(x, y, w, h int, tabs []TabItem) *TabLayout

// Methods
func (t *TabLayout) AddTab(tab TabItem)
func (t *TabLayout) SetActiveTab(index int)
func (t *TabLayout) ActiveTab() int
func (t *TabLayout) SetContent(index int, content Widget)

// Types
type TabItem struct {
    Label   string
    ID      string
    Content Widget
}

// Properties
tablayout.OnTabChange func(index int)
```

## Primitives

### ScrollableList

```go
func NewScrollableList(x, y, w, h int) *ScrollableList

// Methods
func (s *ScrollableList) SetItems(items []interface{})
func (s *ScrollableList) GetItems() []interface{}
func (s *ScrollableList) SelectedIndex() int
func (s *ScrollableList) SetSelectedIndex(idx int)
func (s *ScrollableList) SelectedItem() interface{}

// Properties
list.Style tcell.Style
list.SelectedStyle tcell.Style
list.Renderer ListItemRenderer
```

### Grid

```go
func NewGrid(x, y, w, h int) *Grid

// Methods
func (g *Grid) SetItems(items []interface{})
func (g *Grid) GetItems() []interface{}
func (g *Grid) SelectedIndex() int
func (g *Grid) SetSelectedIndex(idx int)
func (g *Grid) SelectedItem() interface{}

// Properties
grid.MinCellWidth int
grid.MaxCols int
grid.Style tcell.Style
grid.SelectedStyle tcell.Style
grid.Renderer GridCellRenderer
```

### TabBar

```go
func NewTabBar(x, y, w int, tabs []TabItem) *TabBar

// Methods
func (t *TabBar) SetActive(idx int)
func (t *TabBar) ActiveIndex() int
func (t *TabBar) ActiveID() string

// Properties
tabbar.Style tcell.Style
tabbar.ActiveStyle tcell.Style
```

## Adapter

### UIApp

```go
func NewUIApp(ui *UIManager, config *Config) *UIApp

// Config
type Config struct {
    RefreshRate int
    OnInit      func(*UIManager)
}

// Implements texel.App interface
func (a *UIApp) Run() error
func (a *UIApp) Stop()
func (a *UIApp) Resize(cols, rows int)
func (a *UIApp) Render() [][]Cell
func (a *UIApp) HandleKey(ev *tcell.EventKey)
```

## Import Paths

```go
import "texelation/texelui/core"
import "texelation/texelui/widgets"
import "texelation/texelui/primitives"
import "texelation/texelui/layout"
import "texelation/texelui/adapter"
```

## See Also

- [Interfaces Reference](interfaces.md) - All interfaces
- [Widget Interface](../core-concepts/widget-interface.md) - Detailed explanation
- [Architecture](../core-concepts/architecture.md) - System overview

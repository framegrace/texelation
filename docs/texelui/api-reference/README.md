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

See [Interfaces Reference](/texelui/api-reference/interfaces.md) for all interfaces.

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
button.OnClick func()
```

### Input

```go
func NewInput(x, y, width int) *Input

// Properties
input.Text string          // Current text content
input.CaretPos int         // Caret position in runes
input.Placeholder string   // Hint text when empty
input.Style tcell.Style
input.CaretStyle tcell.Style
input.OnChange func(string)
```

### Label

```go
func NewLabel(x, y, w, h int, text string) *Label

// Properties
label.Text string
label.Style tcell.Style
```

### Checkbox

```go
func NewCheckbox(x, y int, label string) *Checkbox

// Properties
checkbox.Label string
checkbox.Checked bool
checkbox.Style tcell.Style
checkbox.OnChange func(checked bool)
```

### TextArea

```go
func NewTextArea(x, y, w, h int) *TextArea

// Methods
func (t *TextArea) Text() string           // Get all content as string
func (t *TextArea) SetText(text string)    // Set content from string

// Properties
textarea.Lines []string    // Text content as lines (direct access)
textarea.CaretX int        // Caret column position
textarea.CaretY int        // Caret line position
textarea.OffY int          // Vertical scroll offset
textarea.Style tcell.Style
textarea.CaretStyle tcell.Style
textarea.OnChange func(text string)  // Called on content changes
```

### ComboBox

```go
func NewComboBox(x, y, w int, items []string, editable bool) *ComboBox

// Methods
func (c *ComboBox) Value() string
func (c *ComboBox) SetValue(text string)

// Properties
combobox.Style tcell.Style
combobox.OnChange func(ColorPickerResult)
```

### ColorPicker

```go
func NewColorPicker(x, y int, config ColorPickerConfig) *ColorPicker

// Methods
func (c *ColorPicker) SetValue(colorStr string)
func (c *ColorPicker) GetResult() ColorPickerResult
func (c *ColorPicker) Toggle()
func (c *ColorPicker) Expand()
func (c *ColorPicker) Collapse()

// Properties
colorpicker.OnChange func(ColorPickerResult)
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
func (b *Border) SetChild(w Widget)

// Properties
border.Style tcell.Style
border.Title string
```

### TabLayout

```go
func NewTabLayout(x, y, w, h int, tabs []primitives.TabItem) *TabLayout

// Methods
func (t *TabLayout) SetTabContent(idx int, w Widget)
func (t *TabLayout) SetActive(idx int)
func (t *TabLayout) ActiveIndex() int

// Uses primitives.TabItem
type TabItem struct {
    Label string
    ID    string
}
```

## Primitives

### ScrollableList

```go
func NewScrollableList(x, y, w, h int) *ScrollableList

// Types
type ListItem struct {
    Text  string
    Value interface{}  // Optional data payload
}

// Methods
func (s *ScrollableList) SetItems(items []ListItem)
func (s *ScrollableList) SetSelected(idx int)
func (s *ScrollableList) SelectedItem() *ListItem

// Properties
list.Style tcell.Style
list.SelectedStyle tcell.Style
list.Renderer ListItemRenderer  // func(p *Painter, rect Rect, item ListItem, selected bool)
```

### Grid

```go
func NewGrid(x, y, w, h int) *Grid

// Types
type GridItem struct {
    Text  string
    Value interface{}  // Optional data payload
}

// Methods
func (g *Grid) SetItems(items []GridItem)
func (g *Grid) SetSelected(idx int)
func (g *Grid) SelectedItem() *GridItem

// Properties
grid.MinCellWidth int
grid.MaxCols int
grid.Style tcell.Style
grid.SelectedStyle tcell.Style
grid.Renderer GridCellRenderer  // func(p *Painter, rect Rect, item GridItem, selected bool)
```

### TabBar

```go
func NewTabBar(x, y, w int, tabs []TabItem) *TabBar

// Types
type TabItem struct {
    Label string
    ID    string
}

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
func NewUIApp(title string, ui *core.UIManager) *UIApp

// Implements texel.App interface
func (a *UIApp) Run() error
func (a *UIApp) Stop()
func (a *UIApp) Resize(cols, rows int)
func (a *UIApp) Render() [][]texel.Cell
func (a *UIApp) HandleKey(ev *tcell.EventKey)
func (a *UIApp) HandleMouse(ev *tcell.EventMouse)
func (a *UIApp) GetTitle() string
func (a *UIApp) UI() *core.UIManager
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

- [Interfaces Reference](/texelui/api-reference/interfaces.md) - All interfaces
- [Widget Interface](/texelui/core-concepts/widget-interface.md) - Detailed explanation
- [Architecture](/texelui/core-concepts/architecture.md) - System overview

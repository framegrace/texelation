# Tutorials

Step-by-step guides for building real applications with TexelUI.

## Available Tutorials

| Tutorial | Description | Difficulty |
|----------|-------------|------------|
| [Building a Form](/texelui/tutorials/building-a-form.md) | Create a complete data entry form with validation | Beginner |
| [Creating a Custom Widget](/texelui/tutorials/custom-widget.md) | Build your own reusable widget | Intermediate |
| [Standalone vs TexelApp](/texelui/tutorials/standalone-vs-texelapp.md) | Understand the two runtime modes | Beginner |

## Learning Path

### For Beginners

1. Start with [Getting Started](/texelui/getting-started/README.md)
2. Follow [Building a Form](/texelui/tutorials/building-a-form.md)
3. Read [Standalone vs TexelApp](/texelui/tutorials/standalone-vs-texelapp.md)
4. Explore the [Widgets Reference](/texelui/widgets/README.md)

### For Intermediate Users

1. Learn [Core Concepts](/texelui/core-concepts/README.md)
2. Build a [Custom Widget](/texelui/tutorials/custom-widget.md)
3. Understand [Layout Systems](/texelui/layout/README.md)
4. Study the [API Reference](/texelui/api-reference/README.md)

### For Advanced Users

1. Read the [Architecture](/texelui/core-concepts/architecture.md) docs
2. Explore the source code in `texelui/`
3. Study existing widgets for patterns
4. Contribute to the project

## Quick Examples

### Simple Form
```go
ui := core.NewUIManager()
ui.SetLayout(layout.NewVBox(1))

ui.AddWidget(widgets.NewLabel(0, 0, 20, 1, "Name:"))
ui.AddWidget(widgets.NewInput(0, 0, 30))
ui.AddWidget(widgets.NewButton(0, 0, 0, 0, "Submit"))
```

### Tabbed Interface
```go
tabs := []primitives.TabItem{
    {Label: "Home", ID: "home"},
    {Label: "Settings", ID: "settings"},
}
tabLayout := widgets.NewTabLayout(0, 0, 80, 24, tabs)
tabLayout.SetTabContent(0, homePane)
tabLayout.SetTabContent(1, settingsPane)
```

### Custom Styling
```go
btn := widgets.NewButton(0, 0, 0, 0, "Custom")
btn.Style = tcell.StyleDefault.
    Foreground(tcell.ColorWhite).
    Background(tcell.ColorBlue)
```

## What's Next?

Choose a tutorial that matches your current skill level, or jump to the [Core Concepts](/texelui/core-concepts/README.md) if you prefer to understand the fundamentals first.

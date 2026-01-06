# Hello World

The simplest possible TexelUI application, explained line by line.

## The Complete Program

```go
package main

import (
	"log"

	"texelation/internal/devshell"
	"texelation/texel"
	"texelation/texelui/adapter"
	"texelation/texelui/core"
	"texelation/texelui/widgets"
)

func main() {
	err := devshell.Run(createApp, nil)
	if err != nil {
		log.Fatal(err)
	}
}

func createApp(args []string) (texel.App, error) {
	// 1. Create the UI manager
	ui := core.NewUIManager()

	// 2. Create a vertical layout container
	vbox := widgets.NewVBox()
	vbox.Spacing = 1

	// 3. Create a simple label
	label := widgets.NewLabel("Hello, World!")

	// 4. Create a button
	btn := widgets.NewButton("Click Me!")
	btn.OnClick = func() {
		label.Text = "Button clicked!"
	}

	// 5. Add widgets to the container
	vbox.AddChild(label)
	vbox.AddChild(btn)

	// 6. Add container to the UI
	ui.AddWidget(vbox)

	// 7. Set initial focus
	ui.Focus(btn)

	// 8. Create and return the app with resize handling
	app := adapter.NewUIApp("Hello", ui)
	app.OnResize(func(w, h int) {
		vbox.SetPosition(5, 5)
		vbox.Resize(w-10, h-10)
	})
	return app, nil
}
```

## Line-by-Line Explanation

### Imports

```go
import (
	"log"

	"texelation/internal/devshell"           // Standalone runner
	"texelation/texel"                       // Core texel types
	"texelation/texelui/adapter"             // texel.App adapter
	"texelation/texelui/core"                // UIManager, Painter, etc.
	"texelation/texelui/widgets"             // Built-in widgets
)
```

**devshell** - Provides standalone execution without needing the Texelation server
**texel** - Defines the `texel.App` interface that all apps implement
**adapter** - Bridges TexelUI's `UIManager` to the `texel.App` interface
**core** - Core TexelUI types: `UIManager`, `Widget`, `Painter`, `Rect`
**widgets** - Pre-built widgets like `Label`, `Button`, `Input`, `VBox`, `HBox`

### The Main Function

```go
func main() {
	err := devshell.Run(createApp, nil)
	if err != nil {
		log.Fatal(err)
	}
}
```

`devshell.Run` handles:
1. Creating a terminal screen (via tcell)
2. Setting up mouse and paste support
3. Running the main event loop
4. Calling your app's `HandleKey`, `HandleMouse`, `Render` methods
5. Cleaning up when you press Ctrl+C

### Creating the UI Manager

```go
ui := core.NewUIManager()
```

The `UIManager` is the heart of TexelUI. It:
- Maintains a list of widgets
- Manages which widget has focus
- Routes keyboard and mouse events
- Orchestrates rendering

### Creating a Layout Container

```go
vbox := widgets.NewVBox()
vbox.Spacing = 1
```

`VBox` is a vertical layout container that stacks children from top to bottom.
- No position parameters needed - containers manage child positioning
- `Spacing` controls the gap between children

### Creating a Label

```go
label := widgets.NewLabel("Hello, World!")
```

Labels auto-size to fit their text. No position parameters needed when using layout containers.

To adjust later:
- `label.SetPosition(x, y)` - Manual positioning
- `label.Resize(w, h)` - Manual sizing
- `label.Align = widgets.AlignCenter` - Text alignment

### Creating a Button

```go
btn := widgets.NewButton("Click Me!")

btn.OnClick = func() {
	label.Text = "Button clicked!"
}
```

Buttons auto-size to fit their text with padding: `[ Text ]`

The `OnClick` callback is called when:
- User presses Enter or Space while button is focused
- User clicks the button with the mouse

### Adding Widgets to Containers

```go
vbox.AddChild(label)
vbox.AddChild(btn)
```

Layout containers manage child positioning automatically:
- `AddChild(w)` - Uses widget's natural size
- `AddChildWithSize(w, size)` - Fixed size in layout direction
- `AddFlexChild(w)` - Expands to fill remaining space

### Adding Container to UIManager

```go
ui.AddWidget(vbox)
```

When you add a widget, the UIManager:
1. Adds it to the widget list
2. Propagates the invalidation callback (for dirty region tracking)
3. Marks the whole screen for redraw

### Setting Focus

```go
ui.Focus(btn)
```

Focus determines which widget receives keyboard input. Only focusable widgets can receive focus.

By default:
- `Label` is NOT focusable
- `Button`, `Input`, `TextArea`, `Checkbox` ARE focusable
- `VBox`, `HBox` delegate focus to their children

### Creating the App

```go
app := adapter.NewUIApp("Hello", ui)
app.OnResize(func(w, h int) {
	vbox.SetPosition(5, 5)
	vbox.Resize(w-10, h-10)
})
return app, nil
```

`UIApp` adapts `UIManager` to the `texel.App` interface:

```
┌─────────────────────────────────────────┐
│              texel.App                  │
│  (Run, Stop, Resize, Render, HandleKey) │
└────────────────┬────────────────────────┘
                 │
         ┌───────▼───────┐
         │    UIApp      │
         │  (Adapter)    │
         └───────┬───────┘
                 │
         ┌───────▼───────┐
         │  UIManager    │
         │   (Widgets)   │
         └───────────────┘
```

The `OnResize` callback lets you adjust layout when the terminal size changes.

## Running the Example

```bash
# Save to cmd/hello-world/main.go
go build -o bin/hello-world ./cmd/hello-world
./bin/hello-world
```

You'll see:
```
     Hello, World!

     [ Click Me! ]
```

Press Enter or Space to click the button. Press Ctrl+C to exit.

## Variations

### Use HBox for Horizontal Layout

```go
// Create a horizontal button row
hbox := widgets.NewHBox()
hbox.Spacing = 2

okBtn := widgets.NewButton("OK")
cancelBtn := widgets.NewButton("Cancel")

hbox.AddChild(okBtn)
hbox.AddChild(cancelBtn)
```

### Add a Border Around Content

```go
// Borders wrap any content
border := widgets.NewBorder(0, 0, 40, 10, tcell.StyleDefault)
border.SetChild(vbox)

ui.AddWidget(border)
```

### Use Flex Children for Expansion

```go
vbox := widgets.NewVBox()

header := widgets.NewLabel("Header")
content := widgets.NewTextArea()
footer := widgets.NewLabel("Footer")

vbox.AddChild(header)            // Natural size (1 row)
vbox.AddFlexChild(content)       // Expands to fill remaining space
vbox.AddChild(footer)            // Natural size (1 row)
```

### Create a Scrollable Form

```go
import "texelation/texelui/scroll"

// Create scroll pane for long content
scrollPane := scroll.NewScrollPane()

// Create form content
form := widgets.NewVBox()
form.Spacing = 1
for i := 0; i < 50; i++ {
    form.AddChild(widgets.NewLabel(fmt.Sprintf("Field %d:", i)))
    form.AddChild(widgets.NewInput())
}

scrollPane.SetChild(form)
scrollPane.SetContentHeight(100) // Total content height

ui.AddWidget(scrollPane)
```

## What's Next?

- **[Quickstart](/texelui/getting-started/quickstart.md)** - Build a complete login form
- **[Building a Form Tutorial](/texelui/tutorials/building-a-form.md)** - Advanced form techniques
- **[Widget Reference](/texelui/widgets/README.md)** - All available widgets

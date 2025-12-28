# Hello World

The simplest possible TexelUI application, explained line by line.

## The Complete Program

```go
package main

import (
	"log"

	"github.com/gdamore/tcell/v2"
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

	// 2. Create a simple label
	label := widgets.NewLabel(5, 5, 20, 1, "Hello, World!")

	// 3. Create a button
	btn := widgets.NewButton(5, 7, 0, 0, "Click Me!")
	btn.OnClick = func() {
		label.Text = "Button clicked!"
	}

	// 4. Add widgets to the UI
	ui.AddWidget(label)
	ui.AddWidget(btn)

	// 5. Set initial focus
	ui.Focus(btn)

	// 6. Create and return the app
	return adapter.NewUIApp("Hello", ui), nil
}
```

## Line-by-Line Explanation

### Imports

```go
import (
	"log"

	"github.com/gdamore/tcell/v2"           // Terminal cell library
	"texelation/internal/devshell"           // Standalone runner
	"texelation/texel"                       // Core texel types
	"texelation/texelui/adapter"             // texel.App adapter
	"texelation/texelui/core"                // UIManager, Painter, etc.
	"texelation/texelui/widgets"             // Built-in widgets
)
```

**tcell/v2** - The underlying terminal library that handles screen drawing and input
**devshell** - Provides standalone execution without needing the Texelation server
**texel** - Defines the `texel.App` interface that all apps implement
**adapter** - Bridges TexelUI's `UIManager` to the `texel.App` interface
**core** - Core TexelUI types: `UIManager`, `Widget`, `Painter`, `Rect`
**widgets** - Pre-built widgets like `Label`, `Button`, `Input`

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

### Creating a Label

```go
label := widgets.NewLabel(5, 5, 20, 1, "Hello, World!")
//                        ^  ^  ^^  ^  ^^^^^^^^^^^^^^^
//                        x  y  w   h  text
```

Parameters:
- **x=5**: Position 5 cells from the left
- **y=5**: Position 5 cells from the top
- **w=20**: Width of 20 cells
- **h=1**: Height of 1 cell (single line)
- **text**: The text to display

### Creating a Button

```go
btn := widgets.NewButton(5, 7, 0, 0, "Click Me!")
//                       ^  ^  ^  ^  ^^^^^^^^^^
//                       x  y  w  h  text

btn.OnClick = func() {
	label.Text = "Button clicked!"
}
```

When width or height is **0**, the button auto-sizes to fit its text.

The `OnClick` callback is called when:
- User presses Enter or Space while button is focused
- User clicks the button with the mouse

### Adding Widgets

```go
ui.AddWidget(label)
ui.AddWidget(btn)
```

Widgets are added in z-order: later widgets are drawn on top.

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

### Creating the App

```go
return adapter.NewUIApp("Hello", ui), nil
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

### Add a Background

```go
// Create a pane first
bg := widgets.NewPane(0, 0, 40, 12, tcell.StyleDefault)

// Add widgets as children
bg.AddChild(label)
bg.AddChild(btn)

// Add only the pane to UI
ui.AddWidget(bg)
```

### Add a Border

```go
// Create a bordered container
border := widgets.NewBorder(3, 3, 34, 8, tcell.StyleDefault)

// Create content pane inside
content := widgets.NewPane(0, 0, 32, 6, tcell.StyleDefault)
content.AddChild(label)
content.AddChild(btn)

// Set pane as border's child
border.SetChild(content)

ui.AddWidget(border)
```

### Handle Resize

```go
app := adapter.NewUIApp("Hello", ui)
app.OnResize(func(w, h int) {
	// Center the content
	label.SetPosition(w/2 - 10, h/2 - 1)
	btn.SetPosition(w/2 - 6, h/2 + 1)
})
return app, nil
```

## What's Next?

- **[Quickstart](quickstart.md)** - Build a complete login form
- **[Building a Form Tutorial](../tutorials/building-a-form.md)** - Advanced form techniques
- **[Widget Reference](../widgets/README.md)** - All available widgets

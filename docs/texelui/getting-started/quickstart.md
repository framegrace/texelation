# Quickstart: Your First TexelUI App

This guide will walk you through creating a complete TexelUI application in under 5 minutes.

## What We're Building

A simple login form with:
- Username input field
- Password input field
- Login button
- Status message

```
┌────────────────────────────────────────┐
│            Login Form                   │
├────────────────────────────────────────┤
│                                        │
│   Username:  [________________]        │
│                                        │
│   Password:  [________________]        │
│                                        │
│          [ Login ]                     │
│                                        │
│   Status: Ready                        │
│                                        │
└────────────────────────────────────────┘
```

## Prerequisites

Make sure you've completed the [Installation](/texelui/getting-started/installation.md) steps.

## Step 1: Create the Project Structure

Create a new file for our app:

```bash
mkdir -p cmd/my-login-app
touch cmd/my-login-app/main.go
```

## Step 2: Write the Application

Copy this complete, runnable code into `cmd/my-login-app/main.go`:

```go
package main

import (
	"fmt"
	"log"

	"github.com/gdamore/tcell/v2"
	"texelation/internal/devshell"
	"texelation/texel"
	"texelation/texelui/adapter"
	"texelation/texelui/core"
	"texelation/texelui/widgets"
)

func main() {
	// Register our app with the devshell runner
	err := devshell.Run(func(args []string) (texel.App, error) {
		return NewLoginApp(), nil
	}, nil)

	if err != nil {
		log.Fatalf("Error: %v", err)
	}
}

// NewLoginApp creates the login form application
func NewLoginApp() texel.App {
	// Create the UIManager - the root of our widget tree
	ui := core.NewUIManager()

	// Create a background pane
	bg := widgets.NewPane(0, 0, 50, 15, tcell.StyleDefault)

	// Title label (centered)
	title := widgets.NewLabel(0, 1, 50, 1, "Login Form")
	title.Align = widgets.AlignCenter

	// Username field
	usernameLabel := widgets.NewLabel(3, 4, 12, 1, "Username:")
	usernameInput := widgets.NewInput(15, 4, 25)
	usernameInput.Placeholder = "Enter username"

	// Password field
	passwordLabel := widgets.NewLabel(3, 6, 12, 1, "Password:")
	passwordInput := widgets.NewInput(15, 6, 25)
	passwordInput.Placeholder = "Enter password"

	// Status label
	statusLabel := widgets.NewLabel(3, 10, 44, 1, "Status: Ready")

	// Login button
	loginButton := widgets.NewButton(17, 8, 0, 0, "Login")
	loginButton.OnClick = func() {
		username := usernameInput.Text
		password := passwordInput.Text

		if username == "" || password == "" {
			statusLabel.Text = "Status: Please fill all fields"
		} else {
			statusLabel.Text = fmt.Sprintf("Status: Welcome, %s!", username)
		}
	}

	// Add all widgets to the UI (order matters for z-index)
	bg.AddChild(title)
	bg.AddChild(usernameLabel)
	bg.AddChild(usernameInput)
	bg.AddChild(passwordLabel)
	bg.AddChild(passwordInput)
	bg.AddChild(loginButton)
	bg.AddChild(statusLabel)

	ui.AddWidget(bg)

	// Set initial focus to the username field
	ui.Focus(usernameInput)

	// Create the app adapter
	app := adapter.NewUIApp("Login", ui)

	// Handle window resize
	app.OnResize(func(w, h int) {
		// Center the form in the available space
		formWidth := 50
		formHeight := 15
		x := (w - formWidth) / 2
		y := (h - formHeight) / 2
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		bg.SetPosition(x, y)
	})

	return app
}
```

## Step 3: Build and Run

```bash
# Build the app
go build -o bin/my-login-app ./cmd/my-login-app

# Run it
./bin/my-login-app
```

## Step 4: Interact with the Form

- **Tab** - Move to the next field
- **Shift+Tab** - Move to the previous field
- **Type** - Enter text in input fields
- **Enter/Space** - Click the focused button
- **Ctrl+C** - Exit the application

## Understanding the Code

Let's break down the key parts:

### 1. UIManager - The Root

```go
ui := core.NewUIManager()
```

The `UIManager` is the root of your widget tree. It handles:
- Focus management (which widget is active)
- Event routing (keyboard and mouse)
- Rendering (composing widgets to a buffer)

### 2. Creating Widgets

```go
// Labels for static text
title := widgets.NewLabel(x, y, width, height, "text")
title.Align = widgets.AlignCenter

// Input fields for user input
usernameInput := widgets.NewInput(x, y, width)
usernameInput.Placeholder = "hint text"

// Buttons for actions
loginButton := widgets.NewButton(x, y, width, height, "label")
loginButton.OnClick = func() {
    // Handle click
}
```

### 3. Container Hierarchy

```go
// Create a container
bg := widgets.NewPane(...)

// Add children (they inherit the container's coordinate space)
bg.AddChild(title)
bg.AddChild(usernameInput)

// Add container to UIManager
ui.AddWidget(bg)
```

### 4. The Adapter Pattern

```go
// UIApp adapts UIManager to the texel.App interface
app := adapter.NewUIApp("Title", ui)

// OnResize lets you handle window size changes
app.OnResize(func(w, h int) {
    // Reposition widgets for new size
})
```

### 5. Running with devshell

```go
devshell.Run(func(args []string) (texel.App, error) {
    return NewLoginApp(), nil
}, nil)
```

The `devshell.Run` function:
- Creates a terminal screen
- Handles the event loop
- Routes events to your app
- Cleans up on exit

## Common Modifications

### Add More Fields

```go
// Add an email field
emailLabel := widgets.NewLabel(3, 8, 12, 1, "Email:")
emailInput := widgets.NewInput(15, 8, 25)
emailInput.Placeholder = "user@example.com"
bg.AddChild(emailLabel)
bg.AddChild(emailInput)
```

### Add a Checkbox

```go
rememberMe := widgets.NewCheckbox(15, 10, "Remember me")
rememberMe.OnChange = func(checked bool) {
    if checked {
        statusLabel.Text = "Will remember you"
    }
}
bg.AddChild(rememberMe)
```

### Use VBox Layout

```go
import "texelation/texelui/layout"

ui := core.NewUIManager()
ui.SetLayout(layout.NewVBox(1))  // 1 cell spacing

// Now widgets stack vertically automatically
ui.AddWidget(title)
ui.AddWidget(usernameInput)
ui.AddWidget(passwordInput)
ui.AddWidget(loginButton)
```

## Next Steps

- **[Hello World](/texelui/getting-started/hello-world.md)** - Minimal example with detailed explanation
- **[Building a Form Tutorial](/texelui/tutorials/building-a-form.md)** - More advanced form building
- **[Widgets Reference](/texelui/widgets/README.md)** - All available widgets
- **[Layout Guide](/texelui/layout/README.md)** - Automatic widget positioning

## Troubleshooting

**Input fields don't respond to typing**
- Make sure you called `ui.Focus(widget)` to set initial focus
- Check that the widget is focusable (inputs are by default)

**Button doesn't trigger**
- Ensure `OnClick` is set before running the app
- Try pressing Enter or Space when the button is focused

**Layout looks wrong**
- Check your x, y, width, height values
- Remember that coordinates are in terminal cells, not pixels
- Use `OnResize` to handle different window sizes

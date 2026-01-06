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

	// Create main vertical layout
	form := widgets.NewVBox()
	form.Spacing = 1

	// Title label (centered)
	title := widgets.NewLabel("Login Form")
	title.Align = widgets.AlignCenter
	form.AddChild(title)

	// Username row
	usernameRow := widgets.NewHBox()
	usernameRow.Spacing = 1
	usernameLabel := widgets.NewLabel("Username:")
	usernameInput := widgets.NewInput()
	usernameInput.Placeholder = "Enter username"
	usernameRow.AddChildWithSize(usernameLabel, 12)
	usernameRow.AddFlexChild(usernameInput)
	form.AddChild(usernameRow)

	// Password row
	passwordRow := widgets.NewHBox()
	passwordRow.Spacing = 1
	passwordLabel := widgets.NewLabel("Password:")
	passwordInput := widgets.NewInput()
	passwordInput.Placeholder = "Enter password"
	passwordRow.AddChildWithSize(passwordLabel, 12)
	passwordRow.AddFlexChild(passwordInput)
	form.AddChild(passwordRow)

	// Status label
	statusLabel := widgets.NewLabel("Status: Ready")

	// Login button
	loginButton := widgets.NewButton("Login")
	loginButton.OnClick = func() {
		username := usernameInput.Text
		password := passwordInput.Text

		if username == "" || password == "" {
			statusLabel.Text = "Status: Please fill all fields"
		} else {
			statusLabel.Text = fmt.Sprintf("Status: Welcome, %s!", username)
		}
	}
	form.AddChild(loginButton)
	form.AddChild(statusLabel)

	// Wrap in a border for visual structure
	border := widgets.NewBorder(0, 0, 50, 12, tcell.StyleDefault)
	border.SetChild(form)

	ui.AddWidget(border)

	// Set initial focus to the username field
	ui.Focus(usernameInput)

	// Create the app adapter
	app := adapter.NewUIApp("Login", ui)

	// Handle window resize - center the form
	app.OnResize(func(w, h int) {
		formWidth := 50
		formHeight := 12
		x := (w - formWidth) / 2
		y := (h - formHeight) / 2
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		border.SetPosition(x, y)
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

### 2. Layout Containers

```go
// VBox stacks children vertically
form := widgets.NewVBox()
form.Spacing = 1  // Gap between children

// HBox arranges children horizontally
row := widgets.NewHBox()
row.Spacing = 1
```

Layout containers automatically position their children. No coordinates needed!

### 3. Creating Widgets

```go
// Labels for static text (auto-sized)
title := widgets.NewLabel("text")
title.Align = widgets.AlignCenter

// Input fields for user input (default 20x1)
usernameInput := widgets.NewInput()
usernameInput.Placeholder = "hint text"

// Buttons for actions (auto-sized with padding)
loginButton := widgets.NewButton("label")
loginButton.OnClick = func() {
    // Handle click
}
```

### 4. Adding Children to Containers

```go
// Natural size - uses widget's preferred size
form.AddChild(title)

// Fixed size in layout direction
row.AddChildWithSize(label, 12)  // 12 cells wide in HBox

// Flex - expands to fill remaining space
row.AddFlexChild(input)
```

### 5. The Adapter Pattern

```go
// UIApp adapts UIManager to the texel.App interface
app := adapter.NewUIApp("Title", ui)

// OnResize lets you handle window size changes
app.OnResize(func(w, h int) {
    // Resize root container to fill window
    border.SetPosition(x, y)
    border.Resize(w, h)
})
```

### 6. Running with devshell

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
// Add an email row
emailRow := widgets.NewHBox()
emailRow.Spacing = 1
emailLabel := widgets.NewLabel("Email:")
emailInput := widgets.NewInput()
emailInput.Placeholder = "user@example.com"
emailRow.AddChildWithSize(emailLabel, 12)
emailRow.AddFlexChild(emailInput)
form.AddChild(emailRow)
```

### Add a Checkbox

```go
rememberMe := widgets.NewCheckbox("Remember me")
rememberMe.OnChange = func(checked bool) {
    if checked {
        statusLabel.Text = "Will remember you"
    }
}
form.AddChild(rememberMe)
```

### Create a Button Row

```go
// Horizontal button row
buttonRow := widgets.NewHBox()
buttonRow.Spacing = 2
buttonRow.Align = widgets.BoxAlignCenter

loginBtn := widgets.NewButton("Login")
cancelBtn := widgets.NewButton("Cancel")

buttonRow.AddChild(loginBtn)
buttonRow.AddChild(cancelBtn)
form.AddChild(buttonRow)
```

### Add a ComboBox

```go
// Country selector
countryRow := widgets.NewHBox()
countryRow.Spacing = 1
countryLabel := widgets.NewLabel("Country:")
countries := []string{"USA", "Canada", "UK", "Germany", "France"}
countryCombo := widgets.NewComboBox(countries, true) // editable=true
countryRow.AddChildWithSize(countryLabel, 12)
countryRow.AddFlexChild(countryCombo)
form.AddChild(countryRow)
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

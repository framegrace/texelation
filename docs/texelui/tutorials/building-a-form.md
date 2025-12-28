# Tutorial: Building a Form

This tutorial walks through building a complete, production-quality data entry form with TexelUI.

## What We'll Build

A user registration form with:
- Personal information fields
- Dropdown selections
- Multi-line notes area
- Validation feedback
- Submit/Cancel actions

```
┌────────────────────────────────────────────────────────────────┐
│                    User Registration                            │
├────────────────────────────────────────────────────────────────┤
│                                                                 │
│   Full Name:     [John Doe_________________________]           │
│                                                                 │
│   Email:         [john@example.com__________________]          │
│                                                                 │
│   Country:       [United States                  ▼]            │
│                                                                 │
│   Bio:           ┌────────────────────────────────┐            │
│                  │ Software developer with 10     │            │
│                  │ years of experience...         │            │
│                  └────────────────────────────────┘            │
│                                                                 │
│   [X] Subscribe to newsletter                                  │
│   [ ] Accept terms and conditions                              │
│                                                                 │
│              [ Submit ]    [ Cancel ]                          │
│                                                                 │
│   ✓ Form is valid                                              │
│                                                                 │
└────────────────────────────────────────────────────────────────┘
```

## Prerequisites

- Completed the [Getting Started](/texelui/getting-started/README.md) guide
- Basic understanding of Go
- TexelUI built and working

## Step 1: Project Setup

Create the file structure:

```bash
mkdir -p cmd/registration-form
touch cmd/registration-form/main.go
```

## Step 2: Basic Structure

Start with the main structure:

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
	err := devshell.Run(func(args []string) (texel.App, error) {
		return NewRegistrationForm(), nil
	}, nil)
	if err != nil {
		log.Fatal(err)
	}
}

func NewRegistrationForm() texel.App {
	ui := core.NewUIManager()

	// We'll add widgets here

	return adapter.NewUIApp("Registration", ui)
}
```

## Step 3: Create the Form Container

Add a bordered container for the form:

```go
func NewRegistrationForm() texel.App {
	ui := core.NewUIManager()

	// Main form dimensions
	formWidth := 70
	formHeight := 22

	// Create background pane
	bg := widgets.NewPane(0, 0, formWidth, formHeight, tcell.StyleDefault)

	// Create border for visual structure
	border := widgets.NewBorder(0, 0, formWidth, formHeight, tcell.StyleDefault)

	// Title
	title := widgets.NewLabel(0, 1, formWidth, 1, "User Registration")
	title.Align = widgets.AlignCenter

	// Add to container
	bg.AddChild(title)

	// Set border's child to the background pane
	border.SetChild(bg)
	ui.AddWidget(border)

	// Create adapter with resize handling
	app := adapter.NewUIApp("Registration", ui)
	app.OnResize(func(w, h int) {
		// Center the form
		x := (w - formWidth) / 2
		y := (h - formHeight) / 2
		if x < 0 { x = 0 }
		if y < 0 { y = 0 }
		border.SetPosition(x, y)
	})

	return app
}
```

## Step 4: Add Input Fields

Add the text input fields:

```go
// Inside NewRegistrationForm(), after the title:

// Full Name field
nameLabel := widgets.NewLabel(3, 3, 12, 1, "Full Name:")
nameInput := widgets.NewInput(16, 3, 45)
nameInput.Placeholder = "Enter your full name"
bg.AddChild(nameLabel)
bg.AddChild(nameInput)

// Email field
emailLabel := widgets.NewLabel(3, 5, 12, 1, "Email:")
emailInput := widgets.NewInput(16, 5, 45)
emailInput.Placeholder = "user@example.com"
bg.AddChild(emailLabel)
bg.AddChild(emailInput)
```

## Step 5: Add a ComboBox

Add a country selector:

```go
// Country dropdown
countryLabel := widgets.NewLabel(3, 7, 12, 1, "Country:")
countries := []string{
	"Australia", "Brazil", "Canada", "France", "Germany",
	"India", "Japan", "Mexico", "Spain", "United Kingdom",
	"United States",
}
countryCombo := widgets.NewComboBox(16, 7, 35, countries, true) // editable=true
countryCombo.Placeholder = "Type to search..."
bg.AddChild(countryLabel)
bg.AddChild(countryCombo)
```

## Step 6: Add a TextArea

Add a multi-line bio field:

```go
// Bio field with border
bioLabel := widgets.NewLabel(3, 9, 12, 1, "Bio:")
bioBorder := widgets.NewBorder(16, 9, 47, 5, tcell.StyleDefault)
bioArea := widgets.NewTextArea(0, 0, 45, 3)
bioBorder.SetChild(bioArea)
bg.AddChild(bioLabel)
bg.AddChild(bioBorder)
```

## Step 7: Add Checkboxes

Add the subscription options:

```go
// Checkboxes
subscribeCheck := widgets.NewCheckbox(16, 15, "Subscribe to newsletter")
subscribeCheck.Checked = true // Default checked

termsCheck := widgets.NewCheckbox(16, 16, "Accept terms and conditions")

bg.AddChild(subscribeCheck)
bg.AddChild(termsCheck)
```

## Step 8: Add Buttons

Add the form actions:

```go
// Status label (for validation feedback)
statusLabel := widgets.NewLabel(3, 19, 64, 1, "")

// Submit button
submitBtn := widgets.NewButton(20, 18, 12, 1, "Submit")
submitBtn.OnClick = func() {
	// Validate form
	if nameInput.Text == "" {
		statusLabel.Text = "✗ Please enter your name"
		return
	}
	if emailInput.Text == "" || !isValidEmail(emailInput.Text) {
		statusLabel.Text = "✗ Please enter a valid email"
		return
	}
	if !termsCheck.Checked {
		statusLabel.Text = "✗ You must accept the terms"
		return
	}

	statusLabel.Text = "✓ Registration successful!"
}

// Cancel button
cancelBtn := widgets.NewButton(36, 18, 12, 1, "Cancel")
cancelBtn.OnClick = func() {
	// Clear the form
	nameInput.Text = ""
	emailInput.Text = ""
	countryCombo.SetValue("")
	bioArea.SetText("")
	subscribeCheck.Checked = true
	termsCheck.Checked = false
	statusLabel.Text = "Form cleared"
}

bg.AddChild(submitBtn)
bg.AddChild(cancelBtn)
bg.AddChild(statusLabel)
```

## Step 9: Add Validation Helper

Add a simple email validation function:

```go
import (
	"strings"
	// ... other imports
)

func isValidEmail(email string) bool {
	// Simple validation - contains @ and .
	return strings.Contains(email, "@") && strings.Contains(email, ".")
}
```

## Step 10: Set Focus Order

Set the initial focus:

```go
// At the end of NewRegistrationForm(), before creating the app:
ui.Focus(nameInput)
```

The default Tab navigation will move through focusable widgets in the order they were added.

## Complete Code

Here's the complete, runnable code:

```go
package main

import (
	"log"
	"strings"

	"github.com/gdamore/tcell/v2"
	"texelation/internal/devshell"
	"texelation/texel"
	"texelation/texelui/adapter"
	"texelation/texelui/core"
	"texelation/texelui/widgets"
)

func main() {
	err := devshell.Run(func(args []string) (texel.App, error) {
		return NewRegistrationForm(), nil
	}, nil)
	if err != nil {
		log.Fatal(err)
	}
}

func NewRegistrationForm() texel.App {
	ui := core.NewUIManager()

	formWidth := 70
	formHeight := 22

	// Background pane
	bg := widgets.NewPane(0, 0, formWidth-2, formHeight-2, tcell.StyleDefault)

	// Title
	title := widgets.NewLabel(0, 0, formWidth-2, 1, "User Registration")
	title.Align = widgets.AlignCenter
	bg.AddChild(title)

	// Full Name
	nameLabel := widgets.NewLabel(2, 2, 12, 1, "Full Name:")
	nameInput := widgets.NewInput(15, 2, 45)
	nameInput.Placeholder = "Enter your full name"
	bg.AddChild(nameLabel)
	bg.AddChild(nameInput)

	// Email
	emailLabel := widgets.NewLabel(2, 4, 12, 1, "Email:")
	emailInput := widgets.NewInput(15, 4, 45)
	emailInput.Placeholder = "user@example.com"
	bg.AddChild(emailLabel)
	bg.AddChild(emailInput)

	// Country
	countryLabel := widgets.NewLabel(2, 6, 12, 1, "Country:")
	countries := []string{
		"Australia", "Brazil", "Canada", "France", "Germany",
		"India", "Japan", "Mexico", "Spain", "United Kingdom",
		"United States",
	}
	countryCombo := widgets.NewComboBox(15, 6, 35, countries, true)
	countryCombo.Placeholder = "Type to search..."
	bg.AddChild(countryLabel)
	bg.AddChild(countryCombo)

	// Bio
	bioLabel := widgets.NewLabel(2, 8, 12, 1, "Bio:")
	bioBorder := widgets.NewBorder(15, 8, 47, 5, tcell.StyleDefault)
	bioArea := widgets.NewTextArea(0, 0, 45, 3)
	bioBorder.SetChild(bioArea)
	bg.AddChild(bioLabel)
	bg.AddChild(bioBorder)

	// Checkboxes
	subscribeCheck := widgets.NewCheckbox(15, 14, "Subscribe to newsletter")
	subscribeCheck.Checked = true
	termsCheck := widgets.NewCheckbox(15, 15, "Accept terms and conditions")
	bg.AddChild(subscribeCheck)
	bg.AddChild(termsCheck)

	// Status label
	statusLabel := widgets.NewLabel(2, 18, 64, 1, "")

	// Submit button
	submitBtn := widgets.NewButton(18, 17, 12, 1, "Submit")
	submitBtn.OnClick = func() {
		if nameInput.Text == "" {
			statusLabel.Text = "✗ Please enter your name"
			return
		}
		if emailInput.Text == "" || !isValidEmail(emailInput.Text) {
			statusLabel.Text = "✗ Please enter a valid email"
			return
		}
		if !termsCheck.Checked {
			statusLabel.Text = "✗ You must accept the terms"
			return
		}
		statusLabel.Text = "✓ Registration successful!"
	}

	// Cancel button
	cancelBtn := widgets.NewButton(34, 17, 12, 1, "Cancel")
	cancelBtn.OnClick = func() {
		nameInput.Text = ""
		emailInput.Text = ""
		countryCombo.SetValue("")
		bioArea.SetText("")
		subscribeCheck.Checked = true
		termsCheck.Checked = false
		statusLabel.Text = "Form cleared"
	}

	bg.AddChild(submitBtn)
	bg.AddChild(cancelBtn)
	bg.AddChild(statusLabel)

	// Create border around the whole form
	border := widgets.NewBorder(0, 0, formWidth, formHeight, tcell.StyleDefault)
	border.SetChild(bg)
	ui.AddWidget(border)

	// Set initial focus
	ui.Focus(nameInput)

	// Create app with resize handling
	app := adapter.NewUIApp("Registration", ui)
	app.OnResize(func(w, h int) {
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

func isValidEmail(email string) bool {
	return strings.Contains(email, "@") && strings.Contains(email, ".")
}
```

## Running the Form

```bash
go build -o bin/registration-form ./cmd/registration-form
./bin/registration-form
```

## Form Interaction

| Key | Action |
|-----|--------|
| Tab | Next field |
| Shift+Tab | Previous field |
| Enter | Activate button (when focused) |
| Space | Toggle checkbox / Activate button |
| Up/Down | Navigate dropdown |
| Type | Enter text / Filter dropdown |
| Ctrl+C | Exit |

## Enhancements to Try

### 1. Real-time Validation

```go
nameInput.OnChange = func(text string) {
	if len(text) < 2 {
		statusLabel.Text = "Name must be at least 2 characters"
	} else {
		statusLabel.Text = ""
	}
}
```

### 2. Field Highlighting on Error

```go
// Custom error style
errorStyle := tcell.StyleDefault.Background(tcell.ColorRed).Foreground(tcell.ColorWhite)

if nameInput.Text == "" {
	nameInput.Style = errorStyle
	statusLabel.Text = "✗ Please enter your name"
}
```

### 3. Loading State

```go
submitBtn.OnClick = func() {
	submitBtn.Text = "Loading..."
	// Simulate async operation
	go func() {
		time.Sleep(time.Second)
		submitBtn.Text = "Submit"
		statusLabel.Text = "✓ Done!"
	}()
}
```

## What's Next?

- [Creating a Custom Widget](/texelui/tutorials/custom-widget.md) - Build your own widgets
- [Standalone vs TexelApp](/texelui/tutorials/standalone-vs-texelapp.md) - Deploy your form
- [Widgets Reference](/texelui/widgets/README.md) - Explore all widget options

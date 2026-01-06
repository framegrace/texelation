# Tutorial: Building a Form

This tutorial walks through building a complete, production-quality data entry form with TexelUI using the Form widget.

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

	// We'll add the form here

	return adapter.NewUIApp("Registration", ui)
}
```

## Step 3: Create the Form Widget

The Form widget automatically handles label alignment and focus highlighting:

```go
func NewRegistrationForm() texel.App {
	ui := core.NewUIManager()

	// Create the Form widget
	form := widgets.NewForm()

	// We'll add fields here...

	// Wrap in border for visual structure
	border := widgets.NewBorder()
	border.SetChild(form)
	ui.AddWidget(border)

	// Create adapter with resize handling
	app := adapter.NewUIApp("Registration", ui)
	app.OnResize(func(w, h int) {
		formWidth := 70
		formHeight := 22
		x := (w - formWidth) / 2
		y := (h - formHeight) / 2
		if x < 0 { x = 0 }
		if y < 0 { y = 0 }
		border.SetPosition(x, y)
		border.Resize(formWidth, formHeight)
	})

	return app
}
```

## Step 4: Add Input Fields

Use `AddField()` for labeled input fields - the Form widget handles alignment automatically:

```go
// Full Name field
nameInput := widgets.NewInput()
nameInput.Placeholder = "Enter your full name"
form.AddField("Full Name:", nameInput)

// Email field
emailInput := widgets.NewInput()
emailInput.Placeholder = "user@example.com"
form.AddField("Email:", emailInput)
```

The Form widget:
- Aligns all labels to the same width
- Highlights the label when its field has focus
- Manages Tab/Shift+Tab navigation between fields

## Step 5: Add a ComboBox

Add a country selector the same way:

```go
countries := []string{
	"Australia", "Brazil", "Canada", "France", "Germany",
	"India", "Japan", "Mexico", "Spain", "United Kingdom",
	"United States",
}
countryCombo := widgets.NewComboBox(countries, true) // editable=true
countryCombo.Placeholder = "Type to search..."
form.AddField("Country:", countryCombo)
```

## Step 6: Add a TextArea

For multi-line fields, use `AddRow()` with a custom height:

```go
bioArea := widgets.NewTextArea()
form.AddRow(widgets.FormRow{
	Label:  widgets.NewLabel("Bio:"),
	Field:  bioArea,
	Height: 4, // Give it more height
})
```

## Step 7: Add Checkboxes

Use `AddFullWidthField()` for checkboxes that span the full width:

```go
form.AddSpacer(1) // Visual separation

subscribeCheck := widgets.NewCheckbox("Subscribe to newsletter")
subscribeCheck.Checked = true // Default checked
form.AddFullWidthField(subscribeCheck, 1)

termsCheck := widgets.NewCheckbox("Accept terms and conditions")
form.AddFullWidthField(termsCheck, 1)
```

## Step 8: Add Buttons

For buttons, use a full-width HBox row:

```go
// Status label (for validation feedback)
statusLabel := widgets.NewLabel("")
form.AddFullWidthField(statusLabel, 1)

// Button row
buttonRow := widgets.NewHBox()
buttonRow.Spacing = 2

submitBtn := widgets.NewButton("Submit")
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

cancelBtn := widgets.NewButton("Cancel")
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

buttonRow.AddChild(submitBtn)
buttonRow.AddChild(cancelBtn)
form.AddFullWidthField(buttonRow, 1)
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

## Step 10: Set Focus

Set the initial focus to the form (it will focus the first field):

```go
// At the end of NewRegistrationForm(), before creating the app:
ui.Focus(form)
```

The Form widget handles Tab/Shift+Tab navigation automatically.

## Complete Code

Here's the complete, runnable code using the Form widget:

```go
package main

import (
	"log"
	"strings"

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

	// Create Form widget
	form := widgets.NewForm()

	// Full Name
	nameInput := widgets.NewInput()
	nameInput.Placeholder = "Enter your full name"
	form.AddField("Full Name:", nameInput)

	// Email
	emailInput := widgets.NewInput()
	emailInput.Placeholder = "user@example.com"
	form.AddField("Email:", emailInput)

	// Country
	countries := []string{
		"Australia", "Brazil", "Canada", "France", "Germany",
		"India", "Japan", "Mexico", "Spain", "United Kingdom",
		"United States",
	}
	countryCombo := widgets.NewComboBox(countries, true)
	countryCombo.Placeholder = "Type to search..."
	form.AddField("Country:", countryCombo)

	// Bio (multi-line with custom height)
	bioArea := widgets.NewTextArea()
	form.AddRow(widgets.FormRow{
		Label:  widgets.NewLabel("Bio:"),
		Field:  bioArea,
		Height: 4,
	})

	// Spacer before checkboxes
	form.AddSpacer(1)

	// Checkboxes (full-width)
	subscribeCheck := widgets.NewCheckbox("Subscribe to newsletter")
	subscribeCheck.Checked = true
	form.AddFullWidthField(subscribeCheck, 1)

	termsCheck := widgets.NewCheckbox("Accept terms and conditions")
	form.AddFullWidthField(termsCheck, 1)

	// Status label
	statusLabel := widgets.NewLabel("")
	form.AddFullWidthField(statusLabel, 1)

	// Button row
	buttonRow := widgets.NewHBox()
	buttonRow.Spacing = 2

	submitBtn := widgets.NewButton("Submit")
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

	cancelBtn := widgets.NewButton("Cancel")
	cancelBtn.OnClick = func() {
		nameInput.Text = ""
		emailInput.Text = ""
		countryCombo.SetValue("")
		bioArea.SetText("")
		subscribeCheck.Checked = true
		termsCheck.Checked = false
		statusLabel.Text = "Form cleared"
	}

	buttonRow.AddChild(submitBtn)
	buttonRow.AddChild(cancelBtn)
	form.AddFullWidthField(buttonRow, 1)

	// Wrap in border
	border := widgets.NewBorder()
	border.SetChild(form)
	ui.AddWidget(border)

	// Set initial focus
	ui.Focus(form)

	// Create app with resize handling
	formWidth := 70
	formHeight := 20
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
		border.Resize(formWidth, formHeight)
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

## Form Widget Features

The Form widget provides several advantages over manual VBox/HBox layouts:

| Feature | Form Widget | Manual VBox/HBox |
|---------|-------------|------------------|
| Label alignment | Automatic | Manual (AddChildWithSize) |
| Label highlighting | Automatic on focus | Manual styling |
| Focus cycling | Built-in | Automatic via container |
| Multi-line fields | Height parameter | AddChildWithSize |
| Code simplicity | High | Moderate |

## Custom Form Configuration

You can customize the Form widget's appearance:

```go
config := widgets.FormConfig{
	PaddingX:   4,   // Horizontal padding
	PaddingY:   2,   // Vertical padding
	LabelWidth: 15,  // Narrower labels
	RowSpacing: 1,   // Space between rows
}
form := widgets.NewFormWithConfig(config)
```

## Alternative: Manual VBox/HBox Layout

For more control, you can build forms manually with VBox and HBox:

```go
// Main container
container := widgets.NewVBox()
container.Spacing = 1

// Create a form row manually
row := widgets.NewHBox()
row.Spacing = 1
row.AddChildWithSize(widgets.NewLabel("Name:"), 12)
input := widgets.NewInput()
row.AddFlexChild(input)
container.AddChild(row)
```

This approach gives you full control over layout but requires more code.

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

### 3. Scrollable Long Forms

For forms with many fields, wrap in a ScrollPane:

```go
import "texelation/texelui/scroll"

sp := scroll.NewScrollPane()
sp.SetChild(form)
sp.SetContentHeight(form.ContentHeight())

border.SetChild(sp)
```

## What's Next?

- [Form Widget Reference](/texelui/widgets/form.md) - Complete Form API
- [Creating a Custom Widget](/texelui/tutorials/custom-widget.md) - Build your own widgets
- [Standalone vs TexelApp](/texelui/tutorials/standalone-vs-texelapp.md) - Deploy your form
- [Widgets Reference](/texelui/widgets/README.md) - Explore all widget options

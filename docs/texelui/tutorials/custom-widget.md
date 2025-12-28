# Tutorial: Creating a Custom Widget

Learn how to build your own reusable widget from scratch.

## What We'll Build

A `ProgressBar` widget that displays a progress percentage:

```
Progress: [████████████░░░░░░░░░░░░] 50%
```

## The Widget Interface

Every widget must implement the `core.Widget` interface:

```go
type Widget interface {
    SetPosition(x, y int)
    Position() (int, int)
    Resize(w, h int)
    Size() (int, int)
    Draw(p *Painter)
    Focusable() bool
    Focus()
    Blur()
    HandleKey(ev *tcell.EventKey) bool
    HitTest(x, y int) bool
}
```

The good news: `core.BaseWidget` provides default implementations for most of these.

## Step 1: Define the Widget Structure

Create a new file `texelui/widgets/progressbar.go`:

```go
package widgets

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
)

// ProgressBar displays a visual progress indicator.
type ProgressBar struct {
	core.BaseWidget

	// Current progress value (0.0 to 1.0)
	Value float64

	// Display style
	Style         tcell.Style
	FilledStyle   tcell.Style
	UnfilledStyle tcell.Style

	// Characters for rendering
	FilledChar   rune
	UnfilledChar rune

	// Show percentage text
	ShowPercent bool

	// Optional label
	Label string

	// Invalidation callback
	inv func(core.Rect)
}
```

## Step 2: Create the Constructor

```go
// NewProgressBar creates a progress bar at the specified position.
// Width should be at least 10 for readable display.
func NewProgressBar(x, y, w int) *ProgressBar {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	accent := tm.GetSemanticColor("action.primary")

	p := &ProgressBar{
		Value:         0.0,
		Style:         tcell.StyleDefault.Foreground(fg).Background(bg),
		FilledStyle:   tcell.StyleDefault.Foreground(accent).Background(bg),
		UnfilledStyle: tcell.StyleDefault.Foreground(fg).Background(bg),
		FilledChar:    '█',
		UnfilledChar:  '░',
		ShowPercent:   true,
	}

	p.SetPosition(x, y)
	p.Resize(w, 1) // Height is always 1
	p.SetFocusable(false) // Progress bars are typically not focusable

	return p
}
```

## Step 3: Implement the Draw Method

This is where the magic happens:

```go
// Draw renders the progress bar.
func (p *ProgressBar) Draw(painter *core.Painter) {
	// Calculate available width for the bar
	barStart := p.Rect.X
	barWidth := p.Rect.W

	// If we have a label, draw it first
	if p.Label != "" {
		painter.DrawText(p.Rect.X, p.Rect.Y, p.Label+" ", p.Style)
		labelLen := len(p.Label) + 1
		barStart += labelLen
		barWidth -= labelLen
	}

	// Reserve space for percentage if shown
	percentText := ""
	if p.ShowPercent {
		percentText = fmt.Sprintf(" %3d%%", int(p.Value*100))
		barWidth -= len(percentText)
	}

	// Clamp value to 0.0-1.0
	value := p.Value
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}

	// Calculate filled portion
	filledWidth := int(float64(barWidth) * value)
	if filledWidth > barWidth {
		filledWidth = barWidth
	}

	// Draw the bar
	x := barStart
	for i := 0; i < barWidth; i++ {
		if i < filledWidth {
			painter.SetCell(x+i, p.Rect.Y, p.FilledChar, p.FilledStyle)
		} else {
			painter.SetCell(x+i, p.Rect.Y, p.UnfilledChar, p.UnfilledStyle)
		}
	}

	// Draw percentage
	if p.ShowPercent {
		painter.DrawText(barStart+barWidth, p.Rect.Y, percentText, p.Style)
	}
}
```

## Step 4: Implement Invalidation

To trigger redraws when the value changes:

```go
// SetInvalidator receives the dirty-region callback.
func (p *ProgressBar) SetInvalidator(fn func(core.Rect)) {
	p.inv = fn
}

// SetValue updates the progress and triggers a redraw.
func (p *ProgressBar) SetValue(v float64) {
	p.Value = v
	p.invalidate()
}

func (p *ProgressBar) invalidate() {
	if p.inv != nil {
		p.inv(p.Rect)
	}
}
```

## Step 5: Optional - Add Interactivity

If you want the progress bar to be clickable (e.g., to set value by clicking):

```go
// Make it focusable in the constructor:
// p.SetFocusable(true)

// HandleMouse allows clicking to set the value.
func (p *ProgressBar) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !p.HitTest(x, y) {
		return false
	}

	if ev.Buttons()&tcell.Button1 != 0 {
		// Calculate which part of the bar was clicked
		barStart := p.Rect.X
		barWidth := p.Rect.W

		if p.Label != "" {
			labelLen := len(p.Label) + 1
			barStart += labelLen
			barWidth -= labelLen
		}
		if p.ShowPercent {
			barWidth -= 5 // " 100%"
		}

		// Set value based on click position
		clickX := x - barStart
		if clickX >= 0 && barWidth > 0 {
			p.Value = float64(clickX) / float64(barWidth)
			if p.Value > 1 {
				p.Value = 1
			}
			p.invalidate()
		}
		return true
	}

	return false
}

// HandleKey allows keyboard control when focused.
func (p *ProgressBar) HandleKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyLeft:
		p.Value -= 0.1
		if p.Value < 0 {
			p.Value = 0
		}
		p.invalidate()
		return true
	case tcell.KeyRight:
		p.Value += 0.1
		if p.Value > 1 {
			p.Value = 1
		}
		p.invalidate()
		return true
	}
	return false
}
```

## Complete Widget Code

```go
package widgets

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"texelation/texel/theme"
	"texelation/texelui/core"
)

// ProgressBar displays a visual progress indicator.
type ProgressBar struct {
	core.BaseWidget

	Value         float64
	Style         tcell.Style
	FilledStyle   tcell.Style
	UnfilledStyle tcell.Style
	FilledChar    rune
	UnfilledChar  rune
	ShowPercent   bool
	Label         string
	inv           func(core.Rect)
}

// NewProgressBar creates a progress bar at the specified position.
func NewProgressBar(x, y, w int) *ProgressBar {
	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	accent := tm.GetSemanticColor("action.primary")

	p := &ProgressBar{
		Value:         0.0,
		Style:         tcell.StyleDefault.Foreground(fg).Background(bg),
		FilledStyle:   tcell.StyleDefault.Foreground(accent).Background(bg),
		UnfilledStyle: tcell.StyleDefault.Foreground(fg).Background(bg),
		FilledChar:    '█',
		UnfilledChar:  '░',
		ShowPercent:   true,
	}

	p.SetPosition(x, y)
	p.Resize(w, 1)
	p.SetFocusable(false)

	return p
}

// SetInvalidator receives the dirty-region callback.
func (p *ProgressBar) SetInvalidator(fn func(core.Rect)) {
	p.inv = fn
}

// SetValue updates the progress and triggers a redraw.
func (p *ProgressBar) SetValue(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	p.Value = v
	p.invalidate()
}

func (p *ProgressBar) invalidate() {
	if p.inv != nil {
		p.inv(p.Rect)
	}
}

// Draw renders the progress bar.
func (p *ProgressBar) Draw(painter *core.Painter) {
	barStart := p.Rect.X
	barWidth := p.Rect.W

	// Draw label
	if p.Label != "" {
		painter.DrawText(p.Rect.X, p.Rect.Y, p.Label+" ", p.Style)
		labelLen := len(p.Label) + 1
		barStart += labelLen
		barWidth -= labelLen
	}

	// Reserve percentage space
	percentText := ""
	if p.ShowPercent {
		percentText = fmt.Sprintf(" %3d%%", int(p.Value*100))
		barWidth -= len(percentText)
	}

	// Calculate filled width
	filledWidth := int(float64(barWidth) * p.Value)
	if filledWidth > barWidth {
		filledWidth = barWidth
	}

	// Draw bar
	x := barStart
	for i := 0; i < barWidth; i++ {
		if i < filledWidth {
			painter.SetCell(x+i, p.Rect.Y, p.FilledChar, p.FilledStyle)
		} else {
			painter.SetCell(x+i, p.Rect.Y, p.UnfilledChar, p.UnfilledStyle)
		}
	}

	// Draw percentage
	if p.ShowPercent {
		painter.DrawText(barStart+barWidth, p.Rect.Y, percentText, p.Style)
	}
}
```

## Using Your Widget

```go
func main() {
	err := devshell.Run(func(args []string) (texel.App, error) {
		ui := core.NewUIManager()

		// Create progress bar
		progress := widgets.NewProgressBar(5, 5, 40)
		progress.Label = "Loading:"
		progress.SetValue(0.5) // 50%

		ui.AddWidget(progress)

		// Create a button to update progress
		btn := widgets.NewButton(5, 8, 0, 0, "Increase")
		btn.OnClick = func() {
			progress.SetValue(progress.Value + 0.1)
		}
		ui.AddWidget(btn)
		ui.Focus(btn)

		return adapter.NewUIApp("Progress Demo", ui), nil
	}, nil)

	if err != nil {
		log.Fatal(err)
	}
}
```

## Best Practices for Custom Widgets

### 1. Use Theme Colors

```go
tm := theme.Get()
fg := tm.GetSemanticColor("text.primary")
bg := tm.GetSemanticColor("bg.surface")
```

Don't hardcode colors - use semantic theme colors for consistency.

### 2. Implement Invalidation

```go
func (w *MyWidget) SetInvalidator(fn func(core.Rect)) {
	w.inv = fn
}

func (w *MyWidget) invalidate() {
	if w.inv != nil {
		w.inv(w.Rect)
	}
}
```

Always call `invalidate()` when internal state changes that affects rendering.

### 3. Handle Edge Cases

```go
// Clamp values
if value < 0 { value = 0 }
if value > 1 { value = 1 }

// Handle zero-size
if w.Rect.W <= 0 || w.Rect.H <= 0 {
	return
}
```

### 4. Respect the Painter's Clip

The `Painter` automatically clips to its rect. Don't draw outside your widget's bounds.

### 5. Document Public API

```go
// SetValue updates the progress value.
// Value should be between 0.0 and 1.0.
// Values outside this range are clamped.
func (p *ProgressBar) SetValue(v float64)
```

### 6. Add Tests

```go
func TestProgressBar_SetValue(t *testing.T) {
	p := NewProgressBar(0, 0, 30)

	p.SetValue(0.5)
	if p.Value != 0.5 {
		t.Errorf("Expected 0.5, got %f", p.Value)
	}

	// Test clamping
	p.SetValue(1.5)
	if p.Value != 1.0 {
		t.Errorf("Expected 1.0 (clamped), got %f", p.Value)
	}
}
```

## Advanced: Optional Interfaces

Your widget can implement additional interfaces for enhanced functionality:

### MouseAware
```go
type MouseAware interface {
	HandleMouse(ev *tcell.EventMouse) bool
}
```

### ZIndexer
```go
type ZIndexer interface {
	ZIndex() int
}
```

### ChildContainer
```go
type ChildContainer interface {
	VisitChildren(func(Widget))
}
```

### Modal
```go
type Modal interface {
	IsModal() bool
	DismissModal()
}
```

## What's Next?

- [Widgets Reference](../widgets/README.md) - Study existing widget implementations
- [Core Concepts](../core-concepts/README.md) - Deep dive into architecture
- [API Reference](../api-reference/README.md) - Complete interface documentation

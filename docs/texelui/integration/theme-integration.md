# Theme Integration

Using Texelation's theme system with TexelUI.

## Overview

Texelation provides a sophisticated theming system based on Catppuccin palettes with semantic color mappings. TexelUI applications can integrate with this system for consistent styling.

```
┌─────────────────────────────────────────────────────────────────┐
│                    Texelation Theme System                       │
│                                                                  │
│  ┌─────────────────┐   ┌─────────────────┐   ┌───────────────┐ │
│  │    Palettes     │ → │    Semantics    │ → │  User Config  │ │
│  │   (Catppuccin)  │   │   (Purpose)     │   │  (Overrides)  │ │
│  └─────────────────┘   └─────────────────┘   └───────────────┘ │
│          │                     │                    │           │
│          └──────────┬──────────┴────────────────────┘           │
│                     ▼                                            │
│           ┌─────────────────┐                                    │
│           │  Resolved Style │ → Your Widget                      │
│           └─────────────────┘                                    │
└─────────────────────────────────────────────────────────────────┘
```

## Three-Layer Resolution

### 1. Palettes (Base Colors)

Catppuccin color palettes provide the foundation:

```go
// From texel/theme/palettes.go
type Palette struct {
    Rosewater tcell.Color
    Flamingo  tcell.Color
    Pink      tcell.Color
    Mauve     tcell.Color
    Red       tcell.Color
    Maroon    tcell.Color
    Peach     tcell.Color
    Yellow    tcell.Color
    Green     tcell.Color
    Teal      tcell.Color
    Sky       tcell.Color
    Sapphire  tcell.Color
    Blue      tcell.Color
    Lavender  tcell.Color
    Text      tcell.Color
    Subtext1  tcell.Color
    Subtext0  tcell.Color
    Overlay2  tcell.Color
    Overlay1  tcell.Color
    Overlay0  tcell.Color
    Surface2  tcell.Color
    Surface1  tcell.Color
    Surface0  tcell.Color
    Base      tcell.Color
    Mantle    tcell.Color
    Crust     tcell.Color
}
```

**Available palettes:**
- `Latte` - Light theme
- `Frappe` - Muted dark
- `Macchiato` - Warmer dark
- `Mocha` - Deep dark (default)

### 2. Semantics (Purpose Mapping)

Semantic colors map palette colors to UI purposes:

```go
// From texel/theme/semantics.go
type Semantics struct {
    // Text hierarchy
    TextPrimary   tcell.Color  // Main text
    TextSecondary tcell.Color  // Subdued text
    TextMuted     tcell.Color  // De-emphasized text

    // Backgrounds
    BgBase        tcell.Color  // Main background
    BgSurface     tcell.Color  // Elevated surfaces
    BgOverlay     tcell.Color  // Overlays/modals

    // Actions
    ActionPrimaryBg   tcell.Color  // Primary button bg
    ActionPrimaryFg   tcell.Color  // Primary button fg
    ActionSecondaryBg tcell.Color  // Secondary button bg
    ActionSecondaryFg tcell.Color  // Secondary button fg

    // Borders
    BorderNormal  tcell.Color  // Default borders
    BorderFocused tcell.Color  // Focused element borders

    // Status
    StatusSuccess tcell.Color
    StatusWarning tcell.Color
    StatusError   tcell.Color
    StatusInfo    tcell.Color

    // Selection
    SelectionBg   tcell.Color
    SelectionFg   tcell.Color
}
```

### 3. User Configuration

Users can override any color in `theme.json`:

```json
{
  "palette": "mocha",
  "overrides": {
    "action.primary.bg": "#ff5500",
    "text.primary": "#ffffff"
  }
}
```

## Accessing Theme in TexelApps

### ThemeSetter Interface

Implement to receive theme updates:

```go
type ThemeSetter interface {
    SetTheme(theme *theme.Theme)
}

type MyApp struct {
    ui    *core.UIManager
    theme *theme.Theme
}

func (a *MyApp) SetTheme(th *theme.Theme) {
    a.theme = th
    a.applyStyles()
}
```

### Applying Theme to Widgets

```go
func (a *MyApp) applyStyles() {
    th := a.theme
    if th == nil {
        return
    }

    // Text styles
    normalText := tcell.StyleDefault.
        Foreground(th.Semantics.TextPrimary).
        Background(th.Semantics.BgBase)

    mutedText := tcell.StyleDefault.
        Foreground(th.Semantics.TextMuted).
        Background(th.Semantics.BgBase)

    // Button styles
    primaryButton := tcell.StyleDefault.
        Foreground(th.Semantics.ActionPrimaryFg).
        Background(th.Semantics.ActionPrimaryBg)

    secondaryButton := tcell.StyleDefault.
        Foreground(th.Semantics.ActionSecondaryFg).
        Background(th.Semantics.ActionSecondaryBg)

    // Input styles
    inputStyle := tcell.StyleDefault.
        Foreground(th.Semantics.TextPrimary).
        Background(th.Semantics.BgSurface)

    inputFocused := tcell.StyleDefault.
        Foreground(th.Semantics.TextPrimary).
        Background(th.Semantics.BgSurface).
        Underline(true)

    // Apply to widgets
    a.title.Style = normalText.Bold(true)
    a.subtitle.Style = mutedText
    a.saveButton.Style = primaryButton
    a.cancelButton.Style = secondaryButton
    a.input.Style = inputStyle
    a.input.FocusStyle = inputFocused
}
```

## Semantic Color Reference

### Text Colors

| Semantic | Purpose | Typical Use |
|----------|---------|-------------|
| `TextPrimary` | Main text | Labels, content |
| `TextSecondary` | Supporting text | Descriptions, hints |
| `TextMuted` | De-emphasized | Placeholders, disabled |

### Background Colors

| Semantic | Purpose | Typical Use |
|----------|---------|-------------|
| `BgBase` | Main background | Window background |
| `BgSurface` | Elevated surface | Panels, cards |
| `BgOverlay` | Overlay background | Modals, dropdowns |

### Action Colors

| Semantic | Purpose | Typical Use |
|----------|---------|-------------|
| `ActionPrimaryBg/Fg` | Primary action | Submit buttons |
| `ActionSecondaryBg/Fg` | Secondary action | Cancel buttons |

### Status Colors

| Semantic | Purpose | Typical Use |
|----------|---------|-------------|
| `StatusSuccess` | Success | Confirmations |
| `StatusWarning` | Warning | Caution messages |
| `StatusError` | Error | Error messages |
| `StatusInfo` | Info | Informational |

## Standalone Theme Loading

Without Texelation, load theme manually:

```go
import "texelation/texel/theme"

func loadTheme() (*theme.Theme, error) {
    // From file
    data, err := os.ReadFile("theme.json")
    if err != nil {
        return theme.Default(), nil
    }
    return theme.LoadFromBytes(data)
}

func main() {
    th, _ := loadTheme()

    ui := core.NewUIManager()

    button := widgets.NewButton(0, 0, 10, 1, "Click")
    button.Style = tcell.StyleDefault.
        Foreground(th.Semantics.ActionPrimaryFg).
        Background(th.Semantics.ActionPrimaryBg)

    ui.AddWidget(button)
    devshell.Run(ui)
}
```

## Theme-Aware Widget Example

```go
package widgets

import (
    "github.com/gdamore/tcell/v2"
    "texelation/texel/theme"
    "texelation/texelui/core"
)

type ThemedButton struct {
    core.BaseWidget
    text    string
    theme   *theme.Theme
    primary bool  // true for primary, false for secondary
}

func NewThemedButton(x, y, w, h int, text string) *ThemedButton {
    btn := &ThemedButton{text: text, primary: true}
    btn.SetPosition(x, y)
    btn.Resize(w, h)
    return btn
}

func (b *ThemedButton) SetTheme(th *theme.Theme) {
    b.theme = th
}

func (b *ThemedButton) SetPrimary(primary bool) {
    b.primary = primary
}

func (b *ThemedButton) style() tcell.Style {
    if b.theme == nil {
        return tcell.StyleDefault.Reverse(true)
    }

    sem := b.theme.Semantics
    if b.primary {
        return tcell.StyleDefault.
            Foreground(sem.ActionPrimaryFg).
            Background(sem.ActionPrimaryBg)
    }
    return tcell.StyleDefault.
        Foreground(sem.ActionSecondaryFg).
        Background(sem.ActionSecondaryBg)
}

func (b *ThemedButton) Draw(p *core.Painter) {
    rect := b.Rect()
    style := b.style()

    // Draw button
    p.Fill(rect, ' ', style)

    // Center text
    x := rect.X + (rect.W-len(b.text))/2
    y := rect.Y + rect.H/2
    p.DrawText(x, y, b.text, style)
}
```

## Dynamic Theme Updates

Handle theme changes at runtime:

```go
type MyApp struct {
    ui      *core.UIManager
    theme   *theme.Theme
    widgets []*ThemedWidget
}

func (a *MyApp) SetTheme(th *theme.Theme) {
    a.theme = th

    // Propagate to all widgets
    for _, w := range a.widgets {
        if ts, ok := w.(ThemeSetter); ok {
            ts.SetTheme(th)
        }
    }

    // Request redraw
    if a.refresh != nil {
        a.refresh <- true
    }
}
```

## Theme Palette Access

Access the underlying palette for custom colors:

```go
func (a *MyApp) SetTheme(th *theme.Theme) {
    // Access palette directly
    palette := th.Palette

    // Use palette colors for custom styling
    highlightStyle := tcell.StyleDefault.
        Foreground(palette.Text).
        Background(palette.Mauve)

    accentStyle := tcell.StyleDefault.
        Foreground(palette.Base).
        Background(palette.Pink)
}
```

## Best Practices

### 1. Use Semantics Over Palette

```go
// Good - semantic meaning
style := tcell.StyleDefault.
    Foreground(th.Semantics.TextPrimary)

// Avoid - palette color directly
style := tcell.StyleDefault.
    Foreground(th.Palette.Text)
```

### 2. Provide Fallbacks

```go
func (w *Widget) getStyle() tcell.Style {
    if w.theme == nil {
        // Sensible default
        return tcell.StyleDefault.Reverse(true)
    }
    return tcell.StyleDefault.
        Foreground(w.theme.Semantics.ActionPrimaryFg).
        Background(w.theme.Semantics.ActionPrimaryBg)
}
```

### 3. Cache Styles

```go
type MyWidget struct {
    theme       *theme.Theme
    cachedStyle tcell.Style
}

func (w *MyWidget) SetTheme(th *theme.Theme) {
    w.theme = th
    w.cachedStyle = w.computeStyle()
}

func (w *MyWidget) Draw(p *core.Painter) {
    // Use cached style, avoid recomputing
    p.Fill(w.Rect(), ' ', w.cachedStyle)
}
```

### 4. Test Both Light and Dark

```go
// Test with Latte (light)
th := theme.WithPalette(theme.Latte)
app.SetTheme(th)
// Verify readability

// Test with Mocha (dark)
th = theme.WithPalette(theme.Mocha)
app.SetTheme(th)
// Verify readability
```

## See Also

- [Theming Concept](../core-concepts/theming.md) - Core concepts
- [TexelApp Mode](texelapp-mode.md) - App integration
- [ColorPicker Widget](../widgets/colorpicker.md) - Theme color selection

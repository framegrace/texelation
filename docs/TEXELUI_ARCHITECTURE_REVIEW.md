# TexelUI Architecture Review & Recommendations

**Date:** 2025-11-18
**Purpose:** Evaluate TexelUI for ease of form building and widget development
**Goal:** Make it as easy to use as popular TUI libraries (bubbletea, tview, gocui, etc.)

---

## Executive Summary

TexelUI has a **solid, clean foundation** with good widget abstractions, focus management, and dirty-region rendering. However, it needs **higher-level primitives** for productive form building and widget composition.

**Current State:** Low-level widget kernel (think: raw Win32 API)
**Desired State:** High-level form builder (think: React, SwiftUI, Flutter)

---

## What Works Well

### 1. Core Architecture ✅
- **Widget interface** is minimal and composable
- **BaseWidget** provides sensible defaults (DRY)
- **UIManager** handles focus, events, dirty regions cleanly
- **Painter** abstraction keeps drawing code simple
- **Theme integration** is well-designed

### 2. Event Handling ✅
- Focus management with Tab/Shift-Tab traversal
- Click-to-focus with proper hit-testing
- Mouse capture for drag operations
- Clean event propagation model

### 3. Rendering ✅
- Efficient dirty-region tracking
- Rectangle merging to reduce redraws
- Proper clipping support

---

## Missing Pieces for Form Building

### 1. Common Form Widgets ❌

**Currently have:** TextArea, Border, Pane
**Need for forms:**
- Label (static text with alignment)
- Button (clickable, keyboard activatable)
- Checkbox (toggle state)
- RadioButton (mutually exclusive groups)
- Select/Dropdown (choose from list)
- Input (single-line text entry with validation)
- ProgressBar
- Spinner/LoadingIndicator

**Example of current pain:**
```go
// Today: ~20 lines to create a labeled input field
pane := widgets.NewPane(0, 0, 40, 10, style)
label := widgets.NewLabel(1, 1, "Username:")  // ❌ Doesn't exist
input := widgets.NewInput(12, 1, 25)         // ❌ Doesn't exist
// Manual positioning, no relationship between label and input
```

### 2. Layout Managers ❌

**Currently have:** Absolute (manual x,y,w,h)
**Need:**
- VBox (vertical stack)
- HBox (horizontal row)
- Grid (rows × columns)
- Flex (flexible sizing)
- Padding/Margins/Spacing helpers

**Example of current pain:**
```go
// Today: Manual calculation for each widget
label1 := NewLabel(5, 5, "Name:")
input1 := NewInput(15, 5, 30)
label2 := NewLabel(5, 7, "Email:")  // Calculate Y = 5 + 2
input2 := NewInput(15, 7, 30)
// If you insert a widget, recalculate EVERYTHING
```

**What we need:**
```go
// Desired: Automatic layout
form := NewVBox(Padding(5))
form.Add(NewHBox(
    NewLabel("Name:"),
    NewInput().Width(30),
))
form.Add(NewHBox(
    NewLabel("Email:"),
    NewInput().Width(30),
))
```

### 3. Declarative Builder API ❌

**Current:** Imperative, verbose
```go
border := widgets.NewBorder(0, 0, 50, 20, style)
ta := widgets.NewTextArea(0, 0, 0, 0)
ta.SetFocusable(true)
border.SetChild(ta)
ui.AddWidget(border)
ui.Focus(ta)
```

**Desired:** Fluent, declarative
```go
UI().
    Widget(Border().
        Child(TextArea().
            Focusable(true).
            Focused(true))).
    Size(50, 20)
```

Or functional:
```go
UI(
    Border(
        style,
        TextArea(
            Focusable(),
            Focused(),
        ),
    ),
)
```

### 4. Form Validation Framework ❌

Forms need:
- Field validators (required, email, minLength, etc.)
- Real-time validation feedback
- Form-level validation (password confirmation, etc.)
- Error message display

**Example:**
```go
form := NewForm()
email := form.AddInput("Email",
    Required(),
    Email(),
)
password := form.AddInput("Password",
    Required(),
    MinLength(8),
)
confirmPassword := form.AddInput("Confirm",
    Required(),
    MatchesField(password, "Passwords must match"),
)

if form.Validate() {
    // Submit
}
```

### 5. Data Binding ❌

Forms need to bind to structs:
```go
type User struct {
    Name  string
    Email string
    Age   int
}

user := &User{}
form := NewForm(user)
form.AddInput("Name", BindTo(&user.Name))
form.AddInput("Email", BindTo(&user.Email))
form.AddInt("Age", BindTo(&user.Age))

// On submit, user struct is automatically populated
```

### 6. Container Widgets ❌

Need containers beyond Border:
- VBox/HBox (directional stacks)
- Grid (table layout)
- ScrollPane (scrollable viewport)
- Tabs (tabbed interface)
- SplitPane (resizable split)

---

## Comparison with Popular TUI Libraries

### tview (Go)
**Strengths:**
- Rich widget set (Form, Table, List, Tree, Modal, etc.)
- Flex layout manager
- Focus management
- Color tags in text

**Example:**
```go
form := tview.NewForm()
form.AddInputField("Name", "", 20, nil, nil)
form.AddPasswordField("Password", "", 20, '*', nil)
form.AddButton("Save", saveFunc)
form.AddButton("Cancel", cancelFunc)
```

### bubbletea (Go)
**Strengths:**
- Elm-inspired (Model-Update-View)
- Composable components
- Strong typed messages
- Active community

**Example:**
```go
type model struct {
    textInput textinput.Model
    err error
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // Handle messages
}

func (m model) View() string {
    return fmt.Sprintf("Enter name: %s", m.textInput.View())
}
```

### What TexelUI Should Learn:
1. **Rich widget library** (tview's breadth)
2. **Layout managers** (tview's Flex)
3. **Declarative API** (bubbletea's composability)
4. **Sensible defaults** (both libraries minimize boilerplate)

---

## Architectural Recommendations

### Priority 1: Common Widgets (2-3 days)

Add these widgets to `texelui/widgets/`:

1. **Label** - Static text with alignment (left/center/right)
2. **Button** - Clickable with Enter/Space activation
3. **Input** - Single-line text entry (based on TextArea)
4. **Checkbox** - Toggle with visual indicator
5. **RadioButton** - Mutually exclusive selection

Each should:
- Use theme colors by default
- Support focused styling
- Have sensible size defaults
- Work with tab navigation

### Priority 2: Layout Managers (2-3 days)

Add to `texelui/layout/`:

1. **VBox** - Vertical stack with spacing
2. **HBox** - Horizontal row with spacing
3. **Grid** - Rows × columns
4. **Padding** - Add padding around widgets

Interface:
```go
type Layout interface {
    Apply(container Rect, children []Widget)
    // Optional: preferred size hints
    PreferredSize(children []Widget) (w, h int)
}
```

### Priority 3: Builder API (1-2 days)

Add fluent builders to make widget creation less verbose:

```go
// Option 1: Fluent methods on widgets
btn := Button("Submit").
    Width(20).
    OnClick(submitHandler).
    Focused()

// Option 2: Functional options
btn := NewButton("Submit",
    Width(20),
    OnClick(submitHandler),
    Focused(),
)
```

### Priority 4: Form Helper (2-3 days)

Add `texelui/forms/` package:

```go
form := NewForm()
form.AddRow("Name:", NewInput())
form.AddRow("Email:", NewInput())
form.AddRow("", NewButton("Submit"))
// Automatic label alignment, spacing, layout
```

With validation:
```go
form.AddRow("Email:", NewInput().
    Validate(Required(), Email()))
```

### Priority 5: Container Widgets (3-4 days)

Add containers:
- **Panel** - Box with title and optional border
- **ScrollPane** - Scrollable viewport
- **Tabs** - Tabbed interface
- **SplitPane** - Resizable split (future)

---

## Implementation Plan

### Phase 1: Essential Widgets (Week 1)
- [ ] Label widget
- [ ] Button widget
- [ ] Input widget (single-line)
- [ ] Checkbox widget
- [ ] Basic validation (Required, MinLength, Email)

### Phase 2: Layout System (Week 1-2)
- [ ] VBox layout
- [ ] HBox layout
- [ ] Padding/Spacing helpers
- [ ] Update UIManager to support layouts

### Phase 3: Form Builder (Week 2)
- [ ] Form widget/helper
- [ ] Automatic label/input pairing
- [ ] Form validation integration
- [ ] Submit/Cancel pattern

### Phase 4: Polish (Week 2-3)
- [ ] Builder/fluent API
- [ ] RadioButton widget
- [ ] Grid layout
- [ ] Documentation & examples

---

## Example: Before vs After

### Before (Current)
```go
// 40+ lines of manual positioning
ui := core.NewUIManager()
ui.Resize(60, 20)

// Create bordered container
border := widgets.NewBorder(5, 3, 50, 14, tcell.StyleDefault)

// Manually position each widget
nameLabel := widgets.NewLabel(1, 1, "Name:")
nameInput := widgets.NewTextArea(10, 1, 35, 1)
nameInput.SetFocusable(true)

emailLabel := widgets.NewLabel(1, 3, "Email:")
emailInput := widgets.NewTextArea(10, 3, 35, 1)
emailInput.SetFocusable(true)

submitBtn := widgets.NewButton(10, 5, 10, 1, "Submit")
submitBtn.SetFocusable(true)

// Wire everything up
// ... more boilerplate ...
```

### After (Proposed)
```go
// 10-15 lines, automatic layout
ui := core.NewUIManager()
ui.Resize(60, 20)

form := NewForm().
    AddRow("Name:", NewInput().Width(35)).
    AddRow("Email:", NewInput().Width(35).Validate(Required(), Email())).
    AddRow("", NewButton("Submit").OnClick(handleSubmit))

ui.AddWidget(Border().Child(form))
ui.Focus(form)
```

---

## Comparison: TexelUI vs Similar Libraries

| Feature | TexelUI (Current) | tview | bubbletea | Recommended |
|---------|-------------------|-------|-----------|-------------|
| Core abstractions | ✅ Excellent | ✅ Good | ✅ Excellent | Keep current |
| Widget variety | ❌ 3 widgets | ✅ 15+ widgets | ⚠️ Component library | Add 8-10 core widgets |
| Layout system | ❌ Manual only | ✅ Flex layout | ⚠️ Manual | Add VBox/HBox/Grid |
| Forms | ❌ No helpers | ✅ Form widget | ⚠️ DIY | Add Form helper |
| Validation | ❌ None | ⚠️ Basic | ❌ None | Add validation framework |
| API style | ⚠️ Verbose | ⚠️ Verbose | ✅ Declarative | Add builder API |
| Theme support | ✅ Integrated | ✅ Good | ⚠️ Basic | Keep current |
| Focus management | ✅ Excellent | ✅ Good | ⚠️ DIY | Keep current |
| Performance | ✅ Dirty regions | ⚠️ Full redraw | ⚠️ Full redraw | Keep current |

**Verdict:** TexelUI has the best foundation (architecture, performance), but needs higher-level primitives to match the ergonomics of tview and bubbletea.

---

## Quick Wins (Can Implement Today)

### 1. Label Widget (30 minutes)
```go
type Label struct {
    core.BaseWidget
    Text  string
    Style tcell.Style
    Align Alignment // Left, Center, Right
}
```

### 2. Button Widget (1 hour)
```go
type Button struct {
    core.BaseWidget
    Text    string
    Style   tcell.Style
    OnClick func()
}
// HandleKey: trigger on Enter/Space
// HandleMouse: trigger on click
```

### 3. VBox Layout (1 hour)
```go
type VBox struct {
    Spacing int
}

func (v *VBox) Apply(container Rect, children []Widget) {
    y := container.Y
    for _, child := range children {
        child.SetPosition(container.X, y)
        _, h := child.Size()
        y += h + v.Spacing
    }
}
```

---

## Conclusion

TexelUI is **architecturally sound** but lacks the **high-level abstractions** needed for rapid form development. The recommendations above will bring it on par with mature TUI libraries while maintaining its performance advantages and clean design.

**Priority order:**
1. Core widgets (Label, Button, Input, Checkbox) - Essential
2. Layout managers (VBox, HBox) - High impact
3. Form helper - Makes common case trivial
4. Builder API - Reduces boilerplate
5. Advanced features (validation, data binding) - Nice to have

**Estimated effort:** 2-3 weeks for Priorities 1-3, which covers 80% of form use cases.
